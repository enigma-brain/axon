package login

import (
	"context"
	"errors"
	"fmt"

	"github.com/enigma-brain/axon/internal/apiserver"
	"github.com/enigma-brain/axon/internal/configfile"
	"github.com/enigma-brain/axon/internal/tsh"
	"github.com/enigma-brain/axon/internal/tunnel"
)

// errNoIdentity reports a server that answers through the tunnel without
// the user's Teleport identity. sky api login would open a browser against
// it, so login stops instead.
var errNoIdentity = errors.New("the SkyPilot API server did not report your Teleport identity through the tunnel. Run `axon login` again.")

// teleport is what login does with Teleport. Run fills it with the tsh
// package; tests replace ensure, which asks the live proxy for its version.
type teleport struct {
	// ensure returns the path of the tsh on PATH when the proxy supports
	// its version.
	ensure func(ctx context.Context, proxy string) (string, error)
	// newTunnel returns the tunnel to the Teleport application app through
	// the Teleport proxy proxy, run with tshPath and recorded in stateDir.
	newTunnel func(stateDir, tshPath, proxy, app string) *tunnel.Tunnel
	// path is the tsh the session stage found, used by the steps after it.
	path string
}

// establishTeleportSession logs in to Teleport, with the tsh that run
// found, when the certificate is not valid. A valid certificate needs no
// login, so no browser opens.
func (l *login) establishTeleportSession(ctx context.Context, p configfile.Pathway, opts Options) error {
	proxy := p.Teleport.Proxy
	path := l.teleport.path

	valid, err := tsh.SessionValid(ctx, path, proxy)
	if err != nil {
		return fmt.Errorf("%w. Run `axon login` again.", err)
	}
	if valid {
		fmt.Fprintf(opts.Out, "teleport: logged in to %s\n", proxy)
		return nil
	}
	fmt.Fprintf(opts.Out, "teleport: logging in to %s\n", proxy)
	if err := tsh.Login(ctx, path, proxy, opts.In); err != nil {
		return fmt.Errorf("%w. Run `axon login` again.", err)
	}
	return nil
}

// connectTeleport runs a teleport pathway's steps up to the tunnel:
// it leaves a tunnel that is up alone, or starts a fresh one. It returns the
// server's health as reported through the tunnel, which carries the user's
// identity. The last step, pointClient, runs later.
func (l *login) connectTeleport(ctx context.Context, p configfile.Pathway, opts Options, clientVersion string) (apiserver.Health, error) {
	endpoint := l.sky.Endpoint(p)
	proxy, app := p.Teleport.Proxy, p.Sky.App
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
		if err := tsh.AppLogin(ctx, l.teleport.path, proxy, app, opts.In); err != nil {
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
		h, err = l.sky.Health(ctx, endpoint, healthOpts)
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

// tunnelUp reports whether the tunnel serves and the server reports the
// user's identity through it, with the server's health when it does. A
// tunnel that serves is recorded, so that logout and a switch of pathway
// stop it. When another process holds the tunnel's port, login stops.
func (l *login) tunnelUp(ctx context.Context, tn *tunnel.Tunnel, endpoint string, opts apiserver.Options) (apiserver.Health, bool, error) {
	st, err := tn.Check(ctx)
	var busy *tunnel.PortBusyError
	switch {
	case errors.As(err, &busy):
		return apiserver.Health{}, false, startFailure(err)
	case err != nil && ctx.Err() != nil:
		return apiserver.Health{}, false, ctx.Err()
	case err != nil:
		return apiserver.Health{}, false, fmt.Errorf("check the Teleport tunnel: %w", err)
	}
	if !st.Healthy {
		return apiserver.Health{}, false, ctx.Err()
	}
	h, err := l.sky.Health(ctx, endpoint, opts)
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
