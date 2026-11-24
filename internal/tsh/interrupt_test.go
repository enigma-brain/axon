package tsh

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/enigma-brain/axon/internal/interrupt"
)

// slowTSH is a tsh that writes its process ID to $FAKETSH_MARKS/pid, waits
// about ten seconds, and writes the name of the signal that ends the wait
// to $FAKETSH_MARKS/signal before it exits 1. It runs sleep by its path,
// since the tests' PATH holds nothing else.
const slowTSH = `#!/bin/sh
trap 'echo TERM > "$FAKETSH_MARKS/signal"; exit 1' TERM
trap 'echo INT > "$FAKETSH_MARKS/signal"; exit 1' INT
echo $$ > "$FAKETSH_MARKS/pid"
i=0
while [ $i -lt 200 ]; do /bin/sleep 0.05; i=$((i + 1)); done
`

// putSlow writes slowTSH into the PATH directory and returns its path and
// the directory it marks in.
func (f *fixture) putSlow() (path, marks string) {
	path = filepath.Join(f.bin, "tsh")
	if err := os.WriteFile(path, []byte(slowTSH), 0o755); err != nil {
		f.t.Fatal(err)
	}
	marks = f.t.TempDir()
	f.t.Setenv("FAKETSH_MARKS", marks)
	return path, marks
}

// whenStarted calls fn with the process ID of the slow tsh once it runs.
func whenStarted(t *testing.T, marks string, fn func(pid int)) {
	go func() {
		for {
			data, err := os.ReadFile(filepath.Join(marks, "pid"))
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && convErr == nil {
				fn(pid)
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
}

func mark(marks string) string {
	data, _ := os.ReadFile(filepath.Join(marks, "signal"))
	return strings.TrimSpace(string(data))
}

// A certificate check cancelled while tsh runs reports the cancellation,
// not a missing login.
func TestSessionValidInterrupted(t *testing.T) {
	f := newFixture(t)
	path, marks := f.putSlow()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	whenStarted(t, marks, func(int) { cancel() })

	start := time.Now()
	valid, err := SessionValid(ctx, path, testProxy)
	if valid || !errors.Is(err, context.Canceled) {
		t.Fatalf("SessionValid = %v, %v, want context.Canceled", valid, err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("SessionValid took %s to stop", d)
	}
}

// Reading tsh's version stops when cancelled.
func TestClientVersionCancelled(t *testing.T) {
	f := newFixture(t)
	path, marks := f.putSlow()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	whenStarted(t, marks, func(int) { cancel() })

	start := time.Now()
	if _, err := clientVersion(ctx, path); err == nil {
		t.Fatal("clientVersion succeeded after the cancellation")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("clientVersion took %s to stop", d)
	}
}

// useBuffers replaces the terminal tsh logs in on with buffers.
func useBuffers(t *testing.T) {
	setVar[io.Reader](t, &stdin, bytes.NewReader(nil))
	setVar[io.Writer](t, &stdout, &bytes.Buffer{})
	setVar[io.Writer](t, &stderr, &bytes.Buffer{})
}

// A Teleport login interrupted by SIGTERM passes the signal to tsh and
// returns once tsh has ended.
func TestLoginInterruptedBySIGTERMForwardsIt(t *testing.T) {
	f := newFixture(t)
	useBuffers(t)
	path, marks := f.putSlow()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	whenStarted(t, marks, func(int) { cancel(&interrupt.Error{Signal: syscall.SIGTERM}) })

	start := time.Now()
	if err := Login(ctx, path, testProxy); err == nil {
		t.Fatal("the interrupted login succeeded")
	}
	if got := mark(marks); got != "TERM" {
		t.Errorf("tsh ended on %q, want SIGTERM", got)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("Login took %s to return", d)
	}
}

// A Teleport login interrupted by SIGINT sends tsh nothing, since the
// terminal delivers SIGINT to tsh itself, and waits for tsh to end.
func TestLoginInterruptedBySIGINTWaitsForTsh(t *testing.T) {
	f := newFixture(t)
	useBuffers(t)
	path, marks := f.putSlow()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	whenStarted(t, marks, func(pid int) {
		cancel(&interrupt.Error{Signal: syscall.SIGINT})
		// The terminal's SIGINT reaches tsh a little later; here the test,
		// which started tsh, delivers it.
		time.Sleep(300 * time.Millisecond)
		syscall.Kill(pid, syscall.SIGINT)
	})

	start := time.Now()
	if err := Login(ctx, path, testProxy); err == nil {
		t.Fatal("the interrupted login succeeded")
	}
	if got := mark(marks); got != "INT" {
		t.Errorf("tsh ended on %q, want the one SIGINT the terminal sent", got)
	}
	if d := time.Since(start); d < 300*time.Millisecond || d > 3*time.Second {
		t.Errorf("Login returned after %s, want once tsh ended on SIGINT", d)
	}
}
