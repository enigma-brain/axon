package skyclient

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// uvBaseURL is where uv's release archives are downloaded from.
var uvBaseURL = "https://github.com/astral-sh/uv/releases/download"

// platform is the GOOS/GOARCH this binary runs on.
var platform = runtime.GOOS + "/" + runtime.GOARCH

// ensureUV returns the path of the pinned uv under tools, downloading and
// verifying it when it is not there. Other versions of uv are removed.
func ensureUV(ctx context.Context, tools string, progress io.Writer) (string, error) {
	uvRoot := filepath.Join(tools, "uv")
	uvDir := filepath.Join(uvRoot, uvVersion)
	uvPath := filepath.Join(uvDir, "uv")
	if isExecutable(uvPath) {
		return uvPath, nil
	}

	build, ok := uvBuilds[platform]
	if !ok {
		return "", unavailableError{fmt.Errorf("the SkyPilot client cannot be installed on %s; axon supports macOS on Apple silicon and Intel, and Linux on x86-64", platform)}
	}

	fail := func(err error) (string, error) {
		return "", fmt.Errorf("download uv %s: %w", uvVersion, err)
	}
	if err := os.MkdirAll(uvDir, dirMode); err != nil {
		return fail(err)
	}

	archive, err := os.CreateTemp(uvRoot, ".uv-*.tar.gz")
	if err != nil {
		return fail(err)
	}
	defer os.Remove(archive.Name())
	defer archive.Close()

	url := fmt.Sprintf("%s/%s/uv-%s.tar.gz", uvBaseURL, uvVersion, build.target)
	err = step(progress, "uv "+uvVersion, false, func(out io.Writer) error {
		sum, err := download(ctx, url, archive, out)
		if err != nil {
			return err
		}
		if sum != build.sha256 {
			return fmt.Errorf("checksum mismatch for %s: got %s, want %s", url, sum, build.sha256)
		}
		if _, err := archive.Seek(0, io.SeekStart); err != nil {
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
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("read the archive: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("the archive holds no uv executable")
		}
		if err != nil {
			return fmt.Errorf("read the archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != "uv" {
			continue
		}
		return replaceFile(path, tr, 0o700)
	}
}

// replaceFile replaces path with the content of r, with mode perm. It writes
// a temporary file, flushes it to disk and renames it, so a reader never sees
// a partial file and a crash leaves either the old file or the new one.
func replaceFile(path string, r io.Reader, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	syncDir(dir)
	return nil
}

// syncDir flushes dir's entries to disk, so a rename into it survives a
// crash. Not every file system supports it, so a failure is ignored: the
// rename itself has succeeded.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	d.Sync()
	d.Close()
}

// uvEnv returns the environment uv runs with: the caller's, without anything
// that points uv or Python elsewhere, and with every directory uv writes to
// placed under tools.
func uvEnv(tools string) []string {
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(key, "UV_"),
			key == "VIRTUAL_ENV",
			key == "CONDA_PREFIX",
			key == "PYTHONHOME",
			key == "PYTHONPATH":
			continue
		}
		env = append(env, kv)
	}
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
