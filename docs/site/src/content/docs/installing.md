---
title: Installing
description: How to install and update axon.
---

`axon` is one program. You install it once, with one line, and nothing on the
way needs `sudo` or a GitHub account.

## Install

Run this in a terminal:

```sh
curl -fsSL https://github.com/enigma-brain/axon/releases/latest/download/install.sh | sh
```

The line downloads the install script of the latest release and runs it. The
script:

1. Finds your platform.
2. Downloads the matching archive and the release's checksum file.
3. Checks the archive against the checksums, and stops before it writes
   anything if they differ.
4. Puts `axon` in `~/.local/bin`.

It needs `curl`, and `sha256sum` or `shasum`. Both are on macOS and on most
Linux systems.

When `~/.local/bin` is not on your `PATH`, the script ends by printing the
line to add to your shell's startup file (`~/.zshrc` or `~/.bashrc`):

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Open a new terminal afterwards, then check the install:

```sh
axon --version
```

## Platforms

`axon` is released for:

- macOS on Apple silicon
- macOS on Intel
- Linux on x86-64

On Windows, install `axon` inside WSL and run it there. On any other
platform the install script stops and says so.

## Update

Run:

```sh
axon update
```

It downloads the latest release for your platform, checks it against the
release's checksums, and replaces the `axon` you ran with it in one step. It
says which version you had and which it installed:

```text
Downloading axon 1.3.0 for darwin_arm64 from https://github.com/enigma-brain/axon
Updated axon from 1.2.3 to 1.3.0 in /Users/you/.local/bin/axon
```

When you already have the latest release, it says so and changes nothing.
Only the `axon` program changes: your pathways, your login and the clients
`axon` installed stay as they are. `axon` looks for a new release only when
you run `axon update`.

To install a named release, such as a pre-release, give its version, with or
without the leading `v`. In this example, `1.2.3-rc.1` stands for the version
you want:

```sh
axon update --version 1.2.3-rc.1
```

Without `--version`, an `axon` newer than the latest release, such as a
pre-release of the next version, is left as it is. A pre-release updates to
its final release once that is out.

Running the install line again also updates `axon` to the latest release. Use
it for an `axon` that has no `axon update`, which is a release before the
command was added, or one built from source.

When the update fails, see [Troubleshooting](/axon/troubleshooting/#updating-axon).
Every failure leaves the `axon` you had in place.

## Install a named version

A pre-release is never the latest release, so the install line does not pick
it up. To install a named version, download that release's install script and
set `AXON_VERSION`. In this example, `1.2.3-rc.1` stands for the version you
want:

```sh
curl -fsSL https://github.com/enigma-brain/axon/releases/download/v1.2.3-rc.1/install.sh | AXON_VERSION=1.2.3-rc.1 sh
```

## Install from a script

When `curl` fails, the `sh` at the end of the install line reads nothing and
exits with status 0, so a calling script cannot tell that nothing was
installed. A script that installs `axon` downloads the install script to a file
first, and runs it only when the download succeeded:

```sh
curl -fsSL -o install.sh https://github.com/enigma-brain/axon/releases/latest/download/install.sh &&
  sh install.sh
```

The install script exits with a non-zero status, and a message that begins
`axon install:`, when it cannot download or verify what it needs.

## Next

Your organization gives you a config file, or an address that serves
one. [Log in with it](/axon/logging-in/).

When the install fails, see [Troubleshooting](/axon/troubleshooting/).
