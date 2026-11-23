package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// build compiles axon with version into a temporary directory.
func build(t *testing.T, version string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "axon")
	cmd := exec.Command("go", "build", "-o", bin, "-ldflags", "-X main.version="+version, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// deadProxy is a proxy address where nothing listens.
const deadProxy = "http://127.0.0.1:1"

// runAxon runs the built axon with args, with stdin as its standard input,
// inside a fence. Every test that runs the built binary goes through it.
//
//   - HOME is the temporary directory the caller passes, and PATH a fresh
//     empty temporary directory, so no real tsh, sky or browser opener can
//     be found.
//   - Every HTTP and HTTPS request goes through a proxy where nothing
//     listens, so no request reaches a real server.
//   - The process runs in a new session, with no controlling terminal, so
//     a question meant for the terminal finds none.
//
// A login that gets as far as connecting a command group is stopped by the
// home directory itself: home(t) makes ~/.axon/tools a file, so the
// SkyPilot client can be neither found nor installed.
func runAxon(t *testing.T, bin, home string, stdin io.Reader, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = []string{
		"HOME=" + home,
		"PATH=" + t.TempDir(),
		"HTTPS_PROXY=" + deadProxy,
		"HTTP_PROXY=" + deadProxy,
		"https_proxy=" + deadProxy,
		"http_proxy=" + deadProxy,
	}
	cmd.Stdin = stdin
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

// home returns a home directory whose ~/.axon/tools is a file, so that no
// login can install or run a client.
func home(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	if err := os.MkdirAll(filepath.Join(h, ".axon"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h, ".axon", "tools"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return h
}

// The config files the tests add. Every detail is made up.
const (
	// formatOneFile is a file of the format an earlier axon used.
	formatOneFile = `format = 1
id = "example-older"

[[pathway]]
id = "older"
kind = "direct"
name = "Older"
description = "Single sign-on in a browser."

[pathway.devenv]
endpoint = "https://sky.example.org"
`

	campusFile = `format = 2
id = "example-campus"

[[pathway]]
id = "campus"
kind = "direct"
name = "Campus"
description = "Single sign-on in a browser."

[pathway.sky]
endpoint = "https://sky.example.org"
`
	twoFile = `format = 2
id = "example-two"

[[pathway]]
id = "north"
kind = "direct"
name = "North"
description = "From the north site."

[pathway.sky]
endpoint = "https://north.example.org"

[[pathway]]
id = "south"
kind = "teleport"
name = "South"
description = "From anywhere."

[pathway.teleport]
proxy = "teleport.example.org"

[pathway.sky]
app = "skypilot"
`
)

// stopped is how a login that reached the sky group's connection ends
// inside the fence.
const stopped = "axon: connect sky: check the SkyPilot client: "

// stores writes data as the stored config file id under home.
func stores(t *testing.T, home, id, data string) {
	t.Helper()
	orgs := filepath.Join(home, ".axon", "orgs")
	if err := os.MkdirAll(orgs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orgs, id+".toml"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readActive(t *testing.T, home string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".axon", "config.toml"))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func wantFailure(t *testing.T, name string, err error) {
	t.Helper()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
		t.Errorf("%s: err = %v, want a non-zero exit", name, err)
	}
}

func TestBinary(t *testing.T) {
	bin := build(t, "1.2.3")

	t.Run("version and help with no file", func(t *testing.T) {
		out, _, err := runAxon(t, bin, home(t), nil, "--version")
		if err != nil || out != "axon 1.2.3\n" {
			t.Errorf("--version = %q, %v", out, err)
		}
		out, _, err = runAxon(t, bin, home(t), nil, "--help")
		if err != nil || !strings.Contains(out, "login") || !strings.Contains(out, "sky") {
			t.Errorf("--help = %q, %v", out, err)
		}
	})

	t.Run("no file", func(t *testing.T) {
		h := home(t)
		_, stderr, err := runAxon(t, bin, h, nil, "login")
		wantFailure(t, "login", err)
		want := "axon: no config file. Run `axon login --config <source>` with the address or file your organization gives you.\n"
		if stderr != want {
			t.Errorf("login: stderr = %q, want %q", stderr, want)
		}
		_, stderr, err = runAxon(t, bin, h, nil, "sky", "status")
		wantFailure(t, "sky", err)
		if stderr != "axon: Run `axon login` first.\n" {
			t.Errorf("sky: stderr = %q", stderr)
		}
	})

	t.Run("logout with nothing logged in", func(t *testing.T) {
		out, stderr, err := runAxon(t, bin, home(t), nil, "logout")
		if err != nil || out != "Nothing is logged in.\n" || stderr != "" {
			t.Errorf("logout = %q, %q, %v", out, stderr, err)
		}
	})

	t.Run("old pathway and no file", func(t *testing.T) {
		h := home(t)
		if err := os.WriteFile(filepath.Join(h, ".axon", "config.toml"), []byte("pathway = \"old-name\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, stderr, err := runAxon(t, bin, h, nil, "login")
		wantFailure(t, "login", err)
		if !strings.HasPrefix(stderr, "axon: no config file.") {
			t.Errorf("stderr = %q", stderr)
		}
	})

	t.Run("one file with one pathway", func(t *testing.T) {
		h := home(t)
		stores(t, h, "example-campus", campusFile)
		_, stderr, err := runAxon(t, bin, h, nil, "login")
		wantFailure(t, "login", err)
		if !strings.HasPrefix(stderr, stopped) {
			t.Errorf("stderr = %q, want the stop at the client", stderr)
		}
		if got := readActive(t, h); got != "pathway = \"campus\"\n" {
			t.Errorf("config.toml = %q", got)
		}
	})

	t.Run("one file with two pathways", func(t *testing.T) {
		h := home(t)
		stores(t, h, "example-two", twoFile)
		_, stderr, err := runAxon(t, bin, h, nil, "login")
		wantFailure(t, "login", err)
		want := "axon: determine the pathway: no pathway chosen. Run `axon login --pathway <id>` with one of north and south.\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
		stdout, _, _ := runAxon(t, bin, h, strings.NewReader("1\n"), "login")
		if !strings.Contains(stdout, "  1) North  From the north site.\n  2) South  From anywhere.\n") {
			t.Errorf("prompt:\n%s", stdout)
		}
		if got := readActive(t, h); got != "pathway = \"north\"\n" {
			t.Errorf("config.toml = %q", got)
		}
	})

	t.Run("two files", func(t *testing.T) {
		h := home(t)
		stores(t, h, "example-two", twoFile)
		stores(t, h, "example-campus", campusFile)
		_, stderr, err := runAxon(t, bin, h, nil, "login")
		wantFailure(t, "login", err)
		if !strings.HasSuffix(stderr, "with one of campus, north and south.\n") {
			t.Errorf("stderr = %q", stderr)
		}
	})

	t.Run("pathway switch", func(t *testing.T) {
		h := home(t)
		stores(t, h, "example-two", twoFile)
		stores(t, h, "example-campus", campusFile)
		_, _, _ = runAxon(t, bin, h, nil, "login", "--pathway", "north")
		state := filepath.Join(h, ".axon", "state")
		if err := os.WriteFile(filepath.Join(state, "email"), []byte("ada@example.org\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, stderr, err := runAxon(t, bin, h, nil, "login", "--pathway", "campus")
		wantFailure(t, "login", err)
		if !strings.HasPrefix(stderr, stopped) {
			t.Errorf("stderr = %q", stderr)
		}
		if got := readActive(t, h); got != "pathway = \"campus\"\n" {
			t.Errorf("config.toml = %q", got)
		}
		if _, err := os.Stat(filepath.Join(state, "email")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the email of the pathway left is still recorded: %v", err)
		}
		record, _ := os.ReadFile(filepath.Join(state, "pathway.toml"))
		if !strings.Contains(string(record), "id = \"campus\"") {
			t.Errorf("connected record = %q", record)
		}
	})

	t.Run("unknown pathway", func(t *testing.T) {
		h := home(t)
		stores(t, h, "example-two", twoFile)
		_, stderr, err := runAxon(t, bin, h, nil, "login", "--pathway", "west")
		wantFailure(t, "login", err)
		want := "axon: determine the pathway: no stored config file has the pathway \"west\". The stored pathways are north and south.\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
		if got := readActive(t, h); got != "" {
			t.Errorf("config.toml = %q, want none", got)
		}
	})

	t.Run("org from a path", func(t *testing.T) {
		h := home(t)
		path := filepath.Join(t.TempDir(), "axon.toml")
		if err := os.WriteFile(path, []byte(campusFile), 0o600); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, err := runAxon(t, bin, h, nil, "login", "--config", path)
		wantFailure(t, "login", err)
		stored := filepath.Join(h, ".axon", "orgs", "example-campus.toml")
		if stdout != "Stored the config file "+stored+"\n" || !strings.HasPrefix(stderr, stopped) {
			t.Errorf("stdout = %q, stderr = %q", stdout, stderr)
		}
		if data, _ := os.ReadFile(stored); string(data) != campusFile {
			t.Errorf("stored file = %q", data)
		}
		if got := readActive(t, h); got != "pathway = \"campus\"\n" {
			t.Errorf("config.toml = %q", got)
		}
	})

	t.Run("org from standard input", func(t *testing.T) {
		h := home(t)
		_, stderr, err := runAxon(t, bin, h, strings.NewReader(campusFile), "login", "--config", "-")
		wantFailure(t, "login", err)
		if !strings.HasPrefix(stderr, stopped) {
			t.Errorf("stderr = %q", stderr)
		}
		if got := readActive(t, h); got != "pathway = \"campus\"\n" {
			t.Errorf("config.toml = %q", got)
		}
	})

	t.Run("org from standard input without a terminal", func(t *testing.T) {
		h := home(t)
		_, stderr, err := runAxon(t, bin, h, strings.NewReader(twoFile), "login", "--config", "-")
		wantFailure(t, "login", err)
		if !strings.HasSuffix(stderr, "Run `axon login --pathway <id>` with one of north and south.\n") {
			t.Errorf("stderr = %q", stderr)
		}
		_, stderr, _ = runAxon(t, bin, h, strings.NewReader(twoFile), "login", "--config", "-", "--pathway", "south")
		// A teleport pathway's session stage asks the proxy for its version,
		// which goes through the dead proxy.
		if !strings.HasPrefix(stderr, "axon: establish the Teleport session: ") {
			t.Errorf("stderr = %q", stderr)
		}
		if got := readActive(t, h); got != "pathway = \"south\"\n" {
			t.Errorf("config.toml = %q", got)
		}
	})

	t.Run("org from an address goes through the fence", func(t *testing.T) {
		h := home(t)
		_, stderr, err := runAxon(t, bin, h, nil, "login", "--config", "https://example.org/axon.toml")
		wantFailure(t, "login", err)
		if !strings.HasPrefix(stderr, "axon: add the config file: ") {
			t.Errorf("stderr = %q", stderr)
		}
		if _, err := os.Stat(filepath.Join(h, ".axon", "orgs")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("something was stored: %v", err)
		}
	})

	t.Run("config file of format 1", func(t *testing.T) {
		h := home(t)
		older := filepath.Join(t.TempDir(), "older.toml")
		if err := os.WriteFile(older, []byte(formatOneFile), 0o600); err != nil {
			t.Fatal(err)
		}
		_, stderr, err := runAxon(t, bin, h, nil, "login", "--config", older)
		wantFailure(t, "login --config", err)
		want := "axon: add the config file: the file is in an older format. Run `axon login --config <source>` again with your organization's current config file.\n"
		if stderr != want {
			t.Errorf("passed: stderr = %q, want %q", stderr, want)
		}

		stores(t, h, "example-older", formatOneFile)
		_, stderr, err = runAxon(t, bin, h, nil, "login")
		wantFailure(t, "login", err)
		stored := filepath.Join(h, ".axon", "orgs", "example-older.toml")
		want = "axon: left out " + stored + ": the file is in an older format. Run `axon login --config <source>` again with your organization's current config file.\n" +
			"axon: no config file. Run `axon login --config <source>` with the address or file your organization gives you.\n"
		if stderr != want {
			t.Errorf("stored: stderr = %q, want %q", stderr, want)
		}
	})

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"devenv", "status"}, "axon: unknown command \"devenv\""},
		{[]string{"sky", "api", "stop"}, "axon: `axon sky api stop` is not available:"},
		{[]string{"bogus"}, "axon: unknown command \"bogus\""},
		{[]string{"login", "--org", "https://example.org/axon.toml"}, "axon: unknown flag: --org"},
	} {
		_, stderr, err := runAxon(t, bin, home(t), nil, tc.args...)
		wantFailure(t, strings.Join(tc.args, " "), err)
		if !strings.HasPrefix(stderr, tc.want) {
			t.Errorf("%v: stderr = %q, want it to begin with %q", tc.args, stderr, tc.want)
		}
	}
}
