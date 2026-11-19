package login

import (
	"context"
	"fmt"
	"io"

	"github.com/enigma-brain/axon/internal/apiserver"
	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/orgfile"
	"github.com/enigma-brain/axon/internal/skyclient"
)

// client is axon's own SkyPilot client, as login uses it.
type client interface {
	// Version returns the installed client's version; false when none is
	// installed.
	Version() (string, bool, error)
	// Ensure installs the client at version, unless it is already at it,
	// and returns the versions it moved from and to.
	Ensure(ctx context.Context, version string, progress io.Writer) (from, to string, err error)
	// Path returns the absolute path of the client's sky executable.
	Path() (string, error)
}

// skyClient is the client in skyclient, kept in one axon directory.
type skyClient struct{ dir axondir.Dir }

func (c skyClient) Version() (string, bool, error) { return skyclient.Version(c.dir) }

func (c skyClient) Ensure(ctx context.Context, version string, progress io.Writer) (string, string, error) {
	return skyclient.Ensure(ctx, c.dir, version, progress)
}

func (c skyClient) Path() (string, error) { return skyclient.Path(c.dir) }

// connectDevenv runs the four steps that connect devenv: make sure the client
// is installed, connect and log it in by the pathway's steps, verify the
// identity the server reports and record the email, and sync the client's
// version with the server's.
func (l *login) connectDevenv(ctx context.Context, p orgfile.Pathway, opts Options) error {
	// 1. The client is installed.
	version, installed, err := l.client.Version()
	if err != nil {
		return fmt.Errorf("check the SkyPilot client: %w. Run `axon login` again.", err)
	}
	if !installed {
		version = skyclient.DefaultVersion
	}
	if _, version, err = l.client.Ensure(ctx, version, opts.Out); err != nil {
		return fmt.Errorf("install the SkyPilot client: %w. Check your network and run `axon login` again.", err)
	}
	sky, err := l.client.Path()
	if err != nil {
		return fmt.Errorf("find the SkyPilot client: %w. Run `axon login` again.", err)
	}

	// 2. The client is connected and logged in.
	endpoint := l.endpoint(p)
	var health apiserver.Health
	switch p.Kind {
	case orgfile.KindDirect:
		health, err = l.connectDirect(ctx, p, opts, sky, version)
	case orgfile.KindTeleport:
		// On this kind Teleport supplies the identity, so the identity
		// check and the version sync below need no SkyPilot login: sky api
		// login runs after them, with the client at the server's version.
		health, err = l.connectTeleport(ctx, p, opts, version)
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
		if p.Kind == orgfile.KindDirect {
			cookies = l.cookieFile
		}
		health, err = l.health(ctx, endpoint, apiserver.Options{ClientVersion: version, CookieFile: cookies})
		if err != nil {
			return fmt.Errorf("verify the login: %w. Run `axon login` again.", err)
		}
	}
	if err := l.recordEmail(health.User); err != nil {
		return err
	}

	// 4. The client's version is the server's.
	switch {
	case health.ServerVersion == "":
		fmt.Fprintf(opts.Out, "devenv: the server did not report its version; the SkyPilot client stays at %s\n", version)
	case health.ServerVersion != version:
		from, to, err := l.client.Ensure(ctx, health.ServerVersion, opts.Out)
		if err != nil {
			return fmt.Errorf("update the SkyPilot client from %s to %s: %w. Check your network and run `axon login` again.",
				version, health.ServerVersion, err)
		}
		fmt.Fprintf(opts.Out, "devenv: SkyPilot client %s -> %s, the server's version\n", from, to)
		version = to
	}

	// A teleport pathway's last step, with the synced client.
	if p.Kind == orgfile.KindTeleport {
		if sky, err = l.client.Path(); err != nil {
			return fmt.Errorf("find the SkyPilot client: %w. Run `axon login` again.", err)
		}
		if err := l.pointClient(ctx, p, opts, sky); err != nil {
			return err
		}
	}

	fmt.Fprintf(opts.Out, "devenv: connected as %s (SkyPilot %s)\n", health.User, version)
	return nil
}

// recordEmail records the logged-in email, unless it is already recorded.
func (l *login) recordEmail(email string) error {
	if recorded, ok, err := l.dir.Email(); err == nil && ok && recorded == email {
		return nil
	}
	if err := l.dir.SetEmail(email); err != nil {
		return fmt.Errorf("record the logged-in email: %w", err)
	}
	return nil
}
