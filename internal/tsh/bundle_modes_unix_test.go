//go:build unix

package tsh

import (
	"archive/tar"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestBundleModesSurviveUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)

	archive := macArchive(t, "#!/bin/sh\n", true, []archiveEntry{
		// A read-only file whose directory's entry comes after it.
		{name: "teleport/tsh.app/Contents/Resources/readonly", typeflag: tar.TypeReg, mode: 0o444, body: "x"},
		{name: "teleport/tsh.app/Contents/Resources/", typeflag: tar.TypeDir, mode: 0o750},
		// A directory whose entry asks for no owner write.
		{name: "teleport/tsh.app/Contents/Library/", typeflag: tar.TypeDir, mode: 0o555},
	})
	dst := filepath.Join(t.TempDir(), "tsh.app")
	if err := extractBundle(bytes.NewReader(archive), dst); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]fs.FileMode{
		".":                                     0o755,
		"Contents":                              0o755,
		"Contents/Info.plist":                   0o644,
		"Contents/MacOS/tsh":                    0o755,
		"Contents/Resources":                    0o750,
		"Contents/Resources/readonly":           0o444,
		"Contents/Library":                      0o755,
		"Contents/_CodeSignature":               0o755,
		"Contents/_CodeSignature/CodeResources": 0o644,
	} {
		info, err := os.Stat(filepath.Join(dst, rel))
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s has mode %v, want %v", rel, got, want)
		}
	}
	// A later install can remove the bundle.
	if err := os.RemoveAll(dst); err != nil {
		t.Errorf("remove the bundle: %v", err)
	}
}

func TestBundleRootMustBeADirectory(t *testing.T) {
	archive := tarGz(t, []archiveEntry{
		{name: "teleport/tsh.app", typeflag: tar.TypeSymlink, linkname: "/"},
		{name: "teleport/tsh.app/Contents/MacOS/tsh", typeflag: tar.TypeReg, mode: 0o755, body: "#!/bin/sh\n"},
	})
	err := extractBundle(bytes.NewReader(archive), filepath.Join(t.TempDir(), "tsh.app"))
	if err == nil || !strings.Contains(err.Error(), "not a file or a directory") {
		t.Fatalf("extractBundle returned %v, want a refusal of the symlink", err)
	}
}
