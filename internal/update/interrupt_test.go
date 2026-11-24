package update

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/enigma-brain/axon/internal/interrupt"
	"github.com/enigma-brain/axon/internal/testkit"
)

// hangingArchive serves repo, except that the archive of release version
// sends part of its body and then waits until its request ends.
type hangingArchive struct {
	repo *fakeRepo
	name string
}

func (h hangingArchive) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/"+h.name) {
		w.Header().Set("Content-Length", "100000")
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		return
	}
	h.repo.ServeHTTP(w, r)
}

// An update cancelled while it downloads the archive stops, and leaves the
// installed binary as it was, with nothing beside it.
func TestUpdateCancelledDuringDownload(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{latest: "v0.5.0"}
	repo.addRelease(t, "0.5.0")
	d := serve(t, hangingArchive{repo: repo, name: "axon_0.5.0_" + testPlatform + ".tar.gz"})
	exe := installed(t, "0.4.0")

	ctx, cancel := context.WithCancelCause(context.Background())
	timer := time.AfterFunc(300*time.Millisecond, func() { cancel(&interrupt.Error{Signal: syscall.SIGINT}) })
	defer timer.Stop()
	start := time.Now()
	d.executable = func() (string, error) { return exe, nil }
	err := d.run(ctx, Options{Current: "0.4.0", Out: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("the cancelled update succeeded")
	}
	// The error formats its cause; main reads the interrupt from the
	// context, not from it.
	if !strings.Contains(err.Error(), "could not download axon_0.5.0") {
		t.Errorf("error = %v, want the failed download", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("the update took %s to stop", d)
	}
	assertUnchanged(t, exe, "0.4.0")
}

// slowBinary is a fake axon that marks that it started, then waits, and on
// SIGTERM marks that it got the signal and exits.
const slowBinary = `#!/bin/sh
trap 'echo terminated > "$AXON_UPDATE_TEST_DIR/term"; exit 1' TERM
touch "$AXON_UPDATE_TEST_DIR/started"
i=0
while [ $i -lt 200 ]; do sleep 0.05; i=$((i + 1)); done
echo "axon 0.5.0"
`

// An update cancelled by SIGTERM while the downloaded binary answers
// --version passes the signal on, stops, and leaves the installed binary
// as it was, with no staged file beside it.
func TestUpdateCancelledDuringVersionCheck(t *testing.T) {
	marks := t.TempDir()
	t.Setenv("AXON_UPDATE_TEST_DIR", marks)
	repo := &fakeRepo{latest: "v0.5.0"}
	name := "axon_0.5.0_" + testPlatform + ".tar.gz"
	archive := tarGz(t, map[string][]byte{"axon": []byte(slowBinary)})
	repo.setArchive("0.5.0", name, archive, archive)
	d := serve(t, repo)
	exe := installed(t, "0.4.0")

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	// The cancellation comes once the binary is seen to start, or after
	// testkit.WaitTimeout, so that a binary that never starts fails the
	// test instead of waiting for ever.
	seen := make(chan bool, 1)
	go func() {
		deadline := time.Now().Add(testkit.WaitTimeout)
		for {
			if _, err := os.Stat(filepath.Join(marks, "started")); err == nil {
				seen <- true
				break
			}
			if time.Now().After(deadline) {
				seen <- false
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		cancel(&interrupt.Error{Signal: syscall.SIGTERM})
	}()

	start := time.Now()
	d.executable = func() (string, error) { return exe, nil }
	err := d.run(ctx, Options{Current: "0.4.0", Out: &bytes.Buffer{}})
	if !<-seen {
		t.Fatalf("the downloaded binary was not seen to start within %v; Run error = %v", testkit.WaitTimeout, err)
	}
	if err == nil {
		t.Fatal("the cancelled update succeeded")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("the update took %s to stop", d)
	}
	if got, _ := os.ReadFile(filepath.Join(marks, "term")); string(got) != "terminated\n" {
		t.Errorf("the downloaded binary did not get SIGTERM (mark %q)", got)
	}
	assertUnchanged(t, exe, "0.4.0")
}
