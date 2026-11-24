// Package skyclient installs and locates axon's own SkyPilot client, kept
// under ~/.axon/tools/ and separate from any other on the laptop.
//
// A pinned uv, verified against its checksum, installs a uv-managed Python
// and skypilot[kubernetes] into one virtual environment per client version.
// Every directory uv writes to is under ~/.axon/tools/, so the install needs
// no Python, uv or sudo on the laptop, and removing ~/.axon removes it. The
// client is always run by absolute path, never looked up on PATH.
package skyclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/interrupt"
)

// DefaultVersion is the client version installed when no other is given.
const DefaultVersion = "0.13.0"

// pythonVersion is the Python the client runs on.
const pythonVersion = "3.12"

// skypilotPackage is the package installed at the client version.
const skypilotPackage = "skypilot[kubernetes]"

const (
	dirMode fs.FileMode = 0o700

	// skypilotDir, under the tools directory, holds one virtual environment
	// per client version, and currentFile.
	skypilotDir = "skypilot"
	// currentFile names the installed client's version. It is written only
	// after an install succeeds, so it always names a complete client.
	currentFile = "current"
)

// ErrUnavailable is matched, with errors.Is, by an install error that no
// download can fix: the platform has no build of uv, or no pre-built package
// satisfies what the client needs at the version asked for. The error's text
// is the installer's own reason; ErrUnavailable adds nothing to it.
var ErrUnavailable = errors.New("no pre-built package is available")

// unavailableError marks err as ErrUnavailable without changing its text.
type unavailableError struct{ err error }

func (e unavailableError) Error() string        { return e.err.Error() }
func (e unavailableError) Unwrap() error        { return e.err }
func (e unavailableError) Is(target error) bool { return target == ErrUnavailable }

// uvNoSolution begins the error uv prints when its resolver finds no set of
// packages that satisfies the request. With --only-binary :all: this is how
// uv reports that a package, or a version of one, has no pre-built package
// for this platform. A failed download is reported differently, so a uv
// error that holds this text is not one a retry fixes.
const uvNoSolution = "No solution found when resolving dependencies"

// versionPattern accepts a release version. It starts with a digit, so it is
// never "." or "..", and holds no separator or requirement operator.
var versionPattern = regexp.MustCompile(`^[0-9][0-9A-Za-z.+!-]*$`)

// Ensure makes sure the client is installed at version, writing progress to
// progress. It returns the version it moved from and the version it moved to;
// from is empty when no client was installed, and from equals to when
// nothing changed. An empty version means DefaultVersion. A failed install
// leaves the previously installed client in place.
//
// Concurrent calls on one axon directory, from this process or another, run
// one at a time: a call that finds another installing says so and waits.
func Ensure(ctx context.Context, dir axondir.Dir, version string, progress io.Writer) (from, to string, err error) {
	return defaults().ensure(ctx, dir, version, progress)
}

// deps are what an install depends on outside its arguments. Ensure uses
// defaults(); tests build their own.
type deps struct {
	// uvBaseURL is where uv's release archives are downloaded from.
	uvBaseURL string
	// platform is the GOOS/GOARCH this binary runs on.
	platform string
	// uvBuilds maps GOOS/GOARCH to the uv archive for that platform.
	uvBuilds map[string]uvBuild
	// maxDownload bounds the size of a download. uv's release archives
	// are a fraction of it.
	maxDownload int64
	// stallTimeout ends a download when no data arrives for this long.
	stallTimeout time.Duration
	// tickInterval is how often a running step adds a dot to its line.
	tickInterval time.Duration
}

func defaults() deps {
	return deps{
		uvBaseURL:    "https://github.com/astral-sh/uv/releases/download",
		platform:     runtime.GOOS + "/" + runtime.GOARCH,
		uvBuilds:     uvBuilds,
		maxDownload:  64 << 20,
		stallTimeout: 30 * time.Second,
		tickInterval: 5 * time.Second,
	}
}

func (d deps) ensure(ctx context.Context, dir axondir.Dir, version string, progress io.Writer) (from, to string, err error) {
	if version == "" {
		version = DefaultVersion
	}
	if !versionPattern.MatchString(version) {
		return "", "", fmt.Errorf("install SkyPilot: %q is not a release version", version)
	}
	if progress == nil {
		progress = io.Discard
	}

	tools, err := dir.ToolsDir()
	if err != nil {
		return "", "", err
	}
	// The lock is held from the version check to the cleanup, so a call
	// that waited sees what the call before it installed.
	unlock, err := lockTools(ctx, tools, progress)
	if err != nil {
		return "", "", err
	}
	defer unlock()

	from, installed, err := Version(dir)
	if err != nil {
		return "", "", err
	}
	if installed && from == version {
		return from, version, nil
	}

	fmt.Fprintf(progress, "sky: installing the SkyPilot client %s, with its own uv and Python, into %s.\n", version, tools)
	fmt.Fprintln(progress, "sky: your system's Python and packages are not changed.")

	uv, err := d.ensureUV(ctx, tools, progress)
	if err != nil {
		return "", "", err
	}
	env := uvEnv(tools)

	err = d.step(progress, "Python "+pythonVersion, true, func(io.Writer) error {
		return runUV(ctx, uv, env, "python", "install", pythonVersion, "--no-bin")
	})
	if err != nil {
		return "", "", fmt.Errorf("install Python %s: %w", pythonVersion, err)
	}

	err = d.step(progress, "SkyPilot "+version, true, func(io.Writer) error {
		return installClient(ctx, uv, env, tools, version)
	})
	if err != nil {
		return "", "", fmt.Errorf("install SkyPilot %s (%s): %w", version, skypilotPackage, err)
	}
	fmt.Fprintf(progress, "sky: installed the SkyPilot client %s\n", version)
	return from, version, nil
}

