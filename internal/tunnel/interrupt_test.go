package tunnel

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// hangPortEnv turns the test binary into a tunnel that listens on the port
// it names and never answers, so that a health request waits until it is
// cancelled.
const hangPortEnv = "TUNNEL_TEST_HANG_PORT"

func init() {
	port := os.Getenv(hangPortEnv)
	if port == "" {
		return
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		os.Exit(1)
	}
	var held []net.Conn
	for {
		conn, err := ln.Accept()
		if err != nil {
			os.Exit(1)
		}
		held = append(held, conn)
	}
}

// startHanging starts, as a user would, a process that the package takes
// for the tunnel, which listens on testPort and never answers. It returns
// once it listens.
func startHanging(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], handArgs(testProxy)...)
	cmd.Args[0] = "tsh"
	cmd.Env = append(os.Environ(), hangPortEnv+"="+strconv.Itoa(testPort))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := make(chan struct{})
	go func() {
		cmd.Wait()
		close(reaped)
	}()
	t.Cleanup(func() {
		cmd.Process.Kill()
		<-reaped
	})
	waitFor(t, func() bool {
		ls, err := portListeners(testPort)
		return err == nil && len(ls) > 0
	})
	return cmd.Process.Pid
}

// cancelAfter returns a context that is cancelled after d.
func cancelAfter(t *testing.T, d time.Duration) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(d, cancel)
	t.Cleanup(func() {
		timer.Stop()
		cancel()
	})
	return ctx
}

// A Start cancelled while it asks a running tunnel for its health stops,
// and leaves the tunnel running: the failed request says nothing about it.
func TestStartCancelledDuringHealthLeavesTunnelRunning(t *testing.T) {
	tn := newTestTunnel(t)
	pid := startHanging(t)
	err := tn.Start(cancelAfter(t, 300*time.Millisecond))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start = %v, want context.Canceled", err)
	}
	if !alive(t, pid) {
		t.Error("the cancelled Start stopped the tunnel")
	}
	if _, ok := recorded(t, tn); ok {
		t.Error("the cancelled Start recorded a tunnel it never saw serve")
	}
}

// A Check cancelled while it asks a running tunnel for its health stops
// with the cancellation.
func TestCheckCancelledDuringHealth(t *testing.T) {
	tn := newTestTunnel(t)
	pid := startHanging(t)
	if _, err := tn.Check(cancelAfter(t, 300*time.Millisecond)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Check = %v, want context.Canceled", err)
	}
	if !alive(t, pid) {
		t.Error("the cancelled Check stopped the tunnel")
	}
}

// A Start cancelled while it waits for the tunnel it launched to serve
// stops that tunnel and removes its record: no tunnel runs unrecorded.
func TestStartCancelledDuringLaunchLeavesNoTunnel(t *testing.T) {
	tn := newTestTunnel(t)
	t.Setenv("FAKETSH_MODE", "idle")
	err := tn.Start(cancelAfter(t, 500*time.Millisecond))
	var startErr *StartError
	if !errors.As(err, &startErr) || startErr.Exited {
		t.Fatalf("Start = %v, want a *StartError for a tunnel that did not exit", err)
	}
	if _, ok := recorded(t, tn); ok {
		t.Error("a record remains after the cancelled start")
	}
	st, err := tn.Status(context.Background())
	if err != nil || st.Running {
		t.Errorf("Status after the cancelled start = %+v, %v, want no process", st, err)
	}
}

// A lock that waits for another holder stops waiting when cancelled.
func TestLockCancelledWhileHeld(t *testing.T) {
	dir := t.TempDir()
	unlock, err := lock(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	start := time.Now()
	if _, err := lock(cancelAfter(t, 100*time.Millisecond), dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("lock while held = %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("lock took %s to stop after the cancellation", d)
	}
}
