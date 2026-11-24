package configfile

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// campusFile and remoteFile are the two example files in the design.
const campusFile = `# campus.toml
format = 2
id = "example-campus"

[[pathway]]
id = "campus"
kind = "direct"
name = "Campus"
description = "Single sign-on in a browser. Needs the campus network or VPN."
unreachable_hint = "Connect to the campus VPN and retry."

[pathway.sky]
endpoint = "https://sky.example.org"
`

const remoteFile = `# remote.toml
format = 2
id = "example-remote"

[[pathway]]
id = "remote"
kind = "teleport"
name = "Remote"
description = "Teleport. Works from anywhere."

[pathway.teleport]
proxy = "teleport.example.org"

[pathway.sky]
app = "skypilot"
`

func TestParseDesignExamples(t *testing.T) {
	campus, err := Parse([]byte(campusFile))
	if err != nil {
		t.Fatal(err)
	}
	want := File{ID: "example-campus", Pathways: []Pathway{{
		File:            "example-campus",
		ID:              "campus",
		Kind:            KindDirect,
		Name:            "Campus",
		Description:     "Single sign-on in a browser. Needs the campus network or VPN.",
		UnreachableHint: "Connect to the campus VPN and retry.",
		Sky:             Sky{Endpoint: "https://sky.example.org"},
	}}}
	if !reflect.DeepEqual(campus, want) {
		t.Errorf("campus = %+v\nwant %+v", campus, want)
	}

	remote, err := Parse([]byte(remoteFile))
	if err != nil {
		t.Fatal(err)
	}
	want = File{ID: "example-remote", Pathways: []Pathway{{
		File:        "example-remote",
		ID:          "remote",
		Kind:        KindTeleport,
		Name:        "Remote",
		Description: "Teleport. Works from anywhere.",
		Teleport:    &Teleport{Proxy: "teleport.example.org"},
		Sky:         Sky{App: "skypilot"},
	}}}
	if !reflect.DeepEqual(remote, want) {
		t.Errorf("remote = %+v\nwant %+v", remote, want)
	}
}

func TestParseSeveralPathwaysKeepsOrder(t *testing.T) {
	data := `format = 2
id = "example"

[[pathway]]
id = "b-remote"
kind = "teleport"
name = "Remote"
description = "Anywhere."
[pathway.teleport]
proxy = "teleport.example.org:3080"
[pathway.sky]
app = "sky"

[[pathway]]
id = "a-campus"
kind = "direct"
name = "Campus"
description = "On campus."
[pathway.sky]
endpoint = "https://sky.example.org:8443/api"
`
	f, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range f.Pathways {
		ids = append(ids, p.ID)
	}
	if want := []string{"b-remote", "a-campus"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("pathway order = %v, want %v", ids, want)
	}
	if got := f.Pathways[0].Teleport.Proxy; got != "teleport.example.org:3080" {
		t.Errorf("proxy = %q", got)
	}
}

// direct is a valid file with one direct pathway, built from parts so that
// each test changes one of them.
type direct struct {
	head, pathway, sky, extra string
}

func (d direct) String() string {
	return d.head + "\n[[pathway]]\n" + d.pathway + "\n" + d.extra + "\n[pathway.sky]\n" + d.sky + "\n"
}

var (
	goodHead    = "format = 2\nid = \"example\"\n"
	goodPathway = "id = \"campus\"\nkind = \"direct\"\nname = \"Campus\"\ndescription = \"On campus.\"\n"
	goodSky     = "endpoint = \"https://sky.example.org\"\n"
)

func teleportFile(teleport, sky string) string {
	return goodHead + "\n[[pathway]]\nid = \"remote\"\nkind = \"teleport\"\nname = \"Remote\"\ndescription = \"Anywhere.\"\n" +
		teleport + "\n[pathway.sky]\n" + sky + "\n"
}

