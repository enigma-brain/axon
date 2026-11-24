---
title: Files on your laptop
description: What axon keeps in ~/.axon, and where the axon binary is.
---

`axon` keeps everything it owns on your laptop in one directory, `~/.axon`.

To see the config files and pathways stored there, and which pathway is in
use, run `axon config list`, as
[Logging in](/axon/logging-in/#seeing-what-is-stored) describes. You do not
need to read the files yourself.

## ~/.axon

| Path | Holds |
|---|---|
| `~/.axon/configs/` | The config files, one per file added, each `<id>.toml` as its source gave it, and the hand-made pathways `axon login` wrote, one file each. |
| `~/.axon/settings.toml` | Your choices: the active pathway, by its identifier. |
| `~/.axon/tools/` | What `axon` installs for its own use: the SkyPilot client with its own `uv` and Python. |
| `~/.axon/state/` | What `axon` records about running state. |

In `~/.axon/state/`, the files you may meet are:

| Path | Holds |
|---|---|
| `email` | The email of your last login. |
| `pathway.toml` | What `axon` recorded about the pathway when it connected it. |
| `configs/` | For each file in `~/.axon/configs/`, where it came from or that it is hand-made, when it was stored, and the SHA-256 of its contents. |
| `tunnel.log` | The log of the Teleport tunnel. |
| `tunnel.json` | The tunnel's process. |

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
