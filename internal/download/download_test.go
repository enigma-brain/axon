package download

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/enigma-brain/axon/internal/netreason"
)

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

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestFetch(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "content")
	})
	var got bytes.Buffer
	var reported []int64
	opts := Options{Limit: 1 << 20, SHA256: sum("content"), Progress: func(done, total int64) {
		reported = append(reported, done)
		if total != 7 {
			t.Errorf("Progress total = %d, want 7", total)
		}
	}}
	if err := Fetch(context.Background(), url, &got, opts); err != nil {
		t.Fatal(err)
	}
	if got.String() != "content" {
		t.Errorf("body = %q", got.String())
	}
	if len(reported) == 0 || reported[len(reported)-1] != 7 {
		t.Errorf("Progress reported %v, want it to end at 7", reported)
	}
}

func TestFetchChecksumMismatch(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "tampered")
	})
	err := Fetch(context.Background(), url, io.Discard, Options{Limit: 1 << 20, SHA256: sum("content")})
	var mismatch *ChecksumError
	if !errors.As(err, &mismatch) || mismatch.Want != sum("content") || mismatch.Got != sum("tampered") {
		t.Fatalf("Fetch = %v, want a checksum mismatch", err)
	}
}

// A status line is printed cleaned of anything that acts on the terminal,
// and the address without its query string.
func TestFetchStatus(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// net/http writes the status text it knows; hijack to send a raw one.
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		buf.WriteString("HTTP/1.1 404 Not \x1b[31mFound\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		buf.Flush()
	})
	srv.Start()
	t.Cleanup(srv.Close)

	err := Fetch(context.Background(), srv.URL+"/file?token=secret", io.Discard, Options{Limit: 1 << 20})
	var status *StatusError
	if !errors.As(err, &status) || status.Code != http.StatusNotFound {
		t.Fatalf("Fetch = %v, want a 404", err)
	}
	if strings.ContainsRune(err.Error(), '\x1b') || strings.Contains(err.Error(), "secret") {
		t.Errorf("error %q holds the escape or the query", err)
	}
	if want := srv.URL + "/file answered 404 Not"; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("error = %q, want it to start %q", err, want)
	}
}

func TestFetchEndsWhenHeadersStall(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, r *http.Request) { hang(t, r) })
	start := time.Now()
	err := Fetch(context.Background(), url, io.Discard, Options{Limit: 1 << 20, Stall: 100 * time.Millisecond})
	var stalled *StallError
	if !errors.As(err, &stalled) {
		t.Fatalf("Fetch = %v, want a stall", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("the stall took %s to end", time.Since(start))
	}
}

// A stall reads in full where it is printed as it is, and as "timed out"
// where it is shortened.
func TestFetchEndsWhenBodyStalls(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		hang(t, r)
	})
	err := Fetch(context.Background(), url, io.Discard, Options{Limit: 1 << 20, Stall: 100 * time.Millisecond})
	var stalled *StallError
	if !errors.As(err, &stalled) {
		t.Fatalf("Fetch = %v, want a stall", err)
	}
	if want := "the download stopped because no data arrived for 100ms"; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
	if got := netreason.Short(err); got != netreason.TimedOut {
		t.Errorf("netreason.Short = %q, want %q", got, netreason.TimedOut)
	}
	if got := (&StallError{After: DefaultStall}).Error(); got != "the download stopped because no data arrived for 30 seconds" {
		t.Errorf("the default stall reads %q", got)
	}
}

func TestFetchKeepsASlowBodyThatMoves(t *testing.T) {
	// Each piece arrives well within the stall limit, but the whole body
	// takes several times as long.
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
	if err := Fetch(context.Background(), url, &got, Options{Limit: 1 << 20, Stall: 300 * time.Millisecond}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.String() != strings.Repeat("x", pieces) {
		t.Errorf("body = %q, want %d bytes of x", got.String(), pieces)
	}
}

// Cancelling the context ends a download part way through its body at once.
func TestFetchCancelledMidBody(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		hang(t, r)
	})
	ctx, cancel := context.WithCancel(context.Background())
	w := &cancelOnWrite{cancel: cancel}
	start := time.Now()
	err := Fetch(ctx, url, w, Options{Limit: 1 << 20})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch = %v, want it cancelled", err)
	}
	var stalled *StallError
	if errors.As(err, &stalled) {
		t.Errorf("a cancellation is reported as a stall: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("the cancellation took %s to end the download", time.Since(start))
	}
}

// cancelOnWrite cancels a context on the first write it receives.
type cancelOnWrite struct{ cancel context.CancelFunc }

func (c *cancelOnWrite) Write(p []byte) (int, error) {
	c.cancel()
	return len(p), nil
}

func TestFetchRefusesADeclaredSizeOverTheLimit(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(2<<20))
		w.(http.Flusher).Flush()
	})
	var got bytes.Buffer
	err := Fetch(context.Background(), url, &got, Options{Limit: 1 << 20})
	var tooLarge *TooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("Fetch = %v, want a refusal for size", err)
	}
	if want := "the download was refused because it is larger than 1 MiB"; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
	if got.Len() != 0 {
		t.Errorf("Fetch wrote %d bytes before refusing", got.Len())
	}
}

func TestFetchRefusesAnUndeclaredSizeOverTheLimit(t *testing.T) {
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
	err := Fetch(context.Background(), url, io.Discard, Options{Limit: 1 << 20})
	var tooLarge *TooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("Fetch = %v, want a refusal for size", err)
	}
}

