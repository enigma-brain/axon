package sky

import (
	"os"
	"path/filepath"
	"strings"
)

// The variables that move SkyPilot's files.
const (
	cookieFileVar   = "SKYPILOT_API_COOKIE_FILE"
	settingsFileVar = "SKYPILOT_GLOBAL_CONFIG"
)

// CookieFile returns the path of the cookie file SkyPilot's client uses for
// the user whose home directory is home: the path in SKYPILOT_API_COOKIE_FILE,
// as getenv reports it, when that is set, and ~/.sky/cookies.txt otherwise.
func CookieFile(home string, getenv func(string) string) string {
	return defaults().cookieFile(home, getenv)
}

func (d deps) cookieFile(home string, getenv func(string) string) string {
	return d.skyPilotFile(home, getenv(cookieFileVar), filepath.Join(".sky", "cookies.txt"))
}

// SettingsFile returns the path of SkyPilot's user configuration file, where
// sky api login stores the endpoint, for the user whose home directory is
// home: the path in SKYPILOT_GLOBAL_CONFIG, as getenv reports it, when that
// is set, and ~/.sky/config.yaml otherwise.
func SettingsFile(home string, getenv func(string) string) string {
	return defaults().settingsFile(home, getenv)
}

func (d deps) settingsFile(home string, getenv func(string) string) string {
	return d.skyPilotFile(home, getenv(settingsFileVar), filepath.Join(".sky", "config.yaml"))
}

// skyPilotFile returns the file a SkyPilot variable names, p, or the file at
// def under home when p is empty. A leading "~", "~/" or "~name/" in p is
// expanded as SkyPilot expands it, and a relative path is put under the
// working directory. The rest of p is kept as it is, not cleaned: SkyPilot
// opens the path as written, and a ".." after a symbolic link leads where
// the link points.
func (d deps) skyPilotFile(home, p, def string) string {
	switch {
	case p == "":
		return filepath.Join(home, def)
	case p == "~" || strings.HasPrefix(p, "~/"):
		return underDir(home, p[1:])
	case strings.HasPrefix(p, "~"):
		// "~name" or "~name/rest" is that account's home directory. An
		// unknown account leaves the path as it is, as SkyPilot does.
		name, _, _ := strings.Cut(p[1:], "/")
		if u, err := d.lookupUser(name); err == nil && u.HomeDir != "" {
			return underDir(u.HomeDir, p[1+len(name):])
		}
	}
	if filepath.IsAbs(p) {
		return p
	}
	if cwd, err := os.Getwd(); err == nil {
		return underDir(cwd, "/"+p)
	}
	return p
}

// underDir returns dir followed by rest, which is empty or begins with "/",
// without cleaning either.
func underDir(dir, rest string) string {
	if rest == "" {
		return dir
	}
	return strings.TrimRight(dir, "/") + rest
}
