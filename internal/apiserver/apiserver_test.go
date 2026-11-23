package apiserver

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func serve(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, body)
}

func TestLoggedIn(t *testing.T) {
	var gotPath, gotAPI, gotVersion string
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPI = r.Header.Get("X-SkyPilot-API-Version")
		gotVersion = r.Header.Get("X-SkyPilot-Version")
		writeJSON(w, `{"status":"healthy","api_version":"56","version":"0.13.0","commit":"abc","user":{"id":"x1","name":"ada@example.org"}}`)
	})
	h, err := Fetch(context.Background(), url+"/", Options{ClientVersion: "0.12.3"})
	if err != nil {
		t.Fatal(err)
	}
	if h != (Health{User: "ada@example.org", ServerVersion: "0.13.0"}) {
		t.Errorf("Health = %+v", h)
	}
	if gotPath != "/api/health" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAPI != "56" || gotVersion != "0.12.3" {
		t.Errorf("headers = %q, %q", gotAPI, gotVersion)
	}
}

func TestNeedsAuthCarriesServerVersion(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"status":"needs_auth","api_version":"56","version":"0.13.0"}`)
	})
	h, err := Fetch(context.Background(), url, Options{ClientVersion: "0.13.0"})
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("err = %v, want ErrNotLoggedIn", err)
	}
	if h.ServerVersion != "0.13.0" || h.User != "" {
		t.Errorf("Health = %+v", h)
	}
}

func TestHealthyWithoutUserIsNotLoggedIn(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"status":"healthy"}`)
	})
	if _, err := Fetch(context.Background(), url, Options{}); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("err = %v, want ErrNotLoggedIn", err)
	}
}

func TestRefusalsAreNotLoggedIn(t *testing.T) {
	for _, code := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusUnauthorized, http.StatusForbidden} {
		url := serve(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/health" {
				t.Errorf("followed a redirect to %s", r.URL.Path)
			}
			w.Header().Set("X-SkyPilot-Version", "0.13.0")
			if code >= 300 && code < 400 {
				w.Header().Set("Location", "/oauth2/sign_in")
			}
			w.WriteHeader(code)
		})
		h, err := Fetch(context.Background(), url, Options{})
		if !errors.Is(err, ErrNotLoggedIn) {
			t.Errorf("HTTP %d: err = %v, want ErrNotLoggedIn", code, err)
		}
		if h.ServerVersion != "0.13.0" {
			t.Errorf("HTTP %d: ServerVersion = %q", code, h.ServerVersion)
		}
	}
}

func TestOtherAnswersAreErrors(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"server error": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		},
		"not json": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "<html>hello</html>")
		},
	}
	for name, h := range cases {
		_, err := Fetch(context.Background(), serve(t, h), Options{})
		if err == nil || errors.Is(err, ErrNotLoggedIn) || errors.Is(err, ErrUnreachable) {
			t.Errorf("%s: err = %v, want a plain error", name, err)
		}
	}
}

func TestClosedPortIsUnreachable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	if _, err := Fetch(context.Background(), "http://"+addr, Options{}); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
}

func TestSlowServerIsUnreachable(t *testing.T) {
	old := timeout
	timeout = 50 * time.Millisecond
	t.Cleanup(func() { timeout = old })
	release := make(chan struct{})
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	if _, err := Fetch(context.Background(), url, Options{}); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
}

func TestCancelledIsNotUnreachable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {})
	_, err := Fetch(ctx, url, Options{})
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestBadEndpoint(t *testing.T) {
	if _, err := Fetch(context.Background(), "not a url", Options{}); err == nil {
		t.Fatal("Fetch succeeded with a bad endpoint")
	}
}

