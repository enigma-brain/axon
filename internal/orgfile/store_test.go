package orgfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/enigma-brain/axon/internal/axondir"
)

func newStore(t *testing.T) (Store, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return NewStore(axondir.New(home)), home
}

// oneDirect returns a file with id fileID and one direct pathway per
// pathway id.
func oneDirect(fileID string, pathwayIDs ...string) string {
	s := "format = 2\nid = \"" + fileID + "\"\n"
	for _, id := range pathwayIDs {
		s += "\n[[pathway]]\nid = \"" + id + "\"\nkind = \"direct\"\nname = \"" + id + "\"\ndescription = \"D\"\n" +
			"[pathway.sky]\nendpoint = \"https://" + id + ".example.org\"\n"
	}
	return s
}

func add(t *testing.T, s Store, data, source string) File {
	t.Helper()
	f, err := s.Add([]byte(data), mustSource(t, source))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	return f
}

// snapshot returns every file under root with its content.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		files[path] = string(data)
		return err
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return files
}

func pathwayIDs(files []File) []string {
	var ids []string
	for _, f := range files {
		for _, p := range f.Pathways {
			ids = append(ids, f.ID+"/"+p.ID)
		}
	}
	return ids
}

func TestAddStoresBytesAndSource(t *testing.T) {
	s, home := newStore(t)
	// A comment and odd spacing show the file is stored as given, not
	// re-encoded.
	data := "# kept as written\n" + campusFile + "\n\n"
	f := add(t, s, data, "https://example.org/campus.toml")
	if f.ID != "example-campus" {
		t.Fatalf("Add returned %q", f.ID)
	}

	path := filepath.Join(home, ".axon", "orgs", "example-campus.toml")
	if s.Path("example-campus") != path {
		t.Errorf("Path = %q, want %q", s.Path("example-campus"), path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != data {
		t.Errorf("stored file = %q, want %q", got, data)
	}
	assertMode(t, path, 0o600)
	assertMode(t, filepath.Dir(path), 0o700)

	src, ok, err := s.Source("example-campus")
	if err != nil || !ok {
		t.Fatalf("Source = %v, %v", ok, err)
	}
	if src.String() != "https://example.org/campus.toml" || !src.IsAddress() {
		t.Errorf("recorded source = %q (address %v)", src, src.IsAddress())
	}
	record := filepath.Join(home, ".axon", "state", "orgs", "example-campus.toml")
	assertMode(t, record, 0o600)
	assertMode(t, filepath.Dir(record), 0o700)

	if _, ok, err := s.Source("example-remote"); ok || err != nil {
		t.Errorf("Source of a file never added = %v, %v", ok, err)
	}
}

func TestRecordKeepsTheWholeAddress(t *testing.T) {
	s, home := newStore(t)
	address := "https://example.org/campus.toml?" + secret
	add(t, s, campusFile, address)
	record, err := os.ReadFile(filepath.Join(home, ".axon", "state", "orgs", "example-campus.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(record), address) {
		t.Errorf("record = %q, want it to hold %s", record, address)
	}
	src, ok, err := s.Source("example-campus")
	if err != nil || !ok || src.raw() != address || src.String() != "https://example.org/campus.toml" {
		t.Errorf("Source = %q (raw %q), %v, %v", src, src.raw(), ok, err)
	}
}

func TestAddKeepsOtherFiles(t *testing.T) {
	s, home := newStore(t)
	add(t, s, campusFile, "-")
	add(t, s, remoteFile, filepath.Join(home, "remote.toml"))
	files, problems, err := s.List()
	if err != nil || len(problems) != 0 {
		t.Fatalf("List = %v, %v", problems, err)
	}
	if got, want := pathwayIDs(files), []string{"example-campus/campus", "example-remote/remote"}; !reflect.DeepEqual(got, want) {
		t.Errorf("pathways = %v, want %v", got, want)
	}
	for id, want := range map[string]string{"example-campus": "-", "example-remote": filepath.Join(home, "remote.toml")} {
		src, _, err := s.Source(id)
		if err != nil || src.String() != want {
			t.Errorf("Source(%s) = %q, %v, want %q", id, src, err, want)
		}
	}
}

func TestAddReplacesTheSameID(t *testing.T) {
	s, home := newStore(t)
	add(t, s, oneDirect("example", "campus", "lab"), "https://example.org/old.toml")
	// The new copy keeps one of its own identifiers and drops the other;
	// its own identifiers do not count against it.
	newer := oneDirect("example", "lab", "annex")
	add(t, s, newer, "-")

	got, err := os.ReadFile(filepath.Join(home, ".axon", "orgs", "example.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != newer {
		t.Errorf("stored file = %q, want the newer copy", got)
	}
	src, _, _ := s.Source("example")
	if src.String() != "-" {
		t.Errorf("source = %q, want -", src)
	}
	files, _, _ := s.List()
	if got, want := pathwayIDs(files), []string{"example/lab", "example/annex"}; !reflect.DeepEqual(got, want) {
		t.Errorf("pathways = %v, want %v", got, want)
	}
}

func TestAddRefusesTakenPathwayID(t *testing.T) {
	s, home := newStore(t)
	add(t, s, oneDirect("first", "campus"), "-")
	before := snapshot(t, filepath.Join(home, ".axon"))

	_, err := s.Add([]byte(oneDirect("second", "lab", "campus")), mustSource(t, "-"))
	if err == nil {
		t.Fatal("Add succeeded with a taken pathway id")
	}
	if !strings.Contains(err.Error(), `"campus"`) || !strings.Contains(err.Error(), s.Path("first")) {
		t.Errorf("error %q does not name the identifier and the stored file", err)
	}
	if after := snapshot(t, filepath.Join(home, ".axon")); !reflect.DeepEqual(after, before) {
		t.Errorf("a refused file changed the disk:\nbefore %v\nafter  %v", before, after)
	}
}

func TestAddRefusedChangesNothing(t *testing.T) {
	s, home := newStore(t)
	// With nothing stored, a refused file creates nothing.
	if _, err := s.Add([]byte("format = 2\n"), mustSource(t, "-")); err == nil {
		t.Fatal("Add succeeded with a bad file")
	}
	if _, err := os.Stat(filepath.Join(home, ".axon")); !os.IsNotExist(err) {
		t.Errorf("a refused file created %s", filepath.Join(home, ".axon"))
	}

	// A refused replacement leaves the stored file and its source.
	add(t, s, campusFile, "https://example.org/campus.toml")
	before := snapshot(t, filepath.Join(home, ".axon"))
	bad := strings.Replace(campusFile, "https://sky.example.org", "http://sky.example.org", 1)
	if _, err := s.Add([]byte(bad), mustSource(t, "-")); err == nil {
		t.Fatal("Add succeeded with a bad replacement")
	}
	if after := snapshot(t, filepath.Join(home, ".axon")); !reflect.DeepEqual(after, before) {
		t.Errorf("a refused file changed the disk:\nbefore %v\nafter  %v", before, after)
	}
}

func TestAddRestoresTheFileWhenTheSourceCannotBeRecorded(t *testing.T) {
	s, home := newStore(t)
	// A file where the source records' directory belongs makes recording
	// the source fail after the config file is written.
	state := filepath.Join(home, ".axon", "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "orgs"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add([]byte(campusFile), mustSource(t, "-")); err == nil {
		t.Fatal("Add succeeded although the source could not be recorded")
	}
	if _, err := os.Stat(s.Path("example-campus")); !os.IsNotExist(err) {
		t.Errorf("a new file stays stored without its source: %v", err)
	}

	// A replacement puts the previous file back.
	if err := os.Remove(filepath.Join(state, "orgs")); err != nil {
		t.Fatal(err)
	}
	add(t, s, campusFile, "-")
	// A directory in place of the source record makes its replacement
	// fail, whatever the process's permissions.
	record := filepath.Join(state, "orgs", "example-campus.toml")
	if err := os.Remove(record); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(record, 0o700); err != nil {
		t.Fatal(err)
	}
	newer := strings.Replace(campusFile, "Campus\"", "Main campus\"", 1)
	if _, err := s.Add([]byte(newer), mustSource(t, "https://example.org/campus.toml")); err == nil {
		t.Fatal("Add succeeded although the source could not be recorded")
	}
	got, err := os.ReadFile(s.Path("example-campus"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != campusFile {
		t.Errorf("stored file = %q, want the previous file", got)
	}
}

func TestAddRefusesToReplaceANonRegularFile(t *testing.T) {
	s, home := newStore(t)
	// A directory stands in for any entry that is not a regular file,
	// such as a named pipe, which reading could block on.
	if err := os.MkdirAll(filepath.Join(home, ".axon", "orgs", "example-campus.toml"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := s.Add([]byte(campusFile), mustSource(t, "-"))
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("Add = %v, want a refusal of the non-regular file", err)
	}
}

func TestAddIgnoresBadStoredFiles(t *testing.T) {
	s, home := newStore(t)
	orgs := filepath.Join(home, ".axon", "orgs")
	if err := os.MkdirAll(orgs, 0o700); err != nil {
		t.Fatal(err)
	}
	// A stored file that fails the checks does not hold its identifiers.
	broken := strings.Replace(oneDirect("broken", "campus"), "format = 2", "format = 9", 1)
	if err := os.WriteFile(filepath.Join(orgs, "broken.toml"), []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	add(t, s, oneDirect("example", "campus"), "-")
}

func TestListOrderAndProblems(t *testing.T) {
	s, home := newStore(t)
	if files, problems, err := s.List(); files != nil || problems != nil || err != nil {
		t.Fatalf("List with no directory = %v, %v, %v", files, problems, err)
	}
	orgs := filepath.Join(home, ".axon", "orgs")
	if err := os.MkdirAll(filepath.Join(orgs, "adir.toml"), 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(orgs, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// "a-b" sorts before "a" as a file name but after it as an id.
	write("a.toml", oneDirect("a", "one", "two"))
	write("a-b.toml", oneDirect("a-b", "three"))
	write("c.toml", oneDirect("c", "four", "two"))   // clashes with a
	write("d.toml", "format = 2\n")                  // fails the checks
	write("e.toml", oneDirect("other", "five"))      // id is not the name
	write(".a.toml.123", oneDirect("a", "six"))      // a temporary file
	write("README", "notes")                         // not a file name of an id
	write("Upper.toml", oneDirect("upper", "seven")) // not a valid id
	write("f.toml", oneDirect("f", "eight"))

	files, problems, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pathwayIDs(files), []string{"a/one", "a/two", "a-b/three", "f/eight"}; !reflect.DeepEqual(got, want) {
		t.Errorf("pathways = %v, want %v", got, want)
	}
	reported := map[string]string{}
	for _, p := range problems {
		reported[filepath.Base(p.Path)] = p.Error()
	}
	want := map[string]string{
		"adir.toml": "regular file",
		"c.toml":    filepath.Join(orgs, "a.toml"),
		"d.toml":    "id: must be",
		"e.toml":    "file name",
	}
	if len(reported) != len(want) {
		t.Errorf("problems = %v", reported)
	}
	for name, part := range want {
		if !strings.Contains(reported[name], part) {
			t.Errorf("problem for %s = %q, want it to mention %q", name, reported[name], part)
		}
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s has mode %o, want %o", path, got, want)
	}
}

// TestConcurrentAddsClaimingOnePathway adds, at the same moment, several
// files with different ids that all claim one pathway identifier. Adds are
// serialized, so exactly one is stored and every other is refused.
func TestConcurrentAddsClaimingOnePathway(t *testing.T) {
	s, _ := newStore(t)
	const n = 8
	errs := make(chan error, n)
	start := make(chan struct{})
	for i := range n {
		go func() {
			<-start
			_, err := s.Add([]byte(oneDirect(fmt.Sprintf("org-%d", i), "shared")), mustSource(t, "-"))
			errs <- err
		}()
	}
	close(start)
	stored := 0
	for range n {
		err := <-errs
		var clash *ClashError
		switch {
		case err == nil:
			stored++
		case errors.As(err, &clash):
		default:
			t.Errorf("Add: %v", err)
		}
	}
	if stored != 1 {
		t.Errorf("%d adds stored a file claiming the pathway, want 1", stored)
	}
	files, problems, err := s.List()
	if err != nil || len(problems) != 0 || len(files) != 1 {
		t.Errorf("List = %v, %v, %v; want one file", pathwayIDs(files), problems, err)
	}
}

func TestAddReleasesTheLock(t *testing.T) {
	s, home := newStore(t)
	add(t, s, oneDirect("org-a", "a"), "-")
	// Refused after the lock is taken: another file claims pathway a.
	_, err := s.Add([]byte(oneDirect("org-c", "a")), mustSource(t, "-"))
	var clash *ClashError
	if !errors.As(err, &clash) {
		t.Fatalf("Add of a file claiming a stored pathway: err = %v", err)
	}
	add(t, s, oneDirect("org-b", "b"), "-")
	if _, err := os.Stat(axondir.New(home).LockPath()); err != nil {
		t.Errorf("lock file: %v", err)
	}
}

const refreshAddress = "https://example.org/a.toml"

func TestRefreshReplacesTheFile(t *testing.T) {
	s, _ := newStore(t)
	add(t, s, oneDirect("org", "a"), refreshAddress)
	f, changed, err := s.Refresh("org", []byte(oneDirect("org", "a", "b")), mustSource(t, refreshAddress), false)
	if err != nil || !changed {
		t.Fatalf("Refresh = %v, %v", changed, err)
	}
	if got := pathwayIDs([]File{f}); !reflect.DeepEqual(got, []string{"org/a", "org/b"}) {
		t.Errorf("returned pathways %v", got)
	}
	data, err := os.ReadFile(s.Path("org"))
	if err != nil || string(data) != oneDirect("org", "a", "b") {
		t.Errorf("stored file = %q, %v", data, err)
	}
	src, ok, err := s.Source("org")
	if err != nil || !ok || src.String() != refreshAddress {
		t.Errorf("source = %q, %v, %v", src, ok, err)
	}
}

func TestRefreshOfTheSameBytesWritesNothing(t *testing.T) {
	s, home := newStore(t)
	add(t, s, oneDirect("org", "a"), refreshAddress)
	path := s.Path("org")
	old := time.Unix(1_000_000, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, filepath.Join(home, ".axon"))
	_, changed, err := s.Refresh("org", []byte(oneDirect("org", "a")), mustSource(t, refreshAddress), false)
	if err != nil || changed {
		t.Fatalf("Refresh = %v, %v, want no change", changed, err)
	}
	if info, err := os.Stat(path); err != nil || !info.ModTime().Equal(old) {
		t.Errorf("the stored file was written again: %v", err)
	}
	if after := snapshot(t, filepath.Join(home, ".axon")); !reflect.DeepEqual(before, after) {
		t.Errorf("~/.axon changed:\nbefore %v\nafter  %v", before, after)
	}
}

func TestRefreshRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, data, want string
	}{
		{"another id", oneDirect("elsewhere", "a"), `id: is "elsewhere", not "org", the id of the stored file`},
		{"a taken pathway", oneDirect("org", "a", "lab"), `already used by the stored file`},
		{"a failed check", "format = 3\n", "format"},
		{"an older format", "format = 1\n", "older format"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, home := newStore(t)
			add(t, s, oneDirect("org", "a"), refreshAddress)
			add(t, s, oneDirect("other", "lab"), "-")
			before := snapshot(t, filepath.Join(home, ".axon"))
			_, changed, err := s.Refresh("org", []byte(tc.data), mustSource(t, refreshAddress), false)
			if err == nil || changed || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Refresh = %v, %v, want a refusal containing %q", changed, err, tc.want)
			}
			if after := snapshot(t, filepath.Join(home, ".axon")); !reflect.DeepEqual(before, after) {
				t.Errorf("a refused refresh changed ~/.axon:\nbefore %v\nafter  %v", before, after)
			}
		})
	}
}

func TestRefreshOfADeletedFileStoresNothing(t *testing.T) {
	s, _ := newStore(t)
	add(t, s, oneDirect("org", "a"), refreshAddress)
	if err := os.Remove(s.Path("org")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Refresh("org", []byte(oneDirect("org", "a")), mustSource(t, refreshAddress), false); err == nil {
		t.Fatal("Refresh stored a file that was deleted")
	}
	if _, err := os.Stat(s.Path("org")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the file is back: %v", err)
	}
}

// A stored file of an older format is left out as older, and the other
// stored files are listed.
func TestListLeavesOutOlderFormat(t *testing.T) {
	s, home := newStore(t)
	add(t, s, oneDirect("example", "campus"), "-")
	older := strings.Replace(oneDirect("older", "lab"), "format = 2", "format = 1", 1)
	older = strings.Replace(older, "[pathway.sky]", "[pathway.devenv]", 1)
	if err := os.WriteFile(filepath.Join(home, ".axon", "orgs", "older.toml"), []byte(older), 0o600); err != nil {
		t.Fatal(err)
	}
	files, problems, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].ID != "example" {
		t.Errorf("files = %v, want example only", pathwayIDs(files))
	}
	if len(problems) != 1 || !errors.Is(problems[0].Err, ErrOlderFormat) {
		t.Errorf("problems = %v, want the older file", problems)
	}
}
