package skyclient

import (
	"slices"
	"strings"
)

// pythonVars are the variables that point Python, or the virtual environment
// it runs in, somewhere other than where the client was installed.
var pythonVars = []string{"VIRTUAL_ENV", "CONDA_PREFIX", "PYTHONHOME", "PYTHONPATH"}

// ClientEnv returns env without the variables that point Python elsewhere,
// so that the client runs with the Python and packages it was installed
// with. Every other entry is kept, in order.
func ClientEnv(env []string) []string {
	return without(env, func(key string) bool { return false })
}

// without returns env without pythonVars and without any variable for which
// drop is true.
func without(env []string, drop func(key string) bool) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if drop(key) || isPythonVar(key) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func isPythonVar(key string) bool { return slices.Contains(pythonVars, key) }
