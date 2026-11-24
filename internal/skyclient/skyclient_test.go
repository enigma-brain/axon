package skyclient

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enigma-brain/axon/internal/axondir"
)

// fakeUV imitates the three uv commands the package runs. It appends each
// invocation and uv's environment to $FAKEUV_LOG, fails the Python or the
// package stage when $FAKEUV_FAIL_PYTHON or $FAKEUV_FAIL_PIP is set, fails
// the package stage as uv's resolver does when no pre-built package exists
// when $FAKEUV_NO_BUILD is set, at length when it is "long", sleeps
// in the package stage for $FAKEUV_PIP_SLEEP seconds, and writes a sky that
// prints its version and arguments. Like uv without --quiet, it writes the
// virtual environment and package lines on success.
const fakeUV = `#!/bin/sh
echo "uv $*" >> "$FAKEUV_LOG"
env | grep '^UV_' | sort | sed 's/^/env /' >> "$FAKEUV_LOG"
case "$1" in
python)
	if [ -n "$FAKEUV_FAIL_PYTHON" ]; then echo "error: failed to download cpython" >&2; exit 2; fi
	mkdir -p "$UV_PYTHON_INSTALL_DIR/cpython-3.12"
	;;
venv)
	echo "Creating virtual environment at: $4" >&2
	echo "Activate with: source $4/bin/activate" >&2
	mkdir -p "$4/bin"
	printf '#!/bin/sh\n' > "$4/bin/python"
	chmod +x "$4/bin/python"
	;;
pip)
	if [ -n "$FAKEUV_PIP_RELEASE" ]; then
		while [ ! -e "$FAKEUV_PIP_RELEASE" ]; do sleep 0.05; done
	fi
	if [ -n "$FAKEUV_PIP_SLEEP" ]; then sleep "$FAKEUV_PIP_SLEEP"; fi
	if [ -n "$FAKEUV_NO_BUILD" ]; then
		echo "  × No solution found when resolving dependencies:" >&2
		echo "  ╰─▶ Because $5 has no usable wheels and building from source is disabled, we can conclude that $5 cannot be used." >&2
		if [ "$FAKEUV_NO_BUILD" = long ]; then
			i=0
			while [ $i -lt 100 ]; do
				echo "      And because a dependency of $5 has no usable wheels, it cannot be used either." >&2
				i=$((i + 1))
			done
		fi
		exit 1
	fi
	if [ -n "$FAKEUV_FAIL_PIP" ]; then
		echo "error: no matching distribution for $5" >&2
		echo "  hint: $5 has no release for this platform" >&2
		exit 2
	fi
	echo "Installed 2 packages in 5ms" >&2
	echo " + certifi==2024.1.1" >&2
	echo " + skypilot==${5##*==}"
	bin=$(dirname "$4")
	ver=${5##*==}
	printf '#!/bin/sh\necho "private sky %s $*"\n' "$ver" > "$bin/sky"
	chmod +x "$bin/sky"
	;;
esac
`

const testTarget = "test-target"

// fixture is a fake uv release served over HTTP, and an empty home.
type fixture struct {
	dir      axondir.Dir
	tools    string
	log      string
	requests atomic.Int32
}

// newFixture serves a fake uv archive and points the package at it.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	archive := uvArchive(t, testTarget, fakeUV)
	sum := sha256.Sum256(archive)

	f := &fixture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if r.URL.Path != "/"+uvVersion+"/uv-"+testTarget+".tar.gz" {
			http.NotFound(w, r)
			return
		}
		w.Write(archive)
	}))
	t.Cleanup(srv.Close)

	setPackageVars(t, srv.URL, "test/platform", map[string]uvBuild{
		"test/platform": {target: testTarget, sha256: hex.EncodeToString(sum[:])},
	})

	home := t.TempDir()
	f.dir = axondir.New(home)
	f.tools = filepath.Join(home, ".axon", "tools")
	f.log = filepath.Join(t.TempDir(), "uv.log")
	t.Setenv("FAKEUV_LOG", f.log)
	t.Setenv("FAKEUV_FAIL_PYTHON", "")
	t.Setenv("FAKEUV_FAIL_PIP", "")
	t.Setenv("FAKEUV_NO_BUILD", "")
	t.Setenv("FAKEUV_PIP_RELEASE", "")
	t.Setenv("FAKEUV_PIP_SLEEP", "")
	return f
}

