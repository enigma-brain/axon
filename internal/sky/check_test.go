package sky

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/enigma-brain/axon/internal/apiserver"
	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/configfile"
	"github.com/enigma-brain/axon/internal/connected"
	"github.com/enigma-brain/axon/internal/fail"
	"github.com/enigma-brain/axon/internal/tsh"
	"github.com/enigma-brain/axon/internal/tunnel"
)

const (
	clientVersion = "0.13.0"
	fakeTshPath   = "/opt/axon-test/tsh"
	// apiServerVar names the URL of the stand-in API server in a helper
	// process.
	apiServerVar = "AXON_TEST_API_SERVER"
)

// The ways the stand-in API server answers the health request.
const (
	// answerOK is a logged-in answer.
	answerOK = "ok"
	// answerNeedsAuth is SkyPilot's answer without a login.
	answerNeedsAuth = "needs_auth"
	// answerError is an HTTP 502, what a proxy sends when the server
	// behind it is gone.
	answerError = "error"
	// answerDrop closes the connection without an answer.
	answerDrop = "drop"
)

// apiServer is a local HTTP server standing in for the SkyPilot API server.
type apiServer struct {
	srv      *httptest.Server
	mu       sync.Mutex
	answer   string
	version  string
	requests int
	// cookies counts the requests that carried a cookie.
	cookies int
}

func newAPIServer(t *testing.T) *apiServer {
	t.Helper()
	a := &apiServer{answer: answerOK, version: clientVersion}
	a.srv = httptest.NewServer(http.HandlerFunc(a.serve))
	t.Cleanup(a.srv.Close)
	return a
}

func (a *apiServer) URL() string { return a.srv.URL }

func (a *apiServer) set(answer, version string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.answer, a.version = answer, version
}

func (a *apiServer) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.requests
}

func (a *apiServer) serve(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	a.requests++
	if r.Header.Get("Cookie") != "" {
		a.cookies++
	}
	answer, version := a.answer, a.version
	a.mu.Unlock()
	if r.URL.Path != "/api/health" {
		http.NotFound(w, r)
		return
	}
	switch answer {
	case answerOK:
		fmt.Fprintf(w, `{"status":"healthy","version":%q,"api_version":"56","user":{"name":"ada@example.org","id":"ada"}}`, version)
	case answerNeedsAuth:
		fmt.Fprintf(w, `{"status":"needs_auth","version":%q,"api_version":"56"}`, version)
	case answerError:
		http.Error(w, "bad gateway", http.StatusBadGateway)
	case answerDrop:
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}
}

// connected returns probes under which the check passes without a network.
func connectedProbes() probes {
	return probes{
		endpoint: Endpoint,
		health: func(context.Context, string, apiserver.Options) (apiserver.Health, error) {
			return apiserver.Health{User: "ada@example.org", ServerVersion: clientVersion}, nil
		},
		cookieFile:    func() (string, error) { return "", nil },
		clientVersion: func(axondir.Dir) (string, bool, error) { return clientVersion, true, nil },
		findTsh:       func() (string, error) { return "", errors.New("unexpected tsh lookup") },
		sessionValid: func(context.Context, string, string) (bool, error) {
			return false, errors.New("unexpected certificate check")
		},
		checkTunnel: func(context.Context, string, string, string) error { return nil },
		startTunnel: func(context.Context, string, string, string, string) error {
			return errors.New("unexpected tunnel start")
		},
	}
}

// helperProbes returns the probes of a helper process: the health request
// goes to the stand-in API server at url, through the real apiserver.Fetch.
func helperProbes(url string) probes {
	p := connectedProbes()
	p.endpoint = func(configfile.Pathway) string { return url }
	p.health = apiserver.Fetch
	return p
}

// world is what one check test sets up: the stand-in API server, a fake
// tsh and a fake tunnel start.
type world struct {
	server *apiServer
	// tshErr is what the fake tsh lookup returns.
	tshErr error
	// valid is whether the fake Teleport certificate is valid.
	valid bool
	// start is what the fake tunnel start does.
	start  func(*apiServer) error
	starts int
	// portErr is what the fake check of the tunnel's port returns, and
	// portChecks counts the checks.
	portErr    error
	portChecks int
}

func newWorld(t *testing.T) *world {
	return &world{
		server: newAPIServer(t),
		valid:  true,
		start: func(a *apiServer) error {
			a.set(answerOK, clientVersion)
			return nil
		},
	}
}

