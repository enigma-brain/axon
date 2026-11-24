package tsh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeTSH returns a tsh that reports version. It appends the arguments of
// every other command to $FAKETSH_LOG and exits with $FAKETSH_EXIT, when
// set. For status it prints $FAKETSH_STATUS, or exits 1 when that is unset,
// as tsh does with no login. It uses only shell builtins, since the tests
// run it with a PATH that holds nothing else.
func fakeTSH(version string) string {
	return `#!/bin/sh
case "$1" in
version)
	echo '{"version": "` + version + `", "gitref": "fake"}'
	exit 0
	;;
status)
	if [ -z "$FAKETSH_STATUS" ]; then echo "ERROR: Not logged in." >&2; exit 1; fi
	printf '%s\n' "$FAKETSH_STATUS"
	exit 0
	;;
esac
echo "$*" >> "$FAKETSH_LOG"
if [ -n "$FAKETSH_EXIT" ]; then echo "tsh failed" >&2; exit "$FAKETSH_EXIT"; fi
`
}

// testProxy is the made-up Teleport proxy the tests pass. The fixture's
// pingURL sends its requests to a local fake.
const testProxy = "teleport.example.org"

// fixedURL returns a pingURL that sends every proxy's request to url.
func fixedURL(url string) func(string) string {
	return func(string) string { return url }
}

// fixture is a fake proxy, an empty home and a PATH that holds only a
// directory the test controls.
type fixture struct {
	t    *testing.T
	home string
	bin  string

	mu    sync.Mutex
	proxy string
	// pinged holds the proxy of every version request.
	pinged []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		t:     t,
		home:  t.TempDir(),
		bin:   t.TempDir(),
		proxy: "18.11.7",
	}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)

	setVar(t, &pingURL, func(proxy string) string {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.pinged = append(f.pinged, proxy)
		return srv.URL + "/webapi/ping"
	})
	t.Setenv("HOME", f.home)
	t.Setenv("PATH", f.bin)
	t.Setenv("FAKETSH_LOG", filepath.Join(t.TempDir(), "log"))
	return f
}

func setVar[T any](t *testing.T, v *T, value T) {
	old := *v
	*v = value
	t.Cleanup(func() { *v = old })
}

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	proxy := f.proxy
	f.mu.Unlock()
	if r.URL.Path == "/webapi/ping" {
		fmt.Fprintf(w, `{"auth": {"type": "saml"}, "server_version": %q, "min_client_version": "17.0.0-aa"}`, proxy)
		return
	}
	http.NotFound(w, r)
}

// putOnPath writes a fake tsh reporting version into the PATH directory.
func (f *fixture) putOnPath(version string) string {
	path := filepath.Join(f.bin, "tsh")
	if err := os.WriteFile(path, []byte(fakeTSH(version)), 0o755); err != nil {
		f.t.Fatal(err)
	}
	return path
}

// putOwn writes a tsh where an earlier axon installed its own, under
// ~/.axon/tools/tsh, and returns its path.
func (f *fixture) putOwn(version string) string {
	path := filepath.Join(f.home, ".axon", "tools", "tsh", version, "tsh")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fakeTSH(version)), 0o755); err != nil {
		f.t.Fatal(err)
	}
	return path
}

func TestPathTSHInRangeIsUsed(t *testing.T) {
	for _, version := range []string{"18.11.7", "18.0.1", "17.4.2"} {
		t.Run(version, func(t *testing.T) {
			f := newFixture(t)
			want := f.putOnPath(version)
			before, _ := os.ReadFile(want)

			got, err := Ensure(context.Background(), testProxy)
			if err != nil {
				t.Fatalf("Ensure: %v", err)
			}
			if got != want {
				t.Errorf("Ensure returned %s, want %s", got, want)
			}
			after, _ := os.ReadFile(want)
			if !bytes.Equal(before, after) {
				t.Error("the tsh on PATH was changed")
			}
		})
	}
}

