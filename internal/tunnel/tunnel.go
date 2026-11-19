// Package tunnel starts, checks, adopts and stops the Teleport tunnel to
// the SkyPilot API server, a detached process that outlives axon.
//
// The tunnel reaches a Teleport application through a Teleport proxy, both
// given by the caller. A process is the tunnel when it belongs to the
// current user, the base name of its first argument is tsh, and its
// remaining arguments are exactly
// "--proxy=<proxy> proxy app <app> --port 46581". Any other spelling of the
// command, or a tunnel to another proxy or application, is another process.
//
// The tunnel's process ID and arguments are recorded in tunnel.json and its
// output kept in tunnel.log, both in the state directory. A recorded process
// is taken for the tunnel only while its command line is still the recorded
// one.
package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/enigma-brain/axon/internal/tsh"
)

// Port is the local port the tunnel listens on, the same for every
// organisation. It is not 46580, the address SkyPilot reserves for an API
// server on the laptop.
const Port = 46581

const (
	recordFile = "tunnel.json"
	logFile    = "tunnel.log"
	// oldRecordFile held only a process ID, with no command line. A record
	// in it is never trusted; it is deleted and its process left alone.
	oldRecordFile = "tunnel.pid"

	fileMode fs.FileMode = 0o600

	// startTimeout bounds the wait for a started tunnel to serve.
	startTimeout = 10 * time.Second
	// pollInterval is the pause between health requests while starting.
	pollInterval = 250 * time.Millisecond
	// probeTimeout bounds one health request.
	probeTimeout = 2 * time.Second
	// logTailLines is how many of the log's last lines a failed start
	// returns.
	logTailLines = 20
)

// stopTimeout is how long a tunnel has to exit after SIGTERM before it is
// sent SIGKILL, and killTimeout how long after SIGKILL. sendSignal sends a
// signal. Tests change them.
var (
	stopTimeout = 3 * time.Second
	killTimeout = time.Second
	sendSignal  = syscall.Kill
)

// Status holds two separate facts about the tunnel, so that a failed tunnel
// can be told from an unavailable server.
type Status struct {
	// Running is true when the tunnel's process exists.
	Running bool
	// Healthy is true when the health endpoint answers through the tunnel.
	Healthy bool
	// PID is the process ID of the tunnel when Running is true.
	PID int
}

// PortBusyError reports a process other than the tunnel holding Port.
type PortBusyError struct {
	PID     int
	Command string
}

func (e *PortBusyError) Error() string {
	return fmt.Sprintf("port %d is held by process %d (%s), which is not the Teleport tunnel", Port, e.PID, e.Command)
}

// StartError reports a started tunnel that did not come to serve. Start has
// stopped the process and deleted the record by the time it returns one.
type StartError struct {
	// Exited is true when the process exited before it served, and false
	// when it ran without answering until the wait ran out.
	Exited bool
	// Log is the end of the tunnel's log.
	Log string

	reason  string
	logPath string
}

func (e *StartError) Error() string {
	return fmt.Sprintf("the Teleport tunnel did not start: %s. The end of %s:\n%s", e.reason, e.logPath, e.Log)
}

// Tunnel is the tunnel to one Teleport application through one Teleport
// proxy, whose record and log are kept in a state directory.
type Tunnel struct {
	stateDir string
	tshPath  string
	// proxy is the Teleport proxy, a host or host:port, and app the
	// Teleport application.
	proxy, app string
	// port is the local port, Port outside tests.
	port int
}

// New returns the tunnel to the Teleport application app through the
// Teleport proxy proxy, run with tshPath and recorded in stateDir.
func New(stateDir, tshPath, proxy, app string) *Tunnel {
	return NewOnPort(stateDir, tshPath, proxy, app, Port)
}

// NewOnPort is New with the tunnel listening on port. Outside tests the
// port is Port; tests pass a free one.
func NewOnPort(stateDir, tshPath, proxy, app string, port int) *Tunnel {
	return &Tunnel{stateDir: stateDir, tshPath: tshPath, proxy: proxy, app: app, port: port}
}

