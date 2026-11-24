package login

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/enigma-brain/axon/internal/apiserver"
	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/configfile"
	"github.com/enigma-brain/axon/internal/connected"
	"github.com/enigma-brain/axon/internal/sky"
	"github.com/enigma-brain/axon/internal/tsh"
	"github.com/enigma-brain/axon/internal/tunnel"
)

// The lines axon logout prints. Each is kept here so that the wording is
// in one place.
const (
	lineNothing      = "Nothing is logged in."
	lineSkyLoggedOut = "sky: logged out of the SkyPilot API server"
	lineSkyNoClient  = "sky: skipped logging out of SkyPilot: the SkyPilot client is not installed"
	lineSkyNoLogin   = "sky: skipped logging out of SkyPilot: the SkyPilot client is not logged in"
	lineSkyCookies   = "sky: removed the saved login for %s"
	lineStopTunnel   = "sky: stopping the Teleport tunnel on port %d"
	lineTeleportOut  = "teleport: logged out of %s"
	lineTeleportNone = "teleport: skipped logging out of %s: tsh is not on PATH"
	lineTeleportOld  = "teleport: skipped logging out of Teleport: the record of the connected pathway is from an earlier axon. Run `tsh logout` to end a Teleport session."
	lineRemoved      = "Removed the record of the connected pathway and the logged-in email"
	lineKept         = "Kept the config files in %s; run `axon login` to log in again"
)

// errLogoutFailed ends a logout in which a step failed.
var errLogoutFailed = errors.New("logout did not finish. Run `axon logout` again.")

// LogoutOptions are the outputs of one logout.
type LogoutOptions struct {
	// Out is where the line of each step is written.
	Out io.Writer
	// Err is where the failure of each step is written.
	Err io.Writer
}

// logout holds what one run of axon logout works with. Logout fills it
// with the real directory and tools; tests replace each part.
type logout struct {
	dir axondir.Dir
	// sky is what logout does with SkyPilot.
	sky sky.Connector
	// findTsh returns the path of the tsh on PATH.
	findTsh func() (string, error)
	// tshLogout ends the Teleport session for a proxy.
	tshLogout func(ctx context.Context, tshPath, proxy string) error
	// newTunnel returns the tunnel to the Teleport application app through
	// the Teleport proxy proxy, recorded in stateDir.
	newTunnel func(stateDir, proxy, app string) teleportTunnel
}

// Logout runs axon logout: it logs the SkyPilot client out, removes the
// API server's cookies from SkyPilot's cookie file, stops the connected
// pathway's tunnel and the recorded one, ends the Teleport session of the connected pathway, and
// removes the records of the connection. It keeps the config files and the
// active pathway. It works by axon's own records alone, never by a config
// file. dir is the user's axon directory.
func Logout(ctx context.Context, dir axondir.Dir, opts LogoutOptions) error {
	lo := &logout{
		dir:       dir,
		sky:       sky.NewConnector(dir, os.Getenv),
		findTsh:   tsh.Find,
		tshLogout: tsh.Logout,
		newTunnel: func(stateDir, proxy, app string) teleportTunnel {
			// Stopping needs no tsh: only a start runs it.
			return newTunnel(stateDir, "", proxy, app)
		},
	}
	return lo.run(ctx, opts)
}

