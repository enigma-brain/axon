// Package tsh holds everything axon does with Teleport's client, tsh:
// finding it on PATH, the Teleport login, the application login, the
// logout and the check of the Teleport certificate.
//
// tsh is a prerequisite of a teleport pathway: axon uses the tsh on PATH,
// never installs one, and never replaces it.
package tsh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"time"
)

// InstallLink is Teleport's page on installing its client.
const InstallLink = "https://goteleport.com/docs/installation/"

// The terminal tsh runs with during the logins, which can open a browser
// and prompt. Tests replace them.
var (
	stdin  io.Reader = os.Stdin
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

// now is the clock the certificate is checked against.
var now = time.Now

// ErrNotFound reports that there is no tsh on PATH.
var ErrNotFound = fmt.Errorf("this pathway needs Teleport's client, tsh, and no tsh was found on PATH. Install it from %s and run `axon login` again.", InstallLink)

// Find returns the path of the tsh on PATH. It makes no network request
// and does not check the version. It returns ErrNotFound when there is
// none.
func Find() (string, error) {
	path, err := exec.LookPath("tsh")
	if err != nil {
		return "", ErrNotFound
	}
	return path, nil
}

// Ensure returns the path of the tsh on PATH when the Teleport proxy at
// proxyAddr supports its version. It returns ErrNotFound when there is no
// tsh on PATH, and stops naming both versions when the proxy does not
// support the one on PATH.
func Ensure(ctx context.Context, proxyAddr string) (string, error) {
	if err := CheckProxy(proxyAddr); err != nil {
		return "", err
	}
	path, err := Find()
	if err != nil {
		return "", err
	}
	proxy, err := proxyVersion(ctx, proxyAddr)
	if err != nil {
		return "", err
	}
	version, err := clientVersion(ctx, path)
	if err != nil {
		return "", err
	}
	if !supported(version, proxy) {
		return "", fmt.Errorf("tsh %s at %s does not work with the Teleport proxy %s, which runs %s and supports tsh %d and %d. Update tsh: %s",
			version, path, proxyAddr, proxy, major(proxy), major(proxy)-1, InstallLink)
	}
	return path, nil
}

// Login runs the Teleport login against the proxy at proxy.
func Login(ctx context.Context, tshPath, proxy string) error {
	if err := CheckProxy(proxy); err != nil {
		return err
	}
	if err := runInTerminal(ctx, tshPath, "login", "--proxy="+proxy); err != nil {
		return fmt.Errorf("Teleport login (tsh login --proxy=%s): %w", proxy, err)
	}
	return nil
}

// AppLogin issues a fresh certificate for the Teleport application app,
// served by the proxy at proxy.
func AppLogin(ctx context.Context, tshPath, proxy, app string) error {
	if err := CheckProxy(proxy); err != nil {
		return err
	}
	if err := CheckApp(app); err != nil {
		return fmt.Errorf("Teleport application login: %w", err)
	}
	if err := runInTerminal(ctx, tshPath, "--proxy="+proxy, "apps", "login", app); err != nil {
		return fmt.Errorf("Teleport application login (tsh --proxy=%s apps login %s): %w", proxy, app, err)
	}
	return nil
}

// runInTerminal runs tsh with args on axon's own terminal.
func runInTerminal(ctx context.Context, tshPath string, args ...string) error {
	cmd := exec.CommandContext(ctx, tshPath, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// SessionValid reports whether the Teleport certificate for the proxy at
// proxy is still valid.
//
// It is valid when tsh's active profile is for proxy and its expiry is in
// the future. When tsh reports no login, by a non-zero exit, the result is
// false with no error.
func SessionValid(ctx context.Context, tshPath, proxy string) (bool, error) {
	if err := CheckProxy(proxy); err != nil {
		return false, err
	}
	cmd := exec.CommandContext(ctx, tshPath, "status", "--format=json")
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		// A tsh killed because ctx ended also exits non-zero; that is not
		// tsh reporting no login.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, fmt.Errorf("check the Teleport certificate (tsh status): %w", ctxErr)
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check the Teleport certificate (tsh status): %w", err)
	}
	var status struct {
		Active *struct {
			ProfileURL string    `json:"profile_url"`
			ValidUntil time.Time `json:"valid_until"`
		} `json:"active"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return false, fmt.Errorf("check the Teleport certificate (tsh status): %w", err)
	}
	if status.Active == nil {
		return false, nil
	}
	u, err := url.Parse(status.Active.ProfileURL)
	if err != nil || !sameProxy(u.Host, proxy) {
		return false, nil
	}
	return status.Active.ValidUntil.After(now()), nil
}
