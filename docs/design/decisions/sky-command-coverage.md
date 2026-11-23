# axon sky passes every command through, apart from four it refuses by name

## Context

`axon sky` runs `axon`'s own SkyPilot client against the API server that
`axon login` connected, with the connection check and the environment
`axon` sets. SkyPilot has many commands, gains new ones with new versions,
and the client on a laptop follows the server's version without an `axon`
release.

A few of SkyPilot's commands manage the very things `axon` owns: the
endpoint and the login, which `axon login` and `axon logout` set and clear,
and an API server on the laptop, which `axon` never runs.

## Rationale

`axon sky` passes every argument through unchanged, for any command,
including commands a later SkyPilot release adds, and prints SkyPilot's own
help. It refuses exactly four commands, by name: `api login`, `api logout`,
`api start` and `api stop`. The refusals of `api login` and `api logout`
name the `axon` command that does the job.

- Every SkyPilot command a user's work needs is there, with SkyPilot's own
  help and documentation, and nothing waits on an `axon` release.
- A SkyPilot upgrade needs no change to `axon`: there is no list to extend
  and no help text to rewrite.
- The four refused commands are the ones that would break or bypass the
  connection `axon` manages, so each refusal protects something specific
  and the rest of `sky api` stays available.

## Alternatives considered

- **A fixed list of commands with help written for `axon`.** Each command
  costs help text and a release, users of other commands keep a second
  SkyPilot install, and the curated help drifts from SkyPilot's.
- **Refuse every `api` command.** It also blocks `api info`, `api status`,
  `api logs` and `api cancel`, which read or manage the user's own requests
  and break nothing.
