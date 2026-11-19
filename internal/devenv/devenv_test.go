package devenv

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
	"github.com/enigma-brain/axon/internal/connected"
	"github.com/enigma-brain/axon/internal/fail"
	"github.com/enigma-brain/axon/internal/orgfile"
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

// exampleOrg is the organisation file the tests store: a direct pathway,
// campus, and a teleport pathway, remote. Every detail is made up.
const exampleOrg = `format = 1
id = "example"

[[pathway]]
id = "campus"
kind = "direct"
name = "Campus"
description = "Single sign-on in a browser."
unreachable_hint = "Connect to the campus VPN and retry."

[pathway.devenv]
endpoint = "https://sky.example.org"

[[pathway]]
id = "remote"
kind = "teleport"
name = "Remote"
description = "Works from anywhere."

[pathway.teleport]
proxy = "teleport.example.org"

[pathway.devenv]
app = "devenv"
`

// pathways are the identifiers of exampleOrg's pathways, with the endpoint
// the client uses on each.
var pathways = map[string]string{
	"campus": "https://sky.example.org",
	"remote": "http://localhost:46581",
}

// storeOrg stores exampleOrg under home.
func storeOrg(t *testing.T, home string) {
	t.Helper()
	src, err := orgfile.ParseSource("-")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orgfile.NewStore(axondir.New(home)).Add([]byte(exampleOrg), src); err != nil {
		t.Fatal(err)
	}
}

