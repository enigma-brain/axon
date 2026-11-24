package login

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/enigma-brain/axon/internal/apiserver"
	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/configfile"
	"github.com/enigma-brain/axon/internal/skyclient"
	"github.com/enigma-brain/axon/internal/tunnel"
)

// fakeSky is a sky executable that records each run and acts out the
// behavior given for it: one line per run in the file "script".
//
//	ok      the login succeeds: the fake server now reports a user
//	silent  sky exits 0 but the server still sees no login
//	needs   the login ends with the status needs_auth
//	fail    sky exits non-zero
//	hang    sky waits until it is stopped
const fakeSky = `#!/bin/sh
dir=$(dirname "$0")
n=$(cat "$dir/count" 2>/dev/null || echo 0)
n=$((n + 1))
echo "$n" > "$dir/count"
echo "$*" >> "$dir/args"
env > "$dir/env.$n"
behavior=$(sed -n "${n}p" "$dir/script" 2>/dev/null)
case "$behavior" in
silent) echo "Logged in" ;;
needs) echo "ValueError: Cannot log in API server at $4 (status: needs_auth)" >&2; exit 1 ;;
fail) echo "boom" >&2; exit 2 ;;
hang) exec sleep 30 ;;
*) touch "$dir/loggedin"; echo "Logged in" ;;
esac
# Like sky api login, store the endpoint in SkyPilot's configuration.
if [ -n "$FAKESKY_CONFIG" ]; then
	mkdir -p "$(dirname "$FAKESKY_CONFIG")"
	printf 'api_server:\n  endpoint: %s\n' "$4" > "$FAKESKY_CONFIG"
fi
`

// fakeClient is axon's SkyPilot client, without an install.
type fakeClient struct {
	version   string
	installed bool
	sky       string
	moves     []string // "from->to" for each Ensure that changed the version
	// paths, when set, gives the sky of each version; otherwise every
	// version's is sky.
	paths map[string]string
	// fail, when set, gives the error an install of each version returns.
	fail map[string]error
}

func (c *fakeClient) Version() (string, bool, error) { return c.version, c.installed, nil }

func (c *fakeClient) Ensure(_ context.Context, v string, _ io.Writer) (string, string, error) {
	if err := c.fail[v]; err != nil {
		return "", "", err
	}
	from := c.version
	if !c.installed {
		from = ""
	}
	if from != v {
		c.moves = append(c.moves, from+"->"+v)
	}
	c.version, c.installed = v, true
	return from, v, nil
}

func (c *fakeClient) Path() (string, error) {
	if p, ok := c.paths[c.version]; ok {
		return p, nil
	}
	return c.sky, nil
}

// exampleConfig is the config file the harness stores: a direct
// pathway, campus, and a teleport pathway, remote. Every detail is made up.
const exampleConfig = `format = 2
id = "example"

[[pathway]]
id = "campus"
kind = "direct"
name = "Campus"
description = "Single sign-on in a browser. Needs the campus network."
unreachable_hint = "Connect to the campus VPN and retry."

[pathway.sky]
endpoint = "https://sky.example.org"

[[pathway]]
id = "remote"
kind = "teleport"
name = "Remote"
description = "Works from anywhere."

[pathway.teleport]
proxy = "teleport.example.org"

[pathway.sky]
app = "skypilot"
`

