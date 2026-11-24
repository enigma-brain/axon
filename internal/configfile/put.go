package configfile

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
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
	unlock, err := s.d.lockStore(s.dir)
	if err != nil {
		return File{}, false, err
	}
	defer unlock()
	if _, err := s.recoverPrevious(); err != nil {
		return File{}, false, err
	}
	p, err := s.planPut(f, data, opts, id)
	if err != nil {
		return File{}, false, err
	}
	if !p.write {
		return f, false, nil
	}
	if err := s.applyPut(p); err != nil {
		return File{}, false, err
	}
	return f, true, nil
}

// putPlan is what a put does to the stored files, decided before any of it
// is done.
type putPlan struct {
	// file is the checked file being stored, and data is its bytes.
	file File
	data []byte
	// origin is where the file comes from, for its record.
	origin Origin
	// change is the comparison with the stored files: the stored file the
	// file updates, and the clashes callers ask about before the put.
	change Change
	// path is where the file is stored.
	path string
	// replace reports that a stored file is in place at path.
	replace bool
	// remove holds the ids of the other stored files to remove, in the order
	// the put was asked to remove them.
	remove []string
	// write is false when the put writes nothing, because data is the stored
	// file byte for byte and has the recorded source.
	write bool
}

// planPut decides, with the store's lock held, what storing f, whose bytes
// are data, does as opts say, writing nothing. It lists the stored files
// once and returns the refusal when the put is refused. When id is not
// empty, the stored file id is being refreshed.
func (s Store) planPut(f File, data []byte, opts PutOptions, id string) (putPlan, error) {
	stored, _, err := s.List()
	if err != nil {
		return putPlan{}, err
	}
	change, err := s.compare(f, opts.Origin.HandMade, stored)
	if err != nil {
		return putPlan{}, err
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
			return putPlan{}, &ChangedError{Path: s.Path(r.ID)}
		}
		remove[r.ID] = true
	}
	for _, c := range change.Clashes {
		if c.SameName && !remove[c.Stored.ID] {
			return putPlan{}, &ClashError{Clash: c, Path: s.Path(c.Stored.ID)}
		}
	}
	if change.Lower() && !opts.AllowLower {
		return putPlan{}, &LowerRevisionError{Stored: change.Previous.Revision, New: f.Revision}
	}

	p := putPlan{file: f, data: data, origin: opts.Origin, change: change, path: s.Path(f.ID), write: true}
	for _, r := range opts.Remove {
		if r.ID != f.ID {
			p.remove = append(p.remove, r.ID)
		}
	}
	var previous []byte
	info, err := os.Stat(p.path)
	p.replace = err == nil
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return putPlan{}, fmt.Errorf("read %s: %w", p.path, axondir.UnwrapPath(err))
	case !info.Mode().IsRegular():
		return putPlan{}, fmt.Errorf("replace %s: it is not a regular file", p.path)
	default:
		if previous, err = os.ReadFile(p.path); err != nil {
			return putPlan{}, fmt.Errorf("read %s: %w", p.path, axondir.UnwrapPath(err))
		}
	}
	if id != "" && !p.replace {
		return putPlan{}, fmt.Errorf("replace %s: it is no longer stored", p.path)
	}
	if id != "" && bytes.Equal(previous, data) {
		if cur, ok, err := s.Source(id); err == nil && ok && cur.raw() == opts.Origin.Source.raw() {
			p.write = false
		}
	}
	return p, nil
}

// applyPut carries out p, which planPut returned with the store's lock still
// held, as one transaction.
func (s Store) applyPut(p putPlan) error {
	if _, err := s.dir.ConfigsDir(); err != nil {
		return err
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
	u := &putUndo{d: s.d, path: p.path, replace: p.replace}
	if p.replace {
		if err := s.d.keepPrevious(p.path); err != nil {
			return err
		}
	} else {
		u.saved = append(u.saved, savedFile{path: p.path})
	}
	sf, err := saveFile(s.recordPath(p.file.ID))
	if err != nil {
		if p.replace {
			s.d.removeFile(p.path + previousExt)
		}
		return err
	}
	u.saved = append(u.saved, sf)

	for _, id := range p.remove {
		for _, path := range []string{s.Path(id), s.recordPath(id)} {
			err := s.d.renameFile(path, path+removingExt)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return u.undo(fmt.Errorf("remove %s: %w", path, axondir.UnwrapPath(err)))
			}
			u.aside = append(u.aside, path)
		}
	}
	if err := s.d.replaceFile(p.path, p.data); err != nil {
		return u.undo(err)
	}
	if err := s.writeRecord(p.file.ID, p.origin, p.data); err != nil {
		return u.undo(err)
	}
	u.finish()
	return nil
}

// putUndo is what a put has changed so far, so that a failure can put it
// all back.
type putUndo struct {
	// d holds the file operations the put makes.
	d deps
	// path is the stored file's path. When replace is set, a stored file
	// was in place there and is kept at path+previousExt.
	path    string
	replace bool
	// saved holds the content of files before the put changed them.
	saved []savedFile
	// aside holds the paths renamed aside, each now at path+removingExt.
	aside []string
}

// undo puts back every step, even after one fails, and returns err with
// each failure to put a step back.
func (u *putUndo) undo(err error) error {
	var undone []string
	for i := len(u.aside) - 1; i >= 0; i-- {
		if undo := u.d.renameFile(u.aside[i]+removingExt, u.aside[i]); undo != nil {
			undone = append(undone, fmt.Sprintf("%s could not be put back from %s: %v", u.aside[i], u.aside[i]+removingExt, axondir.UnwrapPath(undo)))
		}
	}
	if u.replace {
		if undo := u.d.renameFile(u.path+previousExt, u.path); undo != nil {
			undone = append(undone, fmt.Sprintf("%s could not be put back from %s: %v", u.path, u.path+previousExt, axondir.UnwrapPath(undo)))
		} else {
			// Before the new file is written, the copy is a hard link to
			// the stored file itself, and renaming one link over the
			// other leaves both.
			u.d.removeFile(u.path + previousExt)
		}
	}
	for i := len(u.saved) - 1; i >= 0; i-- {
		if undo := u.saved[i].restore(u.d); undo != nil {
			undone = append(undone, fmt.Sprintf("%s could not be restored: %v", u.saved[i].path, undo))
		}
	}
	if len(undone) > 0 {
		return fmt.Errorf("%w; and %s", err, strings.Join(undone, "; "))
	}
	return err
}

// finish deletes, once the put has succeeded, the files renamed aside and
// the previous copy. A file left aside because its deletion fails is
// ignored by every reader, and is safe to delete; a previous copy left
// behind is deleted by the next recovery, since the record matches the
// stored file.
func (u *putUndo) finish() {
	for _, p := range u.aside {
		u.d.removeFile(p + removingExt)
	}
	if u.replace {
		u.d.removeFile(u.path + previousExt)
	}
}
