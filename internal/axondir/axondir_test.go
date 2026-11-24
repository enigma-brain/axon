package axondir

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/iotest"

	"github.com/enigma-brain/axon/internal/testkit"
)

func newDir(t *testing.T) (Dir, string) {
	t.Helper()
	home := t.TempDir()
	return New(home), home
}

func TestNewIsUnderHome(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	d := New(home)
	if want := filepath.Join(home, ".axon"); d.Root() != want {
		t.Errorf("Root() = %q, want %q", d.Root(), want)
	}
	if d.Home() != home {
		t.Errorf("Home() = %q, want %q", d.Home(), home)
	}
}

func TestNewCreatesNothing(t *testing.T) {
	t.Parallel()
	d, _ := newDir(t)
	if _, err := os.Stat(d.Root()); !os.IsNotExist(err) {
		t.Fatalf("New created %s", d.Root())
	}
}

func TestDirectoriesCreatedOnFirstUse(t *testing.T) {
	t.Parallel()
	d, home := newDir(t)
	for name, get := range map[string]func() (string, error){
		"configs": d.ConfigsDir,
		"tools":   d.ToolsDir,
		"state":   d.StateDir,
	} {
		path, err := get()
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(home, ".axon", name); path != want {
			t.Errorf("%s dir = %q, want %q", name, path, want)
		}
		testkit.AssertMode(t, path, 0o700)
		// A second call finds the directory in place.
		if again, err := get(); err != nil || again != path {
			t.Errorf("second call = %q, %v", again, err)
		}
	}
	testkit.AssertMode(t, d.Root(), 0o700)
}