// args returns the tunnel's arguments after the program name.
func (t *Tunnel) args() []string {
	return []string{"--proxy=" + t.proxy, "proxy", "app", t.app, "--port", strconv.Itoa(t.port)}
}

// checkTarget returns an error unless the proxy and the application can be
// passed to tsh.
func (t *Tunnel) checkTarget() error {
	if err := tsh.CheckProxy(t.proxy); err != nil {
		return fmt.Errorf("start the Teleport tunnel: %w", err)
	}
	if err := tsh.CheckApp(t.app); err != nil {
		return fmt.Errorf("start the Teleport tunnel: %w", err)
	}
	return nil
}

func (t *Tunnel) logPath() string { return filepath.Join(t.stateDir, logFile) }

// Status reports whether the tunnel's process runs and whether it serves.
// A process that runs and does not serve is Running and not Healthy.
// Status adopts no process; it discards a record whose process is not the
// tunnel.
func (t *Tunnel) Status(ctx context.Context) (Status, error) {
	procs, err := t.find()
	if err != nil {
		return Status{}, err
	}
	if len(procs) == 0 {
		return Status{}, nil
	}
	pid, healthy, err := t.up(ctx, procs)
	if err != nil {
		return Status{}, err
	}
	return Status{Running: true, Healthy: healthy, PID: pid}, nil
}

// Start makes sure the tunnel runs and serves.
//
// A tunnel that is up, recorded or started by hand, is used as it is and
// recorded: Start never restarts a tunnel that is up, so a caller that needs
// a fresh application certificate to take effect calls Stop first. A tunnel
// that is down is stopped and replaced. When another process holds the
// port, Start leaves it running and returns a *PortBusyError.
//
// A new tunnel runs detached, in its own session, and outlives axon and its
// terminal. Start waits up to 10 seconds for it to serve. When it does not,
// Start stops it, deletes the record, and returns a *StartError that carries
// the end of the tunnel's log.
func (t *Tunnel) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := t.checkTarget(); err != nil {
		return err
	}
	procs, err := t.find()
	if err != nil {
		return err
	}
	if len(procs) > 0 {
		pid, healthy, err := t.up(ctx, procs)
		if err != nil {
			return err
		}
		if healthy {
			return writeRecord(t.stateDir, record{PID: pid, Args: t.args()})
		}
		// A cancelled context fails the health request; that says nothing
		// about the tunnel, so it is left running.
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := t.stopAll(procs); err != nil {
			return err
		}
	}
	if err := t.checkPortFree(); err != nil {
		return err
	}
	return t.launch(ctx)
}

// Stop stops the tunnel, when one is running, and deletes the record. It
// stops only processes that are the tunnel; a recorded process ID that has
// passed to another process is discarded and that process left alone.
func (t *Tunnel) Stop() error {
	procs, err := t.find()
	if err != nil {
		return err
	}
	return t.stopAll(procs)
}

// stopAll stops each of pids that is this tunnel and deletes this tunnel's
// record. A record of a tunnel to another proxy or application is kept.
func (t *Tunnel) stopAll(pids []int) error {
	if err := stopAll(pids, t.isTunnel); err != nil {
		return err
	}
	rec, ok, err := readRecord(t.stateDir)
	if err != nil {
		return err
	}
	if ok && isTunnelShape(rec.Args) && !slices.Equal(rec.Args, t.args()) {
		return nil
	}
	return removeRecord(t.stateDir)
}

// RecordedRunning reports whether the tunnel recorded in stateDir runs: its
// process belongs to the user and its command line is the recorded one. It
// discards a record that does not match its process.
func RecordedRunning(stateDir string) (bool, error) {
	_, ok, err := recordedProcess(stateDir)
	return ok, err
}

