package sky

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/orgfile"
	"github.com/enigma-brain/axon/internal/tunnel"
)

// ErrNoLogin reports that axon sky has nothing to connect to: no
// config file, no active pathway, an active pathway no stored file
// has, or no recorded login.
var ErrNoLogin = errors.New("Run `axon login` first.")

// Endpoint returns the URL of the SkyPilot API server that the client uses
// on p: the pathway's own endpoint on a direct pathway, and the tunnel's
// local address on a teleport pathway.
func Endpoint(p orgfile.Pathway) string {
	if p.Kind == orgfile.KindTeleport {
		return "http://localhost:" + strconv.Itoa(tunnel.Port)
	}
	return p.Sky.Endpoint
}

// Host returns the host of endpoint, as a message names it.
func Host(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		return u.Host
	}
	return endpoint
}

// Unreachable returns the message for a host that does not answer, followed
// by the pathway's hint when it has one.
func Unreachable(p orgfile.Pathway, endpoint string) string {
	msg := fmt.Sprintf("Can't reach %s.", Host(endpoint))
	if p.UnreachableHint != "" {
		msg += " " + p.UnreachableHint
	}
	return msg
}

// activePathway returns the active pathway from the stored organisation
// files. A stored file that fails the checks is left out without a word:
// only axon login reports it. It returns ErrNoLogin when there is no active
// pathway or no stored file has it.
func activePathway(dir axondir.Dir) (orgfile.Pathway, error) {
	id, ok, err := dir.Pathway()
	if err != nil {
		return orgfile.Pathway{}, err
	}
	if !ok {
		return orgfile.Pathway{}, ErrNoLogin
	}
	files, _, err := orgfile.NewStore(dir).List()
	if err != nil {
		return orgfile.Pathway{}, err
	}
	for _, f := range files {
		for _, p := range f.Pathways {
			if p.ID == id {
				return p, nil
			}
		}
	}
	return orgfile.Pathway{}, ErrNoLogin
}
