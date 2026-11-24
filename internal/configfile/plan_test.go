package configfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// plan parses data and returns what storing it as opts say would do, as put
// decides it once it holds the lock. When id is not empty, the stored file
// id is being refreshed.
func plan(t *testing.T, s Store, data string, opts PutOptions, id string) (putPlan, error) {
	t.Helper()
	f, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return s.planPut(f, []byte(data), opts, id)
}

func fromSource(t *testing.T, src string) PutOptions {
	t.Helper()
	return PutOptions{Origin: FromSource(mustSource(t, src))}
}

// sameNames returns the SameName of each clash in c.
func sameNames(c Change) []bool {
	var got []bool
	for _, cl := range c.Clashes {
		got = append(got, cl.SameName)
	}
	return got
}

func TestPlanANewFile(t *testing.T) {
	s, _ := newStore(t)
	data := directConfig("a", "one", "A", "https://a.example.org")
	p, err := plan(t, s, data, fromSource(t, "-"), "")
	if err != nil {
		t.Fatal(err)
	}
	if !p.write || p.replace || p.remove != nil || p.change.Previous != nil || p.change.Clashes != nil {
		t.Errorf("plan = %+v, want a write of a new file", p)
	}
	if p.path != s.Path("a") || p.file.ID != "a" || string(p.data) != data || p.origin.Source.raw() != "-" {
		t.Errorf("plan = %+v, want file a from - at %s", p, s.Path("a"))
	}
}

func TestPlanAnUpdate(t *testing.T) {
	s, _ := newStore(t)
	add(t, s, directConfig("a", "one", "A", "https://a.example.org"), "-")
	p, err := plan(t, s, directConfig("a", "one", "A", "https://b.example.org"), fromSource(t, "-"), "")
	if err != nil {
		t.Fatal(err)
	}
	if !p.write || !p.replace || p.remove != nil || p.change.Clashes != nil {
		t.Errorf("plan = %+v, want a write that replaces the stored file", p)
	}
	if p.change.Previous == nil || p.change.Previous.Pathways[0].Sky.Endpoint != "https://a.example.org" {
		t.Errorf("Previous = %+v, want the stored file", p.change.Previous)
	}
}

func TestPlanAClashByName(t *testing.T) {
	for _, tc := range []struct{ name, added string }{
		{"same name", directConfig("b", "two", "campus ", "https://b.example.org")},
		{"same identifier", directConfig("b", "one", "B", "https://b.example.org")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newStore(t)
			stored := add(t, s, directConfig("a", "one", "Campus", "https://a.example.org"), "-")
			var clash *ClashError
			if _, err := plan(t, s, tc.added, fromSource(t, "-"), ""); !errors.As(err, &clash) || clash.Path != s.Path("a") {
				t.Fatalf("plan = %v, want a clash with %s", err, s.Path("a"))
			}
			opts := fromSource(t, "-")
			opts.Remove = []File{stored}
			p, err := plan(t, s, tc.added, opts, "")
			if err != nil {
				t.Fatal(err)
			}
			if !p.write || p.replace || !reflect.DeepEqual(p.remove, []string{"a"}) || !reflect.DeepEqual(sameNames(p.change), []bool{true}) {
				t.Errorf("plan = %+v, want b written and a removed", p)
			}
		})
	}
}

func TestPlanAClashByFileID(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Put([]byte(directConfig("lab", "mine", "Mine", "https://m.example.org")), PutOptions{Origin: HandMade}); err != nil {
		t.Fatal(err)
	}
	added := directConfig("lab", "theirs", "Theirs", "https://t.example.org")
	var clash *ClashError
	if _, err := plan(t, s, added, fromSource(t, "-"), ""); !errors.As(err, &clash) || !clash.Clash.SameFileID {
		t.Fatalf("plan = %v, want a clash by file id", err)
	}
	stored, _, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	opts := fromSource(t, "-")
	opts.Remove = stored
	p, err := plan(t, s, added, opts, "")
	if err != nil {
		t.Fatal(err)
	}
	// The removed file has the file's own id, so it is replaced in place
	// rather than renamed aside.
	if !p.write || !p.replace || p.remove != nil || p.change.Previous != nil {
		t.Errorf("plan = %+v, want lab replaced and nothing renamed aside", p)
	}
}

