package configfile

import "github.com/enigma-brain/axon/internal/axondir"

// lock takes the exclusive lock on changing the stored config files,
// waiting while another process or goroutine holds it. The returned function
// releases it.
func (s Store) lock() (unlock func(), err error) { return lockStore(s.dir) }

// lockStore takes the lock of an axon directory. Tests replace it to see
// whether the lock is taken.
var lockStore = axondir.Dir.Lock
