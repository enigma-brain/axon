package tsh

import (
	"os"
	"testing"

	"github.com/enigma-brain/axon/internal/testkit"
)

// TestMain runs the tests inside testkit's fence.
func TestMain(m *testing.M) {
	os.Exit(testkit.Main(m, testkit.FenceOptions{}))
}
