package testkit

import (
	"os/exec"
	"strings"
	"testing"
)

// TestNotInTheBinary checks that no package the axon binary is built from
// imports testkit, so that test scaffolding never ships.
func TestNotInTheBinary(t *testing.T) {
	t.Parallel()
	out, err := exec.Command("go", "list", "-deps", "github.com/enigma-brain/axon").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, pkg := range strings.Fields(string(out)) {
		if strings.HasPrefix(pkg, "github.com/enigma-brain/axon/internal/testkit") {
			t.Errorf("the axon binary depends on %s", pkg)
		}
	}
}
