package sky

import (
	"context"
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/skyclient"
)

// skyPath returns the absolute path of axon's own sky executable, and
// skyPathFor that of the client at a version already read. Tests replace
// them with a fake.
var (
	skyPath    = skyclient.Path
	skyPathFor = skyclient.PathFor
)

// execSky replaces the axon process with the program at path. Tests replace
// it to capture the call.
var execSky = syscall.Exec

// forward checks the connection, writing what the check repairs to w, then
// replaces the axon process with sky and args.
func forward(ctx context.Context, w io.Writer, dir axondir.Dir, args []string) error {
	p, email, version, err := checkConnection(ctx, dir, w)
	if err != nil {
		return err
	}
	return run(skyPathFor(dir, version), args, environ(os.Environ(), Endpoint(p), email))
}

// run replaces the axon process with the sky at path, given args and env.
func run(path string, args, env []string) error {
	argv := append([]string{path}, args...)
	if err := execSky(path, argv, env); err != nil {
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
