package skyclient

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/enigma-brain/axon/internal/axondir"
)

// TestRealInstall installs the client for real, from the network, into an
// empty home directory. It downloads a few hundred megabytes, so it runs
// only when AXON_REAL_INSTALL=1.
func TestRealInstall(t *testing.T) {
	if os.Getenv("AXON_REAL_INSTALL") != "1" {
		t.Skip("set AXON_REAL_INSTALL=1 to install the SkyPilot client for real")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	// With the XDG variables unset, anything that falls back to a default
	// directory writes into the empty home, where the check below finds it.
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "XDG_") {
			t.Setenv(key, "")
			os.Unsetenv(key)
		}
	}
	dir := axondir.New(home)
	ctx := context.Background()

	d := defaults()
	log := &testWriter{t: t}
	from, to, err := d.ensure(ctx, dir, "", log)
	log.flush()
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if from != "" || to != DefaultVersion {
		t.Errorf("Ensure moved from %q to %q, want from \"\" to %s", from, to, DefaultVersion)
	}

	// Checked before sky runs: the client creates SkyPilot's own ~/.sky the
	// first time it runs, which is not part of the install.
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != ".axon" {
			t.Errorf("the install created %s outside ~/.axon", e.Name())
		}
	}

	cmd, err := skyCommand(ctx, dir, "--version")
	if err != nil {
		t.Fatalf("skyCommand: %v", err)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sky --version: %v\n%s", err, out)
	}
	t.Logf("sky --version: %s", bytes.TrimSpace(out))
	if !strings.Contains(string(out), DefaultVersion) {
		t.Errorf("sky --version printed %q, want version %s", out, DefaultVersion)
	}

	// A second call at the same version downloads nothing: any download
	// would fail against this address, and no stage reports progress.
	d.uvBaseURL = "http://127.0.0.1:1/unreachable"
	var progress bytes.Buffer
	from, to, err = d.ensure(ctx, dir, DefaultVersion, &progress)
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if from != DefaultVersion || to != DefaultVersion || progress.Len() != 0 {
		t.Errorf("second Ensure moved from %q to %q with progress %q; want no change", from, to, progress.String())
	}
}

// testWriter sends progress to the test log, one line at a time.
type testWriter struct {
	t       *testing.T
	pending []byte
}

func (w *testWriter) Write(p []byte) (int, error) {
	w.pending = append(w.pending, p...)
	for {
		i := bytes.IndexByte(w.pending, '\n')
		if i < 0 {
			return len(p), nil
		}
		w.t.Log(string(w.pending[:i]))
		w.pending = w.pending[i+1:]
	}
}

func (w *testWriter) flush() {
	if len(w.pending) > 0 {
		w.t.Log(string(w.pending))
		w.pending = nil
	}
}
