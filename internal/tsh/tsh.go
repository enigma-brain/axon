// Package tsh holds everything axon does with Teleport's client, tsh:
// finding or installing it, the Teleport login, the application login and
// the check of the Teleport certificate.
//
// A tsh on PATH is used when the proxy supports its version, and is never
// replaced. Otherwise axon uses its own tsh under ~/.axon/tools/, installing
// it at the proxy's version from Teleport's archive after the user agrees.
package tsh

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/enigma-brain/axon/internal/axondir"
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

// ErrNotFound reports that there is no tsh on PATH and axon has installed
// none.
var ErrNotFound = errors.New("Teleport's client, tsh, is not installed. Run `axon login`")

// Find returns the path of the tsh on PATH, or else of axon's own. It makes
// no network request, asks nothing, installs nothing and does not check the
// version. It returns ErrNotFound when there is neither.
func Find(dir axondir.Dir) (string, error) {
	if path, err := exec.LookPath("tsh"); err == nil {
		return path, nil
	}
	tools, err := dir.ToolsDir()
	if err != nil {
		return "", err
	}
	own, _, ok, err := installed(tools)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrNotFound
	}
	return own, nil
}

// Ensure returns the path of a tsh whose version the Teleport proxy at proxy
// supports, installing one when none is present. It reads answers from in
// and writes prompts and progress to out.
//
// A tsh on PATH comes first; when the proxy does not support its version,
// Ensure stops and names both versions. axon's own tsh comes next, and is
// reinstalled at the proxy's version, without asking, when the proxy no
// longer supports it. With neither, Ensure asks before installing.
func Ensure(ctx context.Context, dir axondir.Dir, proxyAddr string, in io.Reader, out io.Writer) (tshPath string, err error) {
	if out == nil {
		out = io.Discard
	}
	if err := CheckProxy(proxyAddr); err != nil {
		return "", err
	}
	proxy, err := proxyVersion(ctx, proxyAddr)
	if err != nil {
		return "", err
	}

	if path, err := exec.LookPath("tsh"); err == nil {
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

	tools, err := dir.ToolsDir()
	if err != nil {
		return "", err
	}
	own, version, ok, err := installed(tools)
	if err != nil {
		return "", err
	}
	if ok && supported(version, proxy) {
		return own, nil
	}

	if !canInstall() {
		return "", fmt.Errorf("Teleport's client, tsh, is not on PATH. Install it from %s and run `axon login` again", InstallLink)
	}
	if ok {
		fmt.Fprintf(out, "Updating axon's tsh from %s to %s, the Teleport proxy's version\n", version, proxy)
	} else {
		fmt.Fprintln(out, "Teleport's client, tsh, is not installed.")
		if !confirm(in, out, fmt.Sprintf("Install tsh %s into ~/.axon/tools? [Y/n] ", proxy)) {
			return "", fmt.Errorf("tsh was not installed. Install it from %s and run `axon login` again", InstallLink)
		}
	}
	path, err := install(ctx, tools, proxy, out)
	if err != nil {
		return "", fmt.Errorf("%w. Install tsh from %s and run `axon login` again", err, InstallLink)
	}
	if ok {
		fmt.Fprintf(out, "Updated tsh from %s to %s\n", version, proxy)
	} else {
		fmt.Fprintf(out, "Installed tsh %s\n", proxy)
	}
	return path, nil
}

// confirm asks question until the answer is yes or no. An empty answer is
// yes. The end of input, or a nil in, is no.
func confirm(in io.Reader, out io.Writer, question string) bool {
	if in == nil {
		return false
	}
	r := bufio.NewReader(in)
	for {
		fmt.Fprint(out, question)
		line, err := r.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		// An answer cut off by the end of input is complete only when it
		// says something: a blank one is not the empty line that means yes.
		if err != nil && answer == "" {
			fmt.Fprintln(out)
			return false
		}
		switch answer {
		case "", "y", "yes":
			return true
		case "n", "no":
			return false
		}
		if err != nil {
			fmt.Fprintln(out)
			return false
		}
	}
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
