package login

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/enigma-brain/axon/internal/orgfile"
)

// errNoAnswer reports a question that got no answer: its input ended, as
// it does when there is no terminal.
var errNoAnswer = errors.New("no answer")

// askYesNo asks question and reads answers from in until one is yes or
// no. Enter alone is no. At the end of input it returns errNoAnswer.
func askYesNo(in io.Reader, out io.Writer, question string) (bool, error) {
	for {
		fmt.Fprint(out, question)
		line, err := readLine(in)
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		case "":
			if err == nil {
				return false, nil
			}
		default:
			fmt.Fprint(out, msgAnswerYesNo)
			if err == nil {
				continue
			}
		}
		fmt.Fprintln(out)
		if errors.Is(err, io.EOF) {
			return false, errNoAnswer
		}
		return false, fmt.Errorf("read the answer: %w", err)
	}
}

// clashOutcome is what the questions about a change's clashes decided.
type clashOutcome struct {
	// remove holds the stored files to remove, as the question showed them.
	remove []orgfile.File
	// declined is the first name clash whose replacement was declined, or
	// nil when every name clash is to be replaced.
	declined *orgfile.Clash
}

// errClashNoTerminal is the stop on a clash with no answer to read.
type errClashNoTerminal struct{ clash orgfile.Clash }

// resolveClashes asks, once per stored file, whether to replace each stored
// file a pathway of change clashes with. With replace, every answer is yes
// and nothing is asked. Removal is not done here: the caller passes the
// outcome's remove to orgfile.Store.Put.
func resolveClashes(store orgfile.Store, change orgfile.Change, replace bool, in io.Reader, out io.Writer) (clashOutcome, error) {
	var outcome clashOutcome
	several := len(change.File.Pathways) > 1
	for _, group := range groupClashes(change.Clashes) {
		c := group[0]
		for _, g := range group {
			if g.SameName {
				c = g
				break
			}
		}
		yes := replace
		if !replace {
			fmt.Fprint(out, clashQuestion(store, c, several))
			var err error
			yes, err = askYesNo(in, out, msgReplacePrompt)
			if errors.Is(err, errNoAnswer) {
				return clashOutcome{}, &errClashNoTerminal{clash: c}
			}
			if err != nil {
				return clashOutcome{}, err
			}
		}
		if yes {
			outcome.remove = append(outcome.remove, c.Stored)
			continue
		}
		if c.SameName {
			if outcome.declined == nil {
				outcome.declined = &c
			}
			continue
		}
		fmt.Fprint(out, msgKeepingBoth)
	}
	return outcome, nil
}

func (e *errClashNoTerminal) Error() string { return "a clash with no terminal to ask on" }

// groupClashes groups clashes by stored file, keeping their order.
func groupClashes(clashes []orgfile.Clash) [][]orgfile.Clash {
	var groups [][]orgfile.Clash
	index := map[string]int{}
	for _, c := range clashes {
		i, ok := index[c.Stored.ID]
		if !ok {
			i = len(groups)
			index[c.Stored.ID] = i
			groups = append(groups, nil)
		}
		groups[i] = append(groups[i], c)
	}
	return groups
}

// clashQuestion is the text before the question about one clash: what
// clashes and what replacing it removes.
func clashQuestion(store orgfile.Store, c orgfile.Clash, several bool) string {
	var reason string
	switch {
	case c.SameName:
		reason = msgClashName
	case c.Pathway.Kind == orgfile.KindTeleport:
		reason = fmt.Sprintf(msgClashTeleport, c.Pathway.Teleport.Proxy, c.Pathway.Sky.App)
	default:
		reason = fmt.Sprintf(msgClashDirect, c.Pathway.Sky.Endpoint)
	}
	s := fmt.Sprintf(msgClashStored, c.Pathway.Name, reason)
	if several {
		s += fmt.Sprintf(msgClashAddingNamed, c.Added.Name)
	} else {
		s += msgClashAdding
	}
	if c.StoredHandMade {
		return s + msgClashRemovesHandMade
	}
	names := make([]string, len(c.Stored.Pathways))
	for i, p := range c.Stored.Pathways {
		names[i] = p.Name
	}
	return s + fmt.Sprintf(msgClashRemovesFile, store.Path(c.Stored.ID), nameList(names))
}

// nameList writes names for a message: "a, b and c".
func nameList(names []string) string {
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// reportRemoved writes one line for each stored file a change removed.
func reportRemoved(store orgfile.Store, change orgfile.Change, removed []orgfile.File, out io.Writer) {
	gone := map[string]bool{}
	for _, f := range removed {
		gone[f.ID] = true
	}
	for _, group := range groupClashes(change.Clashes) {
		c := group[0]
		if !gone[c.Stored.ID] || c.Stored.ID == change.File.ID {
			continue
		}
		if c.StoredHandMade {
			fmt.Fprintf(out, msgRemovedPathway, c.Pathway.Name)
		} else {
			fmt.Fprintf(out, msgRemovedFile, store.Path(c.Stored.ID))
		}
	}
}

// leaveRemoved leaves the active pathway, as a switch of pathway does,
// when it is in a stored file about to be removed.
func (l *login) leaveRemoved(removed []orgfile.File, opts Options) error {
	if len(removed) == 0 {
		return nil
	}
	active, ok, err := l.dir.Pathway()
	if err != nil {
		return fmt.Errorf("%w. Run `axon login --pathway <id>` to replace it.", err)
	}
	if !ok {
		return nil
	}
	for _, f := range removed {
		for _, p := range f.Pathways {
			if p.ID == active {
				if err := l.disconnectAll(opts); err != nil {
					return fmt.Errorf("disconnect the previous pathway: %w", err)
				}
				return nil
			}
		}
	}
	return nil
}

// revisionText is "revision N", or "no revision" for a file without one.
func revisionText(f orgfile.File) string {
	if !f.HasRevision {
		return msgNoRevision
	}
	return fmt.Sprintf(msgRevisionN, f.Revision)
}

// reportStored writes the line for a stored config file: stored, or
// updated from previous when previous is not nil.
func reportStored(out io.Writer, path string, f orgfile.File, previous *orgfile.File) {
	switch {
	case previous == nil && f.HasRevision:
		fmt.Fprintf(out, msgStoredFileRevision, path, f.Revision)
	case previous == nil:
		fmt.Fprintf(out, msgStoredFile, path)
	case f.HasRevision || previous.HasRevision:
		fmt.Fprintf(out, msgUpdatedFileRevision, path, revisionText(*previous), revisionText(f))
	default:
		fmt.Fprintf(out, msgUpdatedFile, path)
	}
}
