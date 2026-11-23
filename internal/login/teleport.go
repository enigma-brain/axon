package login

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

	"github.com/enigma-brain/axon/internal/apiserver"
	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/orgfile"
	"github.com/enigma-brain/axon/internal/tsh"
	"github.com/enigma-brain/axon/internal/tunnel"
)

// skyLoginWait bounds sky api login on a teleport pathway. It opens no
// browser and asks nothing, so it ends quickly or not at all.
const skyLoginWait = time.Minute

// errNoIdentity reports a server that answers through the tunnel without
// the user's Teleport identity. sky api login would open a browser against
// it, so login stops instead.
var errNoIdentity = errors.New("the SkyPilot API server did not report your Teleport identity through the tunnel. Run `axon login` again.")

// teleport is what login does with Teleport. Run fills it with the tsh
// package; tests replace ensure, which asks the live proxy for its version.
type teleport struct {
	// ensure returns the path of a tsh the proxy supports, installing one
	// when none is present.
	ensure func(ctx context.Context, dir axondir.Dir, proxy string, in io.Reader, out io.Writer) (string, error)
	// newTunnel returns the tunnel to the Teleport application app through
	// the Teleport proxy proxy, run with tshPath and recorded in stateDir.
	newTunnel func(stateDir, tshPath, proxy, app string) *tunnel.Tunnel
	// skyConfig is SkyPilot's user configuration file, where sky api login
	// stores the endpoint.
	skyConfig string
	// path is the tsh the session stage found, used by the steps after it.
	path string
}

// establishTeleportSession makes sure tsh is present and logs in to
// Teleport when the certificate is not valid. A valid certificate needs no
// login, so no browser opens.
func (l *login) establishTeleportSession(ctx context.Context, p orgfile.Pathway, opts Options) error {
	proxy := p.Teleport.Proxy
	path, err := l.teleport.ensure(ctx, l.dir, proxy, opts.In, opts.Out)
	if err != nil {
		return err
	}
	l.teleport.path = path

	valid, err := tsh.SessionValid(ctx, path, proxy)
	if err != nil {
		return fmt.Errorf("%w. Run `axon login` again.", err)
	}
	if valid {
		fmt.Fprintf(opts.Out, "teleport: logged in to %s\n", proxy)
		return nil
	}
	fmt.Fprintf(opts.Out, "teleport: logging in to %s\n", proxy)
	if err := tsh.Login(ctx, path, proxy); err != nil {
		return fmt.Errorf("%w. Run `axon login` again.", err)
	}
	return nil
}

// connectTeleport runs a teleport pathway's steps up to the tunnel:
// it leaves a tunnel that is up alone, or starts a fresh one. It returns the
// server's health as reported through the tunnel, which carries the user's
// identity. The last step, pointClient, runs later.
func (l *login) connectTeleport(ctx context.Context, p orgfile.Pathway, opts Options, clientVersion string) (apiserver.Health, error) {
	endpoint := l.endpoint(p)
	proxy, app := p.Teleport.Proxy, p.Devenv.App
	stateDir, err := l.dir.StateDir()
	if err != nil {
		return apiserver.Health{}, err
	}
	tn := l.teleport.newTunnel(stateDir, l.teleport.path, proxy, app)
	// Through the tunnel the request carries no login of its own: Teleport
	// supplies the identity.
	healthOpts := apiserver.Options{ClientVersion: clientVersion}

	// 1. A tunnel that is up and carries the identity is left running, so
	// ssh and VS Code sessions through it carry on.
	h, up, err := l.tunnelUp(ctx, tn, endpoint, healthOpts)
	if err != nil {
		return apiserver.Health{}, err
	}
	if up {
		fmt.Fprintf(opts.Out, "sky: the Teleport tunnel is up on port %d\n", tunnel.Port)
	} else {
		// 2. A fresh application certificate.
		if err := tsh.AppLogin(ctx, l.teleport.path, proxy, app); err != nil {
			return apiserver.Health{}, fmt.Errorf("%w. Run `axon login` again.", err)
		}
		// 3. A running tunnel keeps the certificate it started with.
		if err := tn.Stop(); err != nil {
			return apiserver.Health{}, fmt.Errorf("stop the Teleport tunnel: %w", err)
		}
		// 4 and 5. Start confirms that the port is free, starts the tunnel
		// and waits for it to answer.
		fmt.Fprintf(opts.Out, "sky: starting the Teleport tunnel on port %d\n", tunnel.Port)
		if err := tn.Start(ctx); err != nil {
			if ctx.Err() != nil {
				return apiserver.Health{}, ctx.Err()
			}
			return apiserver.Health{}, startFailure(err)
		}
		h, err = l.health(ctx, endpoint, healthOpts)
		switch {
		case err == nil:
		case ctx.Err() != nil:
			return apiserver.Health{}, ctx.Err()
		case errors.Is(err, apiserver.ErrNotLoggedIn):
			return apiserver.Health{}, errNoIdentity
		default:
			return apiserver.Health{}, fmt.Errorf("check the SkyPilot API server through the Teleport tunnel: %w. Run `axon login` again.", err)
		}
	}

	return h, nil
}

