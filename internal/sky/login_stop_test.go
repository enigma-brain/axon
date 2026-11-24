package sky

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/enigma-brain/axon/internal/fail"
	"github.com/enigma-brain/axon/internal/interrupt"
)

// loginHelperVar names the step a helper process runs: "browser" for the
// browser login of a direct pathway, "tunnel" for sky api login against a
// teleport pathway's tunnel. The fake client is the one loginFakeVar names.
const (
	loginHelperVar = "AXON_TEST_LOGIN_HELPER"
	loginFakeVar   = "AXON_TEST_LOGIN_FAKE"
)

// TestLoginHelperProcess runs one sky api login step as axon login does,
// with its interrupts, and ends as axon does.
func TestLoginHelperProcess(t *testing.T) {
	step := os.Getenv(loginHelperVar)
	if step == "" {
		t.Skip("run only as a helper process")
	}
	ctx, stop := interrupt.Notify(context.Background())
	defer stop()
	c := Connector{LoginWait: time.Minute, Environ: os.Environ}
	opts := Options{Out: os.Stdout, Err: os.Stderr}
	var err error
	switch step {
	case "browser":
		_, err = c.browserLogin(ctx, opts, os.Getenv(loginFakeVar), "https://sky.example.org")
	case "tunnel":
		err = c.skyLogin(ctx, opts, os.Getenv(loginFakeVar), "http://127.0.0.1:46581")
	}
	if sig, ok := interrupt.Signal(ctx); ok && err != nil {
		interrupt.Report(os.Stderr)
		interrupt.Exit(sig)
	}
	if err != nil {
		fail.Exit(err)
	}
	os.Exit(0)
}

// writeFallbackSky writes a client that, like SkyPilot's, reads its first
// SIGINT as a request to carry on another way: it records the fallback and
// keeps waiting. It records its process ID, prints ready, and waits about
// a minute.
func writeFallbackSky(t *testing.T) (sky, dir string) {
	t.Helper()
	dir = t.TempDir()
	sky = filepath.Join(dir, "sky")
	script := fmt.Sprintf(`#!/bin/sh
echo $$ > '%[1]s/pid'
trap 'echo fallback >> "%[1]s/fallback"' INT
echo ready
i=0
while [ $i -lt 600 ]; do sleep 0.1; i=$((i+1)); done
`, dir)
	if err := os.WriteFile(sky, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return sky, dir
}

// interruptLogin runs step in a helper process in a process group of its
// own, against the fallback client, and once the client is ready sends sig
// to what a terminal would: for SIGINT the helper's whole group, for SIGTERM
// the helper alone. It returns how the helper ended, its standard error, how
// long it took to end, and the client's directory.
func interruptLogin(t *testing.T, step string, sig syscall.Signal) (error, string, time.Duration, string) {
	t.Helper()
	sky, dir := writeFallbackSky(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestLoginHelperProcess$")
	cmd.Env = append(os.Environ(), loginHelperVar+"="+step, loginFakeVar+"="+sky)
	// The helper stands for axon started from a terminal, whose group is the
	// one the terminal signals; the test itself is never in it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	r := bufio.NewReader(out)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("the helper ended before the client was ready: %v\n%s", err, errOut.String())
		}
		if line == "ready\n" {
			break
		}
	}
	target := cmd.Process.Pid
	if sig == syscall.SIGINT {
		target = -target
	}
	start := time.Now()
	if err := syscall.Kill(target, sig); err != nil {
		t.Fatal(err)
	}
	go r.WriteTo(&bytes.Buffer{})
	waitErr := cmd.Wait()
	return waitErr, errOut.String(), time.Since(start), dir
}

// endedBy reports whether err is the exit of a process ended by sig.
func endedBy(err error, sig syscall.Signal) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	ws, ok := exit.Sys().(syscall.WaitStatus)
	return ok && ws.Signaled() && ws.Signal() == sig
}

// assertClientGone fails the test unless the client whose process ID is
// recorded in dir has ended and never fell back to another way.
func assertClientGone(t *testing.T, dir string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	// The client's parent has exited, so the client, once ended, is reaped
	// by the system shortly after.
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("the client, process %d, is still running", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(dir, "fallback")); err == nil {
		t.Error("the client received the interrupt and fell back to another way to log in")
	}
}

