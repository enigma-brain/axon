// Package download fetches a file over HTTP and checks it: the one way axon
// downloads a release archive.
//
// A download follows redirects only to https addresses, ends when no data
// arrives for a while but never cuts off a transfer that keeps moving,
// refuses a body larger than a limit, and can check the body's SHA-256. A
// program is taken out of a downloaded .tar.gz by ExtractFile.
package download

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/enigma-brain/axon/internal/termtext"
)

// DefaultStall is how long a download waits for its response, and then for
// each further piece of its body, unless Options.Stall says otherwise.
const DefaultStall = 30 * time.Second

// maxRedirects bounds how many redirects a request follows.
const maxRedirects = 10

// Options are the limits and the check of one download.
type Options struct {
	// Limit is the largest body accepted, in bytes. It is required.
	Limit int64
	// Stall ends the download when no data arrives for this long. Zero
	// means DefaultStall.
	Stall time.Duration
	// SHA256 is the hex SHA-256 the body has. Empty means it is not
	// checked.
	SHA256 string
	// Progress, when set, is called after each piece of the body with the
	// bytes received so far and the size the server declared, or -1 when
	// it declared none.
	Progress func(done, total int64)
}

// Client returns the client every download uses: it follows at most 10
// redirects, and only to https addresses, or to http on the loopback
// address when the first request went there, which only tests do. It
// bounds connecting and waiting for a response's headers, but not reading
// the body, which Fetch bounds by its stall limit.
func Client() *http.Client {
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			if !allowed(via[0].URL, req.URL) {
				return fmt.Errorf("redirected to %s, which is not https", Redact(req.URL))
			}
			return nil
		},
	}
}

var transport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	TLSHandshakeTimeout:   30 * time.Second,
	ResponseHeaderTimeout: DefaultStall,
	ForceAttemptHTTP2:     true,
}

// allowed reports whether a redirect from the first request's address,
// first, to u is followed: to https always, and to http only on the
// loopback address when first is itself http on the loopback address.
func allowed(first, u *url.URL) bool {
	switch u.Scheme {
	case "https":
		return true
	case "http":
		return first.Scheme == "http" && IsLoopback(first) && IsLoopback(u)
	}
	return false
}

// isLoopback reports whether u's host is a loopback IP address.
func IsLoopback(u *url.URL) bool {
	ip := net.ParseIP(u.Hostname())
	return ip != nil && ip.IsLoopback()
}

// Redact returns u as a message prints it: the scheme, host and path,
// without the user information, query string and fragment, which can hold
// a token.
func Redact(u *url.URL) string {
	c := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path, RawPath: u.RawPath}
	return c.String()
}

// redactRaw is Redact of an address that may not parse; one that does not
// is not printed, since it can hold a secret.
func redactRaw(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "the address"
	}
	return Redact(u)
}

// StatusError is a download whose server answered with a status other than
// 200 OK.
type StatusError struct {
	// URL is the address requested, as Redact prints it.
	URL string
	// Code is the HTTP status code.
	Code int
	// Status is the status line, made safe to print.
	Status string
}

func (e *StatusError) Error() string { return fmt.Sprintf("%s answered %s", e.URL, e.Status) }

// StallError is a download ended because no data arrived for After. It is
// a timeout.
type StallError struct{ After time.Duration }

func (e *StallError) Error() string {
	return fmt.Sprintf("the download stopped because no data arrived for %s", inWords(e.After))
}

// Timeout reports that the download ran out of time.
func (e *StallError) Timeout() bool { return true }

// TooLargeError is a body refused because it is larger than Max bytes.
type TooLargeError struct{ Max int64 }

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("the download was refused because it is larger than %s", inBytes(e.Max))
}

// ChecksumError is a body whose SHA-256, Got, is not the one expected, Want.
type ChecksumError struct{ Want, Got string }

func (e *ChecksumError) Error() string {
	return fmt.Sprintf("the download's checksum does not match (expected %s, got %s)", e.Want, e.Got)
}

