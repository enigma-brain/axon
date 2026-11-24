package configfile

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/enigma-brain/axon/internal/axondir"
)

// put checks data and stores it as opts say. When id is not empty, data must
// be the file id, and data identical to the stored file is not written
// again. The boolean reports whether the stored file changed.
func (s Store) put(data []byte, opts PutOptions, id string) (File, bool, error) {
	f, err := Parse(data)
	if err != nil {
		return File{}, false, err
	}
	if id != "" && f.ID != id {
		return File{}, false, &CheckError{Field: "id", Check: fmt.Sprintf("is %q, not %q, the id of the stored file", f.ID, id)}
	}
	unlock, err := lockStore(s.dir)
	if err != nil {
		return File{}, false, err
	}
	defer unlock()
	if _, err := s.recoverPrevious(); err != nil {
		return File{}, false, err
	}
	change, err := s.inspect(f, opts.Origin.HandMade)
	if err != nil {
		return File{}, false, err
	}
	stored, _, err := s.List()
	if err != nil {
		return File{}, false, err
	}
	remove := map[string]bool{}
	for _, r := range opts.Remove {
		var current *File
		for i := range stored {
			if stored[i].ID == r.ID {
				current = &stored[i]
			}
		}
		if current == nil || !reflect.DeepEqual(*current, r) {
			return File{}, false, &ChangedError{Path: s.Path(r.ID)}
		}
		remove[r.ID] = true
	}
	for _, c := range change.Clashes {
		if c.SameName && !remove[c.Stored.ID] {
			return File{}, false, &ClashError{Clash: c, Path: s.Path(c.Stored.ID)}
		}
	}
	if change.Lower() && !opts.AllowLower {
		return File{}, false, &LowerRevisionError{Stored: change.Previous.Revision, New: f.Revision}
	}

	configs, err := s.dir.ConfigsDir()
	if err != nil {
		return File{}, false, err
	}
	path := filepath.Join(configs, f.ID+fileExt)
	var previous []byte
	info, err := os.Stat(path)
	hadPrevious := err == nil
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return File{}, false, fmt.Errorf("read %s: %w", path, axondir.UnwrapPath(err))
	case !info.Mode().IsRegular():
		return File{}, false, fmt.Errorf("replace %s: it is not a regular file", path)
	default:
		if previous, err = os.ReadFile(path); err != nil {
			return File{}, false, fmt.Errorf("read %s: %w", path, axondir.UnwrapPath(err))
		}
	}
	if id != "" && !hadPrevious {
		return File{}, false, fmt.Errorf("replace %s: it is no longer stored", path)
	}
	if id != "" && bytes.Equal(previous, data) {
		if cur, ok, err := s.Source(id); err == nil && ok && cur.raw() == opts.Origin.Source.raw() {
			return f, false, nil
		}
	}
	// A failure at any step puts everything back, so the stored files and
	// records end either all as they were or all as they are meant to
	// become. The stored file being replaced is first kept beside itself,
	// at path+previousExt, and stays in place until the new file replaces
	// it in one rename, so it is never missing and its previous content is
	// on disk throughout. The record being replaced is kept in memory.
	// Each file to remove is first renamed aside, beside itself, and
	// deleted only once everything else has succeeded, so its content stays
	// on disk until then.
	var saved []savedFile
	if hadPrevious {
		if err := keepPrevious(path); err != nil {
			return File{}, false, err
		}
	} else {
		saved = append(saved, savedFile{path: path})
	}
	sf, err := saveFile(s.recordPath(f.ID))
	if err != nil {
		if hadPrevious {
			removeFile(path + previousExt)
		}
		return File{}, false, err
	}
	saved = append(saved, sf)
	var aside []string // paths renamed aside, each now at path+removingExt
	fail := func(err error) (File, bool, error) {
		// Every step is undone, even after one fails, and each failure is
		// reported.
		var undone []string
		for i := len(aside) - 1; i >= 0; i-- {
			if undo := renameFile(aside[i]+removingExt, aside[i]); undo != nil {
				undone = append(undone, fmt.Sprintf("%s could not be put back from %s: %v", aside[i], aside[i]+removingExt, axondir.UnwrapPath(undo)))
			}
		}
		if hadPrevious {
			if undo := renameFile(path+previousExt, path); undo != nil {
				undone = append(undone, fmt.Sprintf("%s could not be put back from %s: %v", path, path+previousExt, axondir.UnwrapPath(undo)))
			} else {
				// Before the new file is written, the copy is a hard
				// link to the stored file itself, and renaming one link
				// over the other leaves both.
				removeFile(path + previousExt)
			}
		}
		for i := len(saved) - 1; i >= 0; i-- {
			if undo := saved[i].restore(); undo != nil {
				undone = append(undone, fmt.Sprintf("%s could not be restored: %v", saved[i].path, undo))
			}
		}
		if len(undone) > 0 {
			return File{}, false, fmt.Errorf("%w; and %s", err, strings.Join(undone, "; "))
		}
		return File{}, false, err
	}

	for _, r := range opts.Remove {
		if r.ID == f.ID {
			continue
		}
		for _, p := range []string{s.Path(r.ID), s.recordPath(r.ID)} {
			err := renameFile(p, p+removingExt)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return fail(fmt.Errorf("remove %s: %w", p, axondir.UnwrapPath(err)))
			}
			aside = append(aside, p)
		}
	}
	if err := replaceFile(path, data); err != nil {
		return fail(err)
	}
	if err := s.writeRecord(f.ID, opts.Origin, data); err != nil {
		return fail(err)
	}
	// The put has succeeded. A file left aside because its deletion
	// fails is ignored by every reader, and is safe to delete; a previous
	// copy left behind is deleted by the next recovery, since the record
	// matches the stored file.
	for _, p := range aside {
		removeFile(p + removingExt)
	}
	if hadPrevious {
		removeFile(path + previousExt)
	}
	return f, true, nil
}
