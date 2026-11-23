# On a direct pathway, does the compiled default client always log in to a newer server?

## Context

On a direct pathway, `axon` installs the SkyPilot client at a default
version compiled into the binary, logs in with it, and only then reads the
server's version and reinstalls the client to match. The sync depends on that
first login succeeding.

On a teleport pathway the question does not arise. Teleport supplies the
identity through the tunnel, so `axon` reads the identity and the server's
version without a SkyPilot login, syncs the client, and only then runs
`sky api login` with the synced client.

When the server has been upgraded past the default, the first login on a
direct pathway on a new laptop runs an older client against a newer server.

The server accepts a client whose API version is at least the server's
minimum compatible API version. At the SkyPilot release observed, the
server's minimum is 24, and the clients one and two minor versions older report API
versions 50 and 31, so the version rule admits both. Whether login then
completes on a direct pathway is not known.

The server's health endpoint answers a request with no login. When the
request carries both of the client's version headers (`X-SkyPilot-API-Version`
and `X-SkyPilot-Version`), the answer names the server's version and says the
login is required; without them the answer holds only the status. When the
request carries both headers and its API version is below the server's
minimum, the answer is an HTTP 400 with an error body and none of the health
fields. A request that carries both headers and reaches the server's version
check gets the server's version in the `X-SkyPilot-Version` response header,
the 400 included; a request without both headers gets no such header, and a
request that fails earlier gets none either. The login proxy's middleware runs
before the version check and answers HTTP 502, without the version header,
when the proxy behind it is unreachable.

Observed at the server's release, to an authenticated request on both kinds
of pathway: the answer is HTTP 200 and its body carries the server's
version, the API version, the commit and the user's identity. Through the
Teleport tunnel the body is the same with and without the client's version
headers, and the response carries the version headers only when the request
carried them. On a direct endpoint, an authenticated request with both
headers returns the body and the version headers. A direct endpoint's answer
to a request with no login, with and without the headers, is not recorded,
and neither is any login by a client one or two minor versions older than
the server.

## Options

- **Rely on the default, and raise it with every `axon` release.** The gap
  stays small as long as `axon` is released at least as often as the server
  is upgraded. Nothing more is built. A laptop that installs an old `axon`
  against a much newer server fails at login with SkyPilot's own error.
- **Read the server's version before login and install to match first.**
  There is no gap when the probe returns the server's version. The version is
  readable without a login when the request carries the client's version
  headers, so this is feasible. An answer with no version header, such as
  the proxy's 502, leaves the
  version unknown, and `axon` then installs the default version.
- **Fall back on a failed login.** When the login fails and the server's
  version is readable, `axon` installs that version and tries once more. It
  covers the gap only where the version is readable, and adds a second path
  through login.

## Recommendation

Rely on the default, and raise it with every `axon` release. It is the
simplest behavior, and the install line always fetches the latest `axon`,
whose default is recent.

## Resolution path

- Log in on a direct pathway with a client one and two minor versions
  older than the server, and record what happens.
- Request the health endpoint without a login, with and without the version
  headers, on a direct endpoint, and record the status and the fields
  returned.
