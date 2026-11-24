// Package axondir owns ~/.axon, the one directory axon keeps on the laptop.
//
// The directory holds the config files, the settings file, the tools axon
// installs for its own use, and the state it records. Every name in it is
// defined here. Directories are created on first use, readable by the user
// only; files are written readable by the user only.
package axondir

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"
)

const (
	dirMode  fs.FileMode = 0o700
	fileMode fs.FileMode = 0o600

	settingsFile = "settings.toml"
	configsDir   = "configs"
	toolsDir     = "tools"
	stateDir     = "state"
	emailFile    = "email"
	// recordsDir, under the state directory, holds a record of each stored
	// config file.
	recordsDir = "configs"
	// lockFile, under the state directory, is the file whose lock
	// serializes changes to the stored config files.
	lockFile = "configs.lock"
)

// Dir is the axon directory under one home directory.
type Dir struct {
	root string
}

// settings is the content of the settings file.
type settings struct {
	Pathway string `toml:"pathway"`
}

// Default returns the axon directory under the user's home directory.
func Default() (Dir, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Dir{}, fmt.Errorf("find the home directory: %w", err)
	}
	return New(home), nil
}

// New returns the axon directory under home. It creates nothing.
func New(home string) Dir {
	return Dir{root: filepath.Join(home, ".axon")}
}

// Root returns the path of the axon directory.
func (d Dir) Root() string { return d.root }

// SettingsPath returns the path of the settings file, which holds the
// user's choices.
func (d Dir) SettingsPath() string { return filepath.Join(d.root, settingsFile) }

// ConfigsDir returns the directory for the config files, creating it
// when it does not exist.
func (d Dir) ConfigsDir() (string, error) { return d.ensure(configsDir) }

// ConfigsPath returns the path of the directory for the config files,
// without creating it.
func (d Dir) ConfigsPath() string { return filepath.Join(d.root, configsDir) }

// RecordsPath returns the path of the directory that holds a record of each
// stored config file, without creating it.
func (d Dir) RecordsPath() string { return filepath.Join(d.root, stateDir, recordsDir) }

// ToolsDir returns the directory for the tools axon installs, creating it
// when it does not exist.
func (d Dir) ToolsDir() (string, error) { return d.ensure(toolsDir) }

// ToolsPath returns the path of the directory for the tools axon installs,
// without creating it.
func (d Dir) ToolsPath() string { return filepath.Join(d.root, toolsDir) }

// StateDir returns the directory for the state axon records, creating it
// when it does not exist.
func (d Dir) StateDir() (string, error) { return d.ensure(stateDir) }

// StatePath returns the path of the directory for the state axon records,
// without creating it.
func (d Dir) StatePath() string { return filepath.Join(d.root, stateDir) }

// Pathway returns the identifier of the active pathway. The boolean is
// false when none is stored. The identifier is not checked against the
// config files: whether a stored file still has it is the caller's
// question.
func (d Dir) Pathway() (string, bool, error) {
	data, err := os.ReadFile(d.SettingsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", d.SettingsPath(), err)
	}
	var c settings
	if _, err := toml.Decode(string(data), &c); err != nil {
		return "", false, fmt.Errorf("read %s: %w", d.SettingsPath(), err)
	}
	if c.Pathway == "" {
		return "", false, nil
	}
	return c.Pathway, true, nil
}

// SetPathway stores id as the active pathway in the settings file.
func (d Dir) SetPathway(id string) error {
	if id == "" || strings.ContainsAny(id, "\r\n") {
		return fmt.Errorf("store the pathway: %q is not a pathway identifier", id)
	}
	if _, err := d.ensure(""); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(settings{Pathway: id}); err != nil {
		return fmt.Errorf("write %s: %w", d.SettingsPath(), err)
	}
	return ReplaceFile(d.SettingsPath(), &buf, fileMode)
}

// Email returns the recorded email of the logged-in user. The boolean is
// false when none is recorded.
func (d Dir) Email() (string, bool, error) {
	path := filepath.Join(d.StatePath(), emailFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", path, err)
	}
	email := strings.TrimSpace(string(data))
	return email, email != "", nil
}

