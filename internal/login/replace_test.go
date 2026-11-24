package login

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/configfile"
	"github.com/enigma-brain/axon/internal/connected"
	"github.com/enigma-brain/axon/internal/testkit"
)

// writeConfig writes data to a file in a temporary directory and returns its
// path, for --config.
func writeConfig(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// otherCampus has a pathway named Campus, like testkit.ExampleConfig's, under another
// identifier and server.
const otherCampus = `format = 2
id = "other"

[[pathway]]
id = "main"
kind = "direct"
name = "campus"
description = "Another campus."

[pathway.sky]
endpoint = "https://main.example.org"
`

// sameServer reaches testkit.ExampleConfig's campus server under another name.
const sameServer = `format = 2
id = "other"

[[pathway]]
id = "main"
kind = "direct"
name = "Main"
description = "The same server."

[pathway.sky]
endpoint = "https://sky.example.org/"
`

func (h *harness) storedIDs() []string {
	h.t.Helper()
	files, _, err := configfile.NewStore(axondir.New(h.home)).List()
	if err != nil {
		h.t.Fatal(err)
	}
	var ids []string
	for _, f := range files {
		ids = append(ids, f.ID)
	}
	return ids
}

func TestConfigNameClashYes(t *testing.T) {
	h := newHarness(t)
	h.logIn()
	if err := h.login(Options{Config: writeConfig(t, otherCampus), In: strings.NewReader("y\n")}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	out := h.out.String()
	path := filepath.Join(h.home, ".axon", "configs", "example.toml")
	for _, want := range []string{
		"The stored pathway \"Campus\" has the same name as the pathway you are adding.\n" +
			"Replacing it removes the config file " + path + " and all its pathways: Campus and Remote.\n" +
			"Replace it? [y/N, Enter for no]: ",
		"Removed the config file " + path + "\n",
		"Stored the config file " + filepath.Join(h.home, ".axon", "configs", "other.toml") + "\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if got := h.storedIDs(); len(got) != 1 || got[0] != "other" || h.active() != "main" {
		t.Errorf("stored %v, active %q", got, h.active())
	}
}

func TestConfigNameClashNo(t *testing.T) {
	h := newHarness(t)
	before := testkit.Snapshot(t, filepath.Join(h.home, ".axon"))
	err := h.login(Options{Config: writeConfig(t, otherCampus), In: strings.NewReader("\n")})
	want := `add the config file: not added, since its pathway "campus" has the same name as a stored pathway in ` +
		filepath.Join(h.home, ".axon", "configs", "example.toml") + ". Run the command again with --replace to replace that file."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if after := testkit.Snapshot(t, filepath.Join(h.home, ".axon")); !equalMaps(before, after) {
		t.Error("a declined file changed ~/.axon")
	}
}

func TestConfigClashWithoutATerminal(t *testing.T) {
	h := newHarness(t)
	err := h.login(Options{Config: writeConfig(t, otherCampus)})
	want := `add the config file: its pathway "campus" clashes with the stored pathway "Campus" in ` +
		filepath.Join(h.home, ".axon", "configs", "example.toml") +
		", and there is no terminal to ask whether to replace it. Run the command again with --replace to replace it."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestConfigClashWithReplace(t *testing.T) {
	h := newHarness(t)
	h.logIn()
	if err := h.login(Options{Config: writeConfig(t, otherCampus), Replace: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.out.String(), "Replace it?") {
		t.Errorf("--replace asked:\n%s", h.out.String())
	}
	if got := h.storedIDs(); len(got) != 1 || got[0] != "other" {
		t.Errorf("stored %v", got)
	}
}

func TestServerClashNoKeepsBoth(t *testing.T) {
	h := newHarness(t)
	h.logIn()
	if err := h.login(Options{Config: writeConfig(t, sameServer), In: strings.NewReader("n\n")}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	out := h.out.String()
	for _, want := range []string{
		`The stored pathway "Campus" uses the same SkyPilot API server, https://sky.example.org, as the pathway you are adding.`,
		"Keeping both.\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if got := h.storedIDs(); len(got) != 2 {
		t.Errorf("stored %v, want both", got)
	}
}

func TestTeleportServerClash(t *testing.T) {
	h := newHarness(t)
	h.servers(nil)
	err := h.login(Options{TeleportProxy: "TELEPORT.example.org:443", Name: "Elsewhere"})
	want := `make the pathway: it clashes with the stored pathway "Remote" in ` + filepath.Join(h.home, ".axon", "configs", "example.toml") +
		", and there is no terminal to ask whether to replace it. Run the command again with --replace to replace it."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	h.out.Reset()
	h.stopAtTsh()
	h.login(Options{TeleportProxy: "teleport.example.org", Name: "Elsewhere", In: strings.NewReader("n\n")})
	if !strings.Contains(h.out.String(), `The stored pathway "Remote" uses the same Teleport proxy and application, teleport.example.org and skypilot, as the pathway you are adding.`) {
		t.Errorf("output:\n%s", h.out.String())
	}
}

func TestHandMadeNameClashAsksForAnotherName(t *testing.T) {
	h := newHarness(t)
	h.servers(nil)
	h.logIn()
	if err := h.login(Options{SkyEndpoint: "lab.example.org", Name: "Lab"}); err != nil {
		t.Fatal(err)
	}
	h.out.Reset()
	if err := h.login(Options{SkyEndpoint: "lab2.example.org", Name: "LAB", In: strings.NewReader("n\nLab two\n")}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	out := h.out.String()
	for _, want := range []string{
		"The stored pathway \"Lab\" has the same name as the pathway you are adding.\n" +
			"Replacing it removes that pathway, which was made on this laptop.\n",
		"Choose another name.\nName for this pathway [Enter for lab2.example.org]: ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if h.active() != "lab-two" || len(h.storedIDs()) != 3 {
		t.Errorf("active %q, stored %v", h.active(), h.storedIDs())
	}
}

func TestHandMadeReplacesAHandMadePathway(t *testing.T) {
	h := newHarness(t)
	h.servers(nil)
	h.logIn()
	if err := h.login(Options{SkyEndpoint: "lab.example.org", Name: "Lab"}); err != nil {
		t.Fatal(err)
	}
	h.out.Reset()
	if err := h.login(Options{SkyEndpoint: "lab2.example.org", Name: "Lab", In: strings.NewReader("yes\n")}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "Removed the pathway \"Lab\"\n") {
		t.Errorf("output:\n%s", h.out.String())
	}
	// The plain identifier is reused for the file.
	f, err := configfile.Parse([]byte(h.stored("lab")))
	if err != nil || f.Pathways[0].Sky.Endpoint != "https://lab2.example.org" {
		t.Errorf("stored = %+v, %v", f, err)
	}
}

func TestReplacingTheActivePathwayLeavesIt(t *testing.T) {
	h := newHarness(t)
	dir := axondir.New(h.home)
	files, _, _ := configfile.NewStore(dir).List()
	campus := files[0].Pathways[0]
	if err := dir.SetPathway(campus.ID); err != nil {
		t.Fatal(err)
	}
	if err := connected.Write(dir, campus); err != nil {
		t.Fatal(err)
	}
	if err := h.l.leaveRemoved([]configfile.File{files[0]}, Options{Out: &h.out}); err != nil {
		t.Fatal(err)
	}
	if same, err := connected.Is(dir, campus); err != nil || same {
		t.Errorf("the replaced pathway is still recorded as connected: %v, %v", same, err)
	}
}

// revised returns testkit.ExampleConfig with a revision.
func revised(n string) string {
	return strings.Replace(testkit.ExampleConfig, "id = \"example\"\n", "id = \"example\"\nrevision = "+n+"\n", 1)
}

func TestLowerRevisionAsks(t *testing.T) {
	for _, tc := range []struct {
		name, answer string
		replace      bool
		wantStored   string
		want         []string
	}{
		{"no", "n\n", false, "7", []string{
			"The stored copy of the config file " + "%PATH%" + " is revision 7, and this one is revision 5, which is older.\n" +
				"Replace the stored copy with revision 5? [y/N, Enter for no]: ",
			"Kept revision 7.\n",
		}},
		{"yes", "y\n", false, "5", []string{"Updated the config file %PATH% from revision 7 to revision 5\n"}},
		{"replace", "", true, "5", []string{"Updated the config file %PATH% from revision 7 to revision 5\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.store(revised("7"))
			h.logIn()
			if err := h.login(Options{Config: writeConfig(t, revised("5")), Pathway: "campus", Replace: tc.replace, In: strings.NewReader(tc.answer)}); err != nil {
				t.Fatalf("login: %v\n%s", err, h.out.String())
			}
			path := filepath.Join(h.home, ".axon", "configs", "example.toml")
			for _, want := range tc.want {
				want = strings.ReplaceAll(want, "%PATH%", path)
				if !strings.Contains(h.out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, h.out.String())
				}
			}
			if !strings.Contains(h.stored("example"), "revision = "+tc.wantStored+"\n") {
				t.Errorf("stored:\n%s", h.stored("example"))
			}
		})
	}
}

func TestLowerRevisionWithoutATerminalKeepsTheStoredCopy(t *testing.T) {
	h := newHarness(t)
	h.store(revised("7"))
	h.logIn()
	path := writeConfig(t, revised("5"))
	if err := h.login(Options{Config: path, Pathway: "campus"}); err != nil {
		t.Fatal(err)
	}
	want := "axon: the config file at " + path + " is revision 5, older than the stored revision 7. Using the stored file.\n"
	if h.errOut.String() != want {
		t.Errorf("stderr = %q, want %q", h.errOut.String(), want)
	}
	if !strings.Contains(h.stored("example"), "revision = 7\n") {
		t.Error("the stored copy was replaced")
	}
}

func TestStoredAndUpdatedLinesNameTheRevision(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	h.logIn()
	path := filepath.Join(h.home, ".axon", "configs", "example.toml")
	if err := h.login(Options{Config: writeConfig(t, revised("3")), Pathway: "campus"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "Stored the config file "+path+" (revision 3)\n") {
		t.Errorf("output:\n%s", h.out.String())
	}
	h.out.Reset()
	if err := h.login(Options{Config: writeConfig(t, testkit.ExampleConfig), Pathway: "campus"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "Updated the config file "+path+" from revision 3 to no revision\n") {
		t.Errorf("output:\n%s", h.out.String())
	}
}

func TestRefreshOfALowerRevision(t *testing.T) {
	for _, tc := range []struct {
		name, answer string
		wantStored   string
		wantErr      string
	}{
		{"no terminal", "", "7", "axon: the config file at https://example.org/config.toml is revision 5, older than the stored revision 7. Using the stored file.\n"},
		{"no", "n\n", "7", ""},
		{"yes", "y\n", "5", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.noFiles()
			srv := h.serveConfigs()
			h.storeFrom("/config.toml", revised("7"))
			srv.publish("/config.toml", revised("5"))
			h.logIn()
			if err := h.l.run(context.Background(), Options{Pathway: "campus", In: strings.NewReader(tc.answer), Out: &h.out, Err: &h.errOut}); err != nil {
				t.Fatalf("login: %v\n%s", err, h.out.String())
			}
			if h.errOut.String() != tc.wantErr {
				t.Errorf("stderr = %q, want %q", h.errOut.String(), tc.wantErr)
			}
			if !strings.Contains(h.stored("example"), "revision = "+tc.wantStored+"\n") {
				t.Errorf("stored:\n%s", h.stored("example"))
			}
		})
	}
}

func TestRefreshThatNewlyClashesIsLeftOut(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	srv := h.serveConfigs()
	h.storeFrom("/config.toml", testkit.ExampleConfig)
	h.store(otherConfig)
	srv.publish("/config.toml", strings.Replace(testkit.ExampleConfig, `name = "Remote"`, `name = "Lab"`, 1))
	h.logIn()
	if err := h.l.run(context.Background(), Options{Pathway: "campus", In: strings.NewReader(""), Out: &h.out, Err: &h.errOut}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.errOut.String(), "Using the stored file.") || strings.Contains(h.stored("example"), `"Lab"`) {
		t.Errorf("stderr = %q", h.errOut.String())
	}
}

func TestAskYesNo(t *testing.T) {
	for in, want := range map[string]bool{"y\n": true, "YES\n": true, "n\n": false, "\n": false, "maybe\ny\n": true} {
		var out strings.Builder
		got, err := askYesNo(context.Background(), strings.NewReader(in), &out, "? ")
		if err != nil || got != want {
			t.Errorf("askYesNo(%q) = %v, %v", in, got, err)
		}
	}
	var out strings.Builder
	if _, err := askYesNo(context.Background(), strings.NewReader("maybe\n"), &out, "? "); err != errNoAnswer || !strings.Contains(out.String(), "Answer y or n.") {
		t.Errorf("err = %v, out %q", err, out.String())
	}
}

// With --config -, standard input is the piped file and the questions go to
// the terminal: a clash is asked about there, not treated as no terminal.
func TestPipedConfigAsksTheClashOnTheTerminal(t *testing.T) {
	h := newHarness(t)
	tty := &closer{Reader: strings.NewReader("y\n")}
	h.l.openTerminal = func() (io.ReadCloser, error) { return tty, nil }
	h.logIn()
	if err := h.login(Options{Config: "-", In: strings.NewReader(otherCampus)}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if !strings.Contains(h.out.String(), "Replace it? [y/N, Enter for no]: ") || !tty.closed {
		t.Errorf("no question on the terminal, or it was left open:\n%s", h.out.String())
	}
	if got := h.storedIDs(); len(got) != 1 || got[0] != "other" {
		t.Errorf("stored %v, want the piped file in place of the stored one", got)
	}
}

// The same for a lower revision.
func TestPipedConfigAsksTheLowerRevisionOnTheTerminal(t *testing.T) {
	h := newHarness(t)
	h.store(revised("7"))
	tty := &closer{Reader: strings.NewReader("y\n")}
	h.l.openTerminal = func() (io.ReadCloser, error) { return tty, nil }
	h.logIn()
	if err := h.login(Options{Config: "-", Pathway: "campus", In: strings.NewReader(revised("5"))}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if !strings.Contains(h.stored("example"), "revision = 5\n") || h.errOut.Len() != 0 {
		t.Errorf("the answer on the terminal was not used; stderr %q", h.errOut.String())
	}
}

// An active pathway that cannot be read stops the replacement before
// anything is removed.
func TestReplacingWithAnUnreadableActivePathwayStops(t *testing.T) {
	h := newHarness(t)
	if err := os.WriteFile(axondir.New(h.home).SettingsPath(), []byte("pathway = [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := h.storedIDs()
	if err := h.login(Options{Config: writeConfig(t, otherCampus), Replace: true}); err == nil {
		t.Fatal("login succeeded with an unreadable active pathway")
	}
	if got := h.storedIDs(); len(got) != len(before) || got[0] != before[0] {
		t.Errorf("stored %v, want %v unchanged", got, before)
	}
}

// A file a stopped replacement left aside does not stop a login.
func TestLoginWithALeftoverRemovingFile(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(h.home, ".axon", "configs", "example.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".removing", data, 0o600); err != nil {
		t.Fatal(err)
	}
	h.logIn()
	if err := h.login(Options{Pathway: "campus"}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.errOut.String())
	}
	if h.errOut.Len() != 0 {
		t.Errorf("stderr = %q", h.errOut.String())
	}
}
