# axon logout ends the Teleport session as well as the SkyPilot login

## Context

`axon login` sets up three things on the laptop: the connection to the
pathway, which on a teleport pathway includes a tunnel; the SkyPilot
client's login; and, on a teleport pathway, the Teleport session, a
certificate that `tsh` keeps in `~/.tsh`. A user logs out to stop acting as
themselves from the laptop, such as before handing it over or at the end of
work on a shared machine.

The Teleport session is shared: the user's own `tsh` commands use the same
certificate for Teleport's other services.

The SkyPilot client keeps its login to the API server as a session cookie
in its cookie file. `sky api logout` removes the API server from the
client's config file and leaves the cookie file as it was, so the cookie
stays valid and the next `axon login` connects without the browser login.
The server offers the client no call that ends the session.

## Rationale

`axon logout` ends every part of the login: it logs the SkyPilot client
out, removes the connected API server's cookies from SkyPilot's cookie
file itself, stops the tunnel, ends the Teleport session for the connected
pathway's proxy with `tsh logout`, and removes `axon`'s records of the
connection. It keeps the config files and the active pathway, so that
`axon login` with no flags logs in again.

- A logout that leaves a valid Teleport certificate behind leaves the user
  able to reach the API server through a new tunnel without logging in,
  which is what logging out is meant to prevent.
- A logout that leaves the API server's session cookie behind leaves the
  laptop logged in to the API server, for the same reason. `axon` removes
  the cookies itself because `sky api logout` does not, and removing them
  from the laptop is the one step the client can take. Only the connected
  server's cookies go: the file is shared with any other server the
  client has logged in to.
- One command undoes one command: what `axon login` set up, `axon logout`
  takes down, and the user does not need to know which parts Teleport
  holds.
- The proxy and the API server's endpoint come from `axon`'s record of
  the connected pathway, so the logout works when the config file that named the pathway is gone.

## Alternatives considered

- **Rely on `sky api logout` alone.** It leaves the session cookie in the
  cookie file, so the next login needs no browser and the laptop stays
  logged in.
- **Delete the whole cookie file.** It also logs the client out of every
  other server whose cookies the file holds.
- **Leave the Teleport session open and print the `tsh logout` command.**
  The laptop stays able to reach the API server through Teleport until the
  user runs a second command that `axon` exists to spare them.
