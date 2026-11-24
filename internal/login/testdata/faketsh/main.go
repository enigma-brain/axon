// Command faketsh stands in for tsh in the login tests. It keeps its state
// in the directory named by FAKETSH_DIR and appends each run's arguments to
// the file "calls" there.
//
//	status --format=json        reports a valid certificate for the proxy
//	                            named in the file "valid" when it exists,
//	                            and exits 1 otherwise
//	login --proxy=<proxy>       writes <proxy> to "valid"
//	--proxy=<proxy> apps login <app>
//	                            succeeds
//	--proxy=<proxy> proxy app <app> --port <n>
//	                            listens on 127.0.0.1:<n> and forwards every
//	                            request to the URL in the file "upstream";
//	                            with "exit" in the file "proxy-mode" it prints
//	                            an error and exits 1, and with "refuse" it
//	                            answers every request with 503
//
// Each of the two logins reads one line of its standard input and writes
// it to "stdin.login" or "stdin.apps". It reads a byte at a time, so a
// line it leaves is there for the next run that shares the input.
package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	dir := os.Getenv("FAKETSH_DIR")
	if dir == "" {
		fmt.Fprintln(os.Stderr, "faketsh: FAKETSH_DIR is not set")
		os.Exit(2)
	}
	args := os.Args[1:]
	f, err := os.OpenFile(filepath.Join(dir, "calls"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		fmt.Fprintln(f, strings.Join(args, " "))
		f.Close()
	}

	switch {
	case len(args) == 2 && args[0] == "status":
		proxyAddr, err := os.ReadFile(filepath.Join(dir, "valid"))
		if err != nil {
			fmt.Fprintln(os.Stderr, "Not logged in.")
			os.Exit(1)
		}
		fmt.Printf(`{"active":{"profile_url":"https://%s","valid_until":%q}}`+"\n",
			strings.TrimSpace(string(proxyAddr)), time.Now().Add(12*time.Hour).UTC().Format(time.RFC3339))
	case len(args) == 2 && args[0] == "login" && strings.HasPrefix(args[1], "--proxy="):
		recordLine(dir, "stdin.login")
		if err := os.WriteFile(filepath.Join(dir, "valid"), []byte(strings.TrimPrefix(args[1], "--proxy=")), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case len(args) == 2 && args[0] == "logout" && strings.HasPrefix(args[1], "--proxy="):
		if _, err := os.Stat(filepath.Join(dir, "logout-fails")); err == nil {
			fmt.Fprintln(os.Stderr, "ERROR: logout failed")
			os.Exit(1)
		}
		os.Remove(filepath.Join(dir, "valid"))
		fmt.Printf("Logged out %s.\n", strings.TrimPrefix(args[1], "--proxy="))
	case len(args) == 4 && strings.HasPrefix(args[0], "--proxy=") && args[1] == "apps" && args[2] == "login":
		recordLine(dir, "stdin.apps")
		fmt.Printf("Logged into app %s\n", args[3])
	case len(args) == 6 && strings.HasPrefix(args[0], "--proxy=") && args[1] == "proxy" && args[2] == "app" && args[4] == "--port":
		proxy(dir, args[3], args[5])
	default:
		fmt.Fprintf(os.Stderr, "faketsh: unexpected arguments %q\n", args)
		os.Exit(2)
	}
}

// recordLine reads one line of standard input, a byte at a time, and
// writes it to the file name in dir.
func recordLine(dir, name string) {
	var line []byte
	b := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(b)
		if n == 1 {
			line = append(line, b[0])
			if b[0] == '\n' {
				break
			}
		}
		if err != nil {
			break
		}
	}
	if err := os.WriteFile(filepath.Join(dir, name), line, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func proxy(dir, app, port string) {
	mode, _ := os.ReadFile(filepath.Join(dir, "proxy-mode"))
	if strings.TrimSpace(string(mode)) == "exit" {
		fmt.Fprintln(os.Stderr, "ERROR: app certificate has expired")
		os.Exit(1)
	}
	data, err := os.ReadFile(filepath.Join(dir, "upstream"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
	upstream, err := url.Parse(strings.TrimSpace(string(data)))
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
	fmt.Printf("Proxying connections to %s on %s\n", app, ln.Addr())
	var h http.Handler = httputil.NewSingleHostReverseProxy(upstream)
	if strings.TrimSpace(string(mode)) == "refuse" {
		h = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		})
	}
	http.Serve(ln, h)
}
