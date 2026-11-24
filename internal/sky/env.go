package sky

import (
	"slices"
	"strings"

	"github.com/enigma-brain/axon/internal/skyclient"
)

// The variables axon sets for the SkyPilot client.
const (
	endpointVar = "SKYPILOT_API_SERVER_ENDPOINT"
	userVar     = "SKYPILOT_USER"
)

// clientEnv returns the environment the SkyPilot client runs with: base
// without the variables that point Python elsewhere and without each
// variable vars names, followed by each entry of vars that sets a value. An
// entry is NAME=value, which sets NAME, or NAME alone, which removes it.
// Every other entry of base is kept, in order.
func clientEnv(base []string, vars ...string) []string {
	names := make([]string, 0, len(vars))
	for _, v := range vars {
		name, _, _ := strings.Cut(v, "=")
		names = append(names, name)
	}
	env := skyclient.ClientEnv(base)
	out := make([]string, 0, len(env)+len(vars))
	for _, kv := range env {
		if name, _, _ := strings.Cut(kv, "="); !slices.Contains(names, name) {
			out = append(out, kv)
		}
	}
	for _, v := range vars {
		if strings.Contains(v, "=") {
			out = append(out, v)
		}
	}
	return out
}

// apiEnviron returns the environment sky api login against the tunnel and
// sky api logout run with: base without the endpoint variable, which both
// refuse to run with.
func apiEnviron(base []string) []string {
	return clientEnv(base, endpointVar)
}

// browserLoginEnviron returns the environment the browser login runs with:
// apiEnviron's, with Python's output unbuffered, so that sky's output,
// which goes through a pipe, reaches the terminal as sky writes it.
func browserLoginEnviron(base []string) []string {
	return clientEnv(base, endpointVar, "PYTHONUNBUFFERED=1")
}

// environ returns the environment a forwarded command runs with: base with
// the SkyPilot endpoint, user and project configuration file set to endpoint,
// email and project, replacing any values base holds.
func environ(base []string, endpoint, email, project string) []string {
	return clientEnv(base, endpointVar+"="+endpoint, userVar+"="+email, projectFileVar+"="+project)
}

// helpEnviron returns the environment sky's help runs with: base with the
// SkyPilot endpoint set to deadEndpoint, replacing any value base holds.
func helpEnviron(base []string) []string {
	return clientEnv(base, endpointVar+"="+deadEndpoint)
}
