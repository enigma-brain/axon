package tunnel

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// pfExiting is the kernel flag of a process that is exiting.
const pfExiting = 0x4

// lookupProcess reads the process with ID pid from /proc. A process that is
// exiting or has exited and not been reaped counts as gone.
func lookupProcess(pid int) (process, bool, error) {
	dir := filepath.Join("/proc", strconv.Itoa(pid))
	stat, err := os.ReadFile(filepath.Join(dir, "stat"))
	if processGone(err) {
		return process{}, false, nil
	}
	if err != nil {
		return process{}, false, fmt.Errorf("read the process %d: %w", pid, err)
	}
	// The state follows the command name, which is in parentheses and may
	// itself contain spaces and parentheses.
	i := bytes.LastIndexByte(stat, ')')
	if i < 0 {
		return process{}, false, fmt.Errorf("read the process %d: unexpected %s", pid, filepath.Join(dir, "stat"))
	}
	// fields[0] is the state and fields[6] the kernel flags. A process
	// that is exiting has given up its arguments before it turns zombie.
	fields := strings.Fields(string(stat[i+1:]))
	if len(fields) < 7 || fields[0] == "Z" || fields[0] == "X" {
		return process{}, false, nil
	}
	if flags, err := strconv.ParseUint(fields[6], 10, 64); err == nil && flags&pfExiting != 0 {
		return process{}, false, nil
	}

	uid, err := readUID(filepath.Join(dir, "status"))
	if processGone(err) {
		return process{}, false, nil
	}
	if err != nil {
		return process{}, false, fmt.Errorf("read the process %d: %w", pid, err)
	}

	cmdline, err := os.ReadFile(filepath.Join(dir, "cmdline"))
	if processGone(err) {
		return process{}, false, nil
	}
	if err != nil {
		return process{}, false, fmt.Errorf("read the process %d: %w", pid, err)
	}
	var args []string
	if len(cmdline) > 0 {
		args = strings.Split(strings.TrimSuffix(string(cmdline), "\x00"), "\x00")
	}
	return process{PID: pid, UID: uid, Args: args}, true, nil
}

// processStart returns the start time of the process with ID pid, in clock
// ticks since boot, while the process exists, exiting and zombie included.
// The ID and start time together identify a process: an ID that passes to
// another process comes with another start time.
func processStart(pid int) (uint64, bool) {
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	i := bytes.LastIndexByte(stat, ')')
	if i < 0 {
		return 0, false
	}
	// The fields after the command name start at the third; the start
	// time is the twenty-second.
	fields := strings.Fields(string(stat[i+1:]))
	if len(fields) < 20 {
		return 0, false
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	return start, err == nil
}

// processExited reports whether the process with ID pid has exited: it is
// gone from /proc, or is a zombie with no thread left, so its files are
// closed.
func processExited(pid int) bool {
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return processGone(err)
	}
	i := bytes.LastIndexByte(stat, ')')
	if i < 0 {
		return false
	}
	fields := strings.Fields(string(stat[i+1:]))
	if len(fields) == 0 || (fields[0] != "Z" && fields[0] != "X") {
		return false
	}
	// A zombie leader's other threads hold the shared file table until
	// they too have exited; then the leader alone remains in task.
	tasks, err := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "task"))
	if err != nil {
		return processGone(err)
	}
	return len(tasks) <= 1
}

// processGone reports whether err, from reading a process's files in
// /proc, means the process no longer exists. A process that exits after
// its file is opened makes the read fail with ESRCH rather than ENOENT.
func processGone(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH)
}

// readUID returns the real user ID from a /proc/<pid>/status file.
func readUID(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "Uid:" {
			return strconv.Atoi(fields[1])
		}
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("no Uid line in %s", path)
}

// pids returns the IDs of the processes listed in /proc.
func pids() ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("list the processes: %w", err)
	}
	var out []int
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil && pid > 0 {
			out = append(out, pid)
		}
	}
	return out, nil
}

// listProcesses reads every process in /proc. A process that exits or
// cannot be read while the list is made is left out.
func listProcesses() ([]process, error) {
	ids, err := pids()
	if err != nil {
		return nil, err
	}
	var out []process
	for _, pid := range ids {
		p, ok, err := lookupProcess(pid)
		if err != nil || !ok {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// portListeners finds the sockets listening on port in /proc/net/tcp and
// tcp6, then the processes holding them among those whose file descriptors
// can be read.
func portListeners(port int) ([]listener, error) {
	inodes := map[string]netip.Addr{}
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		if err := listeningInodes(table, port, inodes); err != nil {
			return nil, err
		}
	}
	if len(inodes) == 0 {
		return nil, nil
	}
	ids, err := pids()
	if err != nil {
		return nil, err
	}
	var out []listener
	for _, pid := range ids {
		fdDir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			inode, ok := strings.CutPrefix(target, "socket:[")
			if !ok {
				continue
			}
			if addr, ok := inodes[strings.TrimSuffix(inode, "]")]; ok {
				out = append(out, listener{PID: pid, Addr: addr})
			}
		}
	}
	return out, nil
}

// tcpListen is the state code of a listening socket in /proc/net/tcp.
const tcpListen = "0A"

// listeningInodes adds to inodes the inode and local address of each socket
// in table that listens on port.
func listeningInodes(table string, port int, inodes map[string]netip.Addr) error {
	f, err := os.Open(table)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", table, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan() // header
	for sc.Scan() {
		// sl local_address rem_address st tx:rx tr:when retrnsmt uid timeout inode
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 || fields[3] != tcpListen {
			continue
		}
		hexAddr, hexPort, ok := strings.Cut(fields[1], ":")
		if !ok {
			continue
		}
		p, err := strconv.ParseUint(hexPort, 16, 16)
		if err != nil || int(p) != port {
			continue
		}
		addr, ok := parseProcAddr(hexAddr)
		if !ok {
			continue
		}
		inodes[fields[9]] = addr
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read %s: %w", table, err)
	}
	return nil
}

// parseProcAddr reads an address from /proc/net/tcp or tcp6: the address's
// bytes printed as 32-bit words in the machine's byte order.
func parseProcAddr(s string) (netip.Addr, bool) {
	if len(s) != 8 && len(s) != 32 {
		return netip.Addr{}, false
	}
	b := make([]byte, 0, len(s)/2)
	for i := 0; i < len(s); i += 8 {
		w, err := strconv.ParseUint(s[i:i+8], 16, 32)
		if err != nil {
			return netip.Addr{}, false
		}
		b = binary.NativeEndian.AppendUint32(b, uint32(w))
	}
	addr, ok := netip.AddrFromSlice(b)
	return addr.Unmap(), ok
}
