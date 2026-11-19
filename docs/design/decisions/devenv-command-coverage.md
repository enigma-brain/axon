# axon devenv accepts a fixed list of commands

## Context

`axon devenv` forwards each command to the SkyPilot command of the same
name, with help and documentation written for `axon`. SkyPilot has more
commands than a platform's users need, and gains new ones with new
versions. The client version on a laptop follows the server's, so the set of
commands the client offers changes without an `axon` release.

Forwarding a command costs its help text and documentation, not code.

## Rationale

`axon devenv` accepts the commands on a list held in `axon` and refuses
every other command with a message that it is not covered. The list holds
the commands users of a platform rely on: the dev environment lifecycle,
GPU listing, managed jobs and volume listing.

- Every command a user reaches has documentation written for `axon`. There
  is no part of the surface that shows SkyPilot's help.
- A SkyPilot upgrade does not expose a command that no one has examined,
  documented or agreed to support.
- Managed jobs are on the list beside dev environments, so a user who runs
  jobs does so through `axon` and keeps no second SkyPilot install for them.
- Adding a command is a deliberate, small change: its entry and its help.

SkyPilot's `api` commands are never on the list. They set the endpoint, the
login and the local API server, which `axon login` owns.

## Alternatives considered

- **Forward any command.** Commands without `axon` help fall back to
  SkyPilot's, and each SkyPilot upgrade widens the surface unseen.
- **Dev environment commands only.** A user who runs jobs keeps a separate
  SkyPilot install and login, which is the setup `axon` replaces.
