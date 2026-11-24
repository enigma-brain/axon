package login

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/enigma-brain/axon/internal/configfile"
	"github.com/enigma-brain/axon/internal/connected"
	"github.com/enigma-brain/axon/internal/tunnel"
)

// disconnectUnless disconnects every command group from the pathway that
// was connected, unless it is p with the same settings for machines, and
// then records p as the connected pathway. With no record, what a record
// would name is unknown, so it disconnects too; disconnecting what is not
// connected changes nothing.
//
// Disconnecting the sky group stops the tunnel by its own record and removes the
// recorded email, so axon sky asks for a login until the new pathway's
// login records one.
func (l *login) disconnectUnless(p configfile.Pathway, opts Options) error {
	same, err := connected.Is(l.dir, p)
	if err != nil {
		return err
	}
	if same {
		return nil
	}
	if err := l.disconnect(opts); err != nil {
		return err
	}
	return connected.Write(l.dir, p)
}

// disconnect disconnects every command group from the pathway that was
// connected, by axon's own records alone: it stops the recorded tunnel,
// when it runs, and removes the recorded email. With nothing recorded it
// changes nothing, and creates no directory.
func (l *login) disconnect(opts Options) error {
	stateDir := l.dir.StatePath()
	if _, err := os.Stat(stateDir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	running, err := tunnel.RecordedRunning(stateDir)
	if err != nil {
		return err
	}
	if running {
		fmt.Fprintf(opts.Out, "sky: stopping the Teleport tunnel on port %d\n", tunnel.Port)
		if err := tunnel.StopRecorded(stateDir); err != nil {
			return err
		}
	}
	return l.dir.RemoveEmail()
}

// disconnectAll disconnects the pathway that was connected and removes its
// record, so that nothing is recorded as connected. It is how a login with
// no stored config file leaves the active pathway.
func (l *login) disconnectAll(opts Options) error {
	if err := l.disconnect(opts); err != nil {
		return err
	}
	return connected.Remove(l.dir)
}
