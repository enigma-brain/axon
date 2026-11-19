package login

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/enigma-brain/axon/internal/apiserver"
	"github.com/enigma-brain/axon/internal/devenv"
	"github.com/enigma-brain/axon/internal/orgfile"
)

// needsAuth is the status sky api login ends with when the server still sees
// no login, which happens when the cookie file holds an expired session that
// is sent ahead of the new one.
const needsAuth = "needs_auth"

// endpointVar is the variable sky api login refuses to run with.
const endpointVar = "SKYPILOT_API_SERVER_ENDPOINT"

// skyWaitDelay bounds how long login waits for sky's output after sky exits
// or is stopped.
const skyWaitDelay = 2 * time.Second

// connectDirect connects devenv on a direct pathway. It returns the
// server's health when the existing session is good; after a browser login it
// returns an empty Health and the identity check that follows asks the server.
func (l *login) connectDirect(ctx context.Context, p orgfile.Pathway, opts Options, sky, clientVersion string) (apiserver.Health, error) {
	endpoint := l.endpoint(p)
	h, err := l.health(ctx, endpoint, apiserver.Options{ClientVersion: clientVersion, CookieFile: l.cookieFile})
	switch {
	case err == nil:
		return h, nil
	case errors.Is(err, apiserver.ErrUnreachable):
		return apiserver.Health{}, errors.New(devenv.Unreachable(p, endpoint))
	case !errors.Is(err, apiserver.ErrNotLoggedIn):
		return apiserver.Health{}, fmt.Errorf("check the SkyPilot API server: %w. Run `axon login` again.", err)
	}

	sawNeedsAuth, err := l.browserLogin(ctx, opts, sky, endpoint)
	if err == nil && sawNeedsAuth {
		fmt.Fprintf(opts.Out, "devenv: the login ended with %s; removing %s and logging in once more\n", needsAuth, l.cookieFile)
		if err := os.Remove(l.cookieFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return apiserver.Health{}, fmt.Errorf("remove %s: %w. Remove it by hand and run `axon login` again.", l.cookieFile, err)
		}
		sawNeedsAuth, err = l.browserLogin(ctx, opts, sky, endpoint)
		if err == nil && sawNeedsAuth {
			err = fmt.Errorf("the SkyPilot login ended with %s twice. Run `axon login` again.", needsAuth)
		}
	}
	return apiserver.Health{}, err
}

// browserLogin runs sky api login against endpoint, which opens the login page
// in the user's default browser, and waits for it up to loginWait. It reports whether sky's
// output named the status needs_auth.
func (l *login) browserLogin(ctx context.Context, opts Options, sky, endpoint string) (sawNeedsAuth bool, err error) {
	env := withoutVars(l.environ(), endpointVar)
	// sky's output goes through a pipe; unbuffered, it reaches the terminal
	// as sky writes it.
	env = append(withoutVars(env, "PYTHONUNBUFFERED"), "PYTHONUNBUFFERED=1")

	ctx, cancel := context.WithTimeout(ctx, l.loginWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, sky, "api", "login", "-e", endpoint)
	cmd.Env = env
	cmd.WaitDelay = skyWaitDelay
	var seen scanWriter
	cmd.Stdin = opts.In
	cmd.Stdout = io.MultiWriter(opts.Out, &seen)
	cmd.Stderr = io.MultiWriter(opts.Err, &seen)

	fmt.Fprintf(opts.Out, "devenv: logging in to %s in your browser\n", endpoint)
	runErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return false, fmt.Errorf("the login was not completed in the browser within %s. %s", formatWait(l.loginWait), notCompletedHint)
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if seen.contains(needsAuth) {
		return true, nil
	}
	if runErr != nil {
		return false, fmt.Errorf("the SkyPilot login failed (%v). Run `axon login` again.", runErr)
	}
	return false, nil
}

// scanWriter keeps what is written to it, so the output can be searched once
// the command ends.
type scanWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *scanWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *scanWriter) contains(s string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return bytes.Contains(w.buf.Bytes(), []byte(s))
}

// withoutVars returns env without the variables named.
func withoutVars(env []string, names ...string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, n := range names {
			if name == n {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// formatWait writes d in words, such as "5 minutes".
func formatWait(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		if m := int(d / time.Minute); m != 1 {
			return fmt.Sprintf("%d minutes", m)
		}
		return "1 minute"
	}
	return d.String()
}