func (w *world) probes() probes {
	p := helperProbes(w.server.URL())
	p.findTsh = func() (string, error) {
		if w.tshErr != nil {
			return "", w.tshErr
		}
		return fakeTshPath, nil
	}
	p.sessionValid = func(_ context.Context, path, proxy string) (bool, error) {
		if path != fakeTshPath || proxy != "teleport.example.org" {
			return false, fmt.Errorf("certificate checked with %q for %q", path, proxy)
		}
		return w.valid, nil
	}
	p.checkTunnel = func(_ context.Context, stateDir, proxy, app string) error {
		w.portChecks++
		if filepath.Base(stateDir) != "state" || proxy != "teleport.example.org" || app != "skypilot" {
			return fmt.Errorf("port checked in %q for %q through %q", stateDir, app, proxy)
		}
		return w.portErr
	}
	p.startTunnel = func(_ context.Context, stateDir, path, proxy, app string) error {
		w.starts++
		if path != fakeTshPath || filepath.Base(stateDir) != "state" || proxy != "teleport.example.org" || app != "skypilot" {
			return fmt.Errorf("tunnel started with %q in %q to %q through %q", path, stateDir, app, proxy)
		}
		return w.start(w.server)
	}
	return p
}

// runChecked logs in on pathway p, runs axon sky status under w's
// probes, and returns the captured sky calls, standard error and error.
func runChecked(t *testing.T, p string, w *world) (*[]call, string, error) {
	t.Helper()
	dir := login(t, p, "ada@example.org")
	calls := capture(t)
	check = w.probes()
	_, errOut, err := runSky("status")
	if _, statErr := os.Stat(filepath.Join(dir.Root(), "tools")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("something was written under ~/.axon/tools: %v", statErr)
	}
	return calls, errOut, err
}

func restarts(errOut string) int {
	return strings.Count(errOut, restartLine+"\n")
}

func TestCheckRunsTheCommandWhenConnected(t *testing.T) {
	for p := range pathways {
		t.Run(p, func(t *testing.T) {
			w := newWorld(t)
			calls, errOut, err := runChecked(t, p, w)
			if err != nil {
				t.Fatal(err)
			}
			if len(*calls) != 1 {
				t.Errorf("sky ran %d times, want 1", len(*calls))
			}
			if errOut != "" || w.starts != 0 || w.server.count() != 1 {
				t.Errorf("stderr %q, %d restarts, %d requests; want none, 0, 1", errOut, w.starts, w.server.count())
			}
			wantChecks := 0
			if p == "remote" {
				wantChecks = 1
			}
			if w.portChecks != wantChecks {
				t.Errorf("%d checks of the tunnel's port, want %d", w.portChecks, wantChecks)
			}
		})
	}
}

func TestCheckRunsTheCommandWhenTheServerReportsNoVersion(t *testing.T) {
	w := newWorld(t)
	w.server.set(answerOK, "")
	calls, _, err := runChecked(t, "campus", w)
	if err != nil || len(*calls) != 1 {
		t.Errorf("err = %v, sky ran %d times; want the command to run", err, len(*calls))
	}
}

func TestCheckRestartsADownTunnelOnce(t *testing.T) {
	for _, answer := range []string{answerDrop, answerError} {
		t.Run(answer, func(t *testing.T) {
			w := newWorld(t)
			w.server.set(answer, clientVersion)
			calls, errOut, err := runChecked(t, "remote", w)
			if err != nil {
				t.Fatal(err)
			}
			if len(*calls) != 1 {
				t.Errorf("sky ran %d times, want 1", len(*calls))
			}
			if errOut != restartLine+"\n" {
				t.Errorf("stderr = %q, want the one restart line", errOut)
			}
			if w.starts != 1 || w.server.count() != 2 {
				t.Errorf("%d restarts and %d requests, want 1 and 2", w.starts, w.server.count())
			}
		})
	}
}

func TestCheckActsOnTheRequestAfterTheRestart(t *testing.T) {
	w := newWorld(t)
	w.server.set(answerDrop, clientVersion)
	w.start = func(a *apiServer) error {
		a.set(answerOK, "0.14.0")
		return nil
	}
	calls, _, err := runChecked(t, "remote", w)
	if !errors.Is(err, errVersion) {
		t.Errorf("err = %v, want %v", err, errVersion)
	}
	if len(*calls) != 0 {
		t.Error("sky ran")
	}
}