// StopRecorded stops the tunnel recorded in stateDir, when it runs, and
// deletes the record. It needs neither the proxy nor the application: the
// recorded process is stopped only when it belongs to the user and its
// command line is still the recorded one. Otherwise the process is left
// alone and the record discarded.
func StopRecorded(stateDir string) error {
	rec, ok, err := recordedProcess(stateDir)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if err := stopAll([]int{rec.PID}, rec.matches); err != nil {
		return err
	}
	return removeRecord(stateDir)
}

// recordedProcess returns the record in stateDir when its process runs and
// matches it. It discards the old form of record, and a record whose
// process has exited or does not match.
func recordedProcess(stateDir string) (record, bool, error) {
	if err := removeOldRecord(stateDir); err != nil {
		return record{}, false, err
	}
	rec, ok, err := readRecord(stateDir)
	if err != nil || !ok {
		return record{}, false, err
	}
	if rec.PID > 0 && isTunnelShape(rec.Args) {
		p, live, err := lookupProcess(rec.PID)
		if err != nil {
			return record{}, false, err
		}
		if live && rec.matches(p) {
			return rec, true, nil
		}
	}
	return record{}, false, removeRecord(stateDir)
}

// find returns the IDs of the user's tunnel processes, the recorded one
// first. It discards a record whose process does not match it.
func (t *Tunnel) find() ([]int, error) {
	var out []int
	rec, ok, err := recordedProcess(t.stateDir)
	if err != nil {
		return nil, err
	}
	// A recorded tunnel to another proxy or application is not this
	// tunnel. Its process is left alone and its record kept, so that it can
	// still be stopped by its record.
	if ok && slices.Equal(rec.Args, t.args()) {
		out = append(out, rec.PID)
	}
	all, err := listProcesses()
	if err != nil {
		return nil, err
	}
	for _, p := range all {
		if t.isTunnel(p) && !slices.Contains(out, p.PID) {
			out = append(out, p.PID)
		}
	}
	return out, nil
}

// isTunnel reports whether p is the user's tunnel.
func (t *Tunnel) isTunnel(p process) bool {
	return p.UID == os.Getuid() && isTunnelCommand(p.Args, t.args())
}

// up returns, of procs, the one that serves, and whether one does. A tunnel
// serves when the health endpoint answers and the address the request
// reached is one a tunnel process listens on; whatever else answers on the
// port does not count. When none serves, the first that listens, or else
// the first, is returned.
func (t *Tunnel) up(ctx context.Context, procs []int) (int, bool, error) {
	listeners, err := portListeners(t.port)
	if err != nil {
		return 0, false, err
	}
	var own []listener
	for _, l := range listeners {
		if slices.Contains(procs, l.PID) {
			own = append(own, l)
		}
	}
	if len(own) == 0 {
		return procs[0], false, nil
	}
	remote, ok := t.healthy(ctx)
	if ok {
		for _, l := range own {
			if l.accepts(remote) {
				return l.PID, true, nil
			}
		}
	}
	return own[0].PID, false, nil
}

// checkPortFree returns a *PortBusyError when a process holds the port.
func (t *Tunnel) checkPortFree() error {
	listeners, err := portListeners(t.port)
	if err != nil {
		return err
	}
	if len(listeners) > 0 {
		pid := listeners[0].PID
		cmd := "unknown command"
		if p, ok, err := lookupProcess(pid); err == nil && ok {
			cmd = p.command()
		}
		return &PortBusyError{PID: pid, Command: cmd}
	}
	// A listener of another user is invisible to the process table but
	// still accepts connections.
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("localhost", strconv.Itoa(t.port)), probeTimeout)
	if err == nil {
		conn.Close()
		return &PortBusyError{PID: 0, Command: "a process axon cannot inspect"}
	}
	return nil
}

