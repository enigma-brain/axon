// Package login is axon login: it adds a config file when one is
// given, determines the pathway, establishes the pathway's session, and
// connects each command group.
package login

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/enigma-brain/axon/internal/apiserver"
	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/configfile"
	skygroup "github.com/enigma-brain/axon/internal/sky"
	"github.com/enigma-brain/axon/internal/tsh"
	"github.com/enigma-brain/axon/internal/tunnel"
)

// Options are the inputs of one login.
type Options struct {
	// Config is the source given with --config: an https address, a file path,
	// or "-" for standard input. When empty, no file is added.
	Config string
	// Pathway is the pathway identifier given with --pathway. When empty,
	// the active pathway is used, or one is chosen.
	Pathway string
	// SkyEndpoint, given with --sky-endpoint, makes a direct pathway to the
	// API server at that address.
	SkyEndpoint string
	// TeleportProxy, given with --teleport-proxy, makes a teleport pathway
	// through that proxy, to the application SkyApp, or skypilot when
	// SkyApp is empty.
	TeleportProxy string
	SkyApp        string
	// Name, given with --name, is the name of the pathway SkyEndpoint or
	// TeleportProxy makes. When empty, it is the address.
	Name string
	// Replace, given with --replace, answers yes to every question about
	// replacing a stored pathway or config file.
	Replace bool
	// In is standard input: the config file with --config -, and
	// otherwise where answers to questions are read from.
	In io.Reader
	// Out is where progress and questions are written.
	Out io.Writer
	// Err is where warnings and the error output of the commands login
	// runs are written.
	Err io.Writer
}

// login holds what one run of axon login works with. Run fills it with the
// real directory, client and server; tests replace each part.
type login struct {
	dir    axondir.Dir
	client client
	health func(ctx context.Context, endpoint string, opts apiserver.Options) (apiserver.Health, error)
	// cookieFile is SkyPilot's cookie file. The sky commands login runs
	// inherit the environment that chose it, so they use the same file.
	cookieFile string
	// endpoint returns the API server endpoint of a pathway.
	endpoint func(configfile.Pathway) string
	// loginWait bounds the browser login.
	loginWait time.Duration
	// environ is the environment the commands login runs start from.
	environ func() []string
	// readConfig reads a config file from its source.
	readConfig func(ctx context.Context, src configfile.Source, stdin io.Reader) ([]byte, error)
	// openTerminal opens the terminal, where questions are asked when
	// standard input is the config file.
	openTerminal func() (io.ReadCloser, error)
	// teleport is what a teleport pathway works with.
	teleport teleport
	// isTerminal reports whether in is a terminal, where a pathway can be
	// asked for.
	isTerminal func(in io.Reader) bool
	// probeSky and probeProxy check that a hand-made pathway's server
	// answers: the API server, or the Teleport proxy.
	probeSky   func(ctx context.Context, endpoint string) error
	probeProxy func(ctx context.Context, proxy string) error
}

// Run runs axon login.
func Run(ctx context.Context, opts Options) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find the home directory: %w", err)
	}
	return newLogin(home, os.Getenv).run(ctx, opts)
}

// newLogin returns the login for the user whose home directory is home, with
// the environment getenv reads.
func newLogin(home string, getenv func(string) string) *login {
	dir := axondir.New(home)
	return &login{
		dir:          dir,
		client:       skyClient{dir: dir},
		health:       apiserver.Fetch,
		cookieFile:   apiserver.CookieFile(home, getenv),
		endpoint:     skygroup.Endpoint,
		loginWait:    loginWait,
		environ:      os.Environ,
		readConfig:   configfile.Read,
		openTerminal: func() (io.ReadCloser, error) { return os.Open("/dev/tty") },
		teleport: teleport{
			ensure:    tsh.Ensure,
			newTunnel: tunnel.New,
			skyConfig: skyConfigPath(home, getenv),
		},
		isTerminal: isTerminal,
		probeSky:   probeSky,
		probeProxy: tsh.Ping,
	}
}

