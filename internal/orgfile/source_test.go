package orgfile

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tlsServer starts a local TLS server running h and points fetches at it
// for the rest of the test.
func tlsServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	old := transport
	transport = srv.Client().Transport
	t.Cleanup(func() { transport = old })
	return srv
}

func mustSource(t *testing.T, s string) Source {
	t.Helper()
	src, err := ParseSource(s)
	if err != nil {
		t.Fatalf("ParseSource(%q): %v", s, err)
	}
	return src
}

func TestParseSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		in, want string
		address  bool
	}{
		{"-", "-", false},
		{"https://example.org/axon.toml", "https://example.org/axon.toml", true},
		{"~/Downloads/axon.toml", filepath.Join(home, "Downloads", "axon.toml"), false},
		{"/tmp/axon.toml", "/tmp/axon.toml", false},
		{"axon.toml", filepath.Join(cwd, "axon.toml"), false},
	}
	for _, tt := range tests {
		src := mustSource(t, tt.in)
		if src.String() != tt.want || src.IsAddress() != tt.address {
			t.Errorf("ParseSource(%q) = %q, address %v; want %q, address %v",
				tt.in, src.String(), src.IsAddress(), tt.want, tt.address)
		}
		// The recorded form names the same source again.
		if again := mustSource(t, src.raw()); again != src {
			t.Errorf("ParseSource(%q) = %+v, want %+v", src.raw(), again, src)
		}
	}
}

func TestParseSourceRefuses(t *testing.T) {
	for in, part := range map[string]string{
		"":                                  "empty",
		"http://example.org/axon.toml":      "https://",
		"ftp://example.org/axon.toml":       "https://",
		"file:///tmp/axon.toml":             "https://",
		"https:///axon.toml":                "no host",
		"https://ada:pw@example.org/a.toml": "user name or password",
		"http://ada:pw@example.org/a.toml":  "https://",
		"https://ada:pw@exa mple.org/a":     "not a valid URL",
	} {
		_, err := ParseSource(in)
		if err == nil {
			t.Errorf("ParseSource(%q) succeeded", in)
			continue
		}
		if !strings.Contains(err.Error(), part) {
			t.Errorf("ParseSource(%q) = %v, want it to mention %q", in, err, part)
		}
		if strings.Contains(err.Error(), ":pw@") {
			t.Errorf("ParseSource(%q) prints the password: %v", in, err)
		}
	}
}

// secret is the part of an address that can carry a token and that no
// message prints.
const secret = "token=s3cret"

func TestSourceStringHidesTheSecret(t *testing.T) {
	for in, want := range map[string]string{
		"https://example.org/axon.toml?" + secret:           "https://example.org/axon.toml",
		"https://example.org/axon.toml?" + secret + "#frag": "https://example.org/axon.toml",
		"https://example.org/axon.toml#" + secret:           "https://example.org/axon.toml",
		"https://example.org:8443/a%20b.toml?" + secret:     "https://example.org:8443/a%20b.toml",
	} {
		src := mustSource(t, in)
		if got := src.String(); got != want {
			t.Errorf("String() of %q = %q, want %q", in, got, want)
		}
		if got := fmt.Sprintf("%s %v", src, src); strings.Contains(got, "s3cret") {
			t.Errorf("formatting %q prints the secret: %s", in, got)
		}
		// What is recorded is the whole address.
		if src.raw() != in {
			t.Errorf("raw() of %q = %q", in, src.raw())
		}
	}
}

func TestParseSourceRefusalHidesTheSecret(t *testing.T) {
	for _, in := range []string{
		"http://example.org/axon.toml?" + secret,
		"https://ada:pw@example.org/a.toml?" + secret,
		"https:///axon.toml?" + secret,
	} {
		_, err := ParseSource(in)
		if err == nil {
			t.Errorf("ParseSource(%q) succeeded", in)
			continue
		}
		if strings.Contains(err.Error(), "s3cret") || strings.Contains(err.Error(), "ada") {
			t.Errorf("ParseSource(%q) = %v, which prints the secret", in, err)
		}
	}
}

