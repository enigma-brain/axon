package sky

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/configfile"
	"github.com/enigma-brain/axon/internal/connected"
	"github.com/enigma-brain/axon/internal/fail"
	"github.com/enigma-brain/axon/internal/skyclient"
)

const fakeSky = "/opt/axon-test/sky"

// call is one captured replacement of the process.
type call struct {
	path string
	argv []string
	env  []string
}

// clearSkyPilotEnv unsets every SKYPILOT_ variable the test inherits, for
// the length of the test, so that assertions see only what the test sets.
func clearSkyPilotEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "SKYPILOT_") {
			t.Setenv(k, "")
			os.Unsetenv(k)
		}
	}
}

// exampleConfig is the config file the tests store: a direct pathway,
// campus, and a teleport pathway, remote. Every detail is made up.
const exampleConfig = `format = 2
id = "example"

[[pathway]]
id = "campus"
kind = "direct"
name = "Campus"
description = "Single sign-on in a browser."
unreachable_hint = "Connect to the campus VPN and retry."

[pathway.sky]
endpoint = "https://sky.example.org"

[[pathway]]
id = "remote"
kind = "teleport"
name = "Remote"
description = "Works from anywhere."

[pathway.teleport]
proxy = "teleport.example.org"

[pathway.sky]
app = "skypilot"
`

// pathways are the identifiers of exampleConfig's pathways, with the endpoint
// the client uses on each.
var pathways = map[string]string{
	"campus": "https://sky.example.org",
	"remote": "http://127.0.0.1:46581",
}

// storeConfig stores exampleConfig under home.
func storeConfig(t *testing.T, home string) {
	t.Helper()
	src, err := configfile.ParseSource("-")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configfile.NewStore(axondir.New(home)).Put([]byte(exampleConfig), configfile.PutOptions{Origin: configfile.FromSource(src)}); err != nil {
		t.Fatal(err)
	}
}

