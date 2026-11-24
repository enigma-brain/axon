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
	"strconv"
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
	case "stubborn":
		childStubborn()
	case "stubborn-child":
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(time.Minute)
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

// childStubborn ignores SIGTERM and starts a child that ignores it too,
// prints the child's process ID and ready, and waits.
func childStubborn() {
	signal.Ignore(syscall.SIGTERM)
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), childEnv+"=stubborn-child")
	if err := cmd.Start(); err != nil {
		os.Exit(5)
	}
	fmt.Println(cmd.Process.Pid)
	fmt.Println("ready")
	time.Sleep(time.Minute)
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
	t.Parallel()
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
	t.Parallel()
	cmd, out := child(t, context.Background(), "notify", nil)
	cmd.Process.Signal(syscall.SIGTERM)
	if line, _ := out.ReadString('\n'); line != "terminated true\n" {
		t.Fatalf("after SIGTERM the child printed %q", line)
	}
}

func TestSignalOfOtherCancellations(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	err := fmt.Errorf("download: %w", &Error{Signal: syscall.SIGINT})
	if !errors.Is(err, context.Canceled) {
		t.Error("an interrupt is not a cancellation")
	}
	if got := (&Error{Signal: syscall.SIGTERM}).Error(); got != "interrupted by SIGTERM" {
		t.Errorf("Error() = %q", got)
	}
}

func TestReport(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	Report(&buf)
	if got := buf.String(); got != Message+"\n" {
		t.Fatalf("Report wrote %q, want one line", got)
	}
}

// stopped runs the trapping child under stop with grace, cancels its
// context with cause once it is ready, and returns what Wait returned, what
// the child printed after it was ready, and how long Wait took after the
// cancellation.
func stopped(t *testing.T, stop func(context.Context, *exec.Cmd, time.Duration), cause error, grace time.Duration, deliver func(*exec.Cmd)) (error, string, time.Duration) {
	t.Helper()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	cmd, out := child(t, ctx, "trap", func(cmd *exec.Cmd) { stop(ctx, cmd, grace) })
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
	// Everything is read before Wait, which closes the pipe.
	printed := <-rest
	err := cmd.Wait()
	return err, printed, time.Since(start)
}

func TestStopForwardsSIGTERM(t *testing.T) {
	t.Parallel()
	_, out, _ := stopped(t, Stop, &Error{Signal: syscall.SIGTERM}, 5*time.Second, nil)
	if !strings.Contains(out, "got terminated") {
		t.Fatalf("the program printed %q, want it to have received SIGTERM", out)
	}
}

func TestStopSendsNothingOnSIGINT(t *testing.T) {
	t.Parallel()
	// The terminal delivers SIGINT to the program; here the test does.
	deliver := func(cmd *exec.Cmd) { cmd.Process.Signal(syscall.SIGINT) }
	err, out, _ := stopped(t, Stop, &Error{Signal: syscall.SIGINT}, 5*time.Second, deliver)
	if !strings.Contains(out, "got interrupt") {
		t.Fatalf("the program printed %q, want it to have received the one SIGINT", out)
	}
	if signaledBy(err, syscall.SIGKILL) {
		t.Fatal("the program was killed, want it to end on its own")
	}
}

func TestStopKillsAfterGrace(t *testing.T) {
	t.Parallel()
	grace := 300 * time.Millisecond
	err, out, took := stopped(t, Stop, &Error{Signal: syscall.SIGINT}, grace, nil)
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
	t.Parallel()
	err, out, took := stopped(t, Stop, context.DeadlineExceeded, time.Minute, nil)
	if strings.Contains(out, "got") || !signaledBy(err, syscall.SIGKILL) {
		t.Fatalf("Wait returned %v and the program printed %q, want it killed", err, out)
	}
	if took > 10*time.Second {
		t.Fatalf("the program was killed after %s, want at once", took)
	}
}

func TestStopGroupStartsAGroupOfItsOwn(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd, _ := child(t, ctx, "trap", func(cmd *exec.Cmd) { StopGroup(ctx, cmd, time.Second) })
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if pgid != cmd.Process.Pid {
		t.Fatalf("the program is in the process group %d, want its own, %d", pgid, cmd.Process.Pid)
	}
}

func TestStopGroupSendsSIGTERMOnEitherSignal(t *testing.T) {
	t.Parallel()
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		_, out, took := stopped(t, StopGroup, &Error{Signal: sig}, time.Minute, nil)
		if !strings.Contains(out, "got terminated") {
			t.Errorf("interrupted by %v, the program printed %q, want it to have received SIGTERM", sig, out)
		}
		if took > 10*time.Second {
			t.Errorf("interrupted by %v, the program ended after %s, want at once", sig, took)
		}
	}
}

func TestStopGroupKillsOnOtherCancellations(t *testing.T) {
	t.Parallel()
	err, out, took := stopped(t, StopGroup, context.DeadlineExceeded, time.Minute, nil)
	if strings.Contains(out, "got") || !signaledBy(err, syscall.SIGKILL) {
		t.Fatalf("Wait returned %v and the program printed %q, want it killed", err, out)
	}
	if took > 10*time.Second {
		t.Fatalf("the program was killed after %s, want at once", took)
	}
}

func TestStopGroupKillsTheGroupAfterGrace(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	grace := 300 * time.Millisecond
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), childEnv+"=stubborn")
	StopGroup(ctx, cmd, grace)
	r, w := io.Pipe()
	cmd.Stdout = w
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	lines := bufio.NewReader(r)
	first, _ := lines.ReadString('\n')
	grandchild, err := strconv.Atoi(strings.TrimSpace(first))
	if err != nil {
		t.Fatalf("the program printed %q, want its child's process ID", first)
	}
	if line, _ := lines.ReadString('\n'); line != "ready\n" {
		t.Fatalf("the program printed %q, want ready", line)
	}
	go io.Copy(&out, lines)
	cancel(&Error{Signal: syscall.SIGINT})
	cmd.Wait()
	w.Close()
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(grandchild, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("the program's child, process %d, ignored SIGTERM and still runs", grandchild)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