// setPackageVars replaces the download address, the platform and the
// checksum table for one test.
func setPackageVars(t *testing.T, baseURL, plat string, builds map[string]uvBuild) {
	t.Helper()
	oldURL, oldPlat, oldBuilds := uvBaseURL, platform, uvBuilds
	uvBaseURL, platform, uvBuilds = baseURL, plat, builds
	t.Cleanup(func() { uvBaseURL, platform, uvBuilds = oldURL, oldPlat, oldBuilds })
}

// uvArchive returns a .tar.gz laid out as uv's release archives are, with
// script as the uv executable.
func uvArchive(t *testing.T, target, script string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	dir := "uv-" + target + "/"
	entries := []struct {
		name string
		mode int64
		body string
		typ  byte
	}{
		{dir, 0o755, "", tar.TypeDir},
		{dir + "uvx", 0o755, "#!/bin/sh\nexit 1\n", tar.TypeReg},
		{dir + "uv", 0o755, script, tar.TypeReg},
	}
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: e.mode, Size: int64(len(e.body)), Typeflag: e.typ}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
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

func (f *fixture) uvLog(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(f.log)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// skyCommand returns a command that runs the installed client's sky with
// args, named by its absolute path.
func skyCommand(ctx context.Context, dir axondir.Dir, args ...string) (*exec.Cmd, error) {
	path, err := Path(dir)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, path, args...), nil
}

