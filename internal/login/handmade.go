package login

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/enigma-brain/axon/internal/apiserver"
	"github.com/enigma-brain/axon/internal/configfile"
	"github.com/enigma-brain/axon/internal/netreason"
	"github.com/enigma-brain/axon/internal/termtext"
	"github.com/enigma-brain/axon/internal/tsh"
)

// defaultApp is the Teleport application a hand-made teleport pathway
// uses when none is given.
const defaultApp = "skypilot"

// maxFileID is the longest identifier a config file may have.
const maxFileID = 32

// handMade is the answers a hand-made pathway is made from.
type handMade struct {
	kind configfile.Kind
	// endpoint is the API server's https address, on a direct pathway.
	endpoint string
	// proxy and app are the Teleport proxy and application, on a teleport
	// pathway.
	proxy, app string
	name       string
}

// address is what the default name and the messages call the pathway's
// server: the endpoint's host, or the proxy.
func (h handMade) address() string {
	if h.kind == configfile.KindTeleport {
		return h.proxy
	}
	u, err := url.Parse(h.endpoint)
	if err != nil || u.Host == "" {
		return h.endpoint
	}
	return u.Host
}

// handFlags reports whether opts give any flag that makes a pathway.
func handFlags(opts Options) bool {
	return opts.SkyEndpoint != "" || opts.TeleportProxy != "" || opts.SkyApp != "" || opts.Name != ""
}

// firstHandFlag is the name of the first flag opts give that makes a
// pathway.
func firstHandFlag(opts Options) string {
	switch {
	case opts.SkyEndpoint != "":
		return "sky-endpoint"
	case opts.TeleportProxy != "":
		return "teleport-proxy"
	case opts.SkyApp != "":
		return "sky-app"
	default:
		return "name"
	}
}

// checkFlags refuses flags that contradict each other.
func checkFlags(opts Options) error {
	if !handFlags(opts) {
		return nil
	}
	flag := firstHandFlag(opts)
	switch {
	case opts.Config != "":
		return fmt.Errorf(msgConfigWith, flag, flag)
	case opts.Pathway != "":
		return fmt.Errorf(msgPathwayWith, flag, flag)
	case opts.SkyEndpoint != "" && opts.TeleportProxy != "":
		return errors.New(msgEndpointAndProxy)
	case opts.SkyApp != "" && opts.TeleportProxy == "":
		return errors.New(msgAppNeedsProxy)
	case opts.SkyEndpoint == "" && opts.TeleportProxy == "":
		return errors.New(msgNameNeedsServer)
	}
	return nil
}

// cleanEndpoint returns s as an API server's https address: https:// is
// added when s has no scheme, and a trailing slash is dropped.
func cleanEndpoint(s string) (string, error) {
	s = strings.TrimSpace(s)
	addr := s
	if !strings.Contains(addr, "://") {
		addr = "https://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil || termtext.HasUnsafe(s) || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf(msgNotHTTPS, s)
	}
	return strings.TrimRight(addr, "/"), nil
}

// cleanProxy returns s as a Teleport proxy address: a leading https:// and
// a trailing slash are dropped.
func cleanProxy(s string) (string, error) {
	s = strings.TrimSpace(s)
	proxy := strings.TrimSuffix(strings.TrimPrefix(s, "https://"), "/")
	// A fully qualified name's final dot names the same host.
	if host, port, found := strings.Cut(proxy, ":"); found {
		proxy = strings.TrimSuffix(host, ".") + ":" + port
	} else {
		proxy = strings.TrimSuffix(proxy, ".")
	}
	if tsh.CheckProxy(proxy) != nil || !configfile.IsHostPort(proxy) {
		return "", fmt.Errorf(msgNotHostPort, s)
	}
	return proxy, nil
}

// cleanApp returns s as a Teleport application name.
func cleanApp(s string) (string, error) {
	s = strings.TrimSpace(s)
	if tsh.CheckApp(s) != nil || termtext.HasUnsafe(s) {
		return "", fmt.Errorf(msgNotApp, s)
	}
	return s, nil
}

// cleanName returns s as a pathway's name: one with a letter or digit.
func cleanName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if identifier(s) == "" {
		return "", errors.New(msgNameEmpty)
	}
	if termtext.HasUnsafe(s) {
		return "", errors.New(msgNameUnsafe)
	}
	return s, nil
}

