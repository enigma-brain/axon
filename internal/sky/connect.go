package sky

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/enigma-brain/axon/internal/apiserver"
	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/configfile"
	"github.com/enigma-brain/axon/internal/skyclient"
	"github.com/enigma-brain/axon/internal/termtext"
)

// Options are the inputs and outputs of the steps axon login and axon
// logout run with SkyPilot.
type Options struct {
	// In is where the commands the steps run read from.
	In io.Reader
	// Out is where progress is written.
	Out io.Writer
	// Err is where the error output of the commands the steps run is
	// written.
	Err io.Writer
}

// Connector is what axon login and axon logout do with SkyPilot for one
// user. NewConnector fills it with the real client, server and files;
// tests replace each part.
type Connector struct {
	// Dir is the user's axon directory.
	Dir axondir.Dir
	// ClientVersion, EnsureClient and ClientPath are axon's own SkyPilot
	// client: its installed version, false when none is installed; its
	// install at a version, unless it is already at it; and the path of
	// its sky executable.
	ClientVersion func(dir axondir.Dir) (string, bool, error)
	EnsureClient  func(ctx context.Context, dir axondir.Dir, version string, progress io.Writer) (from, to string, err error)
	ClientPath    func(dir axondir.Dir) (string, error)
	// Health makes one request to the API server's health endpoint.
	Health func(ctx context.Context, endpoint string, opts apiserver.Options) (apiserver.Health, error)
	// Endpoint returns the API server endpoint of a pathway.
	Endpoint func(configfile.Pathway) string
	// CookieFile is SkyPilot's cookie file. The sky commands the steps run
	// inherit the environment that chose it, so they use the same file.
	CookieFile string
	// SettingsFile is SkyPilot's user configuration file, where sky api
	// login stores the endpoint.
	SettingsFile string
	// LoginWait bounds the browser login.
	LoginWait time.Duration
	// Environ is the environment the commands the steps run start from.
	Environ func() []string
}

// NewConnector returns the Connector for the user whose axon directory is
// dir, with the environment getenv reads.
func NewConnector(dir axondir.Dir, getenv func(string) string) Connector {
	return Connector{
		Dir:           dir,
		ClientVersion: skyclient.Version,
		EnsureClient:  skyclient.Ensure,
		ClientPath:    skyclient.Path,
		Health:        apiserver.Fetch,
		Endpoint:      Endpoint,
		CookieFile:    CookieFile(dir.Home(), getenv),
		SettingsFile:  SettingsFile(dir.Home(), getenv),
		LoginWait:     loginWait,
		Environ:       os.Environ,
	}
}

// Connect runs the four steps that connect the sky group: make sure the client
// is installed, connect and log it in by the pathway's steps, verify the
// identity the server reports and record the email, and sync the client's
// version with the server's. On a teleport pathway, teleport runs the
// pathway's steps up to the tunnel and returns the server's health through
// it.
func (c Connector) Connect(ctx context.Context, p configfile.Pathway, opts Options, teleport func(ctx context.Context, clientVersion string) (apiserver.Health, error)) error {
	// 1. The client is installed.
	version, installed, err := c.ClientVersion(c.Dir)
	if err != nil {
		return fmt.Errorf("check the SkyPilot client: %w. Run `axon login` again.", err)
	}
	if !installed {
		version = skyclient.DefaultVersion
	}
	if _, version, err = c.EnsureClient(ctx, c.Dir, version, opts.Out); err != nil {
		if errors.Is(err, skyclient.ErrUnavailable) {
			return fmt.Errorf("install the SkyPilot client: %w", err)
		}
		return fmt.Errorf("install the SkyPilot client: %w. Check your network and run `axon login` again.", err)
	}
	skyPath, err := c.ClientPath(c.Dir)
	if err != nil {
		return fmt.Errorf("find the SkyPilot client: %w. Run `axon login` again.", err)
	}

	// 2. The client is connected and logged in.
	endpoint := c.Endpoint(p)
	var health apiserver.Health
	switch p.Kind {
	case configfile.KindDirect:
		health, err = c.connectDirect(ctx, p, opts, skyPath, version)
	case configfile.KindTeleport:
		// On this kind Teleport supplies the identity, so the identity
		// check and the version sync below need no SkyPilot login: sky api
		// login runs after them, with the client at the server's version.
		health, err = teleport(ctx, version)
	default:
		err = fmt.Errorf("the pathway %q has the kind %q, which axon does not know", p.ID, p.Kind)
	}
	if err != nil {
		return err
	}

	// 3. The server reports the user's identity.
	if health.User == "" {
		// SkyPilot's cookies go only to a direct pathway's endpoint:
		// through the tunnel the request needs no login, and whatever holds
		// the local port is not known to be the tunnel.
		var cookies string
		if p.Kind == configfile.KindDirect {
			cookies = c.CookieFile
		}
		health, err = c.Health(ctx, endpoint, apiserver.Options{ClientVersion: version, CookieFile: cookies})
		if err != nil {
			return fmt.Errorf("verify the login: %w. Run `axon login` again.", err)
		}
	}
	if err := c.recordEmail(health.User); err != nil {
		return err
	}

	// 4. The client's version is the server's.
	switch {
	case health.ServerVersion == "":
		fmt.Fprintf(opts.Out, "sky: the server did not report its version; the SkyPilot client stays at %s\n", version)
	case health.ServerVersion != version:
		from, to, err := c.EnsureClient(ctx, c.Dir, health.ServerVersion, opts.Out)
		if errors.Is(err, skyclient.ErrUnavailable) {
			return fmt.Errorf("update the SkyPilot client from %s to %s: %w", version, termtext.Clean(health.ServerVersion), err)
		}
		if err != nil {
			return fmt.Errorf("update the SkyPilot client from %s to %s: %w. Check your network and run `axon login` again.",
				version, termtext.Clean(health.ServerVersion), err)
		}
		fmt.Fprintf(opts.Out, "sky: SkyPilot client %s -> %s, the server's version\n", from, to)
		version = to
	}

	// A teleport pathway's last step, with the synced client.
	if p.Kind == configfile.KindTeleport {
		if skyPath, err = c.ClientPath(c.Dir); err != nil {
			return fmt.Errorf("find the SkyPilot client: %w. Run `axon login` again.", err)
		}
		if err := c.pointClient(ctx, p, opts, skyPath); err != nil {
			return err
		}
	}

	fmt.Fprintf(opts.Out, "sky: connected as %s (SkyPilot %s)\n", termtext.Clean(health.User), termtext.Clean(version))
	return nil
}

// recordEmail records the logged-in email, unless it is already recorded.
func (c Connector) recordEmail(email string) error {
	if recorded, ok, err := c.Dir.Email(); err == nil && ok && recorded == email {
		return nil
	}
	if err := c.Dir.SetEmail(email); err != nil {
		return fmt.Errorf("record the logged-in email: %w", err)
	}
	return nil
}
