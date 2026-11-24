# axon keeps its files in one directory, ~/.axon

## Context

`axon` keeps four kinds of file on the laptop: config files, the
user's own settings, the tools it installs for its
own use, and a record of running state such as the tunnel's process ID and
log. The tools take several hundred megabytes; the rest is small.

A config file comes from an organization and is replaced when the
organization changes it. A laptop holds one or several. The user's settings
are the user's choice of pathway, which no update from the organization
overwrites.

`axon`'s users set it up from written instructions and are often helped
remotely. When
something goes wrong, the instruction that resets `axon` has to be short
enough to give in one message and hard to carry out wrongly.

The tools `axon` works beside each keep a single directory in the home
folder: `~/.sky` for SkyPilot and `~/.tsh` for Teleport.

## Rationale

Everything `axon` owns is under `~/.axon`. The config files are in
`configs/`, one each, and the user's settings are `settings.toml` at the
top, with one subdirectory each for installed tools and for state. Each
name says what it holds in the words `axon`'s commands and documentation
use: a config file is never called anything else, and the one file that
the user's choices live in is not mistaken for a config file.

- A user finds all of `axon`'s files in one place, named after the tool.
- Resetting `axon` is removing one directory and logging in again with the
  organization's source.
- Uninstalling is removing that directory and the binary.
- A config file is replaced whole on an update, exactly as its
  source gave it, with no merge, and the user's choice in its own file is
  untouched.
- It is the same convention as `~/.sky` and `~/.tsh`, which the same user
  already has.

The binary is outside this directory, in `~/.local/bin`, because that
directory is commonly on `PATH` and `~/.axon` is not.

`axon` `v0.3.0` and earlier named the config files directory `orgs/`, and
the settings file `config.toml`. A later `axon` renames them, and the
records directory `state/orgs/`, by itself, under the lock that guards the
stored config files, before any command reads them. A user who updates
keeps the stored files, the choice of pathway and the login, with nothing
to do and nothing to read first. Each rename is a single step, so a move
that stops part way is finished by the next command. When a new name
already exists, its content is the newer, and the old one is left for the
user to delete rather than merged into it.

## Alternatives considered

- **The XDG directories: `~/.config/axon`, `~/.local/share/axon` and
  `~/.local/state/axon`.** It separates configuration from large downloads
  and from disposable state, and spreads `axon` over three places that a
  user and every support message have to name.
- **Keeping the names `orgs/` and `config.toml`.** They name neither what
  the directory holds nor what the file is, in the words the commands
  use.
- **Asking the user to rename the old names.** An instruction that every
  user follows by hand is an instruction some users miss, and their stored
  files then look lost.
- **One configuration file holding both the organizations' configuration
  and the user's choice.** Every update from the organization has to merge
  into a file the user's choice lives in, and the stored file no longer
  matches its source.
