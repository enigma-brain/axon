package login

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/configfile"
	"github.com/enigma-brain/axon/internal/connected"
)

// otherConfig is a second config file, with one direct pathway, lab.
const otherConfig = `format = 2
id = "other"

[[pathway]]
id = "lab"
kind = "direct"
name = "Lab"
description = "A browser login. Works from the lab."

[pathway.sky]
endpoint = "https://lab.example.org"
`

// noFiles removes every stored config file.
func (h *harness) noFiles() {
	h.t.Helper()
	if err := os.RemoveAll(filepath.Join(h.home, ".axon", "configs")); err != nil {
		h.t.Fatal(err)
	}
}

// login runs one login with opts, its output going to the harness.
func (h *harness) login(opts Options) error {
	if opts.In == nil {
		opts.In = strings.NewReader("")
	}
	opts.Out, opts.Err = &h.out, &h.errOut
	return h.l.run(context.Background(), opts)
}

func (h *harness) active() string {
	id, _, _ := axondir.New(h.home).Pathway()
	return id
}

func TestNoConfigFileStops(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	err := h.run("")
	want := "no pathway is set up, and there is no terminal to ask on. Run `axon login --sky-endpoint <address>` for a SkyPilot API server you reach directly, `axon login --teleport-proxy <address>` for one through Teleport, or `axon login --config <source>` with the file your organization gives you."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if _, err := os.Stat(axondir.New(h.home).SettingsPath()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("settings.toml was written: %v", err)
	}
	if h.skyRuns() != 0 {
		t.Error("sky ran")
	}
	if h.out.Len() != 0 {
		t.Errorf("login wrote %q", h.out.String())
	}
}