// run runs the steps in order. A step that fails is reported on opts.Err
// and the later steps still run; the records are removed only when every
// earlier step succeeded, so that running axon logout again retries.
func (lo *logout) run(ctx context.Context, opts LogoutOptions) error {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.Err == nil {
		opts.Err = io.Discard
	}
	stateDir := lo.dir.StatePath()

	failed := false
	fail := func(step string, err error) {
		failed = true
		// A step cut short by an interrupt did not fail; axon reports the
		// interrupt once, when logout returns.
		if ctx.Err() == nil {
			fmt.Fprintf(opts.Err, "axon: could not %s: %v\n", step, err)
		}
	}

	// What is recorded. A record that cannot be read is reported, and the
	// steps that do not need it still run; the records are then kept, so
	// that logging out again retries.
	rec, hasRecord, err := connected.Read(lo.dir)
	if err != nil {
		fail("read the record of the connected pathway", err)
	}
	// A record that this axon cannot read as a record, such as one an
	// earlier axon wrote, still marks a connection.
	_, statErr := os.Stat(connected.Path(lo.dir))
	oldRecord := err == nil && !hasRecord && statErr == nil
	_, hasEmail, err := lo.dir.Email()
	if err != nil {
		fail("read the logged-in email", err)
	}
	running, err := tunnelRunning(stateDir)
	if err != nil {
		fail("check the Teleport tunnel", err)
	}
	// The connected pathway's tunnel, which runs with no record when it was
	// started by hand or its record was lost.
	var pathwayTunnel teleportTunnel
	if hasRecord && rec.Kind == string(configfile.KindTeleport) && rec.TeleportProxy != "" && rec.SkyApp != "" {
		pathwayTunnel = lo.newTunnel(stateDir, rec.TeleportProxy, rec.SkyApp)
		up, err := pathwayTunnelRunning(stateDir, pathwayTunnel)
		if err != nil {
			fail("check the Teleport tunnel", err)
		}
		running = running || up
	}
	if !failed && !hasRecord && !oldRecord && !hasEmail && !running {
		fmt.Fprintln(opts.Out, lineNothing)
		return nil
	}

	// 1. The SkyPilot login.
	if err := lo.skyLogout(ctx, opts); err != nil {
		fail("log out of SkyPilot", err)
	}

	// 2. The API server's cookies, which sky api logout leaves in the
	// cookie file. Only a direct pathway records an endpoint.
	if hasRecord && rec.SkyEndpoint != "" {
		n, err := apiserver.RemoveCookies(lo.sky.CookieFile, rec.SkyEndpoint)
		switch {
		case err != nil:
			fail("remove the saved SkyPilot login", err)
		case n > 0:
			fmt.Fprintf(opts.Out, lineSkyCookies+"\n", rec.SkyEndpoint)
		}
	}

	// 3. The tunnel: the connected pathway's, recorded or not, and the
	// one the record names.
	if running {
		if err := stopTunnels(opts.Out, stateDir, pathwayTunnel); err != nil {
			fail("stop the Teleport tunnel", err)
		}
	}

	// 4. The Teleport session of the connected pathway.
	switch {
	case hasRecord && rec.Kind == string(configfile.KindTeleport) && rec.TeleportProxy != "":
		if err := lo.teleportLogout(ctx, opts, rec.TeleportProxy); err != nil {
			fail("log out of Teleport", err)
		}
	case oldRecord:
		fmt.Fprintln(opts.Out, lineTeleportOld)
	}

	// 5. The records of the connection.
	if !failed {
		if err := lo.removeRecords(); err != nil {
			fail("remove the records of the connection", err)
		} else {
			fmt.Fprintln(opts.Out, lineRemoved)
		}
	}

	// A logout whose steps all succeeded is done, interrupted or not.
	if err := ctx.Err(); failed && err != nil {
		return err
	}
	fmt.Fprintf(opts.Out, lineKept+"\n", lo.dir.ConfigsPath())
	if failed {
		return errLogoutFailed
	}
	return nil
}

// skyLogout logs the SkyPilot client out and writes the line for what it
// did.
func (lo *logout) skyLogout(ctx context.Context, opts LogoutOptions) error {
	outcome, err := lo.sky.Logout(ctx)
	if err != nil {
		return err
	}
	switch outcome {
	case sky.NoClient:
		fmt.Fprintln(opts.Out, lineSkyNoClient)
	case sky.NoLogin:
		fmt.Fprintln(opts.Out, lineSkyNoLogin)
	default:
		fmt.Fprintln(opts.Out, lineSkyLoggedOut)
	}
	return nil
}

// teleportLogout ends the Teleport session for proxy with the tsh on PATH,
// or says why it skipped when there is none.
func (lo *logout) teleportLogout(ctx context.Context, opts LogoutOptions, proxy string) error {
	path, err := lo.findTsh()
	if errors.Is(err, tsh.ErrNotFound) {
		fmt.Fprintf(opts.Out, lineTeleportNone+"\n", proxy)
		return nil
	}
	if err != nil {
		return err
	}
	if err := lo.tshLogout(ctx, path, proxy); err != nil {
		return err
	}
	fmt.Fprintf(opts.Out, lineTeleportOut+"\n", proxy)
	return nil
}

// removeRecords removes the logged-in email and the record of the connected
// pathway. The active pathway stays, so that axon login logs in again.
func (lo *logout) removeRecords() error {
	if err := lo.dir.RemoveEmail(); err != nil {
		return err
	}
	return connected.Remove(lo.dir)
}

// tunnelRunning reports whether the tunnel recorded in stateDir runs, and
// creates no directory.
func tunnelRunning(stateDir string) (bool, error) {
	if _, err := os.Stat(stateDir); errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return tunnel.RecordedRunning(stateDir)
}

// pathwayTunnelRunning reports whether tn runs, recorded or not, and creates
// no directory.
func pathwayTunnelRunning(stateDir string, tn teleportTunnel) (bool, error) {
	if _, err := os.Stat(stateDir); errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return tn.Running()
}

// stopTunnels writes to w that the tunnel is stopping, then stops tn, when
// it is not nil, and the tunnel recorded in stateDir.
func stopTunnels(w io.Writer, stateDir string, tn teleportTunnel) error {
	fmt.Fprintf(w, lineStopTunnel+"\n", tunnel.Port)
	if tn != nil {
		if err := tn.Stop(); err != nil {
			return err
		}
	}
	return tunnel.StopRecorded(stateDir)
}
