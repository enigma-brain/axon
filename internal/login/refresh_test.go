package login

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/configfile"
	"github.com/enigma-brain/axon/internal/connected"
	"github.com/enigma-brain/axon/internal/testkit"
)

// configServer is a local TLS server that publishes config files, one
// per path, and counts the requests for each.
type configServer struct {
	srv      *httptest.Server
	mu       sync.Mutex
	files    map[string]string
	status   map[string]int
	requests map[string]int
}

// serveConfigs starts an configServer and points the harness's reads of an
// https address at it: https://example.org/<path> is the server's <path>.
// Every other source is read as axon reads it.
func (h *harness) serveConfigs() *configServer {
	h.t.Helper()
	s := &configServer{files: map[string]string{}, status: map[string]int{}, requests: map[string]int{}}
	s.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.requests[r.URL.Path]++
		if code := s.status[r.URL.Path]; code != 0 {
			w.WriteHeader(code)
			return
		}
		data, ok := s.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, data)
	}))
	h.t.Cleanup(s.srv.Close)

	h.l.readConfig = func(ctx context.Context, src configfile.Source, stdin io.Reader) ([]byte, error) {
		if !src.IsAddress() {
			return configfile.Read(ctx, src, stdin)
		}
		u, err := url.Parse(src.String())
		if err != nil || u.Host != "example.org" {
			h.t.Errorf("fetch of %q, which is not a test address", src)
			return nil, fmt.Errorf("fetch %s: not a test address", src)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.srv.URL+u.Path, nil)
		if err != nil {
			return nil, err
		}
		resp, err := s.srv.Client().Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", src, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch %s: the server answered %s", src, resp.Status)
		}
		return io.ReadAll(resp.Body)
	}
	return s
}

// publish makes the server answer path with data.
func (s *configServer) publish(path, data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[path] = data
	delete(s.status, path)
}

// fail makes the server answer path with code.
func (s *configServer) fail(path string, code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status[path] = code
}

// count returns how many times path was requested.
func (s *configServer) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[path]
}

// total returns how many requests the server had.
func (s *configServer) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.requests {
		n += c
	}
	return n
}

// storeFrom adds data as a config file whose source is the test
// address of path.
func (h *harness) storeFrom(path, data string) {
	h.t.Helper()
	src, err := configfile.ParseSource("https://example.org"+path, "")
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := configfile.NewStore(axondir.New(h.home)).Put([]byte(data), configfile.PutOptions{Origin: configfile.FromSource(src)}); err != nil {
		h.t.Fatal(err)
	}
}

// stored returns the stored file id.
func (h *harness) stored(id string) string {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.home, ".axon", "configs", id+".toml"))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(data)
}

func (h *harness) record() connected.Record {
	h.t.Helper()
	rec, _, err := connected.Read(axondir.New(h.home))
	if err != nil {
		h.t.Fatal(err)
	}
	return rec
}

// connectedTo logs in on id and forgets the output and the sky runs.
func (h *harness) connectedTo(id string) {
	h.t.Helper()
	h.logIn()
	if err := h.run(id); err != nil {
		h.t.Fatalf("login on %s: %v\n%s", id, err, h.out.String())
	}
	h.out.Reset()
	h.errOut.Reset()
	os.Remove(filepath.Join(h.skyDir, "count"))
}

// emailFile returns the file that records the logged-in email. A disconnect
// removes it, so the file after a login that disconnected is another file
// even when it holds the same email.
//
// It also links the file from a directory of the test's, so that the file
// outlives its removal for the rest of the test: a file written after it
// cannot reuse its inode, and os.SameFile tells the two apart.
func (h *harness) emailFile() os.FileInfo {
	h.t.Helper()
	path := filepath.Join(h.home, ".axon", "state", "email")
	info, err := os.Stat(path)
	if err != nil {
		h.t.Fatal(err)
	}
	if err := os.Link(path, filepath.Join(h.t.TempDir(), "email")); err != nil {
		h.t.Fatal(err)
	}
	return info
}

