package sky

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/enigma-brain/axon/internal/apiserver"
)

func TestHasWord(t *testing.T) {
	for text, want := range map[string]bool{
		"needs_auth": true,
		// What sky api login prints when a login ends with needs_auth: the
		// ValueError raised in _show_logged_in_message, in
		// sky/client/sdk.py of skypilot 0.13.0, printed without a traceback.
		"ValueError: Cannot log in API server at https://sky.example.org (status: needs_auth)\n": true,
		"Failed to log in (status: needs_auth)":                                                  true,
		"status: \x1b[31mneeds_auth\x1b[0m":                                                      true,
		"\x1b[1;31mneeds_auth\x1b[0m)":                                                           true,
		"(status:\x1b[0mneeds_auth\x1b[39m)":                                                     true,
		"https://sky.example.org/\x1b[0mx/needs_auth":                                            false,
		"https://sky.example.org/\x1b[0mneeds_auth":                                              false,
		"?status=\x1b[1mneeds_auth\x1b[0m":                                                       false,
		"Open https://sky.example.org/auth:needs_auth":                                           false,
		"(status:needs_auth)":                                                                    true,
		"see https://sky.example.org (status: needs_auth)":                                       true,
		"status needs_auth.":                                                                     true,
		`{"status": "needs_auth"}`:                                                               true,
		"status:needs_auth\n":                                                                    true,
		"https://sky.example.org/needs_auth":                                                     false,
		"https://sky.example.org/?s=needs_auth":                                                  false,
		"x_needs_auth":                                                                           false,
		"needs_auth_soon":                                                                        false,
		"needs_authorization":                                                                    false,
		"/needs_auth then (needs_auth)":                                                          true,
		"":                                                                                       false,
	} {
		if got := hasWord(text, apiserver.NeedsAuth); got != want {
			t.Errorf("hasWord(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestFormatWait(t *testing.T) {
	for d, want := range map[time.Duration]string{
		5 * time.Minute:        "5 minutes",
		time.Minute:            "1 minute",
		300 * time.Millisecond: "300ms",
	} {
		if got := formatWait(d); got != want {
			t.Errorf("formatWait(%s) = %q, want %q", d, got, want)
		}
	}
	if loginWait != 5*time.Minute {
		t.Errorf("loginWait = %s, want 5 minutes", loginWait)
	}
}

func TestSkyLoginKeepsCallerDeadline(t *testing.T) {
	// A sky that waits until it is stopped.
	hang := filepath.Join(t.TempDir(), "sky")
	if err := os.WriteFile(hang, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := Connector{Environ: func() []string { return []string{"PATH=" + os.Getenv("PATH")} }}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := c.skyLogin(ctx, Options{Out: io.Discard, Err: io.Discard}, hang, "http://127.0.0.1:46581")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the caller's deadline", err)
	}
}
