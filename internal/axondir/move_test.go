package axondir

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// oldLayout is what axon v0.3.0 kept, by path relative to ~/.axon, with a
// previous copy and a file being removed that a stopped change left.
var oldLayout = map[string]string{
	"orgs/a.toml":          "id = \"a\"\n",
	"orgs/a.toml.previous": "id = \"a\" # previous\n",
	"orgs/b.toml.removing": "id = \"b\"\n",
	"state/orgs/a.toml":    "sha256 = \"x\"\n",
	"state/orgs.lock":      "",
	"state/email":          "user@example.org\n",
	"tools/marker":         "tool\n",
	"config.toml":          "pathway = \"campus\"\n",
}

// newLayout is oldLayout under the new names. The old lock file is gone.
var newLayout = map[string]string{
	"configs/a.toml":          "id = \"a\"\n",
	"configs/a.toml.previous": "id = \"a\" # previous\n",
	"configs/b.toml.removing": "id = \"b\"\n",
	"state/configs/a.toml":    "sha256 = \"x\"\n",
	"state/email":             "user@example.org\n",
	"tools/marker":            "tool\n",
	"settings.toml":           "pathway = \"campus\"\n",
}

// writeTree writes files, by path relative to root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// readTree returns every file under root by its path relative to root,
// leaving out the lock file, which taking the lock creates.
func readTree(t *testing.T, root string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if rel == filepath.Join("state", lockFile) {
			return nil
		}
		data, err := os.ReadFile(path)
		got[filepath.ToSlash(rel)] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func movedNames(m Moves) []string {
	var names []string
	for _, r := range m.Moved {
		names = append(names, r.OldName+" -> "+r.NewName)
	}
	return names
}

var allMoved = []string{
	"orgs/ -> configs/",
	"state/orgs/ -> state/configs/",
	"config.toml -> settings.toml",
}

func TestMoveOldNamesWithNoAxonDirectoryCreatesNothing(t *testing.T) {
	d, _ := newDir(t)
	m, err := d.MoveOldNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Moved) != 0 || len(m.Both) != 0 {
		t.Errorf("moves = %+v, want none", m)
	}
	if _, err := os.Stat(d.Root()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("MoveOldNames created %s: %v", d.Root(), err)
	}
}

func TestMoveOldNamesOnTheNewLayoutWritesNothing(t *testing.T) {
	d, _ := newDir(t)
	writeTree(t, d.Root(), newLayout)
	m, err := d.MoveOldNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Moved) != 0 || len(m.Both) != 0 {
		t.Errorf("moves = %+v, want none", m)
	}
	if got := readTree(t, d.Root()); !reflect.DeepEqual(got, newLayout) {
		t.Errorf("files = %v, want %v", got, newLayout)
	}
	if _, err := os.Stat(d.LockPath()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the lock was taken: %v", err)
	}
}

func TestMoveOldNamesMovesTheOldLayout(t *testing.T) {
	d, _ := newDir(t)
	writeTree(t, d.Root(), oldLayout)
	m, err := d.MoveOldNames()
	if err != nil {
		t.Fatal(err)
	}
	if got := movedNames(m); !reflect.DeepEqual(got, allMoved) {
		t.Errorf("moved = %q, want %q", got, allMoved)
	}
	if len(m.Both) != 0 {
		t.Errorf("both = %+v, want none", m.Both)
	}
	if got := readTree(t, d.Root()); !reflect.DeepEqual(got, newLayout) {
		t.Errorf("files = %v, want %v", got, newLayout)
	}
	if id, ok, err := d.Pathway(); err != nil || !ok || id != "campus" {
		t.Errorf("Pathway() = %q, %v, %v; want campus", id, ok, err)
	}
	for _, r := range m.Moved {
		if !strings.HasPrefix(r.Old, d.Root()) || !strings.HasPrefix(r.New, d.Root()) {
			t.Errorf("rename %+v is not under %s", r, d.Root())
		}
	}

	// A second run finds nothing to do.
	m, err = d.MoveOldNames()
	if err != nil || len(m.Moved) != 0 || len(m.Both) != 0 {
		t.Errorf("second run: %+v, %v; want nothing", m, err)
	}
}

// A move stopped after any of its renames is finished by the next run.
func TestMoveOldNamesFinishesAnInterruptedMove(t *testing.T) {
	type part struct{ old, new string }
	parts := []part{{"orgs", "configs"}, {"state/orgs", "state/configs"}, {"config.toml", "settings.toml"}}
	for done := 0; done < 1<<len(parts); done++ {
		var name []string
		for i, p := range parts {
			if done&(1<<i) != 0 {
				name = append(name, p.new)
			}
		}
		t.Run("done "+strings.Join(name, ","), func(t *testing.T) {
			d, _ := newDir(t)
			writeTree(t, d.Root(), oldLayout)
			var want []string
			for i, p := range parts {
				if done&(1<<i) != 0 {
					if err := os.Rename(filepath.Join(d.Root(), p.old), filepath.Join(d.Root(), p.new)); err != nil {
						t.Fatal(err)
					}
				} else {
					want = append(want, allMoved[i])
				}
			}
			m, err := d.MoveOldNames()
			if err != nil {
				t.Fatal(err)
			}
			if got := movedNames(m); !reflect.DeepEqual(got, want) {
				t.Errorf("moved = %q, want %q", got, want)
			}
			wantFiles := newLayout
			if len(want) == 0 {
				// With nothing to move, the old lock file is left alone.
				wantFiles = map[string]string{"state/orgs.lock": ""}
				for k, v := range newLayout {
					wantFiles[k] = v
				}
			}
			if got := readTree(t, d.Root()); !reflect.DeepEqual(got, wantFiles) {
				t.Errorf("files = %v, want %v", got, wantFiles)
			}
		})
	}
}

