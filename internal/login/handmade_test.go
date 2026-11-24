package login

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/enigma-brain/axon/internal/apiserver"
	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/configfile"
	"github.com/enigma-brain/axon/internal/testkit"
	"github.com/enigma-brain/axon/internal/tsh"
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

func (h *harness) handMadeRecord(id string) configfile.Record {
	h.t.Helper()
	rec, ok, err := configfile.NewStore(axondir.New(h.home)).Record(id)
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
		`Stored the pathway "sky.example.org" in ` + filepath.Join(h.home, ".axon", "configs", "sky-example-org.toml"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if h.active() != "sky-example-org" {
		t.Errorf("active pathway = %q", h.active())
	}
	f, err := configfile.Parse([]byte(h.stored("sky-example-org")))
	if err != nil {
		t.Fatal(err)
	}
	p := f.Pathways[0]
	if p.Kind != configfile.KindDirect || p.Sky.Endpoint != "https://sky.example.org" || p.Name != "sky.example.org" ||
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
	f, err := configfile.Parse([]byte(h.stored("remote-lab")))
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
	if _, err := configfile.Parse([]byte(h.stored("tp-example-org"))); err != nil {
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
	if _, err := os.Stat(filepath.Join(h.home, ".axon", "configs")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(h.home, ".axon", "configs"))
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
	f, err := configfile.Parse([]byte(h.stored("second-site")))
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
	f, err := configfile.Parse([]byte(h.stored("tp-example-org-3080")))
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
	if f, err := configfile.Parse([]byte(h2.stored("tp-example-org"))); err != nil || f.Pathways[0].Sky.App != "skypilot" {
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
			if entries, _ := os.ReadDir(filepath.Join(h.home, ".axon", "configs")); len(entries) != 0 {
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
		before := testkit.Snapshot(t, filepath.Join(h.home, ".axon"))
		if err := h.login(tc.opts); err == nil || err.Error() != tc.want {
			t.Errorf("err = %v, want %q", err, tc.want)
		}
		if after := testkit.Snapshot(t, filepath.Join(h.home, ".axon")); !equalMaps(before, after) {
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
	store := configfile.NewStore(axondir.New(home))
	must := func(id string, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	if got := must(fileID(store, "2nd-site", nil)); got != "p-2nd-site" {
		t.Errorf("fileID = %q", got)
	}
	long := strings.Repeat("a", 40)
	if got := must(fileID(store, long, nil)); got != strings.Repeat("a", 32) {
		t.Errorf("fileID = %q", got)
	}
	if err := os.MkdirAll(filepath.Join(home, ".axon", "configs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path("lab"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := must(fileID(store, "lab", nil)); got != "lab-2" {
		t.Errorf("fileID of a taken id = %q", got)
	}
	if got := must(fileID(store, "lab", []configfile.File{{ID: "lab"}})); got != "lab" {
		t.Errorf("fileID of an id being removed = %q", got)
	}
}

// An error other than a missing file stops fileID, rather than trying
// one suffix after another.
func TestFileIDReturnsAnError(t *testing.T) {
	home := t.TempDir()
	store := configfile.NewStore(axondir.New(home))
	// A file in place of ~/.axon makes every path below it fail with
	// "not a directory".
	if err := os.WriteFile(filepath.Join(home, ".axon"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fileID(store, "lab", nil); err == nil {
		t.Error("fileID returned no error")
	}
}

func TestCleanProxy(t *testing.T) {
	for in, want := range map[string]string{
		"teleport.example.org.":         "teleport.example.org",
		"https://teleport.example.org/": "teleport.example.org",
		"teleport.example.org.:3080":    "teleport.example.org:3080",
	} {
		if got, err := cleanProxy(in); err != nil || got != want {
			t.Errorf("cleanProxy(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"a..b", "a.-b.org", "a b"} {
		if _, err := cleanProxy(bad); err == nil {
			t.Errorf("cleanProxy(%q) accepted it", bad)
		}
	}
}

// A hand-made pathway's answers are refused when they hold a control or
// format character, which a config file may not hold.
func TestCleanRefusesUnsafeCharacters(t *testing.T) {
	if _, err := cleanName("Lab\u202e"); err == nil || err.Error() != msgNameUnsafe {
		t.Errorf("cleanName with a format character: %v", err)
	}
	if _, err := cleanName("La\x1b[2Jb"); err == nil || err.Error() != msgNameUnsafe {
		t.Errorf("cleanName with an escape: %v", err)
	}
	if _, err := cleanEndpoint("https://sky.example.org/\u202e"); err == nil {
		t.Error("cleanEndpoint accepted a format character")
	}
	for _, bad := range []string{"https://sky.example.org/?", "https://sky.example.org/#", "https://sky%E2%80%8B.example.org", "sky.example.org/%E2%80%AE", "sky.example.org/#x", "https://sky.example.org/?a=b"} {
		if _, err := cleanEndpoint(bad); err == nil {
			t.Errorf("cleanEndpoint(%q) accepted it", bad)
		}
	}
	if got, err := cleanEndpoint("sky.example.org/api/"); err != nil || got != "https://sky.example.org/api" {
		t.Errorf("cleanEndpoint of a plain address = %q, %v", got, err)
	}
	if _, err := cleanApp("sky\u200b"); err == nil {
		t.Error("cleanApp accepted a format character")
	}
	if got, err := cleanName("Lab"); err != nil || got != "Lab" {
		t.Errorf("cleanName(%q) = %q, %v", "Lab", got, err)
	}
}

// netFailure is the error a request to address fails with when its dial
// fails with cause, wrapped as Go wraps it. Nothing is contacted.
func netFailure(address string, cause error) error {
	return &url.Error{Op: "Get", URL: address, Err: &net.OpError{Op: "dial", Net: "tcp", Err: cause}}
}

// serversFailing makes the API server at https://host and the proxy at host
// fail with err, which the API server's check reads as probeSky reads the
// health request's error. Nothing is contacted.
func (h *harness) serversFailing(host string, err error) {
	h.l.probeSky = func(ctx context.Context, endpoint string) error {
		if endpoint != "https://"+host {
			return nil
		}
		return skyProbeResult(ctx, err)
	}
	h.l.probeProxy = func(_ context.Context, proxy string) error {
		if proxy != host {
			return nil
		}
		return err
	}
}

// The reason in a can't-reach message is the short one, at the prompt and
// from the flags, for an API server and a Teleport proxy.
func TestCantReachReasonIsShort(t *testing.T) {
	const host = "sky.wrong.example"
	noHost := &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	refused := os.NewSyscallError("connect", syscall.ECONNREFUSED)
	// As apiserver.Fetch and tsh.Ping report the failure.
	sky := func(cause error) error {
		return fmt.Errorf("%w: %w", apiserver.ErrUnreachable, netFailure("https://"+host+"/api/health", cause))
	}
	proxy := func(cause error) error {
		return &tsh.UnreachableError{Proxy: host, Err: netFailure("https://"+host+"/webapi/ping", cause)}
	}
	deadline := fmt.Errorf("%w: %w", apiserver.ErrUnreachable,
		&url.Error{Op: "Get", URL: "https://" + host + "/api/health", Err: context.DeadlineExceeded})

	for _, tc := range []struct {
		name   string
		down   error
		reason string
	}{
		{"name that does not resolve", sky(noHost), "no such host"},
		{"refused connection", sky(refused), "connection refused"},
		{"timeout after connecting", deadline, "timed out"},
		{"dial timeout", sky(os.ErrDeadlineExceeded), "timed out"},
		{"proxy name that does not resolve", proxy(noHost), "no such host"},
		{"proxy refused connection", proxy(refused), "connection refused"},
		{"proxy timeout", proxy(os.ErrDeadlineExceeded), "timed out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isProxy := strings.HasPrefix(tc.name, "proxy")
			server, flags, kind := "the SkyPilot API server at https://"+host, Options{SkyEndpoint: host}, "1"
			if isProxy {
				server, flags, kind = "the Teleport proxy at "+host, Options{TeleportProxy: host}, "2"
			}

			h := newHarness(t)
			h.noFiles()
			h.serversFailing(host, tc.down)
			want := "make the pathway: can't reach " + server + ": " + tc.reason + ". Check the address and run `axon login` again."
			if err := h.login(flags); err == nil || err.Error() != want {
				t.Errorf("flags: err = %v, want %q", err, want)
			}

			h = newHarness(t)
			h.noFiles()
			h.terminal()
			h.serversFailing(host, tc.down)
			h.login(Options{In: strings.NewReader(kind + "\n" + host + "\n")})
			want = "Can't reach " + server + ": " + tc.reason + ". Check the address and try again.\n"
			if out := h.out.String(); !strings.Contains(out, want) {
				t.Errorf("prompt: output lacks %q:\n%s", want, out)
			}
		})
	}
}

// probeSky, the check the prompt and the flags make, gives the short reason
// for a port where nothing listens.
func TestProbeSkyRefused(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	err = probeSky(context.Background(), "http://"+addr)
	var pe *probeError
	if !errors.As(err, &pe) || pe.notSkyPilot || pe.reason != "connection refused" {
		t.Fatalf("probeSky = %#v, want the reason %q", err, "connection refused")
	}
}

// A config file's Teleport pathway whose proxy can't be reached names the
// proxy and the short reason.
func TestConfigProxyCantBeReached(t *testing.T) {
	h := newHarness(t)
	h.l.teleport.ensure = func(_ context.Context, proxy string) (string, error) {
		return "", &tsh.UnreachableError{Proxy: proxy, Err: netFailure("https://"+proxy+"/webapi/ping",
			&net.DNSError{Err: "no such host", Name: proxy, IsNotFound: true})}
	}
	err := h.run("remote")
	want := "establish the Teleport session: can't reach the Teleport proxy at " + testProxy + ": no such host. Check your network connection and run `axon login` again."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}

	// The pathway's unreachable_hint, when it has one, is the next step.
	h.store(strings.Replace(testkit.ExampleConfig, `description = "Works from anywhere."`,
		"description = \"Works from anywhere.\"\nunreachable_hint = \"Connect to the VPN and retry.\"", 1))
	err = h.run("remote")
	want = "establish the Teleport session: can't reach the Teleport proxy at " + testProxy + ": no such host. Connect to the VPN and retry."
	if err == nil || err.Error() != want {
		t.Fatalf("with a hint, err = %v, want %q", err, want)
	}
}

// A proxy that answers, but not with its version, keeps its full reason.
func TestProxyThatAnswersKeepsItsReason(t *testing.T) {
	const host = "tp.example.org"
	answered := fmt.Errorf("read the Teleport proxy's version at https://%s/webapi/ping: %w", host,
		errors.New("GET https://"+host+"/webapi/ping: 404 Not Found"))
	h := newHarness(t)
	h.noFiles()
	h.servers(map[string]error{host: answered})
	want := "make the pathway: can't reach the Teleport proxy at " + host + ": " + answered.Error() + ". Check the address and run `axon login` again."
	if err := h.login(Options{TeleportProxy: host}); err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}
