package skyclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"
)

var (
	// stallTimeout bounds how long a download waits for its response, and
	// then for each further piece of its body. A download whose body keeps
	// arriving is never cut off, however long it takes; one that stops is.
	stallTimeout = 30 * time.Second
	// maxDownload bounds the size of a download. uv's release archives are
	// a fraction of it.
	maxDownload int64 = 64 << 20
)

// stallError is a download ended because no data arrived for after.
type stallError struct{ after time.Duration }

func (e *stallError) Error() string {
	return fmt.Sprintf("the download stopped because no data arrived for %s", inWords(e.after))
}

// Timeout reports that the download ran out of time.
func (e *stallError) Timeout() bool { return true }

// tooLargeError is a download refused because it exceeds max bytes.
type tooLargeError struct{ max int64 }

func (e *tooLargeError) Error() string {
	return fmt.Sprintf("the download was refused because it is larger than %d MiB", e.max>>20)
}

// download writes the body at url to w, reporting progress by quarters when
// the size is known, and returns the body's SHA-256 in hex. It ends when no
// data arrives for stallTimeout, and refuses a body larger than maxDownload.
func download(ctx context.Context, url string, w io.Writer, progress io.Writer) (string, error) {
	after, limit := stallTimeout, maxDownload
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stalled := &stallError{after: after}
	timer := time.AfterFunc(after, func() { cancel(stalled) })
	defer timer.Stop()
	// wrap reports a stall as such rather than as a cancellation.
	wrap := func(err error) error {
		if context.Cause(ctx) == error(stalled) {
			return stalled
		}
		return fmt.Errorf("GET %s: %w", url, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", wrap(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if resp.ContentLength > limit {
		return "", &tooLargeError{max: limit}
	}
	timer.Reset(after)

	h := sha256.New()
	dst := io.MultiWriter(w, h)
	if resp.ContentLength > 0 {
		dst = io.MultiWriter(dst, &quarterReporter{total: resp.ContentLength, out: progress})
	}
	body := &resettingReader{r: resp.Body, timer: timer, after: after}
	n, err := io.Copy(dst, io.LimitReader(body, limit+1))
	if err != nil {
		return "", wrap(err)
	}
	if n > limit {
		return "", &tooLargeError{max: limit}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

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

// quarterReporter writes the percentage done, on the current line, each time
// the bytes written to it pass a further quarter of total.
type quarterReporter struct {
	total, done int64
	reported    int
	out         io.Writer
}

func (q *quarterReporter) Write(p []byte) (int, error) {
	q.done += int64(len(p))
	for q.reported < 4 && q.done*4 >= q.total*int64(q.reported+1) {
		q.reported++
		fmt.Fprintf(q.out, " %d%%", q.reported*25)
	}
	return len(p), nil
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
