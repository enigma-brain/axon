package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// writeOldRecord writes pid in the old form of record, tunnel.pid.
func writeOldRecord(t *testing.T, tn *Tunnel, pid int) string {
	t.Helper()
	if err := os.MkdirAll(tn.stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tn.stateDir, oldRecordFile)
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func gone(path string) bool {
	_, err := os.Stat(path)
	return errors.Is(err, os.ErrNotExist)
}

// startSleep starts a process that is not a tunnel.
func startSleep(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go cmd.Wait()
	t.Cleanup(func() { cmd.Process.Kill() })
	return cmd.Process.Pid
}

func TestRecordHoldsTheCommandLine(t *testing.T) {
	tn := newTestTunnel(t)
	t.Setenv("FAKETSH_MODE", "serve")
	if err := tn.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(recordPath(tn.stateDir))
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		PID  int      `json:"pid"`
		Args []string `json:"args"`
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("tunnel.json is not JSON: %v\n%s", err, data)
	}
	st, _ := tn.Status(context.Background())
	if rec.PID != st.PID {
		t.Errorf("recorded process ID = %d, want %d", rec.PID, st.PID)
	}
	want := "--proxy=teleport.example.org proxy app skypilot --port " + strconv.Itoa(testPort)
	if got := strings.Join(rec.Args, " "); got != want {
		t.Errorf("recorded arguments = %q, want %q", got, want)
	}
}

func TestTunnelToAnotherProxyIsNotAdoptedOrStopped(t *testing.T) {
	tn := newTestTunnel(t)
	pid := startByHandWith(t, "serve", handArgs("other.example.org"))

	st, err := tn.Status(context.Background())
	if err != nil || st.Running {
		t.Errorf("Status = %+v, %v; want no tunnel", st, err)
	}
	if err := tn.Stop(); err != nil {
		t.Fatal(err)
	}
	if !alive(t, pid) {
		t.Fatal("Stop stopped a tunnel to another proxy")
	}
	err = tn.Start(context.Background())
	var busy *PortBusyError
	if !errors.As(err, &busy) || busy.PID != pid {
		t.Fatalf("Start = %v, want a *PortBusyError naming %d", err, pid)
	}
	if !strings.Contains(busy.Command, "--proxy=other.example.org") {
		t.Errorf("PortBusyError.Command = %q, want the other tunnel's command", busy.Command)
	}
	if !alive(t, pid) {
		t.Error("Start stopped a tunnel to another proxy")
	}
	if _, ok := recorded(t, tn); ok {
		t.Error("Start recorded a tunnel to another proxy")
	}
}

func TestOldFormTunnelIsAnotherProcess(t *testing.T) {
	tn := newTestTunnel(t)
	pid := startByHandWith(t, "serve", oldArgs())
	old := writeOldRecord(t, tn, pid)

	st, err := tn.Status(context.Background())
	if err != nil || st.Running {
		t.Errorf("Status = %+v, %v; want no tunnel", st, err)
	}
	if !gone(old) {
		t.Error("the old record was kept")
	}

	writeOldRecord(t, tn, pid)
	err = tn.Start(context.Background())
	var busy *PortBusyError
	if !errors.As(err, &busy) || busy.PID != pid {
		t.Fatalf("Start = %v, want a *PortBusyError naming %d", err, pid)
	}
	if !alive(t, pid) {
		t.Error("Start stopped the old-form tunnel")
	}
	if !gone(old) {
		t.Error("the old record was kept by Start")
	}

	writeOldRecord(t, tn, pid)
	if err := tn.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := StopRecorded(tn.stateDir); err != nil {
		t.Fatal(err)
	}
	if !alive(t, pid) {
		t.Error("a stop signalled the old-form tunnel")
	}
	if !gone(old) {
		t.Error("the old record was kept by Stop")
	}
}

func TestRecordOfAnotherCommandIsDiscarded(t *testing.T) {
	// The recorded ID now belongs to a tunnel whose command line is not the
	// recorded one.
	tn := newTestTunnel(t)
	pid := startByHandWith(t, "serve", handArgs("other.example.org"))
	if err := tn.record(pid); err != nil {
		t.Fatal(err)
	}
	if err := StopRecorded(tn.stateDir); err != nil {
		t.Fatal(err)
	}
	if !alive(t, pid) {
		t.Error("StopRecorded stopped a process whose command line is not the recorded one")
	}
	if _, ok := recorded(t, tn); ok {
		t.Error("the mismatched record was kept by StopRecorded")
	}

	if err := tn.record(pid); err != nil {
		t.Fatal(err)
	}
	if err := tn.Stop(); err != nil {
		t.Fatal(err)
	}
	if !alive(t, pid) {
		t.Error("Stop stopped a process whose command line is not the recorded one")
	}
	if _, ok := recorded(t, tn); ok {
		t.Error("the mismatched record was kept by Stop")
	}
}

