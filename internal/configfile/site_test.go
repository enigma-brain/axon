package configfile

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// sitePage is the documentation page that explains how to write a
// config file.
var sitePage = filepath.Join("..", "..", "docs", "site", "src", "content", "docs", "config-file.md")

var tomlBlock = regexp.MustCompile("(?s)```toml\n(.*?)```")

// TestSiteExamples checks that every TOML example on the documentation
// page passes the checks of a config file, and that the page has an
// example of each kind of pathway.
func TestSiteExamples(t *testing.T) {
	t.Parallel()
	page, err := os.ReadFile(sitePage)
	if err != nil {
		t.Fatal(err)
	}
	blocks := tomlBlock.FindAllSubmatch(page, -1)
	if len(blocks) == 0 {
		t.Fatalf("%s has no ```toml example", sitePage)
	}
	seen := map[Kind]bool{}
	for i, b := range blocks {
		f, err := Parse(b[1])
		if err != nil {
			t.Errorf("example %d of %s is refused: %v\n%s", i+1, sitePage, err, b[1])
			continue
		}
		for _, p := range f.Pathways {
			seen[p.Kind] = true
		}
	}
	for _, k := range []Kind{KindDirect, KindTeleport} {
		if !seen[k] {
			t.Errorf("%s has no example of a %s pathway", sitePage, k)
		}
	}
}