// run runs the stages of login in order and stops at the first that fails.
// Each error names its stage.
func (l *login) run(ctx context.Context, opts Options) error {
	if opts.In == nil {
		opts.In = os.Stdin
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.Err == nil {
		opts.Err = io.Discard
	}
	if err := checkFlags(opts); err != nil {
		return err
	}

	// A config file update that stopped part way left the previous copy
	// beside the stored file; it is dealt with before anything is read.
	restored, err := configfile.NewStore(l.dir).Recover()
	if err != nil {
		return fmt.Errorf("read the config files: %w", err)
	}
	for _, path := range restored {
		fmt.Fprintf(opts.Out, msgPutBack, path)
	}

	// 1. Add the config file that --config gives. With --config -,
	// standard input is the file, and questions go to the terminal.
	answers := opts.In
	var added *configfile.File
	if opts.Config != "" {
		f, in, closeIn, err := l.addConfig(ctx, opts)
		if err != nil {
			return fmt.Errorf("add the config file: %w", err)
		}
		defer closeIn()
		added, answers = &f, in
	}
	opts.In = answers

	// Or make the pathway the flags give.
	if handFlags(opts) {
		f, err := l.makeFromFlags(ctx, opts)
		if err != nil {
			return fmt.Errorf("make the pathway: %w", err)
		}
		added = &f
	}

	// 2. Determine the pathway. With nothing stored, one is made from
	// answers at the terminal.
	files, err := l.storedFiles(opts.Err)
	if err != nil {
		return fmt.Errorf("read the config files: %w", err)
	}
	if len(files) == 0 && added == nil && opts.Pathway == "" && l.isTerminal != nil && l.isTerminal(opts.In) {
		if err := l.disconnectAll(opts); err != nil {
			return fmt.Errorf("disconnect the previous pathway: %w. Run `axon login` again.", err)
		}
		f, err := l.makeAtPrompt(ctx, opts)
		if err != nil {
			return fmt.Errorf("make the pathway: %w", err)
		}
		added = &f
		if files, err = l.storedFiles(io.Discard); err != nil {
			return fmt.Errorf("read the config files: %w", err)
		}
	}
	if len(files) == 0 {
		return l.noConfig(opts)
	}
	p, err := l.determinePathway(files, added, opts)
	if err != nil {
		return fmt.Errorf("determine the pathway: %w", err)
	}

	// 3. Refresh the pathway's file. When the fetched file no longer has
	// the pathway, the pathway is left and one is determined again.
	p, err = l.refresh(ctx, p, added, opts)
	if errors.Is(err, errNoConfig) {
		return l.noConfig(opts)
	}
	if err != nil {
		return fmt.Errorf("determine the pathway: %w", err)
	}

	// A teleport pathway needs tsh. Without it the login stops here, before
	// the pathway that was connected is left or the active one changes.
	if p.Kind == configfile.KindTeleport {
		path, err := l.teleport.ensure(ctx, p.Teleport.Proxy)
		var unreachable *tsh.UnreachableError
		if errors.As(err, &unreachable) {
			next := msgProxyUnreachableNext
			if p.UnreachableHint != "" {
				next = p.UnreachableHint
			}
			return fmt.Errorf("establish the Teleport session: %w. %s", err, next)
		}
		if err != nil {
			return fmt.Errorf("establish the Teleport session: %w", err)
		}
		l.teleport.path = path
	}

	// Leaving the pathway that was connected, or its settings for
	// machines, disconnects it by what was recorded when it connected,
	// before the new pathway is stored: when that fails, the active
	// pathway is unchanged and the same command tries again.
	if err := l.disconnectUnless(p, opts); err != nil {
		return fmt.Errorf("disconnect the previous pathway: %w. Run `axon login --pathway %s` again.", err, p.ID)
	}
	if err := l.setActive(p.ID); err != nil {
		return fmt.Errorf("determine the pathway: %w", err)
	}

	// 4. Establish the pathway's session.
	if p.Kind == configfile.KindTeleport {
		if err := l.establishTeleportSession(ctx, p, opts); err != nil {
			return fmt.Errorf("establish the Teleport session: %w", err)
		}
	}
	// A direct pathway has no session apart from each service's own.

	// 5. Connect each command group.
	if err := l.connectSky(ctx, p, opts); err != nil {
		return fmt.Errorf("connect sky: %w", err)
	}
	return nil
}

// errNoConfig reports that no config file is stored, and none was given or
// made.
var errNoConfig = errors.New(msgNoPathwayNoTerminal)

// isTerminal reports whether in is a terminal: a file whose terminal
// settings can be read. /dev/null is a character device but not a
// terminal.
func isTerminal(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	_, err := unix.IoctlGetTermios(int(f.Fd()), getTermios)
	return err == nil
}

// noConfig ends a login that finds no stored config file to use and cannot
// ask for a pathway. The active pathway's file is gone, or fails the checks,
// and no other is stored: the pathway is left, which disconnects it, before
// login says how to make one.
func (l *login) noConfig(opts Options) error {
	if err := l.disconnectAll(opts); err != nil {
		return fmt.Errorf("disconnect the previous pathway: %w. Run `axon login` again.", err)
	}
	return errNoConfig
}

// addConfig reads the file --config names, checks it and stores it, asking
// first about each stored pathway it clashes with and about a lower
// revision. It returns the file the login goes on with, the stored copy
// when a lower revision was declined, and where answers to questions are
// read from: standard input, or with --config - the terminal. When there is
// no terminal, answers come from an empty input, so that a question with a
// flag stops and names the flag, and one without is answered no. closeIn
// releases the terminal.
func (l *login) addConfig(ctx context.Context, opts Options) (f configfile.File, in io.Reader, closeIn func(), err error) {
	src, err := configfile.ParseSource(opts.Config)
	if err != nil {
		return configfile.File{}, nil, nil, err
	}
	data, err := l.readConfig(ctx, src, opts.In)
	if err != nil {
		return configfile.File{}, nil, nil, err
	}
	in, closeIn = opts.In, func() {}
	if src.String() == "-" {
		in = strings.NewReader("")
		if tty, err := l.openTerminal(); err == nil {
			in, closeIn = tty, func() { tty.Close() }
		}
	}
	fail := func(err error) (configfile.File, io.Reader, func(), error) {
		closeIn()
		return configfile.File{}, nil, nil, err
	}

	store := configfile.NewStore(l.dir)
	change, err := store.Inspect(data, false)
	if errors.Is(err, configfile.ErrEmpty) {
		name := src.String()
		if name == "-" {
			name = msgStdin
		}
		return fail(fmt.Errorf(msgConfigEmpty, name))
	}
	if err != nil {
		return fail(err)
	}
	outcome, err := resolveClashes(store, change, opts.Replace, in, opts.Out)
	var noTerminal *errClashNoTerminal
	if errors.As(err, &noTerminal) {
		c := noTerminal.clash
		return fail(fmt.Errorf(msgConfigClashNoTerminal, c.Added.Name, c.Pathway.Name, store.Path(c.Stored.ID)))
	}
	if err != nil {
		return fail(err)
	}
	if c := outcome.declined; c != nil {
		return fail(fmt.Errorf(msgConfigNotAdded, c.Added.Name, store.Path(c.Stored.ID)))
	}

	put := configfile.PutOptions{Origin: configfile.FromSource(src), Remove: outcome.remove}
	if change.Lower() {
		prev := change.Previous
		yes := opts.Replace
		if !yes {
			fmt.Fprintf(opts.Out, msgLowerRevision, store.Path(prev.ID), prev.Revision, change.File.Revision)
			yes, err = askYesNo(in, opts.Out, fmt.Sprintf(msgLowerPrompt, change.File.Revision))
			if errors.Is(err, errNoAnswer) {
				fmt.Fprintf(opts.Err, msgLowerRevisionWarning, src, change.File.Revision, prev.Revision)
				return *prev, in, closeIn, nil
			}
			if err != nil {
				return fail(err)
			}
		}
		if !yes {
			fmt.Fprintf(opts.Out, msgKeptRevision, prev.Revision)
			return *prev, in, closeIn, nil
		}
		put.AllowLower = true
	}

	if err := l.leaveRemoved(put.Remove, opts); err != nil {
		return fail(err)
	}
	f, err = store.Put(data, put)
	if err != nil {
		return fail(err)
	}
	reportRemoved(store, change, put.Remove, opts.Out)
	previous := change.Previous
	if previous != nil && !previous.HasRevision && !f.HasRevision {
		previous = nil
	}
	reportStored(opts.Out, store.Path(f.ID), f, previous)
	return f, in, closeIn, nil
}

// storedFiles returns the stored config files that pass the checks,
// and writes one line to warn for each one left out.
func (l *login) storedFiles(warn io.Writer) ([]configfile.File, error) {
	files, problems, err := configfile.NewStore(l.dir).List()
	if err != nil {
		return nil, err
	}
	for _, p := range problems {
		fmt.Fprintf(warn, "axon: left out %s: %v\n", p.Path, p.Err)
	}
	return files, nil
}

// setActive stores id as the active pathway, unless it already is.
func (l *login) setActive(id string) error {
	if active, ok, err := l.dir.Pathway(); err == nil && ok && active == id {
		return nil
	}
	return l.dir.SetPathway(id)
}
