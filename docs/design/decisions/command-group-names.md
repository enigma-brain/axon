# A command group that passes a tool through is named for the tool

## Context

`axon` reaches each part of a platform through a command group. A group
either passes a tool's own commands straight through, with the tool's
names, flags, help and output, or it hides the tool behind commands of its
own that are designed for the platform.

The group that reaches the SkyPilot API server passes SkyPilot's `sky`
commands through unchanged. Its users read SkyPilot's help, SkyPilot's
hints and SkyPilot's documentation, all of which name `sky` commands. In
release v0.1.0 this group was named `devenv`, for the dev environments it
launches, although it exposed SkyPilot's whole vocabulary under that name.

## Rationale

A group that passes straight through to a tool is named for the tool. A
group that hides the tool behind its own commands is named for the part of
the platform it reaches. The SkyPilot group is a pass-through, so it is
`axon sky`, and the name `devenv` is dropped, with no alias.

- The name tells the user what they are talking to. Under `axon sky`, every
  `sky` command in SkyPilot's help, hints and documentation is the same
  command with `axon` in front of it, with nothing to translate.
- A name for a part of the platform promises that what sits behind it is
  free to change without the user learning new commands. A pass-through
  cannot keep that promise: its commands are the tool's, and they change
  when the tool does.
- A name for a part of the platform stays available for a group that does
  hide its tool, with commands designed for that part.

## Alternatives considered

- **Keep `devenv` over SkyPilot's commands.** The name suggests an
  abstraction that is not there: users meet SkyPilot's vocabulary and help
  under a name that hides nothing, and have to translate every `sky` hint.
- **Keep `devenv` as an alias of `axon sky`.** Two names for one group
  keep the misleading one in scripts and in habits, and hold the name back
  from a group that would earn it.
