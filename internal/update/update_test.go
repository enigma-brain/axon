package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The tests serve made-up releases from an httptest server on the loopback
// address, and point repoURL at it. No test reaches a real server.

const testPlatform = "linux_amd64"

// fakeBinary is a shell script that answers --version as axon of version
// would.
func fakeBinary(version string) []byte {
	return []byte(fmt.Sprintf("#!/bin/sh\necho \"axon %s\"\n", version))
}

// tarGz returns a gzipped tar archive holding the named regular files.
func tarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeRepo serves releases the way GitHub does: /releases/latest redirects to
// /releases/tag/<latest>, and /releases/download/<tag>/<file> serves a file.
type fakeRepo struct {
	latest string
	// files maps "<tag>/<name>" to the file's content.
	files map[string][]byte
	// requests counts the requests served.
	requests int
}

func (f *fakeRepo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.requests++
	if r.URL.Path == "/org/axon/releases/latest" {
		if f.latest == "" {
			http.Redirect(w, r, "/org/axon/releases", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/org/axon/releases/tag/"+f.latest, http.StatusFound)
		return
	}
	if rest, ok := strings.CutPrefix(r.URL.Path, "/org/axon/releases/download/"); ok {
		if body, ok := f.files[rest]; ok {
			w.Write(body)
			return
		}
	}
	http.NotFound(w, r)
}

// addRelease adds a release of version with an archive for testPlatform
// holding a fake axon of that version, and its checksums.txt.
func (f *fakeRepo) addRelease(t *testing.T, version string) {
	t.Helper()
	name := fmt.Sprintf("axon_%s_%s.tar.gz", version, testPlatform)
	archive := tarGz(t, map[string][]byte{"axon": fakeBinary(version), "LICENSE": []byte("MIT")})
	f.setArchive(version, name, archive, archive)
}

// setArchive serves archive as name in release version, with checksums.txt
// listing the checksum of listed.
func (f *fakeRepo) setArchive(version, name string, archive, listed []byte) {
	sum := sha256.Sum256(listed)
	tag := "v" + version
	if f.files == nil {
		f.files = map[string][]byte{}
	}
	f.files[tag+"/"+name] = archive
	f.files[tag+"/checksums.txt"] = []byte(fmt.Sprintf("%s  %s\n0000  axon_%s_other_arch.tar.gz\n", hex.EncodeToString(sum[:]), name, version))
}

// serve starts repo and points repoURL at it for the test.
func serve(t *testing.T, repo http.Handler) {
	t.Helper()
	srv := httptest.NewServer(repo)
	t.Cleanup(srv.Close)
	old := repoURL
	repoURL = srv.URL + "/org/axon"
	t.Cleanup(func() { repoURL = old })
}

// installed writes a fake axon of version into a new directory and returns
// its path.
func installed(t *testing.T, version string) string {
	t.Helper()
	// Resolve the directory's symlinks, as Run does, so messages compare
	// equal on macOS, where the temporary directory is under a symlink.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "axon")
	if err := os.WriteFile(exe, fakeBinary(version), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func runUpdate(t *testing.T, current, version, exe string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Run(context.Background(), Options{
		Current:  current,
		Version:  version,
		Exe:      exe,
		Platform: testPlatform,
		Out:      &out,
	})
	return out.String(), err
}

// assertUnchanged checks that exe is still the fake axon of version and that
// its directory holds nothing else.
func assertUnchanged(t *testing.T, exe, version string) {
	t.Helper()
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, fakeBinary(version)) {
		t.Errorf("%s was changed to %q", exe, got)
	}
	assertOnlyAxon(t, filepath.Dir(exe))
}

// assertOnlyAxon checks that dir holds the file axon and nothing else, so
// no staged file was left behind.
func assertOnlyAxon(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "axon" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("%s holds %v, want only axon", dir, names)
	}
}

func assertInstalled(t *testing.T, exe, version string) {
	t.Helper()
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, fakeBinary(version)) {
		t.Errorf("%s holds %q, want the axon of %s", exe, got, version)
	}
	info, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("%s has mode %v, want 0755", exe, info.Mode().Perm())
	}
	assertOnlyAxon(t, filepath.Dir(exe))
}

