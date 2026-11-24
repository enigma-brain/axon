// Package configlist prints the config files stored on the laptop, their
// pathways, and the pathway in use.
//
// It only reads: it takes no lock, creates no directory, contacts no server
// and changes no file.
package configlist

import (
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/orgfile"
)

// timeLayout is how a stored file's time is printed, in local time.
const timeLayout = "2006-01-02 15:04 MST"

// Options are the inputs of Run.
type Options struct {
	// Dir is the axon directory to read.
	Dir axondir.Dir
	// Out receives the listing.
	Out io.Writer
	// Location is the time zone stored times are printed in. Nil means
	// local time.
	Location *time.Location
}

// UnreadableError reports that some of what is stored could not be read.
// The listing has already named each one and its reason.
type UnreadableError struct {
	// Count is how many stored files could not be read.
	Count int
}

func (e *UnreadableError) Error() string {
	if e.Count == 1 {
		return "1 stored file could not be read; the listing names it and the reason"
	}
	return fmt.Sprintf("%d stored files could not be read; the listing names each and the reason", e.Count)
}

// Run prints every stored config file with its pathways, and which pathway
// is in use. It returns an *UnreadableError, after printing everything it
// could read, when a stored file, its record or the file naming the
// pathway in use cannot be read.
func Run(opts Options) error {
	loc := opts.Location
	if loc == nil {
		loc = time.Local
	}
	w := &writer{w: opts.Out}
	store := orgfile.NewStore(opts.Dir)

	files, problems, err := store.List()
	if err != nil {
		return err
	}
	unreadable := len(problems)

	active, hasActive, activeErr := opts.Dir.Pathway()
	if activeErr != nil {
		unreadable++
	}

	if len(files) == 0 && len(problems) == 0 {
		w.line("No config file or pathway is stored. Run `axon login` to add a pathway.")
		if activeErr != nil {
			w.line("")
			w.line(clean(activeErr.Error()))
		}
		return w.result(unreadable)
	}

	var inUse *orgfile.Pathway
	if hasActive {
		for i := range files {
			for j := range files[i].Pathways {
				if files[i].Pathways[j].ID == active {
					inUse = &files[i].Pathways[j]
				}
			}
		}
	}
	switch {
	case activeErr != nil:
		w.line("The pathway in use is not known: " + clean(activeErr.Error()))
	case inUse != nil:
		w.line(fmt.Sprintf("The pathway in use is %s (%s), from %s.", clean(inUse.Name), inUse.ID, inUse.File))
	case hasActive:
		w.line(fmt.Sprintf("No pathway is in use: the chosen pathway %q is in no stored config file. `axon login` chooses one.", clean(active)))
	default:
		w.line("No pathway is in use. `axon login` chooses one.")
	}

	for _, f := range files {
		w.line("")
		w.line(f.ID)
		rec, ok, err := store.Record(f.ID)
		switch {
		case err != nil:
			unreadable++
			w.wrapped("  the record cannot be read: "+clean(err.Error()), "  ")
		case !ok:
			w.line("  source not recorded")
		case rec.HandMade:
			w.line("  made by hand at `axon login`")
		default:
			w.line("  from " + clean(rec.Source.String()))
		}
		facts := "no revision"
		if f.HasRevision {
			facts = fmt.Sprintf("revision %d", f.Revision)
		}
		if err == nil && ok && !rec.StoredAt.IsZero() {
			facts += ", stored " + rec.StoredAt.In(loc).Format(timeLayout)
		}
		w.line("  " + facts)
		w.line("")
		for _, p := range f.Pathways {
			mark := "  "
			if inUse != nil && p.File == inUse.File && p.ID == inUse.ID {
				mark = "* "
			}
			w.wrapped("  "+mark+clean(p.Name)+"  --pathway "+p.ID, "  ")
			w.wrapped("      "+server(p), "  ")
		}
	}

	for _, p := range problems {
		w.line("")
		w.line(clean(p.Path))
		w.wrapped("  cannot be read: "+clean(p.Err.Error()), "  ")
	}

	return w.result(unreadable)
}

// server describes where a pathway reaches its server, after its kind.
func server(p orgfile.Pathway) string {
	switch p.Kind {
	case orgfile.KindTeleport:
		proxy := ""
		if p.Teleport != nil {
			proxy = p.Teleport.Proxy
		}
		return fmt.Sprintf("teleport, proxy %s, application %s", clean(proxy), clean(p.Sky.App))
	default:
		return fmt.Sprintf("%s, %s", p.Kind, clean(p.Sky.Endpoint))
	}
}

// clean returns s without control characters and Unicode format
// characters, such as the bidirectional overrides, so that text from a
// config file cannot move the cursor, change colors or reorder what the
// terminal shows.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}

// writer writes lines and keeps the first error.
type writer struct {
	w   io.Writer
	err error
}

// result returns the error Run ends with: the first write error, else an
// *UnreadableError when unreadable is not zero, else nil.
func (w *writer) result(unreadable int) error {
	if w.err != nil {
		return w.err
	}
	if unreadable > 0 {
		return &UnreadableError{Count: unreadable}
	}
	return nil
}

// width is the width the listing is wrapped to.
const width = 80

// line writes s, wrapped at spaces to width. Wrapped lines keep the
// indentation s starts with. A word longer than the width, such as a long
// path, is not broken.
func (w *writer) line(s string) {
	w.wrapped(s, "")
}

// wrapped writes s as line does, with extra indentation after the
// indentation s starts with on every line after the first.
func (w *writer) wrapped(s, extra string) {
	if w.err != nil {
		return
	}
	body := strings.TrimLeft(s, " ")
	indent := s[:len(s)-len(body)]
	cur := indent
	empty := true
	for _, word := range strings.Split(body, " ") {
		n := len([]rune(word))
		if !empty && n > 0 && len([]rune(cur))+1+n > width && len([]rune(indent+extra))+n <= width {
			if _, w.err = fmt.Fprintln(w.w, cur); w.err != nil {
				return
			}
			cur, empty = indent+extra, true
		}
		if !empty {
			cur += " "
		}
		cur += word
		empty = false
	}
	_, w.err = fmt.Fprintln(w.w, cur)
}
