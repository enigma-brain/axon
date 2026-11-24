package tsh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/enigma-brain/axon/internal/interrupt"
	"github.com/enigma-brain/axon/internal/netreason"
)

// pingURL returns where the proxy at proxy, a host or host:port, reports
// its version. Tests replace it.
var pingURL = func(proxy string) string {
	return "https://" + proxy + "/webapi/ping"
}

// versionPattern accepts a Teleport release version such as 18.11.7 or
// 17.0.0-aa. It starts with a digit, so it is never "." or "..", and holds
// no path separator.
var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+[0-9A-Za-z.+-]*$`)

// proxyVersion reads the version the proxy at proxy reports.
func proxyVersion(ctx context.Context, proxy string) (string, error) {
	ping := pingURL(proxy)
	fail := func(err error) (string, error) {
		return "", fmt.Errorf("read the Teleport proxy's version at %s: %w", ping, err)
	}
	resp, err := get(ctx, ping)
	if err != nil {
		// An answer that is not OK, or a request the caller cancelled, is
		// not a proxy that can't be reached.
		var answered *statusError
		if errors.As(err, &answered) || ctx.Err() != nil {
			return fail(err)
		}
		return "", &UnreachableError{Proxy: proxy, Err: err}
	}
	defer resp.Body.Close()
	var reply struct {
		ServerVersion string `json:"server_version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&reply); err != nil {
		return fail(err)
	}
	if !versionPattern.MatchString(reply.ServerVersion) {
		return fail(fmt.Errorf("%q is not a release version", reply.ServerVersion))
	}
	return reply.ServerVersion, nil
}

// UnreachableError is a Teleport proxy that gave no answer. Its message
// names the proxy and the short reason, such as "no such host".
type UnreachableError struct {
	// Proxy is the proxy's address, a host or host:port.
	Proxy string
	// Err is the failure of the request.
	Err error
}

func (e *UnreachableError) Error() string {
	return fmt.Sprintf("can't reach the Teleport proxy at %s: %s", e.Proxy, netreason.Short(e.Err))
}

func (e *UnreachableError) Unwrap() error { return e.Err }

// clientVersion reads the version of the tsh at path. The command makes no
// network request. Under Teleport's managed client updates it reports the
// version that actually runs, which may be newer than the file at path.
func clientVersion(ctx context.Context, path string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, path, "version", "--client", "--format=json")
	interrupt.Stop(ctx, cmd, waitDelay)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("read the version of %s: %w: %s", path, err, msg)
		}
		return "", fmt.Errorf("read the version of %s: %w", path, err)
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
		return "", fmt.Errorf("read the version of %s: %w", path, err)
	}
	if !versionPattern.MatchString(v.Version) {
		return "", fmt.Errorf("read the version of %s: %q is not a release version", path, v.Version)
	}
	return v.Version, nil
}

// major returns the major version of a version that matches versionPattern.
func major(version string) int {
	m, _ := strconv.Atoi(version[:strings.IndexByte(version, '.')])
	return m
}

// supported reports whether a client at version works with a proxy at
// proxy: Teleport supports a client of the proxy's major version or one
// behind it.
func supported(version, proxy string) bool {
	c, p := major(version), major(proxy)
	return c == p || c == p-1
}

// Ping reports whether the Teleport proxy at proxy, a host or host:port,
// answers: it asks the proxy for its version, which needs no login and no
// tsh. The error says why it did not answer.
func Ping(ctx context.Context, proxy string) error {
	if err := CheckProxy(proxy); err != nil {
		return err
	}
	_, err := proxyVersion(ctx, proxy)
	return err
}
