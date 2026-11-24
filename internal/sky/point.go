package sky

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/enigma-brain/axon/internal/configfile"
	"github.com/enigma-brain/axon/internal/interrupt"
)

// skyLoginWait bounds sky api login on a teleport pathway. It opens no
// browser and asks nothing, so it ends quickly or not at all.
const skyLoginWait = time.Minute

// pointClient runs a teleport pathway's last step: sky api login
// against the tunnel's endpoint, when the endpoint the client has stored
// differs. Connect runs it after the identity check and the version
// sync, which on this pathway need no SkyPilot login, so sky is a client at
// the server's version.
func (c Connector) pointClient(ctx context.Context, p configfile.Pathway, opts Options, skyPath string) error {
	endpoint := c.Endpoint(p)
	stored, err := storedEndpoint(c.SettingsFile)
	if err != nil {
		return err
	}
	if stored == endpoint {
		return nil
	}
	return c.skyLogin(ctx, opts, skyPath, endpoint)
}

// storedEndpoint returns the API server endpoint SkyPilot's client has
// stored in its configuration file; empty when the file or the key is
// missing.
func storedEndpoint(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read SkyPilot's configuration: %w", err)
	}
	var config struct {
		APIServer struct {
			Endpoint string `yaml:"endpoint"`
		} `yaml:"api_server"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("read SkyPilot's configuration %s: %w. Fix or remove the file and run `axon login` again.", path, err)
	}
	return strings.TrimSpace(config.APIServer.Endpoint), nil
}

// skyLogin runs sky api login against endpoint, which stores the endpoint
// in SkyPilot's configuration. The server already reports the user's
// identity, so it opens no browser.
func (c Connector) skyLogin(ctx context.Context, opts Options, skyPath, endpoint string) error {
	parent := ctx
	ctx, cancel := context.WithTimeout(parent, skyLoginWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, skyPath, "api", "login", "-e", endpoint)
	cmd.Env = apiEnviron(c.Environ())
	// The client runs apart from the terminal, as the browser login's
	// does, and writes through pipes, since a group that is not the
	// terminal's can be stopped for writing to it.
	interrupt.StopGroup(ctx, cmd, WaitDelay)
	cmd.Stdout = struct{ io.Writer }{opts.Out}
	cmd.Stderr = struct{ io.Writer }{opts.Err}

	fmt.Fprintf(opts.Out, "sky: pointing the SkyPilot client at %s\n", endpoint)
	runErr := cmd.Run()
	// The caller's own cancellation or deadline comes first; the message
	// about the wait is only for the wait login sets here.
	if err := parent.Err(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return fmt.Errorf("the SkyPilot login did not finish within %s. Run `axon login` again.", formatWait(skyLoginWait))
	}
	if runErr != nil {
		return fmt.Errorf("the SkyPilot login failed (%v). Run `axon login` again.", runErr)
	}
	return nil
}