// launch starts a new tunnel and waits for it to serve.
func (t *Tunnel) launch(ctx context.Context) error {
	if err := os.MkdirAll(t.stateDir, 0o700); err != nil {
		return fmt.Errorf("start the Teleport tunnel: %w", err)
	}
	logf, err := os.OpenFile(t.logPath(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode)
	if err != nil {
		return fmt.Errorf("start the Teleport tunnel: %w", err)
	}
	defer logf.Close()

	cmd := exec.Command(t.tshPath, t.args()...)
	cmd.Stdout = logf
	cmd.Stderr = logf
	// A new session detaches the tunnel from axon's terminal, so closing
	// the terminal does not send it SIGHUP.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start the Teleport tunnel: %w", err)
	}
	pid := cmd.Process.Pid
	// Reap the tunnel if it exits while axon runs; otherwise axon exits
	// first and the tunnel carries on.
	exited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(exited)
	}()

	if err := writeRecord(t.stateDir, record{PID: pid, Args: t.args()}); err != nil {
		terminate(pid, exited, nil)
		return err
	}

	deadline := time.NewTimer(startTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	reason := fmt.Sprintf("it did not answer on %s within %s", t.healthURL(), startTimeout)
	exitedFirst := false
	for {
		if _, healthy, err := t.up(ctx, []int{pid}); err == nil && healthy {
			return nil
		}
		select {
		case <-tick.C:
			continue
		case <-exited:
			reason = "it exited"
			exitedFirst = true
		case <-deadline.C:
		case <-ctx.Done():
			reason = ctx.Err().Error()
		}
		break
	}
	terminate(pid, exited, nil)
	if err := removeRecord(t.stateDir); err != nil {
		return err
	}
	return &StartError{Exited: exitedFirst, Log: t.logTail(), reason: reason, logPath: t.logPath()}
}

// stopAll stops each of pids that is a tunnel, as isTunnel says. A process
// is identified by its ID and start time, captured before it is signalled:
// it is signalled only while both still match, and when it has not exited
// and still matches after the wait, stopAll returns an error, and the
// caller keeps the record.
func stopAll(pids []int, isTunnel func(process) bool) error {
	for _, pid := range pids {
		start, ok := processStart(pid)
		if !ok {
			// No start time is read for a process that has exited. One that
			// still runs as the tunnel cannot be stopped safely without it,
			// so the caller keeps the record.
			p, live, err := lookupProcess(pid)
			if err != nil {
				return err
			}
			if live && isTunnel(p) {
				return fmt.Errorf("could not read the start time of the Teleport tunnel, process %d. Stop it with `kill %d` and try again", pid, pid)
			}
			continue
		}
		// Confirm the command line after taking the start time, so the
		// start time is the tunnel's.
		// A process that cannot be read may still be the tunnel, so the
		// caller keeps the record.
		p, live, err := lookupProcess(pid)
		if err != nil {
			return err
		}
		if !live || !isTunnel(p) {
			continue
		}
		same := func() bool {
			s, ok := processStart(pid)
			return ok && s == start
		}
		terminate(pid, nil, same)
		if !processExited(pid) && same() {
			return fmt.Errorf("could not stop the Teleport tunnel, process %d. Stop it with `kill -9 %d` and try again", pid, pid)
		}
	}
	return nil
}

// terminate sends pid SIGTERM and, when it has not exited within
// stopTimeout, SIGKILL, and waits for it to exit.
//
// When exited is not nil, pid is axon's own child, and exited closes once
// the child has been reaped; until then the ID cannot pass to another
// process. Otherwise same confirms before each signal that pid is still the
// same process, so a signal never reaches a process that took the ID after
// the tunnel exited. The wait ends only when the process has exited, its
// sockets closed.
func terminate(pid int, exited <-chan struct{}, same func() bool) {
	done := func() bool {
		if exited != nil {
			select {
			case <-exited:
				return true
			default:
				return false
			}
		}
		return processExited(pid) || !same()
	}
	wait := func(d time.Duration) bool {
		deadline := time.Now().Add(d)
		for !done() {
			if time.Now().After(deadline) {
				return false
			}
			time.Sleep(50 * time.Millisecond)
		}
		return true
	}
	for _, step := range []struct {
		sig  syscall.Signal
		wait time.Duration
	}{{syscall.SIGTERM, stopTimeout}, {syscall.SIGKILL, killTimeout}} {
		if done() {
			return
		}
		sendSignal(pid, step.sig)
		if wait(step.wait) {
			return
		}
	}
}