// harness is one login against a fake sky and a fake API server.
type harness struct {
	t    *testing.T
	home string
	// directURL is the fake API server's URL, which stands for every
	// direct pathway's endpoint.
	directURL     string
	skyDir        string
	cookieFile    string
	serverVersion string
	user          string
	client        *fakeClient
	l             *login
	out, errOut   bytes.Buffer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, home: t.TempDir(), skyDir: t.TempDir(), serverVersion: "0.13.0", user: "ada@example.org"}
	h.cookieFile = filepath.Join(h.home, ".sky", "cookies.txt")
	sky := filepath.Join(h.skyDir, "sky")
	if err := os.WriteFile(sky, []byte(fakeSky), 0o755); err != nil {
		t.Fatal(err)
	}
	h.client = &fakeClient{version: "0.13.0", installed: true, sky: sky}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if h.loggedIn() {
			fmt.Fprintf(w, `{"status":"healthy","version":%q,"user":{"id":"x","name":%q}}`, h.serverVersion, h.user)
			return
		}
		fmt.Fprintf(w, `{"status":"needs_auth","version":%q}`, h.serverVersion)
	}))
	t.Cleanup(srv.Close)

	h.directURL = srv.URL

	dir := axondir.New(h.home)
	h.l = &login{
		dir:        dir,
		client:     h.client,
		health:     apiserver.Fetch,
		cookieFile: h.cookieFile,
		endpoint: func(p configfile.Pathway) string {
			if p.Kind == configfile.KindTeleport {
				return "http://127.0.0.1:" + strconv.Itoa(testPort)
			}
			return h.directURL
		},
		loginWait: 10 * time.Second,
		environ: func() []string {
			return []string{
				"PATH=" + os.Getenv("PATH"),
				"SKYPILOT_API_SERVER_ENDPOINT=http://elsewhere.example",
				"BROWSER=chromium",
				"PYTHONPATH=/elsewhere/lib",
				"PYTHONHOME=/elsewhere",
				"VIRTUAL_ENV=/elsewhere/venv",
				"CONDA_PREFIX=/elsewhere/conda",
				"UV_INDEX_URL=https://index.example.org",
			}
		},
		readConfig: configfile.Read,
		// The tests never reach the real terminal.
		openTerminal: func() (io.ReadCloser, error) { return nil, errors.New("no terminal in tests") },
		teleport: teleport{
			newTunnel: func(stateDir, tshPath, proxy, app string) *tunnel.Tunnel {
				return tunnel.NewOnPort(stateDir, tshPath, proxy, app, testPort)
			},
			skyConfig: filepath.Join(h.home, ".sky", "config.yaml"),
		},
	}
	h.store(exampleConfig)
	return h
}

// store adds data as a config file.
func (h *harness) store(data string) {
	h.t.Helper()
	src, err := configfile.ParseSource("-", "")
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := configfile.NewStore(axondir.New(h.home)).Put([]byte(data), configfile.PutOptions{Origin: configfile.FromSource(src)}); err != nil {
		h.t.Fatal(err)
	}
}

// script sets what sky does on each run.
func (h *harness) script(runs ...string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.skyDir, "script"), []byte(strings.Join(runs, "\n")+"\n"), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// logIn makes the fake server report a user, as after a browser login.
func (h *harness) logIn() {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.skyDir, "loggedin"), nil, 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) loggedIn() bool {
	_, err := os.Stat(filepath.Join(h.skyDir, "loggedin"))
	return err == nil
}

// skyRuns returns how many times sky ran.
func (h *harness) skyRuns() int {
	data, err := os.ReadFile(filepath.Join(h.skyDir, "count"))
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return n
}

// assertNoPythonVars fails the test when env holds a variable that points
// Python elsewhere, or lacks the harness's UV_ variable, which is kept.
func assertNoPythonVars(t *testing.T, env map[string]string) {
	t.Helper()
	for _, k := range []string{"PYTHONPATH", "PYTHONHOME", "VIRTUAL_ENV", "CONDA_PREFIX"} {
		if v, ok := env[k]; ok {
			t.Errorf("sky ran with %s=%s", k, v)
		}
	}
	if env["UV_INDEX_URL"] != "https://index.example.org" {
		t.Errorf("sky ran without the user's UV_INDEX_URL")
	}
}

// skyEnv returns the environment of sky's nth run.
func (h *harness) skyEnv(n int) map[string]string {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.skyDir, "env."+strconv.Itoa(n)))
	if err != nil {
		h.t.Fatal(err)
	}
	env := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			env[k] = v
		}
	}
	return env
}

func (h *harness) run(p string) error {
	return h.l.run(context.Background(), Options{
		Pathway: p,
		In:      strings.NewReader(""),
		Out:     &h.out,
		Err:     &h.errOut,
	})
}

