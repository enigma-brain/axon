package axondir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Lock takes the exclusive lock on changing the stored config files,
// waiting while another process or goroutine holds it. It creates the axon
// directory and its state directory when they do not exist. The returned
// function releases the lock.
//
// The lock is a flock on a file in the state directory, not the file's
// existence: the kernel releases it when the process that holds it exits,
// however it exits, so a killed axon leaves nothing that blocks the next
// one.
func (d Dir) Lock() (unlock func(), err error) {
	state, err := d.StateDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(state, lockFile)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, fileMode)
	if err != nil {
		return nil, fmt.Errorf("lock %s: %w", path, UnwrapPath(err))
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
