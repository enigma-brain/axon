package skyclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/download"
)

// uvVersion is the version of uv that installs Python and the client.
const uvVersion = "0.13.7"

// uvBuild is one platform's uv release archive.
type uvBuild struct {
	// target is the archive's platform name, as in uv-<target>.tar.gz.
	target string
	// sha256 is the archive's SHA-256, in hex.
	sha256 string
}

// uvBuilds maps GOOS/GOARCH to the uv archive for that platform. The
// checksums are those uv publishes beside each archive of uvVersion. Linux
// uses the statically linked musl build, which runs whatever C library the
// distribution has.
var uvBuilds = map[string]uvBuild{
	"darwin/arm64": {
		target: "aarch64-apple-darwin",
		sha256: "81b811b57e4624df7ed3aac148e0359100d9962f3d9dce00aec533dc2e086731",
	},
	"darwin/amd64": {
		target: "x86_64-apple-darwin",
		sha256: "20c47e4c1f630b8abaff8c291fa9b9aa300fef92c4fbff95adf198b55287cda7",
	},
	"linux/amd64": {
		target: "x86_64-unknown-linux-musl",
		sha256: "287491da03f2a785c70fa75b13e1f849918c2f2b5415fe84876b8214eeb792f9",
	},
}

// ensureUV returns the path of the pinned uv under tools, downloading and
// verifying it when it is not there. Other versions of uv are removed.
func (d deps) ensureUV(ctx context.Context, tools string, progress io.Writer) (string, error) {
	uvRoot := filepath.Join(tools, "uv")
	uvDir := filepath.Join(uvRoot, uvVersion)
	uvPath := filepath.Join(uvDir, "uv")
	if isExecutable(uvPath) {
		return uvPath, nil
	}

	build, ok := d.uvBuilds[d.platform]
	if !ok {
		return "", unavailableError{fmt.Errorf("the SkyPilot client cannot be installed on %s; axon supports macOS on Apple silicon and Intel, and Linux on x86-64", d.platform)}
	}

	// The version's directory is made only once the archive has arrived
	// and is removed again, when still empty, if the install fails, so a
	// failed or interrupted download leaves no empty directory behind.
	fail := func(err error) (string, error) {
		os.Remove(uvDir)
		return "", fmt.Errorf("download uv %s: %w", uvVersion, err)
	}
	if err := os.MkdirAll(uvRoot, dirMode); err != nil {
		return fail(err)
	}

	archive, err := os.CreateTemp(uvRoot, ".uv-*.tar.gz")
	if err != nil {
		return fail(err)
	}
	defer os.Remove(archive.Name())
	defer archive.Close()

	url := fmt.Sprintf("%s/%s/uv-%s.tar.gz", d.uvBaseURL, uvVersion, build.target)
	err = d.step(progress, "uv "+uvVersion, false, func(out io.Writer) error {
		err := download.Fetch(ctx, url, archive, download.Options{
			Limit:    d.maxDownload,
			Stall:    d.stallTimeout,
			SHA256:   build.sha256,
			Progress: quarters(out),
		})
		var mismatch *download.ChecksumError
		switch {
		case errors.As(err, &mismatch):
			return fmt.Errorf("checksum mismatch for %s: got %s, want %s", url, mismatch.Got, mismatch.Want)
		case isPlain(err):
			return fmt.Errorf("GET %s: %w", url, err)
		case err != nil:
			return err
		}
		if _, err := archive.Seek(0, io.SeekStart); err != nil {
			return err
		}
		if err := os.MkdirAll(uvDir, dirMode); err != nil {
			return err
		}
		return extractUV(archive, uvPath)
	})
	if err != nil {
		return fail(err)
	}
	removeOthers(uvRoot, uvVersion)
	return uvPath, nil
}

// extractUV writes the uv executable from the release archive r to path.
func extractUV(r io.Reader, path string) error {
	uv, err := download.ExtractFile(r, "uv", maxUV)
	if errors.Is(err, download.ErrNotInArchive) {
		return errors.New("the archive holds no uv executable")
	}
	if err != nil {
		return err
	}
	return axondir.ReplaceFile(path, uv, 0o700)
}

// isPlain reports whether err is a failed request that does not already
// say what went wrong in words of its own: not a stall, a refusal for size
// or a server's answer.
func isPlain(err error) bool {
	var stalled *download.StallError
	var tooLarge *download.TooLargeError
	var status *download.StatusError
	return err != nil && !errors.As(err, &stalled) && !errors.As(err, &tooLarge) && !errors.As(err, &status)
}

// uvEnv returns the environment uv runs with: the caller's, without anything
// that points uv or Python elsewhere, and with every directory uv writes to
// placed under tools.
func uvEnv(tools string) []string {
	env := without(os.Environ(), func(key string) bool { return strings.HasPrefix(key, "UV_") })
	return append(env,
		"UV_NO_CONFIG=1",
		"UV_MANAGED_PYTHON=1",
		"UV_PYTHON_DOWNLOADS=automatic",
		"UV_CACHE_DIR="+filepath.Join(tools, "uv-cache"),
		"UV_PYTHON_INSTALL_DIR="+filepath.Join(tools, "python"),
		"UV_PYTHON_BIN_DIR="+filepath.Join(tools, "python-bin"),
		"UV_TOOL_DIR="+filepath.Join(tools, "uv-tools"),
		"UV_TOOL_BIN_DIR="+filepath.Join(tools, "uv-tools-bin"),
		"UV_CREDENTIALS_DIR="+filepath.Join(tools, "uv-credentials"),
	)
}
