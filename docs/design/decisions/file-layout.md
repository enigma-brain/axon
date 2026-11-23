# axon keeps its files in one directory, ~/.axon

## Context

`axon` keeps four kinds of file on the laptop: config files, the
user's own configuration, the tools it installs for its
own use, and a record of running state such as the tunnel's process ID and
log. The tools take several hundred megabytes; the rest is small.

A config file comes from an organisation and is replaced when the
organisation changes it. A laptop holds one or several. The user's configuration is the
user's choice of pathway, which no update from the organisation may
overwrite.

`axon`'s users set it up from written instructions and are often helped
remotely. When
something goes wrong, the instruction that resets `axon` has to be short
enough to give in one message and hard to carry out wrongly.

The tools `axon` works beside each keep a single directory in the home
folder: `~/.sky` for SkyPilot and `~/.tsh` for Teleport.

## Rationale

Everything `axon` owns is under `~/.axon`. The config files are in
`orgs/`, one each, and the user's configuration is `config.toml` at the
top, with one subdirectory each for installed tools and for state.

- A user finds all of `axon`'s files in one place, named after the tool.
- Resetting `axon` is removing one directory and logging in again with the
  organisation's source.
- Uninstalling is removing that directory and the binary.
- A config file is replaced whole on an update, exactly as its
  source gave it, with no merge, and the user's choice in its own file is
  untouched.
- It is the same convention as `~/.sky` and `~/.tsh`, which the same user
  already has.

The binary is outside this directory, in `~/.local/bin`, because that
directory is commonly on `PATH` and `~/.axon` is not.

## Alternatives considered

- **The XDG directories: `~/.config/axon`, `~/.local/share/axon` and
  `~/.local/state/axon`.** It separates configuration from large downloads
  and from disposable state, and spreads `axon` over three places that a
  user and every support message have to name.
- **One configuration file holding both the organisations' configuration
  and the user's choice.** Every update from the organisation has to merge
  into a file the user's choice lives in, and the stored file no longer
  matches its source.