func wantErr(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil, want one containing %q", parts)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("err = %q, want it to contain %q", err, p)
		}
	}
}

func TestUpdateToNewerLatest(t *testing.T) {
	repo := &fakeRepo{latest: "v0.4.0"}
	repo.addRelease(t, "0.4.0")
	serve(t, repo)
	exe := installed(t, "0.3.0")

	out, err := runUpdate(t, "0.3.0", "", exe)
	if err != nil {
		t.Fatal(err)
	}
	assertInstalled(t, exe, "0.4.0")
	if want := "Updated axon from 0.3.0 to 0.4.0 in " + exe; !strings.Contains(out, want) {
		t.Errorf("output %q does not contain %q", out, want)
	}
}

func TestUpdateFromReleaseCandidateToFinal(t *testing.T) {
	repo := &fakeRepo{latest: "v0.4.0"}
	repo.addRelease(t, "0.4.0")
	serve(t, repo)
	exe := installed(t, "0.4.0-rc.1")

	out, err := runUpdate(t, "0.4.0-rc.1", "", exe)
	if err != nil {
		t.Fatal(err)
	}
	assertInstalled(t, exe, "0.4.0")
	if !strings.Contains(out, "from 0.4.0-rc.1 to 0.4.0") {
		t.Errorf("output %q does not name both versions", out)
	}
}

func TestAlreadyLatest(t *testing.T) {
	repo := &fakeRepo{latest: "v0.4.0"}
	repo.addRelease(t, "0.4.0")
	serve(t, repo)
	exe := installed(t, "0.4.0")

	out, err := runUpdate(t, "0.4.0", "", exe)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "axon 0.4.0 is the latest release, and is already installed. Nothing changed.") {
		t.Errorf("output = %q", out)
	}
	assertUnchanged(t, exe, "0.4.0")
	// Only the lookup of the latest release was made.
	if repo.requests != 1 {
		t.Errorf("served %d requests, want 1", repo.requests)
	}
}

func TestNewerThanLatest(t *testing.T) {
	repo := &fakeRepo{latest: "v0.4.0"}
	repo.addRelease(t, "0.4.0")
	serve(t, repo)
	exe := installed(t, "0.5.0-rc.1")

	out, err := runUpdate(t, "0.5.0-rc.1", "", exe)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "newer than the latest release, 0.4.0. Nothing changed.") ||
		!strings.Contains(out, "axon update --version 0.4.0") {
		t.Errorf("output = %q", out)
	}
	assertUnchanged(t, exe, "0.5.0-rc.1")
}

func TestNamedVersion(t *testing.T) {
	for _, arg := range []string{"0.5.0-rc.1", "v0.5.0-rc.1"} {
		t.Run(arg, func(t *testing.T) {
			repo := &fakeRepo{latest: "v0.4.0"}
			repo.addRelease(t, "0.4.0")
			repo.addRelease(t, "0.5.0-rc.1")
			serve(t, repo)
			exe := installed(t, "0.4.0")

			if _, err := runUpdate(t, "0.4.0", arg, exe); err != nil {
				t.Fatal(err)
			}
			assertInstalled(t, exe, "0.5.0-rc.1")
		})
	}
}

func TestNamedOlderVersion(t *testing.T) {
	repo := &fakeRepo{latest: "v0.4.0"}
	repo.addRelease(t, "0.3.0")
	serve(t, repo)
	exe := installed(t, "0.4.0")

	if _, err := runUpdate(t, "0.4.0", "0.3.0", exe); err != nil {
		t.Fatal(err)
	}
	assertInstalled(t, exe, "0.3.0")
}

func TestNamedVersionAlreadyInstalled(t *testing.T) {
	repo := &fakeRepo{}
	serve(t, repo)
	exe := installed(t, "0.4.0")

	out, err := runUpdate(t, "0.4.0", "v0.4.0", exe)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "axon 0.4.0 is already installed. Nothing changed.") {
		t.Errorf("output = %q", out)
	}
	if repo.requests != 0 {
		t.Errorf("served %d requests, want 0", repo.requests)
	}
}

