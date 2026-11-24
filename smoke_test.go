package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/enigma-brain/axon/internal/skyclient"
)

// smokeFile is the config file the smoke test adds. Every detail is
// made up. Its endpoint is port 1 on the loopback address, where nothing
// listens, so the login stops at the reachability check, the step before
// the browser login, and no request leaves the machine.
const smokeFile = `format = 2
id = "example-smoke"

[[pathway]]
id = "smoke"
kind = "direct"
name = "Smoke"
description = "A made-up pathway whose server is never reached."

[pathway.sky]
endpoint = "https://127.0.0.1:1"
`

// TestSmokeLogin runs axon as a user would, in an empty home directory:
// axon login --config <file> stores the file, installs the SkyPilot client for
// real, and stops where the login would open a browser; then the installed
// client runs sky --version. The install
// downloads a few hundred megabytes, so the test runs only when
// AXON_REAL_INSTALL=1.
//
// It builds axon from this checkout, or runs the binary AXON_SMOKE_BIN
// names, such as one the install line installed.
//
// The fence is runAxon's, without the dead proxy, since the client is
// downloaded: HOME is a temporary directory, PATH an empty one, and the
// process has no controlling terminal.
func TestSmokeLogin(t *testing.T) {
	if os.Getenv("AXON_REAL_INSTALL") != "1" {
		t.Skip("set AXON_REAL_INSTALL=1 to run axon login with a real SkyPilot client install")
	}

	bin := os.Getenv("AXON_SMOKE_BIN")
	if bin == "" {
		bin = build(t, "0.0.0-smoke")
	}
	home := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "example-smoke.toml")
	if err := os.WriteFile(configPath, []byte(smokeFile), 0o600); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) (stdout, stderr string, err error) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = []string{"HOME=" + home, "PATH=" + t.TempDir()}
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		err = cmd.Run()
		return out.String(), errOut.String(), err
	}

	out, _, err := run("--version")
	if err != nil || !strings.HasPrefix(out, "axon ") {
		t.Fatalf("axon --version = %q, %v", out, err)
	}
	t.Logf("%s", strings.TrimSpace(out))

	stdout, stderr, err := run("login", "--config", configPath)
	t.Logf("axon login stdout:\n%s", stdout)
	t.Logf("axon login stderr:\n%s", stderr)
	wantFailure(t, "axon login", err)

	if !strings.Contains(stdout, "Stored the config file ") {
		t.Errorf("axon login did not report storing the config file")
	}
	if !strings.Contains(stderr, "Can't reach 127.0.0.1:1.") {
		t.Errorf("axon login did not stop at the reachability check, before the browser login")
	}

	stored, err := os.ReadFile(filepath.Join(home, ".axon", "configs", "example-smoke.toml"))
	if err != nil {
		t.Errorf("read the stored config file: %v", err)
	} else if string(stored) != smokeFile {
		t.Errorf("the stored config file is not the file given:\n%s", stored)
	}

	current, err := os.ReadFile(filepath.Join(home, ".axon", "tools", "skypilot", "current"))
	if err != nil {
		t.Errorf("read the installed client's version: %v", err)
	} else if got := strings.TrimSpace(string(current)); got != skyclient.DefaultVersion {
		t.Errorf("the installed client is at %q, want %s", got, skyclient.DefaultVersion)
	}
	sky := filepath.Join(home, ".axon", "tools", "skypilot", skyclient.DefaultVersion, "bin", "sky")
	if _, err := os.Stat(sky); err != nil {
		t.Errorf("no installed sky: %v", err)
	}

	// The login stops before sky runs, so nothing is outside ~/.axon: not
	// even SkyPilot's own ~/.sky, which sky creates when it first runs.
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != ".axon" {
			t.Errorf("axon login created %s outside ~/.axon", e.Name())
		}
	}

	// The installed client runs and reports the version axon installed.
	// This comes after the check above, since sky creates ~/.sky when it
	// first runs.
	cmd := exec.Command(sky, "--version")
	cmd.Env = []string{"HOME=" + home, "PATH=" + t.TempDir()}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	skyOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sky --version: %v\n%s", err, skyOut)
	}
	t.Logf("sky --version: %s", bytes.TrimSpace(skyOut))
	if !strings.Contains(string(skyOut), skyclient.DefaultVersion) {
		t.Errorf("sky --version printed %q, want version %s", skyOut, skyclient.DefaultVersion)
	}
}
