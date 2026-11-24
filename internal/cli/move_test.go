package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestMain gives every test a temporary HOME, so that no test reaches the
// user's own ~/.axon even when it forgets to set one.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "axon-cli-home")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

// countMoves replaces moveOldNames with one that counts its calls.
func countMoves(t *testing.T) *int {
	t.Helper()
	n := new(int)
	saved := moveOldNames
	moveOldNames = func(io.Writer) error { *n++; return nil }
	t.Cleanup(func() { moveOldNames = saved })
	return n
}

// stubbedRoot returns the command tree with every command's own work
// replaced by nothing, so that only the hooks run.
func stubbedRoot() *cobra.Command {
	root := NewRoot("1.2.3")
	var stub func(c *cobra.Command)
	stub = func(c *cobra.Command) {
		if c.Runnable() {
			c.Run = nil
			c.RunE = func(*cobra.Command, []string) error { return nil }
		}
		for _, sub := range c.Commands() {
			stub(sub)
		}
	}
	stub(root)
	return root
}

func execute(root *cobra.Command, args ...string) error {
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs(args)
	return root.ExecuteContext(context.Background())
}

// Every command that can run moves the old names first, except update, so
// that a command added later cannot read ~/.axon under the old names.
func TestEveryCommandMovesTheOldNamesFirst(t *testing.T) {
	var paths [][]string
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Runnable() && c.HasParent() {
			paths = append(paths, strings.Fields(c.CommandPath())[1:])
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(NewRoot("1.2.3"))
	if len(paths) < 4 {
		t.Fatalf("found only the commands %q", paths)
	}
	for _, path := range paths {
		t.Run(strings.Join(path, " "), func(t *testing.T) {
			n := countMoves(t)
			if err := execute(stubbedRoot(), path...); err != nil {
				t.Fatal(err)
			}
			want := 1
			if path[0] == "update" {
				want = 0
			}
			if *n != want {
				t.Errorf("moved the old names %d times, want %d", *n, want)
			}
		})
	}
}

// axon sky passes every argument to sky, --help included, so each form
// moves the old names; cobra answers --help and --version for axon's own
// commands before any hook runs.
func TestWhichFormsMoveTheOldNames(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		moves int
	}{
		{[]string{"sky"}, 1},
		{[]string{"sky", "--help"}, 1},
		{[]string{"sky", "-h"}, 1},
		{[]string{"sky", "api", "--help"}, 1},
		{[]string{"sky", "status"}, 1},
		{[]string{"sky", "--version"}, 1},
		{[]string{"logout"}, 1},
		{[]string{"login", "--pathway", "campus"}, 1},
		{[]string{"--version"}, 0},
		{[]string{"--help"}, 0},
		{[]string{"login", "--help"}, 0},
		{[]string{"update"}, 0},
		{[]string{"update", "--version", "1.2.3"}, 0},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			n := countMoves(t)
			if err := execute(stubbedRoot(), tc.args...); err != nil {
				t.Fatal(err)
			}
			if *n != tc.moves {
				t.Errorf("moved the old names %d times, want %d", *n, tc.moves)
			}
		})
	}
}

// oldHome sets HOME to a new directory holding what axon v0.3.0 kept: the
// two-pathway config file in orgs/ and the active pathway in config.toml.
func oldHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	axon := filepath.Join(home, ".axon")
	if err := os.MkdirAll(filepath.Join(axon, "orgs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(axon, "orgs", "example.toml"), []byte(twoPathways), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(axon, "config.toml"), []byte("pathway = \"campus\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func runSplit(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := NewRoot("1.2.3")
	var out, errOut bytes.Buffer
	root.SetIn(strings.NewReader(""))
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

func TestLogoutMovesTheOldNames(t *testing.T) {
	home := oldHome(t)
	axon := filepath.Join(home, ".axon")
	out, errOut, err := runSplit(t, "logout")
	if err != nil {
		t.Fatal(err)
	}
	want := "Moved axon's files to their new names in " + axon + ": orgs/ to configs/, config.toml to settings.toml\n"
	if errOut != want {
		t.Errorf("stderr = %q, want %q", errOut, want)
	}
	if strings.Contains(out, "Moved") {
		t.Errorf("stdout names the move: %q", out)
	}
	for _, p := range []string{"configs/example.toml", "settings.toml"} {
		if _, err := os.Stat(filepath.Join(axon, p)); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}

	// The next command has nothing to move and says nothing about it.
	if _, errOut, err := runSplit(t, "logout"); err != nil || errOut != "" {
		t.Errorf("second logout: stderr = %q, err = %v", errOut, err)
	}
}

// After the move, login finds the stored pathways and the active one.
func TestLoginAfterTheMoveFindsThePathways(t *testing.T) {
	oldHome(t)
	_, _, err := runSplit(t, "login", "--pathway", "nope")
	if err == nil || !strings.Contains(err.Error(), "The stored pathways are campus and lab.") {
		t.Errorf("login --pathway nope: err = %v", err)
	}
}

func TestVersionLeavesTheOldNames(t *testing.T) {
	home := oldHome(t)
	if _, _, err := runSplit(t, "--version"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".axon", "orgs", "example.toml")); err != nil {
		t.Errorf("--version moved the config files: %v", err)
	}
}

func TestBothNamesAreReportedOncePerCommand(t *testing.T) {
	home := oldHome(t)
	axon := filepath.Join(home, ".axon")
	if err := os.WriteFile(filepath.Join(axon, "settings.toml"), []byte("pathway = \"lab\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	both := "Both " + filepath.Join(axon, "config.toml") + " and " + filepath.Join(axon, "settings.toml") +
		" exist; axon uses " + filepath.Join(axon, "settings.toml") + ", and " + filepath.Join(axon, "config.toml") + " can be deleted\n"
	_, errOut, err := runSplit(t, "logout")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Moved axon's files to their new names in " + axon + ": orgs/ to configs/\n" + both; errOut != want {
		t.Errorf("stderr = %q, want %q", errOut, want)
	}
	if _, errOut, _ := runSplit(t, "logout"); errOut != both {
		t.Errorf("second logout: stderr = %q, want %q", errOut, both)
	}
	if got, _ := os.ReadFile(filepath.Join(axon, "settings.toml")); string(got) != "pathway = \"lab\"\n" {
		t.Errorf("settings.toml = %q, want it unchanged", got)
	}
}

func TestAMoveThatFailsStopsTheCommand(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes to a read-only directory")
	}
	home := oldHome(t)
	axon := filepath.Join(home, ".axon")
	if err := os.Mkdir(filepath.Join(axon, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(axon, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(axon, 0o700) })
	out, _, err := runSplit(t, "logout")
	want := "move axon's files to their new names: rename " + filepath.Join(axon, "orgs") + " to " + filepath.Join(axon, "configs") + ": permission denied"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
	if out != "" {
		t.Errorf("logout ran after the move failed: %q", out)
	}
}