// pathwayOf returns the stored pathway id.
func pathwayOf(t *testing.T, id string) configfile.Pathway {
	t.Helper()
	f, err := configfile.Parse([]byte(exampleConfig))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range f.Pathways {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("no pathway %q", id)
	return configfile.Pathway{}
}

// login sets HOME to a new directory, stores exampleConfig, and records a
// login on the pathway id.
func login(t *testing.T, id string, email string) axondir.Dir {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	storeConfig(t, home)
	dir := axondir.New(home)
	if err := dir.SetPathway(id); err != nil {
		t.Fatal(err)
	}
	if err := connected.Write(dir, pathwayOf(t, id)); err != nil {
		t.Fatal(err)
	}
	if err := dir.SetEmail(email); err != nil {
		t.Fatal(err)
	}
	return dir
}

// capture replaces the sky resolver and the process replacement for one
// test, and returns the calls made.
func capture(t *testing.T) *[]call {
	t.Helper()
	var calls []call
	oldPath, oldExec, oldCheck := skyPath, execSky, check
	check = connectedProbes()
	skyPath = func(axondir.Dir) (string, error) { return fakeSky, nil }
	execSky = func(path string, argv, env []string) error {
		calls = append(calls, call{path, slices.Clone(argv), slices.Clone(env)})
		return nil
	}
	t.Cleanup(func() { skyPath, execSky, check = oldPath, oldExec, oldCheck })
	return &calls
}

// runSky runs axon sky with args and returns its standard output, its
// standard error and its error.
func runSky(args ...string) (string, string, error) {
	cmd := Command()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	// A nil slice makes cobra read the test binary's own arguments.
	cmd.SetArgs(append([]string{}, args...))
	err := cmd.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

func TestForwardsEveryCommand(t *testing.T) {
	login(t, "campus", "ada@example.org")
	for _, args := range [][]string{
		{"launch", "-c", "dev", "task.yaml"},
		{"status", "--refresh"},
		{"jobs", "queue", "-u"},
		{"a-command-sky-adds-later", "--some-flag-axon-does-not-know", "x"},
		{"api", "info"},
		{"api", "status"},
		{"api", "logs", "-l", "login"},
		{"exec", "dev", "--", "--help"},
		{"-c", "dev", "launch"},
	} {
		calls := capture(t)
		if _, _, err := runSky(args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if len(*calls) != 1 {
			t.Fatalf("%v: sky ran %d times", args, len(*calls))
		}
		got := (*calls)[0]
		want := append([]string{fakeSky}, args...)
		if got.path != fakeSky || !slices.Equal(got.argv, want) {
			t.Errorf("%v: ran %s %q, want %q", args, got.path, got.argv, want)
		}
	}
}

func TestEnvironment(t *testing.T) {
	for p, endpoint := range pathways {
		t.Run(p, func(t *testing.T) {
			clearSkyPilotEnv(t)
			login(t, p, "ada@example.org")
			t.Setenv(endpointVar, "http://shell.example")
			t.Setenv(userVar, "shell-user")
			t.Setenv("SKYPILOT_DEBUG", "1")
			calls := capture(t)
			if _, _, err := runSky("status"); err != nil {
				t.Fatal(err)
			}
			env := (*calls)[0].env
			var sky []string
			for _, kv := range env {
				if strings.HasPrefix(kv, "SKYPILOT_") {
					sky = append(sky, kv)
				}
			}
			slices.Sort(sky)
			want := []string{
				endpointVar + "=" + endpoint,
				"SKYPILOT_DEBUG=1",
				userVar + "=ada@example.org",
			}
			if !slices.Equal(sky, want) {
				t.Errorf("SkyPilot variables = %q, want %q", sky, want)
			}
			if !slices.Contains(env, "HOME="+os.Getenv("HOME")) {
				t.Errorf("HOME was not passed on")
			}
		})
	}
}

// The user's Python settings do not reach the client, whether it runs a
// forwarded command or its help.
func TestPythonVarsDoNotReachTheClient(t *testing.T) {
	clearSkyPilotEnv(t)
	login(t, "campus", "ada@example.org")
	pythonVars := map[string]string{
		"PYTHONPATH":   "/elsewhere/lib",
		"PYTHONHOME":   "/elsewhere",
		"VIRTUAL_ENV":  "/elsewhere/venv",
		"CONDA_PREFIX": "/elsewhere/conda",
	}
	for k, v := range pythonVars {
		t.Setenv(k, v)
	}
	t.Setenv("UV_INDEX_URL", "https://index.example.org")
	for _, args := range [][]string{{"status"}, {"status", "--help"}} {
		calls := capture(t)
		if _, _, err := runSky(args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		env := (*calls)[0].env
		for _, kv := range env {
			key, _, _ := strings.Cut(kv, "=")
			if _, ok := pythonVars[key]; ok {
				t.Errorf("%v: sky ran with %s", args, kv)
			}
		}
		if !slices.Contains(env, "UV_INDEX_URL=https://index.example.org") {
			t.Errorf("%v: sky ran without the user's UV_INDEX_URL", args)
		}
	}
}

func TestEnviron(t *testing.T) {
	base := []string{
		"PATH=/bin",
		endpointVar + "=http://a",
		userVar + "=a",
		endpointVar + "_EXTRA=kept",
		endpointVar + "=http://b",
	}
	got := environ(base, "http://127.0.0.1:46581", "ada@example.org")
	want := []string{
		"PATH=/bin",
		endpointVar + "_EXTRA=kept",
		endpointVar + "=http://127.0.0.1:46581",
		userVar + "=ada@example.org",
	}
	if !slices.Equal(got, want) {
		t.Errorf("environ = %q, want %q", got, want)
	}
}

func TestRefused(t *testing.T) {
	login(t, "campus", "ada@example.org")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"api", "login"}, "`axon sky api login` is not available. Run `axon login` to log in."},
		{[]string{"api", "login", "-e", "https://other.example.org"}, "`axon sky api login` is not available. Run `axon login` to log in."},
		{[]string{"api", "logout"}, "`axon sky api logout` is not available. Run `axon logout` to log out."},
		{[]string{"api", "start", "--deploy"}, "`axon sky api start` is not available: axon connects to your organization's API server and does not start one on this computer."},
		{[]string{"api", "stop"}, "`axon sky api stop` is not available: axon connects to your organization's API server and does not start one on this computer."},
		{[]string{"api", "login", "--help"}, "`axon sky api login` is not available."},
		{[]string{"--help", "api", "stop"}, "`axon sky api stop` is not available:"},
		{[]string{"api", "--", "logout"}, "`axon sky api logout` is not available."},
		{[]string{"--", "api", "login"}, "`axon sky api login` is not available."},
	} {
		calls := capture(t)
		_, _, err := runSky(tc.args...)
		if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("%v: err = %v, want it to begin %q", tc.args, err, tc.want)
		}
		if len(*calls) != 0 {
			t.Errorf("%v: sky ran", tc.args)
		}
	}
}

