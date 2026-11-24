package interrupt

import "golang.org/x/sys/unix"

// getTermios is the request that reads a terminal's settings.
const getTermios = unix.TCGETS