func TestFetchFollowsRedirectsOnLoopbackHTTP(t *testing.T) {
	var target string
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/moved" {
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
		io.WriteString(w, "content")
	})
	target = url + "/file"
	var got bytes.Buffer
	if err := Fetch(context.Background(), url+"/moved", &got, Options{Limit: 1 << 20}); err != nil {
		t.Fatal(err)
	}
	if got.String() != "content" {
		t.Errorf("body = %q", got.String())
	}
}

// A redirect does not hand the next server the address redirected from,
// whose query string can hold a token.
func TestFetchSendsNoRefererOnARedirect(t *testing.T) {
	referer := "unset"
	to := serve(t, func(w http.ResponseWriter, r *http.Request) {
		referer = r.Header.Get("Referer")
		io.WriteString(w, "content")
	})
	from := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, to+"/file", http.StatusFound)
	})
	if err := Fetch(context.Background(), from+"/file?token=secret", io.Discard, Options{Limit: 1 << 20}); err != nil {
		t.Fatal(err)
	}
	if referer != "" {
		t.Errorf("the redirected request carried Referer %q", referer)
	}
}

// An address that does not parse is not printed, since it can hold a token.
func TestFetchInvalidAddress(t *testing.T) {
	err := Fetch(context.Background(), "https://example.org/%zz?token=secret", io.Discard, Options{Limit: 1 << 20})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("Fetch = %v, want an error without the address", err)
	}
}

func TestFetchRefusesARedirectAwayFromHTTPS(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.org/file?token=secret", http.StatusFound)
	})
	err := Fetch(context.Background(), url, io.Discard, Options{Limit: 1 << 20})
	if err == nil || !strings.Contains(err.Error(), "redirected to http://example.org/file, which is not https") {
		t.Fatalf("Fetch = %v, want the redirect refused", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("error %q holds the query", err)
	}
}

func TestAllowed(t *testing.T) {
	parse := func(s string) *url.URL {
		u, err := url.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	for _, tc := range []struct {
		first, to string
		want      bool
	}{
		{"https://github.com/a", "https://objects.example.org/b", true},
		{"https://github.com/a", "http://objects.example.org/b", false},
		{"https://github.com/a", "http://127.0.0.1/b", false},
		{"http://127.0.0.1:8080/a", "http://127.0.0.1:9090/b", true},
		{"http://127.0.0.1:8080/a", "http://example.org/b", false},
		{"http://127.0.0.1:8080/a", "https://example.org/b", true},
		{"https://github.com/a", "ftp://example.org/b", false},
	} {
		if got := allowed(parse(tc.first), parse(tc.to)); got != tc.want {
			t.Errorf("allowed(%s, %s) = %v, want %v", tc.first, tc.to, got, tc.want)
		}
	}
}

func TestRedact(t *testing.T) {
	for raw, want := range map[string]string{
		"https://user:pass@example.org/path/file?token=secret#frag": "https://example.org/path/file",
		"https://example.org:8443/a%2Fb":                            "https://example.org:8443/a%2Fb",
		"https://example.org":                                       "https://example.org",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := Redact(u); got != want {
			t.Errorf("Redact(%s) = %q, want %q", raw, got, want)
		}
	}
}

// archive returns a gzipped tar archive of the entries, name then content.
func archive(t *testing.T, entries ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for i := 0; i < len(entries); i += 2 {
		name, content := entries[i], entries[i+1]
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if strings.HasSuffix(name, "/") {
			hdr = &tar.Header{Name: name, Mode: 0o755, Typeflag: tar.TypeDir}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractFile(t *testing.T) {
	data := archive(t, "README", "readme", "dist/uv/", "", "dist/uvx", "other", "dist/uv", "the program", "uv", "second")
	r, err := ExtractFile(bytes.NewReader(data), "uv", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil || string(got) != "the program" {
		t.Errorf("ExtractFile read %q, %v; want the first file named uv", got, err)
	}
}

func TestExtractFileMissing(t *testing.T) {
	data := archive(t, "README", "readme", "uv/", "")
	if _, err := ExtractFile(bytes.NewReader(data), "uv", 1<<20); !errors.Is(err, ErrNotInArchive) {
		t.Errorf("ExtractFile = %v, want ErrNotInArchive", err)
	}
}

func TestExtractFileTooLarge(t *testing.T) {
	data := archive(t, "uv", strings.Repeat("x", 100))
	r, err := ExtractFile(bytes.NewReader(data), "uv", 99)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(r)
	if err == nil || err.Error() != "uv in the archive is larger than 99 bytes" {
		t.Errorf("reading past the limit = %v", err)
	}
	r, err = ExtractFile(bytes.NewReader(data), "uv", 100)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := io.ReadAll(r); err != nil || len(got) != 100 {
		t.Errorf("reading exactly the limit = %d bytes, %v", len(got), err)
	}
}

func TestExtractFileNotAnArchive(t *testing.T) {
	if _, err := ExtractFile(strings.NewReader("not gzip"), "uv", 1<<20); err == nil || !strings.HasPrefix(err.Error(), "read the archive: ") {
		t.Errorf("ExtractFile = %v, want an archive error", err)
	}
}

func TestInBytesAndWords(t *testing.T) {
	for n, want := range map[int64]string{1 << 20: "1 MiB", 256 << 20: "256 MiB", 99: "99 bytes", 1<<20 + 1: "1048577 bytes"} {
		if got := inBytes(n); got != want {
			t.Errorf("inBytes(%d) = %q, want %q", n, got, want)
		}
	}
	for d, want := range map[time.Duration]string{30 * time.Second: "30 seconds", time.Second: "1 second", 1500 * time.Millisecond: "1.5s"} {
		if got := inWords(d); got != want {
			t.Errorf("inWords(%s) = %q, want %q", d, got, want)
		}
	}
}
