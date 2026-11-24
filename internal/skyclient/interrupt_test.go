package skyclient

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/enigma-brain/axon/internal/interrupt"
)

// cancelAfter returns a context that is cancelled with cause after d.
func cancelAfter(t *testing.T, d time.Duration, cause error) context.Context {
	ctx, cancel := context.WithCancelCause(context.Background())
	timer := time.AfterFunc(d, func() { cancel(cause) })
	t.Cleanup(func() {
		timer.Stop()
		cancel(nil)
	})
	return ctx
}

// temporaryFiles returns the files under root whose names begin with a dot
// and hold prefix, as the temporary files of a download or a write do.
func temporaryFiles(t *testing.T, root, prefix string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), "."+prefix) {
			found = append(found, path)
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return found
}

// An install that waits for another install's lock stops waiting when it
// is cancelled.
func TestEnsureWaitStopsWhenCancelled(t *testing.T) {
	f := newFixture(t)
	tools, err := f.dir.ToolsDir()
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := lockTools(context.Background(), tools, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	start := time.Now()
	_, _, err = Ensure(cancelAfter(t, 100*time.Millisecond, nil), f.dir, "0.13.0", nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Ensure error = %v, want the cancellation", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Ensure took %s to stop after the cancellation", d)
	}
}

// An install cancelled while it downloads uv leaves no temporary file, and
// the client installed before it in place.
func TestEnsureCancelledDuringDownloadLeavesNothing(t *testing.T) {
	f := newFixture(t)
	if _, _, err := Ensure(context.Background(), f.dir, "0.13.0", nil); err != nil {
		t.Fatal(err)
	}
	// Without its uv, the next install downloads uv again.
	if err := os.RemoveAll(filepath.Join(f.tools, "uv")); err != nil {
		t.Fatal(err)
	}
	setDownloadLimits(t, time.Minute, 1<<20)
	uvBaseURL = serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		hang(t, r)
	})

	_, _, err := Ensure(cancelAfter(t, 300*time.Millisecond, &interrupt.Error{Signal: syscall.SIGINT}), f.dir, "0.14.1", nil)
	// The HTTP client reports the context's cause, the interrupt.
	var interrupted *interrupt.Error
	if !errors.As(err, &interrupted) {
		t.Fatalf("Ensure error = %v, want the interrupt", err)
	}
	if left := temporaryFiles(t, f.tools, "uv-"); len(left) > 0 {
		t.Errorf("the cancelled download left %v", left)
	}
	if v, ok, _ := Version(f.dir); !ok || v != "0.13.0" {
		t.Errorf("Version = %q, %v after the cancelled install; want 0.13.0", v, ok)
	}
	if out := runSky(t, f.dir, "status"); out != "private sky 0.13.0 status" {
		t.Errorf("sky printed %q", out)
	}
}

// An install cancelled while uv installs the package stops uv and returns
// at once, and leaves the client installed before it in place. Cancelled by
// SIGTERM, uv gets SIGTERM; cancelled otherwise, it is killed.
func TestEnsureCancelledDuringUVStopsUV(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		// stopped is how uv's exit reads.
		stopped string
	}{
		{"SIGTERM", &interrupt.Error{Signal: syscall.SIGTERM}, "signal: terminated"},
		{"cancellation", nil, "signal: killed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if _, _, err := Ensure(context.Background(), f.dir, "0.13.0", nil); err != nil {
				t.Fatal(err)
			}
			// The fake uv waits in the package stage for a file that never
			// appears.
			t.Setenv("FAKEUV_PIP_RELEASE", filepath.Join(t.TempDir(), "never"))
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			go func() {
				for !strings.Contains(f.uvLog(t), "uv pip install --python "+filepath.Join(f.tools, skypilotDir, "0.14.1")) {
					time.Sleep(20 * time.Millisecond)
				}
				cancel(tc.cause)
			}()

			start := time.Now()
			_, _, err := Ensure(ctx, f.dir, "0.14.1", nil)
			if err == nil {
				t.Fatal("the cancelled Ensure succeeded")
			}
			// uv's exit is the error; the caller learns of the
			// cancellation from the context.
			if !strings.Contains(err.Error(), "uv pip: "+tc.stopped) {
				t.Errorf("Ensure error = %v, want uv stopped by %q", err, tc.stopped)
			}
			if d := time.Since(start); d > 3*time.Second {
				t.Errorf("Ensure took %s to stop", d)
			}
			if v, ok, _ := Version(f.dir); !ok || v != "0.13.0" {
				t.Errorf("Version = %q, %v after the cancelled install; want 0.13.0", v, ok)
			}
		})
	}
}
