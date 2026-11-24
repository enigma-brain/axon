package sky

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/skyclient"
)

// The variables axon sets for the SkyPilot client.
const (
	endpointVar = "SKYPILOT_API_SERVER_ENDPOINT"
	userVar     = "SKYPILOT_USER"
)

// skyPath returns the absolute path of axon's own sky executable. Tests
// replace it with a fake.
var skyPath = skyclient.Path

// execSky replaces the axon process with the program at path. Tests replace
// it to capture the call.
var execSky = syscall.Exec

// forward checks the connection, writing what the check repairs to w, then
// replaces the axon process with sky and args.
func forward(ctx context.Context, w io.Writer, dir axondir.Dir, args []string) error {
	p, email, err := Check(ctx, dir, w)
	if err != nil {
		return err
	}
	path, err := skyPath(dir)
	if err != nil {
		return fmt.Errorf("find the SkyPilot client: %w", err)
	}
	return run(path, args, environ(os.Environ(), Endpoint(p), email))
}

// run replaces the axon process with the sky at path, given args and env,
// without the variables that point Python elsewhere.
func run(path string, args, env []string) error {
	argv := append([]string{path}, args...)
	if err := execSky(path, argv, skyclient.ClientEnv(env)); err != nil {
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

// helpEnviron returns base with the SkyPilot endpoint set to deadEndpoint,
// replacing any value base holds. Every other entry is kept.
func helpEnviron(base []string) []string {
	env := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if strings.HasPrefix(kv, endpointVar+"=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, endpointVar+"="+deadEndpoint)
}

// environ returns base with the SkyPilot endpoint and user set to endpoint
// and email, replacing any values base holds. Every other entry is kept.
func environ(base []string, endpoint, email string) []string {
	env := make([]string, 0, len(base)+2)
	for _, kv := range base {
		if strings.HasPrefix(kv, endpointVar+"=") || strings.HasPrefix(kv, userVar+"=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, endpointVar+"="+endpoint, userVar+"="+email)
}
