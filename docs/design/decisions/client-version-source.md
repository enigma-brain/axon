# The SkyPilot client follows the server's version

## Context

The SkyPilot client and the API server have to run the same version. The
server is upgraded from time to time, and every laptop has to follow.
Installing a different client version is cheap: `axon`'s own `uv` replaces
the client in about a minute and touches nothing else. The open point is
what tells `axon` which version to install.

The server reports its version to a logged-in client. Before the first
login there is no client to ask with.

## Rationale

The `axon` binary carries a default client version, used for the first
install. At each `axon login`, once the server is reachable, `axon` reads
the version the server reports and, when its client differs, reinstalls the
client at that version and says so.

- The server is the thing the client has to match, so it is the source of
  truth. No number is kept per laptop to go stale.
- A server upgrade needs no `axon` release and no action from users beyond
  their next login.
- The compiled default removes the circular dependency of the first run: a
  client exists to log in with, and the sync follows.

`axon devenv` makes the same comparison but only reports a difference. A
reinstall is a long operation, and it belongs to `axon login`.

## Alternatives considered

- **The version is compiled into `axon` and nothing else.** Every server
  upgrade needs an `axon` release and a reinstall of `axon` on every laptop.
- **The version comes from the server and nothing else.** The first run has
  no client to log in with, and no version to install before it reaches the
  server.
- **A compiled default with a per-laptop override in the configuration
  file.** A laptop stays on its override after the reason for it has gone,
  and nothing reports it.
