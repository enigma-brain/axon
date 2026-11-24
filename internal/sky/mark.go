package sky

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/enigma-brain/axon/internal/axondir"
)

// projectFileVar names SkyPilot's project configuration file, which the
// client lays over the user's own settings.
const projectFileVar = "SKYPILOT_PROJECT_CONFIG"

// projectFile is the project configuration file SkyPilot reads from the
// working directory when projectFileVar is not set.
const projectFile = ".sky.yaml"

// mark is the SkyPilot configuration axon runs the client with. SkyPilot sends
// it with each request, so an organization's admin policy can tell a request
// made through axon, and puts the label on the pods.
const mark = `# Written by axon.
kubernetes:
  custom_metadata:
    labels:
      axon-client: "true"
`

// writeMark writes the marked configuration into dir's state and returns its
// path.
func writeMark(dir axondir.Dir) (string, error) {
	state, err := dir.StateDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(state, "sky-project.yaml")
	if data, err := os.ReadFile(path); err == nil && string(data) == mark {
		return path, nil
	}
	if err := axondir.ReplaceFile(path, strings.NewReader(mark), 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

// warnProject says on w which project configuration file axon's replaces.
func warnProject(w io.Writer) {
	replaced := os.Getenv(projectFileVar)
	if replaced == "" {
		if _, err := os.Stat(projectFile); err != nil {
			return
		}
		replaced = projectFile
	}
	fmt.Fprintf(w, "sky: %s is not read. Put its settings in ~/.sky/config.yaml or pass them with --config.\n", replaced)
}