// A tsh behind the proxy's range is to be updated; one ahead of it is to be
// replaced by an older one. Either message names both versions, the path,
// the major versions the proxy accepts and the install link.
func TestPathTSHOutOfRangeStops(t *testing.T) {
	for version, want := range map[string]string{
		"16.4.0": "tsh 16.4.0 at %s does not work with the Teleport proxy " + testProxy + ", which runs 18.11.7 and supports tsh 18 and 17. Update tsh: " + InstallLink,
		"19.0.0": "tsh 19.0.0 at %s is newer than the Teleport proxy " + testProxy + " supports: it runs 18.11.7 and accepts tsh 18 or 17. Install an older tsh: " + InstallLink,
	} {
		t.Run(version, func(t *testing.T) {
			f := newFixture(t)
			path := f.putOnPath(version)

			_, err := Ensure(context.Background(), testProxy)
			if err == nil {
				t.Fatal("Ensure succeeded, want an error")
			}
			if want := fmt.Sprintf(want, path); err.Error() != want {
				t.Errorf("error = %q, want %q", err, want)
			}
		})
	}
}

// With no tsh on PATH, Ensure stops before any request, and names tsh as
// the pathway's prerequisite with Teleport's install link. A tsh an earlier
// axon installed under ~/.axon/tools is neither used nor deleted.
func TestNoTSHOnPathStops(t *testing.T) {
	for name, own := range map[string]bool{"nothing": false, "an earlier axon's tsh": true} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			var ownPath string
			if own {
				ownPath = f.putOwn("18.11.7")
			}
			path, err := Ensure(context.Background(), testProxy)
			if !errors.Is(err, ErrNotFound) || path != "" {
				t.Fatalf("Ensure = %q, %v; want ErrNotFound", path, err)
			}
			want := "this pathway needs Teleport's client, tsh, and no tsh was found on PATH. Install it from https://goteleport.com/docs/installation/ and run `axon login` again."
			if err.Error() != want {
				t.Errorf("error = %q, want %q", err, want)
			}
			if len(f.pinged) != 0 {
				t.Errorf("the version of %v was requested", f.pinged)
			}
			if own {
				if _, err := os.Stat(ownPath); err != nil {
					t.Errorf("the earlier axon's tsh is gone: %v", err)
				}
			}
			if got := readLog(t); got != "" {
				t.Errorf("tsh ran with %q", got)
			}
		})
	}
}

func TestStalledProxyStops(t *testing.T) {
	f := newFixture(t)
	f.putOnPath("18.11.7")
	setVar(t, &stallTimeout, 200*time.Millisecond)
	release := make(chan struct{})
	stall := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	// Cleanups run last first: the handlers are released before the server
	// closes, since Close waits for them.
	t.Cleanup(stall.Close)
	t.Cleanup(func() { close(release) })
	pingURL = fixedURL(stall.URL + "/webapi/ping")

	_, err := Ensure(context.Background(), testProxy)
	if want := "can't reach the Teleport proxy at " + testProxy + ": timed out"; err == nil || err.Error() != want {
		t.Errorf("Ensure returned %v, want %q", err, want)
	}
}

func TestProxyUnreachable(t *testing.T) {
	f := newFixture(t)
	f.putOnPath("18.11.7")
	pingURL = fixedURL("http://127.0.0.1:1/webapi/ping")
	_, err := Ensure(context.Background(), testProxy)
	var unreachable *UnreachableError
	if want := "can't reach the Teleport proxy at " + testProxy + ": connection refused"; err == nil || err.Error() != want || !errors.As(err, &unreachable) {
		t.Fatalf("Ensure returned %v, want %q", err, want)
	}
}

