// Package apiserver makes the health request to the SkyPilot API server.
//
// axon login uses it to verify the identity the server reports and to read
// the server's version; the axon sky connection check uses it before a
// command runs.
package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/enigma-brain/axon/internal/termtext"
)

// APIVersion is the SkyPilot API version of the default client, sent with
// every health request. The server only checks that it is at least its own
// minimum compatible API version. It is raised whenever the default client
// version is raised; 56 is the API version of SkyPilot 0.13.0.
const APIVersion = 56

// timeout bounds one health request, from the connection to the last byte of
// the answer. A request that takes longer counts as unreachable.
var timeout = 10 * time.Second

const (
	healthPath           = "/api/health"
	headerAPIVersion     = "X-SkyPilot-API-Version"
	headerVersion        = "X-SkyPilot-Version"
	statusNeedsAuth      = "needs_auth"
	maxHealthBodyInBytes = 1 << 20
)

// Health is what the API server's health endpoint tells axon.
type Health struct {
	// User is the identity the server reports for the caller.
	User string
	// ServerVersion is the SkyPilot version the server runs.
	ServerVersion string
}

// Options are the inputs of one health request.
type Options struct {
	// ClientVersion is the version of axon's SkyPilot client, sent in the
	// X-SkyPilot-Version header. The server returns its own version only
	// when the request carries it.
	ClientVersion string
	// CookieFile is SkyPilot's cookie file, in Netscape format. The cookies
	// in it that match the endpoint are sent with the request. When empty,
	// or when the file does not exist, the request carries no cookies.
	CookieFile string
}

var (
	// ErrUnreachable reports that no answer came from the endpoint.
	ErrUnreachable = errors.New("the SkyPilot API server is unreachable")
	// ErrNotLoggedIn reports that the server answered without a login.
	ErrNotLoggedIn = errors.New("the SkyPilot API server answered without a login")
)

// healthResponse is the part of the health endpoint's JSON that axon reads.
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	User    *struct {
		Name string `json:"name"`
	} `json:"user"`
}

// Fetch makes one request to the health endpoint of the server at endpoint.
// The error wraps ErrUnreachable or ErrNotLoggedIn when one of them applies.
// With ErrNotLoggedIn, the returned Health carries the server's version when
// the server reported one.
func Fetch(ctx context.Context, endpoint string, opts Options) (Health, error) {
	target, err := url.Parse(strings.TrimRight(endpoint, "/") + healthPath)
	if err != nil || target.Host == "" {
		return Health{}, fmt.Errorf("the API server endpoint %q is not a URL", endpoint)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return Health{}, fmt.Errorf("build the health request: %w", err)
	}
	req.Header.Set(headerAPIVersion, fmt.Sprint(APIVersion))
	req.Header.Set(headerVersion, opts.ClientVersion)
	if opts.CookieFile != "" {
		cookies, err := readCookieFile(opts.CookieFile)
		if err != nil {
			return Health{}, err
		}
		for _, c := range matchCookies(cookies, target, time.Now()) {
			req.AddCookie(&http.Cookie{Name: c.name, Value: c.value})
		}
	}

	client := &http.Client{
		// A redirect is the login proxy sending the caller to its login page.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctxErr := context.Cause(ctx); ctxErr != nil && !errors.Is(ctxErr, context.DeadlineExceeded) {
			// The caller cancelled; that is not a fact about the server.
			return Health{}, ctxErr
		}
		return Health{}, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHealthBodyInBytes))
	if err != nil {
		return Health{}, fmt.Errorf("%w: read the health answer: %w", ErrUnreachable, err)
	}

	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400,
		resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusForbidden:
		return Health{ServerVersion: resp.Header.Get(headerVersion)},
			fmt.Errorf("%w (HTTP %d)", ErrNotLoggedIn, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return Health{}, fmt.Errorf("the health request got HTTP %d: %s", resp.StatusCode, snippet(body))
	}

	var hr healthResponse
	if err := json.Unmarshal(body, &hr); err != nil {
		return Health{}, fmt.Errorf("the health answer is not SkyPilot's: %s", snippet(body))
	}
	h := Health{ServerVersion: hr.Version}
	if h.ServerVersion == "" {
		h.ServerVersion = resp.Header.Get(headerVersion)
	}
	if hr.User != nil {
		h.User = strings.TrimSpace(hr.User.Name)
	}
	if hr.Status == statusNeedsAuth || h.User == "" {
		return Health{ServerVersion: h.ServerVersion}, ErrNotLoggedIn
	}
	return h, nil
}

// snippet returns the start of an answer's body, on one line and without
// control or format characters, for a message.
func snippet(body []byte) string {
	s := termtext.Clean(strings.Join(strings.Fields(string(body)), " "))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	if s == "" {
		s = "(empty body)"
	}
	return s
}
