package login

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/enigma-brain/axon/internal/apiserver"
	"github.com/enigma-brain/axon/internal/configfile"
	"github.com/enigma-brain/axon/internal/interrupt"
	"github.com/enigma-brain/axon/internal/sky"
	"github.com/enigma-brain/axon/internal/skyclient"
)

// loginWait is how long login waits for the browser login to complete. It
// matches the window in which sky api login polls the server for the result.
const loginWait = 5 * time.Minute

// notCompletedHint is the next action login gives when the wait ends.
const notCompletedHint = "Run `axon login` again."

// needsAuth is the status sky api login ends with when the server still sees
// no login, which happens when the cookie file holds an expired session that
// is sent ahead of the new one.
const needsAuth = "needs_auth"

// endpointVar is the variable sky api login refuses to run with.
const endpointVar = "SKYPILOT_API_SERVER_ENDPOINT"

// skyWaitDelay bounds how long login waits for sky to end after an
// interrupt, and for sky's output after sky exits or is stopped.
const skyWaitDelay = 2 * time.Second

// connectDirect connects the sky group on a direct pathway. It returns the
// server's health when the existing session is good; after a browser login it
// returns an empty Health and the identity check that follows asks the server.
func (l *login) connectDirect(ctx context.Context, p configfile.Pathway, opts Options, skyPath, clientVersion string) (apiserver.Health, error) {
	endpoint := l.endpoint(p)
	h, err := l.health(ctx, endpoint, apiserver.Options{ClientVersion: clientVersion, CookieFile: l.cookieFile})
	switch {
	case err == nil:
		return h, nil
	case errors.Is(err, apiserver.ErrUnreachable):
		return apiserver.Health{}, errors.New(sky.Unreachable(p, endpoint))
	case !errors.Is(err, apiserver.ErrNotLoggedIn):
		return apiserver.Health{}, fmt.Errorf("check the SkyPilot API server: %w. Run `axon login` again.", err)
	}

	sawNeedsAuth, err := l.browserLogin(ctx, opts, skyPath, endpoint)
	if err == nil && sawNeedsAuth {
		fmt.Fprintf(opts.Out, "sky: the login ended with %s; removing the cookies of %s from %s and logging in once more\n", needsAuth, endpoint, l.cookieFile)
		if _, err := apiserver.RemoveCookies(l.cookieFile, endpoint); err != nil {
			return apiserver.Health{}, fmt.Errorf("remove the cookies of %s from %s: %w. Remove that file by hand and run `axon login` again.", endpoint, l.cookieFile, err)
		}
		sawNeedsAuth, err = l.browserLogin(ctx, opts, skyPath, endpoint)
		if err == nil && sawNeedsAuth {
			err = fmt.Errorf("the SkyPilot login ended with %s twice. Run `axon login` again.", needsAuth)
		}
	}
	return apiserver.Health{}, err
}

// browserLogin runs sky api login against endpoint, which opens the login page
// in the user's default browser, and waits for it up to loginWait. It reports whether sky's
// output named the status needs_auth.
func (l *login) browserLogin(ctx context.Context, opts Options, skyPath, endpoint string) (sawNeedsAuth bool, err error) {
	env := skyEnv(l.environ())
	// sky's output goes through a pipe; unbuffered, it reaches the terminal
	// as sky writes it.
	env = append(withoutVars(env, "PYTHONUNBUFFERED"), "PYTHONUNBUFFERED=1")

	ctx, cancel := context.WithTimeout(ctx, l.loginWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, skyPath, "api", "login", "-e", endpoint)
	cmd.Env = env
	interrupt.Stop(ctx, cmd, skyWaitDelay)
	var seen scanWriter
	cmd.Stdin = opts.In
	cmd.Stdout = io.MultiWriter(opts.Out, &seen)
	cmd.Stderr = io.MultiWriter(opts.Err, &seen)

	fmt.Fprintf(opts.Out, "sky: logging in to %s in your browser\n", endpoint)
	runErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return false, fmt.Errorf("the login was not completed in the browser within %s. %s", formatWait(l.loginWait), notCompletedHint)
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if seen.containsWord(needsAuth) {
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

// containsWord reports whether what was written holds word as a word of its
// own: not inside a longer word, and not inside an address such as a URL.
func (w *scanWriter) containsWord(word string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return hasWord(w.buf.String(), word)
}

// hasWord reports whether text holds word with a word's edges on both sides.
// Before the word comes the start of text, white space, or one of ( [ " ' ` :
// and after it the end of text, white space, or one of ) ] " ' ` . , ; : so that
// "(status: needs_auth)" holds it, and "https://host/needs_auth",
// "?status=needs_auth" and "x_needs_auth" do not. A match inside an address,
// a run of text without white space that holds "://", never counts, so
// "https://host/auth:needs_auth" does not hold it. Terminal color and style
// sequences are removed first, so colored output is read as plain text and a
// sequence never makes an edge of its own.
func hasWord(text, word string) bool {
	text = ansiEscape.ReplaceAllString(text, "")
	for i := 0; ; {
		j := strings.Index(text[i:], word)
		if j < 0 {
			return false
		}
		start := i + j
		end := start + len(word)
		if (start == 0 || isWordEdge(text[start-1], "([\"'`:")) &&
			(end == len(text) || isWordEdge(text[end], ")]\"'`.,;:")) &&
			!strings.Contains(tokenAround(text, start, end), "://") {
			return true
		}
		i = start + 1
	}
}

// tokenAround returns the run of text without white space that holds
// text[start:end].
func tokenAround(text string, start, end int) string {
	from := strings.LastIndexAny(text[:start], " \t\n\r") + 1
	to := len(text)
	if k := strings.IndexAny(text[end:], " \t\n\r"); k >= 0 {
		to = end + k
	}
	return text[from:to]
}

// ansiEscape matches a terminal control sequence, such as one that sets a
// color: ESC [, parameters, and a final letter.
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]")

// isWordEdge reports whether c is white space or one of punct.
func isWordEdge(c byte, punct string) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || strings.IndexByte(punct, c) >= 0
}

// skyEnv returns the environment sky api login and sky api logout run with:
// env without the endpoint variable, which both refuse to run with, and
// without the variables that point Python elsewhere.
func skyEnv(env []string) []string {
	return withoutVars(skyclient.ClientEnv(env), endpointVar)
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