// A proxy that answers, but not with its version, is not one that can't be
// reached.
func TestProxyAnswersWithAnError(t *testing.T) {
	f := newFixture(t)
	f.putOnPath("18.11.7")
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	pingURL = fixedURL(srv.URL + "/webapi/ping")
	_, err := Ensure(context.Background(), testProxy)
	var unreachable *UnreachableError
	if err == nil || errors.As(err, &unreachable) || !strings.Contains(err.Error(), "read the Teleport proxy's version at "+srv.URL+"/webapi/ping: GET "+srv.URL+"/webapi/ping: 404 Not Found") {
		t.Fatalf("Ensure returned %v, want the step and the status named", err)
	}
}

func TestCancelledPingIsNotUnreachable(t *testing.T) {
	newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Ping(ctx, testProxy)
	var unreachable *UnreachableError
	if !errors.Is(err, context.Canceled) || errors.As(err, &unreachable) {
		t.Fatalf("Ping returned %v, want context.Canceled", err)
	}
}

func readLog(t *testing.T) string {
	data, _ := os.ReadFile(os.Getenv("FAKETSH_LOG"))
	return string(data)
}

func TestLogin(t *testing.T) {
	f := newFixture(t)
	path := f.putOnPath("18.11.7")
	for _, proxy := range []string{testProxy, "teleport.example.org:3080"} {
		os.Remove(os.Getenv("FAKETSH_LOG"))
		if err := Login(context.Background(), path, proxy); err != nil {
			t.Fatalf("Login: %v", err)
		}
		if got, want := readLog(t), "login --proxy="+proxy+"\n"; got != want {
			t.Errorf("tsh ran with %q, want %q", got, want)
		}
	}

	t.Setenv("FAKETSH_EXIT", "3")
	err := Login(context.Background(), path, testProxy)
	if err == nil || !strings.HasPrefix(err.Error(), "Teleport login (tsh login --proxy=teleport.example.org): exit status 3") {
		t.Errorf("Login returned %v, want the step named", err)
	}
}

func TestAppLogin(t *testing.T) {
	f := newFixture(t)
	path := f.putOnPath("18.11.7")
	for _, proxy := range []string{testProxy, "teleport.example.org:3080"} {
		os.Remove(os.Getenv("FAKETSH_LOG"))
		if err := AppLogin(context.Background(), path, proxy, "skypilot"); err != nil {
			t.Fatalf("AppLogin: %v", err)
		}
		if got, want := readLog(t), "--proxy="+proxy+" apps login skypilot\n"; got != want {
			t.Errorf("tsh ran with %q, want %q", got, want)
		}
	}

	t.Setenv("FAKETSH_EXIT", "1")
	err := AppLogin(context.Background(), path, testProxy, "skypilot")
	if err == nil || !strings.HasPrefix(err.Error(), "Teleport application login (tsh --proxy=teleport.example.org apps login skypilot): exit status 1") {
		t.Errorf("AppLogin returned %v, want the step named", err)
	}
	for _, app := range []string{"", "--proxy=x", "dev env"} {
		if err := AppLogin(context.Background(), path, testProxy, app); err == nil {
			t.Errorf("AppLogin accepted %q as the application name", app)
		}
	}
}

func TestBadProxyIsRefused(t *testing.T) {
	f := newFixture(t)
	path := f.putOnPath("18.11.7")
	for _, proxy := range []string{"", "-x", "--proxy=x", ".example.org", "teleport.example.org/path", "https://teleport.example.org",
		"teleport.example.org:", "teleport.example.org:0", "teleport.example.org:65536", "teleport.example.org:08", "teleport.example.org:http",
		"user@teleport.example.org", "teleport example.org", "[::1]:443"} {
		t.Run(proxy, func(t *testing.T) {
			if err := Login(context.Background(), path, proxy); err == nil {
				t.Error("Login accepted it")
			}
			if err := AppLogin(context.Background(), path, proxy, "skypilot"); err == nil {
				t.Error("AppLogin accepted it")
			}
			if _, err := SessionValid(context.Background(), path, proxy); err == nil {
				t.Error("SessionValid accepted it")
			}
			if _, err := Ensure(context.Background(), proxy); err == nil {
				t.Error("Ensure accepted it")
			}
		})
	}
	if got := readLog(t); got != "" {
		t.Errorf("tsh ran with %q", got)
	}
	if len(f.pinged) != 0 {
		t.Errorf("the version of %v was requested", f.pinged)
	}
}

