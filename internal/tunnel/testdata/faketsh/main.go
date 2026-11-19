// Command faketsh stands in for
// `tsh --proxy=<proxy> proxy app <app> --port <port>` in the tunnel tests.
// It also accepts the command without --proxy, as an older axon ran it. FAKETSH_MODE selects what it does:
//
//	serve   listen on the port and answer /api/health with 200 (the default)
//	refuse  listen on the port and answer /api/health with 503
//	redirect listen on the port and redirect /api/health to a page that
//	        answers 200
//	idle    run without listening
//	exit    print a message to standard error and exit with status 1
//
// FAKETSH_HOST is the address it listens on, 127.0.0.1 by default.
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && strings.HasPrefix(args[0], "--proxy=") {
		args = args[1:]
	}
	if len(args) != 5 || args[0] != "proxy" || args[1] != "app" || args[3] != "--port" {
		fmt.Fprintf(os.Stderr, "faketsh: unexpected arguments %q\n", args)
		os.Exit(2)
	}
	port, err := strconv.Atoi(args[4])
	if err != nil {
		fmt.Fprintf(os.Stderr, "faketsh: bad port %q\n", args[4])
		os.Exit(2)
	}

	mode := os.Getenv("FAKETSH_MODE")
	if mode == "exit" {
		fmt.Fprintln(os.Stderr, "ERROR: app certificate has expired")
		os.Exit(1)
	}
	if mode == "idle" {
		for {
			time.Sleep(time.Hour)
		}
	}
	status := http.StatusOK
	if mode == "refuse" {
		status = http.StatusServiceUnavailable
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
	fmt.Printf("Proxying connections to %s on %s\n", args[2], ln.Addr())
	http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))
}
