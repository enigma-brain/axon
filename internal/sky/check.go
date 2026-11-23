package sky

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/enigma-brain/axon/internal/apiserver"
	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/connected"
	"github.com/enigma-brain/axon/internal/orgfile"
	"github.com/enigma-brain/axon/internal/skyclient"
	"github.com/enigma-brain/axon/internal/tsh"
	"github.com/enigma-brain/axon/internal/tunnel"
)

// The errors the check stops with. Each names the user's next step.
var (
	errNoClient        = errors.New("the SkyPilot client is not installed. Run `axon login` first.")
	errVersion         = errors.New("Run `axon login` to update the SkyPilot client.")
	errSessionExpired  = errors.New("SkyPilot session expired. Run `axon login`.")
	errNotAnswering    = errors.New("The SkyPilot API server is not answering. Retry later.")
	errTeleportExpired = errors.New("Teleport session expired. Run `axon login`.")
	errNoTsh           = errors.New("Teleport's client, tsh, is not found. Run `axon login`.")
)

const (
	// restartLine is the one line the check prints when it restarts the
	// tunnel.
	restartLine = "sky: restarting the Teleport tunnel"
	// notRestarted begins the error of a tunnel whose process exited
	// during the restart.
	notRestarted = "Couldn't restart the Teleport tunnel. Run `axon login`."
)

// probes are what the check asks about the world. The real ones are in
// realProbes; tests replace them.
type probes struct {
	// endpoint returns the URL the health request goes to on a pathway.
	endpoint func(orgfile.Pathway) string
	// health makes one request to the health endpoint.
	health func(ctx context.Context, endpoint string, opts apiserver.Options) (apiserver.Health, error)
	// cookieFile returns SkyPilot's cookie file.
	cookieFile func() (string, error)
	// clientVersion returns the installed client's version; false when none
	// is installed.
	clientVersion func(axondir.Dir) (string, bool, error)
	// findTsh returns the path of a tsh without installing one.
	findTsh func(axondir.Dir) (string, error)
	// sessionValid reports whether the Teleport certificate for proxy is
	// valid.
	sessionValid func(ctx context.Context, tshPath, proxy string) (bool, error)
	// startTunnel starts the tunnel to the Teleport application app through
	// proxy, run with tshPath and recorded in stateDir.
	startTunnel func(ctx context.Context, stateDir, tshPath, proxy, app string) error
}

var realProbes = probes{
	endpoint: Endpoint,
	health:   apiserver.Fetch,
	cookieFile: func() (string, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find the home directory: %w", err)
		}
		return apiserver.CookieFile(home, os.Getenv), nil
	},
	clientVersion: skyclient.Version,
	findTsh:       tsh.Find,
	sessionValid:  tsh.SessionValid,
	startTunnel: func(ctx context.Context, stateDir, tshPath, proxy, app string) error {
		return tunnel.New(stateDir, tshPath, proxy, app).Start(ctx)
	},
}

// check holds the probes Check uses.
var check = realProbes

// Check checks the connection to the API server before a command runs,
// repairs what needs no action from the user, and otherwise returns an
// error that names the user's next step. It makes one health request. On
// a teleport pathway, when that request fails for a reason other than
// a missing login, it restarts the tunnel once, writes one line saying so to
// w, and makes the request again. It never opens a browser and never
// installs anything.
//
// It returns the active pathway and the logged-in email it checked, read
// once before the health request, so that the command runs with the same
// ones. The pathway counts as connected only while its settings for
// machines are the ones axon login recorded when it connected it.
func Check(ctx context.Context, dir axondir.Dir, w io.Writer) (orgfile.Pathway, string, error) {
	pw, err := activePathway(dir)
	if err != nil {
		return orgfile.Pathway{}, "", err
	}
	email, hasEmail, err := dir.Email()
	if err != nil {
		return orgfile.Pathway{}, "", err
	}
	if !hasEmail {
		return orgfile.Pathway{}, "", ErrNoLogin
	}
	if err := check.run(ctx, dir, pw, w); err != nil {
		return orgfile.Pathway{}, "", err
	}
	return pw, email, nil
}

