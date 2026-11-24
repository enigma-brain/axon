package configlist

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/orgfile"
)

const campus = `format = 2
id = "example-campus"
revision = 3

[[pathway]]
id = "campus"
kind = "direct"
name = "Campus"
description = "On campus."

[pathway.sky]
endpoint = "https://sky.example.org"
`

const remote = `format = 2
id = "example-remote"

[[pathway]]
id = "remote"
kind = "teleport"
name = "Remote"
description = "From anywhere."

[pathway.teleport]
proxy = "teleport.example.org:3080"

[pathway.sky]
app = "skypilot"

[[pathway]]
id = "lab"
kind = "direct"
name = "Lab"
description = "In the lab."

[pathway.sky]
endpoint = "https://lab.example.org/sky"
`

// handMade is a file as axon login writes it for a hand-made pathway.
const handMade = `format = 2
id = "sky-example-net"

[[pathway]]
id = "sky-example-net"
kind = "direct"
name = "sky.example.net"
description = "Made on this laptop. Goes to https://sky.example.net."

[pathway.sky]
endpoint = "https://sky.example.net"
`

var storedLine = regexp.MustCompile(`, stored \d{4}-\d\d-\d\d \d\d:\d\d UTC$`)

func newDir(t *testing.T) (axondir.Dir, orgfile.Store) {
	t.Helper()
	dir := axondir.New(t.TempDir())
	return dir, orgfile.NewStore(dir)
}

func add(t *testing.T, s orgfile.Store, data, source string) {
	t.Helper()
	src, err := orgfile.ParseSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add([]byte(data), src); err != nil {
		t.Fatal(err)
	}
}

func list(t *testing.T, dir axondir.Dir) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Run(Options{Dir: dir, Out: &out, Location: time.UTC})
	text := out.String()
	for i, line := range strings.Split(text, "\n") {
		// A line may be longer only when its last word alone would not
		// fit, such as a long path.
		trimmed := strings.TrimLeft(line, " ")
		last := trimmed[strings.LastIndex(trimmed, " ")+1:]
		if n := len([]rune(line)); n > 80 && len([]rune(last)) <= 80-(len(line)-len(trimmed)) {
			t.Errorf("line %d is %d characters: %q", i+1, n, line)
		}
		if strings.ContainsRune(line, '\x1b') {
			t.Errorf("line %d holds an escape: %q", i+1, line)
		}
	}
	return text, err
}

// lines returns the listing's lines with each stored time replaced by
// ", stored <time>".
func lines(text string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		out = append(out, storedLine.ReplaceAllString(l, ", stored <time>"))
	}
	return out
}