// identifier is a pathway's identifier made from its name: the name in
// lowercase, each run of characters other than a to z and 0 to 9 turned
// into one dash, without dashes at the ends.
func identifier(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
			continue
		}
		dash = true
	}
	return b.String()
}

// probeError is a server that did not answer as the pathway needs.
type probeError struct {
	// notSkyPilot reports that something answered, but not a SkyPilot API
	// server.
	notSkyPilot bool
	reason      string
}

func (e *probeError) Error() string { return e.reason }

// probeSky reports whether a SkyPilot API server answers at endpoint,
// logged in or not. It sends no login.
func probeSky(ctx context.Context, endpoint string) error {
	_, err := apiserver.Fetch(ctx, endpoint, apiserver.Options{})
	return skyProbeResult(ctx, err)
}

// skyProbeResult is what probeSky reports for err, the error of the health
// request: nil when a SkyPilot API server answered, logged in or not.
func skyProbeResult(ctx context.Context, err error) error {
	switch {
	case err == nil, errors.Is(err, apiserver.ErrNotLoggedIn):
		return nil
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, apiserver.ErrUnreachable):
		return &probeError{reason: netreason.Short(err)}
	default:
		return &probeError{notSkyPilot: true, reason: err.Error()}
	}
}

// probe checks that h's server answers: the API server on a direct
// pathway, the Teleport proxy on a teleport one. The error is the message
// fitting where the answers came from: the prompt, or the flags.
func (l *login) probe(ctx context.Context, h handMade, fromFlags bool, out io.Writer) error {
	var err error
	if h.kind == configfile.KindTeleport {
		fmt.Fprintf(out, msgChecking, h.proxy)
		err = l.probeProxy(ctx, h.proxy)
	} else {
		fmt.Fprintf(out, msgChecking, h.endpoint)
		err = l.probeSky(ctx, h.endpoint)
	}
	if err == nil || ctx.Err() != nil {
		return err
	}
	var pe *probeError
	notSkyPilot := errors.As(err, &pe) && pe.notSkyPilot
	// Only a server that gave no answer gets the short reason; any other
	// failure is shown as it is.
	reason := err.Error()
	var unreachable *tsh.UnreachableError
	switch {
	case pe != nil:
		reason = pe.reason
	case errors.As(err, &unreachable):
		reason = netreason.Short(unreachable.Err)
	}
	switch {
	case h.kind == configfile.KindTeleport && fromFlags:
		return fmt.Errorf(msgFlagProxyUnreachable, h.proxy, reason)
	case h.kind == configfile.KindTeleport:
		return fmt.Errorf(strings.TrimSuffix(msgProxyUnreachable, "\n"), h.proxy, reason)
	case notSkyPilot && fromFlags:
		return fmt.Errorf(msgFlagSkyNotSkyPilot, h.endpoint, reason)
	case notSkyPilot:
		return fmt.Errorf(strings.TrimSuffix(msgSkyNotSkyPilot, "\n"), h.endpoint, reason)
	case fromFlags:
		return fmt.Errorf(msgFlagSkyUnreachable, h.endpoint, reason)
	default:
		return fmt.Errorf(strings.TrimSuffix(msgSkyUnreachable, "\n"), h.endpoint, reason)
	}
}

// makeFromFlags makes the pathway the flags give, once its server answers.
func (l *login) makeFromFlags(ctx context.Context, opts Options) (configfile.File, error) {
	bad := func(flag string, err error) error { return fmt.Errorf(msgBadFlag, flag, err) }
	var h handMade
	var err error
	if opts.SkyEndpoint != "" {
		h.kind = configfile.KindDirect
		if h.endpoint, err = cleanEndpoint(opts.SkyEndpoint); err != nil {
			return configfile.File{}, bad("sky-endpoint", err)
		}
	} else {
		h.kind = configfile.KindTeleport
		if h.proxy, err = cleanProxy(opts.TeleportProxy); err != nil {
			return configfile.File{}, bad("teleport-proxy", err)
		}
		h.app = defaultApp
		if opts.SkyApp != "" {
			if h.app, err = cleanApp(opts.SkyApp); err != nil {
				return configfile.File{}, bad("sky-app", err)
			}
		}
	}
	h.name = h.address()
	if opts.Name != "" {
		if h.name, err = cleanName(opts.Name); err != nil {
			return configfile.File{}, bad("name", err)
		}
	}
	if err := l.probe(ctx, h, true, opts.Out); err != nil {
		return configfile.File{}, err
	}
	return l.storeHandMade(h, opts)
}

