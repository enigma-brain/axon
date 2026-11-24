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
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/download"
	"github.com/enigma-brain/axon/internal/interrupt"
	"github.com/enigma-brain/axon/internal/netreason"
	"github.com/enigma-brain/axon/internal/termtext"
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
	// runTimeout bounds the run of the new binary's --version.
	runTimeout = 30 * time.Second
)

var (
	// maxBinary bounds the size of the axon binary inside the archive.
	maxBinary int64 = 512 << 20
	// stallTimeout ends a download when no data arrives for this long.
	stallTimeout = download.DefaultStall
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
	client := download.Client()

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
	fmt.Fprintf(opts.Out, "Downloading axon %s for %s from %s\n", version, platform, download.Redact(base))

	retry := "Check your network connection and run `axon update` again."
	if opts.Version != "" {
		retry = fmt.Sprintf("Check your network connection and run `axon update --version %s` again.", opts.Version)
	}

	var sums bytes.Buffer
	err = fetch(ctx, release.JoinPath("checksums.txt"), &sums, maxChecksums, "")
	var status *download.StatusError
	if errors.As(err, &status) && status.Code == http.StatusNotFound {
		if opts.Version != "" {
			return fmt.Errorf("there is no release %s of axon. Check the version, and find the releases at %s/releases. %s", tag, download.Redact(base), unchanged)
		}
		return fmt.Errorf("release %s has no checksums.txt, so its archive cannot be verified. %s", tag, unchanged)
	}
	if err != nil {
		return fmt.Errorf("could not download checksums.txt of release %s: %v. %s %s", tag, err, unchanged, retry)
	}
	want, ok := checksumFor(sums.Bytes(), name)
	if !ok {
		return fmt.Errorf("release %s has no archive for %s, so it cannot be installed on this machine. %s", tag, platform, unchanged)
	}

	var archive bytes.Buffer
	err = fetch(ctx, release.JoinPath(name), &archive, maxArchive, want)
	var mismatch *download.ChecksumError
	if errors.As(err, &mismatch) {
		return fmt.Errorf("the checksum of %s does not match checksums.txt of release %s (expected %s, got %s). Nothing was changed: %s Run `axon update` again, and report the release if it fails the same way.", name, tag, mismatch.Want, mismatch.Got, unchanged)
	}
	if err != nil {
		return fmt.Errorf("could not download %s of release %s: %v. %s %s", name, tag, err, unchanged, retry)
	}

	dir := filepath.Dir(exe)
	staged, err := stage(archive.Bytes(), dir)
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
	if err == nil && u.Host != "" && (u.Scheme == "https" || (u.Scheme == "http" && download.IsLoopback(u))) {
		return u, nil
	}
	return nil, fmt.Errorf("this axon was built with %q as its release address, which is neither https nor the loopback address. Install a release with the install line:\n\n    %s", raw, InstallLine)
}

// maxRedirects is the most redirects latestTag follows to find the latest
// release's tag.
const maxRedirects = 10

// latestTag returns the tag of the latest release: the one that
// <repo>/releases/latest redirects to, through redirects on the same host
// when the repository has moved. GitHub never serves a pre-release as the
// latest.
func latestTag(ctx context.Context, client *http.Client, base *url.URL) (string, error) {
	latest := base.JoinPath("releases", "latest")
	noRelease := fmt.Errorf("no release of axon was found at %s. Name one with --version, such as `axon update --version 1.2.3`.", download.Redact(latest))

	// Follow the redirects one at a time, on the host of latest, until one
	// names a release tag: a repository that was renamed or moved
	// redirects to its new address first.
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	next := latest
	var target *url.URL
	for hops := 0; ; hops++ {
		if hops == maxRedirects {
			return "", noRelease
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next.String(), nil)
		if err != nil {
			return "", err
		}
		resp, err := c.Do(req)
		if err != nil {
			return "", fmt.Errorf("could not look up the latest release at %s: %v. Check your network connection and run `axon update` again.", download.Redact(latest), netError{err})
		}
		resp.Body.Close()
		loc := resp.Header.Get("Location")
		if resp.StatusCode < 300 || resp.StatusCode > 399 || loc == "" {
			if resp.StatusCode >= 500 {
				return "", fmt.Errorf("could not look up the latest release at %s: %s. Run `axon update` again later.", download.Redact(latest), termtext.Clean(resp.Status))
			}
			return "", noRelease
		}
		u, err := next.Parse(loc)
		if err != nil || u.Scheme != latest.Scheme || u.Host != latest.Host {
			return "", noRelease
		}
		if strings.Contains(u.Path, "/releases/tag/") {
			target = u
			break
		}
		next = u
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

// fetch downloads u into w, reading at most limit bytes, and checks the
// SHA-256 sum when it is not empty. A failed request's error is its short
// reason, such as "connection refused" or "timed out", since the messages
// already name what was requested.
func fetch(ctx context.Context, u *url.URL, w io.Writer, limit int64, sum string) error {
	err := download.Fetch(ctx, u.String(), w, download.Options{Limit: limit, Stall: stallTimeout, SHA256: sum})
	var status *download.StatusError
	var tooLarge *download.TooLargeError
	var mismatch *download.ChecksumError
	if err == nil || errors.As(err, &status) || errors.As(err, &tooLarge) || errors.As(err, &mismatch) {
		return err
	}
	return netError{err}
}

// netError is a failed request. Its message is the short reason, such as
// "connection refused", since the messages already name what was requested.
type netError struct{ err error }

func (e netError) Error() string { return netreason.Short(e.err) }

func (e netError) Unwrap() error { return e.err }

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
	bin, err := download.ExtractFile(bytes.NewReader(archive), "axon", maxBinary)
	if errors.Is(err, download.ErrNotInArchive) {
		return "", errNoBinary{}
	}
	if err != nil {
		return "", err
	}
	// Reserve a name no other update uses, then write the binary to it.
	f, err := os.CreateTemp(dir, ".axon-update-*")
	if err != nil {
		return "", err
	}
	staged := f.Name()
	if err := f.Close(); err != nil {
		os.Remove(staged)
		return "", err
	}
	if err := axondir.ReplaceFile(staged, bin, 0o755); err != nil {
		os.Remove(staged)
		return "", err
	}
	return staged, nil
}

// versionWaitDelay bounds how long update waits for the downloaded axon to
// end after an interrupt, and for its output once it has.
const versionWaitDelay = 2 * time.Second

// runVersion runs bin --version and returns what it printed, trimmed.
func runVersion(ctx context.Context, bin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--version")
	interrupt.Stop(ctx, cmd, versionWaitDelay)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