func runSky(t *testing.T, dir axondir.Dir, args ...string) string {
	t.Helper()
	cmd, err := skyCommand(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("skyCommand: %v", err)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run sky: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestEnsureFirstInstall(t *testing.T) {
	f := newFixture(t)
	var progress bytes.Buffer

	from, to, err := Ensure(context.Background(), f.dir, "0.13.0", &progress)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if from != "" || to != "0.13.0" {
		t.Errorf("Ensure moved from %q to %q, want from \"\" to 0.13.0", from, to)
	}

	want := "sky: installing the SkyPilot client 0.13.0, with its own uv and Python, into " + f.tools + ".\n" +
		"sky: your system's Python and packages are not changed.\n" +
		"sky: uv " + uvVersion + " 25% 50% 75% 100% done\n" +
		"sky: Python " + pythonVersion + " done\n" +
		"sky: SkyPilot 0.13.0 done\n" +
		"sky: installed the SkyPilot client 0.13.0\n"
	if progress.String() != want {
		t.Errorf("progress:\n%s\nwant:\n%s", progress.String(), want)
	}

	v, ok, err := Version(f.dir)
	if err != nil || !ok || v != "0.13.0" {
		t.Errorf("Version = %q, %v, %v; want 0.13.0, true, nil", v, ok, err)
	}

	p, err := Path(f.dir)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	wantPath := filepath.Join(f.tools, "skypilot", "0.13.0", "bin", "sky")
	if p != wantPath || !filepath.IsAbs(p) {
		t.Errorf("Path = %q, want %q", p, wantPath)
	}
	if out := runSky(t, f.dir, "--version"); out != "private sky 0.13.0 --version" {
		t.Errorf("sky printed %q", out)
	}

	if _, err := os.Stat(filepath.Join(f.tools, "uv", uvVersion, "uv")); err != nil {
		t.Errorf("uv is not kept under tools: %v", err)
	}
}

func TestEnsureKeepsUVOutputQuiet(t *testing.T) {
	f := newFixture(t)
	var progress bytes.Buffer
	if _, _, err := Ensure(context.Background(), f.dir, "0.13.0", &progress); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	for _, unwanted := range []string{"+ ", "==", "Activate with", "Creating virtual environment", "Installed 2 packages"} {
		if strings.Contains(progress.String(), unwanted) {
			t.Errorf("progress shows uv's output %q:\n%s", unwanted, progress.String())
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(f.uvLog(t)), "\n") {
		if strings.HasPrefix(line, "uv ") && !strings.HasSuffix(line, " --quiet") {
			t.Errorf("uv ran without --quiet: %s", line)
		}
	}
}

func TestEnsureReinstallOpensWithLocation(t *testing.T) {
	f := newFixture(t)
	if _, _, err := Ensure(context.Background(), f.dir, "0.13.0", nil); err != nil {
		t.Fatal(err)
	}
	var progress bytes.Buffer
	if _, _, err := Ensure(context.Background(), f.dir, "0.14.1", &progress); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	want := "sky: installing the SkyPilot client 0.14.1, with its own uv and Python, into " + f.tools + ".\n" +
		"sky: your system's Python and packages are not changed.\n" +
		"sky: Python " + pythonVersion + " done\n" +
		"sky: SkyPilot 0.14.1 done\n" +
		"sky: installed the SkyPilot client 0.14.1\n"
	if progress.String() != want {
		t.Errorf("progress:\n%s\nwant:\n%s", progress.String(), want)
	}
}

func TestEnsureLongStepShowsItIsWorking(t *testing.T) {
	f := newFixture(t)
	old := tickInterval
	tickInterval = 20 * time.Millisecond
	t.Cleanup(func() { tickInterval = old })
	t.Setenv("FAKEUV_PIP_SLEEP", "0.3")

	var progress bytes.Buffer
	if _, _, err := Ensure(context.Background(), f.dir, "0.13.0", &progress); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !strings.Contains(progress.String(), "sky: SkyPilot 0.13.0 ...") {
		t.Errorf("the package step added no dots while it ran:\n%s", progress.String())
	}
	if !strings.Contains(progress.String(), ". done\n") {
		t.Errorf("the package step did not end its line with done:\n%s", progress.String())
	}
}

func TestEnsureEmptyVersionInstallsDefault(t *testing.T) {
	f := newFixture(t)
	_, to, err := Ensure(context.Background(), f.dir, "", nil)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if to != DefaultVersion {
		t.Errorf("Ensure installed %q, want %q", to, DefaultVersion)
	}
}

func TestEnsureSameVersionDownloadsNothing(t *testing.T) {
	f := newFixture(t)
	if _, _, err := Ensure(context.Background(), f.dir, "0.13.0", nil); err != nil {
		t.Fatal(err)
	}
	requests, log := f.requests.Load(), f.uvLog(t)

	var progress bytes.Buffer
	from, to, err := Ensure(context.Background(), f.dir, "0.13.0", &progress)
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if from != "0.13.0" || to != "0.13.0" {
		t.Errorf("second Ensure moved from %q to %q, want 0.13.0 to 0.13.0", from, to)
	}
	if got := f.requests.Load(); got != requests {
		t.Errorf("second Ensure made %d requests", got-requests)
	}
	if f.uvLog(t) != log {
		t.Error("second Ensure ran uv")
	}
	if progress.Len() != 0 {
		t.Errorf("second Ensure reported progress: %q", progress.String())
	}
}

func TestEnsureOtherVersionReinstalls(t *testing.T) {
	f := newFixture(t)
	if _, _, err := Ensure(context.Background(), f.dir, "0.13.0", nil); err != nil {
		t.Fatal(err)
	}

	from, to, err := Ensure(context.Background(), f.dir, "0.14.1", nil)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if from != "0.13.0" || to != "0.14.1" {
		t.Errorf("Ensure moved from %q to %q, want 0.13.0 to 0.14.1", from, to)
	}
	if out := runSky(t, f.dir, "status"); out != "private sky 0.14.1 status" {
		t.Errorf("sky printed %q", out)
	}
	if _, err := os.Stat(filepath.Join(f.tools, "skypilot", "0.13.0")); !os.IsNotExist(err) {
		t.Errorf("the previous client is still on disk: %v", err)
	}
	if got := f.requests.Load(); got != 1 {
		t.Errorf("uv was downloaded %d times, want once", got)
	}
}

func TestEnsureFailedPackageKeepsPreviousClient(t *testing.T) {
	f := newFixture(t)
	if _, _, err := Ensure(context.Background(), f.dir, "0.13.0", nil); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKEUV_FAIL_PIP", "1")

	var progress bytes.Buffer
	_, _, err := Ensure(context.Background(), f.dir, "0.14.1", &progress)
	if err == nil {
		t.Fatal("Ensure succeeded with a failing package install")
	}
	if !strings.Contains(progress.String(), "sky: SkyPilot 0.14.1 failed\n") {
		t.Errorf("progress does not mark the step failed:\n%s", progress.String())
	}
	for _, want := range []string{
		"install SkyPilot 0.14.1",
		"error: no matching distribution for skypilot[kubernetes]==0.14.1",
		"\n      hint: skypilot[kubernetes]==0.14.1 has no release for this platform",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if errors.Is(err, ErrUnavailable) {
		t.Errorf("a failed package install is ErrUnavailable: %v", err)
	}
	if v, ok, _ := Version(f.dir); !ok || v != "0.13.0" {
		t.Errorf("Version = %q, %v after a failed reinstall; want 0.13.0", v, ok)
	}
	if out := runSky(t, f.dir, "status"); out != "private sky 0.13.0 status" {
		t.Errorf("sky printed %q", out)
	}

	// The next attempt starts over and succeeds.
	t.Setenv("FAKEUV_FAIL_PIP", "")
	if _, _, err := Ensure(context.Background(), f.dir, "0.14.1", nil); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if out := runSky(t, f.dir, "status"); out != "private sky 0.14.1 status" {
		t.Errorf("sky printed %q", out)
	}
}

func TestEnsureFailedPythonNamesPython(t *testing.T) {
	f := newFixture(t)
	t.Setenv("FAKEUV_FAIL_PYTHON", "1")
	_, _, err := Ensure(context.Background(), f.dir, "0.13.0", nil)
	if err == nil {
		t.Fatal("Ensure succeeded with a failing Python install")
	}
	for _, want := range []string{"install Python " + pythonVersion, "failed to download cpython"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if errors.Is(err, ErrUnavailable) {
		t.Errorf("a failed Python download is ErrUnavailable: %v", err)
	}
	if _, ok, _ := Version(f.dir); ok {
		t.Error("a client is recorded after a failed install")
	}
}

func TestEnsureLongResolverErrorIsUnavailable(t *testing.T) {
	f := newFixture(t)
	t.Setenv("FAKEUV_NO_BUILD", "long")
	_, _, err := Ensure(context.Background(), f.dir, "0.14.1", nil)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Ensure error = %v, want ErrUnavailable", err)
	}
	if strings.Contains(err.Error(), uvNoSolution) {
		t.Fatalf("the resolver's header is still in the kept output; the test needs more lines after it")
	}
}

func TestTailFindsResolverHeaderAcrossWrites(t *testing.T) {
	var tl tail
	for _, b := range []byte("error:\n  × " + uvNoSolution + ":\n") {
		tl.Write([]byte{b})
	}
	tl.Write(bytes.Repeat([]byte("x"), 2*tailSize))
	if !tl.noSolution {
		t.Error("tail missed the resolver's header written a byte at a time")
	}

	var other tail
	other.Write([]byte("error: failed to fetch: connection reset\n"))
	if other.noSolution {
		t.Error("tail found the resolver's header in a network error")
	}
}

func TestEnsureNoPrebuiltPackageIsUnavailable(t *testing.T) {
	f := newFixture(t)
	if _, _, err := Ensure(context.Background(), f.dir, "0.13.0", nil); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKEUV_NO_BUILD", "1")

	_, _, err := Ensure(context.Background(), f.dir, "0.14.1", nil)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Ensure error = %v, want ErrUnavailable", err)
	}
	for _, want := range []string{
		"install SkyPilot 0.14.1",
		uvNoSolution,
		"skypilot[kubernetes]==0.14.1 has no usable wheels",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if strings.Contains(err.Error(), ErrUnavailable.Error()) {
		t.Errorf("error %q carries ErrUnavailable's own text", err)
	}
	if v, ok, _ := Version(f.dir); !ok || v != "0.13.0" {
		t.Errorf("Version = %q, %v after a failed reinstall; want 0.13.0", v, ok)
	}
}

func TestEnsureChecksumMismatchNamesUV(t *testing.T) {
	f := newFixture(t)
	b := uvBuilds["test/platform"]
	b.sha256 = strings.Repeat("0", 64)
	uvBuilds["test/platform"] = b

	var progress bytes.Buffer
	_, _, err := Ensure(context.Background(), f.dir, "0.13.0", &progress)
	if err == nil {
		t.Fatal("Ensure succeeded with a wrong checksum")
	}
	if !strings.HasSuffix(progress.String(), " failed\n") || strings.Contains(progress.String(), "done") {
		t.Errorf("progress does not mark the uv step failed:\n%s", progress.String())
	}
	for _, want := range []string{"download uv " + uvVersion, "checksum"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if errors.Is(err, ErrUnavailable) {
		t.Errorf("a checksum mismatch is ErrUnavailable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.tools, "uv", uvVersion, "uv")); !os.IsNotExist(err) {
		t.Errorf("an unverified uv was kept: %v", err)
	}
	if f.uvLog(t) != "" {
		t.Error("an unverified uv was run")
	}
}

func TestEnsureDownloadFailureNamesUV(t *testing.T) {
	f := newFixture(t)
	b := uvBuilds["test/platform"]
	b.target = "missing"
	uvBuilds["test/platform"] = b

	_, _, err := Ensure(context.Background(), f.dir, "0.13.0", nil)
	if err == nil || !strings.Contains(err.Error(), "download uv "+uvVersion) {
		t.Errorf("Ensure error = %v, want one naming the uv download", err)
	}
	if errors.Is(err, ErrUnavailable) {
		t.Errorf("a failed download is ErrUnavailable: %v", err)
	}
}

func TestEnsureUnsupportedPlatform(t *testing.T) {
	f := newFixture(t)
	platform = "plan9/386"
	_, _, err := Ensure(context.Background(), f.dir, "0.13.0", nil)
	if err == nil || !strings.Contains(err.Error(), "plan9/386") {
		t.Errorf("Ensure error = %v, want one naming the platform", err)
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("an unsupported platform is not ErrUnavailable: %v", err)
	}
	if f.requests.Load() != 0 {
		t.Error("Ensure downloaded on an unsupported platform")
	}
}

func TestEnsureRejectsMalformedVersion(t *testing.T) {
	f := newFixture(t)
	for _, v := range []string{"../evil", "latest", "0.13.0 extra", "0.13.0/x", ">=0.13"} {
		if _, _, err := Ensure(context.Background(), f.dir, v, nil); err == nil {
			t.Errorf("Ensure accepted version %q", v)
		}
	}
	if f.requests.Load() != 0 {
		t.Error("Ensure downloaded for a malformed version")
	}
}

func TestUVEnvironmentStaysInsideTools(t *testing.T) {
	f := newFixture(t)
	t.Setenv("UV_INDEX_URL", "https://example.invalid/simple")
	t.Setenv("UV_CACHE_DIR", "/elsewhere/cache")
	t.Setenv("VIRTUAL_ENV", "/elsewhere/venv")

	if _, _, err := Ensure(context.Background(), f.dir, "0.13.0", nil); err != nil {
		t.Fatal(err)
	}
	log := f.uvLog(t)
	for _, want := range []string{
		"env UV_CACHE_DIR=" + filepath.Join(f.tools, "uv-cache"),
		"env UV_PYTHON_INSTALL_DIR=" + filepath.Join(f.tools, "python"),
		"env UV_PYTHON_BIN_DIR=" + filepath.Join(f.tools, "python-bin"),
		"env UV_TOOL_DIR=" + filepath.Join(f.tools, "uv-tools"),
		"env UV_TOOL_BIN_DIR=" + filepath.Join(f.tools, "uv-tools-bin"),
		"env UV_CREDENTIALS_DIR=" + filepath.Join(f.tools, "uv-credentials"),
		"env UV_NO_CONFIG=1",
		"env UV_MANAGED_PYTHON=1",
		"uv python install " + pythonVersion + " --no-bin",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("uv log lacks %q:\n%s", want, log)
		}
	}
	for _, unwanted := range []string{"UV_INDEX_URL", "/elsewhere/cache"} {
		if strings.Contains(log, unwanted) {
			t.Errorf("uv saw the inherited %q:\n%s", unwanted, log)
		}
	}
}

// The client's install takes pre-built packages only, so no package is
// ever compiled on the laptop.
func TestEnsureInstallsPrebuiltPackagesOnly(t *testing.T) {
	f := newFixture(t)
	if _, _, err := Ensure(context.Background(), f.dir, "0.13.0", nil); err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(f.tools, skypilotDir, "0.13.0", "bin", "python")
	want := "uv pip install --python " + python + " " + skypilotPackage + "==0.13.0 --only-binary :all: --quiet\n"
	if log := f.uvLog(t); !strings.Contains(log, want) {
		t.Errorf("uv log lacks %q:\n%s", want, log)
	}
}

func TestCommandIgnoresSkyOnPath(t *testing.T) {
	f := newFixture(t)
	if _, _, err := Ensure(context.Background(), f.dir, "0.13.0", nil); err != nil {
		t.Fatal(err)
	}

	shadow := t.TempDir()
	marker := filepath.Join(shadow, "ran")
	script := "#!/bin/sh\ntouch " + marker + "\necho shadow sky\n"
	if err := os.WriteFile(filepath.Join(shadow, "sky"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shadow+string(os.PathListSeparator)+os.Getenv("PATH"))

	if out := runSky(t, f.dir, "--version"); out != "private sky 0.13.0 --version" {
		t.Errorf("sky printed %q, want the private client's output", out)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("the sky on PATH was run")
	}
}

func TestNotInstalled(t *testing.T) {
	dir := axondir.New(t.TempDir())
	if v, ok, err := Version(dir); err != nil || ok || v != "" {
		t.Errorf("Version = %q, %v, %v; want \"\", false, nil", v, ok, err)
	}
	if _, err := Path(dir); err == nil {
		t.Error("Path succeeded with no client installed")
	}
}

func TestVersionNeedsTheExecutable(t *testing.T) {
	f := newFixture(t)
	if _, _, err := Ensure(context.Background(), f.dir, "0.13.0", nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.tools, "skypilot", "0.13.0", "bin", "sky")); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := Version(f.dir); err != nil || ok {
		t.Errorf("Version reports a client whose sky is missing: %v, %v", ok, err)
	}

	// Ensure repairs it.
	if _, _, err := Ensure(context.Background(), f.dir, "0.13.0", nil); err != nil {
		t.Fatal(err)
	}
	if out := runSky(t, f.dir, "--version"); out != "private sky 0.13.0 --version" {
		t.Errorf("sky printed %q", out)
	}
}

// syncBuffer is a bytes.Buffer safe for concurrent use.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type ensureResult struct {
	from, to string
	err      error
}

// concurrentEnsure starts an install of first and holds its package stage
// until an install of second, started once the first is inside the lock,
// reports that it is waiting. It returns both results and the second call's
// progress.
func concurrentEnsure(t *testing.T, f *fixture, first, second string) (a, b ensureResult, secondProgress string) {
	t.Helper()
	release := filepath.Join(t.TempDir(), "release")
	t.Setenv("FAKEUV_PIP_RELEASE", release)
	releaseFirst := func() {
		if err := os.WriteFile(release, nil, 0o600); err != nil && !os.IsExist(err) {
			t.Error(err)
		}
	}
	// On a failed wait below, release the held install and join both
	// installs before the fixture's directories and server go away.
	// Cleanups run last first, so this one runs before the fixture's.
	var installs sync.WaitGroup
	t.Cleanup(func() {
		releaseFirst()
		installs.Wait()
	})

	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				releaseFirst()
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	firstDone := make(chan ensureResult, 1)
	installs.Add(1)
	go func() {
		defer installs.Done()
		from, to, err := Ensure(context.Background(), f.dir, first, nil)
		firstDone <- ensureResult{from, to, err}
	}()
	waitFor("the first install to reach its package stage", func() bool {
		return strings.Contains(f.uvLog(t), "uv pip install")
	})

	var progress syncBuffer
	secondDone := make(chan ensureResult, 1)
	installs.Add(1)
	go func() {
		defer installs.Done()
		from, to, err := Ensure(context.Background(), f.dir, second, &progress)
		secondDone <- ensureResult{from, to, err}
	}()
	waitFor("the second install to report that it waits", func() bool {
		return strings.Contains(progress.String(), "sky: waiting for another axon")
	})

	releaseFirst()
	a, b = <-firstDone, <-secondDone
	return a, b, progress.String()
}

func TestConcurrentEnsureSameVersionInstallsOnce(t *testing.T) {
	f := newFixture(t)
	a, b, progress := concurrentEnsure(t, f, "0.13.0", "0.13.0")
	if a.err != nil || b.err != nil {
		t.Fatalf("Ensure errors: %v, %v", a.err, b.err)
	}
	if a.from != "" || a.to != "0.13.0" {
		t.Errorf("first Ensure moved from %q to %q", a.from, a.to)
	}
	if b.from != "0.13.0" || b.to != "0.13.0" {
		t.Errorf("second Ensure moved from %q to %q, want no change", b.from, b.to)
	}
	if !strings.Contains(progress, "sky: waiting for another axon") {
		t.Errorf("the second Ensure did not say it waited: %q", progress)
	}
	if n := strings.Count(f.uvLog(t), "uv pip install"); n != 1 {
		t.Errorf("the package was installed %d times, want once", n)
	}
	if out := runSky(t, f.dir, "--version"); out != "private sky 0.13.0 --version" {
		t.Errorf("sky printed %q", out)
	}
}

func TestConcurrentEnsureOtherVersionWaitsThenReinstalls(t *testing.T) {
	f := newFixture(t)
	a, b, progress := concurrentEnsure(t, f, "0.13.0", "0.14.1")
	if a.err != nil || b.err != nil {
		t.Fatalf("Ensure errors: %v, %v", a.err, b.err)
	}
	if b.from != "0.13.0" || b.to != "0.14.1" {
		t.Errorf("second Ensure moved from %q to %q, want 0.13.0 to 0.14.1", b.from, b.to)
	}
	if !strings.Contains(progress, "sky: waiting for another axon") {
		t.Errorf("the second Ensure did not say it waited: %q", progress)
	}
	if v, ok, err := Version(f.dir); err != nil || !ok || v != "0.14.1" {
		t.Errorf("Version = %q, %v, %v; want 0.14.1", v, ok, err)
	}
	if out := runSky(t, f.dir, "--version"); out != "private sky 0.14.1 --version" {
		t.Errorf("sky printed %q", out)
	}
}

func TestEnsureWaitHonorsCancellation(t *testing.T) {
	f := newFixture(t)
	tools, err := f.dir.ToolsDir()
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := lockTools(context.Background(), tools, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var progress bytes.Buffer
	_, _, err = Ensure(ctx, f.dir, "0.13.0", &progress)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Ensure error = %v, want the context's deadline", err)
	}
	if !strings.Contains(progress.String(), "sky: waiting for another axon") {
		t.Errorf("Ensure did not say it waited: %q", progress.String())
	}
	if f.requests.Load() != 0 {
		t.Error("Ensure downloaded while another install held the lock")
	}
}

// Reading the version of a client that is not installed creates nothing.
func TestVersionCreatesNothing(t *testing.T) {
	home := t.TempDir()
	if _, installed, err := Version(axondir.New(home)); installed || err != nil {
		t.Fatalf("Version = %v, %v; want not installed", installed, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".axon")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Version created ~/.axon: %v", err)
	}
}

func TestDamagedVersionRecordIsNoClient(t *testing.T) {
	for name, damage := range map[string]func(path string) error{
		"empty":      func(path string) error { return os.WriteFile(path, nil, 0o600) },
		"blank":      func(path string) error { return os.WriteFile(path, []byte(" \n"), 0o600) },
		"garbled":    func(path string) error { return os.WriteFile(path, []byte("\x00\x01junk"), 0o600) },
		"unreadable": func(path string) error { return os.Chmod(path, 0) },
		"directory": func(path string) error {
			if err := os.Remove(path); err != nil {
				return err
			}
			return os.Mkdir(path, 0o700)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			if _, _, err := Ensure(context.Background(), f.dir, "0.13.0", nil); err != nil {
				t.Fatal(err)
			}
			current := filepath.Join(f.tools, "skypilot", "current")
			if err := damage(current); err != nil {
				t.Fatal(err)
			}
			if v, ok, err := Version(f.dir); err != nil || ok || v != "" {
				t.Errorf("Version = %q, %v, %v; want \"\", false, nil", v, ok, err)
			}
			if _, err := Path(f.dir); !errors.Is(err, ErrNotInstalled) {
				t.Errorf("Path error = %v, want ErrNotInstalled", err)
			}
			// An install repairs it.
			from, to, err := Ensure(context.Background(), f.dir, "0.13.0", nil)
			if err != nil {
				t.Fatalf("Ensure: %v", err)
			}
			if from != "" || to != "0.13.0" {
				t.Errorf("Ensure moved %q -> %q, want \"\" -> 0.13.0", from, to)
			}
			if v, ok, err := Version(f.dir); err != nil || !ok || v != "0.13.0" {
				t.Errorf("Version after the repair = %q, %v, %v; want 0.13.0", v, ok, err)
			}
			if out := runSky(t, f.dir, "--version"); out != "private sky 0.13.0 --version" {
				t.Errorf("sky printed %q", out)
			}
		})
	}
}

func TestReplaceFileLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "current")
	for _, content := range []string{"0.13.0\n", "0.14.0\n"} {
		if err := writeFile(path, content); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != content {
			t.Errorf("file = %q, %v; want %q", data, err, content)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only the file", len(entries))
	}
}

func TestClientEnvDropsPythonVars(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"PYTHONPATH=/elsewhere/lib",
		"PYTHONHOME=/elsewhere",
		"VIRTUAL_ENV=/elsewhere/venv",
		"CONDA_PREFIX=/elsewhere/conda",
		"UV_INDEX_URL=https://index.example.org",
		"PYTHONPATHX=kept",
		"HOME=/home/ada",
	}
	want := []string{"PATH=/usr/bin", "UV_INDEX_URL=https://index.example.org", "PYTHONPATHX=kept", "HOME=/home/ada"}
	if got := ClientEnv(env); !slices.Equal(got, want) {
		t.Errorf("ClientEnv = %q, want %q", got, want)
	}
}
