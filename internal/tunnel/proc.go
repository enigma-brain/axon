package tunnel

import (
	"net/netip"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

// process is one live process of the machine, as the process table shows it.
type process struct {
	PID  int
	UID  int
	Args []string
}

// command returns the process's arguments joined by spaces, for messages.
func (p process) command() string {
	if len(p.Args) == 0 {
		return "unknown command"
	}
	return strings.Join(p.Args, " ")
}

// listener is a process's socket listening on the tunnel's port.
type listener struct {
	PID  int
	Addr netip.Addr
}

// accepts reports whether a connection to remote reaches l: l listens on
// remote, on the unspecified IPv4 address and remote is IPv4, or on the
// unspecified IPv6 address, which also accepts IPv4 on a dual-stack socket.
func (l listener) accepts(remote netip.Addr) bool {
	remote = remote.Unmap()
	switch {
	case l.Addr == remote:
		return true
	case l.Addr == netip.IPv4Unspecified():
		return remote.Is4()
	case l.Addr == netip.IPv6Unspecified():
		return true
	}
	return false
}

// isTunnelCommand reports whether args is the tunnel's command: a first
// argument whose base name is tsh, then exactly want. The first argument is
// a path or a bare name, depending on how the process was started.
func isTunnelCommand(args, want []string) bool {
	if len(args) != len(want)+1 {
		return false
	}
	return filepath.Base(args[0]) == "tsh" && slices.Equal(args[1:], want)
}

// The functions below are provided per operating system:
//
//	lookupProcess(pid int) (process, bool, error)
//	    The live process with ID pid. The boolean is false when no such
//	    process exists or it has exited and not been reaped.
//	processStart(pid int) (uint64, bool)
//	    The start time of the process with ID pid, while it exists in any
//	    state. With the ID it identifies the process.
//	processExited(pid int) bool
//	    Whether the process with ID pid has exited and closed its files:
//	    it is gone or a zombie. A process that is still exiting has not.
//	listProcesses() ([]process, error)
//	    Every live process whose details can be read.
//	portListeners(port int) ([]listener, error)
//	    The sockets listening on TCP port and their processes, among those
//	    whose sockets can be read.

// cleanText returns s without control and format characters, so that a
// process's command line printed in a message cannot move the cursor,
// change colors or reorder the text around it.
func cleanText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}
