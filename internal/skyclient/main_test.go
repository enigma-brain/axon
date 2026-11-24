package skyclient

import (
	"os"
	"testing"

	"github.com/enigma-brain/axon/internal/testkit"
)

// TestMain runs the tests inside testkit's fence.
func TestMain(m *testing.M) {
	os.Exit(testkit.Main(m, testkit.FenceOptions{Tools: fenceTools}))
}

// fenceTools are the commands the fake uv and the fake sky run.
var fenceTools = []string{"sh", "chmod", "dirname", "env", "grep", "mkdir", "sed", "sleep", "sort", "touch"}
