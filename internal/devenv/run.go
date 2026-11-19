package devenv

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
// replaces the axon process with the sky command of c's words followed by
// args.
func forward(ctx context.Context, w io.Writer, c command, args []string) error {
	dir, err := axondir.Default()
	if err != nil {
		return err
	}
	p, email, err := Check(ctx, dir, w)
	if err != nil {
		return err
	}
	path, err := skyPath(dir)
	if err != nil {
		return fmt.Errorf("find the SkyPilot client: %w", err)
	}
	argv := append([]string{path}, c.words...)
	argv = append(argv, args...)
	env := environ(os.Environ(), Endpoint(p), email)
	if err := execSky(path, argv, env); err != nil {
		return fmt.Errorf("run the SkyPilot client %s: %w", path, err)
	}
	return nil
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