// With the last file gone and no tunnel recorded, login still leaves the
// pathway: it removes the recorded email and the record of the connected
// pathway, then asks for --config.
func TestLastFileDeletedWithoutTunnelDisconnects(t *testing.T) {
	h := newHarness(t)
	h.logIn()
	h.script("ok")
	if err := h.run("campus"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if h.email() == "" {
		t.Fatal("no email recorded after the login")
	}
	h.noFiles()
	h.out.Reset()
	if err := h.run(""); !errors.Is(err, errNoConfig) {
		t.Fatalf("err = %v, want errNoConfig", err)
	}
	if e := h.email(); e != "" {
		t.Errorf("email %q stayed after the pathway was left", e)
	}
	if _, ok, _ := connected.Read(axondir.New(h.home)); ok {
		t.Error("the pathway is still recorded as connected")
	}
	if strings.Contains(h.out.String(), "stopping the Teleport tunnel") {
		t.Errorf("output %q stops a tunnel that was not recorded", h.out.String())
	}
	if h.active() != "campus" {
		t.Errorf("active pathway = %q, want campus left in settings.toml", h.active())
	}
}

// A laptop set up before config files has a pathway stored under an
// old name and no file: login stops with the same message.
func TestOldPathwayWithoutFileStops(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	if err := axondir.New(h.home).SetPathway("old-name"); err != nil {
		t.Fatal(err)
	}
	if err := h.run(""); !errors.Is(err, errNoConfig) {
		t.Fatalf("err = %v, want errNoConfig", err)
	}
}

func TestOnePathwayIsUsedWithoutAsking(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	h.store(otherConfig)
	h.logIn()
	if err := h.run(""); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if h.active() != "lab" {
		t.Errorf("active pathway = %q, want lab", h.active())
	}
	if strings.Contains(h.out.String(), "Which pathway") {
		t.Errorf("login asked with one pathway:\n%s", h.out.String())
	}
}

func TestTwoPathwaysInOneFileAsk(t *testing.T) {
	h := newHarness(t)
	h.logIn()
	if err := h.login(Options{In: strings.NewReader("1\n")}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	want := "Which pathway are you on?\n" +
		"  1) Campus  Single sign-on in a browser. Needs the campus network.\n" +
		"  2) Remote  Works from anywhere.\n" +
		"Pathway [1-2]: "
	if !strings.HasPrefix(h.out.String(), want) {
		t.Errorf("output:\n%s\nwant it to begin:\n%s", h.out.String(), want)
	}
	if h.active() != "campus" {
		t.Errorf("active pathway = %q, want campus", h.active())
	}
}

func TestTwoFilesAreOfferedInTheirOrder(t *testing.T) {
	h := newHarness(t)
	h.store(otherConfig)
	h.logIn()
	if err := h.login(Options{In: strings.NewReader("lab\n")}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	for _, want := range []string{"  1) Campus ", "  2) Remote ", "  3) Lab     A browser login. Works from the lab.\n"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("prompt lacks %q:\n%s", want, h.out.String())
		}
	}
	if h.active() != "lab" {
		t.Errorf("active pathway = %q, want lab", h.active())
	}
}

func TestActivePathwayIsUsedWithoutAsking(t *testing.T) {
	h := newHarness(t)
	if err := axondir.New(h.home).SetPathway("campus"); err != nil {
		t.Fatal(err)
	}
	h.logIn()
	if err := h.run(""); err != nil {
		t.Fatalf("login: %v", err)
	}
	if strings.Contains(h.out.String(), "Which pathway") {
		t.Errorf("login asked with an active pathway:\n%s", h.out.String())
	}
}

func TestActivePathwayNoFileHasIsChosenAgain(t *testing.T) {
	h := newHarness(t)
	if err := axondir.New(h.home).SetPathway("old-name"); err != nil {
		t.Fatal(err)
	}
	err := h.run("")
	want := "determine the pathway: no pathway chosen. Run `axon login --pathway <id>` with one of campus and remote."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if h.active() != "old-name" {
		t.Errorf("active pathway = %q; nothing is stored without an answer", h.active())
	}
}

func TestPathwayFlagSwitches(t *testing.T) {
	h := newHarness(t)
	h.store(otherConfig)
	h.logIn()
	if err := h.run("campus"); err != nil {
		t.Fatal(err)
	}
	if err := h.run("lab"); err != nil {
		t.Fatal(err)
	}
	if h.active() != "lab" {
		t.Errorf("active pathway = %q, want lab", h.active())
	}
	rec, ok, err := connected.Read(axondir.New(h.home))
	if err != nil || !ok || rec.ID != "lab" {
		t.Errorf("connected record = %+v, %v, %v; want lab", rec, ok, err)
	}
}

func TestUnknownPathwayIsRefused(t *testing.T) {
	h := newHarness(t)
	h.store(otherConfig)
	err := h.run("nope")
	want := `determine the pathway: no stored config file has the pathway "nope". The stored pathways are campus, remote and lab.`
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if _, ok, _ := axondir.New(h.home).Pathway(); ok {
		t.Error("a pathway was stored")
	}
	if h.skyRuns() != 0 {
		t.Error("sky ran")
	}
}

func TestStoredFileThatFailsIsLeftOutWithOneLine(t *testing.T) {
	h := newHarness(t)
	bad := filepath.Join(h.home, ".axon", "configs", "broken.toml")
	if err := os.WriteFile(bad, []byte("format = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.logIn()
	if err := h.run("campus"); err != nil {
		t.Fatal(err)
	}
	if got := h.errOut.String(); !strings.HasPrefix(got, "axon: left out "+bad+": ") || strings.Count(got, "\n") != 1 {
		t.Errorf("error output = %q, want one line naming %s", got, bad)
	}
}

func TestConfigFromPath(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	path := filepath.Join(t.TempDir(), "axon.toml")
	if err := os.WriteFile(path, []byte(otherConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	h.logIn()
	if err := h.login(Options{Config: path}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	stored := filepath.Join(h.home, ".axon", "configs", "other.toml")
	if data, _ := os.ReadFile(stored); string(data) != otherConfig {
		t.Errorf("stored file = %q", data)
	}
	if !strings.HasPrefix(h.out.String(), "Stored the config file "+stored+"\n") {
		t.Errorf("output:\n%s", h.out.String())
	}
	if h.active() != "lab" {
		t.Errorf("active pathway = %q, want lab", h.active())
	}
}

func TestConfigFromAddress(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	const address = "https://example.org/axon.toml"
	h.l.readConfig = func(_ context.Context, src configfile.Source, _ io.Reader) ([]byte, error) {
		if src.String() != address || !src.IsAddress() {
			t.Errorf("read %q", src)
		}
		return []byte(otherConfig), nil
	}
	h.logIn()
	if err := h.login(Options{Config: address}); err != nil {
		t.Fatalf("login: %v", err)
	}
	src, ok, err := configfile.NewStore(axondir.New(h.home)).Source("other")
	if err != nil || !ok || src.String() != address {
		t.Errorf("recorded source = %q, %v, %v", src, ok, err)
	}
	if h.active() != "lab" {
		t.Errorf("active pathway = %q, want lab", h.active())
	}
}

func TestConfigFromStdinWithTwoPathwaysAsksOnTheTerminal(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	tty := &closer{Reader: strings.NewReader("1\n")}
	h.l.openTerminal = func() (io.ReadCloser, error) { return tty, nil }
	h.logIn()
	if err := h.login(Options{Config: "-", In: strings.NewReader(exampleConfig)}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if h.active() != "campus" {
		t.Errorf("active pathway = %q, want campus", h.active())
	}
	if !strings.Contains(h.out.String(), "Which pathway are you on?") || !tty.closed {
		t.Errorf("no question on the terminal, or it was left open:\n%s", h.out.String())
	}
}

func TestConfigFromStdinWithoutTerminalNamesTheFlag(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	err := h.login(Options{Config: "-", In: strings.NewReader(exampleConfig)})
	want := "determine the pathway: no pathway chosen. Run `axon login --pathway <id>` with one of campus and remote."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	// The file was added; the flag answers the question.
	h.logIn()
	h.out.Reset()
	if err := h.login(Options{Config: "-", Pathway: "campus", In: strings.NewReader(exampleConfig)}); err != nil {
		t.Fatalf("login with --pathway: %v", err)
	}
	if h.active() != "campus" {
		t.Errorf("active pathway = %q, want campus", h.active())
	}
}

func TestConfigKeepsActivePathwayInTheAddedFile(t *testing.T) {
	h := newHarness(t)
	h.store(otherConfig)
	if err := axondir.New(h.home).SetPathway("remote"); err != nil {
		t.Fatal(err)
	}
	h.l.readConfig = func(context.Context, configfile.Source, io.Reader) ([]byte, error) { return []byte(otherConfig), nil }
	h.logIn()
	// The active pathway is not in the added file, which has one pathway.
	if err := h.login(Options{Config: "https://example.org/other.toml"}); err != nil {
		t.Fatal(err)
	}
	if h.active() != "lab" {
		t.Errorf("active pathway = %q, want lab, the added file's", h.active())
	}
	// Now it is: adding the file again keeps it, without asking.
	if err := h.login(Options{Config: "https://example.org/other.toml"}); err != nil {
		t.Fatal(err)
	}
	if h.active() != "lab" {
		t.Errorf("active pathway = %q, want lab", h.active())
	}

	// With several pathways in the added file and the active one in it,
	// nothing is asked.
	if err := axondir.New(h.home).SetPathway("campus"); err != nil {
		t.Fatal(err)
	}
	h.l.readConfig = func(context.Context, configfile.Source, io.Reader) ([]byte, error) { return []byte(exampleConfig), nil }
	h.out.Reset()
	if err := h.login(Options{Config: "https://example.org/example.toml"}); err != nil {
		t.Fatal(err)
	}
	if h.active() != "campus" || strings.Contains(h.out.String(), "Which pathway") {
		t.Errorf("active pathway = %q, output:\n%s", h.active(), h.out.String())
	}
}

func TestConfigOffersOnlyTheAddedFilesPathways(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	h.store(otherConfig)
	if err := axondir.New(h.home).SetPathway("lab"); err != nil {
		t.Fatal(err)
	}
	h.l.readConfig = func(context.Context, configfile.Source, io.Reader) ([]byte, error) { return []byte(exampleConfig), nil }
	err := h.login(Options{Config: "https://example.org/example.toml"})
	if err == nil || !strings.HasSuffix(err.Error(), "with one of campus and remote.") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(h.out.String(), "Lab") {
		t.Errorf("the prompt offers a pathway of another file:\n%s", h.out.String())
	}
}

func TestRefusedConfigChangesNothing(t *testing.T) {
	h := newHarness(t)
	if err := axondir.New(h.home).SetPathway("campus"); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, filepath.Join(h.home, ".axon"))
	clash := strings.Replace(otherConfig, `id = "lab"`, `id = "campus"`, 1)
	path := filepath.Join(t.TempDir(), "clash.toml")
	if err := os.WriteFile(path, []byte(clash), 0o600); err != nil {
		t.Fatal(err)
	}
	err := h.login(Options{Config: path})
	if err == nil || !strings.HasPrefix(err.Error(), "add the config file: ") || !strings.Contains(err.Error(), "example.toml") {
		t.Fatalf("err = %v, want a refusal naming the stored file", err)
	}
	if after := snapshot(t, filepath.Join(h.home, ".axon")); !equalMaps(before, after) {
		t.Errorf("the refused file changed ~/.axon:\nbefore %v\nafter  %v", before, after)
	}
}

func TestConnectedRecord(t *testing.T) {
	h := newHarness(t)
	h.logIn()
	if err := h.run("campus"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(h.home, ".axon", "state", "pathway.toml"))
	if err != nil {
		t.Fatal(err)
	}
	want := "format = 2\nid = \"campus\"\nkind = \"direct\"\nsky_endpoint = \"https://sky.example.org\"\n"
	if string(data) != want {
		t.Errorf("record = %q, want %q", data, want)
	}
}

func TestDisconnectRemovesTheEmailOnlyOnAChange(t *testing.T) {
	h := newHarness(t)
	dir := axondir.New(h.home)
	files, _, err := configfile.NewStore(dir).List()
	if err != nil {
		t.Fatal(err)
	}
	campus, remote := files[0].Pathways[0], files[0].Pathways[1]
	opts := Options{Out: io.Discard}

	if err := h.l.disconnectUnless(campus, opts); err != nil {
		t.Fatal(err)
	}
	if err := dir.SetEmail("ada@example.org"); err != nil {
		t.Fatal(err)
	}
	if err := h.l.disconnectUnless(campus, opts); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := dir.Email(); !ok {
		t.Error("the email was removed although the pathway did not change")
	}

	// The same pathway with other settings for machines is a change.
	moved := campus
	moved.Sky.Endpoint = "https://sky2.example.org"
	if err := h.l.disconnectUnless(moved, opts); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := dir.Email(); ok {
		t.Error("the email stayed after the settings changed")
	}

	// Text for people alone is not.
	if err := dir.SetEmail("ada@example.org"); err != nil {
		t.Fatal(err)
	}
	renamed := moved
	renamed.Name, renamed.Description, renamed.UnreachableHint = "Other", "Other.", "Other."
	if err := h.l.disconnectUnless(renamed, opts); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := dir.Email(); !ok {
		t.Error("a change of text alone disconnected")
	}

	if err := h.l.disconnectUnless(remote, opts); err != nil {
		t.Fatal(err)
	}
	rec, _, _ := connected.Read(dir)
	if rec != (connected.Record{Format: 2, ID: "remote", Kind: "teleport", TeleportProxy: testProxy, SkyApp: testApp}) {
		t.Errorf("record = %+v", rec)
	}
}

func TestAskPathway(t *testing.T) {
	ps := []configfile.Pathway{
		{ID: "campus", Name: "Campus", Description: "On campus."},
		{ID: "remote", Name: "Remote", Description: "From anywhere."},
	}
	cases := map[string]string{
		"1\n":            "campus",
		"2\n":            "remote",
		"campus\n":       "campus",
		" Remote \n":     "remote",
		"3\nnope\n\n1\n": "campus",
		"2":              "remote",
	}
	for in, want := range cases {
		got, err := askPathway(ps, strings.NewReader(in), io.Discard)
		if err != nil || got.ID != want {
			t.Errorf("answers %q: got %q, %v, want %q", in, got.ID, err, want)
		}
	}
	var out bytes.Buffer
	_, err := askPathway(ps, strings.NewReader("3\nnope\n"), &out)
	if err == nil || !strings.Contains(err.Error(), "--pathway") {
		t.Errorf("no valid answer: err = %v", err)
	}
	if want := `"nope" is not one of the pathways.`; !strings.Contains(out.String(), want) {
		t.Errorf("prompt lacks %q:\n%s", want, out.String())
	}
}

func TestAskPathwayLeavesLaterInput(t *testing.T) {
	ps := []configfile.Pathway{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}}
	in := strings.NewReader("1\nTOKEN\n")
	if _, err := askPathway(ps, in, io.Discard); err != nil {
		t.Fatal(err)
	}
	rest, _ := io.ReadAll(in)
	if string(rest) != "TOKEN\n" {
		t.Errorf("input after the answer = %q, want %q", rest, "TOKEN\n")
	}
}

// closer is a terminal that records being closed.
type closer struct {
	io.Reader
	closed bool
}

func (c *closer) Close() error { c.closed = true; return nil }

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
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// Empty input to --config names its source, and nothing is stored.
func TestConfigEmpty(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty.toml")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	comments := filepath.Join(t.TempDir(), "comments.toml")
	if err := os.WriteFile(comments, []byte("# axon config\n\n   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const address = "https://config.example.org/axon.toml"
	for _, tc := range []struct {
		name string
		opts Options
		want string
	}{
		{"empty standard input", Options{Config: "-", In: strings.NewReader("")}, "add the config file: standard input is empty."},
		{"blank standard input", Options{Config: "-", In: strings.NewReader("\n  \n")}, "add the config file: standard input is empty."},
		{"empty file", Options{Config: empty}, "add the config file: " + empty + " is empty."},
		{"comments only", Options{Config: comments}, "add the config file: " + comments + " is empty."},
		{"empty address", Options{Config: address}, "add the config file: " + address + " is empty."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.noFiles()
			// An address is answered by a fake; nothing is fetched.
			h.l.readConfig = func(ctx context.Context, src configfile.Source, stdin io.Reader) ([]byte, error) {
				if src.IsAddress() {
					return nil, nil
				}
				return configfile.Read(ctx, src, stdin)
			}
			err := h.login(tc.opts)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if entries, _ := os.ReadDir(filepath.Join(h.home, ".axon", "configs")); len(entries) != 0 {
				t.Errorf("stored %v", entries)
			}
		})
	}
}