// pathwayOf returns the stored pathway id.
func pathwayOf(t *testing.T, id string) orgfile.Pathway {
	t.Helper()
	f, err := orgfile.Parse([]byte(exampleOrg))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range f.Pathways {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("no pathway %q", id)
	return orgfile.Pathway{}
}

// login sets HOME to a new directory, stores exampleOrg, and records a
// login on the pathway id.
func login(t *testing.T, id string, email string) axondir.Dir {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	storeOrg(t, home)
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

// run runs axon devenv with args and returns its standard output, its
// standard error and its error.
func run(args ...string) (string, string, error) {
	cmd := Command()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	// A nil slice makes cobra read the test binary's own arguments.
	cmd.SetArgs(append([]string{}, args...))
	err := cmd.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

func TestEveryCoveredCommandForwardsItsArguments(t *testing.T) {
	login(t, "campus", "ada@example.org")
	if len(commands) != 13 {
		t.Fatalf("%d commands are covered, want 13", len(commands))
	}
	extra := []string{"-c", "dev", "--some-flag-axon-does-not-know", "x", "-h2", "--", "--help"}
	for _, c := range commands {
		calls := capture(t)
		args := append(slices.Clone(c.words), extra...)
		if _, _, err := run(args...); err != nil {
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

func TestLaunchExample(t *testing.T) {
	login(t, "remote", "ada@example.org")
	calls := capture(t)
	if _, _, err := run("launch", "-c", "dev", "task.yaml"); err != nil {
		t.Fatal(err)
	}
	want := []string{fakeSky, "launch", "-c", "dev", "task.yaml"}
	if len(*calls) != 1 || !slices.Equal((*calls)[0].argv, want) {
		t.Errorf("ran %v, want %q", *calls, want)
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
			if _, _, err := run("status"); err != nil {
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

func TestEnviron(t *testing.T) {
	base := []string{
		"PATH=/bin",
		endpointVar + "=http://a",
		userVar + "=a",
		endpointVar + "_EXTRA=kept",
		endpointVar + "=http://b",
	}
	got := environ(base, "http://localhost:46581", "ada@example.org")
	want := []string{
		"PATH=/bin",
		endpointVar + "_EXTRA=kept",
		endpointVar + "=http://localhost:46581",
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
		{[]string{"api", "login"}, `axon devenv does not cover "api". Run "axon devenv --help" for the commands it covers`},
		{[]string{"stop", "dev"}, `axon devenv does not cover "stop". Run "axon devenv --help"`},
		{[]string{"volumes", "delete", "v1"}, `axon devenv does not cover "volumes delete". The volumes commands it covers are "volumes ls"`},
		{[]string{"jobs"}, `axon devenv does not cover "jobs". The jobs commands it covers are "jobs launch", "jobs queue", "jobs logs", "jobs cancel"`},
		{[]string{"gpus", "--all"}, `axon devenv does not cover "gpus". The gpus commands it covers are "gpus list"`},
		{[]string{"-c", "dev", "launch"}, `axon devenv does not cover "-c".`},
		{[]string{"LAUNCH"}, `axon devenv does not cover "LAUNCH".`},
	} {
		calls := capture(t)
		_, _, err := run(tc.args...)
		if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("%v: err = %v, want it to begin %q", tc.args, err, tc.want)
		}
		if len(*calls) != 0 {
			t.Errorf("%v: sky ran", tc.args)
		}
	}
}

func TestNoCommand(t *testing.T) {
	calls := capture(t)
	out, errOut, err := run()
	if !errors.Is(err, errNoCommand) {
		t.Errorf("err = %v, want %v", err, errNoCommand)
	}
	if out != "" || !strings.Contains(errOut, "gpus list") {
		t.Errorf("stdout = %q, stderr = %q; want the list on stderr only", out, errOut)
	}
	if len(*calls) != 0 {
		t.Error("sky ran")
	}
}

func TestOverviewListsEveryCommand(t *testing.T) {
	calls := capture(t)
	for _, flag := range []string{"--help", "-h"} {
		out, _, err := run(flag)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range commands {
			if !strings.Contains(out, "  "+strings.Join(c.words, " ")+" ") {
				t.Errorf("%s does not list %v:\n%s", flag, c.words, out)
			}
		}
	}
	if len(*calls) != 0 {
		t.Error("sky ran")
	}
}

func TestCommandHelp(t *testing.T) {
	// No login has run: help needs none.
	t.Setenv("HOME", t.TempDir())
	calls := capture(t)
	for _, c := range commands {
		for _, flag := range []string{"--help", "-h"} {
			args := append(slices.Clone(c.words), flag, "ignored")
			out, _, err := run(args...)
			if err != nil {
				t.Fatalf("%v: %v", args, err)
			}
			name := "axon devenv " + strings.Join(c.words, " ")
			for _, want := range []string{c.summary + ".", name + " " + c.args, "Other flags are passed to SkyPilot unchanged.", c.examples[0].line} {
				if !strings.Contains(out, want) {
					t.Errorf("%v: help lacks %q:\n%s", args, want, out)
				}
			}
			for _, line := range strings.Split(out, "\n") {
				if len(line) > lineWidth {
					t.Errorf("%v: line wider than %d: %q", args, lineWidth, line)
				}
			}
		}
	}
	if len(*calls) != 0 {
		t.Error("sky ran")
	}
}

func TestGroupHelp(t *testing.T) {
	calls := capture(t)
	out, _, err := run("jobs", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"jobs launch", "jobs queue", "jobs logs", "jobs cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("jobs --help lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "volumes ls") {
		t.Errorf("jobs --help lists another group:\n%s", out)
	}
	if len(*calls) != 0 {
		t.Error("sky ran")
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
		// The file was replaced after the login, as by an --org that
		// stopped before connecting: the pathway is not the one connected.
		"connected with other settings": func(d axondir.Dir) error {
			if err := d.SetPathway("campus"); err != nil {
				return err
			}
			old := pathwayOf(t, "campus")
			old.Devenv.Endpoint = "https://old.example.org"
			if err := connected.Write(d, old); err != nil {
				return err
			}
			return d.SetEmail("ada@example.org")
		},
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			storeOrg(t, home)
			if err := setup(axondir.New(home)); err != nil {
				t.Fatal(err)
			}
			calls := capture(t)
			_, _, err := run("status")
			if !errors.Is(err, ErrNoLogin) {
				t.Errorf("err = %v, want %v", err, ErrNoLogin)
			}
			if len(*calls) != 0 {
				t.Error("sky ran")
			}
		})
	}
}

// The helper process runs axon devenv with the arguments after "--", using
// the fake sky named by AXON_TEST_FAKE_SKY and the real process replacement.
const helperVar = "AXON_TEST_DEVENV_HELPER"

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

// helper runs axon devenv with args in a helper process with HOME at home,
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
	storeOrg(t, home)
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
	out, errOut, code := helper(t, home, sky, srv.URL(), "launch", "-c", "dev", "task.yaml")
	want := strings.Join([]string{
		sky, "launch", "-c", "dev", "task.yaml",
		endpointVar + "=http://localhost:46581",
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

// axon devenv uses the stored organisation file and never fetches it, even
// when its recorded source is an address.
func TestDevenvNeverFetchesTheOrganisationFile(t *testing.T) {
	var conns atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, exampleOrg)
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	dir := login(t, "campus", "ada@example.org")
	src, err := orgfile.ParseSource(srv.URL + "/axon.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orgfile.NewStore(dir).Add([]byte(exampleOrg), src); err != nil {
		t.Fatal(err)
	}
	calls := capture(t)
	if _, _, err := run("status"); err != nil {
		t.Fatalf("devenv status: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("sky ran %d times, want 1", len(*calls))
	}
	if n := conns.Load(); n != 0 {
		t.Errorf("devenv made %d connections to the file's address", n)
	}
}
