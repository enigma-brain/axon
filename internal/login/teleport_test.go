package login

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/connected"
	"github.com/enigma-brain/axon/internal/tsh"
	"github.com/enigma-brain/axon/internal/tunnel"
)

// fakeTsh is the path of the fake tsh, built by TestMain into a file named
// tsh so that the tunnel package recognizes its command line.
var fakeTsh string

// The made-up Teleport proxy and application the tests log in to.
const (
	testProxy = "teleport.example.org"
	testApp   = "skypilot"
)

// testPort is a free port the tests' tunnels listen on, so that a real
// tunnel on port 46581 is never touched.
var testPort int

func TestMain(m *testing.M) { os.Exit(runTests(m)) }

func runTests(m *testing.M) int {
	bin, err := os.MkdirTemp("", "login-faketsh")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(bin)
	fakeTsh = filepath.Join(bin, "tsh")
	build := exec.Command("go", "build", "-o", fakeTsh, "./testdata/faketsh")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build the fake tsh:", err)
		return 1
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	testPort = ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	// The tests run only the fakes, by their paths, and the shell tools the
	// fake sky uses. HOME and PATH hold nothing else, so no real login tool,
	// client or browser can be found.
	if err := fence(bin); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	code := m.Run()
	// Stop any fake tunnel a failed test left behind.
	tunnel.NewOnPort(bin, fakeTsh, testProxy, testApp, testPort).Stop()
	return code
}

// fence sets HOME to an empty directory and PATH to a directory under bin
// that holds only the shell tools the fakes run, and lsof, which the tunnel
// package runs on macOS to find a port's listeners.
func fence(bin string) error {
	home := filepath.Join(bin, "home")
	path := filepath.Join(bin, "path")
	for _, dir := range []string{home, path} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return err
		}
	}
	for _, tool := range []string{"sh", "cat", "dirname", "env", "mkdir", "sed", "sleep", "touch", "lsof"} {
		real, err := exec.LookPath(tool)
		if err != nil {
			if tool == "lsof" && runtime.GOOS != "darwin" {
				continue
			}
			return err
		}
		if err := os.Symlink(real, filepath.Join(path, tool)); err != nil {
			return err
		}
	}
	os.Setenv("HOME", home)
	os.Setenv("PATH", path)
	return nil
}

// metaHarness is one login on a teleport pathway against the fake tsh,
// the fake sky and the fake API server, reached through a real tunnel on
// testPort.
type metaHarness struct {
	*harness
	tshDir string
}

func newMetaHarness(t *testing.T) *metaHarness {
	t.Helper()
	h := &metaHarness{harness: newHarness(t), tshDir: t.TempDir()}
	// Through the tunnel, Teleport supplies the identity: the server reports
	// the user without a SkyPilot login.
	h.logIn()
	h.writeTsh("upstream", h.directURL)
	t.Setenv("FAKETSH_DIR", h.tshDir)

	h.l.teleport.ensure = func(context.Context, string) (string, error) {
		return fakeTsh, nil
	}
	environ := h.l.environ
	h.l.environ = func() []string {
		return append(environ(), "FAKESKY_CONFIG="+h.l.teleport.skyConfig)
	}
	t.Cleanup(func() {
		if err := h.tunnel().Stop(); err != nil {
			t.Errorf("stop the tunnel at cleanup: %v", err)
		}
	})
	return h
}

func (h *metaHarness) writeTsh(name, content string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.tshDir, name), []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// tunnel returns the login's tunnel.
func (h *metaHarness) tunnel() *tunnel.Tunnel {
	h.t.Helper()
	stateDir, err := axondir.New(h.home).StateDir()
	if err != nil {
		h.t.Fatal(err)
	}
	return h.l.teleport.newTunnel(stateDir, fakeTsh, testProxy, testApp)
}

// tunnelStatus returns the tunnel's status.
func (h *metaHarness) tunnelStatus() tunnel.Status {
	h.t.Helper()
	st, err := h.tunnel().Status(context.Background())
	if err != nil {
		h.t.Fatal(err)
	}
	return st
}

