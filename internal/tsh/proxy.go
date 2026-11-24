package tsh

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// defaultPort is the port of a proxy address that names none.
const defaultPort = "443"

// splitProxy splits a proxy address, a host or host:port, into its host and
// port. The port is defaultPort when the address names none.
func splitProxy(proxy string) (host, port string) {
	if h, p, err := net.SplitHostPort(proxy); err == nil {
		return h, p
	}
	return proxy, defaultPort
}

// hostLabel is one dot-separated label of a host name: letters, digits and
// hyphens, at most 63, starting and ending with a letter or digit.
var hostLabel = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

// CheckProxy returns an error unless proxy is a Teleport proxy address: a
// host name of at most 253 characters, optionally followed by a colon and
// a port from 1 to 65535 written without leading zeros. It is passed to
// tsh as an argument and put in a URL, so it holds nothing else. It is the
// one rule for a proxy address, wherever axon takes one.
func CheckProxy(proxy string) error {
	bad := fmt.Errorf("%q is not a Teleport proxy address: it is a host or host:port", proxy)
	host := proxy
	if strings.Contains(proxy, ":") {
		h, p, err := net.SplitHostPort(proxy)
		if err != nil {
			return bad
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != p {
			return bad
		}
		host = h
	}
	if host == "" || len(host) > 253 {
		return bad
	}
	for _, label := range strings.Split(host, ".") {
		if !hostLabel.MatchString(label) {
			return bad
		}
	}
	return nil
}

// SameProxy reports whether the proxy addresses a and b name the same
// proxy: the same host, ignoring case, and the same port, 443 when one
// names none.
func SameProxy(a, b string) bool {
	ah, ap := splitProxy(a)
	bh, bp := splitProxy(b)
	return strings.EqualFold(ah, bh) && ap == bp
}

// CheckApp returns an error unless app is a Teleport application name that
// tsh cannot take for a flag: not empty, not starting with "-", and without
// white space.
func CheckApp(app string) error {
	if app == "" || strings.HasPrefix(app, "-") || strings.ContainsAny(app, " \t\r\n") {
		return fmt.Errorf("%q is not a Teleport application name", app)
	}
	return nil
}
