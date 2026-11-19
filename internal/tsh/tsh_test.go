package tsh

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enigma-brain/axon/internal/axondir"
)

// fakeTSH returns a tsh that reports version. It appends the arguments of
// every other command to $FAKETSH_LOG and exits with $FAKETSH_EXIT, when
// set. For status it prints $FAKETSH_STATUS, or exits 1 when that is unset,
// as tsh does with no login. It uses only shell builtins, since the tests
// run it with a PATH that holds nothing else.
func fakeTSH(version string) string {
	return `#!/bin/sh
case "$1" in
version)
	echo '{"version": "` + version + `", "gitref": "fake"}'
	exit 0
	;;
status)
	if [ -z "$FAKETSH_STATUS" ]; then echo "ERROR: Not logged in." >&2; exit 1; fi
	printf '%s\n' "$FAKETSH_STATUS"
	exit 0
	;;
esac
echo "$*" >> "$FAKETSH_LOG"
if [ -n "$FAKETSH_EXIT" ]; then echo "tsh failed" >&2; exit "$FAKETSH_EXIT"; fi
`
}

// testProxy is the made-up Teleport proxy the tests pass. The fixture's
// pingURL sends its requests to a local fake.
const testProxy = "teleport.example.org"

// fixedURL returns a pingURL that sends every proxy's request to url.
func fixedURL(url string) func(string) string {
	return func(string) string { return url }
}

// fixture is a fake proxy and CDN, an empty home and a PATH that holds only
// a directory the test controls.
type fixture struct {
	t     *testing.T
	dir   axondir.Dir
	tools string
	bin   string

	mu    sync.Mutex
	proxy string
	// badSum serves a checksum that does not match the archive.
	badSum bool
	// noTSH serves an archive without tsh.
	noTSH bool
	// bundleExtra is added to the bundle in a macOS archive.
	bundleExtra []archiveEntry

	// pinged holds the proxy of every version request.
	pinged []string

	archiveRequests atomic.Int32
	// archiveNames holds the name of every archive requested.
	archiveNames []string
}

// archiveEntry is one entry of a fake archive.
type archiveEntry struct {
	name     string
	typeflag byte
	mode     int64
	body     string
	linkname string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	home := t.TempDir()
	f := &fixture{
		t:     t,
		dir:   axondir.New(home),
		bin:   t.TempDir(),
		proxy: "18.11.7",
	}
	tools, err := f.dir.ToolsDir()
	if err != nil {
		t.Fatal(err)
	}
	f.tools = tools

	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)

	setVar(t, &pingURL, func(proxy string) string {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.pinged = append(f.pinged, proxy)
		return srv.URL + "/webapi/ping"
	})
	setVar(t, &cdnBaseURL, srv.URL)
	setVar(t, &platform, "linux/amd64")
	t.Setenv("PATH", f.bin)
	t.Setenv("FAKETSH_LOG", filepath.Join(t.TempDir(), "log"))
	return f
}

func setVar[T any](t *testing.T, v *T, value T) {
	old := *v
	*v = value
	t.Cleanup(func() { *v = old })
}

func (f *fixture) setProxy(version string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.proxy = version
}

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	proxy, badSum, noTSH, extra := f.proxy, f.badSum, f.noTSH, f.bundleExtra
	f.mu.Unlock()

	if r.URL.Path == "/webapi/ping" {
		fmt.Fprintf(w, `{"auth": {"type": "saml"}, "server_version": %q, "min_client_version": "17.0.0-aa"}`, proxy)
		return
	}
	var version string
	sumFile := strings.HasSuffix(r.URL.Path, ".sha256")
	name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".sha256")
	arch := ""
	for _, a := range []string{"linux-amd64", "darwin-arm64", "darwin-amd64"} {
		if _, err := fmt.Sscanf(name, "teleport-v%s", &version); err == nil && strings.HasSuffix(name, "-"+a+"-bin.tar.gz") {
			arch = a
			version = strings.TrimSuffix(version, "-"+a+"-bin.tar.gz")
			break
		}
	}
	if arch == "" {
		http.NotFound(w, r)
		return
	}
	var archive []byte
	if strings.HasPrefix(arch, "darwin-") {
		archive = macArchive(f.t, fakeTSH(version), !noTSH, extra)
	} else {
		archive = teleportArchive(f.t, fakeTSH(version), !noTSH)
	}
	if sumFile {
		sum := sha256.Sum256(archive)
		if badSum {
			sum = sha256.Sum256([]byte("something else"))
		}
		fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), name)
		return
	}
	f.archiveRequests.Add(1)
	f.mu.Lock()
	f.archiveNames = append(f.archiveNames, name)
	f.mu.Unlock()
	w.Header().Set("Content-Length", fmt.Sprint(len(archive)))
	w.Write(archive)
}