func TestCheckStops(t *testing.T) {
	exited := &tunnel.StartError{Exited: true, Log: "ERROR: app certificate has expired"}
	for _, tc := range []struct {
		name    string
		pathway string
		setup   func(*world)
		// want is the whole message, or its beginning when wantLog is set.
		want    string
		wantLog string
		// starts and requests are the restarts and health requests made.
		starts, requests int
	}{
		{
			name: "Teleport: the Teleport certificate has expired", pathway: "remote",
			setup: func(w *world) { w.server.set(answerDrop, clientVersion); w.valid = false },
			want:  "Teleport session expired. Run `axon login`.", requests: 1,
		},
		{
			name: "Teleport: tsh is not found", pathway: "remote",
			setup: func(w *world) { w.server.set(answerDrop, clientVersion); w.tshErr = tsh.ErrNotFound },
			want:  "this pathway needs Teleport's client, tsh, and no tsh was found on PATH. Install it from https://goteleport.com/docs/installation/ and run `axon login` again.", requests: 1,
		},
		{
			name: "Teleport: the tunnel's process exits during the restart", pathway: "remote",
			setup: func(w *world) {
				w.server.set(answerDrop, clientVersion)
				w.start = func(*apiServer) error { return exited }
			},
			want:    "Couldn't restart the Teleport tunnel. Run `axon login`.",
			wantLog: exited.Log, starts: 1, requests: 1,
		},
		{
			name: "Teleport: the server does not answer through the restarted tunnel", pathway: "remote",
			setup: func(w *world) {
				w.server.set(answerError, clientVersion)
				w.start = func(*apiServer) error { return &tunnel.StartError{Log: "Proxying connections"} }
			},
			want:   "The SkyPilot API server is not answering. Retry later.",
			starts: 1, requests: 1,
		},
		{
			name: "Teleport: the restarted tunnel serves and the server still does not answer", pathway: "remote",
			setup: func(w *world) {
				w.server.set(answerDrop, clientVersion)
				w.start = func(*apiServer) error { return nil }
			},
			want:   "The SkyPilot API server is not answering. Retry later.",
			starts: 1, requests: 2,
		},
		{
			name: "Teleport: another process holds the port", pathway: "remote",
			setup: func(w *world) {
				w.server.set(answerDrop, clientVersion)
				w.start = func(*apiServer) error { return &tunnel.PortBusyError{PID: 42, Command: "nc -l 46581"} }
			},
			want:   "port 46581 is held by process 42 (nc -l 46581), which is not the Teleport tunnel. Stop that process and retry.",
			starts: 1, requests: 1,
		},
		{
			name: "Teleport: another process holds the port before the request", pathway: "remote",
			setup: func(w *world) {
				w.portErr = &tunnel.PortBusyError{PID: 42, Command: "nc -l 46581"}
			},
			want: "port 46581 is held by process 42 (nc -l 46581), which is not the Teleport tunnel. Stop that process and retry.",
		},
		{
			name: "Teleport: the tunnel's port cannot be checked", pathway: "remote",
			setup: func(w *world) { w.portErr = errors.New("lsof did not finish") },
			want:  "Couldn't check the Teleport tunnel: lsof did not finish. Run `axon login`.",
		},
		{
			name: "Teleport: not logged in through the tunnel", pathway: "remote",
			setup: func(w *world) { w.server.set(answerNeedsAuth, clientVersion) },
			want:  "SkyPilot session expired. Run `axon login`.", requests: 1,
		},
		{
			name: "Direct: the session has expired", pathway: "campus",
			setup: func(w *world) { w.server.set(answerNeedsAuth, clientVersion) },
			want:  "SkyPilot session expired. Run `axon login`.", requests: 1,
		},
		{
			name: "Direct: the endpoint is unreachable", pathway: "campus",
			setup:   func(w *world) { w.server.set(answerDrop, clientVersion) },
			want:    "Can't reach ", // the host is the stand-in's; see TestDirectUnreachableMessage
			wantLog: "", requests: 1,
		},
		{
			name: "Direct: the server answers with an error", pathway: "campus",
			setup: func(w *world) { w.server.set(answerError, clientVersion) },
			want:  "The SkyPilot API server is not answering. Retry later.", requests: 1,
		},
		{
			name: "Direct: the client's version differs from the server's", pathway: "campus",
			setup: func(w *world) { w.server.set(answerOK, "0.14.0") },
			want:  "Run `axon login` to update the SkyPilot client.", requests: 1,
		},
		{
			name: "Teleport: the client's version differs from the server's", pathway: "remote",
			setup: func(w *world) { w.server.set(answerOK, "0.14.0") },
			want:  "Run `axon login` to update the SkyPilot client.", requests: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			tc.setup(w)
			calls, errOut, err := runChecked(t, tc.pathway, w)
			if err == nil {
				t.Fatal("the check passed, want it to stop")
			}
			msg := err.Error()
			switch {
			case tc.wantLog != "":
				if !strings.HasPrefix(msg, tc.want) || !strings.HasSuffix(msg, "\n"+tc.wantLog) {
					t.Errorf("error = %q, want %q followed by the log %q", msg, tc.want, tc.wantLog)
				}
			case strings.HasSuffix(tc.want, " "):
				if !strings.HasPrefix(msg, tc.want) {
					t.Errorf("error = %q, want it to begin %q", msg, tc.want)
				}
			default:
				if msg != tc.want {
					t.Errorf("error = %q, want %q", msg, tc.want)
				}
			}
			if strings.Contains(tc.want, "not answering") && strings.Contains(msg, "axon login") {
				t.Errorf("error %q suggests axon login for an unavailable server", msg)
			}
			if len(*calls) != 0 {
				t.Error("sky ran")
			}
			if w.starts != tc.starts || restarts(errOut) != tc.starts {
				t.Errorf("%d restarts, %d restart lines; want %d", w.starts, restarts(errOut), tc.starts)
			}
			if w.server.count() != tc.requests {
				t.Errorf("%d health requests, want %d", w.server.count(), tc.requests)
			}
		})
	}
}