func TestCookiesSent(t *testing.T) {
	var got string
	srvURL := serve(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Cookie")
		writeJSON(w, `{"status":"healthy","version":"0.13.0","user":{"name":"ada@example.org"}}`)
	})
	u, _ := url.Parse(srvURL)
	host := u.Hostname()
	future := time.Now().Add(time.Hour).Unix()
	past := time.Now().Add(-time.Hour).Unix()
	file := filepath.Join(t.TempDir(), "cookies.txt")
	lines := []string{
		"# Netscape HTTP Cookie File",
		fmt.Sprintf("%s\tFALSE\t/\tFALSE\t%d\tsession\tgood", host, future),
		fmt.Sprintf("#HttpOnly_%s\tFALSE\t/\tFALSE\t0\thttponly\tyes", host),
		fmt.Sprintf("%s\tFALSE\t/\tFALSE\t%d\texpired\tno", host, past),
		fmt.Sprintf("%s\tFALSE\t/\tTRUE\t%d\tsecure\tno", host, future),
		fmt.Sprintf("other.example\tFALSE\t/\tFALSE\t%d\tother\tno", future),
		fmt.Sprintf("%s\tFALSE\t/elsewhere\tFALSE\t%d\tpath\tno", host, future),
		"malformed line",
	}
	if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Fetch(context.Background(), srvURL, Options{CookieFile: file}); err != nil {
		t.Fatal(err)
	}
	if got != "session=good; httponly=yes" {
		t.Errorf("Cookie header = %q", got)
	}
}

func TestMissingCookieFileSendsNone(t *testing.T) {
	var got string
	srvURL := serve(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Cookie")
		writeJSON(w, `{"status":"needs_auth","version":"0.13.0"}`)
	})
	_, err := Fetch(context.Background(), srvURL, Options{CookieFile: filepath.Join(t.TempDir(), "none.txt")})
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("err = %v", err)
	}
	if got != "" {
		t.Errorf("Cookie header = %q, want none", got)
	}
}

func TestMatchCookies(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	target, _ := url.Parse("https://sky.lab.example.org/api/health")
	cookies := []cookie{
		{domain: "sky.lab.example.org", path: "/", name: "exact"},
		{domain: ".example.org", path: "/", name: "dotted"},
		{domain: "lab.example.org", subdomains: true, path: "/", name: "subdomains"},
		{domain: "lab.example.org", path: "/", name: "parent-host-only"},
		{domain: "notexample.org", subdomains: true, path: "/", name: "suffix-trick"},
		{domain: "sky.lab.example.org", path: "/api", name: "api-path"},
		{domain: "sky.lab.example.org", path: "/ap", name: "partial-path"},
		{domain: "sky.lab.example.org", path: "/", secure: true, name: "secure"},
		{domain: "sky.lab.example.org", path: "/", expires: 999_999, name: "expired"},
		{domain: "sky.lab.example.org", path: "/", expires: 1_000_001, name: "live"},
	}
	var names []string
	for _, c := range matchCookies(cookies, target, now) {
		names = append(names, c.name)
	}
	want := "exact dotted subdomains api-path secure live"
	if got := strings.Join(names, " "); got != want {
		t.Errorf("matched %q, want %q", got, want)
	}
}

func TestCookieFile(t *testing.T) {
	env := func(v string) func(string) string {
		return func(name string) string {
			if name == "SKYPILOT_API_COOKIE_FILE" {
				return v
			}
			return ""
		}
	}
	cwd, _ := os.Getwd()
	old := lookupUser
	lookupUser = func(name string) (*user.User, error) {
		if name == "ada" {
			return &user.User{HomeDir: "/home/ada"}, nil
		}
		return nil, user.UnknownUserError(name)
	}
	t.Cleanup(func() { lookupUser = old })
	for v, want := range map[string]string{
		"":                 "/home/u/.sky/cookies.txt",
		"~/jar.txt":        "/home/u/jar.txt",
		"/tmp/cookies.txt": "/tmp/cookies.txt",
		"rel/cookies.txt":  filepath.Join(cwd, "rel/cookies.txt"),
		"~ada/.sky/c.txt":  "/home/ada/.sky/c.txt",
		"~ada":             "/home/ada",
		"~nobody/c.txt":    filepath.Join(cwd, "~nobody/c.txt"),
	} {
		if got := CookieFile("/home/u", env(v)); got != want {
			t.Errorf("SKYPILOT_API_COOKIE_FILE=%q: got %q, want %q", v, got, want)
		}
	}
}

