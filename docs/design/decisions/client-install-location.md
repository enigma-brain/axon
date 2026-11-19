# The SkyPilot client is private to axon

## Context

`axon` drives the SkyPilot client, `sky`, which is a Python package. The
client has to be at the version the API server runs. A user's laptop has, in
the general case, neither Python nor `uv`, and in other cases already has a
SkyPilot install that the user keeps for other work.

`uv` is a single binary that installs Python and Python tools without any
prior Python on the machine.

## Rationale

`axon` downloads `uv` into its own directory and has it install Python
and the SkyPilot client there. `axon` runs that client by absolute path and
ignores any `sky` on `PATH`.

- The client `axon` runs is always the one `axon` installed. Nothing the
  user does to another SkyPilot install changes it.
- `axon` never overwrites or downgrades a SkyPilot install the user owns.
- The install works on a laptop with no Python and no `uv`, with no `sudo`.
- Removing `axon`'s directory removes everything `axon` installed.

SkyPilot's own state stays in `~/.sky`, so a user who moves from a manual
install keeps their login.

The cost is that `sky` is not on `PATH` through `axon`. Users reach SkyPilot
through `axon`'s own commands, which is the entry point `axon` exists to
provide.

## Alternatives considered

- **A shared install on `PATH`, through the user's `uv`.** It overwrites
  whatever SkyPilot version the user has, and anything the user later does
  to that install changes what `axon` runs.
- **Check the installed version and print the install command.** It leaves
  the user to obtain Python or `uv` themselves, which is the step that fails
  on a clean laptop.
