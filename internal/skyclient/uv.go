package skyclient

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
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
		return "", fmt.Errorf("the SkyPilot client cannot be installed on %s; axon supports macOS on Apple silicon and Intel, and Linux on x86-64", platform)
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

// download writes the body at url to w, reporting progress by quarters when
// the size is known, and returns the body's SHA-256 in hex.
func download(ctx context.Context, url string, w io.Writer, progress io.Writer) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	h := sha256.New()
	dst := io.MultiWriter(w, h)
	if resp.ContentLength > 0 {
		dst = io.MultiWriter(dst, &quarterReporter{total: resp.ContentLength, out: progress})
	}
	if _, err := io.Copy(dst, resp.Body); err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// quarterReporter writes the percentage done, on the current line, each time
// the bytes written to it pass a further quarter of total.
type quarterReporter struct {
	total, done int64
	reported    int
	out         io.Writer
}

func (q *quarterReporter) Write(p []byte) (int, error) {
	q.done += int64(len(p))
	for q.reported < 4 && q.done*4 >= q.total*int64(q.reported+1) {
		q.reported++
		fmt.Fprintf(q.out, " %d%%", q.reported*25)
	}
	return len(p), nil
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
// a temporary file and renames it, so a reader never sees a partial file.
func replaceFile(path string, r io.Reader, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
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
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
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