func TestNamedVersionNotFound(t *testing.T) {
	serve(t, &fakeRepo{})
	exe := installed(t, "0.4.0")

	_, err := runUpdate(t, "0.4.0", "9.9.9", exe)
	wantErr(t, err, "there is no release v9.9.9 of axon", "axon 0.4.0 is unchanged.")
	assertUnchanged(t, exe, "0.4.0")
}

func TestInvalidVersion(t *testing.T) {
	serve(t, &fakeRepo{})
	exe := installed(t, "0.4.0")

	for _, arg := range []string{"latest", "1.2", "v1.2.3+meta"} {
		_, err := runUpdate(t, "0.4.0", arg, exe)
		wantErr(t, err, "is not a release version")
	}
	assertUnchanged(t, exe, "0.4.0")
}

func TestDevelopmentBuild(t *testing.T) {
	repo := &fakeRepo{latest: "v0.4.0"}
	repo.addRelease(t, "0.4.0")
	serve(t, repo)
	exe := installed(t, "dev")

	for _, arg := range []string{"", "0.4.0"} {
		_, err := runUpdate(t, "dev", arg, exe)
		wantErr(t, err, "development build (dev)", InstallLine)
	}
	assertUnchanged(t, exe, "dev")
	if repo.requests != 0 {
		t.Errorf("served %d requests, want 0", repo.requests)
	}
}

func TestNoRelease(t *testing.T) {
	serve(t, &fakeRepo{})
	exe := installed(t, "0.4.0")

	_, err := runUpdate(t, "0.4.0", "", exe)
	wantErr(t, err, "no release of axon was found", "--version")
	assertUnchanged(t, exe, "0.4.0")
}

func TestChecksumMismatch(t *testing.T) {
	repo := &fakeRepo{latest: "v0.5.0"}
	name := "axon_0.5.0_" + testPlatform + ".tar.gz"
	archive := tarGz(t, map[string][]byte{"axon": fakeBinary("0.5.0")})
	repo.setArchive("0.5.0", name, archive, []byte("something else"))
	serve(t, repo)
	exe := installed(t, "0.4.0")

	_, err := runUpdate(t, "0.4.0", "", exe)
	wantErr(t, err, "the checksum of "+name+" does not match checksums.txt", "axon 0.4.0 is unchanged.")
	assertUnchanged(t, exe, "0.4.0")
}

func TestFailedDownload(t *testing.T) {
	repo := &fakeRepo{latest: "v0.5.0"}
	repo.addRelease(t, "0.5.0")
	delete(repo.files, "v0.5.0/axon_0.5.0_"+testPlatform+".tar.gz")
	serve(t, repo)
	exe := installed(t, "0.4.0")

	_, err := runUpdate(t, "0.4.0", "", exe)
	wantErr(t, err, "could not download axon_0.5.0_"+testPlatform+".tar.gz of release v0.5.0", "Check your network connection", "axon 0.4.0 is unchanged.")
	assertUnchanged(t, exe, "0.4.0")
}

func TestNoNetwork(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close()
	old := repoURL
	repoURL = addr + "/org/axon"
	t.Cleanup(func() { repoURL = old })
	exe := installed(t, "0.4.0")

	_, err := runUpdate(t, "0.4.0", "", exe)
	wantErr(t, err, "could not look up the latest release at "+addr+"/org/axon/releases/latest: connection refused. Check your network connection and run `axon update` again.")
	assertUnchanged(t, exe, "0.4.0")
}

// A download that gets no answer gives the short reason.
func TestNoNetworkForDownload(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close()
	old := repoURL
	repoURL = addr + "/org/axon"
	t.Cleanup(func() { repoURL = old })
	exe := installed(t, "0.4.0")

	_, err := runUpdate(t, "0.4.0", "0.5.0", exe)
	wantErr(t, err, "could not download checksums.txt of release v0.5.0: connection refused. axon 0.4.0 is unchanged.")
	assertUnchanged(t, exe, "0.4.0")
}

func TestNoArchiveForPlatform(t *testing.T) {
	repo := &fakeRepo{latest: "v0.5.0"}
	repo.addRelease(t, "0.5.0")
	serve(t, repo)
	exe := installed(t, "0.4.0")

	var out bytes.Buffer
	err := Run(context.Background(), Options{Current: "0.4.0", Exe: exe, Platform: "darwin_arm64", Out: &out})
	wantErr(t, err, "release v0.5.0 has no archive for darwin_arm64", "axon 0.4.0 is unchanged.")
	assertUnchanged(t, exe, "0.4.0")
}

