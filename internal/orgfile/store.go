package orgfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

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

// record is the content of a stored file's record. A record written by an
// earlier axon holds only the source.
type record struct {
	Source   string `toml:"source,omitempty"`
	HandMade bool   `toml:"hand_made,omitempty"`
	StoredAt string `toml:"stored_at,omitempty"`
	SHA256   string `toml:"sha256,omitempty"`
}

// Record is what axon records about a stored file.
type Record struct {
	// Source is where the file came from. It is the zero Source for a
	// hand-made file.
	Source Source
	// HandMade reports that axon login wrote the file from answers to its
	// questions or from its flags.
	HandMade bool
	// StoredAt is when the file was stored, and is zero in a record
	// written by an earlier axon.
	StoredAt time.Time
	// SHA256 is the hex SHA-256 of the stored file's bytes, and is empty in
	// a record written by an earlier axon.
	SHA256 string
}

// Origin says where a file being stored comes from: a source, or axon
// login itself for a hand-made file.
type Origin struct {
	Source   Source
	HandMade bool
}

// FromSource is the origin of a config file read from src.
func FromSource(src Source) Origin { return Origin{Source: src} }

// HandMade is the origin of a file axon login writes itself.
var HandMade = Origin{HandMade: true}

// Clash is a stored pathway, from a file other than the one being stored,
// that a pathway being stored clashes with.
type Clash struct {
	// Stored is the stored file that holds the stored pathway.
	Stored File
	// StoredHandMade reports that the stored file is hand-made.
	StoredHandMade bool
	// Pathway is the stored pathway.
	Pathway Pathway
	// Added is the pathway being stored that clashes with it.
	Added Pathway
	// SameName reports that the two pathways have the same name, ignoring
	// case, or the same identifier. Otherwise they have the same server.
	// Two stored pathways never share a name, so a name clash is resolved
	// only by removing the stored file.
	SameName bool
	// SameFileID reports that the stored file has the id of the file being
	// stored, and one of the two is hand-made, or both are. The stored file
	// is then removed, or the file is not stored.
	SameFileID bool
}

// Change is what storing a file would do to the stored files.
type Change struct {
	// File is the checked file being stored.
	File File
	// Previous is the stored config file the file updates, when one with
	// the same id is stored and neither is hand-made.
	Previous *File
	// Clashes are the stored pathways the file's pathways clash with, file
	// by file in the order of the stored files' identifiers.
	Clashes []Clash
}

// Lower reports whether the change updates a stored file to a lower
// revision: both have a revision, and the new one is lower.
func (c Change) Lower() bool {
	return c.Previous != nil && c.Previous.HasRevision && c.File.HasRevision && c.File.Revision < c.Previous.Revision
}

// ClashError refuses to store a file that has the same pathway name or
// identifier as a stored pathway in another file that was not removed.
type ClashError struct {
	Clash Clash
	// Path is the stored file's path.
	Path string
}

func (e *ClashError) Error() string {
	if e.Clash.SameFileID {
		return fmt.Sprintf("id: already used by the stored file %s", e.Path)
	}
	return fmt.Sprintf("pathway %q: already used by the stored file %s, whose pathway %q has the same name or identifier", e.Clash.Added.ID, e.Path, e.Clash.Pathway.ID)
}

// ChangedError refuses to remove a stored file that changed after the
// user agreed to remove it.
type ChangedError struct {
	// Path is the stored file's path.
	Path string
}

func (e *ChangedError) Error() string {
	return fmt.Sprintf("%s changed after you were asked about it. Run the command again.", e.Path)
}

// LowerRevisionError refuses to replace a stored file with a lower
// revision of it unless the replacement allows it.
type LowerRevisionError struct {
	Stored, New int64
}

func (e *LowerRevisionError) Error() string {
	return fmt.Sprintf("revision: %d is lower than the stored revision %d", e.New, e.Stored)
}

// PutOptions are the choices made about storing one file.
type PutOptions struct {
	// Origin is where the file comes from.
	Origin Origin
	// Remove holds the stored files to remove when the file is stored, as
	// Inspect returned them. A file that has changed since is not removed,
	// and nothing is stored.
	Remove []File
	// AllowLower allows a lower revision to replace the stored copy.
	AllowLower bool
}

// Inspect checks data as a file being stored and compares it with the
// stored files, writing nothing. It is what Put will find, unless the
// stored files change in between.
func (s Store) Inspect(data []byte, handMade bool) (Change, error) {
	f, err := Parse(data)
	if err != nil {
		return Change{}, err
	}
	return s.inspect(f, handMade)
}