func TestPlanAClashByServer(t *testing.T) {
	s, _ := newStore(t)
	add(t, s, directConfig("a", "one", "A", "https://sky.example.org"), "-")
	p, err := plan(t, s, directConfig("b", "two", "B", "https://Sky.example.org/"), fromSource(t, "-"), "")
	if err != nil {
		t.Fatal(err)
	}
	if !p.write || p.replace || p.remove != nil || !reflect.DeepEqual(sameNames(p.change), []bool{false}) {
		t.Errorf("plan = %+v, want b written beside a, with the server clash", p)
	}
}

func TestPlanALowerRevision(t *testing.T) {
	s, _ := newStore(t)
	add(t, s, withRevision(campusFile, "7"), "-")
	var lower *LowerRevisionError
	if _, err := plan(t, s, withRevision(campusFile, "5"), fromSource(t, "-"), ""); !errors.As(err, &lower) || lower.Stored != 7 || lower.New != 5 {
		t.Fatalf("plan = %v, want a lower revision", err)
	}
	opts := fromSource(t, "-")
	opts.AllowLower = true
	p, err := plan(t, s, withRevision(campusFile, "5"), opts, "")
	if err != nil {
		t.Fatal(err)
	}
	if !p.write || !p.replace || !p.change.Lower() {
		t.Errorf("plan = %+v, want the lower revision written", p)
	}
}

func TestPlanANoOp(t *testing.T) {
	s, _ := newStore(t)
	data := oneDirect("example", "a")
	add(t, s, data, refreshAddress)
	p, err := plan(t, s, data, fromSource(t, refreshAddress), "example")
	if err != nil {
		t.Fatal(err)
	}
	if p.write {
		t.Errorf("plan = %+v, want nothing written", p)
	}
	// The same bytes from another source are written, to record the source.
	p, err = plan(t, s, data, fromSource(t, "-"), "example")
	if err != nil {
		t.Fatal(err)
	}
	if !p.write || !p.replace {
		t.Errorf("plan = %+v, want a write that records the new source", p)
	}
}

// tree returns every file and directory under root, with each file's
// content.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	entries := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			entries[path+"/"] = ""
			return nil
		}
		data, err := os.ReadFile(path)
		entries[path] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// A refused put leaves the home as it was: no file changed, and no
// directory created, even the config files directory when it is missing.
func TestARefusedPutLeavesTheHomeAsItWas(t *testing.T) {
	for _, tc := range []struct {
		name string
		// noConfigs removes the config files directory and keeps the
		// records.
		noConfigs bool
		put       func(t *testing.T, s Store) error
		// refused reports whether err is the refusal the case names.
		refused func(err error) bool
	}{
		{"a refresh of a file no longer stored", true, func(t *testing.T, s Store) error {
			_, _, err := s.Refresh("example", []byte(oneDirect("example", "a")), mustSource(t, refreshAddress), false)
			return err
		}, func(err error) bool { return err != nil && strings.Contains(err.Error(), "it is no longer stored") }},
		{"a clash by name", false, func(t *testing.T, s Store) error {
			_, err := s.Put([]byte(directConfig("b", "two", "Other", "https://b.example.org")), fromSource(t, "-"))
			return err
		}, func(err error) bool { return errors.As(err, new(*ClashError)) }},
		{"a lower revision", false, func(t *testing.T, s Store) error {
			_, err := s.Put([]byte(withRevision(oneDirect("other", "c"), "1")), fromSource(t, "-"))
			return err
		}, func(err error) bool { return errors.As(err, new(*LowerRevisionError)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, home := newStore(t)
			add(t, s, oneDirect("example", "a"), refreshAddress)
			add(t, s, withRevision(directConfig("other", "c", "Other", "https://c.example.org"), "2"), "-")
			if tc.noConfigs {
				if err := os.RemoveAll(s.dir.ConfigsPath()); err != nil {
					t.Fatal(err)
				}
			}
			before := tree(t, home)
			if err := tc.put(t, s); !tc.refused(err) {
				t.Fatalf("put = %v, want %s refused", err, tc.name)
			}
			if after := tree(t, home); !reflect.DeepEqual(after, before) {
				t.Errorf("a refused put changed the home:\nbefore %v\nafter  %v", before, after)
			}
		})
	}
}