func TestUnreleasedPlatform(t *testing.T) {
	repo := &fakeRepo{latest: "v0.5.0"}
	repo.addRelease(t, "0.5.0")
	serve(t, repo)
	exe := installed(t, "0.4.0")

	var out bytes.Buffer
	err := Run(context.Background(), Options{Current: "0.4.0", Exe: exe, Platform: "windows_amd64", Out: &out})
	wantErr(t, err, "axon is not released for windows_amd64")
	assertUnchanged(t, exe, "0.4.0")
}

func TestArchiveWithoutBinary(t *testing.T) {
	repo := &fakeRepo{latest: "v0.5.0"}
	name := "axon_0.5.0_" + testPlatform + ".tar.gz"
	archive := tarGz(t, map[string][]byte{"README.md": []byte("no binary")})
	repo.setArchive("0.5.0", name, archive, archive)
	serve(t, repo)
	exe := installed(t, "0.4.0")

	_, err := runUpdate(t, "0.4.0", "", exe)
	wantErr(t, err, "holds no axon binary")
	assertUnchanged(t, exe, "0.4.0")
}

func TestArchiveEntryPathIsIgnored(t *testing.T) {
	repo := &fakeRepo{latest: "v0.5.0"}
	name := "axon_0.5.0_" + testPlatform + ".tar.gz"
	archive := tarGz(t, map[string][]byte{"../../evil/axon": fakeBinary("0.5.0")})
	repo.setArchive("0.5.0", name, archive, archive)
	serve(t, repo)
	exe := installed(t, "0.4.0")

	if _, err := runUpdate(t, "0.4.0", "", exe); err != nil {
		t.Fatal(err)
	}
	assertInstalled(t, exe, "0.5.0")
}

func TestUnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to any directory")
	}
	repo := &fakeRepo{latest: "v0.5.0"}
	repo.addRelease(t, "0.5.0")
	serve(t, repo)
	exe := installed(t, "0.4.0")
	dir := filepath.Dir(exe)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	_, err := runUpdate(t, "0.4.0", "", exe)
	wantErr(t, err, "cannot write to "+dir, "axon 0.4.0 is unchanged.", InstallLine)
	assertUnchanged(t, exe, "0.4.0")
}

func TestNewBinaryReportsAnotherVersion(t *testing.T) {
	repo := &fakeRepo{latest: "v0.5.0"}
	name := "axon_0.5.0_" + testPlatform + ".tar.gz"
	// This binary runs, and reports another version than its release's.
	archive := tarGz(t, map[string][]byte{"axon": fakeBinary("0.0.1")})
	repo.setArchive("0.5.0", name, archive, archive)
	serve(t, repo)
	exe := installed(t, "0.4.0")

	_, err := runUpdate(t, "0.4.0", "", exe)
	wantErr(t, err, "the downloaded axon 0.5.0 reports \"axon 0.0.1\"", "axon 0.4.0 is unchanged.")
	assertUnchanged(t, exe, "0.4.0")
}

func TestNewBinaryCannotRun(t *testing.T) {
	repo := &fakeRepo{latest: "v0.5.0"}
	name := "axon_0.5.0_" + testPlatform + ".tar.gz"
	// No interpreter exists at this path, so the binary cannot be run.
	archive := tarGz(t, map[string][]byte{"axon": []byte("#!/nonexistent/interpreter\n")})
	repo.setArchive("0.5.0", name, archive, archive)
	serve(t, repo)
	exe := installed(t, "0.4.0")

	_, err := runUpdate(t, "0.4.0", "", exe)
	wantErr(t, err, "the downloaded axon 0.5.0 does not run in "+filepath.Dir(exe), "allows running programs", "axon 0.4.0 is unchanged.")
	assertUnchanged(t, exe, "0.4.0")
}

