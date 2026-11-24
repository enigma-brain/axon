package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/enigma-brain/axon/internal/testkit"
)

// fakeTsh is the path of the fake tsh, built by TestMain.
var fakeTsh string

// The made-up Teleport proxy and application the tests' tunnels reach.
const (
	testProxy = "teleport.example.org"
	testApp   = "skypilot"
)

// testPort is a free port the tests' tunnels listen on, so that a real
// tunnel on port 46581 is never touched.
var testPort int

// Environment variables that turn the test binary into a short-lived axon
// that starts a tunnel and exits.
const (
	helperStateDir = "TUNNEL_TEST_HELPER_STATE_DIR"
	helperTsh      = "TUNNEL_TEST_HELPER_TSH"
	helperPort     = "TUNNEL_TEST_HELPER_PORT"
)

func TestMain(m *testing.M) {
	if dir := os.Getenv(helperStateDir); dir != "" {
		port, _ := strconv.Atoi(os.Getenv(helperPort))
		tn := NewOnPort(dir, os.Getenv(helperTsh), testProxy, testApp, port)
		if err := tn.Start(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(run(m))
}

func run(m *testing.M) int {
	// The tests run only the fake tsh, by its path, and a few shell tools.
	// lsof, which the package runs on macOS, is not on PATH: the package
	// runs lsof by its fixed path.
	f, err := testkit.Fence(testkit.FenceOptions{Tools: []string{"sh", "sleep"}, FakeTsh: true})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer f.Close()
	fakeTsh = f.Tsh
	if testPort, err = testkit.FreePort(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	code := m.Run()
	// Stop any fake tunnel a failed test left behind.
	NewOnPort(f.Dir, fakeTsh, testProxy, testApp, testPort).Stop()
	return code
}

// newTestTunnel returns a tunnel on testPort with its own state directory,
// stopped at the end of the test.
func newTestTunnel(t *testing.T) *Tunnel {
	t.Helper()
	tn := NewOnPort(filepath.Join(t.TempDir(), "state"), fakeTsh, testProxy, testApp, testPort)
	t.Cleanup(func() {
		if err := tn.Stop(); err != nil {
			t.Errorf("Stop at cleanup: %v", err)
		}
	})
	return tn
}

// handArgs returns the tunnel's arguments after the program name, as a user
// types them, to proxy.
func handArgs(proxy string) []string {
	return []string{"--proxy=" + proxy, "proxy", "app", testApp, "--port", strconv.Itoa(testPort)}
}

// startByHand starts the fake tsh the way a user would at a prompt, with a
// bare "tsh" as the first argument, in mode, and with no record. It returns
// once the fake listens.
func startByHand(t *testing.T, mode string) int {
	t.Helper()
	return startByHandWith(t, mode, handArgs(testProxy))
}

// startByHandWith is startByHand with the arguments args.
func startByHandWith(t *testing.T, mode string, args []string) int {
	t.Helper()
	pid := startByHandNoWaitWith(t, mode, args)
	waitFor(t, func() bool {
		ls, err := portListeners(testPort)
		return err == nil && len(ls) > 0
	})
	return pid
}

// startByHandNoWait is startByHand without the wait for a listener.
func startByHandNoWait(t *testing.T, mode string) int {
	t.Helper()
	return startByHandNoWaitWith(t, mode, handArgs(testProxy))
}

// startByHandNoWaitWith is startByHandNoWait with the arguments args.
func startByHandNoWaitWith(t *testing.T, mode string, args []string) int {
	t.Helper()
	cmd := exec.Command(fakeTsh, args...)
	cmd.Args[0] = "tsh"
	cmd.Env = append(os.Environ(), "FAKETSH_MODE="+mode)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := make(chan struct{})
	go func() {
		cmd.Wait()
		close(reaped)
	}()
	// Wait for the process to be reaped, so that a later cleanup never finds
	// it half gone.
	t.Cleanup(func() {
		cmd.Process.Kill()
		<-reaped
	})
	return cmd.Process.Pid
}

// alive reports whether pid is a live process.
func alive(t *testing.T, pid int) bool {
	t.Helper()
	_, ok, err := lookupProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func recorded(t *testing.T, tn *Tunnel) (int, bool) {
	t.Helper()
	rec, ok, err := readRecord(tn.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	return rec.PID, ok
}

// record writes a record of pid with the tunnel's arguments.
func (tn *Tunnel) record(pid int) error {
	return writeRecord(tn.stateDir, record{PID: pid, Args: tn.args()})
}

func TestNew(t *testing.T) {
	for proxy, want := range map[string]string{
		"teleport.example.org":      "--proxy=teleport.example.org proxy app skypilot --port 46581",
		"teleport.example.org:3080": "--proxy=teleport.example.org:3080 proxy app skypilot --port 46581",
	} {
		tn := New("/state", "/bin/tsh", proxy, "skypilot")
		if got := strings.Join(tn.args(), " "); got != want {
			t.Errorf("args = %q, want %q", got, want)
		}
	}
	tn := New("/state", "/bin/tsh", testProxy, testApp)
	if got, want := tn.healthURL(), "http://127.0.0.1:46581/api/health"; got != want {
		t.Errorf("healthURL = %q, want %q", got, want)
	}
	if got, want := recordPath(tn.stateDir), "/state/tunnel.json"; got != want {
		t.Errorf("recordPath = %q, want %q", got, want)
	}
}

func TestStatusNoTunnel(t *testing.T) {
	tn := newTestTunnel(t)
	st, err := tn.Status(context.Background())
	if err != nil || st != (Status{}) {
		t.Errorf("Status = %+v, %v, want zero, nil", st, err)
	}
}

func TestStartOutlivesAxon(t *testing.T) {
	tn := newTestTunnel(t)
	helper := exec.Command(os.Args[0])
	helper.Env = append(os.Environ(),
		helperStateDir+"="+tn.stateDir,
		helperTsh+"="+fakeTsh,
		helperPort+"="+strconv.Itoa(testPort),
		"FAKETSH_MODE=serve",
	)
	if out, err := helper.CombinedOutput(); err != nil {
		t.Fatalf("the helper's Start failed: %v\n%s", err, out)
	}

	st, err := tn.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Running || !st.Healthy {
		t.Fatalf("Status after the starting process exited = %+v, want running and healthy", st)
	}
	if pid, _ := recorded(t, tn); pid != st.PID {
		t.Errorf("recorded process ID = %d, want %d", pid, st.PID)
	}
	// The tunnel leads its own session, so its terminal's hangup does not
	// reach it.
	if pgid, err := syscall.Getpgid(st.PID); err != nil || pgid != st.PID {
		t.Errorf("process group of the tunnel = %d, %v, want %d", pgid, err, st.PID)
	}

	for _, name := range []string{recordFile, logFile} {
		info, err := os.Stat(filepath.Join(tn.stateDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != fileMode {
			t.Errorf("%s mode = %v, want %v", name, mode, fileMode)
		}
	}
	log, err := os.ReadFile(tn.logPath())
	if err != nil || !strings.Contains(string(log), "Proxying connections to skypilot") {
		t.Errorf("log = %q, %v, want the tunnel's output", log, err)
	}
}

func TestStartTruncatesLog(t *testing.T) {
	tn := newTestTunnel(t)
	t.Setenv("FAKETSH_MODE", "serve")
	if err := os.MkdirAll(tn.stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tn.logPath(), []byte("yesterday's run\n"), fileMode); err != nil {
		t.Fatal(err)
	}
	if err := tn.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(tn.logPath())
	if strings.Contains(string(log), "yesterday") {
		t.Errorf("log = %q, want the earlier run removed", log)
	}
}

func TestStartFailsWhenTunnelExits(t *testing.T) {
	tn := newTestTunnel(t)
	t.Setenv("FAKETSH_MODE", "exit")
	err := tn.Start(context.Background())
	if err == nil {
		t.Fatal("Start succeeded, want an error")
	}
	for _, want := range []string{"it exited", "ERROR: app certificate has expired", tn.logPath()} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	if _, ok := recorded(t, tn); ok {
		t.Error("a record remains after a failed start")
	}
	if _, err := os.Stat(tn.logPath()); err != nil {
		t.Errorf("the log is gone after a failed start: %v", err)
	}
}

func TestStartFailsWhenTunnelDoesNotServe(t *testing.T) {
	tn := newTestTunnel(t)
	tn.SetStartTimeout(500 * time.Millisecond)
	t.Setenv("FAKETSH_MODE", "refuse")
	err := tn.Start(context.Background())
	if err == nil {
		t.Fatal("Start succeeded, want an error")
	}
	for _, want := range []string{"did not answer on " + tn.healthURL() + " within 500ms", "Proxying connections"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	if _, ok := recorded(t, tn); ok {
		t.Error("a record remains after a failed start")
	}
	st, err := tn.Status(context.Background())
	if err != nil || st.Running {
		t.Errorf("Status after a failed start = %+v, %v, want no process", st, err)
	}
}

func TestStartErrorSaysWhetherTheTunnelExited(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		exited  bool
		logLine string
	}{
		{"exit", true, "ERROR: app certificate has expired"},
		{"refuse", false, "Proxying connections"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			tn := newTestTunnel(t)
			tn.SetStartTimeout(500 * time.Millisecond)
			t.Setenv("FAKETSH_MODE", tc.mode)
			err := tn.Start(context.Background())
			var se *StartError
			if !errors.As(err, &se) {
				t.Fatalf("Start = %v, want a *StartError", err)
			}
			if se.Exited != tc.exited {
				t.Errorf("Exited = %v, want %v", se.Exited, tc.exited)
			}
			if !strings.Contains(se.Log, tc.logLine) {
				t.Errorf("Log = %q, want it to contain %q", se.Log, tc.logLine)
			}
			if !strings.HasSuffix(err.Error(), ":\n"+se.Log) {
				t.Errorf("error %q does not end with the log", err)
			}
		})
	}
}

func TestStatusRunningButNotServing(t *testing.T) {
	tn := newTestTunnel(t)
	pid := startByHand(t, "refuse")
	st, err := tn.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := (Status{Running: true, Healthy: false, PID: pid}); st != want {
		t.Errorf("Status = %+v, want %+v", st, want)
	}
}

func TestStatusIgnoresOtherServer(t *testing.T) {
	tn := newTestTunnel(t)
	// A server on the port that is not the tunnel answers the health
	// request, and is still no healthy tunnel.
	other := exec.Command(fakeTsh, "proxy", "app", "grafana", "--port", strconv.Itoa(testPort))
	other.Env = append(os.Environ(), "FAKETSH_MODE=serve")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	go other.Wait()
	t.Cleanup(func() { other.Process.Kill() })
	waitFor(t, func() bool { _, ok := tn.healthy(context.Background()); return ok })

	st, err := tn.Status(context.Background())
	if err != nil || st != (Status{}) {
		t.Errorf("Status = %+v, %v, want zero, nil", st, err)
	}
}

func TestStartAdoptsTunnelThatIsUp(t *testing.T) {
	tn := newTestTunnel(t)
	pid := startByHand(t, "serve")
	if err := tn.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := recorded(t, tn); got != pid {
		t.Errorf("recorded process ID = %d, want the hand-started %d", got, pid)
	}
	if !alive(t, pid) {
		t.Error("the hand-started tunnel was stopped")
	}
}

func TestStartLeavesRecordedTunnelThatIsUp(t *testing.T) {
	tn := newTestTunnel(t)
	t.Setenv("FAKETSH_MODE", "serve")
	if err := tn.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, _ := recorded(t, tn)
	if err := tn.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if second, _ := recorded(t, tn); second != first || !alive(t, first) {
		t.Errorf("second Start replaced the tunnel %d with %d", first, second)
	}
}

func TestStartReplacesTunnelThatIsDown(t *testing.T) {
	tn := newTestTunnel(t)
	pid := startByHand(t, "refuse")
	t.Setenv("FAKETSH_MODE", "serve")
	if err := tn.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !alive(t, pid) })
	st, err := tn.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Running || !st.Healthy || st.PID == pid {
		t.Errorf("Status = %+v, want a new healthy tunnel in place of %d", st, pid)
	}
}

func TestRecordedPIDOfAnotherProcess(t *testing.T) {
	tn := newTestTunnel(t)
	other := exec.Command("sleep", "30")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	go other.Wait()
	t.Cleanup(func() { other.Process.Kill() })
	if err := tn.record(other.Process.Pid); err != nil {
		t.Fatal(err)
	}

	st, err := tn.Status(context.Background())
	if err != nil || st.Running {
		t.Errorf("Status = %+v, %v, want not running", st, err)
	}
	if _, ok := recorded(t, tn); ok {
		t.Error("the record of another process was kept")
	}

	if err := tn.record(other.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if err := tn.Stop(); err != nil {
		t.Fatal(err)
	}
	if !alive(t, other.Process.Pid) {
		t.Error("Stop stopped the process that took the recorded ID")
	}
	if _, ok := recorded(t, tn); ok {
		t.Error("the record of another process was kept by Stop")
	}
}

func TestStartReportsOtherProcessOnPort(t *testing.T) {
	tn := newTestTunnel(t)
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(testPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	err = tn.Start(context.Background())
	var busy *PortBusyError
	if !errors.As(err, &busy) {
		t.Fatalf("Start = %v, want a *PortBusyError", err)
	}
	if busy.PID != os.Getpid() {
		t.Errorf("PortBusyError.PID = %d, want this test's %d", busy.PID, os.Getpid())
	}
	if !strings.Contains(err.Error(), "port 46581") || !strings.Contains(err.Error(), strconv.Itoa(os.Getpid())) {
		t.Errorf("error %q does not name the port and the process", err)
	}
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Errorf("the other process's listener no longer accepts: %v", err)
	} else {
		conn.Close()
	}
}

func TestStop(t *testing.T) {
	tn := newTestTunnel(t)
	if err := tn.Stop(); err != nil {
		t.Errorf("Stop with no tunnel = %v, want nil", err)
	}

	t.Setenv("FAKETSH_MODE", "serve")
	if err := tn.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	pid, _ := recorded(t, tn)
	if err := tn.Stop(); err != nil {
		t.Fatal(err)
	}
	if alive(t, pid) {
		t.Errorf("tunnel %d still runs after Stop", pid)
	}
	if _, ok := recorded(t, tn); ok {
		t.Error("the record remains after Stop")
	}
	// Stop returns once the tunnel has closed its listener.
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(testPort)))
	if err != nil {
		t.Errorf("the port is still held after Stop: %v", err)
	} else {
		ln.Close()
	}
}

func TestStopStopsHandStartedTunnel(t *testing.T) {
	tn := newTestTunnel(t)
	pid := startByHand(t, "serve")
	if err := tn.Stop(); err != nil {
		t.Fatal(err)
	}
	if alive(t, pid) {
		t.Errorf("hand-started tunnel %d still runs after Stop", pid)
	}
}

func TestStartWithSpaceInTshPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Jane Doe")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(fakeTsh)
	if err != nil {
		t.Fatal(err)
	}
	tsh := filepath.Join(dir, "tsh")
	if err := os.WriteFile(tsh, data, 0o700); err != nil {
		t.Fatal(err)
	}
	tn := newTestTunnel(t)
	tn.tshPath = tsh
	t.Setenv("FAKETSH_MODE", "serve")
	if err := tn.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, err := tn.Status(context.Background())
	if err != nil || !st.Running || !st.Healthy {
		t.Fatalf("Status = %+v, %v, want running and healthy", st, err)
	}
	if err := tn.Stop(); err != nil {
		t.Fatal(err)
	}
	if alive(t, st.PID) {
		t.Errorf("tunnel %d still runs after Stop", st.PID)
	}
}

func TestTunnelNotListeningWhileOtherServerAnswers(t *testing.T) {
	tn := newTestTunnel(t)
	idle := startByHandNoWait(t, "idle")
	other := exec.Command(fakeTsh, "proxy", "app", "grafana", "--port", strconv.Itoa(testPort))
	other.Env = append(os.Environ(), "FAKETSH_MODE=serve")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	go other.Wait()
	t.Cleanup(func() { other.Process.Kill() })
	waitFor(t, func() bool { _, ok := tn.healthy(context.Background()); return ok })

	st, err := tn.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := (Status{Running: true, Healthy: false, PID: idle}); st != want {
		t.Errorf("Status = %+v, want %+v", st, want)
	}

	err = tn.Start(context.Background())
	var busy *PortBusyError
	if !errors.As(err, &busy) || busy.PID != other.Process.Pid {
		t.Fatalf("Start = %v, want a *PortBusyError naming %d", err, other.Process.Pid)
	}
	if !strings.Contains(busy.Command, "grafana") {
		t.Errorf("PortBusyError.Command = %q, want the other process's command", busy.Command)
	}
	if _, ok := recorded(t, tn); ok {
		t.Error("Start recorded a tunnel that does not listen")
	}
	if !alive(t, other.Process.Pid) {
		t.Error("Start stopped the other process on the port")
	}
}

func TestStartCancelledLeavesTunnelRunning(t *testing.T) {
	tn := newTestTunnel(t)
	pid := startByHand(t, "serve")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := tn.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Start with a cancelled context = %v, want context.Canceled", err)
	}
	if !alive(t, pid) {
		t.Error("Start with a cancelled context stopped the tunnel")
	}
}

func TestHealthDoesNotFollowRedirect(t *testing.T) {
	tn := newTestTunnel(t)
	pid := startByHand(t, "redirect")
	st, err := tn.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := (Status{Running: true, Healthy: false, PID: pid}); st != want {
		t.Errorf("Status = %+v, want %+v", st, want)
	}
}

// TestHealthTiedToAnsweringAddress puts the tunnel and another server on
// the same port at different loopback addresses. The tunnel is up only when
// the health request to 127.0.0.1 reaches the tunnel's address.
func TestHealthTiedToAnsweringAddress(t *testing.T) {
	probe, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 loopback: %v", err)
	}
	probe.Close()

	for _, tc := range []struct{ tunnelHost, otherHost string }{
		{"127.0.0.1", "::1"},
		{"::1", "127.0.0.1"},
	} {
		t.Run("tunnel on "+tc.tunnelHost, func(t *testing.T) {
			tn := newTestTunnel(t)
			t.Setenv("FAKETSH_HOST", tc.tunnelHost)
			pid := startByHand(t, "refuse")

			other := exec.Command(fakeTsh, "proxy", "app", "grafana", "--port", strconv.Itoa(testPort))
			other.Env = append(os.Environ(), "FAKETSH_MODE=serve", "FAKETSH_HOST="+tc.otherHost)
			if err := other.Start(); err != nil {
				t.Fatal(err)
			}
			go other.Wait()
			t.Cleanup(func() { other.Process.Kill() })
			waitFor(t, func() bool {
				ls, err := portListeners(testPort)
				return err == nil && len(ls) == 2
			})

			// The tunnel answers 503 and the other server 200, so whichever
			// listens on 127.0.0.1, the tunnel is not healthy.
			st, err := tn.Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if want := (Status{Running: true, Healthy: false, PID: pid}); st != want {
				t.Errorf("Status = %+v, want %+v", st, want)
			}
		})
	}
}

