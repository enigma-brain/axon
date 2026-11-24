package orgfile

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/enigma-brain/axon/internal/axondir"
)

// updated returns a store holding the file "a" at revision 1, and the
// bytes of revision 2 of it.
func updated(t *testing.T) (Store, string, []byte) {
	t.Helper()
	s, home := newStore(t)
	revision := func(n string) string {
		return strings.Replace(directOrg("a", "one", "Campus", "https://a.example.org"), "format = 2\n", "format = 2\nrevision = "+n+"\n", 1)
	}
	add(t, s, revision("1"), "-")
	newer := revision("2")
	return s, home, []byte(newer)
}

func recordOf(home, id string) string {
	return filepath.Join(home, ".axon", "state", "orgs", id+".toml")
}

func noPrevious(t *testing.T, s Store) {
	t.Helper()
	if _, err := os.Stat(s.Path("a") + previousExt); !os.IsNotExist(err) {
		t.Errorf("the previous copy was left: %v", err)
	}
}

func TestAnUpdateLeavesNoPreviousCopy(t *testing.T) {
	s, _, newer := updated(t)
	if _, err := s.Add(newer, mustSource(t, "-")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(s.Path("a")); string(got) != string(newer) {
		t.Errorf("stored file = %q, want the update", got)
	}
	noPrevious(t, s)
}

// An update that fails leaves the stored file and its record as they were,
// whether the previous copy is a hard link or, where linking fails, a copy.
func TestAFailedUpdatePutsThePreviousFileBack(t *testing.T) {
	for _, link := range []bool{true, false} {
		for _, step := range []string{"writing the new file", "writing the new record"} {
			name := step
			if !link {
				name += ", by a copy"
			}
			t.Run(name, func(t *testing.T) {
				s, home, newer := updated(t)
				if !link {
					linkFile = func(string, string) error { return errors.New("no links here") }
					t.Cleanup(func() { linkFile = os.Link })
				}
				var keptWhileWriting string
				failed := false
				target := s.Path("a")
				if step == "writing the new record" {
					target = recordOf(home, "a")
				}
				before := snapshot(t, filepath.Join(home, ".axon"))
				replaceFile = func(p string, data []byte) error {
					if p == target && !failed {
						failed = true
						kept, _ := os.ReadFile(s.Path("a") + previousExt)
						keptWhileWriting = string(kept)
						return errors.New("injected failure")
					}
					return axondir.ReplaceFile(p, data)
				}
				t.Cleanup(func() { replaceFile = axondir.ReplaceFile })

				_, err := s.Add(newer, mustSource(t, "-"))
				if err == nil || !strings.Contains(err.Error(), "injected failure") {
					t.Fatalf("Add = %v, want the injected failure", err)
				}
				if keptWhileWriting != before[s.Path("a")] {
					t.Errorf("previous copy during the update = %q, want the stored file", keptWhileWriting)
				}
				if after := snapshot(t, filepath.Join(home, ".axon")); !reflect.DeepEqual(before, after) {
					t.Errorf("a failed update left changes:\nbefore %v\nafter  %v", before, after)
				}
			})
		}
	}
}

// When the record cannot be written and the previous file cannot be put
// back either, the previous file stays on disk beside the stored one, and
// the next recovery puts it back.
func TestAFailedPutBackLeavesThePreviousFileOnDisk(t *testing.T) {
	s, home, newer := updated(t)
	original, _ := os.ReadFile(s.Path("a"))
	failReplaceOn(t, recordOf(home, "a"))
	failOn(t, &renameFile, s.Path("a")+previousExt)
	_, err := s.Add(newer, mustSource(t, "-"))
	if err == nil || !strings.Contains(err.Error(), s.Path("a")+" could not be put back from "+s.Path("a")+previousExt) {
		t.Fatalf("Add = %v, want the file named", err)
	}
	if data, _ := os.ReadFile(s.Path("a") + previousExt); string(data) != string(original) {
		t.Errorf("previous copy = %q, want the stored file", data)
	}

	renameFile = os.Rename
	replaceFile = axondir.ReplaceFile
	restored, err := s.Recover()
	if err != nil || !reflect.DeepEqual(restored, []string{s.Path("a")}) {
		t.Fatalf("Recover = %v, %v, want the file put back", restored, err)
	}
	if data, _ := os.ReadFile(s.Path("a")); string(data) != string(original) {
		t.Errorf("stored file = %q, want the previous file", data)
	}
	noPrevious(t, s)
}

// leftover makes the stored file "a" what stored says, its previous copy
// what previous says, and its record's checksum that of record, or no
// checksum when record is nil.
func leftover(t *testing.T, s Store, home string, stored, previous, record []byte) {
	t.Helper()
	if stored == nil {
		if err := os.Remove(s.Path("a")); err != nil {
			t.Fatal(err)
		}
	} else if err := os.WriteFile(s.Path("a"), stored, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path("a")+previousExt, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	rec := "source = \"-\"\n"
	if record != nil {
		rec += "sha256 = \"" + fingerprint(record) + "\"\n"
	}
	if err := os.WriteFile(recordOf(home, "a"), []byte(rec), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverDecidesByTheRecord(t *testing.T) {
	for _, tc := range []struct {
		name string
		// stored, previous and record name the bytes of the stored
		// file, its previous copy and the record's checksum: "old",
		// "new", "other", or "" for none.
		stored, previous, record string
		// want is what is stored after recovery; back reports that it
		// was put back.
		want string
		back bool
	}{
		{"the record matches the stored file", "new", "old", "new", "new", false},
		{"the update never started", "old", "old", "old", "old", false},
		{"the new record was not written", "new", "old", "old", "old", true},
		{"the stored file is missing", "", "old", "old", "old", true},
		{"the stored file is missing and the record matches neither", "", "old", "other", "old", true},
		{"the record matches neither", "new", "old", "other", "new", false},
		{"the record has no checksum", "new", "old", "", "new", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, home, newer := updated(t)
			old, _ := os.ReadFile(s.Path("a"))
			content := map[string][]byte{"old": old, "new": newer, "other": []byte("other"), "": nil}
			leftover(t, s, home, content[tc.stored], content[tc.previous], content[tc.record])

			restored, err := s.Recover()
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			if tc.back {
				want = []string{s.Path("a")}
			}
			if !reflect.DeepEqual(restored, want) {
				t.Errorf("Recover = %v, want %v", restored, want)
			}
			if got, _ := os.ReadFile(s.Path("a")); string(got) != string(content[tc.want]) {
				t.Errorf("stored file = %q, want %q", got, content[tc.want])
			}
			noPrevious(t, s)
		})
	}
}

func TestRecoverWithNothingStoredCreatesNothing(t *testing.T) {
	s, home := newStore(t)
	if restored, err := s.Recover(); err != nil || restored != nil {
		t.Fatalf("Recover = %v, %v", restored, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".axon")); !os.IsNotExist(err) {
		t.Errorf("Recover created %s", filepath.Join(home, ".axon"))
	}
}

// A put recovers a leftover before it compares the file with the stored
// one, so it updates the copy the record names.
func TestAPutRecoversALeftoverFirst(t *testing.T) {
	s, home, newer := updated(t)
	old, _ := os.ReadFile(s.Path("a"))
	leftover(t, s, home, nil, old, old)
	if _, err := s.Add(newer, mustSource(t, "-")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(s.Path("a")); string(got) != string(newer) {
		t.Errorf("stored file = %q, want the update", got)
	}
	noPrevious(t, s)
}

// The previous copy is never read as a stored file.
func TestListIgnoresAPreviousCopy(t *testing.T) {
	s, home, _ := updated(t)
	old, _ := os.ReadFile(s.Path("a"))
	leftover(t, s, home, old, old, old)
	files, problems, err := s.List()
	if err != nil || len(problems) != 0 || len(files) != 1 {
		t.Fatalf("List = %v, %v, %v", pathwayIDs(files), problems, err)
	}
}

// A record that cannot be read stops recovery, and the copy is kept for a
// later one.
func TestRecoverKeepsTheCopyWhenTheRecordCannotBeRead(t *testing.T) {
	s, home, newer := updated(t)
	old, _ := os.ReadFile(s.Path("a"))
	leftover(t, s, home, newer, old, old)
	if err := os.Remove(recordOf(home, "a")); err != nil {
		t.Fatal(err)
	}
	// A directory in place of the record makes reading it fail.
	if err := os.Mkdir(recordOf(home, "a"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Recover(); err == nil {
		t.Fatal("Recover succeeded with an unreadable record")
	}
	if data, _ := os.ReadFile(s.Path("a") + previousExt); string(data) != string(old) {
		t.Errorf("previous copy = %q, want it kept", data)
	}
}

// An update whose copy cannot be synced to disk stops before the stored
// file is replaced.
func TestAnUpdateStopsWhenTheCopyCannotBeSynced(t *testing.T) {
	s, home, newer := updated(t)
	before := snapshot(t, filepath.Join(home, ".axon"))
	real := syncDir
	syncDir = func(string) error { return errors.New("injected failure") }
	t.Cleanup(func() { syncDir = real })
	if _, err := s.Add(newer, mustSource(t, "-")); err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("Add = %v, want the injected failure", err)
	}
	if after := snapshot(t, filepath.Join(home, ".axon")); !reflect.DeepEqual(before, after) {
		t.Errorf("a stopped update left changes:\nbefore %v\nafter  %v", before, after)
	}
}

// A copy that cannot be deleted stops recovery, and a put with it, so a
// later removal of the stored file cannot be undone by the copy.
func TestRecoverStopsWhenTheCopyCannotBeDeleted(t *testing.T) {
	s, home, newer := updated(t)
	old, _ := os.ReadFile(s.Path("a"))
	leftover(t, s, home, newer, old, newer)
	removeFile = func(p string) error {
		if p == s.Path("a")+previousExt {
			return errors.New("injected failure")
		}
		return os.Remove(p)
	}
	t.Cleanup(func() { removeFile = os.Remove })
	if _, err := s.Recover(); err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("Recover = %v, want the injected failure", err)
	}
	if _, err := s.Add([]byte(directOrg("b", "two", "B", "https://b.example.org")), mustSource(t, "-")); err == nil {
		t.Fatal("Add succeeded with a copy that cannot be deleted")
	}
}