// oneCampus is a config file with the single direct pathway campus.
const oneCampus = `format = 2
id = "example"

[[pathway]]
id = "campus"
kind = "direct"
name = "Campus"
description = "Single sign-on in a browser."

[pathway.sky]
endpoint = "https://sky.example.org"
`

func TestRefreshReplacesTheFileWithoutDisconnectingOnText(t *testing.T) {
	h := newHarness(t)
	server := h.serveConfigs()
	h.storeFrom("/example.toml", testkit.ExampleConfig)
	server.publish("/example.toml", testkit.ExampleConfig)
	h.connectedTo("campus")
	before := h.record()
	email := h.emailFile()

	renamed := strings.Replace(testkit.ExampleConfig, `name = "Campus"`, `name = "Campus SSO"`, 1)
	renamed = strings.Replace(renamed, "Connect to the campus VPN and retry.", "Use the VPN.", 1)
	server.publish("/example.toml", renamed)
	if err := h.run(""); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if got := h.stored("example"); got != renamed {
		t.Errorf("stored file = %q, want the fetched one", got)
	}
	path := filepath.Join(h.home, ".axon", "configs", "example.toml")
	if !strings.Contains(h.out.String(), "Updated the config file "+path+"\n") {
		t.Errorf("output lacks the update line:\n%s", h.out.String())
	}
	if !os.SameFile(email, h.emailFile()) {
		t.Error("a change of text alone disconnected")
	}
	if h.record() != before {
		t.Errorf("record = %+v, want %+v", h.record(), before)
	}
	if h.skyRuns() != 0 {
		t.Error("sky ran after a change of text alone")
	}
	if server.count("/example.toml") != 2 {
		t.Errorf("fetched %d times in two logins, want 2", server.count("/example.toml"))
	}
}

func TestRefreshOfTheSameFileSaysNothing(t *testing.T) {
	h := newHarness(t)
	server := h.serveConfigs()
	h.storeFrom("/example.toml", testkit.ExampleConfig)
	server.publish("/example.toml", testkit.ExampleConfig)
	h.connectedTo("campus")

	if err := h.run(""); err != nil {
		t.Fatal(err)
	}
	if got := h.out.String(); got != "sky: connected as ada@example.org (SkyPilot 0.13.0)\n" {
		t.Errorf("output = %q", got)
	}
	if h.errOut.Len() != 0 {
		t.Errorf("warnings = %q", h.errOut.String())
	}
}

