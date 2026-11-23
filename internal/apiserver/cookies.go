package apiserver

import (
	"bufio"
	"bytes"
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
		if c, ok := parseCookieLine(sc.Text()); ok {
			cookies = append(cookies, c)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read the SkyPilot cookie file: %w", err)
	}
	return cookies, nil
}

// parseCookieLine reads one line of a Netscape-format cookie file. It reports
// false for a blank line, a comment or a malformed line.
func parseCookieLine(line string) (cookie, bool) {
	line = strings.TrimRight(line, "\r\n")
	line = strings.TrimPrefix(line, httpOnlyPrefix)
	if line == "" || strings.HasPrefix(line, "#") {
		return cookie{}, false
	}
	fields := strings.Split(line, "\t")
	if len(fields) != 7 {
		return cookie{}, false
	}
	expires, err := strconv.ParseInt(fields[4], 10, 64)
	if fields[4] == "" {
		expires, err = 0, nil
	}
	if err != nil {
		return cookie{}, false
	}
	return cookie{
		domain:     strings.ToLower(fields[0]),
		subdomains: strings.EqualFold(fields[1], "TRUE"),
		path:       fields[2],
		secure:     strings.EqualFold(fields[3], "TRUE"),
		expires:    expires,
		name:       fields[5],
		value:      fields[6],
	}, true
}

// RemoveCookies removes from the Netscape-format cookie file at path every
// cookie for the server at endpoint: each cookie whose domain matches the
// endpoint's host and whose path covers the endpoint's path or lies below
// it, whatever its expiry and Secure flag. Every other line is kept as it
// was. It returns how many cookies it removed. A missing file, or one with
// no cookie for the server, is left as it is and is not an error.
//
// The file is replaced through a temporary file in the same directory, with
// the original file's permissions, so that a failure leaves it whole.
func RemoveCookies(path, endpoint string) (int, error) {
	target, err := url.Parse(endpoint)
	if err != nil || target.Hostname() == "" {
		return 0, fmt.Errorf("read the API server's address %q: not a URL with a host", endpoint)
	}
	host := strings.ToLower(target.Hostname())
	base := target.Path
	if base == "" {
		base = "/"
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read the SkyPilot cookie file: %w", err)
	}

	var kept bytes.Buffer
	removed := 0
	for _, line := range bytes.SplitAfter(data, []byte("\n")) {
		c, ok := parseCookieLine(string(line))
		if ok && domainMatches(c, host) && (pathMatches(c.path, base) || pathMatches(base, c.path)) {
			removed++
			continue
		}
		kept.Write(line)
	}
	if removed == 0 {
		return 0, nil
	}
	if err := replaceFile(path, kept.Bytes()); err != nil {
		return 0, fmt.Errorf("rewrite the SkyPilot cookie file %s: %w", path, err)
	}
	return removed, nil
}

// replaceFile replaces the file at path with data, keeping its permissions.
// It writes a temporary file beside it, syncs it, and renames that over it.
// A path that is a symbolic link is followed, so the link stays and the
// file it points to is replaced.
func replaceFile(path string, data []byte) error {
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // A no-op once the rename succeeds.
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
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
