package tsh

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/enigma-brain/axon/internal/termtext"
)

// stallTimeout bounds how long a request waits for its response, and then
// for each further piece of its body. A response whose body keeps arriving is
// never cut off, however long it takes; one that stops is.
var stallTimeout = 30 * time.Second

// get requests url and returns the response when its status is OK. The
// request is cancelled when the response, or any read of its body, waits
// longer than stallTimeout. Closing the body releases the timer.
func get(ctx context.Context, url string) (*http.Response, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	stalled := &stallError{url: url, after: stallTimeout}
	timer := time.AfterFunc(stallTimeout, func() { cancel(stalled) })
	stop := func() {
		timer.Stop()
		cancel(nil)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		stop()
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		stop()
		if context.Cause(ctx) == error(stalled) {
			return nil, stalled
		}
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		stop()
		return nil, &statusError{url: url, status: termtext.Clean(resp.Status)}
	}
	timer.Reset(stallTimeout)
	resp.Body = &stallBody{ReadCloser: resp.Body, ctx: ctx, stalled: stalled, timer: timer, stop: stop}
	return resp, nil
}

// statusError is an answer whose status is not OK: the server answered.
type statusError struct {
	url, status string
}

func (e *statusError) Error() string { return fmt.Sprintf("GET %s: %s", e.url, e.status) }

// stallError is a request cancelled because it waited longer than
// stallTimeout. It is a timeout.
type stallError struct {
	url   string
	after time.Duration
}

func (e *stallError) Error() string { return fmt.Sprintf("GET %s: no data for %s", e.url, e.after) }

// Timeout reports that the request ran out of time.
func (e *stallError) Timeout() bool { return true }

// stallBody restarts its request's stall timer on every read that returns
// data, and reports a stall as such rather than as a cancellation.
type stallBody struct {
	io.ReadCloser
	ctx     context.Context
	stalled *stallError
	timer   *time.Timer
	stop    func()
}

func (b *stallBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.timer.Reset(stallTimeout)
	}
	if err != nil && context.Cause(b.ctx) == error(b.stalled) {
		return n, b.stalled
	}
	return n, err
}

func (b *stallBody) Close() error {
	err := b.ReadCloser.Close()
	b.stop()
	return err
}
