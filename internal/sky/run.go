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

// run replaces the axon process with the sky at path, given args and env,
// without the variables that point Python elsewhere.
func run(path string, args, env []string) error {
	argv := append([]string{path}, args...)
	if err := execSky(path, argv, skyclient.ClientEnv(env)); err != nil {
		return fmt.Errorf("run the SkyPilot client %s: %w", path, err)
	}
	return nil
}

// helpEnviron returns the environment sky's help runs with: the one a
// forwarded command gets when a login is recorded, and the process's own
// otherwise.
func helpEnviron(dir axondir.Dir) []string {
	p, err := activePathway(dir)
	if err != nil {
		return os.Environ()
	}
	email, ok, err := dir.Email()
	if err != nil || !ok {
		return os.Environ()
	}
	return environ(os.Environ(), Endpoint(p), email)
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