func (s Store) inspect(f File, handMade bool) (Change, error) {
	stored, _, err := s.List()
	if err != nil {
		return Change{}, err
	}
	c := Change{File: f}
	for _, other := range stored {
		rec, _, err := s.Record(other.ID)
		if err != nil {
			return Change{}, err
		}
		if other.ID == f.ID && !handMade && !rec.HandMade {
			prev := other
			c.Previous = &prev
			continue
		}
		var nameClash bool
		for _, sp := range other.Pathways {
			for _, ap := range f.Pathways {
				name := sp.ID == ap.ID || strings.EqualFold(strings.TrimSpace(sp.Name), strings.TrimSpace(ap.Name))
				if name || SameServer(sp, ap) {
					c.Clashes = append(c.Clashes, Clash{Stored: other, StoredHandMade: rec.HandMade, Pathway: sp, Added: ap, SameName: name})
					nameClash = nameClash || name
				}
			}
		}
		if other.ID == f.ID && !nameClash {
			// Two files with one id cannot both be stored: a config file
			// and a hand-made file, or two hand-made files.
			c.Clashes = append(c.Clashes, Clash{Stored: other, StoredHandMade: rec.HandMade, Pathway: other.Pathways[0], Added: f.Pathways[0], SameName: true, SameFileID: true})
		}
	}
	return c, nil
}

// SameServer reports whether a and b reach the same server: the same API
// server address on a direct pathway, or the same Teleport proxy and
// application on a teleport pathway.
func SameServer(a, b Pathway) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case KindDirect:
		return sameEndpoint(a.Sky.Endpoint, b.Sky.Endpoint)
	case KindTeleport:
		return a.Teleport != nil && b.Teleport != nil && sameProxy(a.Teleport.Proxy, b.Teleport.Proxy) && a.Sky.App == b.Sky.App
	}
	return false
}

// sameEndpoint reports whether the https addresses a and b name the same
// API server: the same scheme and host, ignoring case, and the same path,
// ignoring a trailing slash.
func sameEndpoint(a, b string) bool {
	norm := func(e string) string {
		e = strings.TrimRight(e, "/")
		u, err := url.Parse(e)
		if err != nil {
			return e
		}
		u.Scheme = strings.ToLower(u.Scheme)
		u.Host = strings.ToLower(u.Host)
		return u.String()
	}
	return norm(a) == norm(b)
}

// sameProxy reports whether the proxy addresses a and b name the same
// proxy: the same host, ignoring case, and the same port, 443 when one
// names none.
func sameProxy(a, b string) bool {
	split := func(p string) (string, string) {
		if i := strings.LastIndexByte(p, ':'); i >= 0 {
			return strings.ToLower(p[:i]), p[i+1:]
		}
		return strings.ToLower(p), "443"
	}
	ah, ap := split(a)
	bh, bp := split(b)
	return ah == bh && ap == bp
}

// Add checks data as a config file from src and stores it, byte for
// byte, replacing the stored file with the same id, and records src as its
// source. It refuses data that fails the checks, that has the same pathway
// name or identifier as another stored file, or that lowers the stored
// copy's revision. A refused file writes nothing.
func (s Store) Add(data []byte, src Source) (File, error) {
	f, _, err := s.put(data, PutOptions{Origin: FromSource(src)}, "")
	return f, err
}

// Put checks data and stores it as opts say: it removes the stored files
// opts.Remove names, and stores data in place of the stored file with the
// same id. It refuses data that fails the checks, that still clashes by
// name or identifier with a stored file once those are removed, or that
// lowers the stored copy's revision without opts.AllowLower. A refused file
// writes nothing.
//
// Puts are serialized by a lock, held from the comparison with the stored
// files to the record, so two puts cannot both store a file that claims
// the same pathway.
func (s Store) Put(data []byte, opts PutOptions) (File, error) {
	f, _, err := s.put(data, opts, "")
	return f, err
}

