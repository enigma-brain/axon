package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCurl stands in for curl. It answers only the addresses of the
// repository's releases:
//
//   - releases/latest reports, for -w '%{url_effective}', the address of the
//     tag page named by $FAKE_LATEST: a tag such as v1.0.0, "none" for a
//     redirect that ends at no tag, or "404" for a failed request;
//   - releases/download/<tag>/<file> serves $FAKE_ASSETS/<tag>/<file>, or
//     fails with curl's exit status 22 when the file is absent.
//
// It appends each call's arguments to $FAKE_LOG.
const fakeCurl = `#!/bin/sh
echo "$*" >> "$FAKE_LOG"
out= fmt= url=
while [ $# -gt 0 ]; do
	case "$1" in
	-o) out=$2; shift 2 ;;
	-w) fmt=$2; shift 2 ;;
	--proto | --retry) shift 2 ;;
	-*) shift ;;
	*) url=$1; shift ;;
	esac
done
site=https://github.com/enigma-brain/axon
case "$url" in
"$site/releases/latest")
	case "$FAKE_LATEST" in
	404) echo "curl: (22) The requested URL returned error: 404" >&2; exit 22 ;;
	none) final=$site/releases ;;
	*) final=$site/releases/tag/$FAKE_LATEST ;;
	esac
	[ "$fmt" = '%{url_effective}' ] && printf '%s' "$final"
	exit 0
	;;
"$site/releases/download/"*)
	f=$FAKE_ASSETS/${url#"$site/releases/download/"}
	if [ ! -f "$f" ]; then
		echo "curl: (22) The requested URL returned error: 404" >&2
		exit 22
	fi
	cp "$f" "$out"
	exit 0
	;;
esac
echo "fake curl: unexpected address $url" >&2
exit 6
`

// fakeUname prints $FAKE_UNAME_S for -s and $FAKE_UNAME_M for -m.
const fakeUname = `#!/bin/sh
case "$1" in
-s) echo "$FAKE_UNAME_S" ;;
-m) echo "$FAKE_UNAME_M" ;;
esac
`

// installEnv is one run of install.sh against a temporary home.
type installEnv struct {
	t      *testing.T
	home   string
	assets string // one directory per release tag, holding its files
	log    string
	fakes  string // the fake curl and uname
	tools  string // links to the real tools install.sh uses
	env    map[string]string
}

