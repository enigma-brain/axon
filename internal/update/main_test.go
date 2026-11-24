package update

import (
	"os"
	"testing"

	"github.com/enigma-brain/axon/internal/testkit"
)

// TestMain runs the tests inside testkit's fence, with the commands the
// fake axon runs.
func TestMain(m *testing.M) {
	os.Exit(testkit.Main(m, testkit.FenceOptions{Tools: []string{"sleep", "touch"}}))
}
