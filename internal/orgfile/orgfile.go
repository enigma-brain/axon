// Package orgfile reads, checks and stores config files.
//
// A config file is a TOML file that names an organization's
// pathways: the ways a user reaches its services. The package parses and
// checks a file, reads one from an https address, a file path or standard
// input, stores accepted files in ~/.axon/orgs and lists them.
package orgfile

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Kind is the kind of a pathway, which decides how a user logs in and how
// the pathway reaches its services.
type Kind string

const (
	// KindDirect logs in to the service's own login in a browser and
	// reaches the service at its address.
	KindDirect Kind = "direct"
	// KindTeleport logs in through Teleport and reaches the service
	// through a local tunnel to a Teleport application.
	KindTeleport Kind = "teleport"
)

// File is a checked config file.
type File struct {
	// ID is the file's identifier, which names the stored file.
	ID string
	// Revision is the file's revision, when HasRevision is true. An
	// organization raises it each time it changes the file.
	Revision    int64
	HasRevision bool
	// Pathways are the file's pathways, in the file's order.
	Pathways []Pathway
}

// Pathway is one pathway of a config file.
type Pathway struct {
	// File is the ID of the config file that holds the pathway.
	File string
	// ID is the pathway's identifier, the value of --pathway.
	ID   string
	Kind Kind

	// Name, Description and UnreachableHint are text for people, printed
	// as written. UnreachableHint is empty when the file has none.
	Name            string
	Description     string
	UnreachableHint string

	// Teleport holds the settings of a teleport pathway, and is nil for a
	// direct one.
	Teleport *Teleport
	// Sky holds where the sky command group finds its service.
	Sky Sky
}

// Teleport holds the settings only a teleport pathway has.
type Teleport struct {
	// Proxy is the Teleport proxy: a hostname, optionally with a port.
	Proxy string
}

// Sky holds where the sky command group finds the SkyPilot API server on
// a pathway.
type Sky struct {
	// Endpoint is the API server's https address, on a direct pathway.
	Endpoint string
	// App is the Teleport application that serves the API server, on a
	// teleport pathway.
	App string
}

// Format is the only file format this package accepts.
const Format = 2

// ErrOlderFormat is the refusal of a file in a format that an earlier axon
// used.
var ErrOlderFormat = errors.New("the file is in an older format. Run `axon login --config <source>` again with your organization's current config file.")

var (
	fileIDPattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	pathwayIDPattern = regexp.MustCompile(`^[a-z0-9-]+$`)
	hostLabelPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
)

// rawFile and its parts receive the decoded TOML. Values are decoded as
// any so that a value of the wrong type is reported by its field.
type rawFile struct {
	Format   any          `toml:"format"`
	ID       any          `toml:"id"`
	Revision any          `toml:"revision"`
	Pathway  []rawPathway `toml:"pathway"`
}

type rawPathway struct {
	ID              any          `toml:"id"`
	Kind            any          `toml:"kind"`
	Name            any          `toml:"name"`
	Description     any          `toml:"description"`
	UnreachableHint any          `toml:"unreachable_hint"`
	Teleport        *rawTeleport `toml:"teleport"`
	Sky             *rawSky      `toml:"sky"`
}

type rawTeleport struct {
	Proxy any `toml:"proxy"`
}

type rawSky struct {
	Endpoint any `toml:"endpoint"`
	App      any `toml:"app"`
}

// CheckError is the refusal of a config file: the field that failed
// and the check it failed.
type CheckError struct {
	// Field names the field, prefixed with its pathway where it has one,
	// such as `pathway "campus": sky.endpoint`.
	Field string
	// Check says what the field must be.
	Check string
}

func (e *CheckError) Error() string {
	if e.Field == "" {
		return e.Check
	}
	return e.Field + ": " + e.Check
}

func refuse(field, format string, args ...any) error {
	return &CheckError{Field: field, Check: fmt.Sprintf(format, args...)}
}

