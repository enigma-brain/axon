# Does an organization set which version of axon its members run?

## Context

Each member updates `axon` when they choose, with `axon update` or the
install line, and can install any release with `axon update --version`.
An organization has no say: its members can run different versions, older
or newer than the one it tested against its servers.

`axon` reads every organization's settings from its config files, which
`axon login` fetches again from their address. A setting there would reach
every member at their next login, with no new channel.

Nothing that `axon` does across releases depends on a member's version
matching an organization's: the SkyPilot client follows the server's
version whatever the version of `axon`.

## Options

- **No control.** Each member chooses. Simplest; an organization that needs
  a version tells its members in its own setup page.
- **A minimum version in the config file.** `axon login` stops when `axon`
  is older and says to run `axon update`. It catches members who never
  update, without stopping anyone from running something newer.
- **A pinned version in the config file.** `axon update` installs that
  version, not the latest. It keeps a whole organization on one version,
  and it needs a rule for a member whose config files name different
  versions.

## Recommendation

No control, until an organization needs one. A minimum version in the
config file is the likely form, since it fits the file every member already
fetches and does not stop a member from updating.

## Resolution path

- An organization that adopts `axon` reports a need to set or bound the
  version its members run.
- A release of `axon` stops working with an earlier config file format or
  server, so that members on older versions break.
