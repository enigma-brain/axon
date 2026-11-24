package sky

import (
	"os/user"
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
	return skyPilotFile(home, getenv(cookieFileVar), filepath.Join(".sky", "cookies.txt"))
}

// SettingsFile returns the path of SkyPilot's user configuration file, where
// sky api login stores the endpoint, for the user whose home directory is
// home: the path in SKYPILOT_GLOBAL_CONFIG, as getenv reports it, when that
// is set, and ~/.sky/config.yaml otherwise.
func SettingsFile(home string, getenv func(string) string) string {
	return skyPilotFile(home, getenv(settingsFileVar), filepath.Join(".sky", "config.yaml"))
}

// skyPilotFile returns the file a SkyPilot variable names, p, or the file at
// def under home when p is empty. A leading "~", "~/" or "~name/" in p is
// expanded as SkyPilot expands it, and a relative path is made absolute.
func skyPilotFile(home, p, def string) string {
	switch {
	case p == "":
		return filepath.Join(home, def)
	case p == "~":
		return home
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	case strings.HasPrefix(p, "~"):
		// "~name" or "~name/rest" is that account's home directory. An
		// unknown account leaves the path as it is, as SkyPilot does.
		name, rest, _ := strings.Cut(p[1:], "/")
		if u, err := lookupUser(name); err == nil && u.HomeDir != "" {
			return filepath.Join(u.HomeDir, rest)
		}
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// lookupUser finds an account by name; tests replace it.
var lookupUser = user.Lookup
