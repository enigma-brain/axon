package tsh

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// macPlatforms maps the macOS platforms to the archive each one downloads.
var macPlatforms = map[string]string{
	"darwin/arm64": "teleport-v18.11.7-darwin-arm64-bin.tar.gz",
	"darwin/amd64": "teleport-v18.11.7-darwin-amd64-bin.tar.gz",
}

func TestMacInstallAfterYes(t *testing.T) {
	for p, archive := range macPlatforms {
		t.Run(p, func(t *testing.T) {
			f := newFixture(t)
			platform = p
			var out bytes.Buffer
			path, err := Ensure(context.Background(), f.dir, testProxy, strings.NewReader("y\n"), &out)
			if err != nil {
				t.Fatalf("Ensure: %v\n%s", err, out.String())
			}
			bundle := filepath.Join(f.tools, "tsh", "18.11.7", "tsh.app")
			if want := filepath.Join(bundle, "Contents", "MacOS", "tsh"); path != want {
				t.Errorf("Ensure returned %s, want %s", path, want)
			}
			if got := strings.Join(f.archiveNames, ","); got != archive {
				t.Errorf("downloaded %s, want %s", got, archive)
			}
			version, err := clientVersion(context.Background(), path)
			if err != nil || version != "18.11.7" {
				t.Errorf("installed tsh reports %q, %v; want 18.11.7", version, err)
			}
			for _, want := range []string{"Install tsh 18.11.7 into ~/.axon/tools? [Y/n]", "Downloading Teleport 18.11.7 for tsh", "100%", "Installed tsh 18.11.7"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output %q does not contain %q", out.String(), want)
				}
			}
			if got := f.tshEntries(); strings.Join(got, ",") != "18.11.7,current" {
				t.Errorf("~/.axon/tools/tsh holds %v, want [18.11.7 current]", got)
			}

			// Only the bundle is extracted, whole, with its modes.
			entries, _ := os.ReadDir(filepath.Join(f.tools, "tsh", "18.11.7"))
			if len(entries) != 1 || entries[0].Name() != "tsh.app" {
				t.Errorf("the install directory holds %v, want only tsh.app", entries)
			}
			var files []string
			filepath.WalkDir(bundle, func(p string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					rel, _ := filepath.Rel(bundle, p)
					files = append(files, filepath.ToSlash(rel))
				}
				return err
			})
			if got, want := strings.Join(files, ","), "Contents/Info.plist,Contents/MacOS/tsh,Contents/_CodeSignature/CodeResources"; got != want {
				t.Errorf("the bundle holds %s, want %s", got, want)
			}
			for rel, want := range map[string]fs.FileMode{"Contents/Info.plist": 0o644, "Contents/MacOS/tsh": 0o755} {
				info, err := os.Stat(filepath.Join(bundle, rel))
				if err != nil || info.Mode().Perm() != want {
					t.Errorf("%s: %v, %v; want mode %v", rel, info.Mode(), err, want)
				}
			}

			// The next call finds the install and downloads nothing.
			out.Reset()
			again, err := Ensure(context.Background(), f.dir, testProxy, nil, &out)
			if err != nil || again != path || out.Len() != 0 || f.archiveRequests.Load() != 1 {
				t.Errorf("second Ensure returned %s, %v with output %q after %d downloads; want %s, no output, one download",
					again, err, out.String(), f.archiveRequests.Load(), path)
			}
			if found, err := Find(f.dir); err != nil || found != path {
				t.Errorf("Find returned %s, %v; want %s", found, err, path)
			}
		})
	}
}

func TestMacDeclinedInstallStops(t *testing.T) {
	for p := range macPlatforms {
		t.Run(p, func(t *testing.T) {
			f := newFixture(t)
			platform = p
			_, err := Ensure(context.Background(), f.dir, testProxy, strings.NewReader("n\n"), io.Discard)
			if err == nil || !strings.Contains(err.Error(), "tsh was not installed") || !strings.Contains(err.Error(), InstallLink) {
				t.Fatalf("Ensure returned %v, want an error with the install link", err)
			}
			if n := f.archiveRequests.Load(); n != 0 {
				t.Errorf("%d archive requests, want none", n)
			}
		})
	}
}

func TestMacFailedInstallStops(t *testing.T) {
	cases := map[string]struct {
		set  func(*fixture)
		want string
	}{
		"checksum mismatch": {
			func(f *fixture) { f.badSum = true },
			"download Teleport 18.11.7: checksum mismatch",
		},
		"no tsh in the bundle": {
			func(f *fixture) { f.noTSH = true },
			"install tsh 18.11.7: the archive holds no teleport/tsh.app/Contents/MacOS/tsh",
		},
		"symlink in the bundle": {
			func(f *fixture) {
				f.bundleExtra = []archiveEntry{{name: "teleport/tsh.app/Contents/Frameworks", typeflag: tar.TypeSymlink, linkname: "/"}}
			},
			"which is not a file or a directory",
		},
		"hard link in the bundle": {
			func(f *fixture) {
				f.bundleExtra = []archiveEntry{{name: "teleport/tsh.app/Contents/link", typeflag: tar.TypeLink, linkname: "teleport/README.md"}}
			},
			"which is not a file or a directory",
		},
		"path out of the bundle": {
			func(f *fixture) {
				f.bundleExtra = []archiveEntry{{name: "teleport/tsh.app/../../../escaped", typeflag: tar.TypeReg, mode: 0o644, body: "x"}}
			},
			"which is outside the bundle",
		},
	}
	for name, c := range cases {
		for p := range macPlatforms {
			t.Run(name+"/"+p, func(t *testing.T) {
				f := newFixture(t)
				platform = p
				c.set(f)
				_, err := Ensure(context.Background(), f.dir, testProxy, strings.NewReader("\n"), io.Discard)
				if err == nil {
					t.Fatal("Ensure succeeded, want an error")
				}
				for _, want := range []string{c.want, InstallLink} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not contain %q", err, want)
					}
				}
				if _, _, ok, _ := installed(f.tools); ok {
					t.Error("a failed install recorded a tsh")
				}
				for _, name := range f.tshEntries() {
					t.Errorf("a failed install left %s", name)
				}
				filepath.WalkDir(filepath.Dir(filepath.Dir(f.tools)), func(p string, d fs.DirEntry, err error) error {
					if err == nil && d.Name() == "escaped" {
						t.Errorf("the install wrote %s", p)
					}
					return nil
				})
			})
		}
	}
}
