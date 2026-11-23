package skyclient

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// tickInterval is how often a running step adds a dot to its line.
var tickInterval = 5 * time.Second

// step runs one install step on a single line of progress: the label, then
// a dot every tickInterval while run works, then "done" or "failed". When
// ticks is false no dots are added, because run reports its own progress on
// the line. run receives the writer to report on.
func step(progress io.Writer, label string, ticks bool, run func(out io.Writer) error) error {
	out := &lockedWriter{w: progress}
	fmt.Fprintf(out, "sky: %s", label)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	if ticks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := time.NewTicker(tickInterval)
			defer t.Stop()
			dot := " ."
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					fmt.Fprint(out, dot)
					dot = "."
				}
			}
		}()
	}

	err := run(out)
	close(stop)
	wg.Wait()
	if err != nil {
		fmt.Fprintln(out, " failed")
	} else {
		fmt.Fprintln(out, " done")
	}
	return err
}

// lockedWriter serializes writes to w. A step's ticker and the command it
// runs write from separate goroutines, and progress need not be safe for
// concurrent use.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
