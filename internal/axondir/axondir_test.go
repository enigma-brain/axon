package axondir

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func newDir(t *testing.T) (Dir, string) {
	t.Helper()
	home := t.TempDir()
	return New(home), home
}

func assertMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s has mode %o, want %o", path, got, want)
	}
}

func TestDefaultUsesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	d, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".axon"); d.Root() != want {
		t.Errorf("Root() = %q, want %q", d.Root(), want)
	}
}

func TestNewCreatesNothing(t *testing.T) {
	d, _ := newDir(t)
	if _, err := os.Stat(d.Root()); !os.IsNotExist(err) {
		t.Fatalf("New created %s", d.Root())
	}
}

func TestDirectoriesCreatedOnFirstUse(t *testing.T) {
	d, home := newDir(t)
	for name, get := range map[string]func() (string, error){
		"orgs":  d.OrgsDir,
		"tools": d.ToolsDir,
		"state": d.StateDir,
	} {
		path, err := get()
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(home, ".axon", name); path != want {
			t.Errorf("%s dir = %q, want %q", name, path, want)
		}
		assertMode(t, path, 0o700)
		// A second call finds the directory in place.
		if again, err := get(); err != nil || again != path {
			t.Errorf("second call = %q, %v", again, err)
		}
	}
	assertMode(t, d.Root(), 0o700)
}

func TestDirectoryThatIsAFile(t *testing.T) {
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
	assertMode(t, d.ConfigPath(), 0o600)
	assertMode(t, d.Root(), 0o700)

	data, err := os.ReadFile(d.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "pathway = \"remote\"\n"; got != want {
		t.Errorf("config.toml = %q, want %q", got, want)
	}
}

func TestSetPathwayRejectsEmpty(t *testing.T) {
	d, _ := newDir(t)
	for _, id := range []string{"", "a\nb"} {
		if err := d.SetPathway(id); err == nil {
			t.Errorf("SetPathway(%q) succeeded", id)
		}
	}
}

func TestPathwayRejectsBadConfig(t *testing.T) {
	d, _ := newDir(t)
	if err := os.Mkdir(d.Root(), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"pathway = \n", "pathway = 3\n"} {
		if err := os.WriteFile(d.ConfigPath(), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := d.Pathway(); err == nil {
			t.Errorf("Pathway() with config %q succeeded", content)
		}
	}
}

func TestRemoveEmail(t *testing.T) {
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
	assertMode(t, path, 0o600)
	assertMode(t, filepath.Dir(path), 0o700)
}

func TestSetEmailRejectsBlank(t *testing.T) {
	d, _ := newDir(t)
	for _, e := range []string{"", "  ", "a@b\nc@d"} {
		if err := d.SetEmail(e); err == nil {
			t.Errorf("SetEmail(%q) succeeded", e)
		}
	}
}

func TestReplaceFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	for _, content := range []string{"first\n", "second\n"} {
		if err := ReplaceFile(path, []byte(content)); err != nil {
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
	assertMode(t, path, 0o600)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only the file", len(entries))
	}
}

// A directory sync the filesystem does not support counts as done, and any
// other failure is reported.
func TestSyncDirOnAFilesystemWithoutDirectorySync(t *testing.T) {
	real := syncFile
	t.Cleanup(func() { syncFile = real })
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
		syncFile = func(*os.File) error { return tc.err }
		if err := SyncDir(dir); (err != nil) != tc.fail {
			t.Errorf("sync answering %v: SyncDir = %v", tc.err, err)
		}
		if err := ReplaceFile(filepath.Join(dir, "f"), []byte("x")); (err != nil) != tc.fail {
			t.Errorf("sync answering %v: ReplaceFile = %v", tc.err, err)
		}
	}
}
