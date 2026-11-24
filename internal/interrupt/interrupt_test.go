package interrupt

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

// childEnv names the part a test binary plays when it is started by one of
// these tests, which signal only the processes they start.
const childEnv = "AXON_INTERRUPT_CHILD"

// TestMain runs the part childEnv names, when it names one.
func TestMain(m *testing.M) {
	switch os.Getenv(childEnv) {
	case "":
		os.Exit(m.Run())
	case "notify":
		childNotify()
	case "exit":
		Exit(syscall.SIGTERM)
	case "trap":
		childTrap()
	}
	os.Exit(3)
}

// childNotify waits for the context Notify returns to end, prints the
// signal that ended it, and then waits for a second signal.
func childNotify() {
	ctx, _ := Notify(context.Background())
	fmt.Println("ready")
	<-ctx.Done()
	sig, ok := Signal(ctx)
	fmt.Println(sig, ok)
	time.Sleep(10 * time.Second)
	os.Exit(4)
}

// childTrap prints the first SIGINT or SIGTERM it gets and exits 0.
func childTrap() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	fmt.Println("ready")
	fmt.Println("got", <-ch)
	os.Exit(0)
}

// child starts the test binary as part, with its standard output on a pipe,
// and waits for it to print "ready".
func child(t *testing.T, ctx context.Context, part string, prepare func(*exec.Cmd)) (*exec.Cmd, *bufio.Reader) {
	t.Helper()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), childEnv+"="+part)
	if prepare != nil {
		prepare(cmd)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill() })
	r := bufio.NewReader(out)
	if line, err := r.ReadString('\n'); line != "ready\n" {
		t.Fatalf("the child printed %q (%v), want ready", line, err)
	}
	return cmd, r
}

// signaledBy reports whether err is the exit of a process ended by sig.
func signaledBy(err error, sig syscall.Signal) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	ws, ok := exit.Sys().(syscall.WaitStatus)
	return ok && ws.Signaled() && ws.Signal() == sig
}

func TestNotifyCancelsAndASecondSignalEnds(t *testing.T) {
	cmd, out := child(t, context.Background(), "notify", nil)

	cmd.Process.Signal(syscall.SIGINT)
	if line, _ := out.ReadString('\n'); line != "interrupt true\n" {
		t.Fatalf("after SIGINT the child printed %q, want the signal that ended its context", line)
	}
	start := time.Now()
	cmd.Process.Signal(syscall.SIGINT)
	io.Copy(io.Discard, out)
	err := cmd.Wait()
	if !signaledBy(err, syscall.SIGINT) {
		t.Fatalf("after a second SIGINT the child ended with %v, want ended by SIGINT", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("the child took %s to end after the second SIGINT", time.Since(start))
	}
}

func TestNotifySIGTERM(t *testing.T) {
	cmd, out := child(t, context.Background(), "notify", nil)
	cmd.Process.Signal(syscall.SIGTERM)
	if line, _ := out.ReadString('\n'); line != "terminated true\n" {
		t.Fatalf("after SIGTERM the child printed %q", line)
	}
}

func TestSignalOfOtherCancellations(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := Signal(ctx); ok {
		t.Fatal("Signal reports a signal for a plain cancellation")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 0)
	defer cancel()
	if _, ok := Signal(ctx); ok {
		t.Fatal("Signal reports a signal for a deadline")
	}
	parent, cancelCause := context.WithCancelCause(context.Background())
	child, cancelChild := context.WithTimeout(parent, time.Hour)
	defer cancelChild()
	cancelCause(&Error{Signal: syscall.SIGTERM})
	if sig, ok := Signal(child); !ok || sig != syscall.SIGTERM {
		t.Fatalf("Signal of a context whose parent a signal cancelled = %v, %v", sig, ok)
	}
}

func TestExitEndsBySignal(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), childEnv+"=exit")
	if err := cmd.Run(); !signaledBy(err, syscall.SIGTERM) {
		t.Fatalf("Exit(SIGTERM) ended the process with %v, want ended by SIGTERM", err)
	}
}

func TestErrorIsACancellation(t *testing.T) {
	err := fmt.Errorf("download: %w", &Error{Signal: syscall.SIGINT})
	if !errors.Is(err, context.Canceled) {
		t.Error("an interrupt is not a cancellation")
	}
	if got := (&Error{Signal: syscall.SIGTERM}).Error(); got != "interrupted by SIGTERM" {
		t.Errorf("Error() = %q", got)
	}
}

func TestReport(t *testing.T) {
	var buf bytes.Buffer
	Report(&buf)
	if got := buf.String(); got != Message+"\n" {
		t.Fatalf("Report wrote %q, want one line", got)
	}
}

// stopped runs the trapping child under Stop with grace, cancels its
// context with cause once it is ready, and returns what Wait returned, what
// the child printed after it was ready, and how long Wait took after the
// cancellation.
func stopped(t *testing.T, cause error, grace time.Duration, deliver func(*exec.Cmd)) (error, string, time.Duration) {
	t.Helper()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	cmd, out := child(t, ctx, "trap", func(cmd *exec.Cmd) { Stop(ctx, cmd, grace) })
	rest := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(out)
		rest <- string(b)
	}()
	start := time.Now()
	cancel(cause)
	if deliver != nil {
		deliver(cmd)
	}
	err := cmd.Wait()
	return err, <-rest, time.Since(start)
}

func TestStopForwardsSIGTERM(t *testing.T) {
	_, out, _ := stopped(t, &Error{Signal: syscall.SIGTERM}, 5*time.Second, nil)
	if !strings.Contains(out, "got terminated") {
		t.Fatalf("the program printed %q, want it to have received SIGTERM", out)
	}
}

func TestStopSendsNothingOnSIGINT(t *testing.T) {
	// The terminal delivers SIGINT to the program; here the test does.
	deliver := func(cmd *exec.Cmd) { cmd.Process.Signal(syscall.SIGINT) }
	err, out, _ := stopped(t, &Error{Signal: syscall.SIGINT}, 5*time.Second, deliver)
	if !strings.Contains(out, "got interrupt") {
		t.Fatalf("the program printed %q, want it to have received the one SIGINT", out)
	}
	if signaledBy(err, syscall.SIGKILL) {
		t.Fatal("the program was killed, want it to end on its own")
	}
}

func TestStopKillsAfterGrace(t *testing.T) {
	grace := 300 * time.Millisecond
	err, out, took := stopped(t, &Error{Signal: syscall.SIGINT}, grace, nil)
	if strings.Contains(out, "got") {
		t.Fatalf("the program printed %q, want no signal sent to it on SIGINT", out)
	}
	if !signaledBy(err, syscall.SIGKILL) {
		t.Fatalf("Wait returned %v, want the program killed after the grace period", err)
	}
	if took < grace {
		t.Fatalf("the program was killed after %s, before the grace period of %s", took, grace)
	}
}

func TestStopKillsOnOtherCancellations(t *testing.T) {
	err, out, took := stopped(t, context.DeadlineExceeded, time.Minute, nil)
	if strings.Contains(out, "got") || !signaledBy(err, syscall.SIGKILL) {
		t.Fatalf("Wait returned %v and the program printed %q, want it killed", err, out)
	}
	if took > 10*time.Second {
		t.Fatalf("the program was killed after %s, want at once", took)
	}
}