// SetEmail records the email of the logged-in user.
func (d Dir) SetEmail(email string) error {
	email = strings.TrimSpace(email)
	if email == "" || strings.ContainsAny(email, "\r\n") {
		return fmt.Errorf("record the email: %q is not an email address", email)
	}
	dir, err := d.StateDir()
	if err != nil {
		return err
	}
	return ReplaceFile(filepath.Join(dir, emailFile), strings.NewReader(email+"\n"), fileMode)
}

// RemoveEmail deletes the recorded email, when there is one.
func (d Dir) RemoveEmail() error {
	path := filepath.Join(d.StatePath(), emailFile)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

// ensure creates the axon directory and its subdirectory sub, when they do
// not exist, and returns the subdirectory's path.
func (d Dir) ensure(sub string) (string, error) {
	path := d.root
	for _, p := range []string{d.root, filepath.Join(d.root, sub)} {
		err := os.Mkdir(p, dirMode)
		if errors.Is(err, fs.ErrExist) {
			info, statErr := os.Stat(p)
			if statErr != nil {
				return "", fmt.Errorf("create %s: %w", p, statErr)
			}
			if !info.IsDir() {
				return "", fmt.Errorf("create %s: it exists and is not a directory", p)
			}
		} else if err != nil {
			return "", fmt.Errorf("create %s: %w", p, unwrapPath(err))
		}
		path = p
	}
	return path, nil
}

// ReplaceOption changes how ReplaceFile treats the file it replaces.
type ReplaceOption func(*replaceOptions)

type replaceOptions struct {
	keepMode   bool
	followLink bool
}

// KeepMode gives the new file the permissions of the file it replaces, in
// place of the mode ReplaceFile is given. It is for a file another program
// owns, whose permissions the user may have set. The file must exist.
func KeepMode() ReplaceOption { return func(o *replaceOptions) { o.keepMode = true } }

// FollowLink replaces the file a symbolic link at path points to, so the
// link stays. Without it, a link at path is itself replaced by the file.
func FollowLink() ReplaceOption { return func(o *replaceOptions) { o.followLink = true } }

// ReplaceFile replaces path with what r holds, with the permissions perm. It
// writes a temporary file in the same directory and renames it over path,
// so a reader never sees a partial file. It syncs the file before the rename
// and the directory after it, so a replacement that returns is on disk. On
// failure path is left as it was and no temporary file remains, and the
// error reads "write <path>: <cause>", wrapping only the cause. The
// directory must exist.
func ReplaceFile(path string, r io.Reader, perm fs.FileMode, opts ...ReplaceOption) error {
	var o replaceOptions
	for _, opt := range opts {
		opt(&o)
	}
	fail := func(err error) error { return fmt.Errorf("write %s: %w", path, err) }
	target := path
	if o.followLink {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fail(unwrapPath(err))
		}
		target = resolved
	}
	if o.keepMode {
		info, err := os.Stat(target)
		if err != nil {
			return fail(unwrapPath(err))
		}
		perm = info.Mode().Perm()
	}
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".*")
	if err != nil {
		return fail(err)
	}
	defer os.Remove(tmp.Name()) // A no-op once the rename succeeds.
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fail(err)
	}
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		return fail(err)
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return fail(err)
	}
	if err := SyncDir(dir); err != nil {
		return fail(err)
	}
	return nil
}

// SyncDir syncs the directory dir, so that the names in it, as a rename
// left them, are on disk. A filesystem that does not support syncing a
// directory, such as some network and FUSE mounts, answers EINVAL, ENOTSUP
// or EOPNOTSUPP; SyncDir then reports success, since nothing more can be
// done there.
func SyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := syncFile(d); err != nil && !syncUnsupported(err) {
		d.Close()
		return err
	}
	return d.Close()
}

// syncFile syncs an open file. Tests replace it.
var syncFile = (*os.File).Sync

// syncUnsupported reports whether err says the filesystem does not support
// the sync.
func syncUnsupported(err error) bool {
	return errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP)
}