func (t *Tunnel) healthURL() string {
	return "http://localhost:" + strconv.Itoa(t.port) + "/api/health"
}

// healthy reports whether the health endpoint answers with a 2xx status
// within probeTimeout, and the address the request reached.
func (t *Tunnel) healthy(ctx context.Context) (netip.Addr, bool) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.healthURL(), nil)
	if err != nil {
		return netip.Addr{}, false
	}
	var remote netip.Addr
	dialer := &net.Dialer{}
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:             nil,
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				conn, err := dialer.DialContext(ctx, network, addr)
				if err == nil {
					if a, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
						remote = a.AddrPort().Addr().Unmap()
					}
				}
				return conn, err
			},
		},
		// A redirect is not an answer from the health endpoint.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return netip.Addr{}, false
	}
	resp.Body.Close()
	return remote, resp.StatusCode >= 200 && resp.StatusCode < 300 && remote.IsValid()
}

// record is what tunnel.json holds: the tunnel's process ID and its
// arguments after the program name.
type record struct {
	PID  int      `json:"pid"`
	Args []string `json:"args"`
}

// matches reports whether p is the recorded tunnel: it belongs to the user,
// the base name of its first argument is tsh, and its remaining arguments
// are the recorded ones.
func (r record) matches(p process) bool {
	return p.UID == os.Getuid() && isTunnelCommand(p.Args, r.Args)
}

// isTunnelShape reports whether args, after the program name, have the form
// of a tunnel's command: "--proxy=<proxy> proxy app <app> --port <port>".
func isTunnelShape(args []string) bool {
	return len(args) == 6 &&
		strings.HasPrefix(args[0], "--proxy=") && len(args[0]) > len("--proxy=") &&
		args[1] == "proxy" && args[2] == "app" && args[3] != "" && args[4] == "--port"
}

func recordPath(stateDir string) string { return filepath.Join(stateDir, recordFile) }

// readRecord returns the record in stateDir. The boolean is false when there
// is none. A record that cannot be parsed is returned empty, with true, so
// that it is discarded.
func readRecord(stateDir string) (record, bool, error) {
	path := recordPath(stateDir)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return record{}, false, nil
	}
	if err != nil {
		return record{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	var rec record
	if err := json.Unmarshal(data, &rec); err != nil {
		return record{}, true, nil
	}
	return rec, true, nil
}

// writeRecord writes rec as the tunnel's record in stateDir.
func writeRecord(stateDir string, rec record) error {
	path := recordPath(stateDir)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("record the Teleport tunnel: %w", err)
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(stateDir, "."+recordFile+".*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	_, werr := tmp.Write(append(data, '\n'))
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// removeRecord deletes the record in stateDir, when there is one.
func removeRecord(stateDir string) error {
	return removeFile(recordPath(stateDir))
}

// removeOldRecord deletes the old form of record in stateDir, when there is
// one. Its process is never signalled.
func removeOldRecord(stateDir string) error {
	return removeFile(filepath.Join(stateDir, oldRecordFile))
}

func removeFile(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

// logTail returns the last logTailLines lines of the log.
func (t *Tunnel) logTail() string {
	data, err := os.ReadFile(t.logPath())
	if err != nil {
		return fmt.Sprintf("(the log cannot be read: %v)", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > logTailLines {
		lines = lines[len(lines)-logTailLines:]
	}
	text := strings.Join(lines, "\n")
	if strings.TrimSpace(text) == "" {
		return "(the log is empty)"
	}
	return text
}
