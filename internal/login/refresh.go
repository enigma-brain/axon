package login

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/enigma-brain/axon/internal/orgfile"
)

// refresh fetches the file that holds p again, when its recorded source is
// an address and --config did not add it in this login, and replaces the
// stored copy when the fetched file passes the checks and keeps its id. It
// returns the pathway the login goes on with:
//
//   - p as the stored file has it, when nothing is fetched, the fetch
//     fails or the fetched file is refused; each failure is one line on
//     standard error;
//   - p as the fetched file has it, which may carry new settings for
//     machines;
//   - when the fetched file no longer has p, with --pathway an error that
//     lists the stored pathways, and otherwise a pathway chosen again from
//     the stored files, with no further fetch.
func (l *login) refresh(ctx context.Context, p orgfile.Pathway, added *orgfile.File, opts Options) (orgfile.Pathway, error) {
	if added != nil && added.ID == p.File {
		return p, nil
	}
	store := orgfile.NewStore(l.dir)
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
	var previous *orgfile.File
	if prev, err := os.ReadFile(path); err == nil {
		if pf, err := orgfile.Parse(prev); err == nil {
			previous = &pf
		}
	}

	data, err := l.readOrg(ctx, src, nil)
	var (
		f       orgfile.File
		changed bool
	)
	if err == nil {
		f, changed, err = store.Refresh(p.File, data, src, opts.Replace)
	}
	var lower *orgfile.LowerRevisionError
	if errors.As(err, &lower) {
		fmt.Fprintf(opts.Out, msgLowerRevision, path, lower.Stored, lower.New)
		yes, askErr := askYesNo(opts.In, opts.Out, fmt.Sprintf(msgLowerPrompt, lower.New))
		switch {
		case errors.Is(askErr, errNoAnswer):
			fmt.Fprintf(opts.Err, msgLowerRevisionWarning, src, lower.New, lower.Stored)
			return p, nil
		case askErr != nil:
			return orgfile.Pathway{}, askErr
		case !yes:
			fmt.Fprintf(opts.Out, msgKeptRevision, lower.Stored)
			return p, nil
		}
		f, changed, err = store.Refresh(p.File, data, src, true)
	}
	if errors.Is(err, orgfile.ErrEmpty) {
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
		return orgfile.Pathway{}, err
	}
	var all []orgfile.Pathway
	for _, f := range files {
		all = append(all, f.Pathways...)
	}
	if len(all) == 0 {
		// The stored files were removed while this login ran.
		return orgfile.Pathway{}, errNoOrg
	}
	if opts.Pathway != "" {
		return orgfile.Pathway{}, fmt.Errorf("the pathway %q is no longer in its config file. The stored pathways are %s.", p.ID, idList(all))
	}
	gone := fmt.Sprintf("The pathway %q is no longer in its config file.", p.ID)
	if len(all) == 1 {
		fmt.Fprintf(opts.Out, "%s Using the pathway %q.\n", gone, all[0].ID)
		return all[0], nil
	}
	fmt.Fprintln(opts.Out, gone)
	return askPathway(all, opts.In, opts.Out)
}
