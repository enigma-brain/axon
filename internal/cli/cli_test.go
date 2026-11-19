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
	"github.com/enigma-brain/axon/internal/devenv"
)

// twoPathways is an organisation file with two made-up pathways.
const twoPathways = `format = 1
id = "example"

[[pathway]]
id = "campus"
kind = "direct"
name = "Campus"
description = "On campus."

[pathway.devenv]
endpoint = "https://sky.example.org"

[[pathway]]
id = "lab"
kind = "direct"
name = "Lab"
description = "In the lab."

[pathway.devenv]
endpoint = "https://lab.example.org"
`

// storeOrg sets HOME to a new directory holding twoPathways as a stored
// organisation file. The login tests below stop before any network request.
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

func TestHelpListsOnlyLoginAndDevenv(t *testing.T) {
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
	if got := strings.Join(names, " "); got != "devenv login" {
		t.Errorf("help lists %q, want %q", got, "devenv login")
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

func TestLoginHelpNamesItsFlags(t *testing.T) {
	out, err := run(t, "login", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--org", "--pathway", "https address", "standard input"} {
		if !strings.Contains(out, want) {
			t.Errorf("login --help lacks %q:\n%s", want, out)
		}
	}
}

func TestLoginWithoutAFileStops(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := run(t, "login")
	if err == nil || !strings.HasPrefix(err.Error(), "no organisation configuration. Run `axon login --org <source>`") {
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

func TestDevenvIsWired(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out, err := run(t, "devenv", "--help")
	if err != nil || !strings.Contains(out, "jobs launch") {
		t.Errorf("devenv --help: err = %v, output:\n%s", err, out)
	}
	if _, err := run(t, "devenv", "launch", "-c", "dev", "task.yaml"); !errors.Is(err, devenv.ErrNoLogin) {
		t.Errorf("devenv launch without a login: err = %v, want %v", err, devenv.ErrNoLogin)
	}
}
