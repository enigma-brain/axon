package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/sky"
)

// twoPathways is a config file with two made-up pathways.
const twoPathways = `format = 2
id = "example"

[[pathway]]
id = "campus"
kind = "direct"
name = "Campus"
description = "On campus."

[pathway.sky]
endpoint = "https://sky.example.org"

[[pathway]]
id = "lab"
kind = "direct"
name = "Lab"
description = "In the lab."

[pathway.sky]
endpoint = "https://lab.example.org"
`

// storeOrg sets HOME to a new directory holding twoPathways as a stored
// config file. The login tests below stop before any network request.
func storeOrg(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	orgs := filepath.Join(home, ".axon", "orgs")
	if err := os.MkdirAll(orgs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orgs, "example.toml"), []byte(twoPathways), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRoot("1.2.3")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

func TestHelpListsTheCommands(t *testing.T) {
	out, err := run(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	_, commands, ok := strings.Cut(out, "Available Commands:\n")
	if !ok {
		t.Fatalf("help has no command list:\n%s", out)
	}
	commands, _, _ = strings.Cut(commands, "\n\n")
	var names []string
	for _, line := range strings.Split(commands, "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			names = append(names, f[0])
		}
	}
	if got, want := strings.Join(names, " "), "config login logout sky update"; got != want {
		t.Errorf("help lists %q, want %q", got, want)
	}
}

func TestHelpAndCompletionCommandsAbsent(t *testing.T) {
	for _, name := range []string{"help", "completion"} {
		if _, err := run(t, name); err == nil || !strings.Contains(err.Error(), "unknown command") {
			t.Errorf("axon %s: err = %v, want unknown command", name, err)
		}
	}
}

func TestVersion(t *testing.T) {
	out, err := run(t, "--version")
	if err != nil {
		t.Fatal(err)
	}
	if out != "axon 1.2.3\n" {
		t.Errorf("--version printed %q", out)
	}
}

func TestUpdateHelpNamesItsFlag(t *testing.T) {
	out, err := run(t, "update", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Update axon to the latest release", "checksums", "--version", "1.2.3-rc.1"} {
		if !strings.Contains(out, want) {
			t.Errorf("update --help lacks %q:\n%s", want, out)
		}
	}
}

// The root's --version still prints the version; the update command's own
// --version names a release.
func TestUpdateVersionFlagIsTheCommands(t *testing.T) {
	_, err := run(t, "update", "--version")
	if err == nil || !strings.Contains(err.Error(), "flag needs an argument: --version") {
		t.Errorf("update --version: err = %v", err)
	}
}

func TestUpdateOfADevelopmentBuildIsRefused(t *testing.T) {
	root := NewRoot("dev")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"update"})
	err := root.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "development build (dev)") {
		t.Errorf("update of dev: err = %v", err)
	}
}

func TestLoginHelpNamesItsFlags(t *testing.T) {
	out, err := run(t, "login", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--config", "--pathway", "https address", "standard input"} {
		if !strings.Contains(out, want) {
			t.Errorf("login --help lacks %q:\n%s", want, out)
		}
	}
}

func TestLoginWithoutAFileStops(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := run(t, "login")
	if err == nil || !strings.HasPrefix(err.Error(), "no pathway is set up, and there is no terminal to ask on.") {
		t.Errorf("login with no file: err = %v", err)
	}
}

func TestLoginUnknownPathwayIsRefused(t *testing.T) {
	home := storeOrg(t)
	_, err := run(t, "login", "--pathway", "nope")
	if err == nil || !strings.Contains(err.Error(), "The stored pathways are campus and lab.") {
		t.Errorf("login --pathway nope: err = %v", err)
	}
	if _, ok, _ := axondir.New(home).Pathway(); ok {
		t.Error("a pathway was stored")
	}
}

func TestLoginWithoutTerminalAsksForFlag(t *testing.T) {
	storeOrg(t)
	root := NewRoot("1.2.3")
	var out bytes.Buffer
	root.SetIn(strings.NewReader(""))
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"login"})
	err := root.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "--pathway") {
		t.Errorf("login with no input: err = %v, want one naming --pathway", err)
	}
}

func TestSkyIsWired(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// With no client installed, sky's help stops before running anything.
	if _, err := run(t, "sky", "--help"); err == nil || !strings.Contains(err.Error(), "SkyPilot client is not installed") {
		t.Errorf("sky --help with no client: err = %v", err)
	}
	if _, err := run(t, "sky", "launch", "-c", "dev", "task.yaml"); !errors.Is(err, sky.ErrNoLogin) {
		t.Errorf("sky launch without a login: err = %v, want %v", err, sky.ErrNoLogin)
	}
}

func TestDevenvIsGone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := run(t, "devenv", "status"); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("axon devenv: err = %v, want unknown command", err)
	}
}

func TestConfigHelp(t *testing.T) {
	for args, want := range map[string][]string{
		"config":             {"See the config files and pathways stored on this laptop", "list"},
		"config --help":      {"See the config files and pathways stored on this laptop", "list"},
		"config list --help": {"List the stored config files", "--pathway", "change"},
	} {
		out, err := run(t, strings.Fields(args)...)
		if err != nil {
			t.Fatalf("axon %s: %v", args, err)
		}
		for _, w := range want {
			if !strings.Contains(out, w) {
				t.Errorf("axon %s lacks %q:\n%s", args, w, out)
			}
		}
	}
}

func TestConfigListIsWired(t *testing.T) {
	home := storeOrg(t)
	if err := axondir.New(home).SetPathway("lab"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "config", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"The pathway in use is Lab (lab), from example.", "  * Lab  --pathway lab\n", "source not recorded"} {
		if !strings.Contains(out, want) {
			t.Errorf("config list lacks %q:\n%s", want, out)
		}
	}
}

func TestConfigListWithNothingStored(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out, err := run(t, "config", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Run `axon login` to add a pathway.") {
		t.Errorf("config list:\n%s", out)
	}
}