func TestStopKeepsRecordWhenTunnelDoesNotExit(t *testing.T) {
	tn := newTestTunnel(t)
	t.Setenv("FAKETSH_MODE", "serve")
	if err := tn.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	pid, _ := recorded(t, tn)

	// A tunnel that survives every signal, as one stuck in the kernel does.
	savedSignal, savedStop, savedKill := sendSignal, stopTimeout, killTimeout
	sendSignal = func(int, syscall.Signal) error { return nil }
	stopTimeout, killTimeout = 100*time.Millisecond, 100*time.Millisecond
	defer func() { sendSignal, stopTimeout, killTimeout = savedSignal, savedStop, savedKill }()

	err := tn.Stop()
	if err == nil {
		t.Fatal("Stop succeeded, want an error")
	}
	for _, want := range []string{"process " + strconv.Itoa(pid) + " did not exit", "kill -9 " + strconv.Itoa(pid)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "Teleport tunnel") {
		t.Errorf("error %q names the step, which the caller names", err)
	}
	if got, ok := recorded(t, tn); !ok || got != pid {
		t.Errorf("record = %d, %v, want %d kept", got, ok, pid)
	}
	if !alive(t, pid) {
		t.Error("the tunnel exited although no signal was sent")
	}
}

func TestCheckRecordsTunnelWithNoRecord(t *testing.T) {
	tn := newTestTunnel(t)
	pid := startByHand(t, "serve")
	st, err := tn.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := (Status{Running: true, Healthy: true, PID: pid}); st != want {
		t.Errorf("Check = %+v, want %+v", st, want)
	}
	if got, ok := recorded(t, tn); !ok || got != pid {
		t.Errorf("record = %d, %v, want the hand-started %d", got, ok, pid)
	}
	// The record makes the tunnel stoppable by its record alone.
	if err := StopRecorded(tn.stateDir); err != nil {
		t.Fatal(err)
	}
	if alive(t, pid) {
		t.Errorf("tunnel %d still runs after StopRecorded", pid)
	}
}

