// Package update replaces the running axon binary with a release of axon.
//
// It follows the rules install.sh follows: the latest release is the tag
// that <repo>/releases/latest redirects to, a release's archive for a
// platform is axon_<version>_<os>_<arch>.tar.gz, and the archive's SHA-256
// is listed in the release's checksums.txt. The new binary is written beside
// the running one, checked to run and to report the expected version, and
// renamed over the running one, so axon is replaced in one step or not at
// all.
package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// repoURL is the address of axon's repository, under which its releases are
// found. A test build sets it with
//
//	-ldflags "-X github.com/enigma-brain/axon/internal/update.repoURL=<address>"
//
// to update from files served on the loopback address.
var repoURL = "https://github.com/enigma-brain/axon"

// InstallLine is the line that installs the latest release with install.sh.
const InstallLine = "curl -fsSL https://github.com/enigma-brain/axon/releases/latest/download/install.sh | sh"

// releasedPlatforms are the platforms a release has an archive for, as
// <os>_<arch>.
var releasedPlatforms = []string{"darwin_arm64", "darwin_amd64", "linux_amd64"}

const (
	// maxChecksums bounds the size of checksums.txt.
	maxChecksums = 1 << 20
	// maxArchive bounds the size of a release archive.
	maxArchive = 256 << 20
	// maxBinary bounds the size of the axon binary inside the archive.
	maxBinary = 512 << 20
	// requestTimeout bounds each request, from sending it to reading the
	// last byte of the answer, so a stalled download ends.
	requestTimeout = 5 * time.Minute
	// runTimeout bounds the run of the new binary's --version.
	runTimeout = 30 * time.Second
)

// Options are the inputs of an update.
type Options struct {
	// Current is the version of the running axon, without a leading v.
	Current string
	// Version names the release to install, with or without its leading v.
	// Empty means the latest release.
	Version string
	// Exe is the path of the binary to replace. Empty means the running
	// executable.
	Exe string
	// Platform is <os>_<arch>. Empty means the platform axon runs on.
	Platform string
	// Out receives the lines that report progress and the outcome.
	Out io.Writer
}

