package tsh

import (
	"fmt"
	"net"
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

// CheckProxy returns an error unless proxy is a Teleport proxy address: a
// host name, optionally followed by a colon and a port from 1 to 65535. It
// is passed to tsh as an argument and put in a URL, so it holds nothing
// else.
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
	if host == "" || host[0] == '-' || host[0] == '.' {
		return bad
	}
	for _, r := range host {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
			return bad
		}
	}
	return nil
}

// sameProxy reports whether the proxy addresses a and b name the same
// proxy: the same host, ignoring case, and the same port, 443 when one
// names none.
func sameProxy(a, b string) bool {
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