func newInstallEnv(t *testing.T) *installEnv {
	t.Helper()
	root := t.TempDir()
	e := &installEnv{
		t:      t,
		home:   filepath.Join(root, "home"),
		assets: filepath.Join(root, "assets"),
		log:    filepath.Join(root, "curl.log"),
		fakes:  filepath.Join(root, "fakes"),
		tools:  filepath.Join(root, "tools"),
	}
	for _, d := range []string{e.home, e.assets, e.fakes, e.tools} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeExec(t, filepath.Join(e.fakes, "curl"), fakeCurl)
	writeExec(t, filepath.Join(e.fakes, "uname"), fakeUname)

	// Link only the tools install.sh and the fakes use, so that a real
	// curl, gh, uname or browser on the machine is never found.
	for _, name := range []string{
		"sh", "awk", "basename", "cat", "chmod", "cp", "cut", "gzip", "mkdir",
		"mktemp", "mv", "perl", "rm", "sha256sum", "shasum", "tar",
	} {
		if p, err := exec.LookPath(name); err == nil {
			if err := os.Symlink(p, filepath.Join(e.tools, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	e.env = map[string]string{
		"HOME":         e.home,
		"FAKE_ASSETS":  e.assets,
		"FAKE_LOG":     e.log,
		"FAKE_LATEST":  "v1.0.0",
		"FAKE_UNAME_S": "Linux",
		"FAKE_UNAME_M": "x86_64",
	}
	return e
}

func writeExec(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

// release adds an archive for platform holding an axon that prints
// "axon <version>" to release v<version>, and adds its line to that
// release's checksums.txt.
func (e *installEnv) release(version, platform string) (archive string) {
	e.t.Helper()
	return e.releaseScript(version, platform, fmt.Sprintf("#!/bin/sh\necho \"axon %s\"\n", version))
}

// releaseScript is release with script as the archive's axon.
func (e *installEnv) releaseScript(version, platform, script string) (archive string) {
	e.t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "axon", Mode: 0o755, Size: int64(len(script))}); err != nil {
		e.t.Fatal(err)
	}
	if _, err := tw.Write([]byte(script)); err != nil {
		e.t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		e.t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		e.t.Fatal(err)
	}
	dir := e.releaseDir("v" + version)
	archive = fmt.Sprintf("axon_%s_%s.tar.gz", version, platform)
	if err := os.WriteFile(filepath.Join(dir, archive), buf.Bytes(), 0o644); err != nil {
		e.t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	f, err := os.OpenFile(filepath.Join(dir, "checksums.txt"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		e.t.Fatal(err)
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%s  %s\n", hex.EncodeToString(sum[:]), archive); err != nil {
		e.t.Fatal(err)
	}
	return archive
}

func (e *installEnv) releaseDir(tag string) string {
	e.t.Helper()
	dir := filepath.Join(e.assets, tag)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	return dir
}

// checksums replaces the checksums.txt of release tag.
func (e *installEnv) checksums(tag, content string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.releaseDir(tag), "checksums.txt"), []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// run runs install.sh under sh, as the install line does, with the script
// on standard input.
func (e *installEnv) run(extraPath string) (stdout, stderr string, err error) {
	e.t.Helper()
	script, err := os.ReadFile("install.sh")
	if err != nil {
		e.t.Fatal(err)
	}
	path := e.fakes + string(os.PathListSeparator) + e.tools
	if extraPath != "" {
		path = extraPath + string(os.PathListSeparator) + path
	}
	cmd := exec.Command(filepath.Join(e.tools, "sh"))
	cmd.Stdin = bytes.NewReader(script)
	cmd.Env = []string{"PATH=" + path}
	for k, v := range e.env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

func (e *installEnv) binDir() string { return filepath.Join(e.home, ".local", "bin") }

func (e *installEnv) installedVersion() string {
	e.t.Helper()
	out, err := exec.Command(filepath.Join(e.binDir(), "axon")).Output()
	if err != nil {
		e.t.Fatalf("run the installed axon: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// curlCalls is the arguments of every curl call, one call per line.
func (e *installEnv) curlCalls() string {
	b, err := os.ReadFile(e.log)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		e.t.Fatal(err)
	}
	return string(b)
}

const site = "https://github.com/enigma-brain/axon"

func requireFailure(t *testing.T, stdout, stderr string, err error) {
	t.Helper()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
		t.Fatalf("err = %v, want a non-zero exit\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
}

func TestInstall(t *testing.T) {
	e := newInstallEnv(t)
	e.release("1.0.0", "linux_amd64")
	e.release("1.0.0", "darwin_arm64")

	stdout, stderr, err := e.run("")
	if err != nil {
		t.Fatalf("install: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if got := e.installedVersion(); got != "axon 1.0.0" {
		t.Errorf("installed version = %q, want %q", got, "axon 1.0.0")
	}
	if !strings.Contains(stdout, "Installed axon 1.0.0") {
		t.Errorf("stdout does not report the installed version:\n%s", stdout)
	}
	if !strings.Contains(stdout, `export PATH="$HOME/.local/bin:$PATH"`) {
		t.Errorf("stdout does not give the PATH line when ~/.local/bin is not on PATH:\n%s", stdout)
	}
	calls := e.curlCalls()
	for _, want := range []string{
		site + "/releases/latest",
		site + "/releases/download/v1.0.0/checksums.txt",
		site + "/releases/download/v1.0.0/axon_1.0.0_linux_amd64.tar.gz",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("curl was not called for %s:\n%s", want, calls)
		}
	}
	for _, bad := range []string{"Authorization", "-H ", "--header", "token"} {
		if strings.Contains(calls, bad) {
			t.Errorf("a curl call carries %q, but the install uses no credentials:\n%s", bad, calls)
		}
	}
	leftovers, _ := filepath.Glob(filepath.Join(e.binDir(), ".axon.*"))
	if len(leftovers) != 0 {
		t.Errorf("staging files left in %s: %v", e.binDir(), leftovers)
	}
}

func TestInstallLatestUsesOneRelease(t *testing.T) {
	// A newer release exists, but the latest lookup named v1.0.0: the
	// checksums and the archive both come from v1.0.0.
	e := newInstallEnv(t)
	e.release("1.0.0", "linux_amd64")
	e.release("1.1.0", "linux_amd64")
	if _, stderr, err := e.run(""); err != nil {
		t.Fatalf("install: %v\n%s", err, stderr)
	}
	if got := e.installedVersion(); got != "axon 1.0.0" {
		t.Errorf("installed version = %q, want %q", got, "axon 1.0.0")
	}
	if strings.Contains(e.curlCalls(), "v1.1.0") {
		t.Errorf("curl was asked for a release the latest lookup did not name:\n%s", e.curlCalls())
	}
}

func TestInstallAgainReplaces(t *testing.T) {
	e := newInstallEnv(t)
	e.env["FAKE_UNAME_S"], e.env["FAKE_UNAME_M"] = "Darwin", "arm64"
	e.release("1.0.0", "darwin_arm64")
	if _, stderr, err := e.run(""); err != nil {
		t.Fatalf("first install: %v\n%s", err, stderr)
	}
	e.release("1.1.0", "darwin_arm64")
	e.env["FAKE_LATEST"] = "v1.1.0"

	// With ~/.local/bin on PATH, there is no PATH line.
	stdout, stderr, err := e.run(e.binDir())
	if err != nil {
		t.Fatalf("second install: %v\n%s", err, stderr)
	}
	if got := e.installedVersion(); got != "axon 1.1.0" {
		t.Errorf("installed version = %q, want %q", got, "axon 1.1.0")
	}
	if strings.Contains(stdout, "export PATH") {
		t.Errorf("stdout gives the PATH line although ~/.local/bin is on PATH:\n%s", stdout)
	}
}

func TestInstallNamedVersion(t *testing.T) {
	for _, v := range []string{"0.1.0-rc.1", "v0.1.0-rc.1"} {
		t.Run(v, func(t *testing.T) {
			e := newInstallEnv(t)
			e.env["AXON_VERSION"] = v
			e.env["FAKE_LATEST"] = "none" // a pre-release is never the latest
			e.release("0.1.0-rc.1", "linux_amd64")
			stdout, stderr, err := e.run("")
			if err != nil {
				t.Fatalf("install: %v\n%s", err, stderr)
			}
			if got := e.installedVersion(); got != "axon 0.1.0-rc.1" {
				t.Errorf("installed version = %q", got)
			}
			calls := e.curlCalls()
			if strings.Contains(calls, "releases/latest") {
				t.Errorf("curl looked up the latest release although a version was named:\n%s", calls)
			}
			if !strings.Contains(calls, site+"/releases/download/v0.1.0-rc.1/axon_0.1.0-rc.1_linux_amd64.tar.gz") {
				t.Errorf("curl was not asked for release v0.1.0-rc.1:\n%s", calls)
			}
			if !strings.Contains(stdout, "axon 0.1.0-rc.1 for linux_amd64") {
				t.Errorf("stdout does not name the version:\n%s", stdout)
			}
		})
	}
}

func TestInstallBinaryDoesNotRun(t *testing.T) {
	e := newInstallEnv(t)
	e.releaseScript("1.0.0", "linux_amd64", "#!/bin/sh\nexit 1\n")
	stdout, stderr, err := e.run("")
	requireFailure(t, stdout, stderr, err)
	if !strings.Contains(stderr, "axon install: ") || !strings.Contains(stderr, "was written but does not run") {
		t.Errorf("stderr = %q, want the binary named as not running", stderr)
	}
	if strings.Contains(stdout, "Installed") {
		t.Errorf("stdout reports success:\n%s", stdout)
	}
	leftovers, _ := filepath.Glob(filepath.Join(e.binDir(), ".axon.*"))
	if len(leftovers) != 0 {
		t.Errorf("staging files left in %s: %v", e.binDir(), leftovers)
	}
}

func TestInstallFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(e *installEnv)
		want  string
	}{
		{
			name:  "unsupported platform",
			setup: func(e *installEnv) { e.env["FAKE_UNAME_M"] = "aarch64" },
			want:  "there is no axon release for Linux on aarch64",
		},
		{
			name: "curl missing",
			setup: func(e *installEnv) {
				if err := os.Remove(filepath.Join(e.fakes, "curl")); err != nil {
					e.t.Fatal(err)
				}
			},
			want: "curl is not installed",
		},
		{
			name: "no checksum tool",
			setup: func(e *installEnv) {
				for _, n := range []string{"sha256sum", "shasum"} {
					os.Remove(filepath.Join(e.tools, n))
				}
			},
			want: "neither sha256sum nor shasum is installed",
		},
		{
			name:  "no full release exists",
			setup: func(e *installEnv) { e.env["FAKE_LATEST"] = "none" },
			want:  "no release of enigma-brain/axon was found. Name one with AXON_VERSION",
		},
		{
			name:  "latest lookup fails",
			setup: func(e *installEnv) { e.env["FAKE_LATEST"] = "404" },
			want:  "could not look up the latest release at " + site + "/releases/latest; no release of enigma-brain/axon was found. Name one with AXON_VERSION",
		},
		{
			name:  "named release missing",
			setup: func(e *installEnv) { e.env["AXON_VERSION"] = "9.9.9" },
			want:  "could not download checksums.txt for release v9.9.9 from " + site + "/releases/download/v9.9.9/checksums.txt",
		},
		{
			name: "archive asset missing",
			setup: func(e *installEnv) {
				if err := os.Remove(filepath.Join(e.assets, "v1.0.0", "axon_1.0.0_linux_amd64.tar.gz")); err != nil {
					e.t.Fatal(err)
				}
			},
			want: "could not download axon_1.0.0_linux_amd64.tar.gz for release v1.0.0 from " + site + "/releases/download/v1.0.0/axon_1.0.0_linux_amd64.tar.gz",
		},
		{
			name: "checksums asset missing",
			setup: func(e *installEnv) {
				if err := os.Remove(filepath.Join(e.assets, "v1.0.0", "checksums.txt")); err != nil {
					e.t.Fatal(err)
				}
			},
			want: "could not download checksums.txt for release v1.0.0",
		},
		{
			name: "checksum mismatch",
			setup: func(e *installEnv) {
				e.checksums("v1.0.0", strings.Repeat("0", 64)+"  axon_1.0.0_linux_amd64.tar.gz\n")
			},
			want: "the checksum of axon_1.0.0_linux_amd64.tar.gz does not match checksums.txt",
		},
		{
			name:  "checksum missing",
			setup: func(e *installEnv) { e.checksums("v1.0.0", "") },
			want:  "checksums.txt in release v1.0.0 has no checksum for axon_1.0.0_linux_amd64.tar.gz",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newInstallEnv(t)
			e.release("1.0.0", "linux_amd64")
			tc.setup(e)
			stdout, stderr, err := e.run("")
			requireFailure(t, stdout, stderr, err)
			if !strings.Contains(stderr, "axon install: "+tc.want) {
				t.Errorf("stderr = %q, want a line starting \"axon install: %s\"", stderr, tc.want)
			}
			if _, err := os.Stat(e.binDir()); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("%s exists after a failed install (stat: %v)", e.binDir(), err)
			}
		})
	}
}

// TestInstallFromGitHub runs install.sh against the real release. It is off
// unless AXON_REAL_DOWNLOAD is set, since it downloads from GitHub; it names
// the release in AXON_REAL_DOWNLOAD_VERSION, or installs the latest one when
// that is empty.
func TestInstallFromGitHub(t *testing.T) {
	if os.Getenv("AXON_REAL_DOWNLOAD") == "" {
		t.Skip("set AXON_REAL_DOWNLOAD=1 to download from GitHub")
	}
	script, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	cmd := exec.Command("sh")
	cmd.Stdin = bytes.NewReader(script)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home}
	if v := os.Getenv("AXON_REAL_DOWNLOAD_VERSION"); v != "" {
		cmd.Env = append(cmd.Env, "AXON_VERSION="+v)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("install: %v\nstdout: %s\nstderr: %s", err, out.String(), errOut.String())
	}
	got, err := exec.Command(filepath.Join(home, ".local", "bin", "axon"), "--version").Output()
	if err != nil {
		t.Fatalf("run the installed axon: %v", err)
	}
	if !strings.HasPrefix(string(got), "axon ") {
		t.Errorf("axon --version printed %q", got)
	}
}
