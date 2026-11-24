package skyclient

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setDownloadLimits sets the stall timeout and the size limit f installs
// with.
func setDownloadLimits(f *fixture, stall time.Duration, limit int64) {
	f.d.stallTimeout, f.d.maxDownload = stall, limit
}

// serve starts a loopback server with handler and returns its address.
func serve(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
}

// hang blocks until the request is cancelled or the test ends.
func hang(t *testing.T, r *http.Request) {
	select {
	case <-r.Context().Done():
	case <-t.Context().Done():
	}
}

func TestEnsureStalledDownloadSaysSo(t *testing.T) {
	f := newFixture(t)
	setDownloadLimits(f, 100*time.Millisecond, 1<<20)
	f.d.uvBaseURL = serve(t, func(w http.ResponseWriter, r *http.Request) {
		hang(t, r)
	})

	_, _, err := f.d.ensure(context.Background(), f.dir, "0.13.0", nil)
	want := "download uv " + uvVersion + ": the download stopped because no data arrived for 100ms"
	if err == nil || err.Error() != want {
		t.Errorf("Ensure error = %v, want %q", err, want)
	}
	if errors.Is(err, ErrUnavailable) {
		t.Errorf("a stalled download is ErrUnavailable: %v", err)
	}
	uvRoot := filepath.Join(f.tools, "uv")
	entries, err := os.ReadDir(uvRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("%s is left in %s after a failed download", e.Name(), uvRoot)
	}
}

// An install interrupted part way through uv's download, once part of the
// archive is written, stops at once and leaves neither uv nor the partial
// archive.
func TestEnsureCancelledMidDownloadLeavesNothing(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	f.d.uvBaseURL = serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.Write(bytes.Repeat([]byte("x"), 300))
		w.(http.Flusher).Flush()
		hang(t, r)
	})
	// The progress report passes 25% only once the client has written 250
	// bytes of the archive, and the install is cancelled there.
	progress := &cancelAt{mark: "25%", cancel: cancel}

	start := time.Now()
	_, _, err := f.d.ensure(ctx, f.dir, "0.13.0", progress)
	if !progress.seen {
		t.Fatalf("the download was not cancelled part way through; progress: %q, error: %v", progress.buf.String(), err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Ensure error = %v, want it cancelled", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("the cancellation took %s to end the install", time.Since(start))
	}
	uvRoot := filepath.Join(f.tools, "uv")
	if _, err := os.Stat(filepath.Join(uvRoot, uvVersion, "uv")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("uv is there after a cancelled download: %v", err)
	}
	entries, err := os.ReadDir(uvRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("%s is left in %s", e.Name(), uvRoot)
	}
}

// cancelAt records what is written to it, and cancels once mark appears.
type cancelAt struct {
	mark   string
	cancel context.CancelFunc
	buf    bytes.Buffer
	seen   bool
}

func (c *cancelAt) Write(p []byte) (int, error) {
	c.buf.Write(p)
	if !c.seen && strings.Contains(c.buf.String(), c.mark) {
		c.seen = true
		c.cancel()
	}
	return len(p), nil
}

func TestQuarters(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	report := quarters(&out)
	for _, done := range []int64{10, 25, 60, 99, 100, 100} {
		report(done, 100)
	}
	if want := " 25% 50% 75% 100%"; out.String() != want {
		t.Errorf("quarters wrote %q, want %q", out.String(), want)
	}
	out.Reset()
	quarters(&out)(50, -1)
	if out.Len() != 0 {
		t.Errorf("quarters wrote %q for an undeclared size", out.String())
	}
}
