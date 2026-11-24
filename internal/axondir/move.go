package axondir

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// The names that axon v0.3.0 and earlier gave the files it keeps.
const (
	oldSettingsFile = "config.toml"
	oldConfigsDir   = "orgs"
	oldRecordsDir   = "orgs"
	oldLockFile     = "orgs.lock"
)

// Rename is one file or directory whose name changed after axon v0.3.0.
type Rename struct {
	// Old and New are the paths under the old name and the new one.
	Old, New string
	// OldName and NewName name the two relative to the axon directory, with
	// a trailing slash for a directory, as a message gives them.
	OldName, NewName string
}

// Moves is what MoveOldNames found and did.
type Moves struct {
	// Moved are the renames it made, in the order it made them.
	Moved []Rename
	// Both are the renames it did not make because both names exist. The
	// new one is used, and the old one is left where it is.
	Both []Rename
}

// renames returns every rename from the old names to the new ones.
func (d Dir) renames() []Rename {
	return []Rename{
		{
			Old: filepath.Join(d.root, oldConfigsDir), New: d.ConfigsPath(),
			OldName: oldConfigsDir + "/", NewName: configsDir + "/",
		},
		{
			Old: filepath.Join(d.StatePath(), oldRecordsDir), New: d.RecordsPath(),
			OldName: stateDir + "/" + oldRecordsDir + "/", NewName: stateDir + "/" + recordsDir + "/",
		},
		{
			Old: filepath.Join(d.root, oldSettingsFile), New: d.SettingsPath(),
			OldName: oldSettingsFile, NewName: settingsFile,
		},
	}
}

// MoveOldNames renames what axon v0.3.0 and earlier kept under the old
// names to the new ones: the config files directory, the records directory
// and the settings file. Their contents, the .previous and .removing files
// a stopped change left in them included, move with them. A rename whose
// new name already exists is not made and is reported in Both.
//
// It first looks for the old names without the lock, and with none to
// move it writes nothing and creates nothing. Otherwise it takes the lock
// that changes to the stored config files take, looks again, and renames
// each that is still to move, so two axon processes started together move
// each name once. A move stopped part way is finished by the next call.
func (d Dir) MoveOldNames() (Moves, error) {
	pending, err := d.scan()
	if err != nil || len(pending.Moved) == 0 {
		return pending, err
	}
	unlock, err := d.Lock()
	if err != nil {
		return Moves{}, err
	}
	defer unlock()
	todo, err := d.scan()
	if err != nil {
		return Moves{}, err
	}
	moves := Moves{Both: todo.Both}
	for _, r := range todo.Moved {
		if err := os.Rename(r.Old, r.New); err != nil {
			return moves, fmt.Errorf("rename %s to %s: %w", r.Old, r.New, unwrapPath(err))
		}
		moves.Moved = append(moves.Moved, r)
		if err := SyncDir(filepath.Dir(r.New)); err != nil {
			return moves, fmt.Errorf("rename %s to %s: %w", r.Old, r.New, unwrapPath(err))
		}
	}
	// The old lock file is empty and nothing reads it.
	os.Remove(filepath.Join(d.StatePath(), oldLockFile))
	return moves, nil
}

// scan returns, in Moved, the renames still to make: those whose old name
// exists and whose new one does not. Both holds those where both exist.
func (d Dir) scan() (Moves, error) {
	var m Moves
	for _, r := range d.renames() {
		oldExists, err := exists(r.Old)
		if err != nil {
			return Moves{}, err
		}
		if !oldExists {
			continue
		}
		newExists, err := exists(r.New)
		if err != nil {
			return Moves{}, err
		}
		if newExists {
			m.Both = append(m.Both, r)
		} else {
			m.Moved = append(m.Moved, r)
		}
	}
	return m, nil
}

// exists reports whether path names anything, without following a link.
func exists(path string) (bool, error) {
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		// A parent that is not a directory is left for the command that
		// needs it to report.
		return false, nil
	}
	return false, fmt.Errorf("read %s: %w", path, unwrapPath(err))
}
