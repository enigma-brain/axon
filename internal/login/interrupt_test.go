package login

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/enigma-brain/axon/internal/apiserver"
	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/configfile"
	"github.com/enigma-brain/axon/internal/interrupt"
	"github.com/enigma-brain/axon/internal/tunnel"
)

// cancelOn is an output that cancels a login, after a short delay, once
// what was written to it holds trigger: the delay lets the login reach the
// wait that follows the text, such as the read of an answer.
type cancelOn struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	trigger string
	cancel  func()
	fired   bool
}

func (c *cancelOn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf.Write(p)
	if !c.fired && strings.Contains(c.buf.String(), c.trigger) {
		c.fired = true
		time.AfterFunc(100*time.Millisecond, c.cancel)
	}
	return len(p), nil
}

func (c *cancelOn) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// interrupted runs one login with opts, which is cancelled as by sig once
// its output holds trigger, and returns its error and output. It fails the
// test unless the login stopped with the cancellation, promptly.
func (h *harness) interrupted(opts Options, trigger string, sig syscall.Signal) string {
	h.t.Helper()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	out := &cancelOn{trigger: trigger, cancel: func() { cancel(&interrupt.Error{Signal: sig}) }}
	if opts.In == nil {
		opts.In = strings.NewReader("")
	}
	opts.Out, opts.Err = out, &h.errOut
	start := time.Now()
	err := h.l.run(ctx, opts)
	if !errors.Is(err, context.Canceled) {
		h.t.Fatalf("login returned %v, want the cancellation\n%s", err, out.String())
	}
	if took := time.Since(start); took > 5*time.Second {
		h.t.Errorf("login took %s to stop", took)
	}
	return out.String()
}

// silentInput returns standard input that never sends anything, as a
// terminal where nothing is typed.
func silentInput(t *testing.T) io.Reader {
	r, w := io.Pipe()
	t.Cleanup(func() { w.Close() })
	return r
}

func TestReadLineReturnsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	if _, err := readLine(ctx, silentInput(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("readLine returned %v, want the cancellation", err)
	}
	if _, err := readLine(ctx, strings.NewReader("y\n")); !errors.Is(err, context.Canceled) {
		t.Fatalf("readLine with a cancelled context returned %v, want the cancellation", err)
	}
}

func TestQuestionsReturnWhenCancelled(t *testing.T) {
	ps := []configfile.Pathway{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}}
	for name, ask := range map[string]func(context.Context, io.Reader, io.Writer) error{
		"yes or no": func(ctx context.Context, in io.Reader, out io.Writer) error {
			_, err := askYesNo(ctx, in, out, "? ")
			return err
		},
		"pathway": func(ctx context.Context, in io.Reader, out io.Writer) error {
			_, err := askPathway(ctx, ps, in, out)
			return err
		},
		"line": func(ctx context.Context, in io.Reader, out io.Writer) error {
			_, err := askLine(ctx, in, out, "? ")
			return err
		},
		"valid": func(ctx context.Context, in io.Reader, out io.Writer) error {
			_, err := askValid(ctx, in, out, "? ", "", func(s string) (string, error) { return s, nil })
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(50*time.Millisecond, cancel)
			var out bytes.Buffer
			if err := ask(ctx, silentInput(t), &out); !errors.Is(err, context.Canceled) {
				t.Fatalf("the question returned %v, want the cancellation", err)
			}
			if !strings.HasSuffix(out.String(), "? ") && !strings.HasSuffix(out.String(), "]: ") {
				t.Errorf("the question wrote more after the cancellation: %q", out.String())
			}
		})
	}
}

