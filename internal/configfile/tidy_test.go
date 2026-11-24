package configfile

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/enigma-brain/axon/internal/axondir"
)

// Recover deletes a record whose stored file is gone and the files left
// renamed aside, and leaves everything else alone.
func TestRecoverDeletesRecordsWithoutTheirFileAndFilesSetAside(t *testing.T) {
	s, home := newStore(t)
	add(t, s, oneDirect("a", "one"), "-")
	add(t, s, oneDirect("b", "two"), "https://example.org/b.toml")
	if err := os.Remove(s.Path("b")); err != nil {
		t.Fatal(err)
	}
	records := filepath.Join(home, ".axon", "state", "configs")
	configs := filepath.Dir(s.Path("a"))
	write := func(path string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gone := []string{
		recordOf(home, "b"),
		filepath.Join(configs, "c.toml"+removingExt),
		filepath.Join(records, "c.toml"+removingExt),
	}
	for _, p := range gone[1:] {
		write(p)
	}
	kept := []string{
		s.Path("a"),
		recordOf(home, "a"),
		filepath.Join(records, "notes.txt"),
		filepath.Join(records, "Not-An-Id.toml"),
		filepath.Join(configs, "notes.txt"+removingExt),
	}
	for _, p := range kept[2:] {
		write(p)
	}

	if restored, err := s.Recover(); err != nil || restored != nil {
		t.Fatalf("Recover = %v, %v", restored, err)
	}
	for _, p := range gone {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s is left: %v", p, err)
		}
	}
	for _, p := range kept {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s is gone: %v", p, err)
		}
	}
	files, problems, err := s.List()
	if err != nil || len(problems) != 0 || !reflect.DeepEqual(pathwayIDs(files), []string{"a/one"}) {
		t.Errorf("List = %v, %v, %v", pathwayIDs(files), problems, err)
	}
}

// A store with nothing to put in order is not written to, its lock
// included.
func TestRecoverWithNothingToDoWritesNothing(t *testing.T) {
	s, home := newStore(t)
	add(t, s, oneDirect("a", "one"), "-")
	before := snapshot(t, filepath.Join(home, ".axon"))
	locked := false
	realLock := lockStore
	lockStore = func(d axondir.Dir) (func(), error) { locked = true; return realLock(d) }
	t.Cleanup(func() { lockStore = realLock })
	if restored, err := s.Recover(); err != nil || restored != nil {
		t.Fatalf("Recover = %v, %v", restored, err)
	}
	if locked {
		t.Error("Recover took the lock")
	}
	if after := snapshot(t, filepath.Join(home, ".axon")); !reflect.DeepEqual(before, after) {
		t.Errorf("Recover changed the store:\nbefore %v\nafter  %v", before, after)
	}
}

// A put's own recovery, which runs under the lock the put holds, leaves the
// file the put has renamed aside: it is still there when the new file is
// written, and the put deletes it once it has succeeded.
func TestAPutKeepsTheFileItSetAside(t *testing.T) {
	s, home := newStore(t)
	added, old := clashing(t, s)
	aside := s.Path("a") + removingExt
	seen := false
	replaceFile = func(p string, data []byte) error {
		if p == s.Path("b") {
			// The put has renamed "a" aside and recovered first; run
			// the recovery a put runs once more, under the same lock.
			if _, err := s.recoverPrevious(); err != nil {
				t.Error(err)
			}
			_, err := os.Lstat(aside)
			seen = err == nil
		}
		return axondir.ReplaceFile(p, data)
	}
	t.Cleanup(func() { replaceFile = axondir.ReplaceFile })
	if _, err := s.Put(added, PutOptions{Origin: FromSource(mustSource(t, "-")), Remove: []File{old}}); err != nil {
		t.Fatal(err)
	}
	if !seen {
		t.Error("the file set aside was gone before the new file was written")
	}
	for _, p := range []string{aside, recordOf(home, "a") + removingExt} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s is left after the put: %v", p, err)
		}
	}
}
