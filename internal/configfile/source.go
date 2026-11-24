package configfile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/download"
	"github.com/enigma-brain/axon/internal/netreason"
	"github.com/enigma-brain/axon/internal/termtext"
)

// MaxSize is the largest config file read from any source, in bytes.
const MaxSize = 1 << 20

var (
	// fetchTimeout limits a fetch from an address, from the request to the
	// end of the body.
	fetchTimeout = 30 * time.Second
	// transport carries fetches from an address. Tests replace it with one
	// that trusts their local TLS server.
	transport http.RoundTripper = http.DefaultTransport

	schemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)
)

type sourceKind int

const (
	sourcePath sourceKind = iota
	sourceAddress
	sourceStdin
)

// Source is where a config file comes from: an https address, a
// file path or standard input.
type Source struct {
	kind  sourceKind
	value string
}

// stdinValue is the source that names standard input.
const stdinValue = "-"

// ParseSource returns the source s names: "-" for standard input, an
// address when s has a scheme, and a file path otherwise. An address is
// refused unless its scheme is https, it has a host, and it carries no user
// name or password. A path's leading "~/" is the home directory, and the
// path is made absolute.
func ParseSource(s string) (Source, error) {
	switch {
	case s == "":
		return Source{}, errors.New("the config file's source is empty")
	case s == stdinValue:
		return Source{kind: sourceStdin, value: stdinValue}, nil
	case schemePattern.MatchString(s):
		// Errors name the address as download.Redact gives it, and say nothing
		// of an address that does not parse, since it can hold a secret.
		u, err := url.Parse(s)
		if err != nil {
			return Source{}, errors.New("the config file's address is not a valid URL")
		}
		if u.Scheme != "https" {
			return Source{}, fmt.Errorf("%s: an address must start with https://", download.Redact(u))
		}
		if u.Host == "" {
			return Source{}, fmt.Errorf("%s: the address has no host", download.Redact(u))
		}
		if u.User != nil {
			return Source{}, fmt.Errorf("%s: an address must not carry a user name or password", download.Redact(u))
		}
		return Source{kind: sourceAddress, value: s}, nil
	}
	path := s
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return Source{}, fmt.Errorf("find the home directory for %s: %w", s, err)
		}
		path = filepath.Join(home, rest)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Source{}, fmt.Errorf("resolve %s: %w", s, err)
	}
	return Source{kind: sourcePath, value: abs}, nil
}

// String returns the source as messages print it: the address without its
// user information, query string or fragment, which can hold a token; the
// absolute path; or "-".
func (s Source) String() string {
	if s.kind != sourceAddress {
		return s.value
	}
	u, err := url.Parse(s.value)
	if err != nil {
		return s.value
	}
	return download.Redact(u)
}

// raw returns the source as it is recorded and read: the whole address, the
// absolute path, or "-". It is never printed.
func (s Source) raw() string { return s.value }

// IsAddress reports whether the source is an address, the one kind of
// source that can be read again without the user.
func (s Source) IsAddress() bool { return s.kind == sourceAddress }

// Read returns the bytes of the config file at src, exactly as the
// source gives them. stdin is read for the standard-input source. A file
// larger than MaxSize is refused. Every error names the source.
func Read(ctx context.Context, src Source, stdin io.Reader) ([]byte, error) {
	switch src.kind {
	case sourceStdin:
		data, err := readUntilDone(ctx, stdin)
		if err != nil {
			return nil, fmt.Errorf("read the config file from standard input: %w", err)
		}
		return data, nil
	case sourceAddress:
		return fetch(ctx, src.value, src.String())
	case sourcePath:
		f, err := os.Open(src.value)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", src.value, axondir.UnwrapPath(err))
		}
		defer f.Close()
		data, err := readLimited(f)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", src.value, axondir.UnwrapPath(err))
		}
		return data, nil
	}
	return nil, errors.New("read the config file: no source given")
}

// fetch gets address without credentials, cookies or a Referer, following
// redirects only to https addresses. Errors name the address as shown.
func fetch(ctx context.Context, address, shown string) ([]byte, error) {
	fail := func(err error) error {
		return fmt.Errorf("fetch %s: %w", shown, err)
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   fetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// net/http sets the Referer to the address redirected from,
			// query string included, which can hold a token.
			req.Header.Del("Referer")
			if req.URL.Scheme != "https" {
				return fmt.Errorf("redirected to %s, which is not an https address", download.Redact(req.URL))
			}
			if req.URL.User != nil {
				return fmt.Errorf("redirected to %s, which carries a user name or password", download.Redact(req.URL))
			}
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fail(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fail(requestError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fail(fmt.Errorf("the server answered %s", termtext.Clean(resp.Status)))
	}
	data, err := readLimited(resp.Body)
	if err != nil {
		return nil, fail(err)
	}
	return data, nil
}

// requestError returns the error of a failed request as its message gives
// it after the address: the short network reason, such as "no such host" or
// "timed out", or a badRedirect.
func requestError(err error) error {
	// net/http quotes a Location header it cannot parse, query string
	// included, so that reason is replaced.
	var uerr *url.Error
	if errors.As(err, &uerr) && strings.HasPrefix(uerr.Err.Error(), "failed to parse Location header") {
		return badRedirect{uerr.Err}
	}
	return netError{err}
}

// netError is a failed request. Its message is the short reason, since the
// message it is part of already names the address.
type netError struct{ err error }

func (e netError) Error() string { return netreason.Short(e.err) }

func (e netError) Unwrap() error { return e.err }

// badRedirect is a redirect to an address that does not parse. Its message
// leaves the address out, since it can hold a token; the cause it wraps
// names it.
type badRedirect struct{ err error }

func (e badRedirect) Error() string { return "redirected to an address that is not valid" }

func (e badRedirect) Unwrap() error { return e.err }

// readLimited reads r to its end, refusing more than MaxSize bytes.
// readUntilDone reads r as readLimited does, and returns ctx's error as soon
// as ctx ends, without waiting for the input to end. A read that ctx cut
// short is left running; it ends when the input does or axon exits.
func readUntilDone(ctx context.Context, r io.Reader) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type result struct {
		data []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		data, err := readLimited(r)
		done <- result{data, err}
	}()
	select {
	case res := <-done:
		return res.data, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func readLimited(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxSize {
		return nil, fmt.Errorf("the file is larger than %d bytes", MaxSize)
	}
	return data, nil
}
