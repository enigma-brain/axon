package tunnel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// lockFile is the file whose lock serializes starting, stopping and
// recording the tunnel. It is the tunnel's own, apart from the lock on the
// config files, so that a start that waits for the tunnel does not hold up
// a change to the config files.
const lockFile = "tunnel.lock"

// lockPoll is the pause between attempts to take a lock that another
// process holds.
const lockPoll = 50 * time.Millisecond

// lock takes the exclusive lock on the tunnel in stateDir, waiting while
// another process or goroutine holds it, until ctx ends. It creates
// stateDir when it does not exist. The returned function releases the lock.
//
// The lock is a flock, not the file's existence: the kernel releases it
// when the process that holds it exits, however it exits.
func lock(ctx context.Context, stateDir string) (unlock func(), err error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("lock the Teleport tunnel: %w", err)
	}
	path := filepath.Join(stateDir, lockFile)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, fileMode)
	if err != nil {
		return nil, fmt.Errorf("lock the Teleport tunnel: %w", err)
	}
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		switch {
		case err == nil:
			return func() {
				syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				f.Close()
			}, nil
		case errors.Is(err, syscall.EINTR):
			continue
		case !errors.Is(err, syscall.EWOULDBLOCK):
			f.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(lockPoll):
		}
	}
}