// Run updates the binary at opts.Exe, or the running executable, to the
// release opts.Version names, or to the latest release. On any failure the
// binary is left as it was.
func Run(ctx context.Context, opts Options) error {
	current := opts.Current
	if !semver.IsValid("v" + current) {
		return fmt.Errorf("this axon is a development build (%s), and `axon update` updates only a released axon. Install a release with the install line:\n\n    %s", current, InstallLine)
	}
	platform := opts.Platform
	if platform == "" {
		platform = runtime.GOOS + "_" + runtime.GOARCH
	}
	unchanged := fmt.Sprintf("axon %s is unchanged.", current)

	base, err := parseRepoURL(repoURL)
	if err != nil {
		return err
	}
	client := newClient(base)

	var tag string
	if opts.Version != "" {
		tag = opts.Version
		if !strings.HasPrefix(tag, "v") {
			tag = "v" + tag
		}
		if !semver.IsValid(tag) || semver.Canonical(tag) != tag {
			return fmt.Errorf("%q is not a release version. Name one such as 1.2.3 or 1.2.3-rc.1.", opts.Version)
		}
	} else {
		tag, err = latestTag(ctx, client, base)
		if err != nil {
			return err
		}
	}
	version := strings.TrimPrefix(tag, "v")

	switch cmp := semver.Compare(tag, "v"+current); {
	case cmp == 0 && opts.Version != "":
		fmt.Fprintf(opts.Out, "axon %s is already installed. Nothing changed.\n", current)
		return nil
	case cmp == 0:
		fmt.Fprintf(opts.Out, "axon %s is the latest release, and is already installed. Nothing changed.\n", current)
		return nil
	case cmp < 0 && opts.Version == "":
		fmt.Fprintf(opts.Out, "axon %s is newer than the latest release, %s. Nothing changed. To install %s, run `axon update --version %s`.\n", current, version, version, version)
		return nil
	}

	if !released(platform) {
		return fmt.Errorf("axon is not released for %s; it is released for macOS on Apple silicon, macOS on Intel and Linux on x86-64. %s", platform, unchanged)
	}

	exe := opts.Exe
	if exe == "" {
		if exe, err = os.Executable(); err != nil {
			return fmt.Errorf("could not find the path of the running axon: %v. %s Update it with the install line:\n\n    %s", err, unchanged, InstallLine)
		}
	}
	// Replace the file a symlink points to, not the symlink, so the
	// rename stays within the file's own directory.
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return fmt.Errorf("could not find the path of the running axon: %v. %s Update it with the install line:\n\n    %s", err, unchanged, InstallLine)
	}

	name := fmt.Sprintf("axon_%s_%s.tar.gz", version, platform)
	release := base.JoinPath("releases", "download", tag)
	fmt.Fprintf(opts.Out, "Downloading axon %s for %s from %s\n", version, platform, redact(base))

	retry := "Check your network connection and run `axon update` again."
	if opts.Version != "" {
		retry = fmt.Sprintf("Check your network connection and run `axon update --version %s` again.", opts.Version)
	}

	sums, status, err := get(ctx, client, release.JoinPath("checksums.txt"), maxChecksums)
	if status == http.StatusNotFound {
		if opts.Version != "" {
			return fmt.Errorf("there is no release %s of axon. Check the version, and find the releases at %s/releases. %s", tag, redact(base), unchanged)
		}
		return fmt.Errorf("release %s has no checksums.txt, so its archive cannot be verified. %s", tag, unchanged)
	}
	if err != nil {
		return fmt.Errorf("could not download checksums.txt of release %s: %v. %s %s", tag, err, unchanged, retry)
	}
	want, ok := checksumFor(sums, name)
	if !ok {
		return fmt.Errorf("release %s has no archive for %s, so it cannot be installed on this machine. %s", tag, platform, unchanged)
	}

	archive, _, err := get(ctx, client, release.JoinPath(name), maxArchive)
	if err != nil {
		return fmt.Errorf("could not download %s of release %s: %v. %s %s", name, tag, err, unchanged, retry)
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("the checksum of %s does not match checksums.txt of release %s (expected %s, got %s). Nothing was changed: %s Run `axon update` again, and report the release if it fails the same way.", name, tag, want, got, unchanged)
	}

	dir := filepath.Dir(exe)
	staged, err := stage(archive, dir)
	if err != nil {
		var noBinary errNoBinary
		if errors.As(err, &noBinary) {
			return fmt.Errorf("%s of release %s holds no axon binary. %s", name, tag, unchanged)
		}
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("cannot write to %s, where axon is installed: %v. %s Run `axon update` as a user who can write there, or update with the install line:\n\n    %s", dir, err, unchanged, InstallLine)
		}
		return fmt.Errorf("could not unpack %s into %s: %v. %s", name, dir, err, unchanged)
	}
	defer os.Remove(staged)

	reported, err := runVersion(ctx, staged)
	if err != nil {
		return fmt.Errorf("the downloaded axon %s does not run in %s: %v. %s Check that %s is on a filesystem that allows running programs.", version, dir, err, unchanged, dir)
	}
	if want := "axon " + version; reported != want {
		return fmt.Errorf("the downloaded axon %s reports %q, not %q, so it was not installed. %s Report the release.", version, reported, want, unchanged)
	}
	if err := os.Rename(staged, exe); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("cannot replace %s: %v. %s Run `axon update` as a user who can write there, or update with the install line:\n\n    %s", exe, err, unchanged, InstallLine)
		}
		return fmt.Errorf("could not replace %s: %v. %s", exe, err, unchanged)
	}
	fmt.Fprintf(opts.Out, "Updated axon from %s to %s in %s\n", current, version, exe)
	return nil
}

// parseRepoURL checks the repository address. It is https, or http on the
// loopback address, which only a test build sets.
func parseRepoURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err == nil && u.Host != "" && (u.Scheme == "https" || (u.Scheme == "http" && isLoopback(u))) {
		return u, nil
	}
	return nil, fmt.Errorf("this axon was built with %q as its release address, which is neither https nor the loopback address. Install a release with the install line:\n\n    %s", raw, InstallLine)
}

// isLoopback reports whether u's host is a loopback IP address.
func isLoopback(u *url.URL) bool {
	ip := net.ParseIP(u.Hostname())
	return ip != nil && ip.IsLoopback()
}

// allowed reports whether a request to u may be sent: https always, and
// http only to the loopback address when the repository address is itself
// http on the loopback address.
func allowed(base, u *url.URL) bool {
	switch u.Scheme {
	case "https":
		return true
	case "http":
		return base.Scheme == "http" && isLoopback(u)
	}
	return false
}

