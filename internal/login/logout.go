package login

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/connected"
	"github.com/enigma-brain/axon/internal/orgfile"
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
	lineTeleportOut  = "teleport: logged out of %s"
	lineTeleportNone = "teleport: skipped logging out of %s: tsh is not on PATH"
	lineTeleportOld  = "teleport: skipped logging out of Teleport: the record of the connected pathway is from an earlier axon. Run `tsh logout` to end a Teleport session."
	lineRemoved      = "Removed the record of the connected pathway and the logged-in email"
	lineKept         = "Kept the config files in %s; run `axon login` to log in again"
)

// errLogoutFailed ends a logout in which a step failed.
var errLogoutFailed = errors.New("logout did not finish. Run `axon logout` again.")

// skyLogoutWait bounds sky api logout, which changes local files only.
const skyLogoutWait = time.Minute

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
	dir    axondir.Dir
	client client
	// skyConfig is SkyPilot's user configuration file, where sky api
	// login stores the endpoint.
	skyConfig string
	// environ is the environment sky api logout starts from.
	environ func() []string
	// findTsh returns the path of the tsh on PATH.
	findTsh func() (string, error)
	// tshLogout ends the Teleport session for a proxy.
	tshLogout func(ctx context.Context, tshPath, proxy string) error
}

// Logout runs axon logout: it logs the SkyPilot client out, stops the
// recorded tunnel, ends the Teleport session of the connected pathway, and
// removes the records of the connection. It keeps the config files and the
// active pathway. It works by axon's own records alone, never by a config
// file.
func Logout(ctx context.Context, opts LogoutOptions) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find the home directory: %w", err)
	}
	dir := axondir.New(home)
	lo := &logout{
		dir:       dir,
		client:    skyClient{dir: dir},
		skyConfig: skyConfigPath(home, os.Getenv),
		environ:   os.Environ,
		findTsh:   tsh.Find,
		tshLogout: tsh.Logout,
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
	stateDir := filepath.Join(lo.dir.Root(), "state")

	failed := false
	fail := func(step string, err error) {
		failed = true
		fmt.Fprintf(opts.Err, "axon: could not %s: %v\n", step, err)
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
	if !failed && !hasRecord && !oldRecord && !hasEmail && !running {
		fmt.Fprintln(opts.Out, lineNothing)
		return nil
	}

	// 1. The SkyPilot login.
	if err := lo.skyLogout(ctx, opts); err != nil {
		fail("log out of SkyPilot", err)
	}

	// 2. The tunnel, by its own record.
	if running {
		fmt.Fprintf(opts.Out, "sky: stopping the Teleport tunnel on port %d\n", tunnel.Port)
		if err := tunnel.StopRecorded(stateDir); err != nil {
			fail("stop the Teleport tunnel", err)
		}
	}

	// 3. The Teleport session of the connected pathway.
	switch {
	case hasRecord && rec.Kind == string(orgfile.KindTeleport) && rec.TeleportProxy != "":
		if err := lo.teleportLogout(ctx, opts, rec.TeleportProxy); err != nil {
			fail("log out of Teleport", err)
		}
	case oldRecord:
		fmt.Fprintln(opts.Out, lineTeleportOld)
	}

	// 4. The records of the connection.
	if !failed {
		if err := lo.removeRecords(); err != nil {
			fail("remove the records of the connection", err)
		} else {
			fmt.Fprintln(opts.Out, lineRemoved)
		}
	}

	fmt.Fprintf(opts.Out, lineKept+"\n", filepath.Join(lo.dir.Root(), "orgs"))
	if failed {
		return errLogoutFailed
	}
	return nil
}

// skyLogout runs sky api logout with axon's own client, unless the client
// is not installed or stores no endpoint, in which case it says why it
// skipped. It never installs the client.
func (lo *logout) skyLogout(ctx context.Context, opts LogoutOptions) error {
	_, installed, err := lo.client.Version()
	if err != nil {
		return err
	}
	if !installed {
		fmt.Fprintln(opts.Out, lineSkyNoClient)
		return nil
	}
	// With no stored endpoint, sky api logout takes the endpoint for a
	// server on the laptop and fails.
	endpoint, err := storedEndpoint(lo.skyConfig)
	if err != nil {
		return err
	}
	if endpoint == "" {
		fmt.Fprintln(opts.Out, lineSkyNoLogin)
		return nil
	}
	sky, err := lo.client.Path()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, skyLogoutWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, sky, "api", "logout")
	// sky api logout refuses to run while the endpoint variable is set.
	cmd.Env = withoutVars(lo.environ(), endpointVar)
	cmd.WaitDelay = skyWaitDelay
	out, err := cmd.CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("sky api logout: %w: %s", err, msg)
		}
		return fmt.Errorf("sky api logout: %w", err)
	}
	fmt.Fprintln(opts.Out, lineSkyLoggedOut)
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

// skyConfigPath returns SkyPilot's user configuration file as the client
// resolves it: the file SKYPILOT_GLOBAL_CONFIG names, when it is set, and
// ~/.sky/config.yaml otherwise.
func skyConfigPath(home string, getenv func(string) string) string {
	if path := getenv("SKYPILOT_GLOBAL_CONFIG"); path != "" {
		if path == "~" || strings.HasPrefix(path, "~/") {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
		return path
	}
	return filepath.Join(home, ".sky", "config.yaml")
}