func TestInterruptAtTheFirstLoginPrompt(t *testing.T) {
	for _, tc := range []struct {
		name, typed, trigger string
	}{
		{"the kind", "", msgKindPrompt},
		{"the name", "1\nsky.example.org\n", "Name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.noFiles()
			h.terminal()
			h.servers(nil)
			in := io.MultiReader(strings.NewReader(tc.typed), silentInput(t))
			h.interrupted(Options{In: in}, tc.trigger, syscall.SIGINT)
			if ids := h.storedIDs(); len(ids) != 0 {
				t.Errorf("stored %v, want nothing", ids)
			}
			if h.active() != "" {
				t.Errorf("the active pathway is %q, want none", h.active())
			}
		})
	}
}

func TestInterruptWhileCheckingAHandMadeServer(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	h.terminal()
	h.l.probeSky = func(ctx context.Context, endpoint string) error {
		<-ctx.Done()
		return errors.New("the server did not answer")
	}
	out := h.interrupted(Options{In: strings.NewReader("1\nsky.example.org\n")}, "sky.example.org", syscall.SIGINT)
	if strings.Contains(out, "did not answer") {
		t.Errorf("the failed check was reported:\n%s", out)
	}
	if ids := h.storedIDs(); len(ids) != 0 {
		t.Errorf("stored %v, want nothing", ids)
	}
}

func TestSkyProbeResultWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := skyProbeResult(ctx, errors.New("dial tcp: operation was canceled")); !errors.Is(err, context.Canceled) {
		t.Fatalf("skyProbeResult = %v, want the cancellation", err)
	}
}

func TestInterruptAtThePathwayQuestion(t *testing.T) {
	h := newHarness(t)
	h.interrupted(Options{In: silentInput(t)}, "Pathway [1-2]: ", syscall.SIGINT)
	if h.active() != "" {
		t.Errorf("the active pathway is %q, want none", h.active())
	}
}

