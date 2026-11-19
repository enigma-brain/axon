// Package login is axon login: it adds an organisation file when one is
// given, determines the pathway, establishes the pathway's session, and
// connects each command group.
package login

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/enigma-brain/axon/internal/apiserver"
	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/devenv"
	"github.com/enigma-brain/axon/internal/orgfile"
	"github.com/enigma-brain/axon/internal/tsh"
	"github.com/enigma-brain/axon/internal/tunnel"
)

// Options are the inputs of one login.
type Options struct {
	// Org is the source given with --org: an https address, a file path,
	// or "-" for standard input. When empty, no file is added.
	Org string
	// Pathway is the pathway identifier given with --pathway. When empty,
	// the active pathway is used, or one is chosen.
	Pathway string
	// In is standard input: the organisation file with --org -, and
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
	endpoint func(orgfile.Pathway) string
	// loginWait bounds the browser login.
	loginWait time.Duration
	// environ is the environment the commands login runs start from.
	environ func() []string
	// readOrg reads an organisation file from its source.
	readOrg func(ctx context.Context, src orgfile.Source, stdin io.Reader) ([]byte, error)
	// openTerminal opens the terminal, where questions are asked when
	// standard input is the organisation file.
	openTerminal func() (io.ReadCloser, error)
	// teleport is what a teleport pathway works with.
	teleport teleport
}

// Run runs axon login.
func Run(ctx context.Context, opts Options) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find the home directory: %w", err)
	}
	dir := axondir.New(home)
	l := &login{
		dir:          dir,
		client:       skyClient{dir: dir},
		health:       apiserver.Fetch,
		cookieFile:   apiserver.CookieFile(home, os.Getenv),
		endpoint:     devenv.Endpoint,
		loginWait:    loginWait,
		environ:      os.Environ,
		readOrg:      orgfile.Read,
		openTerminal: func() (io.ReadCloser, error) { return os.Open("/dev/tty") },
		teleport: teleport{
			ensure:    tsh.Ensure,
			newTunnel: tunnel.New,
			skyConfig: filepath.Join(home, ".sky", "config.yaml"),
		},
	}
	return l.run(ctx, opts)
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

	// 1. Add the organisation file that --org gives. With --org -,
	// standard input is the file, and questions go to the terminal.
	answers := opts.In
	var added *orgfile.File
	if opts.Org != "" {
		f, in, closeIn, err := l.addOrg(ctx, opts)
		if err != nil {
			return fmt.Errorf("add the organisation file: %w", err)
		}
		defer closeIn()
		added, answers = &f, in
	}
	opts.In = answers

	// 2. Determine the pathway.
	files, err := l.storedFiles(opts.Err)
	if err != nil {
		return fmt.Errorf("read the organisation files: %w", err)
	}
	if len(files) == 0 {
		return l.noOrg(opts)
	}
	p, err := l.determinePathway(files, added, opts)
	if err != nil {
		return fmt.Errorf("determine the pathway: %w", err)
	}

	// 3. Refresh the pathway's file. When the fetched file no longer has
	// the pathway, the pathway is left and one is determined again.
	p, err = l.refresh(ctx, p, added, opts)
	if errors.Is(err, errNoOrg) {
		return l.noOrg(opts)
	}
	if err != nil {
		return fmt.Errorf("determine the pathway: %w", err)
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
	if p.Kind == orgfile.KindTeleport {
		if err := l.establishTeleportSession(ctx, p, opts); err != nil {
			return fmt.Errorf("establish the Teleport session: %w", err)
		}
	}
	// A direct pathway has no session apart from each service's own.

	// 5. Connect each command group.
	if err := l.connectDevenv(ctx, p, opts); err != nil {
		return fmt.Errorf("connect devenv: %w", err)
	}
	return nil
}

// errNoOrg reports that no organisation file is stored and --org was not
// given.
var errNoOrg = errors.New("no organisation configuration. Run `axon login --org <source>` with the address or file your organisation gives you.")

// noOrg ends a login that finds no stored organisation file to use. The
// active pathway's file is gone, or fails the checks, and no other is
// stored: the pathway is left, which disconnects it, before login asks for
// a file.
func (l *login) noOrg(opts Options) error {
	if err := l.disconnectAll(opts); err != nil {
		return fmt.Errorf("disconnect the previous pathway: %w. Run `axon login` again.", err)
	}
	return errNoOrg
}

// addOrg reads the file --org names, checks it and stores it. It returns
// the stored file and where answers to questions are read from: standard
// input, or with --org - the terminal. When there is no terminal, answers
// come from an empty input, so that a question with a flag stops and names
// the flag, and one without is answered no. closeIn releases the terminal.
func (l *login) addOrg(ctx context.Context, opts Options) (f orgfile.File, in io.Reader, closeIn func(), err error) {
	src, err := orgfile.ParseSource(opts.Org)
	if err != nil {
		return orgfile.File{}, nil, nil, err
	}
	data, err := l.readOrg(ctx, src, opts.In)
	if err != nil {
		return orgfile.File{}, nil, nil, err
	}
	store := orgfile.NewStore(l.dir)
	f, err = store.Add(data, src)
	if err != nil {
		return orgfile.File{}, nil, nil, err
	}
	fmt.Fprintf(opts.Out, "Stored the organisation file %s\n", store.Path(f.ID))

	in, closeIn = opts.In, func() {}
	if src.String() == "-" {
		in = strings.NewReader("")
		if tty, err := l.openTerminal(); err == nil {
			in, closeIn = tty, func() { tty.Close() }
		}
	}
	return f, in, closeIn, nil
}

// storedFiles returns the stored organisation files that pass the checks,
// and writes one line to warn for each one left out.
func (l *login) storedFiles(warn io.Writer) ([]orgfile.File, error) {
	files, problems, err := orgfile.NewStore(l.dir).List()
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