// Parse parses data as a config file and applies every check on a
// single file. It does not compare the file with the stored files.
func Parse(data []byte) (File, error) {
	if len(data) > MaxSize {
		return File{}, refuse("", "the file is larger than %d bytes", MaxSize)
	}
	var generic map[string]any
	if _, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&generic); err != nil {
		var perr toml.ParseError
		if errors.As(err, &perr) {
			return File{}, refuse(perr.LastKey, "not valid TOML: %s (line %d)", perr.Message, perr.Position.Line)
		}
		return File{}, refuse("", "not valid TOML: %v", err)
	}
	if n, ok := generic["format"].(int64); ok && n >= 1 && n < Format {
		return File{}, ErrOlderFormat
	}
	if err := checkTables(generic); err != nil {
		return File{}, err
	}
	if err := checkKeys(generic); err != nil {
		return File{}, err
	}
	var raw rawFile
	md, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&raw)
	if err != nil {
		return File{}, refuse("", "not valid TOML: %v", err)
	}

	if n, ok := raw.Format.(int64); !ok || n != Format {
		return File{}, refuse("format", "must be %d", Format)
	}
	id, ok := raw.ID.(string)
	if !ok || !fileIDPattern.MatchString(id) {
		return File{}, refuse("id", "must be lowercase letters, digits and hyphens, start with a letter, and be at most 32 characters")
	}
	if len(raw.Pathway) == 0 {
		return File{}, refuse("pathway", "the file must have at least one [[pathway]]")
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return File{}, refuse(undecoded[0].String(), "not a field of a config file")
	}

	f := File{ID: id}
	if raw.Revision != nil {
		n, ok := raw.Revision.(int64)
		if !ok || n < 0 {
			return File{}, refuse("revision", "must be a whole number, 0 or more")
		}
		f.Revision, f.HasRevision = n, true
	}
	seen := map[string]bool{}
	for i, rp := range raw.Pathway {
		p, err := checkPathway(i, rp)
		if err != nil {
			return File{}, err
		}
		if seen[p.ID] {
			return File{}, refuse(fmt.Sprintf("pathway %q: id", p.ID), "appears more than once in the file")
		}
		seen[p.ID] = true
		p.File = id
		f.Pathways = append(f.Pathways, p)
	}
	return f, nil
}

// checkTables checks that the tables of the file are tables, so that
// decoding into rawFile cannot fail on a value of the wrong type.
func checkTables(generic map[string]any) error {
	v, ok := generic["pathway"]
	if !ok {
		return nil
	}
	pathways, ok := v.([]map[string]any)
	if !ok {
		return refuse("pathway", "must be [[pathway]] entries")
	}
	for i, p := range pathways {
		for _, name := range []string{"teleport", "sky"} {
			if t, ok := p[name]; ok {
				if _, ok := t.(map[string]any); !ok {
					return refuse(fmt.Sprintf("pathway %d: %s", i+1, name), "must be a table")
				}
			}
		}
	}
	return nil
}

// The keys a config file may hold, spelled exactly. The decoder
// matches struct fields regardless of case, so a key such as ID beside id
// would otherwise both decode into the same field.
var (
	fileKeys     = keySet("format", "id", "revision", "pathway")
	pathwayKeys  = keySet("id", "kind", "name", "description", "unreachable_hint", "teleport", "sky")
	teleportKeys = keySet("proxy")
	skyKeys      = keySet("endpoint", "app")
)

func keySet(keys ...string) map[string]bool {
	set := map[string]bool{}
	for _, k := range keys {
		set[k] = true
	}
	return set
}

