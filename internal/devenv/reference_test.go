package devenv

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

const referencePath = "../../docs/reference/devenv.md"

func TestReferenceIsCurrent(t *testing.T) {
	committed, err := os.ReadFile(referencePath)
	if err != nil {
		t.Fatalf("%v. Run \"go generate ./...\" to write it", err)
	}
	if !bytes.Equal(committed, Reference()) {
		t.Fatalf("docs/reference/devenv.md is out of date. Run \"go generate ./...\" and commit the result")
	}
}

func TestReferenceCoversEveryCommand(t *testing.T) {
	ref := string(Reference())
	for _, c := range commands {
		name := "### `axon devenv " + strings.Join(c.words, " ") + "`"
		if !strings.Contains(ref, name) {
			t.Errorf("the reference has no section %s", name)
		}
	}
}
