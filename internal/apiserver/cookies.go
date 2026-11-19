package apiserver

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// cookie is one line of a Netscape-format cookie file.
type cookie struct {
	domain     string
	subdomains bool
	path       string
	secure     bool
	expires    int64 // Unix seconds; 0 for a session cookie.
	name       string
	value      string
}

// httpOnlyPrefix marks an HttpOnly cookie in curl's form of the format. Any
// other line that begins with "#" is a comment.
const httpOnlyPrefix = "#HttpOnly_"

// readCookieFile reads a Netscape-format cookie file, the format SkyPilot's
// client saves. A missing file holds no cookies; malformed lines are skipped.
func readCookieFile(path string) ([]cookie, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the SkyPilot cookie file: %w", err)
	}
	defer f.Close()

	var cookies []cookie
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		line = strings.TrimPrefix(line, httpOnlyPrefix)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 7 {
			continue
		}
		expires, err := strconv.ParseInt(fields[4], 10, 64)
		if fields[4] == "" {
			expires, err = 0, nil
		}
		if err != nil {
			continue
		}
		cookies = append(cookies, cookie{
			domain:     strings.ToLower(fields[0]),
			subdomains: strings.EqualFold(fields[1], "TRUE"),
			path:       fields[2],
			secure:     strings.EqualFold(fields[3], "TRUE"),
			expires:    expires,
			name:       fields[5],
			value:      fields[6],
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read the SkyPilot cookie file: %w", err)
	}
	return cookies, nil
}

// matchCookies returns the cookies a browser would send to target at now: the
// domain and path match, the cookie has not expired, and a Secure cookie goes
// only over HTTPS.
func matchCookies(cookies []cookie, target *url.URL, now time.Time) []cookie {
	host := strings.ToLower(target.Hostname())
	path := target.Path
	if path == "" {
		path = "/"
	}
	var out []cookie
	for _, c := range cookies {
		if c.expires != 0 && c.expires <= now.Unix() {
			continue
		}
		if c.secure && target.Scheme != "https" {
			continue
		}
		if !domainMatches(c, host) || !pathMatches(c.path, path) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func domainMatches(c cookie, host string) bool {
	d := strings.TrimPrefix(c.domain, ".")
	if host == d {
		return true
	}
	if c.subdomains || strings.HasPrefix(c.domain, ".") {
		return strings.HasSuffix(host, "."+d)
	}
	return false
}

func pathMatches(cookiePath, reqPath string) bool {
	if cookiePath == "" || cookiePath == "/" {
		return true
	}
	if !strings.HasPrefix(reqPath, cookiePath) {
		return false
	}
	return strings.HasSuffix(cookiePath, "/") || len(reqPath) == len(cookiePath) || reqPath[len(cookiePath)] == '/'
}

// cookieFileVar names the variable that moves SkyPilot's cookie file.
const cookieFileVar = "SKYPILOT_API_COOKIE_FILE"

// CookieFile returns the path of the cookie file SkyPilot's client uses for
// the user whose home directory is home: the path in SKYPILOT_API_COOKIE_FILE,
// as getenv reports it, when that is set, and ~/.sky/cookies.txt otherwise.
// A leading "~", "~/" or "~name/" is expanded as SkyPilot expands it.
func CookieFile(home string, getenv func(string) string) string {
	p := getenv(cookieFileVar)
	switch {
	case p == "":
		return filepath.Join(home, ".sky", "cookies.txt")
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