// makeAtPrompt asks for a pathway and makes it, asking for an address
// again until its server answers.
func (l *login) makeAtPrompt(ctx context.Context, opts Options) (configfile.File, error) {
	in, out := opts.In, opts.Out
	fmt.Fprint(out, msgPromptIntro)
	var h handMade
	for h.kind == "" {
		fmt.Fprint(out, msgKindQuestion)
		answer, err := askLine(in, out, msgKindPrompt)
		if err != nil {
			return configfile.File{}, err
		}
		switch answer {
		case "1":
			h.kind = configfile.KindDirect
		case "2":
			h.kind = configfile.KindTeleport
		default:
			fmt.Fprintf(out, msgNotAKind, answer)
		}
	}

	for {
		var err error
		if h.kind == configfile.KindDirect {
			h.endpoint, err = askValid(in, out, msgEndpointPrompt, "", cleanEndpoint)
		} else {
			h.proxy, err = askValid(in, out, msgProxyPrompt, "", cleanProxy)
		}
		if err != nil {
			return configfile.File{}, err
		}
		err = l.probe(ctx, h, false, out)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return configfile.File{}, ctx.Err()
		}
		fmt.Fprintln(out, err)
	}
	if h.kind == configfile.KindTeleport {
		var err error
		if h.app, err = askValid(in, out, fmt.Sprintf(msgAppPrompt, defaultApp), defaultApp, cleanApp); err != nil {
			return configfile.File{}, err
		}
	}
	var err error
	if h.name, err = askValid(in, out, fmt.Sprintf(msgNamePrompt, h.address()), h.address(), cleanName); err != nil {
		return configfile.File{}, err
	}
	return l.storeHandMade(h, opts)
}

// askLine asks with prompt and returns the trimmed answer. At the end of
// input with no answer, it stops and says how to make a pathway.
func askLine(in io.Reader, out io.Writer, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	line, err := readLine(in)
	answer := strings.TrimSpace(line)
	if err != nil && (answer == "" || !errors.Is(err, io.EOF)) {
		fmt.Fprintln(out)
		if errors.Is(err, io.EOF) {
			return "", errors.New(msgNoPathwayMade)
		}
		return "", fmt.Errorf("read the answer: %w", err)
	}
	return answer, nil
}

// askValid asks with prompt until clean accepts the answer, and returns
// what clean makes of it. An empty answer is def, when def is not empty.
func askValid(in io.Reader, out io.Writer, prompt, def string, clean func(string) (string, error)) (string, error) {
	for {
		answer, err := askLine(in, out, prompt)
		if err != nil {
			return "", err
		}
		if answer == "" {
			if def == "" {
				continue
			}
			answer = def
		}
		v, err := clean(answer)
		if err == nil {
			return v, nil
		}
		fmt.Fprintln(out, err)
	}
}

