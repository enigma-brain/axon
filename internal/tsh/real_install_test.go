package tsh

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/enigma-brain/axon/internal/axondir"
)

// realInstallVersion is the Teleport release TestRealInstall installs when
// it is given no proxy to ask.
const realInstallVersion = "18.11.7"

// TestRealInstall installs tsh for real, from Teleport's archive of about
// 180 to 215 MB, into an empty home directory, then runs the installed tsh.
// It runs only when AXON_REAL_INSTALL=1, and only where axon installs tsh.
//
// The version comes from the Teleport proxy that AXON_REAL_INSTALL_PROXY
// names, a host or host:port, when it is set. Otherwise a local stand-in
// for a proxy reports realInstallVersion, and only the download, the
// checksum and the install are real.
func TestRealInstall(t *testing.T) {
	if os.Getenv("AXON_REAL_INSTALL") != "1" {
		t.Skip("set AXON_REAL_INSTALL=1 to install tsh for real")
	}
	proxyAddr := os.Getenv("AXON_REAL_INSTALL_PROXY")
	if proxyAddr == "" {
		proxyAddr = "teleport.example.org"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"server_version":%q}`, realInstallVersion)
		}))
		t.Cleanup(srv.Close)
		setVar(t, &pingURL, fixedURL(srv.URL+"/webapi/ping"))
	}
	if !canInstall() {
		t.Skipf("axon does not install tsh on %s", platform)
	}
	if path, err := exec.LookPath("tsh"); err == nil {
		t.Fatalf("a tsh is on PATH at %s; the test needs PATH without one", path)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := axondir.New(home)
	ctx := context.Background()

	proxy, err := proxyVersion(ctx, proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("the proxy runs %s", proxy)

	var progress bytes.Buffer
	path, err := Ensure(ctx, dir, proxyAddr, strings.NewReader("y\n"), &progress)
	t.Log(progress.String())
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != ".axon" {
			t.Errorf("the install created %s outside ~/.axon", e.Name())
		}
	}

	out, err := exec.CommandContext(ctx, path, "version", "--client", "--format=json").CombinedOutput()
	if err != nil {
		t.Fatalf("tsh version: %v\n%s", err, out)
	}
	t.Logf("tsh version: %s", bytes.TrimSpace(out))
	version, err := clientVersion(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if version != proxy {
		t.Errorf("the installed tsh reports %s, want the proxy's %s", version, proxy)
	}

	// A second call downloads nothing: any download would fail against this
	// address, and nothing is printed.
	setVar(t, &cdnBaseURL, "http://127.0.0.1:1/unreachable")
	progress.Reset()
	again, err := Ensure(ctx, dir, proxyAddr, nil, &progress)
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if again != path || progress.Len() != 0 {
		t.Errorf("second Ensure returned %s with output %q; want %s and no output", again, progress.String(), path)
	}
}