func TestSymlinkedBinary(t *testing.T) {
	repo := &fakeRepo{latest: "v0.5.0"}
	repo.addRelease(t, "0.5.0")
	serve(t, repo)
	exe := installed(t, "0.4.0")
	link := filepath.Join(t.TempDir(), "axon")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}

	if _, err := runUpdate(t, "0.4.0", "", link); err != nil {
		t.Fatal(err)
	}
	assertInstalled(t, exe, "0.5.0")
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the symlink %s was replaced", link)
	}
}

// A download that stalls ends with the short reason.
func TestStalledDownload(t *testing.T) {
	old := stallTimeout
	stallTimeout = 100 * time.Millisecond
	t.Cleanup(func() { stallTimeout = old })
	repo := &fakeRepo{latest: "v0.5.0"}
	repo.addRelease(t, "0.5.0")
	name := "axon_0.5.0_" + testPlatform + ".tar.gz"
	serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/"+name) {
			w.Header().Set("Content-Length", "1000")
			w.Write([]byte("partial"))
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
			case <-t.Context().Done():
			}
			return
		}
		repo.ServeHTTP(w, r)
	}))
	exe := installed(t, "0.4.0")

	_, err := runUpdate(t, "0.4.0", "", exe)
	wantErr(t, err, "could not download "+name+" of release v0.5.0: timed out. axon 0.4.0 is unchanged. Check your network connection and run `axon update` again.")
	assertUnchanged(t, exe, "0.4.0")
}

// A binary that cannot be written in full leaves nothing in the directory:
// neither the reserved name nor a temporary file.
func TestStagingFailureLeavesNothing(t *testing.T) {
	// noise is content gzip cannot shrink, so a cut archive is cut in the
	// binary's body, after its header.
	noise := make([]byte, 0, 64<<10)
	for seed := sha256.Sum256(nil); len(noise) < cap(noise); seed = sha256.Sum256(seed[:]) {
		noise = append(noise, seed[:]...)
	}
	whole := tarGz(t, map[string][]byte{"axon": noise})
	for _, tc := range []struct {
		name    string
		archive []byte
		limit   int64
		want    string
	}{
		{"too large", whole, 1 << 10, "axon in the archive is larger than 1024 bytes"},
		{"cut short", whole[:len(whole)*3/4], 512 << 20, "read the archive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := maxBinary
			maxBinary = tc.limit
			t.Cleanup(func() { maxBinary = old })
			repo := &fakeRepo{latest: "v0.5.0"}
			name := "axon_0.5.0_" + testPlatform + ".tar.gz"
			repo.setArchive("0.5.0", name, tc.archive, tc.archive)
			serve(t, repo)
			exe := installed(t, "0.4.0")

			_, err := runUpdate(t, "0.4.0", "", exe)
			// The failure comes after the staged name was reserved.
			wantErr(t, err, "could not unpack "+name, "/.axon-update-", tc.want, "axon 0.4.0 is unchanged.")
			assertUnchanged(t, exe, "0.4.0")
		})
	}
}

func TestRepoURLMustBeHTTPSOrLoopback(t *testing.T) {
	for _, raw := range []string{"http://example.org/org/axon", "ftp://127.0.0.1/x", "file:///tmp/x"} {
		if _, err := parseRepoURL(raw); err == nil {
			t.Errorf("parseRepoURL(%q) = nil error", raw)
		}
	}
	for _, raw := range []string{"https://github.com/enigma-brain/axon", "http://127.0.0.1:8080/org/axon"} {
		if _, err := parseRepoURL(raw); err != nil {
			t.Errorf("parseRepoURL(%q) = %v", raw, err)
		}
	}
}

func TestChecksumFor(t *testing.T) {
	sums := []byte("aaaa  axon_1_linux_amd64.tar.gz\nBBBB *axon_1_darwin_arm64.tar.gz\n")
	if got, ok := checksumFor(sums, "axon_1_linux_amd64.tar.gz"); !ok || got != "aaaa" {
		t.Errorf("linux = %q, %v", got, ok)
	}
	if got, ok := checksumFor(sums, "axon_1_darwin_arm64.tar.gz"); !ok || got != "bbbb" {
		t.Errorf("darwin = %q, %v", got, ok)
	}
	if _, ok := checksumFor(sums, "axon_1_darwin_amd64.tar.gz"); ok {
		t.Error("found a checksum for a missing archive")
	}
}