func TestVersionIsReadFromTheProxyPassed(t *testing.T) {
	for _, proxy := range []string{testProxy, "teleport.example.org:3080"} {
		t.Run(proxy, func(t *testing.T) {
			f := newFixture(t)
			f.putOnPath("18.11.7")
			if _, err := Ensure(context.Background(), proxy); err != nil {
				t.Fatal(err)
			}
			if len(f.pinged) != 1 || f.pinged[0] != proxy {
				t.Errorf("the version was requested of %v, want [%s]", f.pinged, proxy)
			}
		})
	}
}

func TestPingURL(t *testing.T) {
	for proxy, want := range map[string]string{
		"teleport.example.org":      "https://teleport.example.org/webapi/ping",
		"teleport.example.org:3080": "https://teleport.example.org:3080/webapi/ping",
	} {
		if got := pingURL(proxy); got != want {
			t.Errorf("pingURL(%q) = %q, want %q", proxy, got, want)
		}
	}
}

func TestSessionValid(t *testing.T) {
	f := newFixture(t)
	path := f.putOnPath("18.11.7")
	setVar(t, &now, func() time.Time { return time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC) })

	status := func(url, until string) string {
		return fmt.Sprintf(`{"active": {"profile_url": %q, "cluster": "teleport.example.org", "valid_until": %q}, "profiles": []}`, url, until)
	}
	const later = "2026-11-19T23:00:00Z"
	cases := map[string]struct {
		proxy  string
		status string
		want   bool
	}{
		"valid":                        {testProxy, status("https://teleport.example.org:443", "2026-11-19T05:04:57-08:00"), true},
		"expired":                      {testProxy, status("https://teleport.example.org:443", "2026-11-19T03:59:59-08:00"), false},
		"other proxy":                  {testProxy, status("https://other.example.org:443", later), false},
		"same host, other port":        {testProxy, status("https://teleport.example.org:3080", later), false},
		"profile without a port":       {testProxy, status("https://teleport.example.org", later), true},
		"proxy with port 443":          {"teleport.example.org:443", status("https://teleport.example.org:443", later), true},
		"proxy with port 443, bare":    {"teleport.example.org:443", status("https://teleport.example.org", later), true},
		"proxy with another port":      {"teleport.example.org:3080", status("https://teleport.example.org:3080", later), true},
		"proxy with another port, 443": {"teleport.example.org:3080", status("https://teleport.example.org:443", later), false},
		"host in another case":         {"Teleport.Example.org", status("https://teleport.example.org:443", later), true},
		"no active":                    {testProxy, `{"profiles": []}`, false},
		"not logged in":                {testProxy, "", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("FAKETSH_STATUS", c.status)
			got, err := SessionValid(context.Background(), path, c.proxy)
			if err != nil {
				t.Fatalf("SessionValid: %v", err)
			}
			if got != c.want {
				t.Errorf("SessionValid = %v, want %v", got, c.want)
			}
		})
	}

	t.Setenv("FAKETSH_STATUS", "not json")
	if _, err := SessionValid(context.Background(), path, testProxy); err == nil {
		t.Error("SessionValid accepted output that is not JSON")
	}
	if _, err := SessionValid(context.Background(), filepath.Join(f.bin, "missing"), testProxy); err == nil {
		t.Error("SessionValid accepted a tsh that does not exist")
	}
}