func TestDirectoryThatIsAFile(t *testing.T) {
	t.Parallel()
	d, _ := newDir(t)
	if err := os.Mkdir(d.Root(), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tools", "state"} {
		if err := os.WriteFile(filepath.Join(d.Root(), name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.ToolsDir(); err == nil {
		t.Error("ToolsDir() succeeded when tools is a file")
	}
	if _, err := d.StateDir(); err == nil {
		t.Error("StateDir() succeeded when state is a file")
	}
}

func TestPathwayRoundTrip(t *testing.T) {
	d, _ := newDir(t)
	if p, ok, err := d.Pathway(); err != nil || ok || p != "" {
		t.Fatalf("Pathway() before any write = %q, %v, %v", p, ok, err)
	}
	for _, want := range []string{"campus", "remote"} {
		if err := d.SetPathway(want); err != nil {
			t.Fatal(err)
		}
		got, ok, err := d.Pathway()
		if err != nil || !ok || got != want {
			t.Fatalf("Pathway() = %q, %v, %v, want %q", got, ok, err, want)
		}
	}
	testkit.AssertMode(t, d.SettingsPath(), 0o600)
	testkit.AssertMode(t, d.Root(), 0o700)

	data, err := os.ReadFile(d.SettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "pathway = \"remote\"\n"; got != want {
		t.Errorf("settings.toml = %q, want %q", got, want)
	}
}

func TestSetPathwayRejectsEmpty(t *testing.T) {
	t.Parallel()
	d, _ := newDir(t)
	for _, id := range []string{"", "a\nb"} {
		if err := d.SetPathway(id); err == nil {
			t.Errorf("SetPathway(%q) succeeded", id)
		}
	}
}

func TestPathwayRejectsBadConfig(t *testing.T) {
	t.Parallel()
	d, _ := newDir(t)
	if err := os.Mkdir(d.Root(), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"pathway = \n", "pathway = 3\n"} {
		if err := os.WriteFile(d.SettingsPath(), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := d.Pathway(); err == nil {
			t.Errorf("Pathway() with config %q succeeded", content)
		}
	}
}

func TestRemoveEmail(t *testing.T) {
	t.Parallel()
	d, _ := newDir(t)
	if err := d.RemoveEmail(); err != nil {
		t.Fatalf("RemoveEmail with none recorded: %v", err)
	}
	if err := d.SetEmail("ada@example.org"); err != nil {
		t.Fatal(err)
	}
	if err := d.RemoveEmail(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := d.Email(); ok || err != nil {
		t.Errorf("Email() after RemoveEmail = %v, %v", ok, err)
	}
}

func TestEmailRoundTrip(t *testing.T) {
	t.Parallel()
	d, home := newDir(t)
	if e, ok, err := d.Email(); err != nil || ok || e != "" {
		t.Fatalf("Email() before any write = %q, %v, %v", e, ok, err)
	}
	if err := d.SetEmail("ada@example.org"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := d.Email()
	if err != nil || !ok || got != "ada@example.org" {
		t.Fatalf("Email() = %q, %v, %v", got, ok, err)
	}
	path := filepath.Join(home, ".axon", "state", "email")
	testkit.AssertMode(t, path, 0o600)
	testkit.AssertMode(t, filepath.Dir(path), 0o700)
}

func TestSetEmailRejectsBlank(t *testing.T) {
	t.Parallel()
	d, _ := newDir(t)
	for _, e := range []string{"", "  ", "a@b\nc@d"} {
		if err := d.SetEmail(e); err == nil {
			t.Errorf("SetEmail(%q) succeeded", e)
		}
	}
}

func TestReplaceFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	for _, content := range []string{"first\n", "second\n"} {
		if err := ReplaceFile(path, strings.NewReader(content), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != content {
			t.Errorf("content = %q, want %q", got, content)
		}
	}
	testkit.AssertMode(t, path, 0o600)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only the file", len(entries))
	}
}

func TestReplaceFileMode(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "f")
	for _, mode := range []fs.FileMode{0o755, 0o600} {
		if err := ReplaceFile(path, strings.NewReader("x"), mode); err != nil {
			t.Fatal(err)
		}
		testkit.AssertMode(t, path, mode)
	}
}

// KeepMode keeps the permissions of the file replaced, and FollowLink
// replaces the file a link points to and keeps the link.
func TestReplaceFileKeepModeFollowLink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceFile(link, strings.NewReader("new"), 0o600, KeepMode(), FollowLink()); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("link after the write: %v, %v; want a symbolic link", info, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
		t.Errorf("target = %q, %v; want new", got, err)
	}
	testkit.AssertMode(t, target, 0o640)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("directory holds %d entries, want the link and its target", len(entries))
	}
}

// Without FollowLink a link at the path is itself replaced, and without
// KeepMode the mode given is used.
func TestReplaceFileReplacesALink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceFile(link, strings.NewReader("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("link after the write: %v, %v; want a regular file", info, err)
	}
	if got, _ := os.ReadFile(target); string(got) != "old" {
		t.Errorf("target = %q, want it unchanged", got)
	}
	testkit.AssertMode(t, link, 0o600)
}

// KeepMode needs the file to exist.
func TestReplaceFileKeepModeMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := ReplaceFile(path, strings.NewReader("x"), 0o600, KeepMode()); err == nil {
		t.Fatal("ReplaceFile with KeepMode of a missing file succeeded")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("directory holds %d entries, want none", len(entries))
	}
}

// A reader that fails leaves the file as it was and no temporary file.
func TestReplaceFileReadFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(errors.New("injected failure")))
	err := ReplaceFile(path, r, 0o600)
	if err == nil || !strings.Contains(err.Error(), "injected failure") || !strings.Contains(err.Error(), path) {
		t.Fatalf("ReplaceFile = %v, want the failure, naming the path", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "old" {
		t.Errorf("file = %q, want it unchanged", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only the file", len(entries))
	}
}

// A directory sync the filesystem does not support counts as done, and any
// other failure is reported.
func TestSyncDirOnAFilesystemWithoutDirectorySync(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range []struct {
		err  error
		fail bool
	}{
		{syscall.EINVAL, false},
		{syscall.ENOTSUP, false},
		{syscall.EOPNOTSUPP, false},
		{&os.PathError{Op: "sync", Path: dir, Err: syscall.EINVAL}, false},
		{syscall.EIO, true},
	} {
		d := deps{syncFile: func(*os.File) error { return tc.err }}
		if err := d.syncDir(dir); (err != nil) != tc.fail {
			t.Errorf("sync answering %v: SyncDir = %v", tc.err, err)
		}
		if err := d.replaceFile(filepath.Join(dir, "f"), strings.NewReader("x"), 0o600); (err != nil) != tc.fail {
			t.Errorf("sync answering %v: ReplaceFile = %v", tc.err, err)
		}
	}
}
