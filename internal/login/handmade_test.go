package login

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/orgfile"
)

// servers makes the harness's server checks answer from fakes: every API
// server and proxy answers, except those given in down, which fail with
// their error. Nothing is contacted.
func (h *harness) servers(down map[string]error) {
	h.l.probeSky = func(_ context.Context, endpoint string) error { return down[endpoint] }
	h.l.probeProxy = func(_ context.Context, proxy string) error { return down[proxy] }
}

// terminal makes the harness's standard input a terminal.
func (h *harness) terminal() {
	h.l.isTerminal = func(io.Reader) bool { return true }
}

// stopAtTsh makes a teleport pathway's login stop where it needs tsh.
func (h *harness) stopAtTsh() {
	h.l.teleport.ensure = func(context.Context, string) (string, error) {
		return "", errors.New("stopped at tsh")
	}
}

func (h *harness) handMadeRecord(id string) orgfile.Record {
	h.t.Helper()
	rec, ok, err := orgfile.NewStore(axondir.New(h.home)).Record(id)
	if err != nil || !ok {
		h.t.Fatalf("record of %s: %v, %v", id, ok, err)
	}
	return rec
}

func TestPromptMakesADirectPathway(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	h.terminal()
	h.servers(nil)
	h.logIn()
	if err := h.login(Options{In: strings.NewReader("1\nsky.example.org\n\n")}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	out := h.out.String()
	for _, want := range []string{
		"No pathway is set up on this laptop yet.",
		"How do you reach the SkyPilot API server?\n  1) Directly, at its address\n  2) Through Teleport\nKind [1-2]: ",
		"SkyPilot API server address, such as https://sky.example.org: ",
		"Checking https://sky.example.org\n",
		"Name for this pathway [Enter for sky.example.org]: ",
		`Stored the pathway "sky.example.org" in ` + filepath.Join(h.home, ".axon", "orgs", "sky-example-org.toml"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if h.active() != "sky-example-org" {
		t.Errorf("active pathway = %q", h.active())
	}
	f, err := orgfile.Parse([]byte(h.stored("sky-example-org")))
	if err != nil {
		t.Fatal(err)
	}
	p := f.Pathways[0]
	if p.Kind != orgfile.KindDirect || p.Sky.Endpoint != "https://sky.example.org" || p.Name != "sky.example.org" ||
		p.Description != "Made on this laptop. Direct to https://sky.example.org." || f.HasRevision {
		t.Errorf("stored pathway = %+v", f)
	}
	if !h.handMadeRecord("sky-example-org").HandMade {
		t.Error("the file is not recorded as hand-made")
	}
}

func TestPromptMakesATeleportPathway(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	h.terminal()
	h.servers(nil)
	h.stopAtTsh()
	err := h.login(Options{In: strings.NewReader("2\nhttps://teleport.example.org/\n\nRemote lab\n")})
	if err == nil || !strings.Contains(err.Error(), "stopped at tsh") {
		t.Fatalf("login = %v, want it to stop at tsh\n%s", err, h.out.String())
	}
	out := h.out.String()
	for _, want := range []string{
		"Teleport proxy address, such as teleport.example.org: ",
		"Checking teleport.example.org\n",
		"Teleport application that serves SkyPilot [Enter for skypilot]: ",
		"Name for this pathway [Enter for teleport.example.org]: ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	f, err := orgfile.Parse([]byte(h.stored("remote-lab")))
	if err != nil {
		t.Fatal(err)
	}
	p := f.Pathways[0]
	if p.ID != "remote-lab" || p.Name != "Remote lab" || p.Teleport.Proxy != "teleport.example.org" || p.Sky.App != "skypilot" ||
		p.Description != "Made on this laptop. Through Teleport at teleport.example.org, application skypilot." {
		t.Errorf("stored pathway = %+v", p)
	}
}

func TestPromptAsksAgainUntilTheServerAnswers(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	h.terminal()
	h.servers(map[string]error{
		"https://down.example.org":  errors.New("dial tcp: no such host"),
		"https://other.example.org": &probeError{notSkyPilot: true, reason: "the health request got HTTP 404: (empty body)"},
	})
	h.logIn()
	in := "3\n1\nhttp://plain.example.org\ndown.example.org\nother.example.org\nsky.example.org\nCampus\n"
	if err := h.login(Options{In: strings.NewReader(in)}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	out := h.out.String()
	for _, want := range []string{
		`"3" is not one of the kinds.`,
		`"http://plain.example.org" is not an https address.`,
		"Can't reach the SkyPilot API server at https://down.example.org: dial tcp: no such host. Check the address and try again.\n",
		"https://other.example.org answered, but not as a SkyPilot API server (the health request got HTTP 404: (empty body)). Check the address and try again.\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "SkyPilot API server address") != 4 {
		t.Errorf("the address was not asked four times:\n%s", out)
	}
	if h.active() != "campus" {
		t.Errorf("active pathway = %q, want campus", h.active())
	}
}

func TestPromptProxyThatDoesNotAnswer(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	h.terminal()
	h.servers(map[string]error{"down.example.org": errors.New("connection refused")})
	h.stopAtTsh()
	h.login(Options{In: strings.NewReader("2\ndown.example.org\nbad proxy\ntp.example.org\n-x\nlab\n\n")})
	out := h.out.String()
	for _, want := range []string{
		"Can't reach the Teleport proxy at down.example.org: connection refused. Check the address and try again.\n",
		`"bad proxy" is not a host or host:port.`,
		`"-x" is not a Teleport application name.`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if _, err := orgfile.Parse([]byte(h.stored("tp-example-org"))); err != nil {
		t.Error(err)
	}
}

func TestPromptEndOfInput(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	h.terminal()
	h.servers(nil)
	err := h.login(Options{In: strings.NewReader("1\n")})
	want := "make the pathway: no pathway made. Run `axon login` again on a terminal, or with --sky-endpoint, --teleport-proxy or --config."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if _, err := os.Stat(filepath.Join(h.home, ".axon", "orgs")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(h.home, ".axon", "orgs"))
		if len(entries) != 0 {
			t.Errorf("something was stored: %v", entries)
		}
	}
}

func TestPromptNameWithoutALetterAsksAgain(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	h.terminal()
	h.servers(nil)
	h.logIn()
	if err := h.login(Options{In: strings.NewReader("1\nsky.example.org\n--\nMy Lab 2\n")}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "The name needs at least one letter or digit.") || h.active() != "my-lab-2" {
		t.Errorf("active = %q\n%s", h.active(), h.out.String())
	}
}

func TestSecondLoginAsksNothing(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	h.terminal()
	h.servers(nil)
	h.logIn()
	if err := h.login(Options{In: strings.NewReader("1\nsky.example.org\n\n")}); err != nil {
		t.Fatal(err)
	}
	h.out.Reset()
	if err := h.login(Options{In: strings.NewReader("")}); err != nil {
		t.Fatalf("second login: %v\n%s", err, h.out.String())
	}
	if strings.Contains(h.out.String(), "?") || h.active() != "sky-example-org" {
		t.Errorf("second login asked something, or changed pathway (%q):\n%s", h.active(), h.out.String())
	}
}

func TestStoredFileMeansNoPrompt(t *testing.T) {
	h := newHarness(t)
	h.terminal()
	h.servers(nil)
	h.logIn()
	if err := h.login(Options{Pathway: "campus"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.out.String(), "No pathway is set up") {
		t.Errorf("the prompt appeared with a stored file:\n%s", h.out.String())
	}
}

func TestFlagsMakeADirectPathway(t *testing.T) {
	h := newHarness(t)
	h.servers(nil)
	h.logIn()
	// A config file is stored: the flags work all the same.
	if err := h.login(Options{SkyEndpoint: "https://sky2.example.org/", Name: "Second site"}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if h.active() != "second-site" {
		t.Errorf("active pathway = %q", h.active())
	}
	f, err := orgfile.Parse([]byte(h.stored("second-site")))
	if err != nil || f.Pathways[0].Sky.Endpoint != "https://sky2.example.org" {
		t.Errorf("stored = %+v, %v", f, err)
	}
	if strings.Contains(h.out.String(), "?") {
		t.Errorf("flags asked a question:\n%s", h.out.String())
	}
}

func TestFlagsMakeATeleportPathway(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	h.servers(nil)
	h.stopAtTsh()
	h.login(Options{TeleportProxy: "tp.example.org:3080", SkyApp: "sky"})
	f, err := orgfile.Parse([]byte(h.stored("tp-example-org-3080")))
	if err != nil {
		t.Fatal(err)
	}
	p := f.Pathways[0]
	if p.Name != "tp.example.org:3080" || p.Teleport.Proxy != "tp.example.org:3080" || p.Sky.App != "sky" {
		t.Errorf("stored pathway = %+v", p)
	}
	// The default application.
	h2 := newHarness(t)
	h2.noFiles()
	h2.servers(nil)
	h2.stopAtTsh()
	h2.login(Options{TeleportProxy: "tp.example.org"})
	if f, err := orgfile.Parse([]byte(h2.stored("tp-example-org"))); err != nil || f.Pathways[0].Sky.App != "skypilot" {
		t.Errorf("stored = %+v, %v", f, err)
	}
}

func TestFlagsWithAServerThatDoesNotAnswer(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
		down error
		want string
	}{
		{"unreachable", Options{SkyEndpoint: "sky.example.org"}, errors.New("timeout"),
			"make the pathway: can't reach the SkyPilot API server at https://sky.example.org: timeout. Check the address and run `axon login` again."},
		{"not SkyPilot", Options{SkyEndpoint: "sky.example.org"}, &probeError{notSkyPilot: true, reason: "HTTP 404"},
			"make the pathway: https://sky.example.org answered, but not as a SkyPilot API server (HTTP 404). Check the address and run `axon login` again."},
		{"proxy", Options{TeleportProxy: "sky.example.org"}, errors.New("timeout"),
			"make the pathway: can't reach the Teleport proxy at sky.example.org: timeout. Check the address and run `axon login` again."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.noFiles()
			h.servers(map[string]error{"https://sky.example.org": tc.down, "sky.example.org": tc.down})
			err := h.login(tc.opts)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if entries, _ := os.ReadDir(filepath.Join(h.home, ".axon", "orgs")); len(entries) != 0 {
				t.Errorf("stored %v", entries)
			}
		})
	}
}

func TestFlagValuesAreChecked(t *testing.T) {
	for _, tc := range []struct {
		opts Options
		want string
	}{
		{Options{SkyEndpoint: "http://sky.example.org"}, `make the pathway: --sky-endpoint: "http://sky.example.org" is not an https address.`},
		{Options{TeleportProxy: "a b"}, `make the pathway: --teleport-proxy: "a b" is not a host or host:port.`},
		{Options{TeleportProxy: "tp.example.org", SkyApp: "-x"}, `make the pathway: --sky-app: "-x" is not a Teleport application name.`},
		{Options{SkyEndpoint: "sky.example.org", Name: "!!"}, `make the pathway: --name: The name needs at least one letter or digit.`},
	} {
		h := newHarness(t)
		h.servers(nil)
		if err := h.login(tc.opts); err == nil || err.Error() != tc.want {
			t.Errorf("err = %v, want %q", err, tc.want)
		}
	}
}

func TestContradictingFlags(t *testing.T) {
	for _, tc := range []struct {
		opts Options
		want string
	}{
		{Options{SkyEndpoint: "a", TeleportProxy: "b"}, "--sky-endpoint and --teleport-proxy can't be used together: --sky-endpoint is for a server you reach directly, --teleport-proxy for one through Teleport."},
		{Options{SkyApp: "a"}, "--sky-app needs --teleport-proxy."},
		{Options{SkyEndpoint: "a", SkyApp: "b"}, "--sky-app needs --teleport-proxy."},
		{Options{Name: "a"}, "--name needs --sky-endpoint or --teleport-proxy."},
		{Options{Config: "-", TeleportProxy: "a"}, "--config can't be used with --teleport-proxy: --config adds a config file, and --teleport-proxy makes a pathway without one."},
		{Options{Pathway: "campus", SkyEndpoint: "a"}, "--pathway can't be used with --sky-endpoint: --pathway picks a stored pathway, and --sky-endpoint makes a new one."},
	} {
		h := newHarness(t)
		before := snapshot(t, filepath.Join(h.home, ".axon"))
		if err := h.login(tc.opts); err == nil || err.Error() != tc.want {
			t.Errorf("err = %v, want %q", err, tc.want)
		}
		if after := snapshot(t, filepath.Join(h.home, ".axon")); !equalMaps(before, after) {
			t.Errorf("refused flags changed ~/.axon")
		}
	}
}

func TestIdentifier(t *testing.T) {
	for name, want := range map[string]string{
		"Campus":          "campus",
		"sky.example.org": "sky-example-org",
		"  My  Lab (2) ":  "my-lab-2",
		"Zürich":          "z-rich",
		"--":              "",
	} {
		if got := identifier(name); got != want {
			t.Errorf("identifier(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestFileID(t *testing.T) {
	home := t.TempDir()
	store := orgfile.NewStore(axondir.New(home))
	if got := fileID(store, "2nd-site", nil); got != "p-2nd-site" {
		t.Errorf("fileID = %q", got)
	}
	long := strings.Repeat("a", 40)
	if got := fileID(store, long, nil); got != strings.Repeat("a", 32) {
		t.Errorf("fileID = %q", got)
	}
	if err := os.MkdirAll(filepath.Join(home, ".axon", "orgs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path("lab"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := fileID(store, "lab", nil); got != "lab-2" {
		t.Errorf("fileID of a taken id = %q", got)
	}
	if got := fileID(store, "lab", []string{"lab"}); got != "lab" {
		t.Errorf("fileID of an id being removed = %q", got)
	}
}