func TestCtrlCEndsTheLoginAtOnce(t *testing.T) {
	t.Parallel()
	for _, step := range []string{"browser", "tunnel"} {
		t.Run(step, func(t *testing.T) {
			t.Parallel()
			err, errOut, took, dir := interruptLogin(t, step, syscall.SIGINT)
			if !endedBy(err, syscall.SIGINT) {
				t.Errorf("axon ended with %v, want ended by SIGINT\n%s", err, errOut)
			}
			if !strings.Contains(errOut, interrupt.Message+"\n") {
				t.Errorf("axon printed %q, want %q", errOut, interrupt.Message)
			}
			if took >= WaitDelay {
				t.Errorf("axon took %s to end, want less than %s", took, WaitDelay)
			}
			assertClientGone(t, dir)
		})
	}
}

func TestSIGTERMEndsTheLoginAndTheClient(t *testing.T) {
	t.Parallel()
	for _, step := range []string{"browser", "tunnel"} {
		t.Run(step, func(t *testing.T) {
			t.Parallel()
			err, errOut, took, dir := interruptLogin(t, step, syscall.SIGTERM)
			if !endedBy(err, syscall.SIGTERM) {
				t.Errorf("axon ended with %v, want ended by SIGTERM\n%s", err, errOut)
			}
			if took >= WaitDelay {
				t.Errorf("axon took %s to end, want less than %s", took, WaitDelay)
			}
			assertClientGone(t, dir)
		})
	}
}

// writeLoginSky writes a client that writes body as is and exits with
// code.
func writeLoginSky(t *testing.T, body string, code int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sky")
	script := fmt.Sprintf("#!/bin/sh\n%s\nexit %d\n", body, code)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestUninterruptedLoginOutput checks that each step passes on the client's
// output as the client writes it, and that the client reads no input.
func TestUninterruptedLoginOutput(t *testing.T) {
	t.Parallel()
	body := `if read line; then echo "read $line"; else echo "no input"; fi
printf 'Logged in\033[32m green\033[0m\n'
echo 'to the error output' >&2`
	sky := writeLoginSky(t, body, 0)
	c := Connector{LoginWait: time.Minute, Environ: os.Environ}
	for _, step := range []string{"browser", "tunnel"} {
		var out, errOut bytes.Buffer
		opts := Options{Out: &out, Err: &errOut}
		var err error
		var intro string
		switch step {
		case "browser":
			_, err = c.browserLogin(context.Background(), opts, sky, "https://sky.example.org")
			intro = "sky: logging in to https://sky.example.org in your browser\n"
		case "tunnel":
			err = c.skyLogin(context.Background(), opts, sky, "http://127.0.0.1:46581")
			intro = "sky: pointing the SkyPilot client at http://127.0.0.1:46581\n"
		}
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		if want := intro + "no input\nLogged in\x1b[32m green\x1b[0m\n"; out.String() != want {
			t.Errorf("%s wrote %q, want %q", step, out.String(), want)
		}
		if errOut.String() != "to the error output\n" {
			t.Errorf("%s wrote %q to the error output", step, errOut.String())
		}
	}
}

// TestTunnelStepWritesThroughAPipe checks that the client never writes to
// the file axon writes to, which can be the terminal, but to a pipe.
func TestTunnelStepWritesThroughAPipe(t *testing.T) {
	t.Parallel()
	sky := writeLoginSky(t, `if [ -p /dev/stdout ] && [ -p /dev/stderr ]; then echo pipes; else echo files; fi`, 0)
	f, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c := Connector{LoginWait: time.Minute, Environ: os.Environ}
	if err := c.skyLogin(context.Background(), Options{Out: f, Err: f}, sky, "http://127.0.0.1:46581"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "pipes\n") {
		t.Fatalf("the client wrote %q, want it to have written to pipes", data)
	}
}

// TestPastePromptWithoutInput gives the reason when the client, having no
// way through the browser that worked, asked for a token it could not read.
func TestPastePromptWithoutInput(t *testing.T) {
	t.Parallel()
	sky := writeLoginSky(t, `printf 'Paste the token: '; read token; echo; echo Cancelled.`, 1)
	c := Connector{LoginWait: time.Minute, Environ: os.Environ}
	var out bytes.Buffer
	_, err := c.browserLogin(context.Background(), Options{Out: &out, Err: &out}, sky, "https://sky.example.org")
	if err == nil || !strings.Contains(err.Error(), "The login could not be completed in a browser. Run `axon login` again.") {
		t.Fatalf("the login returned %v, want the reason", err)
	}
}
