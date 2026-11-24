// Package testkit holds what axon's tests share: the fence that keeps a
// test off the real machine, the fake tsh, and small helpers. Only test
// files import it, so it is never part of the axon binary.
//
// No test reaches a real service. A package whose tests run commands
// calls Fence from its TestMain, so that HOME is an empty directory and
// PATH holds only the tools the package names, and no real login tool,
// client or browser can be found. A test that runs the built axon starts
// it with Command, which gives the process its own empty HOME and PATH
// and no controlling terminal.
package testkit

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// FakeTshPackage is the import path of the fake tsh.
const FakeTshPackage = "github.com/enigma-brain/axon/internal/testkit/faketsh"

// Fenced is the temporary directory a package's tests run in.
type Fenced struct {
	// Dir holds Home, the fence's PATH directory, and the fake tsh.
	Dir string
	// Home is HOME.
	Home string
	// Tsh is the path of the fake tsh, when it was asked for.
	Tsh string

	inherited bool
}

// fenceVar names the fence's directory in the environment of a fenced
// process and of every process it starts.
const fenceVar = "AXON_TEST_FENCE"

// FenceOptions says what Fence sets up.
type FenceOptions struct {
	// Tools are the commands put on PATH, linked from where the PATH in
	// force finds them. Every one must be found.
	Tools []string
	// FakeTsh builds the fake tsh into Dir, as a file named tsh. It is
	// not put on PATH.
	FakeTsh bool
}

// Fence makes a temporary directory, builds the fake tsh into it when
// asked, and then, for the rest of the process, sets HOME to an empty
// directory in it and PATH to a directory in it that holds only links to
// opts.Tools. It is meant for TestMain, before the tests run; Close
// removes the directory.
//
// The fake tsh is built with go build, which reuses Go's build cache, so
// it is compiled once and linked by each package that asks for it.
//
// A test binary that a fenced test starts again, as a helper process,
// inherits the fence: Fence then changes nothing, so that the HOME the
// parent gave the helper stays, and Close removes nothing.
func Fence(opts FenceOptions) (*Fenced, error) {
	if dir := os.Getenv(fenceVar); dir != "" {
		f := &Fenced{Dir: dir, Home: filepath.Join(dir, "home"), inherited: true}
		if opts.FakeTsh {
			f.Tsh = filepath.Join(dir, "tsh")
		}
		return f, nil
	}
	dir, err := os.MkdirTemp("", "axon-test")
	if err != nil {
		return nil, err
	}
	f := &Fenced{Dir: dir, Home: filepath.Join(dir, "home")}
	if err := f.setUp(opts); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return f, nil
}

func (f *Fenced) setUp(opts FenceOptions) error {
	if opts.FakeTsh {
		f.Tsh = filepath.Join(f.Dir, "tsh")
		build := exec.Command("go", "build", "-o", f.Tsh, FakeTshPackage)
		if out, err := build.CombinedOutput(); err != nil {
			return fmt.Errorf("build the fake tsh: %v\n%s", err, out)
		}
	}
	path := filepath.Join(f.Dir, "path")
	for _, d := range []string{f.Home, path} {
		if err := os.Mkdir(d, 0o700); err != nil {
			return err
		}
	}
	for _, tool := range opts.Tools {
		real, err := exec.LookPath(tool)
		if err != nil {
			return err
		}
		if err := os.Symlink(real, filepath.Join(path, tool)); err != nil {
			return err
		}
	}
	if err := os.Setenv("HOME", f.Home); err != nil {
		return err
	}
	if err := os.Setenv(fenceVar, f.Dir); err != nil {
		return err
	}
	return os.Setenv("PATH", path)
}

// Close removes the fence's directory, unless the fence was inherited.
func (f *Fenced) Close() {
	if !f.inherited {
		os.RemoveAll(f.Dir)
	}
}

// Main is a TestMain for a package that needs nothing but the fence: it
// fences the process as Fence does, runs the tests, and returns the exit
// code.
func Main(m *testing.M, opts FenceOptions) int {
	f, err := Fence(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer f.Close()
	return m.Run()
}

// FreePort returns a port on the loopback address that was free when it
// was asked for, so that a test's tunnel never touches a real one.
func FreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// deadProxy is a proxy address where nothing listens.
const deadProxy = "http://127.0.0.1:1"

// DeadProxy is the environment that sends every HTTP and HTTPS request
// through a proxy where nothing listens, so that no request reaches a
// real server.
func DeadProxy() []string {
	return []string{
		"HTTPS_PROXY=" + deadProxy,
		"HTTP_PROXY=" + deadProxy,
		"https_proxy=" + deadProxy,
		"http_proxy=" + deadProxy,
	}
}

// Command returns the command that runs name with args inside a fence:
// its environment is HOME set to home, PATH set to a new empty temporary
// directory, and then env, and it runs in a new session, with no
// controlling terminal, so a question meant for the terminal finds none.
// Every test that runs the built axon starts it this way.
func Command(t testing.TB, home string, env []string, name string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Env = append([]string{"HOME=" + home, "PATH=" + t.TempDir()}, env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd
}

// Snapshot returns every file and directory under root. A file maps to
// its content, and a directory, whose key ends in a slash, to "". A
// missing root gives an empty map.
func Snapshot(t testing.TB, root string) map[string]string {
	t.Helper()
	return snapshot(t, root, false)
}

// SnapshotTimes is Snapshot with each entry's mode and modification time
// before its content, for a test that checks nothing under root was even
// rewritten.
func SnapshotTimes(t testing.TB, root string) map[string]string {
	t.Helper()
	return snapshot(t, root, true)
}

func snapshot(t testing.TB, root string, times bool) map[string]string {
	t.Helper()
	entries := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		prefix := ""
		if times {
			info, err := d.Info()
			if err != nil {
				return err
			}
			prefix = info.Mode().String() + " " + info.ModTime().String() + " "
		}
		if d.IsDir() {
			entries[path+"/"] = strings.TrimSuffix(prefix, " ")
			return nil
		}
		data, err := os.ReadFile(path)
		entries[path] = prefix + string(data)
		return err
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return entries
}

// AssertMode reports an error unless the permission bits of path are
// want.
func AssertMode(t testing.TB, path string, want fs.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s has mode %o, want %o", path, got, want)
	}
}

// WaitTimeout bounds WaitUntil.
const WaitTimeout = 10 * time.Second

// WaitUntil fails the test when cond is not true within WaitTimeout. It
// checks cond every 20 milliseconds.
func WaitUntil(t testing.TB, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(WaitTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %v", WaitTimeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ExampleConfig is a config file with a direct pathway, campus, and a
// teleport pathway, remote. Every detail is made up.
const ExampleConfig = `format = 2
id = "example"

[[pathway]]
id = "campus"
kind = "direct"
name = "Campus"
description = "Single sign-on in a browser. Needs the campus network."
unreachable_hint = "Connect to the campus VPN and retry."

[pathway.sky]
endpoint = "https://sky.example.org"

[[pathway]]
id = "remote"
kind = "teleport"
name = "Remote"
description = "Works from anywhere."

[pathway.teleport]
proxy = "teleport.example.org"

[pathway.sky]
app = "skypilot"
`