// teleportArchive builds an archive laid out like Teleport's, holding tsh
// when withTSH is set.
func teleportArchive(t *testing.T, tsh string, withTSH bool) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := []struct{ name, body string }{
		{"teleport/README.md", "Teleport\n"},
		{"teleport/tctl", "#!/bin/sh\n"},
	}
	if withTSH {
		files = append(files, struct{ name, body string }{"teleport/tsh", tsh})
	}
	for _, file := range files {
		if err := tw.WriteHeader(&tar.Header{Name: file.name, Mode: 0o755, Size: int64(len(file.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Error(err)
		}
		tw.Write([]byte(file.body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// macArchive builds an archive laid out like Teleport's for macOS: tsh in
// teleport/tsh.app, beside other tools. The bundle holds tsh when withTSH is
// set, and extra.
func macArchive(t *testing.T, tsh string, withTSH bool, extra []archiveEntry) []byte {
	entries := []archiveEntry{
		{name: "teleport/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "teleport/README.md", typeflag: tar.TypeReg, mode: 0o644, body: "Teleport\n"},
		{name: "teleport/tctl.app/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "teleport/tctl.app/Contents/MacOS/tctl", typeflag: tar.TypeReg, mode: 0o755, body: "#!/bin/sh\n"},
		{name: "teleport/tsh.app/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "teleport/tsh.app/Contents/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "teleport/tsh.app/Contents/Info.plist", typeflag: tar.TypeReg, mode: 0o644, body: "<plist/>\n"},
		{name: "teleport/tsh.app/Contents/_CodeSignature/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "teleport/tsh.app/Contents/_CodeSignature/CodeResources", typeflag: tar.TypeReg, mode: 0o644, body: "signature\n"},
		{name: "teleport/tsh.app/Contents/MacOS/", typeflag: tar.TypeDir, mode: 0o755},
	}
	if withTSH {
		entries = append(entries, archiveEntry{name: "teleport/tsh.app/Contents/MacOS/tsh", typeflag: tar.TypeReg, mode: 0o755, body: tsh})
	}
	return tarGz(t, append(entries, extra...))
}

// tarGz builds a gzipped tar archive of entries.
func tarGz(t *testing.T, entries []archiveEntry) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Typeflag: e.typeflag, Mode: e.mode, Linkname: e.linkname}
		if e.typeflag == tar.TypeReg {
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Error(err)
		}
		tw.Write([]byte(e.body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// putOnPath writes a fake tsh reporting version into the PATH directory.
func (f *fixture) putOnPath(version string) string {
	path := filepath.Join(f.bin, "tsh")
	if err := os.WriteFile(path, []byte(fakeTSH(version)), 0o755); err != nil {
		f.t.Fatal(err)
	}
	return path
}

// tshEntries lists the entries of ~/.axon/tools/tsh.
func (f *fixture) tshEntries() []string {
	entries, err := os.ReadDir(filepath.Join(f.tools, tshDir))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestPathTSHInRangeIsUsed(t *testing.T) {
	for _, version := range []string{"18.11.7", "18.0.1", "17.4.2"} {
		t.Run(version, func(t *testing.T) {
			f := newFixture(t)
			want := f.putOnPath(version)
			before, _ := os.ReadFile(want)

			got, err := Ensure(context.Background(), f.dir, testProxy, strings.NewReader("y\n"), io.Discard)
			if err != nil {
				t.Fatalf("Ensure: %v", err)
			}
			if got != want {
				t.Errorf("Ensure returned %s, want %s", got, want)
			}
			after, _ := os.ReadFile(want)
			if !bytes.Equal(before, after) {
				t.Error("the tsh on PATH was changed")
			}
			if n := f.archiveRequests.Load(); n != 0 {
				t.Errorf("%d archive requests, want none", n)
			}
			if entries := f.tshEntries(); len(entries) != 0 {
				t.Errorf("axon installed %v beside a tsh on PATH", entries)
			}
		})
	}
}

func TestPathTSHOutOfRangeStops(t *testing.T) {
	for _, version := range []string{"16.4.0", "19.0.0"} {
		t.Run(version, func(t *testing.T) {
			f := newFixture(t)
			path := f.putOnPath(version)

			_, err := Ensure(context.Background(), f.dir, testProxy, strings.NewReader("y\n"), io.Discard)
			if err == nil {
				t.Fatal("Ensure succeeded, want an error")
			}
			for _, want := range []string{version, "18.11.7", path, "Update tsh", InstallLink} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
			if n := f.archiveRequests.Load(); n != 0 {
				t.Errorf("%d archive requests, want none", n)
			}
		})
	}
}

func TestInstallAfterYes(t *testing.T) {
	for name, answer := range map[string]string{"yes": "yes\n", "empty": "\n", "asked again": "maybe\nY\n"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			var out bytes.Buffer
			path, err := Ensure(context.Background(), f.dir, testProxy, strings.NewReader(answer), &out)
			if err != nil {
				t.Fatalf("Ensure: %v\n%s", err, out.String())
			}
			if want := filepath.Join(f.tools, "tsh", "18.11.7", "tsh"); path != want {
				t.Errorf("Ensure returned %s, want %s", path, want)
			}
			version, err := clientVersion(context.Background(), path)
			if err != nil || version != "18.11.7" {
				t.Errorf("installed tsh reports %q, %v; want 18.11.7", version, err)
			}
			for _, want := range []string{"Install tsh 18.11.7 into ~/.axon/tools? [Y/n]", "Downloading Teleport 18.11.7 for tsh", "100%", "Installed tsh 18.11.7"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output %q does not contain %q", out.String(), want)
				}
			}
			if got := f.tshEntries(); strings.Join(got, ",") != "18.11.7,current" {
				t.Errorf("~/.axon/tools/tsh holds %v, want [18.11.7 current]", got)
			}
			// Only tsh is extracted.
			entries, _ := os.ReadDir(filepath.Join(f.tools, "tsh", "18.11.7"))
			if len(entries) != 1 {
				t.Errorf("the install directory holds %d entries, want only tsh", len(entries))
			}
		})
	}
}

func TestInstalledTSHIsReused(t *testing.T) {
	f := newFixture(t)
	first, err := Ensure(context.Background(), f.dir, testProxy, strings.NewReader("\n"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	second, err := Ensure(context.Background(), f.dir, testProxy, nil, &out)
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if second != first || out.Len() != 0 || f.archiveRequests.Load() != 1 {
		t.Errorf("second Ensure returned %s with output %q after %d downloads; want %s, no output, one download",
			second, out.String(), f.archiveRequests.Load(), first)
	}
}

func TestDeclinedInstallStops(t *testing.T) {
	for name, in := range map[string]io.Reader{
		"no":                      strings.NewReader("n\n"),
		"end of input":            strings.NewReader(""),
		"blank then end of input": strings.NewReader("  "),
		"unclear then end":        strings.NewReader("maybe\n"),
		"no input":                nil,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			_, err := Ensure(context.Background(), f.dir, testProxy, in, io.Discard)
			if err == nil || !strings.Contains(err.Error(), InstallLink) {
				t.Fatalf("Ensure returned %v, want an error with the install link", err)
			}
			if n := f.archiveRequests.Load(); n != 0 {
				t.Errorf("%d archive requests, want none", n)
			}
		})
	}
}

func TestFailedInstallStops(t *testing.T) {
	cases := map[string]struct {
		set  func(*fixture)
		want string
	}{
		"checksum mismatch": {func(f *fixture) { f.badSum = true }, "download Teleport 18.11.7: checksum mismatch"},
		"no tsh in archive": {func(f *fixture) { f.noTSH = true }, "install tsh 18.11.7: the archive holds no teleport/tsh"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			c.set(f)
			_, err := Ensure(context.Background(), f.dir, testProxy, strings.NewReader("\n"), io.Discard)
			if err == nil {
				t.Fatal("Ensure succeeded, want an error")
			}
			for _, want := range []string{c.want, InstallLink} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			if _, _, ok, _ := installed(f.tools); ok {
				t.Error("a failed install recorded a tsh")
			}
			for _, name := range f.tshEntries() {
				t.Errorf("a failed install left %s", name)
			}
		})
	}
}

func TestOwnTSHReinstalledAfterProxyUpgrade(t *testing.T) {
	f := newFixture(t)
	f.setProxy("17.5.0")
	if _, err := Ensure(context.Background(), f.dir, testProxy, strings.NewReader("\n"), io.Discard); err != nil {
		t.Fatal(err)
	}

	// One major ahead is still supported: nothing is installed.
	f.setProxy("18.11.7")
	if _, err := Ensure(context.Background(), f.dir, testProxy, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	if n := f.archiveRequests.Load(); n != 1 {
		t.Errorf("%d archive requests, want 1", n)
	}

	// The earlier version is old by the time the proxy is upgraded.
	past := time.Now().Add(-2 * staleAge)
	os.Chtimes(filepath.Join(f.tools, tshDir, "17.5.0"), past, past)

	f.setProxy("19.0.2")
	var out bytes.Buffer
	path, err := Ensure(context.Background(), f.dir, testProxy, nil, &out)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if want := filepath.Join(f.tools, "tsh", "19.0.2", "tsh"); path != want {
		t.Errorf("Ensure returned %s, want %s", path, want)
	}
	if !strings.Contains(out.String(), "Updated tsh from 17.5.0 to 19.0.2") || strings.Contains(out.String(), "[Y/n]") {
		t.Errorf("output %q: want the move named and no question", out.String())
	}
	if got := f.tshEntries(); strings.Join(got, ",") != "19.0.2,current" {
		t.Errorf("~/.axon/tools/tsh holds %v, want [19.0.2 current]", got)
	}
}

func TestRecentOtherVersionIsKept(t *testing.T) {
	// A version published moments ago, as by a concurrent install against a
	// proxy that has just been upgraded, may be about to run; it stays.
	f := newFixture(t)
	other := filepath.Join(f.tools, tshDir, "19.0.2")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(other, "tsh"), []byte(fakeTSH("19.0.2")), 0o700)

	if _, err := Ensure(context.Background(), f.dir, testProxy, strings.NewReader("y\n"), io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := f.tshEntries(); strings.Join(got, ",") != "18.11.7,19.0.2,current" {
		t.Errorf("~/.axon/tools/tsh holds %v, want [18.11.7 19.0.2 current]", got)
	}
}

func TestStalledDownloadStops(t *testing.T) {
	f := newFixture(t)
	setVar(t, &stallTimeout, 200*time.Millisecond)
	release := make(chan struct{})
	stall := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			fmt.Fprintf(w, "%064x  archive\n", 0)
			return
		}
		// Send part of the body, then nothing.
		w.Header().Set("Content-Length", "1000")
		w.Write(make([]byte, 10))
		w.(http.Flusher).Flush()
		<-release
	}))
	// Cleanups run last first: the handlers are released before the server
	// closes, since Close waits for them.
	t.Cleanup(stall.Close)
	t.Cleanup(func() { close(release) })
	cdnBaseURL = stall.URL

	done := make(chan error, 1)
	go func() {
		_, err := Ensure(context.Background(), f.dir, testProxy, strings.NewReader("y\n"), io.Discard)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "no data for 200ms") || !strings.Contains(err.Error(), InstallLink) {
			t.Errorf("Ensure returned %v, want the stall named and the install link", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Ensure did not stop on a stalled download")
	}
}

func TestStalledProxyStops(t *testing.T) {
	f := newFixture(t)
	setVar(t, &stallTimeout, 200*time.Millisecond)
	release := make(chan struct{})
	stall := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	// Cleanups run last first: the handlers are released before the server
	// closes, since Close waits for them.
	t.Cleanup(stall.Close)
	t.Cleanup(func() { close(release) })
	pingURL = fixedURL(stall.URL + "/webapi/ping")

	_, err := Ensure(context.Background(), f.dir, testProxy, nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "read the Teleport proxy's version") || !strings.Contains(err.Error(), "no data for 200ms") {
		t.Errorf("Ensure returned %v, want the stalled step named", err)
	}
}

func TestNoInstallOnOtherPlatforms(t *testing.T) {
	for _, p := range []string{"linux/arm64", "windows/amd64"} {
		t.Run(p, func(t *testing.T) {
			f := newFixture(t)
			platform = p
			_, err := Ensure(context.Background(), f.dir, testProxy, strings.NewReader("y\n"), io.Discard)
			if err == nil || !strings.Contains(err.Error(), "not on PATH") || !strings.Contains(err.Error(), InstallLink) {
				t.Fatalf("Ensure returned %v, want the install link", err)
			}
			if n := f.archiveRequests.Load(); n != 0 {
				t.Errorf("%d archive requests, want none", n)
			}
		})
	}
}

func TestProxyUnreachable(t *testing.T) {
	f := newFixture(t)
	f.putOnPath("18.11.7")
	pingURL = fixedURL("http://127.0.0.1:1/webapi/ping")
	_, err := Ensure(context.Background(), f.dir, testProxy, nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "read the Teleport proxy's version at http://127.0.0.1:1/webapi/ping") {
		t.Fatalf("Ensure returned %v, want the step named", err)
	}
}

func TestConcurrentInstalls(t *testing.T) {
	f := newFixture(t)
	const n = 4
	paths := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			paths[i], errs[i] = Ensure(context.Background(), f.dir, testProxy, strings.NewReader("y\n"), io.Discard)
		}()
	}
	wg.Wait()
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("Ensure %d: %v", i, errs[i])
		}
		if paths[i] != paths[0] {
			t.Errorf("Ensure %d returned %s, Ensure 0 %s", i, paths[i], paths[0])
		}
	}
	if got := f.tshEntries(); strings.Join(got, ",") != "18.11.7,current" {
		t.Errorf("~/.axon/tools/tsh holds %v, want [18.11.7 current]", got)
	}
	path, version, ok, err := installed(f.tools)
	if err != nil || !ok || version != "18.11.7" || path != paths[0] {
		t.Errorf("installed() = %s, %s, %v, %v", path, version, ok, err)
	}
	if v, err := clientVersion(context.Background(), path); err != nil || v != "18.11.7" {
		t.Errorf("installed tsh reports %q, %v", v, err)
	}
}

func TestStaleTemporaryEntriesAreRemoved(t *testing.T) {
	f := newFixture(t)
	root := filepath.Join(f.tools, tshDir)
	os.MkdirAll(root, 0o700)
	old := filepath.Join(root, ".install-old")
	fresh := filepath.Join(root, ".install-fresh")
	os.Mkdir(old, 0o700)
	os.Mkdir(fresh, 0o700)
	past := time.Now().Add(-2 * staleAge)
	os.Chtimes(old, past, past)

	if _, err := Ensure(context.Background(), f.dir, testProxy, strings.NewReader("y\n"), io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("a stale temporary directory was kept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a recent temporary directory, possibly another install's, was removed")
	}
}

func readLog(t *testing.T) string {
	data, _ := os.ReadFile(os.Getenv("FAKETSH_LOG"))
	return string(data)
}

func TestLogin(t *testing.T) {
	f := newFixture(t)
	path := f.putOnPath("18.11.7")
	for _, proxy := range []string{testProxy, "teleport.example.org:3080"} {
		os.Remove(os.Getenv("FAKETSH_LOG"))
		if err := Login(context.Background(), path, proxy); err != nil {
			t.Fatalf("Login: %v", err)
		}
		if got, want := readLog(t), "login --proxy="+proxy+"\n"; got != want {
			t.Errorf("tsh ran with %q, want %q", got, want)
		}
	}

	t.Setenv("FAKETSH_EXIT", "3")
	err := Login(context.Background(), path, testProxy)
	if err == nil || !strings.HasPrefix(err.Error(), "Teleport login (tsh login --proxy=teleport.example.org): exit status 3") {
		t.Errorf("Login returned %v, want the step named", err)
	}
}

func TestAppLogin(t *testing.T) {
	f := newFixture(t)
	path := f.putOnPath("18.11.7")
	for _, proxy := range []string{testProxy, "teleport.example.org:3080"} {
		os.Remove(os.Getenv("FAKETSH_LOG"))
		if err := AppLogin(context.Background(), path, proxy, "devenv"); err != nil {
			t.Fatalf("AppLogin: %v", err)
		}
		if got, want := readLog(t), "--proxy="+proxy+" apps login devenv\n"; got != want {
			t.Errorf("tsh ran with %q, want %q", got, want)
		}
	}

	t.Setenv("FAKETSH_EXIT", "1")
	err := AppLogin(context.Background(), path, testProxy, "devenv")
	if err == nil || !strings.HasPrefix(err.Error(), "Teleport application login (tsh --proxy=teleport.example.org apps login devenv): exit status 1") {
		t.Errorf("AppLogin returned %v, want the step named", err)
	}
	for _, app := range []string{"", "--proxy=x", "dev env"} {
		if err := AppLogin(context.Background(), path, testProxy, app); err == nil {
			t.Errorf("AppLogin accepted %q as the application name", app)
		}
	}
}

func TestBadProxyIsRefused(t *testing.T) {
	f := newFixture(t)
	path := f.putOnPath("18.11.7")
	for _, proxy := range []string{"", "-x", "--proxy=x", ".example.org", "teleport.example.org/path", "https://teleport.example.org",
		"teleport.example.org:", "teleport.example.org:0", "teleport.example.org:65536", "teleport.example.org:08", "teleport.example.org:http",
		"user@teleport.example.org", "teleport example.org", "[::1]:443"} {
		t.Run(proxy, func(t *testing.T) {
			if err := Login(context.Background(), path, proxy); err == nil {
				t.Error("Login accepted it")
			}
			if err := AppLogin(context.Background(), path, proxy, "devenv"); err == nil {
				t.Error("AppLogin accepted it")
			}
			if _, err := SessionValid(context.Background(), path, proxy); err == nil {
				t.Error("SessionValid accepted it")
			}
			if _, err := Ensure(context.Background(), f.dir, proxy, nil, io.Discard); err == nil {
				t.Error("Ensure accepted it")
			}
		})
	}
	if got := readLog(t); got != "" {
		t.Errorf("tsh ran with %q", got)
	}
	if len(f.pinged) != 0 {
		t.Errorf("the version of %v was requested", f.pinged)
	}
}

func TestVersionIsReadFromTheProxyPassed(t *testing.T) {
	for _, proxy := range []string{testProxy, "teleport.example.org:3080"} {
		t.Run(proxy, func(t *testing.T) {
			f := newFixture(t)
			f.putOnPath("18.11.7")
			if _, err := Ensure(context.Background(), f.dir, proxy, nil, io.Discard); err != nil {
				t.Fatal(err)
			}
			if len(f.pinged) != 1 || f.pinged[0] != proxy {
				t.Errorf("the version was requested of %v, want [%s]", f.pinged, proxy)
			}
		})
	}
}

func TestPingURL(t *testing.T) {
	for proxy, want := range map[string]string{
		"teleport.example.org":      "https://teleport.example.org/webapi/ping",
		"teleport.example.org:3080": "https://teleport.example.org:3080/webapi/ping",
	} {
		if got := pingURL(proxy); got != want {
			t.Errorf("pingURL(%q) = %q, want %q", proxy, got, want)
		}
	}
}

func TestSessionValid(t *testing.T) {
	f := newFixture(t)
	path := f.putOnPath("18.11.7")
	setVar(t, &now, func() time.Time { return time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC) })

	status := func(url, until string) string {
		return fmt.Sprintf(`{"active": {"profile_url": %q, "cluster": "teleport.example.org", "valid_until": %q}, "profiles": []}`, url, until)
	}
	const later = "2026-11-19T23:00:00Z"
	cases := map[string]struct {
		proxy  string
		status string
		want   bool
	}{
		"valid":                        {testProxy, status("https://teleport.example.org:443", "2026-11-19T05:04:57-08:00"), true},
		"expired":                      {testProxy, status("https://teleport.example.org:443", "2026-11-19T03:59:59-08:00"), false},
		"other proxy":                  {testProxy, status("https://other.example.org:443", later), false},
		"same host, other port":        {testProxy, status("https://teleport.example.org:3080", later), false},
		"profile without a port":       {testProxy, status("https://teleport.example.org", later), true},
		"proxy with port 443":          {"teleport.example.org:443", status("https://teleport.example.org:443", later), true},
		"proxy with port 443, bare":    {"teleport.example.org:443", status("https://teleport.example.org", later), true},
		"proxy with another port":      {"teleport.example.org:3080", status("https://teleport.example.org:3080", later), true},
		"proxy with another port, 443": {"teleport.example.org:3080", status("https://teleport.example.org:443", later), false},
		"host in another case":         {"Teleport.Example.org", status("https://teleport.example.org:443", later), true},
		"no active":                    {testProxy, `{"profiles": []}`, false},
		"not logged in":                {testProxy, "", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("FAKETSH_STATUS", c.status)
			got, err := SessionValid(context.Background(), path, c.proxy)
			if err != nil {
				t.Fatalf("SessionValid: %v", err)
			}
			if got != c.want {
				t.Errorf("SessionValid = %v, want %v", got, c.want)
			}
		})
	}

	t.Setenv("FAKETSH_STATUS", "not json")
	if _, err := SessionValid(context.Background(), path, testProxy); err == nil {
		t.Error("SessionValid accepted output that is not JSON")
	}
	if _, err := SessionValid(context.Background(), filepath.Join(f.bin, "missing"), testProxy); err == nil {
		t.Error("SessionValid accepted a tsh that does not exist")
	}
}

