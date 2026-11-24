// Package connected records the pathway that axon login connected, with
// the settings for machines it had then, in ~/.axon/state/pathway.toml.
//
// Disconnecting goes by this record, never by a config file, so it
// works when the file that named the pathway has changed or is gone; and a
// command group runs only while the active pathway's settings are the ones
// recorded.
package connected

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/configfile"
)

const (
	file = "pathway.toml"
	// format is the format of the record this axon writes. A record of
	// another format, such as one an older axon wrote, is treated as no
	// record.
	format = 2
)

// Record is the content of the record: the pathway's identifier, its kind,
// and its settings for machines, each empty when the kind has none. Text
// for people is not recorded: a change to it alone changes nothing.
type Record struct {
	Format        int    `toml:"format"`
	ID            string `toml:"id"`
	Kind          string `toml:"kind"`
	TeleportProxy string `toml:"teleport_proxy,omitempty"`
	SkyEndpoint   string `toml:"sky_endpoint,omitempty"`
	SkyApp        string `toml:"sky_app,omitempty"`
}

// Of returns the record of p.
func Of(p configfile.Pathway) Record {
	r := Record{
		Format:      format,
		ID:          p.ID,
		Kind:        string(p.Kind),
		SkyEndpoint: p.Sky.Endpoint,
		SkyApp:      p.Sky.App,
	}
	if p.Teleport != nil {
		r.TeleportProxy = p.Teleport.Proxy
	}
	return r
}

// Path returns the path of the record in dir.
func Path(dir axondir.Dir) string {
	return filepath.Join(dir.StatePath(), file)
}

// Read returns the record. The boolean is false when there is none, or
// when it cannot be read as a record of this format.
func Read(dir axondir.Dir) (Record, bool, error) {
	path := Path(dir)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	var r Record
	if _, err := toml.Decode(string(data), &r); err != nil || r.Format != format {
		return Record{}, false, nil
	}
	return r, true, nil
}

// Write records p as the connected pathway.
func Write(dir axondir.Dir, p configfile.Pathway) error {
	if _, err := dir.StateDir(); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(Of(p)); err != nil {
		return fmt.Errorf("write %s: %w", Path(dir), err)
	}
	return axondir.ReplaceFile(Path(dir), buf.Bytes())
}

// Is reports whether the record in dir is p with its present settings for
// machines.
func Is(dir axondir.Dir, p configfile.Pathway) (bool, error) {
	r, ok, err := Read(dir)
	return ok && r == Of(p), err
}

// Remove deletes the record, when there is one.
func Remove(dir axondir.Dir) error {
	path := Path(dir)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}
