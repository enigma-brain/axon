package netreason

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"syscall"
	"testing"
	"time"
)

// get requests address with a client that gives up after limit.
func get(address string, limit time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		resp.Body.Close()
	}
	return err
}

// The lookup is made up, so that no test asks a real resolver.
func TestNameThatDoesNotResolve(t *testing.T) {
	t.Parallel()
	err := &url.Error{Op: "Get", URL: "https://sky.wrong.example/api/health", Err: &net.OpError{
		Op: "dial", Net: "tcp",
		Err: &net.DNSError{Err: "no such host", Name: "sky.wrong.example", IsNotFound: true},
	}}
	if got := Short(fmt.Errorf("the SkyPilot API server is unreachable: %w", err)); got != "no such host" {
		t.Errorf("Short = %q, want %q", got, "no such host")
	}
}

func TestRefusedConnection(t *testing.T) {
	t.Parallel()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	err = get("http://"+addr+"/api/health", 5*time.Second)
	if err == nil {
		t.Fatal("the request to a closed port succeeded")
	}
	if got := Short(err); got != "connection refused" {
		t.Errorf("Short(%v) = %q, want %q", err, got, "connection refused")
	}
}

func TestTimeouts(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	deadline := get(srv.URL, 50*time.Millisecond)
	if deadline == nil {
		t.Fatal("the request to a silent server succeeded")
	}
	dial := &url.Error{Op: "Get", URL: "https://sky.example.org/api/health", Err: &net.OpError{
		Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ETIMEDOUT),
	}}
	cases := map[string]error{
		"deadline exceeded":     deadline,
		"bare deadline":         context.DeadlineExceeded,
		"dial i/o timeout":      &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded},
		"connect timed out":     dial,
		"wrapped with two %w":   fmt.Errorf("%w: %w", errors.New("unreachable"), deadline),
		"marked as a timeout":   fmt.Errorf("ping: %w", marked{}),
		"lookup that times out": &net.DNSError{Err: "i/o timeout", Name: "x", IsTimeout: true},
	}
	for name, err := range cases {
		if got := Short(err); got != TimedOut {
			t.Errorf("%s: Short(%v) = %q, want %q", name, err, got, TimedOut)
		}
	}
}

// marked is an error that says it is a timeout.
type marked struct{}

func (marked) Error() string { return "GET https://p/webapi/ping: no data for 30s" }
func (marked) Timeout() bool { return true }

func TestNoShorterForm(t *testing.T) {
	t.Parallel()
	cert := &url.Error{Op: "Get", URL: "https://sky.example.org/api/health", Err: x509.UnknownAuthorityError{}}
	cases := map[string]struct {
		err  error
		want string
	}{
		"plain":             {errors.New("boom"), "boom"},
		"certificate":       {cert, x509.UnknownAuthorityError{}.Error()},
		"joined, last kept": {fmt.Errorf("%w: read the answer: %w", errors.New("unreachable"), errors.New("unexpected EOF")), "unexpected EOF"},
		"not a timeout":     {fmt.Errorf("wrap: %w", unmarked{}), "slow, but not a timeout"},
	}
	for name, c := range cases {
		if got := Short(c.err); got != c.want {
			t.Errorf("%s: Short = %q, want %q", name, got, c.want)
		}
	}
	if got := Short(nil); got != "" {
		t.Errorf("Short(nil) = %q", got)
	}
}

type unmarked struct{}

func (unmarked) Error() string { return "slow, but not a timeout" }
func (unmarked) Timeout() bool { return false }