// redact returns u without any user information, for messages.
func redact(u *url.URL) string {
	c := *u
	c.User = nil
	return c.String()
}

// newClient returns a client that follows redirects only to allowed
// addresses and bounds each request.
func newClient(base *url.URL) *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   requestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			if !allowed(base, req.URL) {
				return fmt.Errorf("redirected to %s, which is not https", redact(req.URL))
			}
			return nil
		},
	}
}

// latestTag returns the tag of the latest release: the one that
// <repo>/releases/latest redirects to. GitHub never serves a pre-release as
// the latest.
func latestTag(ctx context.Context, client *http.Client, base *url.URL) (string, error) {
	latest := base.JoinPath("releases", "latest")
	noRelease := fmt.Errorf("no release of axon was found at %s. Name one with --version, such as `axon update --version 1.2.3`.", redact(latest))

	// Read the first redirect, and do not follow it.
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latest.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not look up the latest release at %s: %v. Check your network connection and run `axon update` again.", redact(latest), unwrapURLError(err))
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode < 300 || resp.StatusCode > 399 || loc == "" {
		if resp.StatusCode >= 500 {
			return "", fmt.Errorf("could not look up the latest release at %s: %s. Run `axon update` again later.", redact(latest), resp.Status)
		}
		return "", noRelease
	}
	target, err := latest.Parse(loc)
	if err != nil {
		return "", noRelease
	}
	_, tag, ok := strings.Cut(target.Path, "/releases/tag/")
	if !ok || tag == "" || strings.Contains(tag, "/") {
		return "", noRelease
	}
	if !semver.IsValid(tag) {
		return "", fmt.Errorf("the latest release's tag, %q, is not a version. Name a release with --version, such as `axon update --version 1.2.3`.", tag)
	}
	return tag, nil
}

// get downloads u, reading at most limit bytes, and returns the body and the
// HTTP status. A status other than 200 is an error.
func get(ctx context.Context, client *http.Client, u *url.URL, limit int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, unwrapURLError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("%s answered %s", redact(u), resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if int64(len(body)) > limit {
		return nil, resp.StatusCode, fmt.Errorf("%s is larger than %d bytes", redact(u), limit)
	}
	return body, resp.StatusCode, nil
}

// unwrapURLError drops the method and address that *url.Error adds, which
// the messages already name.
func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// checksumFor returns the hex SHA-256 that checksums.txt lists for name. A
// line is "<sum>  <name>", or "<sum> *<name>" for a binary-mode entry.
func checksumFor(sums []byte, name string) (string, bool) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		if fields[1] == name || fields[1] == "*"+name {
			return strings.ToLower(fields[0]), true
		}
	}
	return "", false
}

func released(platform string) bool {
	for _, p := range releasedPlatforms {
		if p == platform {
			return true
		}
	}
	return false
}

// errNoBinary is returned when the archive holds no axon binary.
type errNoBinary struct{}

func (errNoBinary) Error() string { return "the archive holds no axon binary" }

// stage writes the axon binary from the gzipped tar archive to a new hidden
// file in dir, with mode 0755, and returns the file's path. Only the first
// regular file named axon, whatever directory the archive puts it in, is
// written; every other entry is ignored. On failure nothing is left in dir.
func stage(archive []byte, dir string) (string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return "", fmt.Errorf("read the archive: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return "", errNoBinary{}
		}
		if err != nil {
			return "", fmt.Errorf("read the archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || path.Base(hdr.Name) != "axon" {
			continue
		}
		return writeStaged(dir, tr)
	}
}

// writeStaged copies r into a new hidden file in dir, with mode 0755.
func writeStaged(dir string, r io.Reader) (staged string, err error) {
	f, err := os.CreateTemp(dir, ".axon-update-*")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(f.Name())
		}
	}()
	n, err := io.Copy(f, io.LimitReader(r, maxBinary+1))
	if err != nil {
		return "", err
	}
	if n > maxBinary {
		return "", fmt.Errorf("the axon binary in the archive is larger than %d bytes", maxBinary)
	}
	if err := f.Chmod(0o755); err != nil {
		return "", err
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// runVersion runs bin --version and returns what it printed, trimmed.
func runVersion(ctx context.Context, bin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