// tshCalls returns the arguments of each tsh run so far, and forgets them.
func (h *metaHarness) tshCalls() []string {
	h.t.Helper()
	path := filepath.Join(h.tshDir, "calls")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		h.t.Fatal(err)
	}
	os.Remove(path)
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func (h *metaHarness) endpoint() string { return "http://localhost:" + strconv.Itoa(testPort) }

func (h *metaHarness) proxyCall() string {
	return fmt.Sprintf("--proxy=teleport.example.org proxy app skypilot --port %d", testPort)
}

func contains(calls []string, call string) bool {
	for _, c := range calls {
		if c == call {
			return true
		}
	}
	return false
}

func TestTeleportFirstLogin(t *testing.T) {
	h := newMetaHarness(t)
	if err := h.run("remote"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}

	calls := h.tshCalls()
	for _, want := range []string{"status --format=json", "login --proxy=teleport.example.org", "--proxy=teleport.example.org apps login skypilot", h.proxyCall()} {
		if !contains(calls, want) {
			t.Errorf("tsh calls %q lack %q", calls, want)
		}
	}
	if !h.tunnelStatus().Healthy {
		t.Error("the tunnel does not serve after login")
	}
	if n := h.skyRuns(); n != 1 {
		t.Fatalf("sky ran %d times, want 1", n)
	}
	args, _ := os.ReadFile(filepath.Join(h.skyDir, "args"))
	if want := "api login -e " + h.endpoint() + "\n"; string(args) != want {
		t.Errorf("sky args = %q, want %q", args, want)
	}
	if _, ok := h.skyEnv(1)[endpointVar]; ok {
		t.Errorf("sky ran with %s set", endpointVar)
	}
	assertNoPythonVars(t, h.skyEnv(1))
	if got := h.email(); got != h.user {
		t.Errorf("recorded email = %q, want %q", got, h.user)
	}
	if !strings.Contains(h.out.String(), "sky: connected as "+h.user) {
		t.Errorf("output lacks the connected line:\n%s", h.out.String())
	}
}