// checkKeys refuses any key, at any level, that is not spelled exactly as
// the format lists it. It runs after checkTables, so the tables have their
// expected types.
func checkKeys(generic map[string]any) error {
	if err := unknownKey("", generic, fileKeys); err != nil {
		return err
	}
	pathways, _ := generic["pathway"].([]map[string]any)
	for _, p := range pathways {
		if err := unknownKey("pathway.", p, pathwayKeys); err != nil {
			return err
		}
		for name, allowed := range map[string]map[string]bool{"teleport": teleportKeys, "sky": skyKeys} {
			if t, ok := p[name].(map[string]any); ok {
				if err := unknownKey("pathway."+name+".", t, allowed); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// unknownKey refuses the first key of table, in sorted order, that is not
// in allowed, naming it with prefix.
func unknownKey(prefix string, table map[string]any, allowed map[string]bool) error {
	var unknown []string
	for k := range table {
		if !allowed[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return refuse(prefix+unknown[0], "not a field of a config file")
}

// checkPathway checks the pathway at index i of the file.
func checkPathway(i int, rp rawPathway) (Pathway, error) {
	where := fmt.Sprintf("pathway %d", i+1)
	id, ok := rp.ID.(string)
	if !ok || !pathwayIDPattern.MatchString(id) {
		return Pathway{}, refuse(where+": id", "must be lowercase letters, digits and hyphens")
	}
	where = fmt.Sprintf("pathway %q", id)
	field := func(name string) string { return where + ": " + name }

	p := Pathway{ID: id}
	switch k, _ := rp.Kind.(string); Kind(k) {
	case KindDirect, KindTeleport:
		p.Kind = Kind(k)
	default:
		return Pathway{}, refuse(field("kind"), "must be %q or %q", KindDirect, KindTeleport)
	}

	var err error
	if p.Name, err = requiredText(field("name"), rp.Name); err != nil {
		return Pathway{}, err
	}
	if p.Description, err = requiredText(field("description"), rp.Description); err != nil {
		return Pathway{}, err
	}
	if rp.UnreachableHint != nil {
		hint, ok := rp.UnreachableHint.(string)
		if !ok {
			return Pathway{}, refuse(field("unreachable_hint"), "must be text")
		}
		p.UnreachableHint = hint
	}

	sky := rp.Sky
	if sky == nil {
		sky = &rawSky{}
	}
	switch p.Kind {
	case KindDirect:
		if rp.Teleport != nil {
			return Pathway{}, refuse(field("teleport"), "not allowed on a direct pathway")
		}
		if sky.App != nil {
			return Pathway{}, refuse(field("sky.app"), "not allowed on a direct pathway")
		}
		endpoint, ok := sky.Endpoint.(string)
		if !ok || !isHTTPSAddress(endpoint) {
			return Pathway{}, refuse(field("sky.endpoint"), "must be an https address")
		}
		p.Sky.Endpoint = endpoint
	case KindTeleport:
		if sky.Endpoint != nil {
			return Pathway{}, refuse(field("sky.endpoint"), "not allowed on a teleport pathway")
		}
		var proxy any
		if rp.Teleport != nil {
			proxy = rp.Teleport.Proxy
		}
		host, ok := proxy.(string)
		if !ok || !isHostPort(host) {
			return Pathway{}, refuse(field("teleport.proxy"), "must be a hostname, optionally with a port")
		}
		p.Teleport = &Teleport{Proxy: host}
		if p.Sky.App, err = requiredText(field("sky.app"), sky.App); err != nil {
			return Pathway{}, err
		}
	}
	return p, nil
}

// requiredText returns v as a string, refusing it when it is missing, not
// text, or blank.
func requiredText(field string, v any) (string, error) {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", refuse(field, "is required and must be text")
	}
	return s, nil
}

// isHTTPSAddress reports whether s is an absolute https URL with a host.
func isHTTPSAddress(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Opaque == ""
}

// isHostPort reports whether s is a hostname, optionally followed by a
// colon and a port from 1 to 65535.
func isHostPort(s string) bool {
	host := s
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		host = s[:i]
		port := s[i+1:]
		if port == "" || strings.TrimLeft(port, "0123456789") != "" {
			return false
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !hostLabelPattern.MatchString(label) {
			return false
		}
	}
	return true
}