func TestHelpPassesThroughWithoutALogin(t *testing.T) {
	clearSkyPilotEnv(t)
	// No login has run: sky's help needs none.
	t.Setenv("HOME", t.TempDir())
	for _, args := range [][]string{
		nil,
		{"--help"},
		{"-h"},
		{"launch", "--help"},
		{"jobs", "launch", "-h"},
		{"api", "--help"},
	} {
		calls := capture(t)
		check = probes{} // a connection check would panic
		out, errOut, err := runSky(args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if out != "" || errOut != "" {
			t.Errorf("%v: axon printed %q %q", args, out, errOut)
		}
		want := append([]string{fakeSky}, args...)
		if len(*calls) != 1 || !slices.Equal((*calls)[0].argv, want) {
			t.Fatalf("%v: ran %v, want %q", args, *calls, want)
		}
		for _, kv := range (*calls)[0].env {
			if strings.HasPrefix(kv, "SKYPILOT_") {
				t.Errorf("%v: help got %s with no login", args, kv)
			}
		}
	}
}

func TestHelpGetsTheLoginsEnvironment(t *testing.T) {
	clearSkyPilotEnv(t)
	login(t, "remote", "ada@example.org")
	calls := capture(t)
	check = probes{} // a connection check would panic
	if _, _, err := runSky("status", "--help"); err != nil {
		t.Fatal(err)
	}
	env := (*calls)[0].env
	for _, want := range []string{endpointVar + "=http://127.0.0.1:46581", userVar + "=ada@example.org"} {
		if !slices.Contains(env, want) {
			t.Errorf("help environment lacks %s", want)
		}
	}
}

func TestHelpNeedsTheClient(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := capture(t)
	skyPath = func(axondir.Dir) (string, error) { return "", skyclient.ErrNotInstalled }
	_, _, err := runSky("--help")
	if !errors.Is(err, errNoClient) {
		t.Errorf("err = %v, want %v", err, errNoClient)
	}
	if len(*calls) != 0 {
		t.Error("sky ran")
	}
}

func TestWantsHelp(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{nil, true},
		{[]string{"--help"}, true},
		{[]string{"launch", "-h"}, true},
		{[]string{"launch"}, false},
		{[]string{"exec", "dev", "--", "--help"}, false},
		{[]string{"launch", "--helpful"}, false},
		{[]string{"--", "status", "--help"}, true},
		{[]string{"exec", "dev", "--", "echo", "-h"}, false},
	} {
		if got := wantsHelp(tc.args); got != tc.want {
			t.Errorf("wantsHelp(%q) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestNoLogin(t *testing.T) {
	for name, setup := range map[string]func(axondir.Dir) error{
		"nothing":  func(axondir.Dir) error { return nil },
		"no email": func(d axondir.Dir) error { return d.SetPathway("campus") },
		"no pathway": func(d axondir.Dir) error {
			return d.SetEmail("ada@example.org")
		},
		"a pathway no file has": func(d axondir.Dir) error {
			if err := d.SetPathway("old-name"); err != nil {
				return err
			}
			return d.SetEmail("ada@example.org")
		},
		"not connected": func(d axondir.Dir) error {
			if err := d.SetPathway("campus"); err != nil {
				return err
			}
			return d.SetEmail("ada@example.org")
		},
		// The file was replaced after the login, as by an --config that
		// stopped before connecting: the pathway is not the one connected.
		"connected with other settings": func(d axondir.Dir) error {
			if err := d.SetPathway("campus"); err != nil {
				return err
			}
			old := pathwayOf(t, "campus")
			old.Sky.Endpoint = "https://old.example.org"
			if err := connected.Write(d, old); err != nil {
				return err
			}
			return d.SetEmail("ada@example.org")
		},
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			storeConfig(t, home)
			if err := setup(axondir.New(home)); err != nil {
				t.Fatal(err)
			}
			calls := capture(t)
			_, _, err := runSky("status")
			if !errors.Is(err, ErrNoLogin) {
				t.Errorf("err = %v, want %v", err, ErrNoLogin)
			}
			if len(*calls) != 0 {
				t.Error("sky ran")
			}
		})
	}
}

// The helper process runs axon sky with the arguments after "--", using
// the fake sky named by AXON_TEST_FAKE_SKY and the real process replacement.
const helperVar = "AXON_TEST_SKY_HELPER"

func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperVar) != "1" {
		t.Skip("run only as a helper process")
	}
	args := os.Args[slices.Index(os.Args, "--")+1:]
	skyPath = func(axondir.Dir) (string, error) { return os.Getenv("AXON_TEST_FAKE_SKY"), nil }
	check = helperProbes(os.Getenv(apiServerVar))
	cmd := Command()
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		fail.Exit(err)
	}
	os.Exit(0)
}