// TestInterruptAtAQuestionOnTheTerminal interrupts a question that --config -
// asks on the terminal, since standard input holds the config file.
func TestInterruptAtAQuestionOnTheTerminal(t *testing.T) {
	h := newHarness(t)
	h.store(revised("7"))
	before := h.stored("example")
	tty := silentInput(t)
	h.l.openTerminal = func() (io.ReadCloser, error) { return io.NopCloser(tty), nil }
	h.interrupted(Options{Config: "-", Pathway: "campus", In: strings.NewReader(revised("5"))}, "Replace the stored copy", syscall.SIGINT)
	if after := h.stored("example"); after != before {
		t.Errorf("the stored file changed:\n%s", after)
	}
	entries, err := os.ReadDir(filepath.Join(h.home, ".axon", "configs"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "example.toml" {
			t.Errorf("left %s beside the stored file", e.Name())
		}
	}
}

func TestInterruptAtAClashQuestion(t *testing.T) {
	h := newHarness(t)
	before := h.stored("example")
	clashing := strings.Replace(exampleConfig, `id = "example"`, `id = "other"`, 1)
	h.interrupted(Options{Config: writeConfig(t, clashing), Pathway: "campus", In: silentInput(t)}, msgReplacePrompt, syscall.SIGINT)
	if after := h.stored("example"); after != before {
		t.Errorf("the stored file changed:\n%s", after)
	}
	if ids := h.storedIDs(); len(ids) != 1 {
		t.Errorf("stored %v, want the one file", ids)
	}
}

// TestInterruptDuringTheBrowserLogin forwards SIGTERM to sky, which ends at
// once; the login stops with the cancellation and stores no login.
func TestInterruptDuringTheBrowserLogin(t *testing.T) {
	h := newHarness(t)
	h.script("hang")
	h.interrupted(Options{Pathway: "campus"}, "in your browser", syscall.SIGTERM)
	if h.email() != "" {
		t.Errorf("stored the login of %q", h.email())
	}
}

// TestInterruptDuringTheBrowserLoginWaitsForSky interrupts by SIGINT, which
// the terminal delivers to sky itself: axon sends sky nothing, and kills it
// only once skyWaitDelay has passed.
func TestInterruptDuringTheBrowserLoginWaitsForSky(t *testing.T) {
	h := newHarness(t)
	h.script("hang")
	start := time.Now()
	h.interrupted(Options{Pathway: "campus"}, "in your browser", syscall.SIGINT)
	if took := time.Since(start); took < skyWaitDelay {
		t.Errorf("the login stopped after %s, before sky's grace period of %s", took, skyWaitDelay)
	}
}

// teleportInterrupted runs a teleport login, cancelled by cancelAt, and
// returns its error.
func (h *metaHarness) runCancellable(ctx context.Context) error {
	return h.l.run(ctx, Options{Pathway: "remote", In: strings.NewReader(""), Out: &h.out, Err: &h.errOut})
}

// cancelAtTunnel makes the login cancel ctx when it reaches the tunnel.
func (h *metaHarness) cancelAtTunnel(cancel func()) {
	newTunnel := h.l.teleport.newTunnel
	h.l.teleport.newTunnel = func(stateDir, tshPath, proxy, app string) *tunnel.Tunnel {
		cancel()
		return newTunnel(stateDir, tshPath, proxy, app)
	}
}

// cancelAtTunnelHealth makes the health request through the tunnel cancel
// ctx and fail.
func (h *metaHarness) cancelAtTunnelHealth(cancel func()) {
	health := h.l.health
	h.l.health = func(ctx context.Context, endpoint string, opts apiserver.Options) (apiserver.Health, error) {
		if endpoint == h.endpoint() {
			cancel()
			return apiserver.Health{}, errors.New("the request was cancelled")
		}
		return health(ctx, endpoint, opts)
	}
}

func (h *metaHarness) assertRunningAndRecorded(want bool) {
	h.t.Helper()
	st := h.tunnelStatus()
	if st.Running != want {
		h.t.Errorf("the tunnel runs: %v, want %v", st.Running, want)
	}
	stateDir, err := axondir.New(h.home).StateDir()
	if err != nil {
		h.t.Fatal(err)
	}
	_, err = os.Stat(filepath.Join(stateDir, "tunnel.json"))
	if recorded := err == nil; recorded != want {
		h.t.Errorf("the tunnel is recorded: %v, want %v", recorded, want)
	}
}

func TestInterruptTeleportLogin(t *testing.T) {
	for _, tc := range []struct {
		name string
		// first is whether a login runs first, so that the tunnel is up.
		first bool
		setup func(h *metaHarness, cancel func())
		// running is whether the tunnel is running and recorded after.
		running bool
	}{
		{"checking a tunnel that is down", false, (*metaHarness).cancelAtTunnel, false},
		{"checking a tunnel that is up", true, (*metaHarness).cancelAtTunnel, true},
		{"asking the server through a tunnel that is up", true, (*metaHarness).cancelAtTunnelHealth, true},
		{"asking the server through a tunnel just started", false, (*metaHarness).cancelAtTunnelHealth, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newMetaHarness(t)
			if tc.first {
				if err := h.run("remote"); err != nil {
					t.Fatalf("first login: %v\n%s", err, h.out.String())
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			tc.setup(h, cancel)
			if err := h.runCancellable(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("login returned %v, want the cancellation\n%s", err, h.out.String())
			}
			h.assertRunningAndRecorded(tc.running)
		})
	}
}

// TestInterruptWhileTheTunnelStarts cancels the login while it waits for a
// new tunnel to serve: the tunnel is stopped and not recorded.
func TestInterruptWhileTheTunnelStarts(t *testing.T) {
	h := newMetaHarness(t)
	h.writeTsh("proxy-mode", "refuse")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &cancelOn{trigger: "starting the Teleport tunnel", cancel: cancel}
	err := h.l.run(ctx, Options{Pathway: "remote", In: strings.NewReader(""), Out: out, Err: &h.errOut})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("login returned %v, want the cancellation\n%s", err, out.String())
	}
	h.assertRunningAndRecorded(false)
}
