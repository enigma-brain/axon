package login

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/connected"
	"github.com/enigma-brain/axon/internal/tsh"
	"github.com/enigma-brain/axon/internal/tunnel"
)

// v010Record is a record of the connected pathway as axon v0.1.0 wrote it.
const v010Record = `format = 1
id = "remote"
kind = "teleport"
teleport_proxy = "teleport.example.org"
devenv_app = "skypilot"
`

// logoutRun is one run of axon logout against a harness's directory, fake
// sky and fake tsh.
type logoutRun struct {
	lo          *logout
	out, errOut bytes.Buffer
	err         error
}

// newLogout returns a logout that works on h's directory, with h's fake
// client and the fake tsh at tshPath; an empty tshPath is no tsh on PATH.
func newLogout(h *harness, tshPath string) *logout {
	return &logout{
		dir: axondir.New(h.home),
		sky: h.l.sky,
		findTsh: func() (string, error) {
			if tshPath == "" {
				return "", tsh.ErrNotFound
			}
			return tshPath, nil
		},
		tshLogout: tsh.Logout,
		newTunnel: func(stateDir, proxy, app string) *tunnel.Tunnel {
			return h.l.teleport.newTunnel(stateDir, "", proxy, app)
		},
	}
}

func (r *logoutRun) run() *logoutRun {
	r.err = r.lo.run(context.Background(), LogoutOptions{Out: &r.out, Err: &r.errOut})
	return r
}

