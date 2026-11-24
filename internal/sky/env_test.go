package sky

import (
	"slices"
	"testing"
)

// TestClientEnvOfEachCaller pins the environment each caller gives the
// client, from one environment that holds every variable any of them
// changes.
func TestClientEnvOfEachCaller(t *testing.T) {
	t.Parallel()
	base := []string{
		"PATH=/bin",
		"SKYPILOT_API_SERVER_ENDPOINT=http://shell.example",
		"SKYPILOT_USER=shell-user",
		"PYTHONUNBUFFERED=0",
		"PYTHONPATH=/elsewhere/lib",
		"PYTHONHOME=/elsewhere",
		"VIRTUAL_ENV=/elsewhere/venv",
		"CONDA_PREFIX=/elsewhere/conda",
		"BROWSER=chromium",
		"SKYPILOT_API_SERVER_ENDPOINT_EXTRA=kept",
	}
	for _, tc := range []struct {
		caller string
		env    []string
		want   []string
	}{
		{"the browser login", browserLoginEnviron(base), []string{
			"PATH=/bin",
			"SKYPILOT_USER=shell-user",
			"BROWSER=chromium",
			"SKYPILOT_API_SERVER_ENDPOINT_EXTRA=kept",
			"PYTHONUNBUFFERED=1",
		}},
		{"sky api login against the tunnel, and sky api logout", apiEnviron(base), []string{
			"PATH=/bin",
			"SKYPILOT_USER=shell-user",
			"PYTHONUNBUFFERED=0",
			"BROWSER=chromium",
			"SKYPILOT_API_SERVER_ENDPOINT_EXTRA=kept",
		}},
		{"a forwarded command", environ(base, "http://127.0.0.1:46581", "ada@example.org", "/p.yaml"), []string{
			"PATH=/bin",
			"PYTHONUNBUFFERED=0",
			"BROWSER=chromium",
			"SKYPILOT_API_SERVER_ENDPOINT_EXTRA=kept",
			"SKYPILOT_API_SERVER_ENDPOINT=http://127.0.0.1:46581",
			"SKYPILOT_USER=ada@example.org",
			"SKYPILOT_PROJECT_CONFIG=/p.yaml",
		}},
		{"help", helpEnviron(base), []string{
			"PATH=/bin",
			"SKYPILOT_USER=shell-user",
			"PYTHONUNBUFFERED=0",
			"BROWSER=chromium",
			"SKYPILOT_API_SERVER_ENDPOINT_EXTRA=kept",
			"SKYPILOT_API_SERVER_ENDPOINT=axon-no-server://help",
		}},
	} {
		if !slices.Equal(tc.env, tc.want) {
			t.Errorf("%s runs the client with\n%q\nwant\n%q", tc.caller, tc.env, tc.want)
		}
	}
}
