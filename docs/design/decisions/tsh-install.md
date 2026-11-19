# axon installs tsh when it is missing

## Context

A teleport pathway reaches the API server through Teleport, and every
Teleport step runs through Teleport's client, `tsh`. `axon login` runs those
steps so that the user runs no `tsh` command by hand. A user new to such a
pathway has no `tsh` installed.

`tsh` has to be compatible with the Teleport proxy it logs in to. The proxy
reports its own version publicly. Some users already have a `tsh` that their
organisation installs and updates.

## Rationale

When `tsh` is not on `PATH` and `axon` has not installed one, `axon login`
installs it at the version the proxy reports.

- The promise that the user handles no Teleport step by hand covers the
  install as well as the login.
- Taking the version from the proxy keeps `tsh` compatible with it, with no
  pinned number to maintain in `axon`.
- An existing `tsh` is used as it is and never replaced, so `axon` does not
  fight an install that someone else manages. When its version is outside
  the range the proxy supports, `axon` says so and names both versions; the
  owner of that install updates it.

When the install fails or the user declines it, `axon` prints Teleport's
install link and stops, which is the behaviour of the alternative below and
loses nothing.

## Alternatives considered

- **Print the install link and stop.** The user installs a tool by hand
  before running the command whose purpose is that they do not have to.
- **Always install a private `tsh`, whatever is on `PATH`.** It duplicates a
  working install and puts two versions of `tsh`, with one shared profile
  directory, on the same laptop.