// TestLiveServerBody parses the body a SkyPilot API server returns to a
// logged-in health request with both version headers.
func TestLiveServerBody(t *testing.T) {
	const body = `{"status":"healthy","api_version":"56","version":"0.13.0","version_on_disk":"0.13.0","commit":"b1431e5","basic_auth_enabled":false,"user":{"id":"13ce5100","name":"ada@example.org","password":null,"created_at":null,"user_type":"sso","preferred_workspace":null},"service_account_token_enabled":true,"ingress_basic_auth_enabled":false,"latest_version":"0.14.3","external_proxy_auth_enabled":true,"telemetry_enabled":true,"restrict_config_to_admins":true}`
	url := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SkyPilot-Version", "0.13.0")
		w.Header().Set("X-SkyPilot-API-Version", "56")
		writeJSON(w, body)
	})
	h, err := Fetch(context.Background(), url, Options{ClientVersion: "0.13.0"})
	if err != nil {
		t.Fatal(err)
	}
	if h != (Health{User: "ada@example.org", ServerVersion: "0.13.0"}) {
		t.Errorf("Health = %+v", h)
	}
}

// cookieLines is a cookie file with a header, a cookie for the server at
// https://sky.example.org, an HttpOnly one for it, a session cookie for it
// that has expired, and cookies for other servers.
var cookieLines = []string{
	"# Netscape HTTP Cookie File",
	"# This is a generated file! Do not edit.",
	"",
	"sky.example.org\tFALSE\t/\tTRUE\t0\t_oauth2_proxy\tsession",
	"#HttpOnly_sky.example.org\tFALSE\t/api\tTRUE\t4102444800\tcsrf\tx",
	"sky.example.org\tFALSE\t/\tFALSE\t1\told\texpired",
	"other.example.org\tFALSE\t/\tTRUE\t0\t_oauth2_proxy\tkeep",
	"#HttpOnly_.example.com\tTRUE\t/\tFALSE\t0\tsid\tkeep",
	"not a cookie line",
}

func writeCookies(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRemoveCookies(t *testing.T) {
	path := writeCookies(t, cookieLines...)
	n, err := RemoveCookies(path, "https://sky.example.org")
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("removed %d cookies, want 3", n)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		cookieLines[0], cookieLines[1], cookieLines[2],
		cookieLines[6], cookieLines[7], cookieLines[8],
	}, "\n") + "\n"
	if string(data) != want {
		t.Errorf("file:\n%s\nwant:\n%s", data, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("directory holds %d files, want only the cookie file", len(entries))
	}
}

func TestRemoveCookiesKeepsTheFileWhenTheLastCookieGoes(t *testing.T) {
	path := writeCookies(t, cookieLines[0], cookieLines[3])
	if n, err := RemoveCookies(path, "https://sky.example.org/"); err != nil || n != 1 {
		t.Fatalf("RemoveCookies = %d, %v", n, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != cookieLines[0]+"\n" {
		t.Errorf("file = %q, want the header alone", data)
	}
}

func TestRemoveCookiesWithNoCookieForTheServer(t *testing.T) {
	path := writeCookies(t, cookieLines[0], cookieLines[6])
	before, _ := os.Stat(path)
	if n, err := RemoveCookies(path, "https://sky.example.org"); err != nil || n != 0 {
		t.Fatalf("RemoveCookies = %d, %v", n, err)
	}
	after, _ := os.Stat(path)
	if !os.SameFile(before, after) {
		t.Error("the file was replaced with nothing to remove")
	}
}

func TestRemoveCookiesWithNoFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "none.txt")
	if n, err := RemoveCookies(path, "https://sky.example.org"); err != nil || n != 0 {
		t.Fatalf("RemoveCookies = %d, %v", n, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the file was created: %v", err)
	}
}

func TestRemoveCookiesWhenTheFileCannotBeRewritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes to a read-only directory")
	}
	path := writeCookies(t, cookieLines...)
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	_, err := RemoveCookies(path, "https://sky.example.org")
	if err == nil || !strings.Contains(err.Error(), "rewrite the SkyPilot cookie file "+path) {
		t.Fatalf("err = %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != strings.Join(cookieLines, "\n")+"\n" {
		t.Error("the file changed after a failed rewrite")
	}
}

func TestRemoveCookiesRefusesAnEndpointWithNoHost(t *testing.T) {
	if _, err := RemoveCookies(writeCookies(t, cookieLines...), "not a url"); err == nil {
		t.Fatal("no error")
	}
}