func TestFind(t *testing.T) {
	t.Run("on PATH", func(t *testing.T) {
		f := newFixture(t)
		want := f.putOnPath("16.0.0")
		pingURL = fixedURL("http://127.0.0.1:1/webapi/ping")
		got, err := Find(f.dir)
		if err != nil || got != want {
			t.Errorf("Find = %s, %v; want %s, even out of range and offline", got, err, want)
		}
	})
	t.Run("own", func(t *testing.T) {
		f := newFixture(t)
		want, err := Ensure(context.Background(), f.dir, testProxy, strings.NewReader("y\n"), io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		requests := f.archiveRequests.Load()
		pingURL = fixedURL("http://127.0.0.1:1/webapi/ping")
		got, err := Find(f.dir)
		if err != nil || got != want {
			t.Errorf("Find = %s, %v; want %s", got, err, want)
		}
		if f.archiveRequests.Load() != requests {
			t.Error("Find downloaded")
		}
	})
	t.Run("neither", func(t *testing.T) {
		f := newFixture(t)
		pingURL = fixedURL("http://127.0.0.1:1/webapi/ping")
		got, err := Find(f.dir)
		if !errors.Is(err, ErrNotFound) || got != "" {
			t.Errorf("Find = %q, %v; want ErrNotFound", got, err)
		}
		if f.archiveRequests.Load() != 0 || len(f.tshEntries()) != 0 {
			t.Error("Find installed")
		}
	})
}

func TestSessionValidCancelled(t *testing.T) {
	f := newFixture(t)
	// A tsh that never answers, killed when the context ends.
	path := filepath.Join(f.bin, "slow-tsh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nwhile :; do :; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	ok, err := SessionValid(ctx, path, testProxy)
	if ok || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("SessionValid = %v, %v; want an error carrying the deadline, not \"no login\"", ok, err)
	}
}