// helper runs axon sky with args in a helper process with HOME at home,
// and returns its standard output, standard error and exit code.
func helper(t *testing.T, home, sky, apiServer string, args ...string) (string, string, int) {
	t.Helper()
	clearSkyPilotEnv(t)
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestHelperProcess$", "--"}, args...)...)
	cmd.Env = append(os.Environ(),
		helperVar+"=1",
		"AXON_TEST_FAKE_SKY="+sky,
		apiServerVar+"="+apiServer,
		"HOME="+home,
		endpointVar+"=http://shell.example",
		userVar+"=shell-user",
	)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatal(err)
	}
	return out.String(), errOut.String(), cmd.ProcessState.ExitCode()
}

// writeFakeSky writes a sky that prints its program name, its arguments and
// the SkyPilot variables, one per line, and exits 7.
func writeFakeSky(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sky")
	script := `#!/bin/sh
printf '%s\n' "$0" "$@"
env | grep '^SKYPILOT_' | sort
exit 7
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReplacesProcessWithSky(t *testing.T) {
	sky := writeFakeSky(t)
	home := t.TempDir()
	storeConfig(t, home)
	dir := axondir.New(home)
	if err := dir.SetPathway("remote"); err != nil {
		t.Fatal(err)
	}
	if err := connected.Write(dir, pathwayOf(t, "remote")); err != nil {
		t.Fatal(err)
	}
	if err := dir.SetEmail("ada@example.org"); err != nil {
		t.Fatal(err)
	}
	srv := newAPIServer(t)
	out, errOut, code := helper(t, home, sky, srv.URL(), "a-command-sky-adds-later", "-c", "dev", "task.yaml")
	want := strings.Join([]string{
		sky, "a-command-sky-adds-later", "-c", "dev", "task.yaml",
		endpointVar + "=http://127.0.0.1:46581",
		userVar + "=ada@example.org",
	}, "\n") + "\n"
	if out != want {
		t.Errorf("sky printed:\n%s\nwant:\n%s", out, want)
	}
	if errOut != "" {
		t.Errorf("stderr = %q", errOut)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want sky's 7", code)
	}
}

func TestNoLoginStopsBeforeSky(t *testing.T) {
	sky := writeFakeSky(t)
	out, errOut, code := helper(t, t.TempDir(), sky, "", "status")
	if out != "" {
		t.Errorf("sky ran: %q", out)
	}
	if errOut != "axon: Run `axon login` first.\n" {
		t.Errorf("stderr = %q", errOut)
	}
	if code != fail.Code {
		t.Errorf("exit code = %d, want %d", code, fail.Code)
	}
}

// axon sky uses the stored config file and never fetches it, even
// when its recorded source is an address.
func TestSkyNeverFetchesTheConfigFile(t *testing.T) {
	var conns atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, exampleConfig)
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	dir := login(t, "campus", "ada@example.org")
	src, err := configfile.ParseSource(srv.URL + "/axon.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configfile.NewStore(dir).Put([]byte(exampleConfig), configfile.PutOptions{Origin: configfile.FromSource(src)}); err != nil {
		t.Fatal(err)
	}
	calls := capture(t)
	if _, _, err := runSky("status"); err != nil {
		t.Fatalf("sky status: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("sky ran %d times, want 1", len(*calls))
	}
	if n := conns.Load(); n != 0 {
		t.Errorf("axon sky made %d connections to the file's address", n)
	}
}