func TestRefreshFailureUsesTheStoredFile(t *testing.T) {
	path := func(h *harness) string { return filepath.Join(h.home, ".axon", "configs", "example.toml") }
	for _, tc := range []struct {
		name   string
		serve  func(*configServer)
		reason string
	}{
		{"unreachable", func(s *configServer) { s.fail("/example.toml", http.StatusInternalServerError) }, "the server answered 500 Internal Server Error"},
		{"refused", func(s *configServer) { s.publish("/example.toml", "format = 3\n") }, "format"},
		{"another id", func(s *configServer) {
			s.publish("/example.toml", strings.Replace(testkit.ExampleConfig, `id = "example"`, `id = "elsewhere"`, 1))
		}, `id: is "elsewhere", not "example"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			server := h.serveConfigs()
			h.storeFrom("/example.toml", testkit.ExampleConfig)
			h.connectedTo("campus")
			tc.serve(server)

			if err := h.run(""); err != nil {
				t.Fatalf("login: %v\n%s", err, h.out.String())
			}
			lines := strings.Split(strings.TrimSuffix(h.errOut.String(), "\n"), "\n")
			prefix := "axon: could not update " + path(h) + " from https://example.org/example.toml: "
			if len(lines) != 1 || !strings.HasPrefix(lines[0], prefix) ||
				!strings.Contains(lines[0], tc.reason) || !strings.HasSuffix(lines[0], ". Using the stored file.") {
				t.Errorf("warnings = %q, want one line beginning %q and naming %q", h.errOut.String(), prefix, tc.reason)
			}
			if h.stored("example") != testkit.ExampleConfig {
				t.Error("the stored file changed")
			}
			if h.active() != "campus" || h.email() != h.user {
				t.Errorf("active = %q, email = %q; want the login kept", h.active(), h.email())
			}
		})
	}
}

func TestRefreshFetchesOnlyThePathwaysFile(t *testing.T) {
	h := newHarness(t)
	server := h.serveConfigs()
	h.storeFrom("/example.toml", testkit.ExampleConfig)
	h.storeFrom("/other.toml", otherConfig)
	server.publish("/example.toml", testkit.ExampleConfig)
	server.publish("/other.toml", otherConfig)
	h.connectedTo("lab")

	if got := server.count("/other.toml"); got != 1 {
		t.Errorf("the pathway's file was fetched %d times, want 1", got)
	}
	if got := server.count("/example.toml"); got != 0 {
		t.Errorf("a file the login does not use was fetched %d times", got)
	}
}

func TestRefreshSkipsFilesNotFromAnAddress(t *testing.T) {
	h := newHarness(t)
	server := h.serveConfigs()
	// The harness stored testkit.ExampleConfig from standard input. A file from a
	// path, and a file with no recorded source, are not fetched either.
	file := filepath.Join(t.TempDir(), "other.toml")
	if err := os.WriteFile(file, []byte(otherConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	h.connectedTo("campus")
	if err := h.login(Options{Config: file, Pathway: "lab"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(h.home, ".axon", "state", "configs", "other.toml")); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"campus", "lab", ""} {
		if err := h.run(id); err != nil {
			t.Fatalf("login on %q: %v", id, err)
		}
	}
	if server.total() != 0 {
		t.Errorf("%d fetches", server.total())
	}
	if h.errOut.Len() != 0 {
		t.Errorf("warnings = %q", h.errOut.String())
	}
}

func TestConfigAddedIsNotFetchedAgain(t *testing.T) {
	h := newHarness(t)
	server := h.serveConfigs()
	server.publish("/other.toml", otherConfig)
	h.logIn()
	if err := h.login(Options{Config: "https://example.org/other.toml"}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if got := server.count("/other.toml"); got != 1 {
		t.Errorf("fetched %d times, want once, by --config", got)
	}
	if h.active() != "lab" {
		t.Errorf("active = %q, want lab", h.active())
	}
}

// dropCampus is testkit.ExampleConfig without the pathway campus.
var dropCampus = strings.Replace(testkit.ExampleConfig, testkit.ExampleConfig[strings.Index(testkit.ExampleConfig, "[[pathway]]\nid = \"campus\""):strings.Index(testkit.ExampleConfig, "[[pathway]]\nid = \"remote\"")], "", 1)

func TestDroppedPathwayNamedByFlagStops(t *testing.T) {
	h := newHarness(t)
	server := h.serveConfigs()
	h.storeFrom("/example.toml", testkit.ExampleConfig)
	h.store(otherConfig)
	h.connectedTo("campus")
	before := h.record()
	server.publish("/example.toml", dropCampus)

	err := h.login(Options{Pathway: "campus", Replace: true})
	want := `determine the pathway: the pathway "campus" is no longer in its config file. The stored pathways are remote and lab.`
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if h.stored("example") != dropCampus {
		t.Error("the fetched file was not stored")
	}
	if h.active() != "campus" || h.record() != before || h.email() != h.user {
		t.Errorf("active = %q, record = %+v, email = %q; a failed login changed them", h.active(), h.record(), h.email())
	}
}

func TestDroppedPathwayWithOneLeftIsReplacedWithoutAsking(t *testing.T) {
	h := newHarness(t)
	h.noFiles()
	server := h.serveConfigs()
	h.storeFrom("/example.toml", oneCampus)
	h.connectedTo("campus")
	h.terminal()
	server.publish("/example.toml", strings.Replace(oneCampus, `id = "campus"`, `id = "campus-sso"`, 1))

	if err := h.login(Options{In: strings.NewReader("y\n")}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	asked := "The config file at https://example.org/example.toml changes where the pathway \"campus\" goes:\n" +
		"  now: direct, https://sky.example.org\n" +
		"  new: no longer in the file\n" +
		"Switch to the fetched config file? [y/N, Enter for no]: "
	if !strings.Contains(h.out.String(), asked) {
		t.Errorf("output lacks the question:\n%s", h.out.String())
	}
	if !strings.Contains(h.out.String(), "The pathway \"campus\" is no longer in its config file. Using the pathway \"campus-sso\".\n") {
		t.Errorf("output lacks the line naming the new pathway:\n%s", h.out.String())
	}
	if strings.Contains(h.out.String(), "Which pathway") {
		t.Error("login asked for a pathway with one pathway left")
	}
	if h.active() != "campus-sso" || h.record().ID != "campus-sso" {
		t.Errorf("active = %q, record = %+v; want campus-sso", h.active(), h.record())
	}
	if server.count("/example.toml") != 2 {
		t.Errorf("fetched %d times in two logins; choosing again fetches nothing", server.count("/example.toml"))
	}
}

func TestDroppedPathwayWithSeveralLeftAsks(t *testing.T) {
	h := newHarness(t)
	server := h.serveConfigs()
	h.storeFrom("/example.toml", testkit.ExampleConfig)
	h.storeFrom("/other.toml", otherConfig)
	server.publish("/other.toml", otherConfig)
	h.connectedTo("campus")
	h.terminal()
	server.publish("/example.toml", dropCampus)

	if err := h.login(Options{In: strings.NewReader("y\nlab\n")}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	want := "The pathway \"campus\" is no longer in its config file.\nWhich pathway are you on?\n" +
		"  1) Remote  Works from anywhere.\n" +
		"  2) Lab     A browser login. Works from the lab.\n"
	if !strings.Contains(h.out.String(), want) {
		t.Errorf("output:\n%s\nwant it to contain:\n%s", h.out.String(), want)
	}
	if h.active() != "lab" || h.record().ID != "lab" {
		t.Errorf("active = %q, record = %+v; want lab", h.active(), h.record())
	}
	if got := server.count("/other.toml"); got != 0 {
		t.Errorf("the newly chosen pathway's file was fetched %d times", got)
	}
}

func TestDroppedPathwayWithoutAnAnswerChangesNothing(t *testing.T) {
	h := newHarness(t)
	server := h.serveConfigs()
	h.storeFrom("/example.toml", testkit.ExampleConfig)
	h.store(otherConfig)
	h.connectedTo("campus")
	h.terminal()
	before := h.record()
	server.publish("/example.toml", dropCampus)

	err := h.login(Options{In: strings.NewReader("y\n")})
	want := "determine the pathway: no pathway chosen. Run `axon login --pathway <id>` with one of remote and lab."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if h.active() != "campus" || h.record() != before || h.email() != h.user {
		t.Errorf("active = %q, record = %+v, email = %q; a failed login changed them", h.active(), h.record(), h.email())
	}
}

func TestRefreshedSettingsReconnectDirectPathway(t *testing.T) {
	h := newHarness(t)
	server := h.serveConfigs()
	h.storeFrom("/example.toml", testkit.ExampleConfig)
	h.connectedTo("campus")
	before := h.emailFile()
	server.publish("/example.toml", strings.Replace(testkit.ExampleConfig, "https://sky.example.org", "https://sky2.example.org", 1))

	if err := h.login(Options{Replace: true}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if h.active() != "campus" {
		t.Errorf("active = %q, want campus kept", h.active())
	}
	if got := h.record().SkyEndpoint; got != "https://sky2.example.org" {
		t.Errorf("recorded endpoint = %q, want the new one", got)
	}
	if os.SameFile(before, h.emailFile()) {
		t.Error("the login on the old settings was kept")
	}
}

func TestActiveFileThatFailsIsLeft(t *testing.T) {
	h := newHarness(t)
	h.store(otherConfig)
	h.connectedTo("campus")
	before := h.emailFile()
	path := filepath.Join(h.home, ".axon", "configs", "example.toml")
	if err := os.WriteFile(path, []byte("format = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := h.run(""); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if !strings.HasPrefix(h.errOut.String(), "axon: left out "+path+": ") || strings.Count(h.errOut.String(), "\n") != 1 {
		t.Errorf("warnings = %q, want one line naming %s", h.errOut.String(), path)
	}
	if h.active() != "lab" || h.record().ID != "lab" {
		t.Errorf("active = %q, record = %+v; want lab", h.active(), h.record())
	}
	if os.SameFile(before, h.emailFile()) {
		t.Error("leaving campus kept its login")
	}
}

func TestRefreshedSettingsRestartTheTunnel(t *testing.T) {
	h := newTeleportHarness(t)
	server := h.serveConfigs()
	h.noFiles()
	h.storeFrom("/example.toml", testkit.ExampleConfig)
	server.publish("/example.toml", testkit.ExampleConfig)
	if err := h.run("remote"); err != nil {
		t.Fatalf("teleport login: %v\n%s", err, h.out.String())
	}
	old := h.tunnelStatus()
	if !old.Running {
		t.Fatal("no tunnel after the teleport login")
	}
	h.tshCalls()

	server.publish("/example.toml", strings.Replace(testkit.ExampleConfig, `app = "skypilot"`, `app = "skypilot2"`, 1))
	stateDir, err := axondir.New(h.home).StateDir()
	if err != nil {
		t.Fatal(err)
	}
	moved := h.l.teleport.newTunnel(stateDir, fakeTsh, testProxy, "skypilot2")
	t.Cleanup(func() { moved.Stop() })
	h.terminal()

	if err := h.login(Options{In: strings.NewReader("yes\n")}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if !strings.Contains(h.out.String(), "sky: stopping the Teleport tunnel") {
		t.Errorf("output does not say the old tunnel stopped:\n%s", h.out.String())
	}
	if st := h.tunnelStatus(); st.Running {
		t.Errorf("the tunnel to the old application %+v still runs", st)
	}
	st, err := moved.Status(context.Background())
	if err != nil || !st.Healthy {
		t.Errorf("tunnel to the new application = %+v, %v; want it serving", st, err)
	}
	if want := fmt.Sprintf("--proxy=%s proxy app skypilot2 --port %d", testProxy, testPort); !contains(h.tshCalls(), want) {
		t.Errorf("tsh did not run %q", want)
	}
	if h.active() != "remote" || h.record().SkyApp != "skypilot2" {
		t.Errorf("active = %q, record = %+v; want remote with skypilot2", h.active(), h.record())
	}
}

// An empty answer to the fetch names the address as --config does, without
// the query string that can hold a token, and the login goes on with the
// stored file.
func TestRefreshOfAnEmptyFileNamesTheAddress(t *testing.T) {
	h := newHarness(t)
	server := h.serveConfigs()
	h.storeFrom("/example.toml?token=s3cret", testkit.ExampleConfig)
	h.connectedTo("campus")
	server.publish("/example.toml", "# nothing but a comment\n")

	if err := h.run(""); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	path := filepath.Join(h.home, ".axon", "configs", "example.toml")
	want := "axon: could not update " + path + ": https://example.org/example.toml is empty. Using the stored file.\n"
	if got := h.errOut.String(); got != want {
		t.Errorf("warnings = %q, want %q", got, want)
	}
	if h.stored("example") != testkit.ExampleConfig {
		t.Error("the stored file changed")
	}
}

// A failed fetch names the address without its query string.
func TestRefreshFailureHidesTheToken(t *testing.T) {
	h := newHarness(t)
	server := h.serveConfigs()
	h.storeFrom("/example.toml?token=s3cret", testkit.ExampleConfig)
	h.connectedTo("campus")
	server.fail("/example.toml", http.StatusInternalServerError)

	if err := h.run(""); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	got := h.errOut.String()
	if !strings.Contains(got, " from https://example.org/example.toml: ") || strings.Contains(got, "s3cret") {
		t.Errorf("warnings = %q, want the address without its query string", got)
	}
}