// When both names exist, the new one is kept as it is, the old one is left
// where it is, and the rest still moves.
func TestMoveOldNamesLeavesBothNames(t *testing.T) {
	d, _ := newDir(t)
	writeTree(t, d.Root(), oldLayout)
	writeTree(t, d.Root(), map[string]string{"configs/new.toml": "id = \"new\"\n"})
	m, err := d.MoveOldNames()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := movedNames(m), allMoved[1:]; !reflect.DeepEqual(got, want) {
		t.Errorf("moved = %q, want %q", got, want)
	}
	if len(m.Both) != 1 || m.Both[0].Old != filepath.Join(d.Root(), "orgs") || m.Both[0].New != filepath.Join(d.Root(), "configs") {
		t.Fatalf("both = %+v, want orgs and configs", m.Both)
	}
	got := readTree(t, d.Root())
	for _, rel := range []string{"orgs/a.toml", "orgs/a.toml.previous", "orgs/b.toml.removing", "configs/new.toml", "settings.toml", "state/configs/a.toml"} {
		if _, ok := got[rel]; !ok {
			t.Errorf("%s is missing; files = %v", rel, got)
		}
	}
	if _, ok := got["configs/a.toml"]; ok {
		t.Errorf("the old directory was merged into the new one")
	}

	// Every later run reports both again and moves nothing.
	m, err = d.MoveOldNames()
	if err != nil || len(m.Moved) != 0 || len(m.Both) != 1 {
		t.Errorf("second run: %+v, %v; want one pair and no move", m, err)
	}
}

// An old layout without a state directory gets one, for the lock.
func TestMoveOldNamesCreatesTheStateDirectoryForTheLock(t *testing.T) {
	d, _ := newDir(t)
	writeTree(t, d.Root(), map[string]string{"orgs/a.toml": "x", "config.toml": "pathway = \"a\"\n"})
	m, err := d.MoveOldNames()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := movedNames(m), []string{allMoved[0], allMoved[2]}; !reflect.DeepEqual(got, want) {
		t.Errorf("moved = %q, want %q", got, want)
	}
	if _, err := os.Stat(d.LockPath()); err != nil {
		t.Errorf("lock file: %v", err)
	}
	assertMode(t, d.StatePath(), 0o700)
}

// Two processes started together move each name once and lose nothing.
func TestMoveOldNamesTogether(t *testing.T) {
	d, _ := newDir(t)
	writeTree(t, d.Root(), oldLayout)
	const n = 8
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		moved []string
		errs  []error
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := New(filepath.Dir(d.Root())).MoveOldNames()
			mu.Lock()
			defer mu.Unlock()
			moved = append(moved, movedNames(m)...)
			if err != nil {
				errs = append(errs, err)
			}
		}()
	}
	wg.Wait()
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if len(moved) != len(allMoved) {
		t.Errorf("moved = %q, want each of %q once", moved, allMoved)
	}
	if got := readTree(t, d.Root()); !reflect.DeepEqual(got, newLayout) {
		t.Errorf("files = %v, want %v", got, newLayout)
	}
}

// A move that cannot be made stops with the path and the reason, and
// leaves the name it could not move under its old name.
func TestMoveOldNamesThatCannotBeMade(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes to a read-only directory")
	}
	t.Run("a read-only axon directory", func(t *testing.T) {
		d, _ := newDir(t)
		writeTree(t, d.Root(), oldLayout)
		readOnly(t, d.Root())
		m, err := d.MoveOldNames()
		want := "rename " + filepath.Join(d.Root(), "orgs") + " to " + filepath.Join(d.Root(), "configs") + ": permission denied"
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
		if len(m.Moved) != 0 {
			t.Errorf("moved = %q, want none", movedNames(m))
		}
		if _, err := os.Stat(filepath.Join(d.Root(), "orgs", "a.toml")); err != nil {
			t.Errorf("the old config files directory: %v", err)
		}
	})
	t.Run("no state directory and a read-only axon directory", func(t *testing.T) {
		d, _ := newDir(t)
		writeTree(t, d.Root(), map[string]string{"orgs/a.toml": "x"})
		readOnly(t, d.Root())
		_, err := d.MoveOldNames()
		want := "create " + d.StatePath() + ": permission denied"
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
		if _, err := os.Stat(filepath.Join(d.Root(), "orgs", "a.toml")); err != nil {
			t.Errorf("the old config files directory: %v", err)
		}
	})
}

// readOnly makes dir read-only until the test ends.
func readOnly(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
}