func (h *harness) email() string {
	e, _, _ := axondir.New(h.home).Email()
	return e
}

func TestDirectFirstLogin(t *testing.T) {
	h := newHarness(t)
	h.script("ok")
	if err := h.run("campus"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if n := h.skyRuns(); n != 1 {
		t.Fatalf("sky ran %d times, want 1", n)
	}
	args, _ := os.ReadFile(filepath.Join(h.skyDir, "args"))
	if want := "api login -e " + h.directURL + "\n"; string(args) != want {
		t.Errorf("sky args = %q, want %q", args, want)
	}
	env := h.skyEnv(1)
	if v, ok := env["SKYPILOT_API_SERVER_ENDPOINT"]; ok {
		t.Errorf("sky ran with SKYPILOT_API_SERVER_ENDPOINT=%q", v)
	}
	if env["BROWSER"] != "chromium" {
		t.Errorf("BROWSER = %q, want the user's own", env["BROWSER"])
	}
	assertNoPythonVars(t, env)
	for _, word := range []string{"Firefox", "Chrome", "Chromium"} {
		if strings.Contains(h.out.String(), word) {
			t.Errorf("output names %s:\n%s", word, h.out.String())
		}
	}
	if h.email() != "ada@example.org" {
		t.Errorf("email = %q", h.email())
	}
	if !strings.Contains(h.out.String(), "sky: connected as ada@example.org (SkyPilot 0.13.0)") {
		t.Errorf("output:\n%s", h.out.String())
	}
	if p, _, _ := axondir.New(h.home).Pathway(); p != "campus" {
		t.Errorf("stored pathway = %q", p)
	}
}

func TestDirectIdempotent(t *testing.T) {
	h := newHarness(t)
	h.logIn()
	if err := h.run("campus"); err != nil {
		t.Fatal(err)
	}
	emailPath := filepath.Join(h.home, ".axon", "state", "email")
	before, err := os.Stat(emailPath)
	if err != nil {
		t.Fatal(err)
	}
	configBefore, _ := os.ReadFile(axondir.New(h.home).SettingsPath())
	h.out.Reset()

	// The second login uses the stored pathway.
	if err := h.run(""); err != nil {
		t.Fatal(err)
	}
	if n := h.skyRuns(); n != 0 {
		t.Errorf("sky ran %d times, want 0", n)
	}
	if len(h.client.moves) != 0 {
		t.Errorf("client moved: %v", h.client.moves)
	}
	after, _ := os.Stat(emailPath)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("the email file was rewritten")
	}
	configAfter, _ := os.ReadFile(axondir.New(h.home).SettingsPath())
	if !bytes.Equal(configBefore, configAfter) {
		t.Error("the configuration changed")
	}
	if got := h.out.String(); got != "sky: connected as ada@example.org (SkyPilot 0.13.0)\n" {
		t.Errorf("output = %q", got)
	}
}

