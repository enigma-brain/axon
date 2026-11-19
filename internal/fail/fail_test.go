package fail

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPrint(t *testing.T) {
	var buf bytes.Buffer
	Print(&buf, errors.New("login failed. Run `axon login`."))
	if got, want := buf.String(), "axon: login failed. Run `axon login`.\n"; got != want {
		t.Fatalf("Print wrote %q, want %q", got, want)
	}
}

// TestExit runs Exit in a child process, since it ends the process.
func TestExit(t *testing.T) {
	if os.Getenv("AXON_FAIL_EXIT_CHILD") == "1" {
		Exit(errors.New("boom"))
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestExit$")
	cmd.Env = append(os.Environ(), "AXON_FAIL_EXIT_CHILD=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != Code {
		t.Fatalf("child exited with %v, want exit code %d", err, Code)
	}
	if got := stderr.String(); !strings.HasPrefix(got, "axon: boom\n") {
		t.Fatalf("stderr = %q, want it to begin with %q", got, "axon: boom\n")
	}
	if strings.Contains(stdout.String(), "boom") {
		t.Fatalf("the message went to stdout: %q", stdout.String())
	}
}
