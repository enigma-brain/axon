package tsh

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
)

const (
	dirMode fs.FileMode = 0o700

	// tshDir, under the tools directory, holds one directory per installed
	// version and currentFile.
	tshDir = "tsh"
	// currentFile names the installed version. It is written only after an
	// install succeeds, so it always names a complete install.
	currentFile = "current"
	// staleAge is how old a temporary entry under tshDir is before it is
	// taken to be left by an install that was killed, and removed.
	staleAge = time.Hour
)

// cdnBaseURL is where Teleport's release archives are downloaded from.
var cdnBaseURL = "https://cdn.teleport.dev"

// platform is the GOOS/GOARCH this binary runs on.
var platform = runtime.GOOS + "/" + runtime.GOARCH

// archivePlatforms maps the platforms axon installs tsh on to the platform
// part of Teleport's archive name.
var archivePlatforms = map[string]string{
	"linux/amd64":  "linux-amd64",
	"darwin/arm64": "darwin-arm64",
	"darwin/amd64": "darwin-amd64",
}

// bundleName is the application bundle that holds tsh on macOS. Its tsh runs
// only from inside the bundle, so the bundle is installed whole.
const bundleName = "tsh.app"

// bundleBinary is the path of tsh inside bundleName.
var bundleBinary = filepath.Join("Contents", "MacOS", "tsh")

// bundled reports whether tsh on this platform comes as bundleName.
func bundled() bool {
	return strings.HasPrefix(platform, "darwin/")
}

// shaPattern accepts a SHA-256 in hex.
var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ownPath returns the path of axon's own tsh at version under tools. It is
// the one place that knows where the binary sits inside an install.
func ownPath(tools, version string) string {
	dir := filepath.Join(tools, tshDir, version)
	if bundled() {
		return filepath.Join(dir, bundleName, bundleBinary)
	}
	return filepath.Join(dir, "tsh")
}

// installed returns axon's own tsh: its path and version. The boolean is
// false when axon has installed none.
func installed(tools string) (path, version string, ok bool, err error) {
	record := filepath.Join(tools, tshDir, currentFile)
	data, err := os.ReadFile(record)
	if errors.Is(err, fs.ErrNotExist) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("read %s: %w", record, err)
	}
	version = strings.TrimSpace(string(data))
	if !versionPattern.MatchString(version) {
		return "", "", false, fmt.Errorf("read %s: %q is not a release version", record, version)
	}
	path = ownPath(tools, version)
	if !isExecutable(path) {
		return "", "", false, nil
	}
	return path, version, true, nil
}

// canInstall reports whether axon installs tsh on this platform.
func canInstall() bool {
	_, ok := archivePlatforms[platform]
	return ok
}