func (p probes) run(ctx context.Context, dir axondir.Dir, pw orgfile.Pathway, w io.Writer) error {
	// A pathway whose settings changed after it was connected, such as by
	// a file that --config replaced, is not the one connected.
	same, err := connected.Is(dir, pw)
	if err != nil {
		return err
	}
	if !same {
		return ErrNoLogin
	}

	version, installed, err := p.clientVersion(dir)
	if err != nil {
		return fmt.Errorf("check the SkyPilot client: %w. Run `axon login`.", err)
	}
	if !installed {
		return errNoClient
	}
	// Through the tunnel the request needs no login, and until the tunnel
	// is known to be up, whatever listens on the local port is unknown, so
	// SkyPilot's cookies go only to a direct pathway's endpoint.
	var cookies string
	if pw.Kind == orgfile.KindDirect {
		if cookies, err = p.cookieFile(); err != nil {
			return err
		}
	}

	endpoint := p.endpoint(pw)
	opts := apiserver.Options{ClientVersion: version, CookieFile: cookies}
	h, err := p.health(ctx, endpoint, opts)
	if err != nil && pw.Kind == orgfile.KindTeleport && !errors.Is(err, apiserver.ErrNotLoggedIn) && ctx.Err() == nil {
		if err := p.restartTunnel(ctx, dir, pw, w); err != nil {
			return err
		}
		h, err = p.health(ctx, endpoint, opts)
	}
	if err != nil {
		return failure(ctx, pw, endpoint, err)
	}
	if h.ServerVersion != "" && h.ServerVersion != version {
		return errVersion
	}
	return nil
}

// failure returns the error the check stops with when the health request
// failed with err.
func failure(ctx context.Context, p orgfile.Pathway, endpoint string, err error) error {
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, apiserver.ErrNotLoggedIn):
		return errSessionExpired
	case p.Kind == orgfile.KindDirect && errors.Is(err, apiserver.ErrUnreachable):
		return errors.New(Unreachable(p, endpoint))
	default:
		return errNotAnswering
	}
}

// restartTunnel restarts the tunnel when the Teleport certificate is valid.
// It returns nil once the restarted tunnel serves.
func (p probes) restartTunnel(ctx context.Context, dir axondir.Dir, pw orgfile.Pathway, w io.Writer) error {
	tshPath, err := p.findTsh(dir)
	switch {
	case errors.Is(err, tsh.ErrNotFound):
		return errNoTsh
	case err != nil:
		return fmt.Errorf("find Teleport's client, tsh: %w. Run `axon login`.", err)
	}
	valid, err := p.sessionValid(ctx, tshPath, pw.Teleport.Proxy)
	if err != nil {
		return fmt.Errorf("check the Teleport certificate: %w. Run `axon login`.", err)
	}
	if !valid {
		return errTeleportExpired
	}
	stateDir, err := dir.StateDir()
	if err != nil {
		return err
	}

	fmt.Fprintln(w, restartLine)
	err = p.startTunnel(ctx, stateDir, tshPath, pw.Teleport.Proxy, pw.Sky.App)
	var startErr *tunnel.StartError
	var busy *tunnel.PortBusyError
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.As(err, &startErr) && startErr.Exited:
		return fmt.Errorf("%s The end of the tunnel's log:\n%s", notRestarted, startErr.Log)
	case errors.As(err, &startErr):
		// The tunnel ran and the server behind it did not answer. A login
		// would not fix the server.
		return errNotAnswering
	case errors.As(err, &busy):
		return fmt.Errorf("%w. Stop that process and retry.", busy)
	default:
		return fmt.Errorf("Couldn't restart the Teleport tunnel: %w. Run `axon login`.", err)
	}
}
