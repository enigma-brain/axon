package skyclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// lockFile, under the tools directory, is the file installs lock.
const lockFile = "skypilot.lock"

// lockPoll is how often a waiting install retries the lock.
var lockPoll = 200 * time.Millisecond

// lockTools takes the exclusive install lock on the tools directory, waiting
// for another process or goroutine that holds it and saying once that it
// waits. It returns when the lock is held or ctx is done. The returned
// function releases the lock.
//
// The lock is a flock on the file, not the file's existence. The kernel
// releases a flock when the process that holds it exits, however it exits,
// so an install that was killed leaves nothing that blocks the next one.
func lockTools(ctx context.Context, tools string, progress io.Writer) (unlock func(), err error) {
	path := filepath.Join(tools, lockFile)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	fd := int(f.Fd())
	waiting := false
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				syscall.Flock(fd, syscall.LOCK_UN)
				f.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			f.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		if !waiting {
			fmt.Fprintln(progress, "devenv: waiting for another axon to finish installing the SkyPilot client")
			waiting = true
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, fmt.Errorf("wait for another SkyPilot client install: %w", ctx.Err())
		case <-time.After(lockPoll):
		}
	}
}