func TestDirectUnreachable(t *testing.T) {
	h := newHarness(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	h.directURL = "http://" + addr

	err = h.run("campus")
	want := "connect sky: Can't reach " + addr + ". Connect to the campus VPN and retry."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if n := h.skyRuns(); n != 0 {
		t.Errorf("sky ran %d times; no browser may open", n)
	}
}

func TestDirectNeedsAuthRetriesOnce(t *testing.T) {
	h := newHarness(t)
	if err := os.MkdirAll(filepath.Dir(h.cookieFile), 0o700); err != nil {
		t.Fatal(err)
	}
	// The fake server's address is on 127.0.0.1; the other server's
	// cookie is not for it.
	const otherCookie = "other.example.org\tFALSE\t/\tTRUE\t0\t_oauth2_proxy\tkeep\n"
	cookies := "# Netscape HTTP Cookie File\n" +
		"127.0.0.1\tFALSE\t/\tFALSE\t0\t_oauth2_proxy\tstale\n" +
		otherCookie
	if err := os.WriteFile(h.cookieFile, []byte(cookies), 0o600); err != nil {
		t.Fatal(err)
	}
	h.script("needs", "ok")
	if err := h.run("campus"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if n := h.skyRuns(); n != 2 {
		t.Errorf("sky ran %d times, want 2", n)
	}
	data, err := os.ReadFile(h.cookieFile)
	if err != nil {
		t.Fatalf("the cookie file is gone: %v", err)
	}
	if want := "# Netscape HTTP Cookie File\n" + otherCookie; string(data) != want {
		t.Errorf("cookie file =\n%s\nwant only the other server's cookie:\n%s", data, want)
	}
	if want := "removing the cookies of " + h.directURL + " from " + h.cookieFile; !strings.Contains(h.out.String(), want) {
		t.Errorf("output lacks %q:\n%s", want, h.out.String())
	}
	if !strings.Contains(h.errOut.String(), "needs_auth") {
		t.Errorf("sky's error output was not forwarded: %q", h.errOut.String())
	}
}

func TestDirectNeedsAuthRetriesWithNoCookieFile(t *testing.T) {
	h := newHarness(t)
	h.script("needs", "ok")
	if err := h.run("campus"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if n := h.skyRuns(); n != 2 {
		t.Errorf("sky ran %d times, want 2", n)
	}
}

func TestDirectCookieRemovalFailureSaysWhatToDo(t *testing.T) {
	h := newHarness(t)
	// The server's cookie is in a directory that cannot be written, so
	// the file can be read but not rewritten without it.
	skyDir := filepath.Dir(h.cookieFile)
	if err := os.MkdirAll(skyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.cookieFile, []byte("127.0.0.1\tFALSE\t/\tFALSE\t0\t_oauth2_proxy\tstale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(skyDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(skyDir, 0o700) })
	// A process that may write any directory, such as root, is not stopped
	// by the mode, so the failure cannot be made here.
	if probe, err := os.CreateTemp(skyDir, "probe"); err == nil {
		probe.Close()
		os.Remove(probe.Name())
		t.Skip("the directory stays writable for this user")
	}
	h.script("needs", "ok")
	err := h.run("campus")
	want := "remove the cookies of " + h.directURL + " from " + h.cookieFile + ": "
	if err == nil || !strings.Contains(err.Error(), want) ||
		!strings.HasSuffix(err.Error(), ". Remove that file by hand and run `axon login` again.") {
		t.Fatalf("err = %v, want one starting %q that says to remove the file", err, want)
	}
	if n := h.skyRuns(); n != 1 {
		t.Errorf("sky ran %d times, want 1", n)
	}
}

// An address that holds needs_auth, such as the login page's, is not the
// status, so no retry follows.
func TestDirectNeedsAuthInAnAddressIsNotTheStatus(t *testing.T) {
	h := newHarness(t)
	h.script("address")
	if err := os.WriteFile(filepath.Join(h.skyDir, "sky"), []byte(strings.Replace(fakeSky,
		"silent) echo",
		"address) echo \"Open https://sky.example.org/auth/needs_auth?state=needs_auth\"; touch \"$dir/loggedin\" ;;\nsilent) echo", 1)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.run("campus"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if n := h.skyRuns(); n != 1 {
		t.Errorf("sky ran %d times, want 1", n)
	}
}

func TestHasWord(t *testing.T) {
	for text, want := range map[string]bool{
		"needs_auth": true,
		// What sky api login prints when a login ends with needs_auth: the
		// ValueError raised in _show_logged_in_message, in
		// sky/client/sdk.py of skypilot 0.13.0, printed without a traceback.
		"ValueError: Cannot log in API server at https://sky.example.org (status: needs_auth)\n": true,
		"Failed to log in (status: needs_auth)":                                                  true,
		"status: \x1b[31mneeds_auth\x1b[0m":                                                      true,
		"\x1b[1;31mneeds_auth\x1b[0m)":                                                           true,
		"(status:\x1b[0mneeds_auth\x1b[39m)":                                                     true,
		"https://sky.example.org/\x1b[0mx/needs_auth":                                            false,
		"https://sky.example.org/\x1b[0mneeds_auth":                                              false,
		"?status=\x1b[1mneeds_auth\x1b[0m":                                                       false,
		"Open https://sky.example.org/auth:needs_auth":                                           false,
		"(status:needs_auth)":                                                                    true,
		"see https://sky.example.org (status: needs_auth)":                                       true,
		"status needs_auth.":                                                                     true,
		`{"status": "needs_auth"}`:                                                               true,
		"status:needs_auth\n":                                                                    true,
		"https://sky.example.org/needs_auth":                                                     false,
		"https://sky.example.org/?s=needs_auth":                                                  false,
		"x_needs_auth":                                                                           false,
		"needs_auth_soon":                                                                        false,
		"needs_authorization":                                                                    false,
		"/needs_auth then (needs_auth)":                                                          true,
		"":                                                                                       false,
	} {
		if got := hasWord(text, needsAuth); got != want {
			t.Errorf("hasWord(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestDirectNeedsAuthTwice(t *testing.T) {
	h := newHarness(t)
	h.script("needs", "needs")
	err := h.run("campus")
	if err == nil || !strings.Contains(err.Error(), "needs_auth twice") {
		t.Fatalf("err = %v", err)
	}
	if n := h.skyRuns(); n != 2 {
		t.Errorf("sky ran %d times, want 2", n)
	}
}

func TestDirectLoginFails(t *testing.T) {
	h := newHarness(t)
	h.script("fail")
	err := h.run("campus")
	if err == nil || !strings.HasPrefix(err.Error(), "connect sky: the SkyPilot login failed") {
		t.Fatalf("err = %v", err)
	}
	if n := h.skyRuns(); n != 1 {
		t.Errorf("sky ran %d times, want 1", n)
	}
}

func TestDirectSilentFailureCaughtByIdentityCheck(t *testing.T) {
	h := newHarness(t)
	h.script("silent")
	err := h.run("campus")
	if err == nil || !strings.HasPrefix(err.Error(), "connect sky: verify the login:") ||
		!errors.Is(err, apiserver.ErrNotLoggedIn) {
		t.Fatalf("err = %v", err)
	}
	if h.email() != "" {
		t.Errorf("recorded email %q without a login", h.email())
	}
}

func TestDirectLeavesBrowserUnset(t *testing.T) {
	h := newHarness(t)
	h.l.environ = func() []string { return []string{"PATH=" + os.Getenv("PATH")} }
	h.script("ok")
	if err := h.run("campus"); err != nil {
		t.Fatal(err)
	}
	if v, ok := h.skyEnv(1)["BROWSER"]; ok {
		t.Errorf("sky ran with BROWSER=%q; login chooses no browser", v)
	}
}

func TestDirectBrowserWaitEnds(t *testing.T) {
	h := newHarness(t)
	h.l.loginWait = 300 * time.Millisecond
	h.script("hang")
	start := time.Now()
	err := h.run("campus")
	want := "connect sky: the login was not completed in the browser within 300ms. Run `axon login` again."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("login took %s after the wait ended", d)
	}
}

func TestVersionSync(t *testing.T) {
	h := newHarness(t)
	h.client.version = "0.12.3"
	h.logIn()
	if err := h.run("campus"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.client.moves, " "); got != "0.12.3->0.13.0" {
		t.Errorf("client moves = %q", got)
	}
	out := h.out.String()
	if !strings.Contains(out, "sky: SkyPilot client 0.12.3 -> 0.13.0") ||
		!strings.Contains(out, "connected as ada@example.org (SkyPilot 0.13.0)") {
		t.Errorf("output:\n%s", out)
	}
}

func TestClientInstalledAtDefault(t *testing.T) {
	h := newHarness(t)
	h.client.installed, h.client.version = false, ""
	h.serverVersion = skyclient.DefaultVersion
	h.logIn()
	if err := h.run("campus"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.client.moves, " "); got != "->"+skyclient.DefaultVersion {
		t.Errorf("client moves = %q", got)
	}
}

// errNoBuild is an install error that no download fixes, as skyclient
// returns it: the installer's reason, matching skyclient.ErrUnavailable.
type errNoBuild struct{}

func (errNoBuild) Error() string        { return "uv pip: no pre-built package for this platform" }
func (errNoBuild) Is(target error) bool { return target == skyclient.ErrUnavailable }

func TestClientInstallFailureMessages(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		network bool
	}{
		{"failed download", errors.New("download uv 0.13.7: GET example: 502 Bad Gateway"), true},
		{"no pre-built package", errNoBuild{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.client.installed, h.client.version = false, ""
			h.client.fail = map[string]error{skyclient.DefaultVersion: tc.err}
			err := h.run("campus")
			if err == nil || !strings.Contains(err.Error(), "install the SkyPilot client: "+tc.err.Error()) {
				t.Fatalf("err = %v", err)
			}
			if got := strings.Contains(err.Error(), "Check your network"); got != tc.network {
				t.Errorf("err = %q; mentions the network: %v, want %v", err, got, tc.network)
			}
			if !tc.network && !strings.HasSuffix(err.Error(), tc.err.Error()) {
				t.Errorf("err = %q, want it to end with the installer's reason", err)
			}
		})
	}
}

func TestClientUpdateFailureMessages(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		network bool
	}{
		{"failed download", errors.New("install SkyPilot 0.13.0: uv pip: exit status 2: error: connection reset"), true},
		{"no pre-built package", errNoBuild{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.client.version = "0.12.3"
			h.client.fail = map[string]error{"0.13.0": tc.err}
			h.logIn()
			err := h.run("campus")
			if err == nil || !strings.Contains(err.Error(), "update the SkyPilot client from 0.12.3 to 0.13.0: "+tc.err.Error()) {
				t.Fatalf("err = %v", err)
			}
			if got := strings.Contains(err.Error(), "Check your network"); got != tc.network {
				t.Errorf("err = %q; mentions the network: %v, want %v", err, got, tc.network)
			}
			if !tc.network && !strings.HasSuffix(err.Error(), tc.err.Error()) {
				t.Errorf("err = %q, want it to end with the installer's reason", err)
			}
		})
	}
}

func TestFormatWait(t *testing.T) {
	for d, want := range map[time.Duration]string{
		5 * time.Minute:        "5 minutes",
		time.Minute:            "1 minute",
		300 * time.Millisecond: "300ms",
	} {
		if got := formatWait(d); got != want {
			t.Errorf("formatWait(%s) = %q, want %q", d, got, want)
		}
	}
	if loginWait != 5*time.Minute {
		t.Errorf("loginWait = %s, want 5 minutes", loginWait)
	}
}

// A config file update that stopped with the stored file missing leaves
// the previous copy beside it, which the next login puts back and reports.
func TestLoginPutsBackAnInterruptedUpdate(t *testing.T) {
	h := newHarness(t)
	h.script("ok")
	path := configfile.NewStore(axondir.New(h.home)).Path("example")
	if err := os.Rename(path, path+".previous"); err != nil {
		t.Fatal(err)
	}
	if err := h.run("campus"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if want := fmt.Sprintf(msgPutBack, path); !strings.Contains(h.out.String(), want) {
		t.Errorf("output does not say the file was put back:\n%s", h.out.String())
	}
	if got, _ := os.ReadFile(path); string(got) != exampleConfig {
		t.Errorf("stored file = %q, want the previous copy", got)
	}
	if _, err := os.Stat(path + ".previous"); !os.IsNotExist(err) {
		t.Errorf("the previous copy was left: %v", err)
	}
}

func TestLoginReadsSkyPilotsSettingsFile(t *testing.T) {
	for v, want := range map[string]string{
		"":              "/home/ada/.sky/config.yaml",
		"/etc/sky.yaml": "/etc/sky.yaml",
	} {
		getenv := func(k string) string {
			if k == "SKYPILOT_GLOBAL_CONFIG" {
				return v
			}
			return ""
		}
		if got := newLogin("/home/ada", getenv).teleport.skyConfig; got != want {
			t.Errorf("SKYPILOT_GLOBAL_CONFIG=%q: login reads %q, want %q", v, got, want)
		}
	}
}
