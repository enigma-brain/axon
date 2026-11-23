package orgfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockFile, under axon's state directory, is the file that serializes
// adding config files.
const lockFile = "orgs.lock"

// lock takes the exclusive lock on adding config files, waiting while
// another process or goroutine holds it. The returned function releases it.
//
// The lock is a flock on the file, not the file's existence: the kernel
// releases it when the process that holds it exits, however it exits, so a
// killed axon leaves nothing that blocks the next one.
func (s Store) lock() (unlock func(), err error) {
	state, err := s.dir.StateDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(state, lockFile)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lock %s: %w", path, err)
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
