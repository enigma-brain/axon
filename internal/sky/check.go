package sky

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/enigma-brain/axon/internal/apiserver"
	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/configfile"
	"github.com/enigma-brain/axon/internal/connected"
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
	endpoint func(configfile.Pathway) string
	// health makes one request to the health endpoint.
	health func(ctx context.Context, endpoint string, opts apiserver.Options) (apiserver.Health, error)
	// cookieFile returns SkyPilot's cookie file for the user whose home
	// directory is home.
	cookieFile func(home string) string
	// clientVersion returns the installed client's version; false when none
	// is installed.
	clientVersion func(axondir.Dir) (string, bool, error)
	// findTsh returns the path of the tsh on PATH.
	findTsh func() (string, error)
	// sessionValid reports whether the Teleport certificate for proxy is
	// valid.
	sessionValid func(ctx context.Context, tshPath, proxy string) (bool, error)
	// checkTunnel checks that no process other than the tunnel to the
	// Teleport application app through proxy holds the tunnel's port, and
	// records that tunnel in stateDir when it serves unrecorded. It returns
	// a *tunnel.PortBusyError when another process holds the port.
	checkTunnel func(ctx context.Context, stateDir, proxy, app string) error
	// startTunnel starts the tunnel to the Teleport application app through
	// proxy, run with tshPath and recorded in stateDir.
	startTunnel func(ctx context.Context, stateDir, tshPath, proxy, app string) error
}

var realProbes = probes{
	endpoint: Endpoint,
	health:   apiserver.Fetch,
	cookieFile: func(home string) string {
		return CookieFile(home, os.Getenv)
	},
	clientVersion: skyclient.Version,
	findTsh:       tsh.Find,
	sessionValid:  tsh.SessionValid,
	checkTunnel: func(ctx context.Context, stateDir, proxy, app string) error {
		// Checking needs no tsh: only a start runs it.
		_, err := tunnel.New(stateDir, "", proxy, app, tunnel.Port).Check(ctx)
		return err
	},
	startTunnel: func(ctx context.Context, stateDir, tshPath, proxy, app string) error {
		return tunnel.New(stateDir, tshPath, proxy, app, tunnel.Port).Start(ctx)
	},
}

// check holds the probes checkConnection uses.
var check = realProbes

// checkConnection checks the connection to the API server before a command runs,
// repairs what needs no action from the user, and otherwise returns an
// error that names the user's next step. It makes one health request. On
// a teleport pathway it first checks that the process on the tunnel's port
// is the tunnel, and stops when it is another; when the health request fails for a reason other than
// a missing login, it restarts the tunnel once, writes one line saying so to
// w, and makes the request again. It never opens a browser and never
// installs anything.
//
// It returns the active pathway and the logged-in email it checked, read
// once before the health request, and the installed client's version, so
// that the command runs with the same ones. The pathway counts as connected only while its settings for
// machines are the ones axon login recorded when it connected it.
func checkConnection(ctx context.Context, dir axondir.Dir, w io.Writer) (configfile.Pathway, string, string, error) {
	pw, err := activePathway(dir)
	if err != nil {
		return configfile.Pathway{}, "", "", err
	}
	email, hasEmail, err := dir.Email()
	if err != nil {
		return configfile.Pathway{}, "", "", err
	}
	if !hasEmail {
		return configfile.Pathway{}, "", "", ErrNoLogin
	}
	version, err := check.run(ctx, dir, pw, w)
	if err != nil {
		return configfile.Pathway{}, "", "", err
	}
	return pw, email, version, nil
}

// run checks the connection on pw and returns the installed client's
// version, which the health request sends.
func (p probes) run(ctx context.Context, dir axondir.Dir, pw configfile.Pathway, w io.Writer) (string, error) {
	// A pathway whose settings changed after it was connected, such as by
	// a file that --config replaced, is not the one connected.
	same, err := connected.Is(dir, pw)
	if err != nil {
		return "", err
	}
	if !same {
		return "", ErrNoLogin
	}

	version, installed, err := p.clientVersion(dir)
	if err != nil {
		return "", fmt.Errorf("check the SkyPilot client: %w. Run `axon login`.", err)
	}
	if !installed {
		return "", errNoClient
	}
	// Through the tunnel the request needs no login, and until the tunnel
	// is known to be up, whatever listens on the local port is unknown, so
	// SkyPilot's cookies go only to a direct pathway's endpoint.
	var cookies string
	if pw.Kind == configfile.KindDirect {
		cookies = p.cookieFile(dir.Home())
	}

	if pw.Kind == configfile.KindTeleport {
		if err := p.checkPort(ctx, dir, pw); err != nil {
			return "", err
		}
	}

	endpoint := p.endpoint(pw)
	opts := apiserver.Options{ClientVersion: version, CookieFile: cookies}
	h, err := p.health(ctx, endpoint, opts)
	if err != nil && pw.Kind == configfile.KindTeleport && !errors.Is(err, apiserver.ErrNotLoggedIn) && ctx.Err() == nil {
		if err := p.restartTunnel(ctx, dir, pw, w); err != nil {
			return "", err
		}
		h, err = p.health(ctx, endpoint, opts)
	}
	if err != nil {
		return "", failure(ctx, pw, endpoint, err)
	}
	if h.ServerVersion != "" && h.ServerVersion != version {
		return "", errVersion
	}
	return version, nil
}

// failure returns the error the check stops with when the health request
// failed with err.
func failure(ctx context.Context, p configfile.Pathway, endpoint string, err error) error {
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, apiserver.ErrNotLoggedIn):
		return errSessionExpired
	case p.Kind == configfile.KindDirect && errors.Is(err, apiserver.ErrUnreachable):
		return errors.New(unreachable(p, endpoint))
	default:
		return errNotAnswering
	}
}

// checkPort returns an error when a process other than the pathway's
// tunnel holds the tunnel's port, so that the client never sends its
// requests to it. A tunnel that is down is left to the restart.
func (p probes) checkPort(ctx context.Context, dir axondir.Dir, pw configfile.Pathway) error {
	stateDir, err := dir.StateDir()
	if err != nil {
		return err
	}
	err = p.checkTunnel(ctx, stateDir, pw.Teleport.Proxy, pw.Sky.App)
	var busy *tunnel.PortBusyError
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.As(err, &busy):
		return fmt.Errorf("%w. Stop that process and retry.", busy)
	default:
		return fmt.Errorf("Couldn't check the Teleport tunnel: %w. Run `axon login`.", err)
	}
}

// restartTunnel restarts the tunnel when the Teleport certificate is valid.
// It returns nil once the restarted tunnel serves.
func (p probes) restartTunnel(ctx context.Context, dir axondir.Dir, pw configfile.Pathway, w io.Writer) error {
	tshPath, err := p.findTsh()
	switch {
	case errors.Is(err, tsh.ErrNotFound):
		return err
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
