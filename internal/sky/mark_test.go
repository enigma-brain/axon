package sky

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMark(t *testing.T) {
	clearSkyPilotEnv(t)
	login(t, "campus", "ada@example.org")
	t.Chdir(t.TempDir())
	path := filepath.Join(os.Getenv("HOME"), ".axon", "state", "sky-project.yaml")

	// A changed file is written again.
	for _, stale := range []string{"", "kubernetes: {}\n"} {
		if stale != "" {
			if err := os.WriteFile(path, []byte(stale), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		c := capture(t)
		out, err := c.run("launch")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "is not read") {
			t.Errorf("warned without a project file: %q", out)
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != mark {
			t.Errorf("%s = %q, %v", path, data, err)
		}
	}

	// A project file of the user's is named.
	if err := os.WriteFile(projectFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, _ := capture(t).run("launch"); !strings.Contains(out, ".sky.yaml is not read") {
		t.Errorf("no line about .sky.yaml: %q", out)
	}
	t.Setenv(projectFileVar, "/mine.yaml")
	if out, _ := capture(t).run("launch"); !strings.Contains(out, "/mine.yaml is not read") {
		t.Errorf("no line about the shell's project file: %q", out)
	}
}