// storeHandMade writes h as a config file and stores it as hand-made,
// asking first about each stored pathway it clashes with. When the user
// keeps a stored pathway of the same name, it asks for another name.
func (l *login) storeHandMade(h handMade, opts Options) (configfile.File, error) {
	store := configfile.NewStore(l.dir)
	for {
		id := identifier(h.name)
		fid, err := fileID(store, id, nil)
		if err != nil {
			return configfile.File{}, err
		}
		data, err := handMadeFile(fid, id, h)
		if err != nil {
			return configfile.File{}, err
		}
		change, err := store.Inspect(data, true)
		if err != nil {
			return configfile.File{}, err
		}
		outcome, err := resolveClashes(store, change, opts.Replace, opts.In, opts.Out)
		var noTerminal *errClashNoTerminal
		if errors.As(err, &noTerminal) {
			c := noTerminal.clash
			return configfile.File{}, fmt.Errorf(msgPathwayClashNoTerminal, c.Pathway.Name, store.Path(c.Stored.ID))
		}
		if err != nil {
			return configfile.File{}, err
		}
		if outcome.declined != nil {
			fmt.Fprint(opts.Out, msgChooseName)
			if h.name, err = askValid(opts.In, opts.Out, fmt.Sprintf(msgNamePrompt, h.address()), h.address(), cleanName); err != nil {
				return configfile.File{}, err
			}
			continue
		}

		// With the files to remove known, the file takes the plain
		// identifier when its holder is one of them.
		if fid, err = fileID(store, id, outcome.remove); err != nil {
			return configfile.File{}, err
		}
		if data, err = handMadeFile(fid, id, h); err != nil {
			return configfile.File{}, err
		}
		if err := l.leaveRemoved(outcome.remove, opts); err != nil {
			return configfile.File{}, err
		}
		f, err := store.Put(data, configfile.PutOptions{Origin: configfile.HandMade, Remove: outcome.remove})
		if err != nil {
			return configfile.File{}, err
		}
		reportRemoved(store, change, outcome.remove, opts.Out)
		fmt.Fprintf(opts.Out, msgStoredPathway, h.name, store.Path(f.ID))
		return f, nil
	}
}

// fileID is the identifier of a hand-made file whose pathway's identifier
// is id: id itself, cut to the longest a file identifier can be and
// starting with a letter, with -2, -3 and so on added while a stored file
// other than one in remove has it.
func fileID(store configfile.Store, id string, remove []configfile.File) (string, error) {
	base := id
	if base[0] >= '0' && base[0] <= '9' {
		base = "p-" + base
	}
	free := func(candidate string) (bool, error) {
		for _, r := range remove {
			if r.ID == candidate {
				return true, nil
			}
		}
		_, err := os.Lstat(store.Path(candidate))
		switch {
		case errors.Is(err, os.ErrNotExist):
			return true, nil
		case err != nil:
			return false, fmt.Errorf("check %s: %w", store.Path(candidate), err)
		}
		return false, nil
	}
	cut := func(s string, n int) string {
		if len(s) > n {
			s = s[:n]
		}
		return strings.TrimRight(s, "-")
	}
	candidate := cut(base, maxFileID)
	for n := 2; ; n++ {
		ok, err := free(candidate)
		if err != nil || ok {
			return candidate, err
		}
		suffix := "-" + strconv.Itoa(n)
		candidate = cut(base, maxFileID-len(suffix)) + suffix
	}
}

// The TOML of a hand-made file.
type handMadeTOML struct {
	Format  int               `toml:"format"`
	ID      string            `toml:"id"`
	Pathway []handMadePathway `toml:"pathway"`
}

type handMadePathway struct {
	ID          string            `toml:"id"`
	Kind        string            `toml:"kind"`
	Name        string            `toml:"name"`
	Description string            `toml:"description"`
	Teleport    *handMadeTeleport `toml:"teleport,omitempty"`
	Sky         handMadeSky       `toml:"sky"`
}

type handMadeTeleport struct {
	Proxy string `toml:"proxy"`
}

type handMadeSky struct {
	Endpoint string `toml:"endpoint,omitempty"`
	App      string `toml:"app,omitempty"`
}

// handMadeFile writes h as a config file with the identifier fileID, whose
// one pathway has the identifier id.
func handMadeFile(fileID, id string, h handMade) ([]byte, error) {
	p := handMadePathway{ID: id, Kind: string(h.kind), Name: h.name}
	if h.kind == configfile.KindTeleport {
		p.Description = fmt.Sprintf(descTeleport, h.proxy, h.app)
		p.Teleport = &handMadeTeleport{Proxy: h.proxy}
		p.Sky.App = h.app
	} else {
		p.Description = fmt.Sprintf(descDirect, h.endpoint)
		p.Sky.Endpoint = h.endpoint
	}
	var buf bytes.Buffer
	err := toml.NewEncoder(&buf).Encode(handMadeTOML{Format: configfile.Format, ID: fileID, Pathway: []handMadePathway{p}})
	if err != nil {
		return nil, fmt.Errorf("write the pathway's file: %w", err)
	}
	return buf.Bytes(), nil
}