func TestParseRefuses(t *testing.T) {
	good := direct{goodHead, goodPathway, goodSky, ""}
	if _, err := Parse([]byte(good.String())); err != nil {
		t.Fatalf("the base file is refused: %v", err)
	}
	if _, err := Parse([]byte(teleportFile("[pathway.teleport]\nproxy = \"teleport.example.org\"\n", "app = \"sky\"\n"))); err != nil {
		t.Fatalf("the base teleport file is refused: %v", err)
	}

	tests := []struct {
		name      string
		data      string
		field     string
		checkPart string
	}{
		{"not TOML", "format = = 1", "format", "not valid TOML"},
		{"not TOML, no key", "[[", "", "not valid TOML"},
		{"pathway not a table", goodHead + "pathway = \"campus\"\n", "pathway", "[[pathway]]"},
		{"pathway a single table", goodHead + "[pathway]\n" + goodPathway, "pathway", "[[pathway]]"},
		{"sky not a table", goodHead + "\n[[pathway]]\n" + goodPathway + "sky = \"x\"\n", "pathway 1: sky", "table"},
		{"teleport not a table", teleportFile("teleport = 1\n", "app = \"sky\"\n"), "pathway 1: teleport", "table"},
		{"format missing", "id = \"example\"\n[[pathway]]\n" + goodPathway, "format", "must be 2"},
		{"format 3", direct{"format = 3\nid = \"example\"\n", goodPathway, goodSky, ""}.String(), "format", "must be 2"},
		{"format text", direct{"format = \"2\"\nid = \"example\"\n", goodPathway, goodSky, ""}.String(), "format", "must be 2"},
		{"id missing", direct{"format = 2\n", goodPathway, goodSky, ""}.String(), "id", "lowercase"},
		{"id uppercase", direct{"format = 2\nid = \"Example\"\n", goodPathway, goodSky, ""}.String(), "id", "lowercase"},
		{"id starts with a digit", direct{"format = 2\nid = \"1example\"\n", goodPathway, goodSky, ""}.String(), "id", "start with a letter"},
		{"id too long", direct{"format = 2\nid = \"" + strings.Repeat("a", 33) + "\"\n", goodPathway, goodSky, ""}.String(), "id", "at most 32"},
		{"id with a dot", direct{"format = 2\nid = \"ex.ample\"\n", goodPathway, goodSky, ""}.String(), "id", "lowercase"},
		{"no pathway", goodHead, "pathway", "at least one"},
		{"unknown top-level field", direct{goodHead + "name = \"Example\"\n", goodPathway, goodSky, ""}.String(), "name", "not a field"},
		{"unknown top-level table", direct{goodHead, goodPathway, goodSky, ""}.String() + "[extra]\nkey = 1\n", "extra", "not a field"},
		{"unknown pathway field", direct{goodHead, goodPathway + "network = \"campus\"\n", goodSky, ""}.String(), "pathway.network", "not a field"},
		{"unknown sky field", direct{goodHead, goodPathway, goodSky + "port = 1\n", ""}.String(), "pathway.sky.port", "not a field"},
		{"unknown pathway table", direct{goodHead, goodPathway, goodSky, "[pathway.jobs]\nendpoint = \"https://jobs.example.org\"\n"}.String(), "pathway.jobs", "not a field"},
		{"pathway id missing", direct{goodHead, "kind = \"direct\"\nname = \"Campus\"\ndescription = \"On campus.\"\n", goodSky, ""}.String(), "pathway 1: id", "lowercase"},
		{"pathway id uppercase", direct{goodHead, strings.Replace(goodPathway, "campus", "Campus", 1), goodSky, ""}.String(), "pathway 1: id", "lowercase"},
		{"pathway id with underscore", direct{goodHead, strings.Replace(goodPathway, "campus", "on_campus", 1), goodSky, ""}.String(), "pathway 1: id", "lowercase"},
		{"kind missing", direct{goodHead, strings.Replace(goodPathway, "kind = \"direct\"\n", "", 1), goodSky, ""}.String(), `pathway "campus": kind`, `"direct" or "teleport"`},
		{"kind unknown", direct{goodHead, strings.Replace(goodPathway, "direct", "vpn", 1), goodSky, ""}.String(), `pathway "campus": kind`, `"direct" or "teleport"`},
		{"name missing", direct{goodHead, strings.Replace(goodPathway, "name = \"Campus\"\n", "", 1), goodSky, ""}.String(), `pathway "campus": name`, "required"},
		{"name blank", direct{goodHead, strings.Replace(goodPathway, "\"Campus\"", "\"  \"", 1), goodSky, ""}.String(), `pathway "campus": name`, "required"},
		{"description missing", direct{goodHead, strings.Replace(goodPathway, "description = \"On campus.\"\n", "", 1), goodSky, ""}.String(), `pathway "campus": description`, "required"},
		{"description not text", direct{goodHead, strings.Replace(goodPathway, "\"On campus.\"", "3", 1), goodSky, ""}.String(), `pathway "campus": description`, "required"},
		{"hint not text", direct{goodHead, goodPathway + "unreachable_hint = 3\n", goodSky, ""}.String(), `pathway "campus": unreachable_hint`, "text"},
		{"direct without endpoint", direct{goodHead, goodPathway, "", ""}.String(), `pathway "campus": sky.endpoint`, "https address"},
		{"direct without sky", goodHead + "\n[[pathway]]\n" + goodPathway, `pathway "campus": sky.endpoint`, "https address"},
		{"direct with http endpoint", direct{goodHead, goodPathway, "endpoint = \"http://sky.example.org\"\n", ""}.String(), `pathway "campus": sky.endpoint`, "https address"},
		{"direct with hostless endpoint", direct{goodHead, goodPathway, "endpoint = \"https:sky.example.org\"\n", ""}.String(), `pathway "campus": sky.endpoint`, "https address"},
		{"direct with app", direct{goodHead, goodPathway, goodSky + "app = \"sky\"\n", ""}.String(), `pathway "campus": sky.app`, "not allowed on a direct"},
		{"direct with teleport table", direct{goodHead, goodPathway, goodSky, "[pathway.teleport]\nproxy = \"teleport.example.org\"\n"}.String(), `pathway "campus": teleport`, "not allowed on a direct"},
		{"teleport without proxy", teleportFile("", "app = \"sky\"\n"), `pathway "remote": teleport.proxy`, "hostname"},
		{"teleport with scheme", teleportFile("[pathway.teleport]\nproxy = \"https://teleport.example.org\"\n", "app = \"sky\"\n"), `pathway "remote": teleport.proxy`, "hostname"},
		{"teleport with path", teleportFile("[pathway.teleport]\nproxy = \"teleport.example.org/x\"\n", "app = \"sky\"\n"), `pathway "remote": teleport.proxy`, "hostname"},
		{"teleport with user", teleportFile("[pathway.teleport]\nproxy = \"ada@teleport.example.org\"\n", "app = \"sky\"\n"), `pathway "remote": teleport.proxy`, "hostname"},
		{"teleport with port 0", teleportFile("[pathway.teleport]\nproxy = \"teleport.example.org:0\"\n", "app = \"sky\"\n"), `pathway "remote": teleport.proxy`, "hostname"},
		{"teleport with port too large", teleportFile("[pathway.teleport]\nproxy = \"teleport.example.org:65536\"\n", "app = \"sky\"\n"), `pathway "remote": teleport.proxy`, "hostname"},
		{"teleport with empty port", teleportFile("[pathway.teleport]\nproxy = \"teleport.example.org:\"\n", "app = \"sky\"\n"), `pathway "remote": teleport.proxy`, "hostname"},
		{"teleport with bad label", teleportFile("[pathway.teleport]\nproxy = \"-teleport.example.org\"\n", "app = \"sky\"\n"), `pathway "remote": teleport.proxy`, "hostname"},
		{"teleport without app", teleportFile("[pathway.teleport]\nproxy = \"teleport.example.org\"\n", ""), `pathway "remote": sky.app`, "required"},
		{"teleport with endpoint", teleportFile("[pathway.teleport]\nproxy = \"teleport.example.org\"\n", "app = \"sky\"\nendpoint = \"https://sky.example.org\"\n"), `pathway "remote": sky.endpoint`, "not allowed on a teleport"},
		{"unknown teleport field", teleportFile("[pathway.teleport]\nproxy = \"teleport.example.org\"\ncluster = \"c\"\n", "app = \"sky\"\n"), "pathway.teleport.cluster", "not a field"},
		{"id in another case", direct{goodHead + "ID = \"other\"\n", goodPathway, goodSky, ""}.String(), "ID", "not a field"},
		{"pathway field in another case", direct{goodHead, goodPathway + "Kind = \"teleport\"\n", goodSky, ""}.String(), "pathway.Kind", "not a field"},
		{"sky field in another case", direct{goodHead, goodPathway, goodSky + "Endpoint = \"https://x.example.org\"\n", ""}.String(), "pathway.sky.Endpoint", "not a field"},
		{"too large", direct{goodHead, goodPathway, goodSky, ""}.String() + "#" + strings.Repeat("x", MaxSize), "", "larger than"},
		{"pathway id twice", direct{goodHead, goodPathway, goodSky, ""}.String() + "\n[[pathway]]\n" + goodPathway + "\n[pathway.sky]\n" + goodSky, `pathway "campus": id`, "more than once"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.data))
			if err == nil {
				t.Fatalf("Parse succeeded on:\n%s", tt.data)
			}
			var ce *CheckError
			if !errors.As(err, &ce) {
				t.Fatalf("error %v is not a *CheckError", err)
			}
			if ce.Field != tt.field {
				t.Errorf("field = %q, want %q (error: %v)", ce.Field, tt.field, err)
			}
			if !strings.Contains(ce.Check, tt.checkPart) {
				t.Errorf("check = %q, want it to contain %q", ce.Check, tt.checkPart)
			}
		})
	}
}