func TestCheckRecordsNothingForTunnelThatIsDown(t *testing.T) {
	tn := newTestTunnel(t)
	pid := startByHand(t, "refuse")
	st, err := tn.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := (Status{Running: true, Healthy: false, PID: pid}); st != want {
		t.Errorf("Check = %+v, want %+v", st, want)
	}
	if _, ok := recorded(t, tn); ok {
		t.Error("Check recorded a tunnel that does not serve")
	}
}

func TestCheckReportsOtherProcessOnPort(t *testing.T) {
	tn := newTestTunnel(t)
	// Another server answers the health request on the tunnel's port.
	other := exec.Command(fakeTsh, "proxy", "app", "grafana", "--port", strconv.Itoa(testPort))
	other.Env = append(os.Environ(), "FAKETSH_MODE=serve")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	go other.Wait()
	t.Cleanup(func() { other.Process.Kill() })
	waitFor(t, func() bool { _, ok := tn.healthy(context.Background()); return ok })

	_, err := tn.Check(context.Background())
	var busy *PortBusyError
	if !errors.As(err, &busy) || busy.PID != other.Process.Pid {
		t.Fatalf("Check = %v, want a *PortBusyError naming %d", err, other.Process.Pid)
	}
	if !strings.Contains(err.Error(), "grafana") {
		t.Errorf("error %q does not name the other process's command", err)
	}
	if _, ok := recorded(t, tn); ok {
		t.Error("Check recorded the other process")
	}
	if !alive(t, other.Process.Pid) {
		t.Error("Check stopped the other process on the port")
	}
}