// writeSkyConfig stores endpoint in the fake SkyPilot configuration, as
// sky api login does.
func writeSkyConfig(t *testing.T, h *harness, endpoint string) {
	t.Helper()
	path := h.l.sky.SettingsFile
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("api_server:\n  endpoint: "+endpoint+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// lastSkyArgs returns the arguments of sky's last run.
func lastSkyArgs(t *testing.T, h *harness) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.skyDir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	return lines[len(lines)-1]
}

func wantLines(t *testing.T, got string, want ...string) {
	t.Helper()
	if w := strings.Join(want, "\n") + "\n"; got != w {
		t.Errorf("output:\n%s\nwant:\n%s", got, w)
	}
}

func TestLogoutTeleport(t *testing.T) {
	h := newTeleportHarness(t)
	if err := h.run("remote"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if !h.tunnelStatus().Healthy {
		t.Fatal("the tunnel is not up after the login")
	}
	h.tshCalls()
	runs := h.skyRuns()

	r := (&logoutRun{lo: newLogout(h.harness, fakeTsh)}).run()
	if r.err != nil {
		t.Fatalf("logout: %v\n%s", r.err, r.errOut.String())
	}
	configs := filepath.Join(h.home, ".axon", "configs")
	wantLines(t, r.out.String(),
		lineSkyLoggedOut,
		"sky: stopping the Teleport tunnel on port 46581",
		"teleport: logged out of teleport.example.org",
		lineRemoved,
		"Kept the config files in "+configs+"; run `axon login` to log in again",
	)
	if h.skyRuns() != runs+1 || lastSkyArgs(t, h.harness) != "api logout" {
		t.Errorf("sky ran %d more times, last with %q; want api logout once", h.skyRuns()-runs, lastSkyArgs(t, h.harness))
	}
	if env := h.skyEnv(h.skyRuns()); env["SKYPILOT_API_SERVER_ENDPOINT"] != "" {
		t.Errorf("sky api logout ran with SKYPILOT_API_SERVER_ENDPOINT=%s", env["SKYPILOT_API_SERVER_ENDPOINT"])
	}
	assertNoPythonVars(t, h.skyEnv(h.skyRuns()))
	if calls := h.tshCalls(); !contains(calls, "logout --proxy=teleport.example.org") {
		t.Errorf("tsh calls = %q, want logout --proxy=teleport.example.org", calls)
	}
	if st := h.tunnelStatus(); st.Running {
		t.Error("the tunnel still runs")
	}
	dir := axondir.New(h.home)
	if _, ok, _ := dir.Email(); ok {
		t.Error("the email is still recorded")
	}
	if _, ok, _ := connected.Read(dir); ok {
		t.Error("the connected pathway is still recorded")
	}
	if h.active() != "remote" {
		t.Errorf("active pathway = %q, want it kept", h.active())
	}
	if _, err := os.Stat(filepath.Join(configs, "example.toml")); err != nil {
		t.Errorf("the config file is gone: %v", err)
	}
}

func TestLogoutDirect(t *testing.T) {
	h := newHarness(t)
	h.script("ok")
	if err := h.run("campus"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	writeSkyConfig(t, h, h.directURL)
	writeCookieFile(t, h)

	// A direct pathway has no Teleport session, so tsh is not looked for.
	lo := newLogout(h, "")
	lo.findTsh = func() (string, error) { return "", errors.New("tsh looked for") }
	r := (&logoutRun{lo: lo}).run()
	if r.err != nil {
		t.Fatalf("logout: %v\n%s", r.err, r.errOut.String())
	}
	wantLines(t, r.out.String(),
		lineSkyLoggedOut,
		"sky: removed the saved login for "+campusEndpoint,
		lineRemoved,
		"Kept the config files in "+filepath.Join(h.home, ".axon", "configs")+"; run `axon login` to log in again",
	)
	if lastSkyArgs(t, h) != "api logout" {
		t.Errorf("sky ran with %q", lastSkyArgs(t, h))
	}
	wantCookieFile(t, h, otherServerCookie)
}

// campusEndpoint is the endpoint the harness's config file gives the direct
// pathway, campus, and so the one axon records.
const campusEndpoint = "https://sky.example.org"

// otherServerCookie is a cookie for a server other than the harness's.
const otherServerCookie = "other.example.org\tFALSE\t/\tTRUE\t0\t_oauth2_proxy\tkeep"

// writeCookieFile saves, in SkyPilot's cookie file, a session cookie for
// campus's API server and one for another server.
func writeCookieFile(t *testing.T, h *harness) {
	t.Helper()
	lines := "# Netscape HTTP Cookie File\n" +
		"sky.example.org\tFALSE\t/\tFALSE\t0\t_oauth2_proxy\tsession\n" +
		otherServerCookie + "\n"
	if err := os.MkdirAll(filepath.Dir(h.cookieFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.cookieFile, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
}

// wantCookieFile checks that SkyPilot's cookie file holds the header and
// the given cookie lines alone.
func wantCookieFile(t *testing.T, h *harness, cookies ...string) {
	t.Helper()
	data, err := os.ReadFile(h.cookieFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Netscape HTTP Cookie File\n"
	for _, c := range cookies {
		want += c + "\n"
	}
	if string(data) != want {
		t.Errorf("cookie file:\n%s\nwant:\n%s", data, want)
	}
}

func TestLogoutRemovesTheCookieWithoutAClient(t *testing.T) {
	h := newHarness(t)
	h.script("ok")
	if err := h.run("campus"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	writeCookieFile(t, h)
	h.client.installed = false

	r := (&logoutRun{lo: newLogout(h, "")}).run()
	if r.err != nil {
		t.Fatalf("logout: %v\n%s", r.err, r.errOut.String())
	}
	wantLines(t, r.out.String(),
		lineSkyNoClient,
		"sky: removed the saved login for "+campusEndpoint,
		lineRemoved,
		"Kept the config files in "+filepath.Join(h.home, ".axon", "configs")+"; run `axon login` to log in again",
	)
	wantCookieFile(t, h, otherServerCookie)
}

func TestLogoutRemovesTheCookieWhenSkyLogoutFails(t *testing.T) {
	h := newHarness(t)
	h.script("ok", "fail")
	if err := h.run("campus"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	writeSkyConfig(t, h, h.directURL)
	writeCookieFile(t, h)

	r := (&logoutRun{lo: newLogout(h, "")}).run()
	if r.err == nil {
		t.Fatal("logout succeeded after sky api logout failed")
	}
	wantLines(t, r.out.String(),
		"sky: removed the saved login for "+campusEndpoint,
		"Kept the config files in "+filepath.Join(h.home, ".axon", "configs")+"; run `axon login` to log in again",
	)
	wantCookieFile(t, h, otherServerCookie)
}

func TestLogoutWithNoCookieFile(t *testing.T) {
	h := newHarness(t)
	h.script("ok")
	if err := h.run("campus"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	writeSkyConfig(t, h, h.directURL)

	r := (&logoutRun{lo: newLogout(h, "")}).run()
	if r.err != nil {
		t.Fatalf("logout: %v\n%s", r.err, r.errOut.String())
	}
	wantLines(t, r.out.String(),
		lineSkyLoggedOut,
		lineRemoved,
		"Kept the config files in "+filepath.Join(h.home, ".axon", "configs")+"; run `axon login` to log in again",
	)
	if _, err := os.Stat(h.cookieFile); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the cookie file exists after the logout: %v", err)
	}
}

func TestLogoutWithNoCookieForTheServer(t *testing.T) {
	h := newHarness(t)
	h.script("ok")
	if err := h.run("campus"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	writeSkyConfig(t, h, h.directURL)
	if err := os.MkdirAll(filepath.Dir(h.cookieFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.cookieFile, []byte("# Netscape HTTP Cookie File\n"+otherServerCookie+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	r := (&logoutRun{lo: newLogout(h, "")}).run()
	if r.err != nil {
		t.Fatalf("logout: %v\n%s", r.err, r.errOut.String())
	}
	wantLines(t, r.out.String(),
		lineSkyLoggedOut,
		lineRemoved,
		"Kept the config files in "+filepath.Join(h.home, ".axon", "configs")+"; run `axon login` to log in again",
	)
	wantCookieFile(t, h, otherServerCookie)
}

func TestLogoutWhenTheCookieFileCannotBeRewritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes to a read-only directory")
	}
	h := newHarness(t)
	h.script("ok")
	if err := h.run("campus"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	writeSkyConfig(t, h, h.directURL)
	writeCookieFile(t, h)
	dir := filepath.Dir(h.cookieFile)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	r := (&logoutRun{lo: newLogout(h, "")}).run()
	if r.err == nil || r.err.Error() != "logout did not finish. Run `axon logout` again." {
		t.Fatalf("err = %v", r.err)
	}
	if !strings.HasPrefix(r.errOut.String(), "axon: could not remove the saved SkyPilot login: rewrite the SkyPilot cookie file "+h.cookieFile) {
		t.Errorf("stderr = %q", r.errOut.String())
	}
	wantLines(t, r.out.String(),
		lineSkyLoggedOut,
		"Kept the config files in "+filepath.Join(h.home, ".axon", "configs")+"; run `axon login` to log in again",
	)
	// The records stay, so that logging out again retries.
	if _, ok, _ := axondir.New(h.home).Email(); !ok {
		t.Error("the email was removed after a failed step")
	}
	if _, ok, _ := connected.Read(axondir.New(h.home)); !ok {
		t.Error("the connected pathway was removed after a failed step")
	}
}

func TestLogoutSkipsSteps(t *testing.T) {
	h := newTeleportHarness(t)
	if err := h.run("remote"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	// The client is gone and tsh is not on PATH.
	h.client.installed = false
	runs := h.skyRuns()
	h.tshCalls()

	r := (&logoutRun{lo: newLogout(h.harness, "")}).run()
	if r.err != nil {
		t.Fatalf("logout: %v\n%s", r.err, r.errOut.String())
	}
	wantLines(t, r.out.String(),
		lineSkyNoClient,
		"sky: stopping the Teleport tunnel on port 46581",
		"teleport: skipped logging out of teleport.example.org: tsh is not on PATH",
		lineRemoved,
		"Kept the config files in "+filepath.Join(h.home, ".axon", "configs")+"; run `axon login` to log in again",
	)
	if h.skyRuns() != runs {
		t.Error("sky ran without a client")
	}
	if calls := h.tshCalls(); len(calls) != 0 {
		t.Errorf("tsh ran: %q", calls)
	}
}

func TestLogoutSkipsSkyWithNoStoredEndpoint(t *testing.T) {
	h := newHarness(t)
	h.script("ok")
	if err := h.run("campus"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	runs := h.skyRuns()
	r := (&logoutRun{lo: newLogout(h, "")}).run()
	if r.err != nil {
		t.Fatalf("logout: %v", r.err)
	}
	if !strings.HasPrefix(r.out.String(), lineSkyNoLogin+"\n") {
		t.Errorf("output = %q, want it to begin %q", r.out.String(), lineSkyNoLogin)
	}
	if h.skyRuns() != runs {
		t.Error("sky ran with no stored endpoint")
	}
}

func TestLogoutWithNothingConnected(t *testing.T) {
	h := newHarness(t)
	writeSkyConfig(t, h, h.directURL)
	r := (&logoutRun{lo: newLogout(h, fakeTsh)}).run()
	if r.err != nil {
		t.Fatalf("logout: %v", r.err)
	}
	wantLines(t, r.out.String(), lineNothing)
	if h.skyRuns() != 0 {
		t.Error("sky ran")
	}
}

func TestLogoutFailingStepDoesNotStopTheOthers(t *testing.T) {
	h := newTeleportHarness(t)
	if err := h.run("remote"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	h.tshCalls()
	// sky's next run, the logout, fails.
	h.script(append(strings.Split(strings.Repeat("ok,", h.skyRuns()), ",")[:h.skyRuns()], "fail")...)

	r := (&logoutRun{lo: newLogout(h.harness, fakeTsh)}).run()
	if r.err == nil || r.err.Error() != "logout did not finish. Run `axon logout` again." {
		t.Fatalf("err = %v", r.err)
	}
	if !strings.HasPrefix(r.errOut.String(), "axon: could not log out of SkyPilot: sky api logout: exit status 2: boom") {
		t.Errorf("stderr = %q", r.errOut.String())
	}
	wantLines(t, r.out.String(),
		"sky: stopping the Teleport tunnel on port 46581",
		"teleport: logged out of teleport.example.org",
		"Kept the config files in "+filepath.Join(h.home, ".axon", "configs")+"; run `axon login` to log in again",
	)
	if calls := h.tshCalls(); !contains(calls, "logout --proxy=teleport.example.org") {
		t.Errorf("tsh calls = %q", calls)
	}
	if h.tunnelStatus().Running {
		t.Error("the tunnel still runs")
	}
	// The records stay, so that logging out again retries.
	if _, ok, _ := axondir.New(h.home).Email(); !ok {
		t.Error("the email was removed after a failed step")
	}
}

func TestLogoutWithAnEarlierAxonsRecord(t *testing.T) {
	h := newTeleportHarness(t)
	if err := h.run("remote"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if err := os.WriteFile(connected.Path(axondir.New(h.home)), []byte(v010Record), 0o600); err != nil {
		t.Fatal(err)
	}
	h.tshCalls()

	r := (&logoutRun{lo: newLogout(h.harness, fakeTsh)}).run()
	if r.err != nil {
		t.Fatalf("logout: %v\n%s", r.err, r.errOut.String())
	}
	wantLines(t, r.out.String(),
		lineSkyLoggedOut,
		"sky: stopping the Teleport tunnel on port 46581",
		lineTeleportOld,
		lineRemoved,
		"Kept the config files in "+filepath.Join(h.home, ".axon", "configs")+"; run `axon login` to log in again",
	)
	if calls := h.tshCalls(); len(calls) != 0 {
		t.Errorf("tsh ran: %q", calls)
	}
	if h.tunnelStatus().Running {
		t.Error("the tunnel still runs")
	}
	if _, err := os.Stat(connected.Path(axondir.New(h.home))); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the earlier record is still there: %v", err)
	}
}

// A login over a record an earlier axon wrote treats it as no record: it
// disconnects, stopping the tunnel by its own record, and connects again.
func TestLoginWithAnEarlierAxonsRecord(t *testing.T) {
	h := newTeleportHarness(t)
	if err := h.run("remote"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	dir := axondir.New(h.home)
	if err := os.WriteFile(connected.Path(dir), []byte(v010Record), 0o600); err != nil {
		t.Fatal(err)
	}
	h.out.Reset()
	if err := h.run("remote"); err != nil {
		t.Fatalf("second login: %v\n%s", err, h.out.String())
	}
	if !strings.Contains(h.out.String(), "sky: stopping the Teleport tunnel") {
		t.Errorf("the login did not disconnect:\n%s", h.out.String())
	}
	if rec, ok, _ := connected.Read(dir); !ok || rec.SkyApp != testApp {
		t.Errorf("record = %+v, %v; want the pathway recorded again", rec, ok)
	}
	if !h.tunnelStatus().Healthy {
		t.Error("the tunnel is not up after the login")
	}
}

// An email record that cannot be read is reported, and the steps that do
// not need it still run.
func TestLogoutContinuesWhenTheEmailCannotBeRead(t *testing.T) {
	h := newTeleportHarness(t)
	if err := h.run("remote"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	h.tshCalls()
	emailPath := filepath.Join(h.home, ".axon", "state", "email")
	if err := os.Remove(emailPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(emailPath, 0o700); err != nil {
		t.Fatal(err)
	}

	r := (&logoutRun{lo: newLogout(h.harness, fakeTsh)}).run()
	if r.err == nil {
		t.Fatal("logout succeeded with an unreadable email record")
	}
	if !strings.HasPrefix(r.errOut.String(), "axon: could not read the logged-in email: ") {
		t.Errorf("stderr = %q", r.errOut.String())
	}
	if calls := h.tshCalls(); !contains(calls, "logout --proxy=teleport.example.org") {
		t.Errorf("tsh calls = %q, want the Teleport logout", calls)
	}
	if h.tunnelStatus().Running {
		t.Error("the tunnel still runs")
	}
	if _, ok, _ := connected.Read(axondir.New(h.home)); !ok {
		t.Error("the record was removed after a failed step")
	}
}

func TestLogoutStopsTunnelWithNoRecord(t *testing.T) {
	h := newTeleportHarness(t)
	if err := h.run("remote"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if !h.tunnelStatus().Healthy {
		t.Fatal("the tunnel is not up after the login")
	}
	h.removeTunnelRecord()

	r := (&logoutRun{lo: newLogout(h.harness, fakeTsh)}).run()
	if r.err != nil {
		t.Fatalf("logout: %v\n%s", r.err, r.errOut.String())
	}
	if !strings.Contains(r.out.String(), "sky: stopping the Teleport tunnel on port 46581") {
		t.Errorf("output %q does not say the tunnel stopped", r.out.String())
	}
	if st := h.tunnelStatus(); st.Running {
		t.Errorf("tunnel %+v still runs after logout", st)
	}
}
