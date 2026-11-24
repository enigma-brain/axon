package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/interrupt"
	"github.com/enigma-brain/axon/internal/testkit"
)

// lockedBuffer is a buffer that a process writes to while a test reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// interruptAxon starts the built axon with args inside the fence runAxon
// describes, with a standard input where nothing is typed, sends it sig
// once its standard output holds waitFor, and returns its standard error
// and how it ended. The signal goes only to the axon the test started.
func interruptAxon(t *testing.T, bin, home string, sig syscall.Signal, waitFor string, args ...string) (string, error) {
	t.Helper()
	cmd := testkit.Command(t, home, testkit.DeadProxy(), bin, args...)
	// A pipe of the operating system's, so that Wait does not wait for a
	// copy of the input.
	stdin, typed, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer typed.Close()
	cmd.Stdin = stdin
	var out, errOut lockedBuffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stdin.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	deadline := time.After(30 * time.Second)
	for !strings.Contains(out.String(), waitFor) {
		select {
		case err := <-done:
			t.Fatalf("axon ended with %v before it printed %q\nstdout: %s\nstderr: %s", err, waitFor, out.String(), errOut.String())
		case <-deadline:
			cmd.Process.Kill()
			t.Fatalf("axon did not print %q\nstdout: %s", waitFor, out.String())
		case <-time.After(20 * time.Millisecond):
		}
	}
	// Give axon the moment it takes to start reading the answer.
	time.Sleep(100 * time.Millisecond)
	cmd.Process.Signal(sig)
	select {
	case err := <-done:
		return errOut.String(), err
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatalf("axon did not end after %v\nstdout: %s", sig, out.String())
		return "", nil
	}
}

// endedBy reports whether err is the end of a process by sig.
func endedBy(err error, sig syscall.Signal) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	ws, ok := exit.Sys().(syscall.WaitStatus)
	return ok && ws.Signaled() && ws.Signal() == sig
}

// TestInterruptAtAQuestion interrupts axon login where it asks which
// pathway the user is on: axon ends by the signal, prints one line, and
// sets no pathway.
func TestInterruptAtAQuestion(t *testing.T) {
	bin := build(t, "1.2.3")
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			h := home(t)
			config := filepath.Join(t.TempDir(), "two.toml")
			if err := os.WriteFile(config, []byte(twoFile), 0o600); err != nil {
				t.Fatal(err)
			}
			stderr, err := interruptAxon(t, bin, h, sig, "Pathway [1-2]: ", "login", "--config", config)
			if !endedBy(err, sig) {
				t.Errorf("axon ended with %v, want ended by %v", err, sig)
			}
			if stderr != interrupt.Message+"\n" {
				t.Errorf("stderr = %q, want the one line %q", stderr, interrupt.Message)
			}
			if id, ok, err := axondir.New(h).Pathway(); ok || err != nil {
				t.Errorf("the active pathway is %q (%v), want none", id, err)
			}
		})
	}
}

// TestEnd runs end in a child process, since it ends the process.
func TestEnd(t *testing.T) {
	if mode := os.Getenv("AXON_END_CHILD"); mode != "" {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(&interrupt.Error{Signal: syscall.SIGINT})
		var err error
		if mode == "failed" {
			err = errors.New("the command failed")
		}
		end(ctx, err)
		os.Exit(0)
	}
	for _, tc := range []struct {
		mode       string
		wantSignal bool
		wantStderr string
	}{
		{"failed", true, interrupt.Message + "\n"},
		// A command that finished its work before it acted on the signal
		// has printed its result; nothing is printed over it.
		{"succeeded", false, ""},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestEnd$")
			cmd.Env = append(os.Environ(), "AXON_END_CHILD="+tc.mode)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			err := cmd.Run()
			if tc.wantSignal && !endedBy(err, syscall.SIGINT) {
				t.Errorf("the process ended with %v, want ended by SIGINT", err)
			}
			if !tc.wantSignal && err != nil {
				t.Errorf("the process ended with %v, want success", err)
			}
			if got := stderr.String(); got != tc.wantStderr {
				t.Errorf("stderr = %q, want %q", got, tc.wantStderr)
			}
		})
	}
}
