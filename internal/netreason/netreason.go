// Package netreason turns the error of a failed network request into the
// short reason a person reads after "Can't reach <address>:".
//
// Go wraps a network failure in layers that each repeat the request's
// method and address, such as `Get "https://host/path": dial tcp: lookup
// host: no such host`. The message that prints the reason already names the
// address, so only the innermost part is kept.
package netreason

import (
	"context"
	"errors"
	"net"
	"syscall"
)

// TimedOut is the reason for every request that ran out of time, whether Go
// reports it as an i/o timeout or as an exceeded deadline.
const TimedOut = "timed out"

// timeout is an error that can tell whether it is a timeout, as net.Error
// and url.Error do.
type timeout interface{ Timeout() bool }

// Short returns the short reason err gives:
//
//   - "timed out" for a timeout;
//   - the reason of a failed name lookup, such as "no such host";
//   - the system's reason, such as "connection refused";
//   - otherwise the innermost error err wraps, as it is.
//
// Of errors joined in one, such as by fmt.Errorf with two %w verbs, the last
// is followed, since a wrapper's cause comes after its label.
func Short(err error) string {
	if err == nil {
		return ""
	}
	var t timeout
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &t) && t.Timeout() {
		return TimedOut
	}
	var dns *net.DNSError
	if errors.As(err, &dns) && dns.Err != "" {
		return dns.Err
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno.Error()
	}
	return innermost(err).Error()
}

// innermost follows err's chain of wrapped errors to its end.
func innermost(err error) error {
	for {
		var next error
		switch e := err.(type) {
		case interface{ Unwrap() error }:
			next = e.Unwrap()
		case interface{ Unwrap() []error }:
			if errs := e.Unwrap(); len(errs) > 0 {
				next = errs[len(errs)-1]
			}
		}
		if next == nil {
			return err
		}
		err = next
	}
}
