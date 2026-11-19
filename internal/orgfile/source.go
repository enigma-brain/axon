package orgfile

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
)

// MaxSize is the largest organisation file read from any source, in bytes.
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

// Source is where an organisation file comes from: an https address, a
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
		return Source{}, errors.New("the organisation file's source is empty")
	case s == stdinValue:
		return Source{kind: sourceStdin, value: stdinValue}, nil
	case schemePattern.MatchString(s):
		// Errors name the address with any password redacted, and say
		// nothing of an address that does not parse, since it can hold one.
		u, err := url.Parse(s)
		if err != nil {
			return Source{}, errors.New("the organisation file's address is not a valid URL")
		}
		if u.Scheme != "https" {
			return Source{}, fmt.Errorf("%s: an address must start with https://", u.Redacted())
		}
		if u.Host == "" {
			return Source{}, fmt.Errorf("%s: the address has no host", u.Redacted())
		}
		if u.User != nil {
			return Source{}, fmt.Errorf("%s: an address must not carry a user name or password", u.Redacted())
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

// String returns the source as it is recorded: the address, the absolute
// path, or "-".
func (s Source) String() string { return s.value }

// IsAddress reports whether the source is an address, the one kind of
// source that can be read again without the user.
func (s Source) IsAddress() bool { return s.kind == sourceAddress }

// Read returns the bytes of the organisation file at src, exactly as the
// source gives them. stdin is read for the standard-input source. A file
// larger than MaxSize is refused. Every error names the source.
func Read(ctx context.Context, src Source, stdin io.Reader) ([]byte, error) {
	switch src.kind {
	case sourceStdin:
		data, err := readLimited(stdin)
		if err != nil {
			return nil, fmt.Errorf("read the organisation file from standard input: %w", err)
		}
		return data, nil
	case sourceAddress:
		return fetch(ctx, src.value)
	case sourcePath:
		f, err := os.Open(src.value)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", src.value, unwrapPath(err))
		}
		defer f.Close()
		data, err := readLimited(f)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", src.value, unwrapPath(err))
		}
		return data, nil
	}
	return nil, errors.New("read the organisation file: no source given")
}

// fetch gets address without credentials or cookies, following redirects
// only to https addresses.
func fetch(ctx context.Context, address string) ([]byte, error) {
	fail := func(err error) error {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return fmt.Errorf("fetch %s: %w", address, err)
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   fetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return fmt.Errorf("redirected to %s, which is not an https address", req.URL.Redacted())
			}
			if req.URL.User != nil {
				return fmt.Errorf("redirected to %s, which carries a user name or password", req.URL.Redacted())
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
		return nil, fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fail(fmt.Errorf("the server answered %s", resp.Status))
	}
	data, err := readLimited(resp.Body)
	if err != nil {
		return nil, fail(err)
	}
	return data, nil
}

// readLimited reads r to its end, refusing more than MaxSize bytes.
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

// unwrapPath drops the path from a *fs.PathError, whose message the caller
// already prefixes with the path.
func unwrapPath(err error) error {
	var perr *os.PathError
	if errors.As(err, &perr) {
		return perr.Err
	}
	return err
}