// With the config file on standard input, tsh's logins read the terminal,
// as axon's own questions do, and not the used-up standard input.
func TestTeleportLoginsReadTheTerminalWithConfigFromStdin(t *testing.T) {
	h := newMetaHarness(t)
	h.noFiles()
	// A file the two logins share, as they share a terminal: each reads one
	// line of it.
	ttyPath := filepath.Join(t.TempDir(), "tty")
	if err := os.WriteFile(ttyPath, []byte("for login\nfor apps login\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tty, err := os.Open(ttyPath)
	if err != nil {
		t.Fatal(err)
	}
	h.l.openTerminal = func() (io.ReadCloser, error) { return tty, nil }
	if err := h.login(Options{Config: "-", Pathway: "remote", In: strings.NewReader(exampleConfig)}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	for name, want := range map[string]string{"stdin.login": "for login\n", "stdin.apps": "for apps login\n"} {
		got, err := os.ReadFile(filepath.Join(h.tshDir, name))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v, want %q", name, got, err, want)
		}
	}
}

// With no terminal, tsh's logins read an empty input.
func TestTeleportLoginsReadNothingWithConfigFromStdinAndNoTerminal(t *testing.T) {
	h := newMetaHarness(t)
	h.noFiles()
	if err := h.login(Options{Config: "-", Pathway: "remote", In: strings.NewReader(exampleConfig)}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	for _, name := range []string{"stdin.login", "stdin.apps"} {
		got, err := os.ReadFile(filepath.Join(h.tshDir, name))
		if err != nil || len(got) != 0 {
			t.Errorf("%s = %q, %v, want it empty", name, got, err)
		}
	}
}

func TestTeleportRepeatedLoginKeepsTunnel(t *testing.T) {
	h := newMetaHarness(t)
	if err := h.run("remote"); err != nil {
		t.Fatalf("first login: %v", err)
	}
	before := h.tunnelStatus()
	h.tshCalls()

	if err := h.run("remote"); err != nil {
		t.Fatalf("second login: %v\n%s", err, h.out.String())
	}
	after := h.tunnelStatus()
	if !after.Healthy || after.PID != before.PID {
		t.Errorf("tunnel after = %+v, want the same process %d serving", after, before.PID)
	}
	calls := h.tshCalls()
	for _, unwanted := range []string{"login --proxy=teleport.example.org", "--proxy=teleport.example.org apps login skypilot", h.proxyCall()} {
		if contains(calls, unwanted) {
			t.Errorf("the repeated login ran tsh %q", unwanted)
		}
	}
	if n := h.skyRuns(); n != 1 {
		t.Errorf("sky ran %d times over two logins, want 1", n)
	}
}

func TestTeleportReplacesDeadTunnel(t *testing.T) {
	h := newMetaHarness(t)
	if err := h.run("remote"); err != nil {
		t.Fatalf("first login: %v", err)
	}
	before := h.tunnelStatus()
	if err := syscall.Kill(before.PID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { return !h.tunnelStatus().Running })
	h.tshCalls()

	if err := h.run("remote"); err != nil {
		t.Fatalf("second login: %v\n%s", err, h.out.String())
	}
	after := h.tunnelStatus()
	if !after.Healthy || after.PID == before.PID {
		t.Errorf("tunnel after = %+v, want a new process serving", after)
	}
	if calls := h.tshCalls(); !contains(calls, "--proxy=teleport.example.org apps login skypilot") {
		t.Errorf("tsh calls %q lack the application login", calls)
	}
}

func TestTeleportPortHeld(t *testing.T) {
	h := newMetaHarness(t)
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(testPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	err = h.run("remote")
	var busy *tunnel.PortBusyError
	if !errors.As(err, &busy) {
		t.Fatalf("err = %v, want a *tunnel.PortBusyError", err)
	}
	msg := err.Error()
	for _, want := range []string{"connect sky: ", "port ", "process " + strconv.Itoa(os.Getpid()), "Stop that process and run `axon login` again."} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q lacks %q", msg, want)
		}
	}
	if h.skyRuns() != 0 {
		t.Error("sky ran although the tunnel did not start")
	}
}

func TestTeleportStoredEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name    string
		config  string // "" writes no file
		skyRuns int
	}{
		{"no file", "", 1},
		{"no key", "kubernetes:\n  context: x\n", 1},
		{"another endpoint", "api_server:\n  endpoint: https://sky.example.org\n", 1},
		{"the tunnel's", "api_server:\n  endpoint: %s\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newMetaHarness(t)
			if tc.config != "" {
				config := tc.config
				if strings.Contains(config, "%s") {
					config = fmt.Sprintf(config, h.endpoint())
				}
				if err := os.MkdirAll(filepath.Dir(h.l.teleport.skyConfig), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(h.l.teleport.skyConfig, []byte(config), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := h.run("remote"); err != nil {
				t.Fatalf("login: %v\n%s", err, h.out.String())
			}
			if n := h.skyRuns(); n != tc.skyRuns {
				t.Errorf("sky ran %d times, want %d", n, tc.skyRuns)
			}
		})
	}
}

func TestTeleportNoIdentityStopsBeforeSky(t *testing.T) {
	h := newMetaHarness(t)
	if err := os.Remove(filepath.Join(h.skyDir, "loggedin")); err != nil {
		t.Fatal(err)
	}
	err := h.run("remote")
	if !errors.Is(err, errNoIdentity) {
		t.Fatalf("err = %v, want errNoIdentity", err)
	}
	if h.skyRuns() != 0 {
		t.Error("sky ran against a server that reports no identity")
	}
}

func TestTeleportValidCertificateSkipsTeleportLogin(t *testing.T) {
	h := newMetaHarness(t)
	h.writeTsh("valid", testProxy)
	if err := h.run("remote"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if calls := h.tshCalls(); contains(calls, "login --proxy=teleport.example.org") {
		t.Errorf("tsh login ran with a valid certificate: %q", calls)
	}
}

// With no tsh on PATH, the login stops before any login step, with the
// real lookup, which makes no request.
func TestTeleportTshMissingStops(t *testing.T) {
	h := newMetaHarness(t)
	h.l.teleport.ensure = tsh.Ensure
	err := h.run("remote")
	want := "establish the Teleport session: this pathway needs Teleport's client, tsh, and no tsh was found on PATH. Install it from https://goteleport.com/docs/installation/ and run `axon login` again."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if h.skyRuns() != 0 || len(h.tshCalls()) != 0 {
		t.Error("login went past the session stage")
	}
}

// A missing tsh leaves the pathway that was connected as it was: the
// active pathway and its record do not change.
func TestTeleportTshMissingKeepsThePreviousPathway(t *testing.T) {
	h := newMetaHarness(t)
	h.script("ok")
	if err := h.run("campus"); err != nil {
		t.Fatalf("login on campus: %v\n%s", err, h.out.String())
	}
	h.l.teleport.ensure = tsh.Ensure
	if err := h.run("remote"); err == nil || !strings.Contains(err.Error(), "no tsh was found on PATH") {
		t.Fatalf("login on remote: err = %v", err)
	}
	if h.active() != "campus" || h.record().ID != "campus" || h.email() == "" {
		t.Errorf("active = %q, record = %+v, email = %q; want campus kept", h.active(), h.record(), h.email())
	}
}

// The login uses the tsh it finds on PATH.
func TestTeleportUsesTshOnPath(t *testing.T) {
	h := newMetaHarness(t)
	t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+filepath.Dir(fakeTsh))
	var found string
	h.l.teleport.ensure = func(context.Context, string) (string, error) {
		path, err := tsh.Find()
		found = path
		return path, err
	}
	if err := h.run("remote"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if found != fakeTsh {
		t.Errorf("found tsh at %q, want %q", found, fakeTsh)
	}
	if calls := h.tshCalls(); !contains(calls, "--proxy=teleport.example.org apps login skypilot") {
		t.Errorf("tsh calls = %q", calls)
	}
}

func TestSwitchToDirectStopsTunnel(t *testing.T) {
	h := newMetaHarness(t)
	if err := h.run("remote"); err != nil {
		t.Fatalf("teleport login: %v", err)
	}
	if !h.tunnelStatus().Running {
		t.Fatal("no tunnel after the teleport login")
	}
	if err := h.run("campus"); err != nil {
		t.Fatalf("direct login: %v\n%s", err, h.out.String())
	}
	if st := h.tunnelStatus(); st.Running {
		t.Errorf("tunnel %+v still runs after the switch to the direct pathway", st)
	}
}

func TestSwitchStopsRecordedTunnelByItsRecord(t *testing.T) {
	h := newMetaHarness(t)
	if err := h.run("remote"); err != nil {
		t.Fatalf("teleport login: %v", err)
	}
	st := h.tunnelStatus()
	if !st.Running {
		t.Fatal("no tunnel after the teleport login")
	}
	// Disconnecting needs no config file: with the file that named
	// the pathway gone, the records are enough.
	if err := os.Remove(filepath.Join(h.home, ".axon", "configs", "example.toml")); err != nil {
		t.Fatal(err)
	}
	h.store(otherConfig)
	if err := h.run("lab"); err != nil {
		t.Fatalf("direct login: %v\n%s", err, h.out.String())
	}
	if !strings.Contains(h.out.String(), "sky: stopping the Teleport tunnel") {
		t.Errorf("output %q does not say the tunnel stopped", h.out.String())
	}
	waitUntil(t, func() bool {
		_, err := os.FindProcess(st.PID)
		return err == nil && syscall.Kill(st.PID, 0) != nil
	})
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestTeleportTunnelExitsPrintsLog(t *testing.T) {
	h := newMetaHarness(t)
	h.writeTsh("proxy-mode", "exit")
	err := h.run("remote")
	var startErr *tunnel.StartError
	if !errors.As(err, &startErr) || !startErr.Exited {
		t.Fatalf("err = %v, want a *tunnel.StartError for an exited tunnel", err)
	}
	msg := err.Error()
	for _, want := range []string{"connect sky: ", "ERROR: app certificate has expired", "Run `axon login` again."} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q lacks %q", msg, want)
		}
	}
	if errors.Is(err, errNotAnswering) {
		t.Error("an exited tunnel is reported as an unavailable server")
	}
	if h.skyRuns() != 0 {
		t.Error("sky ran although the tunnel did not start")
	}
}

func TestTeleportTunnelRunsWithoutAnswerReportsServer(t *testing.T) {
	if testing.Short() {
		t.Skip("waits the full start timeout")
	}
	h := newMetaHarness(t)
	h.writeTsh("proxy-mode", "refuse")
	err := h.run("remote")
	if !errors.Is(err, errNotAnswering) {
		t.Fatalf("err = %v, want errNotAnswering", err)
	}
	if strings.Contains(err.Error(), "Proxying connections") {
		t.Errorf("error %q prints the tunnel's log", err)
	}
	if h.skyRuns() != 0 {
		t.Error("sky ran although the server did not answer")
	}
}

func TestSwitchToDirectKeepsTeleportWhenStopFails(t *testing.T) {
	h := newMetaHarness(t)
	dir := axondir.New(h.home)
	if err := dir.SetPathway("remote"); err != nil {
		t.Fatal(err)
	}
	// A file where the state directory belongs makes the stop fail.
	state := filepath.Join(h.home, ".axon", "state")
	if err := os.RemoveAll(state); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Runs before the harness's own cleanup, which needs the directory.
	t.Cleanup(func() { os.Remove(state) })
	err := h.run("campus")
	if err == nil || !strings.HasPrefix(err.Error(), "disconnect the previous pathway: ") ||
		!strings.HasSuffix(err.Error(), "Run `axon login --pathway campus` again.") {
		t.Fatalf("err = %v, want the failed disconnect", err)
	}
	if p, _, _ := dir.Pathway(); p != "remote" {
		t.Errorf("stored pathway = %q, want remote so the next login disconnects it", p)
	}
}

func TestSkyLoginKeepsCallerDeadline(t *testing.T) {
	h := newMetaHarness(t)
	h.script("hang")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := h.l.skyLogin(ctx, Options{Out: io.Discard, Err: io.Discard}, h.client.sky, h.endpoint())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the caller's deadline", err)
	}
}

func TestTeleportSyncsClientBeforeSkyLogin(t *testing.T) {
	h := newMetaHarness(t)
	h.serverVersion = "0.14.0"
	newDir := t.TempDir()
	newSky := filepath.Join(newDir, "sky")
	if err := os.WriteFile(newSky, []byte(fakeSky), 0o755); err != nil {
		t.Fatal(err)
	}
	h.client.paths = map[string]string{"0.14.0": newSky}

	if err := h.run("remote"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if want := []string{"0.13.0->0.14.0"}; strings.Join(h.client.moves, ",") != strings.Join(want, ",") {
		t.Errorf("client moves = %q, want %q", h.client.moves, want)
	}
	if n := h.skyRuns(); n != 0 {
		t.Errorf("the 0.13.0 client ran %d times; sky api login runs only after the sync", n)
	}
	args, err := os.ReadFile(filepath.Join(newDir, "args"))
	if err != nil {
		t.Fatalf("the 0.14.0 client did not run: %v", err)
	}
	if want := "api login -e " + h.endpoint() + "\n"; string(args) != want {
		t.Errorf("0.14.0 sky args = %q, want %q", args, want)
	}
}

// lastFileGone runs a teleport login, then has gone make the stored config file disappear, and
// checks that the next login disconnects by the records, stopping the
// tunnel, before it asks for --config.
func lastFileGone(t *testing.T, gone func(h *metaHarness)) {
	h := newMetaHarness(t)
	if err := h.run("remote"); err != nil {
		t.Fatalf("teleport login: %v", err)
	}
	st := h.tunnelStatus()
	if !st.Running {
		t.Fatal("no tunnel after the teleport login")
	}
	if h.email() == "" {
		t.Fatal("no email recorded after the teleport login")
	}
	gone(h)
	h.out.Reset()

	if err := h.run(""); !errors.Is(err, errNoConfig) {
		t.Fatalf("err = %v, want errNoConfig", err)
	}
	if !strings.Contains(h.out.String(), "sky: stopping the Teleport tunnel") {
		t.Errorf("output %q does not say the tunnel stopped", h.out.String())
	}
	waitUntil(t, func() bool { return syscall.Kill(st.PID, 0) != nil })
	if e := h.email(); e != "" {
		t.Errorf("email %q stayed after the pathway was left", e)
	}
	if _, ok, _ := connected.Read(axondir.New(h.home)); ok {
		t.Error("the pathway is still recorded as connected")
	}

	// A second login has nothing left to disconnect.
	h.out.Reset()
	if err := h.run(""); !errors.Is(err, errNoConfig) {
		t.Fatalf("second login: err = %v, want errNoConfig", err)
	}
	if h.out.Len() != 0 {
		t.Errorf("second login wrote %q", h.out.String())
	}
}

func TestLastFileDeletedStopsTunnel(t *testing.T) {
	lastFileGone(t, func(h *metaHarness) {
		if err := os.Remove(filepath.Join(h.home, ".axon", "configs", "example.toml")); err != nil {
			t.Fatal(err)
		}
	})
}

func TestLastFileFailingChecksStopsTunnel(t *testing.T) {
	lastFileGone(t, func(h *metaHarness) {
		if err := os.WriteFile(filepath.Join(h.home, ".axon", "configs", "example.toml"), []byte("not toml ["), 0o600); err != nil {
			t.Fatal(err)
		}
	})
}

func TestLastFileDeletedKeepsRecordsWhenStopFails(t *testing.T) {
	h := newMetaHarness(t)
	h.noFiles()
	// A file where the state directory belongs makes the disconnect fail.
	state := filepath.Join(h.home, ".axon", "state")
	if err := os.RemoveAll(state); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(state) })
	err := h.run("")
	if err == nil || !strings.HasPrefix(err.Error(), "disconnect the previous pathway: ") ||
		!strings.HasSuffix(err.Error(), "Run `axon login` again.") {
		t.Fatalf("err = %v, want the failed disconnect", err)
	}
}