func TestCheckSendsCookiesOnlyToADirectEndpoint(t *testing.T) {
	for _, tc := range []struct {
		pathway string
		want    int
	}{{"campus", 1}, {"remote", 0}} {
		t.Run(tc.pathway, func(t *testing.T) {
			w := newWorld(t)
			host := strings.TrimPrefix(w.server.URL(), "http://")
			host = host[:strings.LastIndex(host, ":")]
			jar := filepath.Join(t.TempDir(), "cookies.txt")
			line := host + "\tFALSE\t/\tFALSE\t0\tsession\tsecret\n"
			if err := os.WriteFile(jar, []byte(line), 0o600); err != nil {
				t.Fatal(err)
			}
			login(t, tc.pathway, "ada@example.org")
			capture(t)
			check = w.probes()
			check.cookieFile = func() (string, error) { return jar, nil }
			if _, _, err := runSky("status"); err != nil {
				t.Fatal(err)
			}
			w.server.mu.Lock()
			got := w.server.cookies
			w.server.mu.Unlock()
			if got != tc.want {
				t.Errorf("%d requests carried a cookie, want %d", got, tc.want)
			}
		})
	}
}

func TestCheckStopsWithoutAClient(t *testing.T) {
	w := newWorld(t)
	login(t, "campus", "ada@example.org")
	calls := capture(t)
	check = w.probes()
	check.clientVersion = func(axondir.Dir) (string, bool, error) { return "", false, nil }
	_, _, err := runSky("status")
	if err == nil || err.Error() != "the SkyPilot client is not installed. Run `axon login` first." {
		t.Errorf("err = %v", err)
	}
	if len(*calls) != 0 || w.server.count() != 0 {
		t.Errorf("sky ran %d times and %d requests were made, want none", len(*calls), w.server.count())
	}
}

func TestDirectUnreachableMessage(t *testing.T) {
	p := pathwayOf(t, "campus")
	err := failure(context.Background(), p, Endpoint(p), apiserver.ErrUnreachable)
	want := "Can't reach sky.example.org. Connect to the campus VPN and retry."
	if err == nil || err.Error() != want {
		t.Errorf("failure = %v, want %q", err, want)
	}
	// Without a hint, the message is the host alone.
	p.UnreachableHint = ""
	err = failure(context.Background(), p, Endpoint(p), apiserver.ErrUnreachable)
	if want := "Can't reach sky.example.org."; err == nil || err.Error() != want {
		t.Errorf("failure = %v, want %q", err, want)
	}
}

// TestCheckNeverInstallsOrOpensABrowser checks the source of the package:
// it calls none of the functions that install something or open a browser.
func TestCheckNeverInstallsOrOpensABrowser(t *testing.T) {
	forbidden := map[string][]string{
		"skyclient": {"Ensure"},
		"tsh":       {"Ensure", "Login", "AppLogin"},
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok {
				for _, fn := range forbidden[pkg.Name] {
					if sel.Sel.Name == fn {
						t.Errorf("%s calls %s.%s", fset.Position(sel.Pos()), pkg.Name, fn)
					}
				}
			}
			return true
		})
		if strings.Contains(mustRead(t, name), `"api", "login"`) {
			t.Errorf("%s runs sky api login", name)
		}
	}
}

