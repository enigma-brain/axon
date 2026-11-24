package tunnel

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	// zombie is the p_stat of a process that has exited and not been
	// reaped.
	zombie = 5
	// exiting is the p_flag bit of a process that is exiting.
	exiting = 0x2000
)

// lookupProcess reads the process with ID pid from the kernel.
func lookupProcess(pid int) (process, bool, error) {
	kps, err := unix.SysctlKinfoProcSlice("kern.proc.pid", pid)
	if err != nil {
		return process{}, false, fmt.Errorf("read the process %d: %w", pid, err)
	}
	if len(kps) == 0 {
		return process{}, false, nil
	}
	return fromKinfo(kps[0])
}

// processStart returns the start time of the process with ID pid, in
// microseconds since the epoch, while the process exists, exiting and
// zombie included. The ID and start time together identify a process: an
// ID that passes to another process comes with another start time.
func processStart(pid int) (uint64, bool) {
	kps, err := unix.SysctlKinfoProcSlice("kern.proc.pid", pid)
	if err != nil || len(kps) == 0 {
		return 0, false
	}
	tv := kps[0].Proc.P_starttime
	return uint64(tv.Sec)*1_000_000 + uint64(tv.Usec), true
}

// processExited reports whether the process with ID pid has exited: it is
// gone from the kernel's table or is a zombie, whose files are closed.
func processExited(pid int) bool {
	kps, err := unix.SysctlKinfoProcSlice("kern.proc.pid", pid)
	if err != nil {
		return false
	}
	return len(kps) == 0 || kps[0].Proc.P_stat == zombie
}

// listProcesses reads every process from the kernel. A process whose
// arguments cannot be read is listed without them.
func listProcesses() ([]process, error) {
	kps, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, fmt.Errorf("list the processes: %w", err)
	}
	var out []process
	for _, kp := range kps {
		if p, ok, err := fromKinfo(kp); err == nil && ok {
			out = append(out, p)
		}
	}
	return out, nil
}

// fromKinfo turns a kernel process entry into a process, reading its
// arguments. The boolean is false for a process that is exiting or has
// exited and not been reaped.
func fromKinfo(kp unix.KinfoProc) (process, bool, error) {
	if kp.Proc.P_stat == zombie || kp.Proc.P_flag&exiting != 0 {
		return process{}, false, nil
	}
	pid := int(kp.Proc.P_pid)
	p := process{PID: pid, UID: int(kp.Eproc.Pcred.P_ruid)}
	// The kernel refuses the arguments of another user's process and of a
	// process that is exiting; such a process is left without them and
	// never matches the tunnel's command.
	if raw, err := unix.SysctlRaw("kern.procargs2", pid); err == nil {
		p.Args = parseProcargs(raw)
	}
	return p, true, nil
}

// parseProcargs reads the argument vector from a kern.procargs2 buffer: the
// argument count, the executable path, padding, then the arguments, each
// ending with a NUL byte. The arguments keep their exact boundaries.
func parseProcargs(raw []byte) []string {
	if len(raw) < 4 {
		return nil
	}
	argc := int(binary.NativeEndian.Uint32(raw[:4]))
	rest := raw[4:]
	// Skip the executable path and the NUL padding after it.
	i := bytes.IndexByte(rest, 0)
	if i < 0 {
		return nil
	}
	rest = rest[i:]
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}
	args := make([]string, 0, argc)
	for len(args) < argc {
		j := bytes.IndexByte(rest, 0)
		if j < 0 {
			return nil
		}
		args = append(args, string(rest[:j]))
		rest = rest[j+1:]
	}
	return args
}

// lsofPath is where macOS installs lsof. It is run from there, so that a
// PATH without /usr/sbin, or with another lsof first, changes nothing.
const lsofPath = "/usr/sbin/lsof"

// lsofTimeout bounds one run of lsof.
const lsofTimeout = 5 * time.Second

// portListeners asks lsof for the processes listening on port and their
// addresses. lsof sees only the sockets of processes it may inspect.
func portListeners(port int) ([]listener, error) {
	ctx, cancel := context.WithTimeout(context.Background(), lsofTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, lsofPath, "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fptn").Output()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("find the process on port %d: lsof did not finish within %s", port, lsofTimeout)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(bytes.TrimSpace(out)) == 0 {
		// lsof exits non-zero when it finds nothing.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find the process on port %d: lsof: %w", port, err)
	}
	return parseLsof(string(out)), nil
}

// parseLsof reads lsof's -Fptn output: a p line per process, then for each
// of its sockets a t line with the family and an n line with the address.
func parseLsof(out string) []listener {
	var ls []listener
	pid, family := 0, ""
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		val := line[1:]
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(val)
		case 'f':
			family = ""
		case 't':
			family = val
		case 'n':
			i := strings.LastIndexByte(val, ':')
			if i < 0 || pid == 0 {
				continue
			}
			host := strings.Trim(val[:i], "[]")
			var addr netip.Addr
			switch {
			case host == "*" && family == "IPv6":
				addr = netip.IPv6Unspecified()
			case host == "*":
				addr = netip.IPv4Unspecified()
			default:
				a, err := netip.ParseAddr(host)
				if err != nil {
					continue
				}
				addr = a.Unmap()
			}
			ls = append(ls, listener{PID: pid, Addr: addr})
		}
	}
	return ls
}