func TestIsHostPort(t *testing.T) {
	for s, want := range map[string]bool{
		"teleport.example.org":       true,
		"teleport.example.org:443":   true,
		"teleport.example.org:65535": true,
		"localhost":                  true,
		"TELEPORT.example.org":       true,
		"":                           false,
		":443":                       false,
		"teleport.example.org:+443":  false,
		"teleport..example.org":      false,
		"teleport.example.org.":      false,
		"teleport_x.example.org":     false,
		"teleport.example.org:1:2":   false,
		strings.Repeat("a", 64):      false,
	} {
		if got := isHostPort(s); got != want {
			t.Errorf("isHostPort(%q) = %v, want %v", s, got, want)
		}
	}
}

// A file of the format an earlier axon used is refused as older, before
// its tables are checked.
func TestParseRefusesOlderFormat(t *testing.T) {
	v1 := `format = 1
id = "example"

[[pathway]]
id = "campus"
kind = "direct"
name = "Campus"
description = "On campus."

[pathway.devenv]
endpoint = "https://sky.example.org"
`
	if _, err := Parse([]byte(v1)); !errors.Is(err, ErrOlderFormat) {
		t.Errorf("Parse of a format 1 file: err = %v, want %v", err, ErrOlderFormat)
	}
}

func TestParseEmpty(t *testing.T) {
	for name, data := range map[string]string{
		"no bytes":        "",
		"white space":     " \n\t\r\n",
		"comments":        "# a config file\n\n  # nothing else\n",
		"comment, no eol": "# only",
	} {
		if _, err := Parse([]byte(data)); !errors.Is(err, ErrEmpty) {
			t.Errorf("%s: Parse = %v, want ErrEmpty", name, err)
		}
	}
	// A file with something in it is checked as before.
	if _, err := Parse([]byte("id = \"x\"\n")); err == nil || errors.Is(err, ErrEmpty) || err.Error() != "format: must be 2" {
		t.Errorf("Parse of a file without format = %v, want the format refused", err)
	}
}