// installClient builds the virtual environment for version, records it as
// the installed client, and removes every other version. The environment is
// built at its final path, since a virtual environment's scripts name its
// absolute path; the record is what switches from one client to the next.
func installClient(ctx context.Context, uv string, env []string, tools, version string) error {
	root := filepath.Join(tools, skypilotDir)
	venv := filepath.Join(root, version)
	if err := os.MkdirAll(root, dirMode); err != nil {
		return err
	}
	// Anything at venv is a partial install from an earlier attempt, or a
	// client whose sky is missing.
	if err := os.RemoveAll(venv); err != nil {
		return err
	}
	if err := runUV(ctx, uv, env, "venv", "--python", pythonVersion, venv); err != nil {
		return err
	}
	python := filepath.Join(venv, "bin", "python")
	// Pre-built packages only: a package with no build for this machine is
	// taken at its newest version that has one, and none is ever compiled,
	// so the install needs no compiler on the laptop.
	if err := runUV(ctx, uv, env, "pip", "install", "--python", python, skypilotPackage+"=="+version, "--only-binary", ":all:"); err != nil {
		return err
	}
	if !isExecutable(filepath.Join(venv, "bin", "sky")) {
		return errors.New("the package installed no sky executable")
	}
	// A rename cannot replace a directory, so a directory where the record
	// belongs is removed first.
	current := filepath.Join(root, currentFile)
	if info, err := os.Lstat(current); err == nil && info.IsDir() {
		if err := os.RemoveAll(current); err != nil {
			return err
		}
	}
	if err := writeFile(current, version+"\n"); err != nil {
		return err
	}
	removeOthers(root, version, currentFile)
	return nil
}

// Version returns the version of the installed client without installing
// anything. The boolean is false when no client is installed. A record that
// is missing, cannot be read, or names no release version is no client, so
// that an install replaces it.
func Version(dir axondir.Dir) (version string, installed bool, err error) {
	// A read creates no directory.
	path := filepath.Join(dir.ToolsPath(), skypilotDir, currentFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, nil
	}
	version = strings.TrimSpace(string(data))
	if !versionPattern.MatchString(version) {
		return "", false, nil
	}
	if !isExecutable(skyPath(dir.ToolsPath(), version)) {
		return "", false, nil
	}
	return version, true, nil
}

// ErrNotInstalled reports that axon has not installed its SkyPilot client.
var ErrNotInstalled = errors.New("the SkyPilot client is not installed. Run `axon login`")

// Path returns the absolute path of the client's sky executable. It
// creates no directory.
func Path(dir axondir.Dir) (string, error) {
	version, installed, err := Version(dir)
	if err != nil {
		return "", err
	}
	if !installed {
		return "", ErrNotInstalled
	}
	return PathFor(dir, version), nil
}

// PathFor returns the absolute path of the sky executable of the client at
// version, as Version reported it.
func PathFor(dir axondir.Dir, version string) string {
	return skyPath(dir.ToolsPath(), version)
}

func skyPath(tools, version string) string {
	return filepath.Join(tools, skypilotDir, version, "bin", "sky")
}

// runUV runs uv quietly with args, so that it writes nothing but warnings
// and errors, and keeps none of its output. On failure the error carries
// what uv wrote to standard error, and is ErrUnavailable when uv found no
// pre-built package that satisfies the request.
func runUV(ctx context.Context, uv string, env []string, args ...string) error {
	var stderr tail
	cmd := exec.CommandContext(ctx, uv, append(args, "--quiet")...)
	interrupt.Stop(ctx, cmd, uvWaitDelay)
	cmd.Env = env
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if text := stderr.text(); text != "" {
			err = fmt.Errorf("uv %s: %w: %s", args[0], err, text)
			if stderr.noSolution {
				return unavailableError{err}
			}
			return err
		}
		return fmt.Errorf("uv %s: %w", args[0], err)
	}
	return nil
}

// uvWaitDelay bounds how long axon waits for uv to end after an interrupt,
// and for its output once it has.
const uvWaitDelay = 5 * time.Second

// tail keeps the last few kilobytes written to it, and notes whether
// uvNoSolution was ever written, since a long explanation from uv's resolver
// can push the line that holds it out of what is kept.
type tail struct {
	buf []byte
	// noSolution is set once uvNoSolution has been written.
	noSolution bool
	// edge holds the end of what was written, one byte shorter than
	// uvNoSolution, so that a match split across writes is found.
	edge []byte
}

const tailSize = 4096

func (t *tail) Write(p []byte) (int, error) {
	if !t.noSolution {
		t.edge = append(t.edge, p...)
		if bytes.Contains(t.edge, []byte(uvNoSolution)) {
			t.noSolution, t.edge = true, nil
		} else if keep := len(uvNoSolution) - 1; len(t.edge) > keep {
			t.edge = append([]byte(nil), t.edge[len(t.edge)-keep:]...)
		}
	}
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailSize {
		t.buf = t.buf[len(t.buf)-tailSize:]
	}
	return len(p), nil
}

// text returns what was kept, trimmed, with every line after the first
// indented so that it reads as part of the line it follows.
func (t *tail) text() string {
	return strings.ReplaceAll(strings.TrimSpace(string(t.buf)), "\n", "\n    ")
}

// removeOthers removes every entry of dir except those named in keep. It is
// cleanup: a failure leaves only unused files behind, so it is ignored.
func removeOthers(dir string, keep ...string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !slices.Contains(keep, e.Name()) {
			os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}

// writeFile replaces path with data, readable by the user only.
func writeFile(path, data string) error {
	return axondir.ReplaceFile(path, strings.NewReader(data), 0o600)
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
}
