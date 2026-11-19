package connected

import (
	"os"
	"testing"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/orgfile"
)

func remote() orgfile.Pathway {
	return orgfile.Pathway{
		File: "example", ID: "remote", Kind: orgfile.KindTeleport,
		Name: "Remote", Description: "From anywhere.",
		Teleport: &orgfile.Teleport{Proxy: "teleport.example.org"},
		Devenv:   orgfile.Devenv{App: "devenv"},
	}
}

func TestWriteAndRead(t *testing.T) {
	dir := axondir.New(t.TempDir())
	if _, ok, err := Read(dir); ok || err != nil {
		t.Fatalf("Read before any write = %v, %v", ok, err)
	}
	if err := Write(dir, remote()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	want := "format = 1\nid = \"remote\"\nkind = \"teleport\"\nteleport_proxy = \"teleport.example.org\"\ndevenv_app = \"devenv\"\n"
	if string(data) != want {
		t.Errorf("record = %q, want %q", data, want)
	}
	r, ok, err := Read(dir)
	if err != nil || !ok || r != Of(remote()) {
		t.Errorf("Read = %+v, %v, %v", r, ok, err)
	}
}

func TestIs(t *testing.T) {
	dir := axondir.New(t.TempDir())
	if same, err := Is(dir, remote()); same || err != nil {
		t.Errorf("Is with no record = %v, %v", same, err)
	}
	if err := Write(dir, remote()); err != nil {
		t.Fatal(err)
	}

	renamed := remote()
	renamed.Name, renamed.Description, renamed.UnreachableHint = "Other", "Other.", "Other."
	if same, _ := Is(dir, renamed); !same {
		t.Error("a change of text for people alone is another pathway")
	}
	for name, change := range map[string]func(*orgfile.Pathway){
		"id":    func(p *orgfile.Pathway) { p.ID = "elsewhere" },
		"proxy": func(p *orgfile.Pathway) { p.Teleport = &orgfile.Teleport{Proxy: "teleport.example.org:3080"} },
		"app":   func(p *orgfile.Pathway) { p.Devenv.App = "other" },
	} {
		p := remote()
		change(&p)
		if same, _ := Is(dir, p); same {
			t.Errorf("a change of %s is the same pathway", name)
		}
	}
}

func TestOtherFormatIsNoRecord(t *testing.T) {
	dir := axondir.New(t.TempDir())
	if _, err := dir.StateDir(); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"format = 2\nid = \"remote\"\n", "not toml ["} {
		if err := os.WriteFile(Path(dir), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := Read(dir); ok || err != nil {
			t.Errorf("Read of %q = %v, %v; want no record", content, ok, err)
		}
	}
}
