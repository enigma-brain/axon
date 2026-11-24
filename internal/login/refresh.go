package login

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/enigma-brain/axon/internal/configfile"
	"github.com/enigma-brain/axon/internal/termtext"
)

// refresh fetches the file that holds p again, when its recorded source is
// an address and --config did not add it in this login, and replaces the
// stored copy when the fetched file passes the checks and keeps its id. It
// returns the pathway the login goes on with:
//
//   - p as the stored file has it, when nothing is fetched, the fetch
//     fails or the fetched file is refused, each failure one line on
//     standard error, or when the user declines a fetched file that
//     changes where p goes;
//   - p as the fetched file has it, which may carry new settings for
//     machines;
//   - when the fetched file no longer has p, with --pathway an error that
//     lists the stored pathways, and otherwise a pathway chosen again from
//     the stored files, with no further fetch.
func (l *login) refresh(ctx context.Context, p configfile.Pathway, added *configfile.File, opts Options) (configfile.Pathway, error) {
	if added != nil && added.ID == p.File {
		return p, nil
	}
	store := configfile.NewStore(l.dir)
	path := store.Path(p.File)
	src, ok, err := store.Source(p.File)
	if err != nil {
		fmt.Fprintf(opts.Err, "axon: could not update %s: %v. Using the stored file.\n", path, err)
		return p, nil
	}
	if !ok || !src.IsAddress() {
		return p, nil
	}

	// The stored copy, as it was, for the line that reports the update.
	var previous *configfile.File
	if prev, err := os.ReadFile(path); err == nil {
		if pf, err := configfile.Parse(prev); err == nil {
			previous = &pf
		}
	}

	data, err := l.readConfig(ctx, src, nil)
	// A fetch cut short by the login's own cancellation is not a failure
	// to fall back from: the login stops.
	if err != nil && ctx.Err() != nil {
		return configfile.Pathway{}, ctx.Err()
	}
	var (
		f       configfile.File
		changed bool
	)
	if err == nil {
		switch keep, askErr := l.confirmDestination(ctx, p, data, src, path, opts); {
		case askErr != nil:
			return configfile.Pathway{}, askErr
		case keep:
			return p, nil
		}
		f, changed, err = store.Refresh(p.File, data, src, opts.Replace)
	}
	var lower *configfile.LowerRevisionError
	if errors.As(err, &lower) {
		fmt.Fprintf(opts.Out, msgLowerRevision, path, lower.Stored, lower.New)
		yes, askErr := askYesNo(ctx, opts.In, opts.Out, fmt.Sprintf(msgLowerPrompt, lower.New))
		switch {
		case errors.Is(askErr, errNoAnswer):
			fmt.Fprintf(opts.Err, msgLowerRevisionWarning, src, lower.New, lower.Stored)
			return p, nil
		case askErr != nil:
			return configfile.Pathway{}, askErr
		case !yes:
			fmt.Fprintf(opts.Out, msgKeptRevision, lower.Stored)
			return p, nil
		}
		f, changed, err = store.Refresh(p.File, data, src, true)
	}
	if errors.Is(err, configfile.ErrEmpty) {
		fmt.Fprintf(opts.Err, msgRefreshEmpty, path, src)
		return p, nil
	}
	if err != nil {
		fmt.Fprintf(opts.Err, "axon: could not update %s from %s: %v. Using the stored file.\n", path, src, err)
		return p, nil
	}
	if changed {
		if previous != nil && !previous.HasRevision && !f.HasRevision {
			previous = nil
		}
		if previous == nil {
			fmt.Fprintf(opts.Out, msgUpdatedFile, path)
		} else {
			reportStored(opts.Out, path, f, previous)
		}
	}
	if fresh, ok := find(f.Pathways, p.ID); ok {
		return fresh, nil
	}

	// The pathway is left. The other stored files were reported when
	// they were first read, so they are not reported again.
	files, err := l.storedFiles(io.Discard)
	if err != nil {
		return configfile.Pathway{}, err
	}
	var all []configfile.Pathway
	for _, f := range files {
		all = append(all, f.Pathways...)
	}
	if len(all) == 0 {
		// The stored files were removed while this login ran.
		return configfile.Pathway{}, errNoConfig
	}
	if opts.Pathway != "" {
		return configfile.Pathway{}, fmt.Errorf("the pathway %q is no longer in its config file. The stored pathways are %s.", p.ID, idList(all))
	}
	gone := fmt.Sprintf("The pathway %q is no longer in its config file.", p.ID)
	if len(all) == 1 {
		fmt.Fprintf(opts.Out, "%s Using the pathway %q.\n", gone, all[0].ID)
		return all[0], nil
	}
	fmt.Fprintln(opts.Out, gone)
	return askPathway(ctx, all, opts.In, opts.Out)
}

// confirmDestination asks whether to take the fetched file data when it
// changes where the pathway in use, p, goes: its kind, its API server's
// address, its Teleport proxy or its application, compared as clashes
// compare servers, or when it no longer holds p. It prints the old and the
// new values first. The default answer is no; --replace answers yes. keep
// reports that the stored copy is kept, and the login goes on with it:
// the answer was no, or standard input is not a terminal, which a warning
// line says. Input that is not a terminal is never read for this answer,
// so a script that pipes answers in cannot move the pathway without
// --replace. A file that fails the checks, or whose id is not p's file's,
// is not asked about: storing it refuses it.
func (l *login) confirmDestination(ctx context.Context, p configfile.Pathway, data []byte, src configfile.Source, path string, opts Options) (keep bool, err error) {
	fetched, err := configfile.Parse(data)
	if err != nil || fetched.ID != p.File {
		return false, nil
	}
	fresh, ok := find(fetched.Pathways, p.ID)
	if ok && configfile.SameServer(p, fresh) {
		return false, nil
	}
	fmt.Fprintf(opts.Out, msgDestinationChanged, src, p.ID)
	fmt.Fprintf(opts.Out, msgDestinationNow, termtext.Clean(p.Destination()))
	if ok {
		fmt.Fprintf(opts.Out, msgDestinationNew, termtext.Clean(fresh.Destination()))
	} else {
		fmt.Fprint(opts.Out, msgDestinationGone)
	}
	if opts.Replace {
		return false, nil
	}
	if l.isTerminal == nil || !l.isTerminal(opts.In) {
		fmt.Fprintf(opts.Err, msgDestinationWarning, src, p.ID)
		return true, nil
	}
	yes, err := askYesNo(ctx, opts.In, opts.Out, msgDestinationPrompt)
	switch {
	case errors.Is(err, errNoAnswer):
		fmt.Fprintf(opts.Err, msgDestinationWarning, src, p.ID)
		return true, nil
	case err != nil:
		return false, err
	case !yes:
		fmt.Fprintf(opts.Out, msgDestinationKept, path)
		return true, nil
	}
	return false, nil
}

// reportDestination prints the old and the new values of where the pathway
// in use, active, goes when an update of the file that holds it, from
// previous to f, changes them or drops the pathway. It asks nothing.
func reportDestination(out io.Writer, previous, f configfile.File, active string) {
	old, ok := find(previous.Pathways, active)
	if !ok {
		return
	}
	fresh, ok := find(f.Pathways, active)
	if ok && configfile.SameServer(old, fresh) {
		return
	}
	fmt.Fprintf(out, msgDestinationChangedAdded, active)
	fmt.Fprintf(out, msgDestinationNow, termtext.Clean(old.Destination()))
	if ok {
		fmt.Fprintf(out, msgDestinationNew, termtext.Clean(fresh.Destination()))
	} else {
		fmt.Fprint(out, msgDestinationGone)
	}
}
