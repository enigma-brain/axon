package skyclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// setDownloadLimits replaces the stall timeout and the size limit for one
// test.
func setDownloadLimits(t *testing.T, stall time.Duration, limit int64) {
	t.Helper()
	oldStall, oldLimit := stallTimeout, maxDownload
	stallTimeout, maxDownload = stall, limit
	t.Cleanup(func() { stallTimeout, maxDownload = oldStall, oldLimit })
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

func TestDownloadEndsWhenHeadersStall(t *testing.T) {
	setDownloadLimits(t, 100*time.Millisecond, 1<<20)
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		hang(t, r)
	})

	start := time.Now()
	_, err := download(context.Background(), url, io.Discard, io.Discard)
	var stalled *stallError
	if !errors.As(err, &stalled) {
		t.Fatalf("download error = %v, want a stall", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("the stall took %s to end", time.Since(start))
	}
}

func TestDownloadEndsWhenBodyStalls(t *testing.T) {
	setDownloadLimits(t, 100*time.Millisecond, 1<<20)
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		hang(t, r)
	})

	_, err := download(context.Background(), url, io.Discard, io.Discard)
	var stalled *stallError
	if !errors.As(err, &stalled) {
		t.Fatalf("download error = %v, want a stall", err)
	}
	if want := "the download stopped because no data arrived for 100ms"; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}

func TestDownloadKeepsASlowBodyThatMoves(t *testing.T) {
	// Each piece arrives well within the stall timeout, but the whole body
	// takes several times as long.
	setDownloadLimits(t, 300*time.Millisecond, 1<<20)
	const pieces = 12
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(pieces))
		for i := 0; i < pieces; i++ {
			w.Write([]byte("x"))
			w.(http.Flusher).Flush()
			time.Sleep(100 * time.Millisecond)
		}
	})

	var got bytes.Buffer
	sum, err := download(context.Background(), url, &got, io.Discard)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	want := sha256.Sum256([]byte(strings.Repeat("x", pieces)))
	if sum != hex.EncodeToString(want[:]) || got.String() != strings.Repeat("x", pieces) {
		t.Errorf("download = %q (%s), want %d bytes of x", got.String(), sum, pieces)
	}
}

func TestDownloadRefusesADeclaredSizeOverTheLimit(t *testing.T) {
	setDownloadLimits(t, time.Second, 1<<20)
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(2<<20))
		w.(http.Flusher).Flush()
	})

	var got bytes.Buffer
	_, err := download(context.Background(), url, &got, io.Discard)
	var tooLarge *tooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("download error = %v, want a refusal for size", err)
	}
	if want := "the download was refused because it is larger than 1 MiB"; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
	if got.Len() != 0 {
		t.Errorf("download wrote %d bytes before refusing", got.Len())
	}
}

func TestDownloadRefusesAnUndeclaredSizeOverTheLimit(t *testing.T) {
	setDownloadLimits(t, time.Second, 1<<20)
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		// No Content-Length: the body is sent in chunks.
		chunk := bytes.Repeat([]byte("x"), 64<<10)
		for i := 0; i < 40; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	})

	_, err := download(context.Background(), url, io.Discard, io.Discard)
	var tooLarge *tooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("download error = %v, want a refusal for size", err)
	}
}

func TestEnsureStalledDownloadSaysSo(t *testing.T) {
	f := newFixture(t)
	setDownloadLimits(t, 100*time.Millisecond, 1<<20)
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		hang(t, r)
	})
	uvBaseURL = url

	_, _, err := Ensure(context.Background(), f.dir, "0.13.0", nil)
	want := "download uv " + uvVersion + ": the download stopped because no data arrived for 100ms"
	if err == nil || err.Error() != want {
		t.Errorf("Ensure error = %v, want %q", err, want)
	}
	if errors.Is(err, ErrUnavailable) {
		t.Errorf("a stalled download is ErrUnavailable: %v", err)
	}
}

func TestInWords(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second:        "30 seconds",
		time.Second:             "1 second",
		1500 * time.Millisecond: "1.5s",
	} {
		if got := inWords(d); got != want {
			t.Errorf("inWords(%s) = %q, want %q", d, got, want)
		}
	}
}
