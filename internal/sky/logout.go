package sky

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/enigma-brain/axon/internal/interrupt"
)

// LogoutOutcome is what Logout did.
type LogoutOutcome int

const (
	// LoggedOut is a logout sky api logout carried out.
	LoggedOut LogoutOutcome = iota + 1
	// NoClient is a logout skipped because the client is not installed.
	NoClient
	// NoLogin is a logout skipped because the client stores no endpoint.
	NoLogin
)

// skyLogoutWait bounds sky api logout, which changes local files only.
const skyLogoutWait = time.Minute

// Logout runs sky api logout with axon's own client, unless the client
// is not installed or stores no endpoint, in which case it reports why it
// skipped. It never installs the client.
func (c Connector) Logout(ctx context.Context) (LogoutOutcome, error) {
	_, installed, err := c.ClientVersion(c.Dir)
	if err != nil {
		return 0, err
	}
	if !installed {
		return NoClient, nil
	}
	// With no stored endpoint, sky api logout takes the endpoint for a
	// server on the laptop and fails.
	endpoint, err := storedEndpoint(c.SettingsFile)
	if err != nil {
		return 0, err
	}
	if endpoint == "" {
		return NoLogin, nil
	}
	skyPath, err := c.ClientPath(c.Dir)
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, skyLogoutWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, skyPath, "api", "logout")
	// sky api logout refuses to run while the endpoint variable is set.
	cmd.Env = apiEnviron(c.Environ())
	interrupt.Stop(ctx, cmd, WaitDelay)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return 0, fmt.Errorf("sky api logout: %w: %s", err, msg)
		}
		return 0, fmt.Errorf("sky api logout: %w", err)
	}
	return LoggedOut, nil
}