func TestRecordedTunnelToAnotherProxyIsNotThisTunnel(t *testing.T) {
	// The record and its process agree, but name another proxy: the
	// tunnel of another pathway.
	tn := newTestTunnel(t)
	args := handArgs("other.example.org")
	pid := startByHandWith(t, "serve", args)
	if err := writeRecord(tn.stateDir, record{PID: pid, Args: args}); err != nil {
		t.Fatal(err)
	}
	err := tn.Start(context.Background())
	var busy *PortBusyError
	if !errors.As(err, &busy) || busy.PID != pid {
		t.Fatalf("Start = %v, want a *PortBusyError naming %d", err, pid)
	}
	if !alive(t, pid) {
		t.Error("Start stopped the other proxy's tunnel")
	}
	// The record still names the other tunnel, which leaving its pathway
	// stops by the record alone.
	if got, ok := recorded(t, tn); !ok || got != pid {
		t.Errorf("record = %d, %v; want the other proxy's tunnel %d kept", got, ok, pid)
	}
	if err := tn.Stop(); err != nil {
		t.Fatal(err)
	}
	if !alive(t, pid) {
		t.Error("Stop stopped the other proxy's tunnel")
	}
	if running, err := RecordedRunning(tn.stateDir); err != nil || !running {
		t.Errorf("RecordedRunning = %v, %v; want true", running, err)
	}
	if err := StopRecorded(tn.stateDir); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !alive(t, pid) })
}

func TestStopRecorded(t *testing.T) {
	t.Run("no record", func(t *testing.T) {
		tn := newTestTunnel(t)
		if err := StopRecorded(tn.stateDir); err != nil {
			t.Errorf("StopRecorded = %v, want nil", err)
		}
	})
	t.Run("recorded tunnel", func(t *testing.T) {
		tn := newTestTunnel(t)
		t.Setenv("FAKETSH_MODE", "serve")
		if err := tn.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		pid, _ := recorded(t, tn)
		if running, err := RecordedRunning(tn.stateDir); err != nil || !running {
			t.Errorf("RecordedRunning = %v, %v; want true", running, err)
		}
		if err := StopRecorded(tn.stateDir); err != nil {
			t.Fatal(err)
		}
		if alive(t, pid) {
			t.Errorf("tunnel %d still runs after StopRecorded", pid)
		}
		if _, ok := recorded(t, tn); ok {
			t.Error("the record remains after StopRecorded")
		}
		if running, err := RecordedRunning(tn.stateDir); err != nil || running {
			t.Errorf("RecordedRunning = %v, %v; want false", running, err)
		}
	})
	t.Run("recorded tunnel to another proxy", func(t *testing.T) {
		// Leaving a pathway stops its tunnel by the record alone, whatever
		// proxy it reached.
		tn := newTestTunnel(t)
		args := handArgs("other.example.org:3080")
		pid := startByHandWith(t, "serve", args)
		if err := writeRecord(tn.stateDir, record{PID: pid, Args: args}); err != nil {
			t.Fatal(err)
		}
		if err := StopRecorded(tn.stateDir); err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool { return !alive(t, pid) })
	})
	t.Run("unrecorded tunnel", func(t *testing.T) {
		tn := newTestTunnel(t)
		pid := startByHand(t, "serve")
		if running, err := RecordedRunning(tn.stateDir); err != nil || running {
			t.Errorf("RecordedRunning = %v, %v; want false", running, err)
		}
		if err := StopRecorded(tn.stateDir); err != nil {
			t.Fatal(err)
		}
		if !alive(t, pid) {
			t.Error("StopRecorded stopped a tunnel it has no record of")
		}
	})
	t.Run("process that is not a tunnel", func(t *testing.T) {
		tn := newTestTunnel(t)
		pid := startSleep(t)
		if err := writeRecord(tn.stateDir, record{PID: pid, Args: tn.args()}); err != nil {
			t.Fatal(err)
		}
		if err := StopRecorded(tn.stateDir); err != nil {
			t.Fatal(err)
		}
		if !alive(t, pid) {
			t.Error("StopRecorded stopped a process that took the recorded ID")
		}
		if _, ok := recorded(t, tn); ok {
			t.Error("the record was kept")
		}
	})
	t.Run("record not of a tunnel's command", func(t *testing.T) {
		// A record that names a command other than a tunnel's is never
		// acted on, even when a process runs it.
		tn := newTestTunnel(t)
		pid := startSleep(t)
		p, _, err := lookupProcess(pid)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeRecord(tn.stateDir, record{PID: pid, Args: slices.Clone(p.Args[1:])}); err != nil {
			t.Fatal(err)
		}
		if err := StopRecorded(tn.stateDir); err != nil {
			t.Fatal(err)
		}
		if !alive(t, pid) {
			t.Error("StopRecorded stopped a process that is not a tunnel")
		}
	})
	t.Run("unreadable record", func(t *testing.T) {
		tn := newTestTunnel(t)
		os.MkdirAll(tn.stateDir, 0o700)
		if err := os.WriteFile(recordPath(tn.stateDir), []byte("12345\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := StopRecorded(tn.stateDir); err != nil {
			t.Fatal(err)
		}
		if _, ok := recorded(t, tn); ok {
			t.Error("a record that cannot be read was kept")
		}
	})
}

func TestStartRefusesBadTarget(t *testing.T) {
	for _, c := range []struct{ proxy, app string }{
		{"", testApp}, {"-x", testApp}, {"a.example.org/x", testApp}, {"a b", testApp},
		{"teleport.example.org:65536", testApp}, {"user@teleport.example.org", testApp},
		{testProxy, ""}, {testProxy, "--port=1"}, {testProxy, "dev env"},
	} {
		tn := newTestTunnel(t)
		tn.proxy, tn.app = c.proxy, c.app
		if err := tn.Start(context.Background()); err == nil {
			t.Errorf("Start with proxy %q and application %q succeeded", c.proxy, c.app)
		}
		if _, ok := recorded(t, tn); ok {
			t.Errorf("Start with proxy %q and application %q left a record", c.proxy, c.app)
		}
	}
}