func TestReadAddressFailureHidesTheSecret(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"status", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}},
		{"http redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://example.org/axon.toml?"+secret, http.StatusFound)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotQuery string
			srv := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.RawQuery
				tt.handler(w, r)
			})
			_, err := Read(context.Background(), mustSource(t, srv.URL+"/axon.toml?"+secret), nil)
			if err == nil {
				t.Fatal("Read succeeded")
			}
			if gotQuery != secret {
				t.Errorf("the fetch sent the query %q, want %q", gotQuery, secret)
			}
			if !strings.Contains(err.Error(), srv.URL+"/axon.toml") || strings.Contains(err.Error(), "s3cret") {
				t.Errorf("Read = %v, want an error naming %s/axon.toml without the secret", err, srv.URL)
			}
		})
	}
}

func TestReadStdin(t *testing.T) {
	data, err := Read(context.Background(), mustSource(t, "-"), strings.NewReader(campusFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != campusFile {
		t.Errorf("Read(-) = %q", data)
	}
}

func TestReadPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "remote.toml")
	if err := os.WriteFile(path, []byte(remoteFile), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := Read(context.Background(), mustSource(t, "~/remote.toml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != remoteFile {
		t.Errorf("Read = %q", data)
	}

	missing := filepath.Join(home, "missing.toml")
	_, err = Read(context.Background(), mustSource(t, missing), nil)
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Errorf("Read(missing) = %v, want an error naming %s", err, missing)
	}
}

func TestReadAddress(t *testing.T) {
	var gotCookie, gotAuth string
	srv := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotCookie, gotAuth = r.Header.Get("Cookie"), r.Header.Get("Authorization")
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "x"})
		w.Write([]byte(campusFile))
	})
	src := mustSource(t, srv.URL+"/axon.toml")
	for range 2 {
		data, err := Read(context.Background(), src, nil)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != campusFile {
			t.Errorf("Read = %q", data)
		}
	}
	if gotCookie != "" || gotAuth != "" {
		t.Errorf("the fetch sent Cookie %q and Authorization %q", gotCookie, gotAuth)
	}
}

func TestReadAddressFollowsHTTPSRedirect(t *testing.T) {
	srv := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new", http.StatusFound)
			return
		}
		w.Write([]byte(remoteFile))
	})
	data, err := Read(context.Background(), mustSource(t, srv.URL+"/old"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != remoteFile {
		t.Errorf("Read = %q", data)
	}
}

func TestReadAddressFailures(t *testing.T) {
	big := bytes.Repeat([]byte("#"), MaxSize+1)
	tests := []struct {
		name    string
		handler http.HandlerFunc
		part    string
	}{
		{"status", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}, "404"},
		{"http redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://example.org/axon.toml", http.StatusFound)
		}, "not an https address"},
		{"too large", func(w http.ResponseWriter, r *http.Request) {
			w.Write(big)
		}, "larger than"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tlsServer(t, tt.handler)
			address := srv.URL + "/axon.toml"
			_, err := Read(context.Background(), mustSource(t, address), nil)
			if err == nil {
				t.Fatal("Read succeeded")
			}
			if !strings.Contains(err.Error(), address) || !strings.Contains(err.Error(), tt.part) {
				t.Errorf("Read = %v, want an error naming %s and %q", err, address, tt.part)
			}
		})
	}
}

func TestReadAddressTimeLimit(t *testing.T) {
	release := make(chan struct{})
	srv := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	old := fetchTimeout
	fetchTimeout = 50 * time.Millisecond
	t.Cleanup(func() { fetchTimeout = old })

	address := srv.URL + "/axon.toml"
	_, err := Read(context.Background(), mustSource(t, address), nil)
	if err == nil {
		t.Fatal("Read succeeded")
	}
	if !strings.Contains(err.Error(), address) || !strings.Contains(err.Error(), "Timeout") {
		t.Errorf("Read = %v, want an error naming %s and the time limit", err, address)
	}
}

func TestReadStdinTooLarge(t *testing.T) {
	_, err := Read(context.Background(), mustSource(t, "-"), bytes.NewReader(bytes.Repeat([]byte("#"), MaxSize+1)))
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("Read = %v, want a size refusal", err)
	}
}
