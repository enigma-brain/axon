package skyclient

import (
	"fmt"
	"io"
)

// maxUV bounds the size of the uv executable in its archive.
const maxUV int64 = 256 << 20

// quarters returns a progress report that writes the percentage done to
// out, on the current line, each time the bytes received pass a further
// quarter of the size the server declared. Nothing is written when it
// declared none.
func quarters(out io.Writer) func(done, total int64) {
	reported := 0
	return func(done, total int64) {
		if total <= 0 {
			return
		}
		for reported < 4 && done*4 >= total*int64(reported+1) {
			reported++
			fmt.Fprintf(out, " %d%%", reported*25)
		}
	}
}
