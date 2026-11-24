package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/enigma-brain/axon/internal/testkit"
)

// The tests in this file run `axon update` for real: they build axon from
// this checkout as an old release, 0.0.0, and let it replace itself with a
// release's archive. Each is off unless its variables are set.

// oldVersion is the version the built axon reports, older than any release.
const oldVersion = "0.0.0"

// buildOld compiles axon from this checkout as version oldVersion into a
// temporary directory, with extra linker flags, and returns its path.
func buildOld(t *testing.T, ldflags ...string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "axon")
	flags := strings.Join(append([]string{"-X main.version=" + oldVersion}, ldflags...), " ")
	cmd := exec.Command("go", "build", "-o", bin, "-ldflags", flags, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// runFenced runs bin with args inside testkit's fence, in an empty home
// directory, and with env added to its environment.
func runFenced(t *testing.T, bin string, env []string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := testkit.Command(t, t.TempDir(), env, bin, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

// updateAndCheck runs `axon update` with args on bin and checks that bin then
// reports version want, and that nothing is left beside it.
func updateAndCheck(t *testing.T, bin string, env []string, want string, args ...string) {
	t.Helper()
	stdout, stderr, err := runFenced(t, bin, env, append([]string{"update"}, args...)...)
	t.Logf("axon update %s stdout:\n%s", strings.Join(args, " "), stdout)
	t.Logf("axon update %s stderr:\n%s", strings.Join(args, " "), stderr)
	if err != nil {
		t.Fatalf("axon update %s: %v", strings.Join(args, " "), err)
	}
	if line := "Updated axon from " + oldVersion + " to " + want + " in " + bin; !strings.Contains(stdout, line) {
		t.Errorf("axon update did not print %q", line)
	}
	out, _, err := runFenced(t, bin, env, "--version")
	if got := strings.TrimSpace(out); err != nil || got != "axon "+want {
		t.Errorf("after the update, axon --version = %q, %v; want %q", got, err, "axon "+want)
	}
	entries, err := os.ReadDir(filepath.Dir(bin))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the update left %d files beside axon", len(entries)-1)
	}
}

// TestReleaseUpdate updates an old axon to a release whose files are on
// disk, as the release workflow's gate has them before the release is
// published. AXON_UPDATE_GATE_DIR names the directory holding the release's
// archives and checksums.txt, and AXON_UPDATE_GATE_TAG the release's tag.
//
// The files are served on the loopback address the way GitHub serves a
// release: /releases/latest redirects to the tag, and
// /releases/download/<tag>/<name> is a file. The axon under test is built
// with that address as its repository, so the update reaches no other
// server; HTTP and HTTPS proxies where nothing listens catch any request
// that is not to the loopback address.
func TestReleaseUpdate(t *testing.T) {
	dir, tag := os.Getenv("AXON_UPDATE_GATE_DIR"), os.Getenv("AXON_UPDATE_GATE_TAG")
	if dir == "" || tag == "" {
		t.Skip("set AXON_UPDATE_GATE_DIR and AXON_UPDATE_GATE_TAG to update an old axon to a release on disk")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /enigma-brain/axon/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/enigma-brain/axon/releases/tag/"+tag, http.StatusFound)
	})
	mux.HandleFunc("GET /enigma-brain/axon/releases/download/{tag}/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if r.PathValue("tag") != tag || name != filepath.Base(name) || strings.HasPrefix(name, ".") {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, name))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	repo := "-X github.com/enigma-brain/axon/internal/update.repoURL=" + srv.URL + "/enigma-brain/axon"
	env := testkit.DeadProxy()
	want := strings.TrimPrefix(tag, "v")

	t.Run("named", func(t *testing.T) {
		updateAndCheck(t, buildOld(t, repo), env, want, "--version", tag)
	})
	t.Run("latest", func(t *testing.T) {
		updateAndCheck(t, buildOld(t, repo), env, want)
	})
}

// TestRealUpdate updates an old axon from axon's real releases on GitHub.
// It runs only with AXON_REAL_UPDATE=1. AXON_UPDATE_TO names the release to
// install with --version; without it, axon update installs the latest
// release.
func TestRealUpdate(t *testing.T) {
	if os.Getenv("AXON_REAL_UPDATE") != "1" {
		t.Skip("set AXON_REAL_UPDATE=1 to update an old axon from the real releases")
	}
	bin := buildOld(t)
	if to := os.Getenv("AXON_UPDATE_TO"); to != "" {
		updateAndCheck(t, bin, nil, strings.TrimPrefix(to, "v"), "--version", to)
		return
	}
	stdout, stderr, err := runFenced(t, bin, nil, "update")
	t.Logf("axon update stdout:\n%s", stdout)
	t.Logf("axon update stderr:\n%s", stderr)
	if err != nil {
		t.Fatalf("axon update: %v", err)
	}
	m := regexp.MustCompile(`Updated axon from ` + regexp.QuoteMeta(oldVersion) + ` to (\S+) in `).FindStringSubmatch(stdout)
	if m == nil {
		t.Fatal("axon update did not report the versions it moved between")
	}
	out, _, err := runFenced(t, bin, nil, "--version")
	if got := strings.TrimSpace(out); err != nil || got != "axon "+m[1] {
		t.Errorf("after the update, axon --version = %q, %v; want %q", got, err, "axon "+m[1])
	}
}