func assertLines(t *testing.T, text string, want ...string) {
	t.Helper()
	got := lines(text)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("listing:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestNothingStored(t *testing.T) {
	dir, _ := newDir(t)
	text, err := list(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, text, "No config file or pathway is stored. Run `axon login` to add a pathway.")
	if _, err := os.Stat(dir.Root()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("listing created %s: %v", dir.Root(), err)
	}
}

func TestOneFileOnePathway(t *testing.T) {
	dir, s := newDir(t)
	add(t, s, campus, "https://example.org/campus.toml")
	text, err := list(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, text,
		"No pathway is in use. `axon login` chooses one.",
		"",
		"example-campus",
		"  from https://example.org/campus.toml",
		"  revision 3, stored <time>",
		"",
		"    Campus  --pathway campus",
		"      direct, https://sky.example.org",
	)
}

func TestSeveralFilesOneInUse(t *testing.T) {
	dir, s := newDir(t)
	add(t, s, campus, "https://example.org/campus.toml")
	path := filepath.Join(t.TempDir(), "remote.toml")
	add(t, s, remote, path)
	if err := dir.SetPathway("remote"); err != nil {
		t.Fatal(err)
	}
	text, err := list(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, text,
		"The pathway in use is Remote (remote), from example-remote.",
		"",
		"example-campus",
		"  from https://example.org/campus.toml",
		"  revision 3, stored <time>",
		"",
		"    Campus  --pathway campus",
		"      direct, https://sky.example.org",
		"",
		"example-remote",
		"  from "+path,
		"  no revision, stored <time>",
		"",
		"  * Remote  --pathway remote",
		"      teleport, proxy teleport.example.org:3080, application skypilot",
		"    Lab  --pathway lab",
		"      direct, https://lab.example.org/sky",
	)
}

func TestHandMadePathway(t *testing.T) {
	dir, s := newDir(t)
	if _, err := s.Put([]byte(handMade), orgfile.PutOptions{Origin: orgfile.HandMade}); err != nil {
		t.Fatal(err)
	}
	if err := dir.SetPathway("sky-example-net"); err != nil {
		t.Fatal(err)
	}
	text, err := list(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, text,
		"The pathway in use is sky.example.net (sky-example-net), from sky-example-net.",
		"",
		"sky-example-net",
		"  made by hand at `axon login`",
		"  no revision, stored <time>",
		"",
		"  * sky.example.net  --pathway sky-example-net",
		"      direct, https://sky.example.net",
	)
}

func TestAddressWithQueryString(t *testing.T) {
	dir, s := newDir(t)
	add(t, s, campus, "https://example.org/campus.toml?token=s3cret#frag")
	text, err := list(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "  from https://example.org/campus.toml\n") {
		t.Errorf("listing lacks the printable address:\n%s", text)
	}
	for _, secret := range []string{"s3cret", "token", "frag"} {
		if strings.Contains(text, secret) {
			t.Errorf("listing holds %q:\n%s", secret, text)
		}
	}
}

// A record written by an earlier axon holds only the source; a file may
// also have no record at all.
func TestOlderAndMissingRecords(t *testing.T) {
	dir, s := newDir(t)
	add(t, s, campus, "https://example.org/campus.toml")
	record := filepath.Join(dir.Root(), "state", "orgs", "example-campus.toml")
	if _, err := os.Stat(record); err != nil {
		t.Fatalf("the record is not where this test expects it: %v", err)
	}
	if err := os.WriteFile(record, []byte("source = \"https://example.org/campus.toml\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path("example-remote"), []byte(remote), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := list(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, text,
		"No pathway is in use. `axon login` chooses one.",
		"",
		"example-campus",
		"  from https://example.org/campus.toml",
		"  revision 3",
		"",
		"    Campus  --pathway campus",
		"      direct, https://sky.example.org",
		"",
		"example-remote",
		"  source not recorded",
		"  no revision",
		"",
		"    Remote  --pathway remote",
		"      teleport, proxy teleport.example.org:3080, application skypilot",
		"    Lab  --pathway lab",
		"      direct, https://lab.example.org/sky",
	)
}

func TestUnreadableFile(t *testing.T) {
	dir, s := newDir(t)
	add(t, s, campus, "https://example.org/campus.toml")
	broken := s.Path("broken")
	if err := os.WriteFile(broken, []byte("format = 2\nid = \"broken\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := dir.SetPathway("campus"); err != nil {
		t.Fatal(err)
	}
	text, err := list(t, dir)
	var unreadable *UnreadableError
	if !errors.As(err, &unreadable) || unreadable.Count != 1 {
		t.Fatalf("err = %v, want one unreadable file", err)
	}
	if !strings.Contains(err.Error(), "1 stored file could not be read") {
		t.Errorf("err = %q", err)
	}
	got := lines(text)
	if len(got) < 3 || got[len(got)-2] != broken || !strings.HasPrefix(got[len(got)-1], "  cannot be read: ") {
		t.Errorf("listing does not end with the unreadable file:\n%s", text)
	}
	if !strings.Contains(text, "  * Campus  --pathway campus\n") {
		t.Errorf("the readable file is not listed:\n%s", text)
	}
}

func TestOnlyUnreadableFiles(t *testing.T) {
	dir, s := newDir(t)
	if err := os.MkdirAll(filepath.Dir(s.Path("x")), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path("old"), []byte("format = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := list(t, dir)
	if err == nil {
		t.Fatal("no error")
	}
	if strings.Contains(text, "No config file or pathway is stored") {
		t.Errorf("listing says nothing is stored:\n%s", text)
	}
	if !strings.Contains(text, s.Path("old")+"\n  cannot be read: the file is in an older format.") {
		t.Errorf("listing does not name the old file:\n%s", text)
	}
}

func TestChosenPathwayInNoFile(t *testing.T) {
	dir, s := newDir(t)
	add(t, s, campus, "https://example.org/campus.toml")
	if err := dir.SetPathway("gone"); err != nil {
		t.Fatal(err)
	}
	text, err := list(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text, "No pathway is in use: the chosen pathway \"gone\" is in no stored config file.\n`axon login` chooses one.\n\n") {
		t.Errorf("listing:\n%s", text)
	}
	if strings.Contains(text, "* ") {
		t.Errorf("a pathway is marked:\n%s", text)
	}
}

func TestUnreadablePathwayInUse(t *testing.T) {
	dir, s := newDir(t)
	add(t, s, campus, "https://example.org/campus.toml")
	if err := os.WriteFile(dir.ConfigPath(), []byte("pathway = \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := list(t, dir)
	if err == nil {
		t.Fatal("no error")
	}
	if !strings.HasPrefix(text, "The pathway in use is not known: read "+dir.ConfigPath()+":\n") {
		t.Errorf("listing:\n%s", text)
	}
	if !strings.Contains(text, "    Campus  --pathway campus\n") {
		t.Errorf("the file is not listed:\n%s", text)
	}
}

func TestControlCharactersRemoved(t *testing.T) {
	dir, s := newDir(t)
	data := strings.Replace(campus, `name = "Campus"`, `name = "Cam\u001b[31mpus\u202e\u0007"`, 1)
	add(t, s, data, "https://example.org/campus.toml")
	text, err := list(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "    Cam[31mpus  --pathway campus\n") {
		t.Errorf("listing:\n%q", text)
	}
	for _, r := range []rune{'\x1b', '\u202e', '\a'} {
		if strings.ContainsRune(text, r) {
			t.Errorf("listing holds %U", r)
		}
	}
}

func TestClean(t *testing.T) {
	for in, want := range map[string]string{
		"plain":             "plain",
		"tab\there":         "tabhere",
		"new\nline\r":       "newline",
		"\u200bzero width":  "zero width",
		"bidi\u2066x\u2069": "bidix",
		"é and ü":           "é and ü",
	} {
		if got := clean(in); got != want {
			t.Errorf("clean(%q) = %q, want %q", in, got, want)
		}
	}
}

// Listing writes nothing: the directory's files are the same after it.
func TestListingChangesNothing(t *testing.T) {
	dir, s := newDir(t)
	add(t, s, campus, "https://example.org/campus.toml")
	before := snapshot(t, dir.Root())
	if _, err := list(t, dir); err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, dir.Root())
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("files changed:\nbefore %v\nafter  %v", before, after)
	}
}

func snapshot(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		data := ""
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			data = string(b)
		}
		out = append(out, path+" "+info.Mode().String()+" "+info.ModTime().String()+" "+data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
