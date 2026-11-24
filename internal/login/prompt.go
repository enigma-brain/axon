package login

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/enigma-brain/axon/internal/configfile"
	"github.com/enigma-brain/axon/internal/termtext"
)

// determinePathway returns the pathway this login uses, chosen from the
// pathways of files, which are in the order of their identifiers. It does
// not store it.
//
//   - --pathway names any stored pathway, and an unknown one is refused.
//   - A file added with --config gives the pathway, unless the active pathway
//     is already in it: its only pathway, or one of its pathways the user
//     picks.
//   - Otherwise the active pathway, when a stored file has it.
//   - Otherwise the only stored pathway, or one the user picks.
func (l *login) determinePathway(files []configfile.File, added *configfile.File, opts Options) (configfile.Pathway, error) {
	var all []configfile.Pathway
	for _, f := range files {
		all = append(all, f.Pathways...)
	}

	if opts.Pathway != "" {
		if p, ok := find(all, opts.Pathway); ok {
			return p, nil
		}
		return configfile.Pathway{}, fmt.Errorf("no stored config file has the pathway %q. The stored pathways are %s.",
			opts.Pathway, idList(all))
	}

	// An active pathway that no stored file has is no active pathway.
	activeID, _, err := l.dir.Pathway()
	if err != nil {
		return configfile.Pathway{}, fmt.Errorf("%w. Run `axon login --pathway <id>` to replace it.", err)
	}
	active, hasActive := find(all, activeID)

	candidates := all
	if added != nil {
		if hasActive && active.File == added.ID {
			return active, nil
		}
		candidates = added.Pathways
	} else if hasActive {
		return active, nil
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	return askPathway(candidates, opts.In, opts.Out)
}

// find returns the pathway in ps whose identifier is id.
func find(ps []configfile.Pathway, id string) (configfile.Pathway, bool) {
	for _, p := range ps {
		if p.ID == id {
			return p, true
		}
	}
	return configfile.Pathway{}, false
}

// idList writes the identifiers of ps for a message: "a, b and c".
func idList(ps []configfile.Pathway) string {
	ids := make([]string, len(ps))
	for i, p := range ps {
		ids[i] = p.ID
	}
	if len(ids) == 1 {
		return ids[0]
	}
	return strings.Join(ids[:len(ids)-1], ", ") + " and " + ids[len(ids)-1]
}

// askPathway asks which of ps the user is on, offering each by its name
// with its description, until it gets an answer it accepts: a number from
// the list or a pathway's identifier. At the end of input it stops and
// names --pathway.
func askPathway(ps []configfile.Pathway, in io.Reader, out io.Writer) (configfile.Pathway, error) {
	fmt.Fprintln(out, "Which pathway are you on?")
	width := 0
	for _, p := range ps {
		width = max(width, len(termtext.Clean(p.Name)))
	}
	for i, p := range ps {
		fmt.Fprintf(out, "  %d) %-*s  %s\n", i+1, width, termtext.Clean(p.Name), termtext.Clean(p.Description))
	}

	for {
		fmt.Fprintf(out, "Pathway [1-%d]: ", len(ps))
		line, err := readLine(in)
		answer := strings.TrimSpace(line)
		if answer != "" {
			if p, ok := parseAnswer(ps, answer); ok {
				return p, nil
			}
			fmt.Fprintf(out, "%q is not one of the pathways.\n", answer)
		}
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(out)
			return configfile.Pathway{}, fmt.Errorf("no pathway chosen. Run `axon login --pathway <id>` with one of %s.", idList(ps))
		}
		if err != nil {
			return configfile.Pathway{}, fmt.Errorf("read the answer: %w", err)
		}
	}
}

// parseAnswer returns the pathway of ps that answer names, by its number in
// the list or by its identifier.
func parseAnswer(ps []configfile.Pathway, answer string) (configfile.Pathway, bool) {
	if n, err := strconv.Atoi(answer); err == nil {
		if n >= 1 && n <= len(ps) {
			return ps[n-1], true
		}
		return configfile.Pathway{}, false
	}
	return find(ps, strings.ToLower(answer))
}

// readLine reads one line from r, up to and including the newline. It reads a
// byte at a time, so that nothing after the line is consumed: the commands
// login runs next read the same input.
func readLine(r io.Reader) (string, error) {
	var line []byte
	b := make([]byte, 1)
	for {
		n, err := r.Read(b)
		if n > 0 {
			line = append(line, b[0])
			if b[0] == '\n' {
				return string(line), nil
			}
		}
		if err != nil {
			return string(line), err
		}
	}
}