func TestFind(t *testing.T) {
	t.Run("on PATH", func(t *testing.T) {
		f := newFixture(t)
		want := f.putOnPath("16.0.0")
		pingURL = fixedURL("http://127.0.0.1:1/webapi/ping")
		got, err := Find()
		if err != nil || got != want {
			t.Errorf("Find = %s, %v; want %s, even out of range and offline", got, err, want)
		}
	})
	t.Run("an earlier axon's tsh only", func(t *testing.T) {
		f := newFixture(t)
		f.putOwn("18.11.7")
		got, err := Find()
		if !errors.Is(err, ErrNotFound) || got != "" {
			t.Errorf("Find = %q, %v; want ErrNotFound", got, err)
		}
	})
	t.Run("none", func(t *testing.T) {
		newFixture(t)
		got, err := Find()
		if !errors.Is(err, ErrNotFound) || got != "" {
			t.Errorf("Find = %q, %v; want ErrNotFound", got, err)
		}
	})
}

func TestSessionValidCancelled(t *testing.T) {
	f := newFixture(t)
	// A tsh that never answers, killed when the context ends.
	path := filepath.Join(f.bin, "slow-tsh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nwhile :; do :; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	ok, err := SessionValid(ctx, path, testProxy)
	if ok || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("SessionValid = %v, %v; want an error carrying the deadline, not \"no login\"", ok, err)
	}
}

func TestLogout(t *testing.T) {
	f := newFixture(t)
	path := f.putOnPath("18.11.7")
	if err := Logout(context.Background(), path, testProxy); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if got, want := readLog(t), "logout --proxy="+testProxy+"\n"; got != want {
		t.Errorf("tsh ran with %q, want %q", got, want)
	}
	t.Setenv("FAKETSH_EXIT", "1")
	err := Logout(context.Background(), path, testProxy)
	if err == nil || !strings.HasPrefix(err.Error(), "tsh logout --proxy=teleport.example.org: exit status 1: tsh failed") {
		t.Errorf("Logout returned %v, want the command and tsh's output", err)
	}
	if err := Logout(context.Background(), path, "--proxy=x"); err == nil {
		t.Error("Logout accepted a bad proxy")
	}
}

// A tsh logout that exits without ending the session is reported.
func TestLogoutThatEndsNoSession(t *testing.T) {
	f := newFixture(t)
	path := f.putOnPath("18.11.7")
	t.Setenv("FAKETSH_STATUS", fmt.Sprintf(`{"active": {"profile_url": "https://teleport.example.org:443", "valid_until": %q}}`,
		time.Now().Add(time.Hour).UTC().Format(time.RFC3339)))
	err := Logout(context.Background(), path, testProxy)
	if !errors.Is(err, ErrStillLoggedIn) || !strings.Contains(err.Error(), "tsh logout --proxy=teleport.example.org --user=<user>") {
		t.Errorf("Logout returned %v, want ErrStillLoggedIn with the command to run", err)
	}
}

// A tsh found only through a relative entry of PATH is an error, not an
// absent tsh, and is not run.
func TestFindRefusesARelativePathEntry(t *testing.T) {
	f := newFixture(t)
	f.putOnPath("18.11.7")
	t.Chdir(f.bin)
	t.Setenv("PATH", ".")
	path, err := Find()
	if err == nil || errors.Is(err, ErrNotFound) || path != "" {
		t.Errorf("Find = %q, %v; want an error that is not ErrNotFound", path, err)
	}
}

func TestPing(t *testing.T) {
	f := newFixture(t)
	// No tsh on PATH: the ping does not need one.
	if err := Ping(context.Background(), testProxy); err != nil {
		t.Errorf("Ping = %v", err)
	}
	if len(f.pinged) != 1 || f.pinged[0] != testProxy {
		t.Errorf("pinged %v", f.pinged)
	}
	pingURL = fixedURL("http://127.0.0.1:1/webapi/ping")
	if err := Ping(context.Background(), testProxy); err == nil {
		t.Error("Ping of a proxy that does not answer succeeded")
	}
	if err := Ping(context.Background(), "bad host"); err == nil {
		t.Error("Ping of a bad address succeeded")
	}
}
