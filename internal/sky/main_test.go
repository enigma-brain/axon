package sky

import (
	"os"
	"testing"

	"github.com/enigma-brain/axon/internal/testkit"
)

// TestMain runs the tests inside testkit's fence.
func TestMain(m *testing.M) {
	os.Exit(testkit.Main(m, testkit.FenceOptions{Tools: fenceTools}))
}

// fenceTools are the commands the fake sky runs.
var fenceTools = []string{"sh", "env", "grep", "sleep", "sort"}
