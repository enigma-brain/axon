package tunnel

import (
	"net"
	"net/netip"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"

	"github.com/enigma-brain/axon/internal/testkit"
)

func TestIsTunnelCommand(t *testing.T) {
	t.Parallel()
	want := []string{"proxy", "app", "skypilot", "--port", "46581"}
	tests := []struct {
		name string
		args []string
		ok   bool
	}{
		{"bare name", []string{"tsh", "proxy", "app", "skypilot", "--port", "46581"}, true},
		{"absolute path", []string{"/usr/local/bin/tsh", "proxy", "app", "skypilot", "--port", "46581"}, true},
		{"axon's own tsh", []string{"/home/u/.axon/tools/tsh", "proxy", "app", "skypilot", "--port", "46581"}, true},
		{"other port", []string{"tsh", "proxy", "app", "skypilot", "--port", "46580"}, false},
		{"other application", []string{"tsh", "proxy", "app", "grafana", "--port", "46581"}, false},
		{"joined flag", []string{"tsh", "proxy", "app", "skypilot", "--port=46581"}, false},
		{"other order", []string{"tsh", "proxy", "app", "--port", "46581", "skypilot"}, false},
		{"extra argument", []string{"tsh", "proxy", "app", "skypilot", "--port", "46581", "-d"}, false},
		{"other program", []string{"/bin/sh", "proxy", "app", "skypilot", "--port", "46581"}, false},
		{"tsh in a shell", []string{"/bin/sh", "tsh", "proxy", "app", "skypilot", "--port", "46581"}, false},
		{"no arguments", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTunnelCommand(tt.args, want); got != tt.ok {
				t.Errorf("isTunnelCommand(%q) = %v, want %v", tt.args, got, tt.ok)
			}
		})
	}
}

func TestLookupProcess(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })

	// Right after the fork the child has not yet replaced its arguments.
	var p process
	testkit.WaitUntil(t, func() bool {
		q, ok, err := lookupProcess(cmd.Process.Pid)
		p = q
		return err == nil && ok && len(q.Args) == 2
	})
	if p.UID != os.Getuid() {
		t.Errorf("UID = %d, want %d", p.UID, os.Getuid())
	}
	if len(p.Args) != 2 || p.Args[1] != "30" {
		t.Errorf("Args = %q, want sleep 30", p.Args)
	}

	all, err := listProcesses()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(all, func(q process) bool { return q.PID == cmd.Process.Pid }) {
		t.Errorf("listProcesses does not list the sleep process %d", cmd.Process.Pid)
	}

	cmd.Process.Kill()
	cmd.Wait()
	if _, ok, err := lookupProcess(cmd.Process.Pid); ok || err != nil {
		t.Errorf("lookupProcess after exit = %v, %v, want false, nil", ok, err)
	}
}

func TestLookupProcessUnreaped(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Wait() })
	// The child exits at once and stays in the table until it is reaped.
	testkit.WaitUntil(t, func() bool {
		_, ok, err := lookupProcess(cmd.Process.Pid)
		return err == nil && !ok
	})
}

func TestPortListeners(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	got, err := portListeners(port)
	if err != nil {
		t.Fatal(err)
	}
	want := listener{PID: os.Getpid(), Addr: netip.MustParseAddr("127.0.0.1")}
	if !slices.Contains(got, want) {
		t.Errorf("portListeners(%d) = %v, want it to include %v", port, got, want)
	}

	ln.Close()
	got, err = portListeners(port)
	if err != nil || len(got) != 0 {
		t.Errorf("portListeners after close = %v, %v, want none", got, err)
	}
}

func TestListenerAccepts(t *testing.T) {
	t.Parallel()
	v4, v6 := netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("::1")
	tests := []struct {
		listen, remote netip.Addr
		ok             bool
	}{
		{v4, v4, true},
		{v6, v6, true},
		{v4, v6, false},
		{v6, v4, false},
		{netip.IPv4Unspecified(), v4, true},
		{netip.IPv4Unspecified(), v6, false},
		{netip.IPv6Unspecified(), v6, true},
		{netip.IPv6Unspecified(), v4, true},
	}
	for _, tt := range tests {
		if got := (listener{Addr: tt.listen}).accepts(tt.remote); got != tt.ok {
			t.Errorf("listener on %v accepts %v = %v, want %v", tt.listen, tt.remote, got, tt.ok)
		}
	}
}

func TestProcessStart(t *testing.T) {
	t.Parallel()
	first := exec.Command("sleep", "30")
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.Process.Kill(); first.Wait() })
	// Start times have a resolution of one clock tick.
	time.Sleep(100 * time.Millisecond)
	second := exec.Command("sleep", "30")
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { second.Process.Kill(); second.Wait() })

	a, ok1 := processStart(first.Process.Pid)
	again, ok2 := processStart(first.Process.Pid)
	b, ok3 := processStart(second.Process.Pid)
	if !ok1 || !ok2 || !ok3 {
		t.Fatalf("processStart = %v, %v, %v, want all found", ok1, ok2, ok3)
	}
	if a != again {
		t.Errorf("start time of one process changed: %d, then %d", a, again)
	}
	if a == b {
		t.Errorf("two processes started 100ms apart share the start time %d", a)
	}

	// A zombie still has its start time; a reaped process has none.
	first.Process.Kill()
	testkit.WaitUntil(t, func() bool { return processExited(first.Process.Pid) })
	if s, ok := processStart(first.Process.Pid); !ok || s != a {
		t.Errorf("processStart of the zombie = %d, %v, want %d", s, ok, a)
	}
	first.Wait()
	if _, ok := processStart(first.Process.Pid); ok {
		t.Error("processStart found a reaped process")
	}
}
