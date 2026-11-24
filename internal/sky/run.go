package sky

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/user"
	"syscall"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/skyclient"
)

// deps are what the command group depends on outside its arguments. Run,
// CookieFile and SettingsFile use defaults(); tests build their own.
type deps struct {
	// check holds the probes the connection check uses.
	check probes
	// skyPath returns the absolute path of axon's own sky executable, and
	// skyPathFor that of the client at a version already read.
	skyPath    func(axondir.Dir) (string, error)
	skyPathFor func(axondir.Dir, string) string
	// execSky replaces the axon process with the program at path.
	execSky func(path string, argv, env []string) error
	// lookupUser finds an account by name.
	lookupUser func(name string) (*user.User, error)
}

func defaults() deps {
	return deps{
		check:      realProbes(),
		skyPath:    skyclient.Path,
		skyPathFor: skyclient.PathFor,
		execSky:    syscall.Exec,
		lookupUser: user.Lookup,
	}
}

// forward checks the connection, writing what the check repairs to w, then
// replaces the axon process with sky and args.
func (d deps) forward(ctx context.Context, w io.Writer, dir axondir.Dir, args []string) error {
	p, email, version, err := d.checkConnection(ctx, dir, w)
	if err != nil {
		return err
	}
	project, err := writeMark(dir)
	if err != nil {
		return err
	}
	warnProject(w)
	return d.exec(d.skyPathFor(dir, version), args, environ(os.Environ(), Endpoint(p), email, project))
}

// exec replaces the axon process with the sky at path, given args and env.
func (d deps) exec(path string, args, env []string) error {
	argv := append([]string{path}, args...)
	if err := d.execSky(path, argv, env); err != nil {
		return fmt.Errorf("run the SkyPilot client %s: %w", path, err)
	}
	return nil
}

// deadEndpoint is the endpoint sky's help runs against. Its scheme is
// neither http nor https, so the client's HTTP library, which sends only
// those, refuses any request to it before opening a connection, and it is
// not one of the addresses where SkyPilot starts a local API server. Should
// sky run a command where help was expected, it reaches no server. A port
// cannot do this: no port is sure to be free, and the library drops a port
// of 0 and connects to port 80.
const deadEndpoint = "axon-no-server://help"