// Fetch writes the body at rawURL to w. It fails with *StatusError when
// the answer is not 200 OK, *StallError when no data arrives for the stall
// limit, *TooLargeError when the body exceeds opts.Limit, and
// *ChecksumError when opts.SHA256 is set and the body does not match it.
// Other failures, including a cancelled ctx, are the HTTP client's,
// without the address it quotes. Whatever was written to w before a failure is the
// caller's to discard. Every wait ends as soon as ctx is done.
func Fetch(ctx context.Context, rawURL string, w io.Writer, opts Options) error {
	after := opts.Stall
	if after <= 0 {
		after = DefaultStall
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stalled := &StallError{After: after}
	timer := time.AfterFunc(after, func() { cancel(stalled) })
	defer timer.Stop()
	// wrap reports a stall as such rather than as a cancellation, and drops
	// the address the HTTP client quotes, query string included, since it
	// can hold a token.
	wrap := func(err error) error {
		if context.Cause(ctx) == error(stalled) {
			return stalled
		}
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		if strings.HasPrefix(err.Error(), "failed to parse Location header") {
			err = errBadRedirect
		}
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := Client().Do(req)
	if err != nil {
		return wrap(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &StatusError{URL: redactRaw(rawURL), Code: resp.StatusCode, Status: termtext.Clean(resp.Status)}
	}
	if resp.ContentLength > opts.Limit {
		return &TooLargeError{Max: opts.Limit}
	}
	timer.Reset(after)

	h := sha256.New()
	dst := io.MultiWriter(w, h)
	if opts.Progress != nil {
		dst = io.MultiWriter(dst, &progressWriter{total: resp.ContentLength, report: opts.Progress})
	}
	body := &resettingReader{r: resp.Body, timer: timer, after: after}
	n, err := io.Copy(dst, io.LimitReader(body, opts.Limit+1))
	if err != nil {
		return wrap(err)
	}
	if n > opts.Limit {
		return &TooLargeError{Max: opts.Limit}
	}
	if opts.SHA256 != "" {
		if got := hex.EncodeToString(h.Sum(nil)); got != opts.SHA256 {
			return &ChecksumError{Want: opts.SHA256, Got: got}
		}
	}
	return nil
}

// errBadRedirect is a redirect to an address that does not parse.
var errBadRedirect = errors.New("redirected to an address that is not valid")

// resettingReader restarts timer on every read that returns data.
type resettingReader struct {
	r     io.Reader
	timer *time.Timer
	after time.Duration
}

func (b *resettingReader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if n > 0 {
		b.timer.Reset(b.after)
	}
	return n, err
}

// progressWriter reports the bytes written to it so far.
type progressWriter struct {
	total, done int64
	report      func(done, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	p.report(p.done, p.total)
	return len(b), nil
}

// ErrNotInArchive is ExtractFile's error when the archive holds no regular
// file of the name asked for.
var ErrNotInArchive = errors.New("the archive holds no such file")

// ExtractFile returns a reader of the first regular file named name, in
// whatever directory, in the gzipped tar archive r. Every other entry is
// ignored. Reading more than max bytes from it fails with an error that
// names the file. It fails with ErrNotInArchive when there is no such
// file.
func ExtractFile(r io.Reader, name string, max int64) (io.Reader, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("read the archive: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, ErrNotInArchive
		}
		if err != nil {
			return nil, fmt.Errorf("read the archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || path.Base(hdr.Name) != name {
			continue
		}
		return &limitedEntry{r: tr, left: max, name: name, max: max}, nil
	}
}

// limitedEntry reads an archive entry, failing once it passes max bytes.
type limitedEntry struct {
	r         io.Reader
	left, max int64
	name      string
}

func (e *limitedEntry) Read(p []byte) (int, error) {
	if e.left < 0 {
		return 0, e.tooLarge()
	}
	if int64(len(p)) > e.left+1 {
		p = p[:e.left+1]
	}
	n, err := e.r.Read(p)
	e.left -= int64(n)
	if e.left < 0 {
		return 0, e.tooLarge()
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return n, fmt.Errorf("read the archive: %w", err)
	}
	return n, err
}

func (e *limitedEntry) tooLarge() error {
	return fmt.Errorf("%s in the archive is larger than %s", e.name, inBytes(e.max))
}

// inWords writes d in words, such as "30 seconds".
func inWords(d time.Duration) string {
	if d >= time.Second && d%time.Second == 0 {
		if s := int(d / time.Second); s != 1 {
			return fmt.Sprintf("%d seconds", s)
		}
		return "1 second"
	}
	return d.String()
}

// inBytes writes n bytes in MiB when it is a whole number of them, and in
// bytes otherwise.
func inBytes(n int64) string {
	if n >= 1<<20 && n%(1<<20) == 0 {
		return fmt.Sprintf("%d MiB", n>>20)
	}
	return fmt.Sprintf("%d bytes", n)
}
