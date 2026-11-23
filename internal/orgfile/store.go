package orgfile

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/enigma-brain/axon/internal/axondir"
)

const (
	fileExt = ".toml"
	// stateSub is the subdirectory of axon's state directory that records
	// each stored file's source.
	stateSub = "orgs"
)

// Store is the set of config files kept in an axon directory: each
// file in ~/.axon/orgs/<id>.toml, and its source in
// ~/.axon/state/orgs/<id>.toml.
type Store struct {
	dir axondir.Dir
}

// NewStore returns the store in dir. It creates nothing.
func NewStore(dir axondir.Dir) Store { return Store{dir: dir} }

// Path returns the path of the stored file with the given id.
func (s Store) Path(id string) string {
	return filepath.Join(s.dir.Root(), "orgs", id+fileExt)
}

// Problem is a stored file that List left out, and why.
type Problem struct {
	// Path is the stored file's path.
	Path string
	// Err is the reason it was left out.
	Err error
}

func (p Problem) Error() string { return p.Path + ": " + p.Err.Error() }

// sourceRecord is the content of a file's source record.
type sourceRecord struct {
	Source string `toml:"source"`
}

// Add checks data as a config file from src and stores it, byte for
// byte, replacing the stored file with the same id, and records src as its
// source. It refuses data that fails the checks or that has a pathway
// identifier held by another stored file. A refused file writes nothing.
//
// Adds are serialized by a lock, held from the check against the stored
// files to the source record, so two adds cannot both store a file that
// claims the same pathway identifier.
func (s Store) Add(data []byte, src Source) (File, error) {
	f, _, err := s.put(data, src, "")
	return f, err
}

// Refresh replaces the stored file id with data, fetched again from src,
// as Add does, and refuses data whose id is not id. When data is the stored
// file byte for byte, it writes nothing and reports no change. The boolean
// reports whether the stored file changed.
func (s Store) Refresh(id string, data []byte, src Source) (File, bool, error) {
	return s.put(data, src, id)
}

// put checks data and stores it from src. When id is not empty, data must be
// the file id, and data identical to the stored file is not written again.
// The boolean reports whether the stored file changed.
func (s Store) put(data []byte, src Source, id string) (File, bool, error) {
	f, err := Parse(data)
	if err != nil {
		return File{}, false, err
	}
	if id != "" && f.ID != id {
		return File{}, false, &CheckError{Field: "id", Check: fmt.Sprintf("is %q, not %q, the id of the stored file", f.ID, id)}
	}
	unlock, err := s.lock()
	if err != nil {
		return File{}, false, err
	}
	defer unlock()
	stored, _, err := s.List()
	if err != nil {
		return File{}, false, err
	}
	for _, other := range stored {
		if other.ID == f.ID {
			continue
		}
		taken := map[string]bool{}
		for _, p := range other.Pathways {
			taken[p.ID] = true
		}
		for _, p := range f.Pathways {
			if taken[p.ID] {
				return File{}, false, &CheckError{
					Field: fmt.Sprintf("pathway %q: id", p.ID),
					Check: fmt.Sprintf("already used by the stored file %s", s.Path(other.ID)),
				}
			}
		}
	}

	orgs, err := s.dir.OrgsDir()
	if err != nil {
		return File{}, false, err
	}
	path := filepath.Join(orgs, f.ID+fileExt)
	var previous []byte
	info, err := os.Stat(path)
	hadPrevious := err == nil
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return File{}, false, fmt.Errorf("read %s: %w", path, unwrapPath(err))
	case !info.Mode().IsRegular():
		return File{}, false, fmt.Errorf("replace %s: it is not a regular file", path)
	default:
		if previous, err = os.ReadFile(path); err != nil {
			return File{}, false, fmt.Errorf("read %s: %w", path, unwrapPath(err))
		}
	}
	if id != "" && !hadPrevious {
		return File{}, false, fmt.Errorf("replace %s: it is no longer stored", path)
	}
	if id != "" && bytes.Equal(previous, data) {
		if cur, ok, err := s.Source(id); err == nil && ok && cur.String() == src.String() {
			return f, false, nil
		}
	}
	if err := axondir.ReplaceFile(path, data); err != nil {
		return File{}, false, err
	}
	if err := s.recordSource(f.ID, src); err != nil {
		// Put the stored file back as it was, so that it and its
		// recorded source still belong together.
		var undo error
		if hadPrevious {
			undo = axondir.ReplaceFile(path, previous)
		} else {
			undo = os.Remove(path)
		}
		if undo != nil {
			return File{}, false, fmt.Errorf("%w; and %s could not be restored: %v", err, path, undo)
		}
		return File{}, false, err
	}
	return f, true, nil
}

// recordSource writes src as the source of the stored file id.
func (s Store) recordSource(id string, src Source) error {
	state, err := s.dir.StateDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(state, stateSub)
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(sourceRecord{Source: src.String()}); err != nil {
		return fmt.Errorf("record the source of %s: %w", id, err)
	}
	return axondir.ReplaceFile(filepath.Join(dir, id+fileExt), buf.Bytes())
}

// Source returns the recorded source of the stored file id. The boolean is
// false when none is recorded.
func (s Store) Source(id string) (Source, bool, error) {
	path := filepath.Join(s.dir.Root(), "state", stateSub, id+fileExt)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Source{}, false, nil
	}
	if err != nil {
		return Source{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	var rec sourceRecord
	if _, err := toml.Decode(string(data), &rec); err != nil {
		return Source{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	src, err := ParseSource(rec.Source)
	if err != nil {
		return Source{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	return src, true, nil
}

// List returns every stored file that passes the checks, in the order of
// their identifiers, each with its pathways in the file's order. A stored
// file is left out, and reported as a Problem, when it fails the checks,
// when its id is not its file name, or when it has a pathway identifier
// that a file earlier in that order already has. Files whose names are not
// <id>.toml for a valid id are ignored.
func (s Store) List() ([]File, []Problem, error) {
	dir := filepath.Join(s.dir.Root(), "orgs")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", dir, err)
	}

	var ids []string
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), fileExt)
		if ok && fileIDPattern.MatchString(id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	var (
		files    []File
		problems []Problem
		owner    = map[string]string{} // pathway id -> file id
	)
	for _, id := range ids {
		path := filepath.Join(dir, id+fileExt)
		f, err := readStored(path, id)
		if err == nil {
			for _, p := range f.Pathways {
				if first, ok := owner[p.ID]; ok {
					err = &CheckError{
						Field: fmt.Sprintf("pathway %q: id", p.ID),
						Check: fmt.Sprintf("already used by the stored file %s", filepath.Join(dir, first+fileExt)),
					}
					break
				}
			}
		}
		if err != nil {
			problems = append(problems, Problem{Path: path, Err: err})
			continue
		}
		for _, p := range f.Pathways {
			owner[p.ID] = f.ID
		}
		files = append(files, f)
	}
	return files, problems, nil
}

// readStored reads and checks the stored file at path, whose name says its
// id is id.
func readStored(path, id string) (File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return File{}, err
	}
	if !info.Mode().IsRegular() {
		return File{}, errors.New("not a regular file")
	}
	if info.Size() > MaxSize {
		return File{}, fmt.Errorf("the file is larger than %d bytes", MaxSize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, unwrapPath(err)
	}
	f, err := Parse(data)
	if err != nil {
		return File{}, err
	}
	if f.ID != id {
		return File{}, &CheckError{Field: "id", Check: fmt.Sprintf("is %q, which does not match the file name", f.ID)}
	}
	return f, nil
}
