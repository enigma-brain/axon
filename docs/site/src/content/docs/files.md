---
title: Files on your laptop
description: What axon keeps in ~/.axon, and where the axon binary is.
---

`axon` keeps everything it owns on your laptop in one directory, `~/.axon`.

## ~/.axon

| Path | Holds |
|---|---|
| `~/.axon/configs/` | The config files, one per file added, each `<id>.toml` as its source gave it, and the hand-made pathways `axon login` wrote, one file each. |
| `~/.axon/settings.toml` | Your choices: the active pathway, by its identifier. |
| `~/.axon/tools/` | What `axon` installs for its own use: the SkyPilot client with its own `uv` and Python. A `tsh` that an earlier `axon` installed here is no longer used. |
| `~/.axon/state/` | What `axon` records about running state. |

In `~/.axon/state/`, the files you may meet are:

| Path | Holds |
|---|---|
| `email` | The email of your last login. |
| `pathway.toml` | What `axon` recorded about the pathway when it connected it. |
| `configs/` | For each file in `~/.axon/configs/`, where it came from or that it is hand-made, when it was stored, and the SHA-256 of its contents. |
| `tunnel.log` | The log of the Teleport tunnel. |
| `tunnel.json` | The tunnel's process. |

## Files from axon v0.3.0 and earlier

`axon` `v0.3.0` and earlier used other names for three of these:

| Old name | New name |
|---|---|
| `~/.axon/orgs/` | `~/.axon/configs/` |
| `~/.axon/state/orgs/` | `~/.axon/state/configs/` |
| `~/.axon/config.toml` | `~/.axon/settings.toml` |

The first `axon` command after an update that reads or writes `~/.axon`
renames them and prints one line that says so. Your config files, your
choice of pathway and your login are kept, and there is nothing for you to
do. `axon update` and `axon --version` rename nothing.

When an old name and its new name both exist, `axon` uses the new one,
leaves the old one where it is, and says on each command that the old one
can be deleted. Delete it once you have checked that nothing in it is
missing from the new one.

There is no way back: an `axon` of `v0.3.0` or earlier, run after the
renames, finds nothing stored, and you log in again with
`axon login --config <source>`.

## The axon binary

`axon` itself is not in `~/.axon`. The install script puts it in
`~/.local/bin`, as [Installing](/axon/installing/) describes.

## Removing everything

To remove everything `axon` keeps, delete `~/.axon`. The next login asks for a
pathway again, or takes `--config`.

Files that another program writes stay where they are:

- SkyPilot's, in `~/.sky`: see
  [sky](/axon/sky/#skypilots-files-in-sky).
- Teleport's, in `~/.tsh`: see
  [Teleport](/axon/pathways/teleport/#teleports-files-in-tsh).

A Teleport tunnel that `axon` started keeps running after `~/.axon` is
deleted. The same section of the Teleport page says what the next login does
with it.
