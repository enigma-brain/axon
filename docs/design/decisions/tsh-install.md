# tsh is a prerequisite of a teleport pathway, and axon does not install it

## Context

A teleport pathway reaches the API server through Teleport, and every
Teleport step runs through Teleport's client, `tsh`. `axon login` runs those
steps so that the user runs no `tsh` command by hand.

An organization that gives its members a teleport pathway already runs
Teleport, and its members use `tsh` for Teleport's other services, so they
have it installed, by the organization's own means and kept up to date with
its proxy. `tsh` has to be compatible with the Teleport proxy it logs in to,
and Teleport publishes its own install instructions for every platform.

## Rationale

`axon` uses the `tsh` on `PATH` and never installs one. With no `tsh` on
`PATH`, `axon login` stops before any login step, says that the pathway
needs `tsh` and that none was found, and gives the address of Teleport's
install instructions.

- `axon` downloads nothing from Teleport, so it carries no pinned version,
  no checksums and no handling of each platform's archive, and it never
  competes with the install the organization manages.
- One `tsh` on the laptop means one version and one profile directory,
  `~/.tsh`, used by `axon` and by the member's own `tsh` commands alike.
- The member meets the requirement once, with Teleport's own instructions,
  which cover every platform `axon` runs on.

## Alternatives considered

- **Install `tsh` when it is missing.** It downloads hundreds of megabytes
  of Teleport's archive and keeps a second `tsh` beside the one the
  organization manages, for a member who almost always has `tsh` already.
- **Always install a private `tsh`, whatever is on `PATH`.** It duplicates a
  working install and puts two versions of `tsh`, with one shared profile
  directory, on the same laptop.
