// Package interrupt turns SIGINT and SIGTERM into the cancellation of the
// running command, and ends axon the way a shell expects once the command
// has stopped.
//
// The first signal cancels the context Notify returns, with an *Error that
// names the signal as the cause, and restores the signal's default action,
// so that a second signal ends axon at once, wherever it is.
package interrupt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Error is the cause of a context cancelled by a signal.
type Error struct {
	Signal os.Signal
}

func (e *Error) Error() string {
	if n, ok := e.Signal.(syscall.Signal); ok {
		return "interrupted by " + unix.SignalName(n)
	}
	return fmt.Sprintf("interrupted by %v", e.Signal)
}

// Is reports a cancellation by a signal as a cancellation, so that an error
// that carries the cause, as one from net/http does, still reads as one.
func (e *Error) Is(target error) bool { return target == context.Canceled }

// signals are the signals that interrupt a command.
var signals = []os.Signal{syscall.SIGINT, syscall.SIGTERM}

// Notify returns a context derived from parent that is cancelled when axon
// receives SIGINT or SIGTERM, and a function that stops watching. A signal
// that axon was started with ignored, as a command started in the
// background with nohup is, stays ignored.
func Notify(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	var watched []os.Signal
	for _, s := range signals {
		if !signal.Ignored(s) {
			watched = append(watched, s)
		}
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, watched...)
	go func() {
		select {
		case s := <-ch:
			// The default action again, so that a second signal ends
			// axon while the command cleans up.
			signal.Reset(watched...)
			cancel(&Error{Signal: s})
		case <-ctx.Done():
		}
	}()
	return ctx, func() {
		signal.Stop(ch)
		cancel(nil)
	}
}

// Signal returns the signal that cancelled ctx, or one of its parents.
func Signal(ctx context.Context) (os.Signal, bool) {
	var e *Error
	if errors.As(context.Cause(ctx), &e) {
		return e.Signal, true
	}
	return nil, false
}

// Message is the line axon prints when a signal stopped a command.
const Message = "axon: interrupted"

// Report writes Message to w. On a terminal it starts on a new line, since
// the terminal has echoed ^C where the cursor was.
func Report(w io.Writer) {
	if isTerminal(w) {
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, Message)
}

// Exit ends axon by sig: it restores sig's default action and sends sig to
// axon itself, so that the shell sees a process ended by the signal. When
// the signal does not end the process, it exits with 128 plus the signal's
// number, the status a shell reports for it.
func Exit(sig os.Signal) {
	n, ok := sig.(syscall.Signal)
	if !ok {
		os.Exit(1)
	}
	signal.Reset(sig)
	syscall.Kill(os.Getpid(), n)
	time.Sleep(time.Second)
	os.Exit(128 + int(n))
}

// Stop sets how cmd, started with exec.CommandContext(ctx, ...), is stopped
// when ctx ends, and how long axon waits for it then.
//
//   - Interrupted by SIGINT, axon sends nothing: a program started from a
//     terminal is in axon's process group and has received the terminal's
//     SIGINT itself.
//   - Interrupted by SIGTERM, which reaches axon alone, axon sends the
//     program SIGTERM.
//   - Otherwise, as when a deadline axon set has passed, the program is
//     killed.
//
// When the program has not exited grace after ctx ended, it is killed, and
// its output is no longer waited for.
func Stop(ctx context.Context, cmd *exec.Cmd, grace time.Duration) {
	cmd.WaitDelay = grace
	cmd.Cancel = func() error {
		sig, ok := Signal(ctx)
		switch {
		case !ok:
			return cmd.Process.Kill()
		case sig == syscall.SIGINT:
			return nil
		default:
			return cmd.Process.Signal(sig)
		}
	}
}

// StopGroup starts cmd, made with exec.CommandContext(ctx, ...), in a
// process group of its own, and sets how the group is stopped when ctx
// ends and how long axon waits for cmd then.
//
// In a group of its own the program does not receive the terminal's SIGINT,
// which reaches axon alone. It is for a program that reads an interrupt as
// a request to carry on another way rather than to stop, and that neither
// reads from the terminal nor changes its settings: from a group that is
// not the terminal's, either would stop the program.
//
//   - Interrupted by SIGINT or SIGTERM, axon sends the group SIGTERM.
//   - Otherwise, as when a deadline axon set has passed, the group is
//     killed.
//
// When the program has not exited grace after ctx ended, it is killed, and
// its output is no longer waited for; whatever is left of its group is
// killed then too.
func StopGroup(ctx context.Context, cmd *exec.Cmd, grace time.Duration) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.WaitDelay = grace
	cmd.Cancel = func() error {
		// The group's ID is the program's own process ID.
		group := -cmd.Process.Pid
		sig := syscall.SIGKILL
		if _, ok := Signal(ctx); ok {
			sig = syscall.SIGTERM
			time.AfterFunc(grace, func() { syscall.Kill(group, syscall.SIGKILL) })
		}
		err := syscall.Kill(group, sig)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}

// isTerminal reports whether w is a terminal.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	_, err := unix.IoctlGetTermios(int(f.Fd()), getTermios)
	return err == nil
}