func mustRead(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestStopExitsNonZeroWithTheMessage(t *testing.T) {
	sky := writeFakeSky(t)
	home := t.TempDir()
	storeConfig(t, home)
	dir := axondir.New(home)
	if err := dir.SetPathway("campus"); err != nil {
		t.Fatal(err)
	}
	if err := connected.Write(dir, pathwayOf(t, "campus")); err != nil {
		t.Fatal(err)
	}
	if err := dir.SetEmail("ada@example.org"); err != nil {
		t.Fatal(err)
	}
	srv := newAPIServer(t)
	srv.set(answerNeedsAuth, clientVersion)
	out, errOut, code := helper(t, home, sky, srv.URL(), "status")
	if out != "" {
		t.Errorf("sky ran: %q", out)
	}
	if errOut != "axon: SkyPilot session expired. Run `axon login`.\n" {
		t.Errorf("stderr = %q", errOut)
	}
	if code != fail.Code {
		t.Errorf("exit code = %d, want %d", code, fail.Code)
	}
}

// A login that records another email while the health request runs does
// not reach the command: it runs with the email the check read.
func TestCommandRunsWithTheCheckedEmail(t *testing.T) {
	dir := login(t, "campus", "ada@example.org")
	calls := capture(t)
	p := connectedProbes()
	health := p.health
	p.health = func(ctx context.Context, endpoint string, opts apiserver.Options) (apiserver.Health, error) {
		if err := dir.SetEmail("grace@example.org"); err != nil {
			t.Fatal(err)
		}
		return health(ctx, endpoint, opts)
	}
	check = p
	if _, _, err := runSky("status"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || !slices.Contains((*calls)[0].env, userVar+"=ada@example.org") {
		t.Errorf("sky ran with %v, want SKYPILOT_USER=ada@example.org", *calls)
	}
}

// TestCheckStopsWhenCancelled cancels the check at each step that waits,
// and checks that it stops with the cancellation and repairs nothing more.
func TestCheckStopsWhenCancelled(t *testing.T) {
	failed := errors.New("the request failed")
	for _, tc := range []struct {
		name    string
		pathway string
		// setup makes one step cancel the check, by cancel, and fail.
		setup  func(p *probes, cancel func())
		starts int
	}{
		{
			name: "direct: during the health request", pathway: "campus",
			setup: func(p *probes, cancel func()) {
				p.health = func(context.Context, string, apiserver.Options) (apiserver.Health, error) {
					cancel()
					return apiserver.Health{}, failed
				}
			},
		},
		{
			name: "teleport: during the health request, with no restart", pathway: "remote",
			setup: func(p *probes, cancel func()) {
				p.health = func(context.Context, string, apiserver.Options) (apiserver.Health, error) {
					cancel()
					return apiserver.Health{}, failed
				}
			},
		},
		{
			name: "teleport: during the check of the tunnel's port", pathway: "remote",
			setup: func(p *probes, cancel func()) {
				p.checkTunnel = func(context.Context, string, string, string) error {
					cancel()
					return failed
				}
			},
		},
		{
			name: "teleport: during the restart of the tunnel", pathway: "remote", starts: 1,
			setup: func(p *probes, cancel func()) {
				p.health = func(context.Context, string, apiserver.Options) (apiserver.Health, error) {
					return apiserver.Health{}, failed
				}
				p.findTsh = func() (string, error) { return fakeTshPath, nil }
				p.sessionValid = func(context.Context, string, string) (bool, error) { return true, nil }
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := login(t, tc.pathway, "ada@example.org")
			calls := capture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := connectedProbes()
			starts := 0
			p.startTunnel = func(context.Context, string, string, string, string) error {
				starts++
				cancel()
				return errors.New("the tunnel did not start")
			}
			tc.setup(&p, cancel)
			check = p
			_, _, _, err := checkConnection(ctx, dir, io.Discard)
			if !errors.Is(err, context.Canceled) {
				t.Errorf("checkConnection returned %v, want the cancellation", err)
			}
			if starts != tc.starts {
				t.Errorf("the tunnel was started %d times, want %d", starts, tc.starts)
			}
			if len(*calls) != 0 {
				t.Error("sky ran")
			}
		})
	}
}