// install downloads Teleport's archive at version, verifies it, and makes
// its tsh axon's own. A failed install leaves the previous one in place.
//
// Concurrent installs need no lock: each builds its own temporary
// directory, the switch to it is a rename, and an install that finds the
// version already in place keeps that one.
func install(ctx context.Context, tools, version string, progress io.Writer) (string, error) {
	arch, ok := archivePlatforms[platform]
	if !ok {
		return "", fmt.Errorf("install tsh %s: axon installs tsh only on Linux on x86-64 and on macOS, not on %s", version, platform)
	}
	root := filepath.Join(tools, tshDir)
	if err := os.MkdirAll(root, dirMode); err != nil {
		return "", fmt.Errorf("install tsh %s: %w", version, err)
	}
	removeStale(root)

	name := fmt.Sprintf("teleport-v%s-%s-bin.tar.gz", version, arch)
	url := cdnBaseURL + "/" + name
	archive, err := os.CreateTemp(root, ".archive-*")
	if err != nil {
		return "", fmt.Errorf("install tsh %s: %w", version, err)
	}
	defer os.Remove(archive.Name())
	defer archive.Close()

	if err := fetch(ctx, url, archive, version, progress); err != nil {
		return "", fmt.Errorf("download Teleport %s: %w", version, err)
	}

	fail := func(err error) (string, error) {
		return "", fmt.Errorf("install tsh %s: %w", version, err)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	staging, err := os.MkdirTemp(root, ".install-*")
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(staging)
	if bundled() {
		err = extractBundle(archive, filepath.Join(staging, bundleName))
	} else {
		err = extractTSH(archive, filepath.Join(staging, "tsh"))
	}
	if err != nil {
		return fail(err)
	}
	archive.Close()
	os.Remove(archive.Name())

	final := filepath.Join(root, version)
	if err := os.Rename(staging, final); err != nil {
		// Another install of this version finished first, or an earlier
		// one left a directory without its tsh.
		if !isExecutable(ownPath(tools, version)) {
			if err := os.RemoveAll(final); err != nil {
				return fail(err)
			}
			if err := os.Rename(staging, final); err != nil {
				return fail(err)
			}
		}
	}
	if err := replaceFile(filepath.Join(root, currentFile), strings.NewReader(version+"\n"), 0o600); err != nil {
		return fail(err)
	}
	removeOthers(root, version, currentFile)
	return ownPath(tools, version), nil
}

// fetch writes the archive at url to w, reporting its size and progress, and
// checks it against the checksum Teleport publishes beside it.
func fetch(ctx context.Context, url string, w io.Writer, version string, progress io.Writer) error {
	want, err := publishedSum(ctx, url+".sha256")
	if err != nil {
		return err
	}
	resp, err := get(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.ContentLength > 0 {
		fmt.Fprintf(progress, "Downloading Teleport %s for tsh (%d MB)\n", version, (resp.ContentLength+1<<19)>>20)
	} else {
		fmt.Fprintf(progress, "Downloading Teleport %s for tsh\n", version)
	}
	h := sha256.New()
	dst := io.MultiWriter(w, h)
	if resp.ContentLength > 0 {
		dst = io.MultiWriter(dst, &quarterReporter{total: resp.ContentLength, out: progress})
	}
	if _, err := io.Copy(dst, resp.Body); err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", url, got, want)
	}
	return nil
}

// publishedSum reads a .sha256 file: the hex digest, then the file name.
func publishedSum(ctx context.Context, url string) (string, error) {
	resp, err := get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 || !shaPattern.MatchString(strings.ToLower(fields[0])) {
		return "", fmt.Errorf("GET %s: no SHA-256 in the response", url)
	}
	return strings.ToLower(fields[0]), nil
}

// stallTimeout bounds how long a request waits for its response, and then
// for each further piece of its body. A download that keeps moving is never
// cut off, however long it takes; one that stops is.
var stallTimeout = 30 * time.Second

// get requests url and returns the response when its status is OK. The
// request is cancelled when the response, or any read of its body, waits
// longer than stallTimeout. Closing the body releases the timer.
func get(ctx context.Context, url string) (*http.Response, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	stalled := fmt.Errorf("GET %s: no data for %s", url, stallTimeout)
	timer := time.AfterFunc(stallTimeout, func() { cancel(stalled) })
	stop := func() {
		timer.Stop()
		cancel(nil)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		stop()
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		stop()
		if context.Cause(ctx) == stalled {
			return nil, stalled
		}
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		stop()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	timer.Reset(stallTimeout)
	resp.Body = &stallBody{ReadCloser: resp.Body, ctx: ctx, stalled: stalled, timer: timer, stop: stop}
	return resp, nil
}

// stallBody restarts its request's stall timer on every read that returns
// data, and reports a stall as such rather than as a cancellation.
type stallBody struct {
	io.ReadCloser
	ctx     context.Context
	stalled error
	timer   *time.Timer
	stop    func()
}

func (b *stallBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.timer.Reset(stallTimeout)
	}
	if err != nil && context.Cause(b.ctx) == b.stalled {
		return n, b.stalled
	}
	return n, err
}

func (b *stallBody) Close() error {
	err := b.ReadCloser.Close()
	b.stop()
	return err
}

// quarterReporter writes a line each time the bytes written to it pass a
// further quarter of total.
type quarterReporter struct {
	total, done int64
	reported    int
	out         io.Writer
}

func (q *quarterReporter) Write(p []byte) (int, error) {
	q.done += int64(len(p))
	for q.reported < 4 && q.done*4 >= q.total*int64(q.reported+1) {
		q.reported++
		fmt.Fprintf(q.out, "  %d%%\n", q.reported*25)
	}
	return len(p), nil
}

// extractTSH writes teleport/tsh from the archive r to dst.
func extractTSH(r io.Reader, dst string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("read the archive: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("the archive holds no teleport/tsh")
		}
		if err != nil {
			return fmt.Errorf("read the archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || path.Clean(hdr.Name) != "teleport/tsh" {
			continue
		}
		return replaceFile(dst, tr, 0o700)
	}
}

// bundlePrefix is where bundleName sits in Teleport's macOS archive.
const bundlePrefix = "teleport/" + bundleName

// extractBundle writes teleport/tsh.app from the archive r to dst. Every
// entry of the bundle has to be a directory or a regular file inside it, and
// the bundle has to hold its tsh; anything else fails the extraction.
// Entries outside the bundle are skipped.
//
// Files keep their archived modes. Directories keep theirs too, except that
// the owner can always read, write and enter them, so that a later install
// can remove them.
func extractBundle(r io.Reader, dst string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("read the archive: %w", err)
	}
	if err := os.Mkdir(dst, dirMode); err != nil {
		return err
	}
	// The archived directory modes are applied once every entry is written:
	// a directory can appear after the entries inside it, or not at all.
	dirModes := map[string]fs.FileMode{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read the archive: %w", err)
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		if name != bundlePrefix && !strings.HasPrefix(name, bundlePrefix+"/") {
			continue
		}
		rel := strings.Trim(strings.TrimPrefix(name, bundlePrefix), "/")
		if rel != "" && !filepath.IsLocal(rel) {
			return fmt.Errorf("the archive's %s holds %q, which is outside the bundle", bundleName, hdr.Name)
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		perm := hdr.FileInfo().Mode().Perm()
		switch {
		case hdr.Typeflag == tar.TypeDir:
			if err := os.MkdirAll(target, dirMode); err != nil {
				return err
			}
			dirModes[target] = perm
		case hdr.Typeflag == tar.TypeReg && rel != "":
			if err := writeNew(target, tr, perm); err != nil {
				return err
			}
		default:
			return fmt.Errorf("the archive's %s holds %q, which is not a file or a directory", bundleName, hdr.Name)
		}
	}
	if !isExecutable(filepath.Join(dst, bundleBinary)) {
		return fmt.Errorf("the archive holds no %s", path.Join(bundlePrefix, filepath.ToSlash(bundleBinary)))
	}
	for dir, perm := range dirModes {
		if err := os.Chmod(dir, perm|0o700); err != nil {
			return err
		}
	}
	return nil
}

// writeNew creates path, which must not exist, with the content of r and mode
// perm, creating its parent directories.
func writeNew(path string, r io.Reader, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Chmod, unlike OpenFile, is not reduced by the umask.
	return os.Chmod(path, perm)
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

// removeOthers removes the earlier versions in dir: every entry except those
// named in keep, the temporary entries, whose names begin with a dot, and
// any entry changed within staleAge. A recent entry may be a version that a
// concurrent install has just published, and that its caller is about to
// run; it is removed by a later install once it is old. Removal is cleanup,
// so failures are ignored.
func removeOthers(dir string, keep ...string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || slices.Contains(keep, name) {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) <= staleAge {
			continue
		}
		os.RemoveAll(filepath.Join(dir, name))
	}
}

// removeStale removes temporary entries of dir older than staleAge, which an
// install that was killed left behind. It is cleanup, so failures are
// ignored.
func removeStale(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err == nil && time.Since(info.ModTime()) > staleAge {
			os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
}