func TestCheckWithNothingOnPort(t *testing.T) {
	tn := newTestTunnel(t)
	st, err := tn.Check(context.Background())
	if err != nil || st != (Status{}) {
		t.Errorf("Check = %+v, %v, want zero, nil", st, err)
	}
}

func TestRunning(t *testing.T) {
	tn := newTestTunnel(t)
	if running, err := tn.Running(); err != nil || running {
		t.Errorf("Running with no tunnel = %v, %v, want false, nil", running, err)
	}
	startByHandNoWait(t, "idle")
	if running, err := tn.Running(); err != nil || !running {
		t.Errorf("Running with a hand-started tunnel = %v, %v, want true, nil", running, err)
	}
}

// TestConcurrentStarts restarts a dead tunnel from two goroutines at once,
// as two axon commands would. Both succeed, and one tunnel runs, recorded.
func TestConcurrentStarts(t *testing.T) {
	tn := newTestTunnel(t)
	dead := startByHand(t, "refuse")
	t.Setenv("FAKETSH_MODE", "serve")

	const n = 2
	errs := make(chan error, n)
	start := make(chan struct{})
	for range n {
		other := *tn
		go func() {
			<-start
			errs <- other.Start(context.Background())
		}()
	}
	close(start)
	for range n {
		if err := <-errs; err != nil {
			t.Errorf("Start = %v, want nil", err)
		}
	}

	st, err := tn.Status(context.Background())
	if err != nil || !st.Running || !st.Healthy || st.PID == dead {
		t.Fatalf("Status = %+v, %v, want a new healthy tunnel", st, err)
	}
	if pid, ok := recorded(t, tn); !ok || pid != st.PID {
		t.Errorf("record = %d, %v, want the running tunnel %d", pid, ok, st.PID)
	}
	procs, err := tn.find(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 1 {
		t.Errorf("tunnel processes = %v, want one", procs)
	}
}

func TestFailedStartKeepsRecordOfAnotherProcess(t *testing.T) {
	dir := t.TempDir()
	if err := writeRecord(dir, record{PID: 12345, Args: handArgs(testProxy)}); err != nil {
		t.Fatal(err)
	}
	if err := removeRecordOf(dir, 54321); err != nil {
		t.Fatal(err)
	}
	if rec, ok, err := readRecord(dir); err != nil || !ok || rec.PID != 12345 {
		t.Errorf("record = %+v, %v, %v, want the record of 12345 kept", rec, ok, err)
	}
	if err := removeRecordOf(dir, 12345); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := readRecord(dir); err != nil || ok {
		t.Errorf("record of 12345 = %v, %v, want removed", ok, err)
	}
}

func TestLockWaitsAndHonorsContext(t *testing.T) {
	dir := t.TempDir()
	unlock, err := lock(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := lock(ctx, dir); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("lock while held = %v, want context.DeadlineExceeded", err)
	}
	unlock()
	again, err := lock(context.Background(), dir)
	if err != nil {
		t.Fatalf("lock after release = %v", err)
	}
	again()
}

func TestPortBusyErrorCleansCommand(t *testing.T) {
	err := &PortBusyError{PID: 7, Command: "evil \x1b[2J‮cmd\n"}
	if got, want := err.Error(), "port 46581 is held by process 7 (evil [2Jcmd), which is not the Teleport tunnel"; got != want {
		t.Errorf("Error = %q, want %q", got, want)
	}
}

func TestAdoptSkipsProcessThatIsGone(t *testing.T) {
	tn := newTestTunnel(t)
	gone := exec.Command("sleep", "0")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	if err := tn.adopt(context.Background(), gone.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if _, ok := recorded(t, tn); ok {
		t.Error("adopt recorded a process that has exited")
	}
}

func TestAdoptKeepsRecordOfTunnelStartedInItsPlace(t *testing.T) {
	tn := newTestTunnel(t)
	seen := startByHandNoWait(t, "idle")
	// Another axon recorded a tunnel after seen was observed.
	other := startByHandNoWait(t, "idle")
	if err := tn.record(other); err != nil {
		t.Fatal(err)
	}
	if err := tn.adopt(context.Background(), seen); err != nil {
		t.Fatal(err)
	}
	if got, ok := recorded(t, tn); !ok || got != other {
		t.Errorf("record = %d, %v, want %d kept", got, ok, other)
	}
}

func TestDiscardRecordKeepsReplacement(t *testing.T) {
	dir := t.TempDir()
	stale := record{PID: 12345, Args: handArgs(testProxy)}
	fresh := record{PID: 54321, Args: handArgs(testProxy)}
	if err := writeRecord(dir, fresh); err != nil {
		t.Fatal(err)
	}
	// A reader without the lock read stale before fresh replaced it.
	if err := discardRecord(context.Background(), dir, stale, false); err != nil {
		t.Fatal(err)
	}
	if rec, ok, err := readRecord(dir); err != nil || !ok || rec.PID != fresh.PID {
		t.Errorf("record = %+v, %v, %v, want the replacement kept", rec, ok, err)
	}
	if err := discardRecord(context.Background(), dir, fresh, false); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := readRecord(dir); err != nil || ok {
		t.Errorf("record after discarding it = %v, %v, want none", ok, err)
	}
}

func TestDiscardRecordStopsWaitingWhenCancelled(t *testing.T) {
	dir := t.TempDir()
	stale := record{PID: 12345, Args: handArgs(testProxy)}
	if err := writeRecord(dir, stale); err != nil {
		t.Fatal(err)
	}
	unlock, err := lock(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := discardRecord(ctx, dir, stale, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("discardRecord while the lock is held = %v, want context.DeadlineExceeded", err)
	}
}

// TestCheckPortFreeProbesWhenOwnListenerMissesLoopback has the tunnel
// listen only on the IPv6 loopback address. Its listener does not take the
// client's address, 127.0.0.1, so that address is probed; with nothing
// there the port counts as free. Another user's listener there, invisible
// to the process table, would answer the probe, which a test run by one
// user cannot set up.
func TestCheckPortFreeProbesWhenOwnListenerMissesLoopback(t *testing.T) {
	probe, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 loopback: %v", err)
	}
	probe.Close()

	tn := newTestTunnel(t)
	t.Setenv("FAKETSH_HOST", "::1")
	pid := startByHand(t, "serve")
	if err := tn.checkPortFree([]int{pid}); err != nil {
		t.Errorf("checkPortFree with nothing on 127.0.0.1 = %v, want nil", err)
	}
}