// With SKYPILOT_GLOBAL_CONFIG set, the stored endpoint is read from the
// file it names, and ~/.sky/config.yaml is not read.
func TestTeleportStoredEndpointFollowsSkyPilotGlobalConfig(t *testing.T) {
	for _, tc := range []struct {
		name          string
		named, inHome string // the endpoints in each file
		skyRuns       int
	}{
		{"the named file has the tunnel's", "%s", "https://sky.example.org", 0},
		{"only ~/.sky has the tunnel's", "https://sky.example.org", "%s", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newMetaHarness(t)
			named := filepath.Join(h.home, "conf", "sky.yaml")
			getenv := func(k string) string {
				if k == "SKYPILOT_GLOBAL_CONFIG" {
					return named
				}
				return ""
			}
			inHome := h.l.teleport.skyConfig
			h.l.teleport.skyConfig = newLogin(h.home, getenv).teleport.skyConfig
			for path, endpoint := range map[string]string{named: tc.named, inHome: tc.inHome} {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				config := "api_server:\n  endpoint: " + strings.ReplaceAll(endpoint, "%s", h.endpoint()) + "\n"
				if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := h.run("remote"); err != nil {
				t.Fatalf("login: %v\n%s", err, h.out.String())
			}
			if n := h.skyRuns(); n != tc.skyRuns {
				t.Errorf("sky ran %d times, want %d", n, tc.skyRuns)
			}
		})
	}
}
