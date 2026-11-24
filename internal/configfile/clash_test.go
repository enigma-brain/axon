package configfile

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// withRevision returns data with a top-level revision n after its id.
func withRevision(data string, n string) string {
	i := strings.Index(data, "\n[[pathway]]")
	return data[:i] + "\nrevision = " + n + data[i:]
}

func TestParseRevision(t *testing.T) {
	f, err := Parse([]byte(withRevision(campusFile, "7")))
	if err != nil || !f.HasRevision || f.Revision != 7 {
		t.Fatalf("Parse = %+v, %v; want revision 7", f, err)
	}
	f, err = Parse([]byte(campusFile))
	if err != nil || f.HasRevision {
		t.Fatalf("Parse without revision = %+v, %v", f, err)
	}
	for _, bad := range []string{"-1", `"3"`, "1.5", "true"} {
		_, err := Parse([]byte(withRevision(campusFile, bad)))
		if err == nil || err.Error() != "revision: must be a whole number, 0 or more" {
			t.Errorf("revision = %s: err = %v", bad, err)
		}
	}
}

func TestRecordHoldsTimeAndFingerprint(t *testing.T) {
	s, home := newStore(t)
	at := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return at }
	t.Cleanup(func() { now = time.Now })

	add(t, s, campusFile, "https://example.org/campus.toml")
	rec, ok, err := s.Record("example-campus")
	if err != nil || !ok {
		t.Fatalf("Record = %v, %v", ok, err)
	}
	if rec.HandMade || rec.Source.String() != "https://example.org/campus.toml" || !rec.StoredAt.Equal(at) || len(rec.SHA256) != 64 {
		t.Errorf("record = %+v", rec)
	}
	data, _ := os.ReadFile(filepath.Join(home, ".axon", "state", "configs", "example-campus.toml"))
	for _, want := range []string{`source = "https://example.org/campus.toml"`, `stored_at = "2026-11-23T12:00:00Z"`, `sha256 = "` + rec.SHA256 + `"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("record file %q lacks %s", data, want)
		}
	}
}

func TestRecordWrittenByAnEarlierAxon(t *testing.T) {
	s, home := newStore(t)
	add(t, s, campusFile, "-")
	path := filepath.Join(home, ".axon", "state", "configs", "example-campus.toml")
	if err := os.WriteFile(path, []byte("source = \"https://example.org/campus.toml\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := s.Record("example-campus")
	if err != nil || !ok || rec.HandMade || !rec.StoredAt.IsZero() || rec.SHA256 != "" || rec.Source.String() != "https://example.org/campus.toml" {
		t.Fatalf("Record = %+v, %v, %v", rec, ok, err)
	}
	src, ok, err := s.Source("example-campus")
	if err != nil || !ok || !src.IsAddress() {
		t.Errorf("Source = %v, %v, %v", src, ok, err)
	}
	// A refresh rewrites it in full.
	if _, _, err := s.Refresh("example-campus", []byte(withRevision(campusFile, "2")), src, false); err != nil {
		t.Fatal(err)
	}
	if rec, _, _ := s.Record("example-campus"); rec.StoredAt.IsZero() || rec.SHA256 == "" {
		t.Errorf("record after refresh = %+v", rec)
	}
}

func TestHandMadeRecordHasNoSource(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Put([]byte(campusFile), PutOptions{Origin: HandMade}); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := s.Record("example-campus")
	if err != nil || !ok || !rec.HandMade {
		t.Fatalf("Record = %+v, %v, %v", rec, ok, err)
	}
	if _, ok, err := s.Source("example-campus"); ok || err != nil {
		t.Errorf("Source of a hand-made file = %v, %v", ok, err)
	}
}

// directConfig returns a file fileID with one direct pathway.
func directConfig(fileID, id, name, endpoint string) string {
	return "format = 2\nid = \"" + fileID + "\"\n\n[[pathway]]\nid = \"" + id + "\"\nkind = \"direct\"\nname = \"" + name +
		"\"\ndescription = \"D\"\n[pathway.sky]\nendpoint = \"" + endpoint + "\"\n"
}

// teleportConfig returns a file fileID with one teleport pathway.
func teleportConfig(fileID, id, name, proxy, app string) string {
	return "format = 2\nid = \"" + fileID + "\"\n\n[[pathway]]\nid = \"" + id + "\"\nkind = \"teleport\"\nname = \"" + name +
		"\"\ndescription = \"D\"\n[pathway.teleport]\nproxy = \"" + proxy + "\"\n[pathway.sky]\napp = \"" + app + "\"\n"
}

func TestInspectClashes(t *testing.T) {
	for _, tc := range []struct {
		name, stored, added string
		want                []bool // SameName of each clash
	}{
		{"same name, any case", directConfig("a", "one", "Campus", "https://a.example.org"), directConfig("b", "two", "campus ", "https://b.example.org"), []bool{true}},
		{"same identifier", directConfig("a", "one", "A", "https://a.example.org"), directConfig("b", "one", "B", "https://b.example.org"), []bool{true}},
		{"same endpoint", directConfig("a", "one", "A", "https://Sky.example.org/"), directConfig("b", "two", "B", "https://sky.example.org"), []bool{false}},
		{"same proxy and app", teleportConfig("a", "one", "A", "tp.example.org", "skypilot"), teleportConfig("b", "two", "B", "TP.example.org:443", "skypilot"), []bool{false}},
		{"another app", teleportConfig("a", "one", "A", "tp.example.org", "skypilot"), teleportConfig("b", "two", "B", "tp.example.org", "other"), nil},
		{"another port", teleportConfig("a", "one", "A", "tp.example.org", "skypilot"), teleportConfig("b", "two", "B", "tp.example.org:3080", "skypilot"), nil},
		{"same id is an update", directConfig("a", "one", "A", "https://a.example.org"), directConfig("a", "one", "A", "https://a.example.org"), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newStore(t)
			add(t, s, tc.stored, "-")
			c, err := s.Inspect([]byte(tc.added), false)
			if err != nil {
				t.Fatal(err)
			}
			var got []bool
			for _, cl := range c.Clashes {
				got = append(got, cl.SameName)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("clashes = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPutRemovesAndStores(t *testing.T) {
	s, home := newStore(t)
	add(t, s, directConfig("a", "one", "Campus", "https://a.example.org"), "-")
	add(t, s, directConfig("keep", "keep", "Keep", "https://k.example.org"), "-")
	added := directConfig("b", "two", "Campus", "https://b.example.org")

	if _, err := s.Put([]byte(added), PutOptions{Origin: FromSource(mustSource(t, "-"))}); !errors.As(err, new(*ClashError)) {
		t.Fatalf("Put without removing = %v, want a clash", err)
	}
	c, err := s.Inspect([]byte(added), false)
	if err != nil || len(c.Clashes) != 1 {
		t.Fatalf("Inspect = %+v, %v", c, err)
	}
	if _, err := s.Put([]byte(added), PutOptions{Origin: FromSource(mustSource(t, "-")), Remove: []File{c.Clashes[0].Stored}}); err != nil {
		t.Fatal(err)
	}
	files, _, _ := s.List()
	if got := pathwayIDs(files); !reflect.DeepEqual(got, []string{"b/two", "keep/keep"}) {
		t.Errorf("stored = %v", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".axon", "state", "configs", "a.toml")); !os.IsNotExist(err) {
		t.Errorf("the removed file's record is still there: %v", err)
	}
}

func TestPutKeepsBothOnAServerClash(t *testing.T) {
	s, _ := newStore(t)
	add(t, s, directConfig("a", "one", "A", "https://sky.example.org"), "-")
	add(t, s, directConfig("b", "two", "B", "https://sky.example.org"), "-")
	files, _, _ := s.List()
	if len(files) != 2 {
		t.Errorf("stored %v, want both", pathwayIDs(files))
	}
}

func TestLowerRevision(t *testing.T) {
	s, _ := newStore(t)
	add(t, s, withRevision(campusFile, "7"), "-")
	c, err := s.Inspect([]byte(withRevision(campusFile, "5")), false)
	if err != nil || !c.Lower() || c.Previous.Revision != 7 {
		t.Fatalf("Inspect = %+v, %v; want a lower revision", c, err)
	}
	var lower *LowerRevisionError
	if _, err := s.Add([]byte(withRevision(campusFile, "5")), mustSource(t, "-")); !errors.As(err, &lower) || lower.Stored != 7 || lower.New != 5 {
		t.Fatalf("Add of a lower revision = %v", err)
	}
	if _, err := s.Put([]byte(withRevision(campusFile, "5")), PutOptions{Origin: FromSource(mustSource(t, "-")), AllowLower: true}); err != nil {
		t.Fatal(err)
	}
	// Equal, higher, or no revision on one side replaces without asking.
	for _, data := range []string{withRevision(campusFile, "5"), withRevision(campusFile, "6"), campusFile, withRevision(campusFile, "1")} {
		c, err := s.Inspect([]byte(data), false)
		if err != nil || c.Lower() {
			t.Errorf("Inspect = %v, %v; want no lower revision", c.Lower(), err)
		}
		add(t, s, data, "-")
	}
}

func TestRefreshWithANameClashIsRefused(t *testing.T) {
	s, _ := newStore(t)
	add(t, s, directConfig("example", "a", "A", "https://a.example.org"), refreshAddress)
	add(t, s, directConfig("other", "b", "B", "https://b.example.org"), "-")
	if _, _, err := s.Refresh("example", []byte(directConfig("example", "a", "b", "https://a.example.org")), mustSource(t, refreshAddress), false); !errors.As(err, new(*ClashError)) {
		t.Errorf("Refresh with a name clash = %v", err)
	}
	if _, _, err := s.Refresh("example", []byte(directConfig("example", "a", "A", "https://b.example.org")), mustSource(t, refreshAddress), false); err != nil {
		t.Errorf("Refresh with a server clash = %v", err)
	}
}

func TestSameIDAsAHandMadeFileClashes(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Put([]byte(directConfig("lab", "mine", "Mine", "https://m.example.org")), PutOptions{Origin: HandMade}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Inspect([]byte(directConfig("lab", "theirs", "Theirs", "https://t.example.org")), false)
	if err != nil || c.Previous != nil || len(c.Clashes) != 1 || !c.Clashes[0].SameFileID || !c.Clashes[0].StoredHandMade {
		t.Fatalf("Inspect = %+v, %v", c, err)
	}
	if _, err := s.Add([]byte(directConfig("lab", "theirs", "Theirs", "https://t.example.org")), mustSource(t, "-")); !errors.As(err, new(*ClashError)) {
		t.Errorf("Add over a hand-made file = %v", err)
	}
}

// A file that changed after the user agreed to remove it is not removed,
// and nothing is stored.
func TestPutRefusesToRemoveAFileThatChanged(t *testing.T) {
	s, home := newStore(t)
	add(t, s, directConfig("a", "one", "Campus", "https://a.example.org"), "-")
	added := directConfig("b", "two", "Campus", "https://b.example.org")
	c, err := s.Inspect([]byte(added), false)
	if err != nil {
		t.Fatal(err)
	}
	// Another login adds a pathway to the file in between.
	add(t, s, oneDirect("a", "one", "extra"), "-")
	before := snapshot(t, filepath.Join(home, ".axon"))
	_, err = s.Put([]byte(added), PutOptions{Origin: FromSource(mustSource(t, "-")), Remove: []File{c.Clashes[0].Stored}})
	var changed *ChangedError
	if !errors.As(err, &changed) || changed.Path != s.Path("a") {
		t.Fatalf("Put = %v, want the file named as changed", err)
	}
	if after := snapshot(t, filepath.Join(home, ".axon")); !reflect.DeepEqual(before, after) {
		t.Error("a refused put changed ~/.axon")
	}
}

// clashing stores file a, then returns a file b whose pathway has its name
// and the stored file to remove for it, as Inspect gives it.
func clashing(t *testing.T, s Store) ([]byte, File) {
	t.Helper()
	add(t, s, directConfig("a", "one", "Campus", "https://a.example.org"), "-")
	added := []byte(directConfig("b", "two", "Campus", "https://b.example.org"))
	c, err := s.Inspect(added, false)
	if err != nil || len(c.Clashes) != 1 {
		t.Fatalf("Inspect = %+v, %v", c, err)
	}
	return added, c.Clashes[0].Stored
}

// failOn makes the named step fail for path.
func failOn(t *testing.T, step *func(string, string) error, path string) {
	t.Helper()
	real := *step
	*step = func(from, to string) error {
		if from == path {
			return errors.New("injected failure")
		}
		return real(from, to)
	}
	t.Cleanup(func() { *step = real })
}

func failReplaceOn(t *testing.T, path string) {
	t.Helper()
	replaceFile = func(p string, data []byte) error {
		if p == path {
			return errors.New("injected failure")
		}
		return writeFile(p, data)
	}
	t.Cleanup(func() { replaceFile = writeFile })
}

// A put that fails, before or after the file to remove is set aside,
// leaves the stored files and records as they were.
func TestPutRestoresEverythingWhenAStepFails(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail func(t *testing.T, home string, s Store)
	}{
		{"setting aside the old record", func(t *testing.T, home string, s Store) {
			failOn(t, &renameFile, filepath.Join(home, ".axon", "state", "configs", "a.toml"))
		}},
		{"writing the new file", func(t *testing.T, home string, s Store) {
			failReplaceOn(t, s.Path("b"))
		}},
		{"writing the new record", func(t *testing.T, home string, s Store) {
			failReplaceOn(t, filepath.Join(home, ".axon", "state", "configs", "b.toml"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, home := newStore(t)
			added, old := clashing(t, s)
			before := snapshot(t, filepath.Join(home, ".axon"))
			tc.fail(t, home, s)
			_, err := s.Put(added, PutOptions{Origin: FromSource(mustSource(t, "-")), Remove: []File{old}})
			if err == nil || !strings.Contains(err.Error(), "injected failure") {
				t.Fatalf("Put = %v, want the injected failure", err)
			}
			if after := snapshot(t, filepath.Join(home, ".axon")); !reflect.DeepEqual(before, after) {
				t.Errorf("a failed put left changes:\nbefore %v\nafter  %v", before, after)
			}
		})
	}
}

// When putting a file back fails, the others are still put back, and the
// file's content stays on disk beside it.
func TestRollbackContinuesAfterAStepFails(t *testing.T) {
	s, home := newStore(t)
	added, old := clashing(t, s)
	original, _ := os.ReadFile(s.Path("a"))
	failReplaceOn(t, filepath.Join(home, ".axon", "state", "configs", "b.toml"))
	failOn(t, &renameFile, s.Path("a")+removingExt)
	_, err := s.Put(added, PutOptions{Origin: FromSource(mustSource(t, "-")), Remove: []File{old}})
	if err == nil || !strings.Contains(err.Error(), s.Path("a")+" could not be put back from "+s.Path("a")+removingExt) {
		t.Fatalf("Put = %v, want the file named", err)
	}
	if data, _ := os.ReadFile(s.Path("a") + removingExt); string(data) != string(original) {
		t.Errorf("the old content is not beside the file: %q", data)
	}
	if _, err := os.Stat(filepath.Join(home, ".axon", "state", "configs", "a.toml")); err != nil {
		t.Errorf("the old record was not put back: %v", err)
	}
	if _, err := os.Stat(s.Path("b")); !os.IsNotExist(err) {
		t.Errorf("the new file stayed: %v", err)
	}
}

// A file left aside by a put that stopped is never read, and does not stop
// a later add or replacement.
func TestALeftoverRemovingFileIsIgnored(t *testing.T) {
	s, home := newStore(t)
	add(t, s, directConfig("a", "one", "Campus", "https://a.example.org"), "-")
	for _, p := range []string{s.Path("a"), filepath.Join(home, ".axon", "state", "configs", "a.toml")} {
		data, _ := os.ReadFile(p)
		if err := os.WriteFile(p+removingExt, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The leftover has the same pathway as the stored file, and is not
	// read as a second copy of it.
	files, problems, err := s.List()
	if err != nil || len(problems) != 0 || len(files) != 1 {
		t.Fatalf("List = %v, %v, %v", pathwayIDs(files), problems, err)
	}
	add(t, s, directConfig("c", "three", "C", "https://c.example.org"), "-")
	// A replacement sets the file aside again over the leftover.
	added := []byte(directConfig("b", "two", "Campus", "https://b.example.org"))
	c, err := s.Inspect(added, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(added, PutOptions{Origin: FromSource(mustSource(t, "-")), Remove: []File{c.Clashes[0].Stored}}); err != nil {
		t.Fatal(err)
	}
	if files, _, _ := s.List(); !reflect.DeepEqual(pathwayIDs(files), []string{"b/two", "c/three"}) {
		t.Errorf("stored %v", pathwayIDs(files))
	}
	if _, err := os.Stat(s.Path("a") + removingExt); !os.IsNotExist(err) {
		t.Errorf("the file set aside was not deleted: %v", err)
	}
}

func TestSameServerKeepsThePathsCase(t *testing.T) {
	a := Pathway{Kind: KindDirect, Sky: Sky{Endpoint: "https://Sky.example.org/Team/"}}
	b := Pathway{Kind: KindDirect, Sky: Sky{Endpoint: "https://sky.example.org/Team"}}
	c := Pathway{Kind: KindDirect, Sky: Sky{Endpoint: "https://sky.example.org/team"}}
	if !SameServer(a, b) || SameServer(a, c) {
		t.Errorf("SameServer: host case and trailing slash ignored = %v, path case ignored = %v", SameServer(a, b), SameServer(a, c))
	}
}