// Refresh replaces the stored file id with data, fetched again from src,
// as Add does, and refuses data whose id is not id. When data is the stored
// file byte for byte, it writes nothing and reports no change. The boolean
// reports whether the stored file changed.
func (s Store) Refresh(id string, data []byte, src Source, allowLower bool) (File, bool, error) {
	return s.put(data, PutOptions{Origin: FromSource(src), AllowLower: allowLower}, id)
}

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
	unlock, err := s.lock()
	if err != nil {
		return File{}, false, err
	}
	defer unlock()
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
		if cur, ok, err := s.Source(id); err == nil && ok && cur.String() == opts.Origin.Source.String() {
			return f, false, nil
		}
	}
	// Everything the put changes is kept as it was, so that a failure at
	// any step puts it all back: the stored files and records stay either
	// all as they were or all as they are meant to become.
	paths := []string{path, s.recordPath(f.ID)}
	for _, r := range opts.Remove {
		if r.ID != f.ID {
			paths = append(paths, s.Path(r.ID), s.recordPath(r.ID))
		}
	}
	var saved []savedFile
	for _, p := range paths {
		sf, err := saveFile(p)
		if err != nil {
			return File{}, false, err
		}
		saved = append(saved, sf)
	}
	fail := func(err error) (File, bool, error) {
		for i := len(saved) - 1; i >= 0; i-- {
			if undo := saved[i].restore(); undo != nil {
				return File{}, false, fmt.Errorf("%w; and %s could not be restored: %v", err, saved[i].path, undo)
			}
		}
		return File{}, false, err
	}

	if err := axondir.ReplaceFile(path, data); err != nil {
		return fail(err)
	}
	if err := s.writeRecord(f.ID, opts.Origin, data); err != nil {
		return fail(err)
	}
	for _, r := range opts.Remove {
		if r.ID == f.ID {
			continue
		}
		if err := s.remove(r.ID); err != nil {
			return fail(err)
		}
	}
	return f, true, nil
}

// savedFile is a file's content before a put changed it.
type savedFile struct {
	path    string
	data    []byte
	existed bool
}

// saveFile keeps the content of the file at path, which need not exist.
func saveFile(path string) (savedFile, error) {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return savedFile{path: path}, nil
	case err != nil:
		return savedFile{}, fmt.Errorf("read %s: %w", path, unwrapPath(err))
	}
	return savedFile{path: path, data: data, existed: true}, nil
}

// restore puts the file back as it was saved.
func (f savedFile) restore() error {
	if f.existed {
		return axondir.ReplaceFile(f.path, f.data)
	}
	if err := os.Remove(f.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// removeFile removes a file. Tests replace it to make a removal fail.
var removeFile = os.Remove

// remove deletes the stored file id and its record.
func (s Store) remove(id string) error {
	for _, path := range []string{s.Path(id), s.recordPath(id)} {
		if err := removeFile(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", path, unwrapPath(err))
		}
	}
	return nil
}

func (s Store) recordPath(id string) string {
	return filepath.Join(s.dir.Root(), "state", stateSub, id+fileExt)
}

// writeRecord records the origin of the stored file id, when it was stored
// and the fingerprint of its bytes, data.
func (s Store) writeRecord(id string, origin Origin, data []byte) error {
	state, err := s.dir.StateDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(state, stateSub)
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	sum := sha256.Sum256(data)
	rec := record{
		HandMade: origin.HandMade,
		StoredAt: now().UTC().Format(time.RFC3339),
		SHA256:   hex.EncodeToString(sum[:]),
	}
	if !origin.HandMade {
		rec.Source = origin.Source.String()
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(rec); err != nil {
		return fmt.Errorf("record the source of %s: %w", id, err)
	}
	return axondir.ReplaceFile(filepath.Join(dir, id+fileExt), buf.Bytes())
}

// now is the clock a record's time is read from. Tests replace it.
var now = time.Now

// Record returns what is recorded about the stored file id. The boolean is
// false when nothing is recorded.
func (s Store) Record(id string) (Record, bool, error) {
	path := s.recordPath(id)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	var raw record
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return Record{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	rec := Record{HandMade: raw.HandMade, SHA256: raw.SHA256}
	if !raw.HandMade {
		if rec.Source, err = ParseSource(raw.Source); err != nil {
			return Record{}, false, fmt.Errorf("read %s: %w", path, err)
		}
	}
	if raw.StoredAt != "" {
		if rec.StoredAt, err = time.Parse(time.RFC3339, raw.StoredAt); err != nil {
			return Record{}, false, fmt.Errorf("read %s: stored_at: %w", path, err)
		}
	}
	return rec, true, nil
}

// Source returns the recorded source of the stored file id. The boolean is
// false when none is recorded, as for a hand-made file.
func (s Store) Source(id string) (Source, bool, error) {
	rec, ok, err := s.Record(id)
	if err != nil || !ok || rec.HandMade {
		return Source{}, false, err
	}
	return rec.Source, true, nil
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