// pointClient runs a teleport pathway's last step: sky api login
// against the tunnel's endpoint, when the endpoint the client has stored
// differs. connectSky runs it after the identity check and the version
// sync, which on this pathway need no SkyPilot login, so sky is a client at
// the server's version.
func (l *login) pointClient(ctx context.Context, p orgfile.Pathway, opts Options, sky string) error {
	endpoint := l.endpoint(p)
	stored, err := storedEndpoint(l.teleport.skyConfig)
	if err != nil {
		return err
	}
	if stored == endpoint {
		return nil
	}
	return l.skyLogin(ctx, opts, sky, endpoint)
}

// tunnelUp reports whether the tunnel serves and the server reports the
// user's identity through it, with the server's health when it does.
func (l *login) tunnelUp(ctx context.Context, tn *tunnel.Tunnel, endpoint string, opts apiserver.Options) (apiserver.Health, bool, error) {
	st, err := tn.Status(ctx)
	if err != nil {
		return apiserver.Health{}, false, fmt.Errorf("check the Teleport tunnel: %w", err)
	}
	if !st.Healthy {
		return apiserver.Health{}, false, ctx.Err()
	}
	h, err := l.health(ctx, endpoint, opts)
	if err != nil {
		// Whatever the reason, the tunnel is restarted; a cancelled login
		// stops instead.
		return apiserver.Health{}, false, ctx.Err()
	}
	return h, true, nil
}

// errNotAnswering reports a tunnel whose process ran without the server
// answering through it. Another login would stop and restart the tunnel and
// could not fix the server.
var errNotAnswering = errors.New("the SkyPilot API server is not answering through the Teleport tunnel. Retry `axon login` later.")

// startFailure returns the error login stops with when the tunnel did not
// start. A tunnel whose process exited is the tunnel's own failure, and its
// log says why; one whose process ran without answering points at the
// server.
func startFailure(err error) error {
	var busy *tunnel.PortBusyError
	var startErr *tunnel.StartError
	switch {
	case errors.As(err, &busy):
		return fmt.Errorf("%w. Stop that process and run `axon login` again.", busy)
	case errors.As(err, &startErr) && !startErr.Exited:
		return errNotAnswering
	default:
		return fmt.Errorf("%w\nRun `axon login` again.", err)
	}
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
func (l *login) skyLogin(ctx context.Context, opts Options, sky, endpoint string) error {
	parent := ctx
	ctx, cancel := context.WithTimeout(parent, skyLoginWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, sky, "api", "login", "-e", endpoint)
	cmd.Env = withoutVars(l.environ(), endpointVar)
	cmd.WaitDelay = skyWaitDelay
	cmd.Stdout = opts.Out
	cmd.Stderr = opts.Err

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
