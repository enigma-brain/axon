# Each axon devenv command forwards to the sky command of the same name

## Context

Once a user is logged in, their work is done with SkyPilot's commands:
launching and tearing down dev environments, listing GPUs, running jobs,
managing volumes. `axon` exists so that users learn one entry point that its
maintainers own, and so that what sits behind that entry point is free to
change.

SkyPilot's commands and flags are numerous, and they change between SkyPilot
versions. The client version on a laptop follows the server's.

## Rationale

Every SkyPilot operation a user reaches goes through `axon devenv
<command>`. Each command runs the `sky` command of the same name and passes
its arguments through unchanged. The help text and the documentation of each
command are written for `axon`.

- Users learn `axon`'s commands from `axon`'s documentation, which describes
  the commands as an `axon` user meets them. SkyPilot becomes an
  implementation detail.
- The behaviour is SkyPilot's own, so there is no second implementation to
  keep in step with it. A command costs its help text, not code.
- Because the names and arguments match, existing SkyPilot knowledge and
  existing task files carry over unchanged.
- The connection check and the environment `axon` sets apply to every
  command in one place.

## Alternatives considered

- **A raw passthrough, `axon sky <args>`.** Users learn SkyPilot's surface
  from SkyPilot's documentation, so the entry point is `axon`'s in name
  only.
- **Curated commands with their own names and flags.** It is a second
  interface to design, build and keep working across SkyPilot versions, and
  users' task files and habits stop carrying over.
