package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// releaseWorkflow is the part of a GitHub Actions workflow that decides the
// environment of its steps.
type releaseWorkflow struct {
	Env  map[string]string `yaml:"env"`
	Jobs map[string]struct {
		Env   map[string]string `yaml:"env"`
		Steps []struct {
			Name string            `yaml:"name"`
			Uses string            `yaml:"uses"`
			Env  map[string]string `yaml:"env"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// pushedRef is the expression for the name of the tag whose push started
// the run.
const pushedRef = "${{github.ref_name}}"

// compactExpr removes the whitespace from a value, so that
// "${{ github.ref_name }}" and "${{github.ref_name}}" compare equal.
func compactExpr(v string) string {
	return strings.Join(strings.Fields(v), "")
}

// checkReleaseTag returns an error unless every GoReleaser step in the
// workflow sets GORELEASER_CURRENT_TAG to the pushed ref, either directly or
// through an env variable that is itself the pushed ref. GoReleaser
// otherwise finds the tag in git, and picks any of the tags on the commit.
func checkReleaseTag(data []byte) error {
	var wf releaseWorkflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return fmt.Errorf("parse workflow: %w", err)
	}
	found := 0
	for jobName, job := range wf.Jobs {
		for i, step := range job.Steps {
			if !strings.HasPrefix(step.Uses, "goreleaser/goreleaser-action@") {
				continue
			}
			found++
			// A step's env overrides its job's, which overrides the
			// workflow's. lookup searches the scopes from the given one
			// outward and returns the scope where the name is defined.
			scopes := []map[string]string{step.Env, job.Env, wf.Env}
			lookup := func(name string, from int) (string, int, bool) {
				for j := from; j < len(scopes); j++ {
					if v, ok := scopes[j][name]; ok {
						return compactExpr(v), j, true
					}
				}
				return "", 0, false
			}
			where := fmt.Sprintf("job %q, step %d (%s)", jobName, i+1, step.Uses)
			tag, scope, ok := lookup("GORELEASER_CURRENT_TAG", 0)
			if !ok {
				return fmt.Errorf("%s does not set GORELEASER_CURRENT_TAG", where)
			}
			if tag == pushedRef {
				continue
			}
			if name, isEnv := strings.CutPrefix(tag, "${{env."); isEnv && strings.HasSuffix(name, "}}") {
				name = strings.TrimSuffix(name, "}}")
				// An env value sees only the env of the scopes around it,
				// not the other entries of its own map.
				if v, _, ok := lookup(name, scope+1); ok && v == pushedRef {
					continue
				}
				return fmt.Errorf("%s sets GORELEASER_CURRENT_TAG to env.%s, which is not ${{ github.ref_name }}", where, name)
			}
			return fmt.Errorf("%s sets GORELEASER_CURRENT_TAG to %q, not the pushed tag", where, tag)
		}
	}
	if found == 0 {
		return errors.New("no step uses goreleaser/goreleaser-action")
	}
	return nil
}

func TestReleaseWorkflowBuildsPushedTag(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkReleaseTag(data); err != nil {
		t.Errorf("release.yml: %v", err)
	}
}

func TestCheckReleaseTag(t *testing.T) {
	const head = "on:\n  push:\n    tags: [\"v*\"]\n"
	tests := []struct {
		name string
		yaml string
		ok   bool
	}{
		{
			name: "ref name on the step",
			yaml: head + `jobs:
  draft:
    steps:
      - uses: goreleaser/goreleaser-action@v7
        env:
          GORELEASER_CURRENT_TAG: ${{ github.ref_name }}
`,
			ok: true,
		},
		{
			name: "workflow env through env.TAG",
			yaml: head + `env:
  TAG: ${{ github.ref_name }}
jobs:
  draft:
    steps:
      - uses: goreleaser/goreleaser-action@v7
        env:
          GORELEASER_CURRENT_TAG: ${{ env.TAG }}
`,
			ok: true,
		},
		{
			name: "job env",
			yaml: head + `jobs:
  draft:
    env:
      GORELEASER_CURRENT_TAG: ${{github.ref_name}}
    steps:
      - uses: goreleaser/goreleaser-action@v7
`,
			ok: true,
		},
		{
			name: "not set",
			yaml: head + `env:
  TAG: ${{ github.ref_name }}
jobs:
  draft:
    steps:
      - uses: goreleaser/goreleaser-action@v7
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
`,
		},
		{
			name: "another value",
			yaml: head + `jobs:
  draft:
    steps:
      - uses: goreleaser/goreleaser-action@v7
        env:
          GORELEASER_CURRENT_TAG: ${{ github.sha }}
`,
		},
		{
			name: "env variable that is not the ref name",
			yaml: head + `env:
  TAG: v1.0.0
jobs:
  draft:
    steps:
      - uses: goreleaser/goreleaser-action@v7
        env:
          GORELEASER_CURRENT_TAG: ${{ env.TAG }}
`,
		},
		{
			name: "env variable overridden beside it in the same map",
			yaml: head + `env:
  TAG: v1.0.0
jobs:
  draft:
    steps:
      - uses: goreleaser/goreleaser-action@v7
        env:
          TAG: ${{ github.ref_name }}
          GORELEASER_CURRENT_TAG: ${{ env.TAG }}
`,
		},
		{
			name: "job env variable referenced from the step",
			yaml: head + `jobs:
  draft:
    env:
      TAG: ${{ github.ref_name }}
    steps:
      - uses: goreleaser/goreleaser-action@v7
        env:
          GORELEASER_CURRENT_TAG: ${{ env.TAG }}
`,
			ok: true,
		},
		{
			name: "env variable that is not defined",
			yaml: head + `jobs:
  draft:
    steps:
      - uses: goreleaser/goreleaser-action@v7
        env:
          GORELEASER_CURRENT_TAG: ${{ env.TAG }}
`,
		},
		{
			name: "one of two GoReleaser steps not set",
			yaml: head + `jobs:
  draft:
    steps:
      - uses: goreleaser/goreleaser-action@v7
        env:
          GORELEASER_CURRENT_TAG: ${{ github.ref_name }}
      - uses: goreleaser/goreleaser-action@v7
`,
		},
		{
			name: "no GoReleaser step",
			yaml: head + `jobs:
  draft:
    steps:
      - uses: actions/checkout@v7
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkReleaseTag([]byte(tt.yaml))
			if tt.ok && err != nil {
				t.Errorf("got %v, want no error", err)
			}
			if !tt.ok && err == nil {
				t.Error("got no error, want one")
			}
		})
	}
}
