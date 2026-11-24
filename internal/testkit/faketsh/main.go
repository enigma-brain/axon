// Command faketsh stands in for tsh in the tests. A test builds it with
// testkit.Fence, into a file named tsh, so that the tunnel package
// recognizes its command line.
//
// When FAKETSH_DIR names a directory, the fake keeps its state there and
// appends each run's arguments to the file "calls" in it. It then accepts:
//
//	status --format=json        reports a valid certificate for the proxy
//	                            named in the file "valid" when it exists,
//	                            and exits 1 otherwise
//	login --proxy=<proxy>       writes <proxy> to "valid"
//	logout --proxy=<proxy>      removes "valid", and fails when the file
//	                            "logout-fails" exists
//	--proxy=<proxy> apps login <app>
//	                            succeeds
//
// Each of the two logins reads one line of its standard input and writes
// it to "stdin.login" or "stdin.apps". It reads a byte at a time, so a
// line it leaves is there for the next run that shares the input.
//
// With or without FAKETSH_DIR, it accepts the tunnel's command,
// `[--proxy=<proxy>] proxy app <app> --port <port>`, and listens on
// FAKETSH_HOST, 127.0.0.1 by default, at the port. FAKETSH_MODE, or else
// the file "proxy-mode" in FAKETSH_DIR, selects what it does:
//
//	serve    answer requests (the default)
//	refuse   answer with 503
//	redirect redirect /api/health to a page that answers 200
//	idle     run without listening
//	exit     print a message to standard error and exit with status 1
//
// With FAKETSH_DIR set, it answers by forwarding every request to the URL
// in the file "upstream", and refuse answers every request with 503.
// Without it, it answers /api/health itself, with 200, or 503 for refuse,
// and every other path with 404.
package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	dir := os.Getenv("FAKETSH_DIR")
	args := os.Args[1:]
	if dir != "" {
		f, err := os.OpenFile(filepath.Join(dir, "calls"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintln(f, strings.Join(args, " "))
			f.Close()
		}
	}

	if app, port, ok := proxyArgs(args); ok {
		proxy(dir, app, port)
		return
	}
	if dir == "" {
		fmt.Fprintf(os.Stderr, "faketsh: FAKETSH_DIR is not set for %q\n", args)
		os.Exit(2)
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
	default:
		fmt.Fprintf(os.Stderr, "faketsh: unexpected arguments %q\n", args)
		os.Exit(2)
	}
}

// proxyArgs returns the application and port of the tunnel's command, and
// whether args are that command.
func proxyArgs(args []string) (app string, port int, ok bool) {
	if len(args) > 0 && strings.HasPrefix(args[0], "--proxy=") {
		args = args[1:]
	}
	if len(args) != 5 || args[0] != "proxy" || args[1] != "app" || args[3] != "--port" {
		return "", 0, false
	}
	port, err := strconv.Atoi(args[4])
	if err != nil {
		fmt.Fprintf(os.Stderr, "faketsh: bad port %q\n", args[4])
		os.Exit(2)
	}
	return args[2], port, true
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

// proxy runs the tunnel's command for app on port.
func proxy(dir, app string, port int) {
	mode := os.Getenv("FAKETSH_MODE")
	if mode == "" && dir != "" {
		data, _ := os.ReadFile(filepath.Join(dir, "proxy-mode"))
		mode = strings.TrimSpace(string(data))
	}
	if mode == "exit" {
		fmt.Fprintln(os.Stderr, "ERROR: app certificate has expired")
		os.Exit(1)
	}
	if mode == "idle" {
		for {
			time.Sleep(time.Hour)
		}
	}

	var h http.Handler
	if dir != "" {
		h = forward(dir, mode)
	} else {
		h = health(mode)
	}

	host := os.Getenv("FAKETSH_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Proxying connections to %s on %s\n", app, ln.Addr())
	http.Serve(ln, h)
}

// forward returns the handler that forwards every request to the URL in
// the file "upstream" in dir, or answers every request with 503 in the
// mode refuse.
func forward(dir, mode string) http.Handler {
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
	if mode == "refuse" {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		})
	}
	return httputil.NewSingleHostReverseProxy(upstream)
}

// health returns the handler that answers /api/health itself, as mode
// says.
func health(mode string) http.Handler {
	status := http.StatusOK
	if mode == "refuse" {
		status = http.StatusServiceUnavailable
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode == "redirect" && r.URL.Path == "/api/health" {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		if mode == "redirect" && r.URL.Path == "/login" {
			fmt.Fprintln(w, "log in")
			return
		}
		if r.URL.Path != "/api/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		fmt.Fprintln(w, `{"status":"healthy"}`)
	})
}
