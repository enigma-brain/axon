package sky

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

// fakeAccounts makes ada the one account lookupUser knows, with its home
// directory at /home/ada.
func fakeAccounts(t *testing.T) {
	t.Helper()
	old := lookupUser
	lookupUser = func(name string) (*user.User, error) {
		if name == "ada" {
			return &user.User{HomeDir: "/home/ada"}, nil
		}
		return nil, user.UnknownUserError(name)
	}
	t.Cleanup(func() { lookupUser = old })
}

// only returns a getenv that reports v for name and nothing else.
func only(name, v string) func(string) string {
	return func(n string) string {
		if n == name {
			return v
		}
		return ""
	}
}

func TestCookieFile(t *testing.T) {
	fakeAccounts(t)
	cwd, _ := os.Getwd()
	for v, want := range map[string]string{
		"":                 "/home/u/.sky/cookies.txt",
		"~/jar.txt":        "/home/u/jar.txt",
		"/tmp/cookies.txt": "/tmp/cookies.txt",
		"rel/cookies.txt":  filepath.Join(cwd, "rel/cookies.txt"),
		"~ada/.sky/c.txt":  "/home/ada/.sky/c.txt",
		"~ada":             "/home/ada",
		"~nobody/c.txt":    filepath.Join(cwd, "~nobody/c.txt"),
	} {
		if got := CookieFile("/home/u", only("SKYPILOT_API_COOKIE_FILE", v)); got != want {
			t.Errorf("SKYPILOT_API_COOKIE_FILE=%q: got %q, want %q", v, got, want)
		}
	}
}

// The settings file follows the cookie file's rule for a leading "~".
func TestSettingsFile(t *testing.T) {
	fakeAccounts(t)
	cwd, _ := os.Getwd()
	for v, want := range map[string]string{
		"":                 "/home/u/.sky/config.yaml",
		"~":                "/home/u",
		"~/sky.yaml":       "/home/u/sky.yaml",
		"/etc/sky.yaml":    "/etc/sky.yaml",
		"rel/sky.yaml":     filepath.Join(cwd, "rel/sky.yaml"),
		"~ada/.sky/x.yaml": "/home/ada/.sky/x.yaml",
		"~nobody/sky.yaml": filepath.Join(cwd, "~nobody/sky.yaml"),
	} {
		if got := SettingsFile("/home/u", only("SKYPILOT_GLOBAL_CONFIG", v)); got != want {
			t.Errorf("SKYPILOT_GLOBAL_CONFIG=%q: got %q, want %q", v, got, want)
		}
	}
}

// A ".." after a symbolic link leads where the link points, as it does
// when SkyPilot opens the path, for a relative path and for one under "~".
func TestSkyPilotFileKeepsDotDotAfterALink(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "srv", "sky", "sub")
	work := filepath.Join(root, "work")
	for _, d := range []string{sub, work} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	want := filepath.Join(root, "srv", "sky", "config.yaml")
	if err := os.WriteFile(want, []byte("api_server:\n  endpoint: http://127.0.0.1:46581\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sub, filepath.Join(work, "link")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)
	for _, v := range []string{"link/../config.yaml", "~/link/../config.yaml"} {
		got := SettingsFile(work, only("SKYPILOT_GLOBAL_CONFIG", v))
		if endpoint, err := storedEndpoint(got); err != nil || endpoint != "http://127.0.0.1:46581" {
			t.Errorf("SKYPILOT_GLOBAL_CONFIG=%q: %s holds %q, %v; want the endpoint in %s", v, got, endpoint, err, want)
		}
	}
}
