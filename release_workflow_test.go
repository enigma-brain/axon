package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestReleaseWorkflowBuildsPushedTag checks that every GoReleaser step in the
// release workflow sets GORELEASER_CURRENT_TAG, in its own env, to the tag
// whose push started the run. Without it GoReleaser finds the tag in git, and
// picks any of the tags on the commit.
func TestReleaseWorkflowBuildsPushedTag(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				Uses string            `yaml:"uses"`
				Env  map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatalf("parse release.yml: %v", err)
	}

	// Whitespace inside the expression does not matter.
	const want = "${{github.ref_name}}"
	found := 0
	for jobName, job := range wf.Jobs {
		for i, step := range job.Steps {
			if !strings.HasPrefix(step.Uses, "goreleaser/goreleaser-action@") {
				continue
			}
			found++
			got := strings.Join(strings.Fields(step.Env["GORELEASER_CURRENT_TAG"]), "")
			if got != want {
				t.Errorf("job %q, step %d: GORELEASER_CURRENT_TAG is %q, want ${{ github.ref_name }}",
					jobName, i+1, step.Env["GORELEASER_CURRENT_TAG"])
			}
		}
	}
	if found == 0 {
		t.Error("no step in release.yml uses goreleaser/goreleaser-action")
	}
}
