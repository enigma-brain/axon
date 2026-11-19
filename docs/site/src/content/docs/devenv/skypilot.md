---
title: 'Backend: SkyPilot'
description: How axon devenv runs SkyPilot, the client axon installs for it, and SkyPilot's files.
---

The backend of [`axon devenv`](/axon/devenv/) is SkyPilot. Your organisation
runs a SkyPilot API server, and `axon` runs a SkyPilot client of its own
against it.

## How a command runs

Each `axon devenv` command runs the `sky` command of the same name, with the
same arguments:

```sh
axon devenv launch -c dev task.yaml     # runs: sky launch -c dev task.yaml
```

Flags that `axon`'s help does not list pass through to SkyPilot unchanged.
The terminal, the output and the exit code are SkyPilot's. The output is
not rewritten, so the hints SkyPilot prints name `sky` commands. Where
`axon devenv` covers a hint's command, run it as `axon devenv <command>`.

SkyPilot's `api` commands are not covered. The server address and the login
belong to `axon login`.

## Examples

Launch a cluster named `dev` from a task file, then look at it:

```sh
axon devenv launch -c dev task.yaml
axon devenv status
```

Launch a cluster with one A100 and run nothing on it, and have it stop after
an hour without jobs or SSH sessions:

```sh
axon devenv launch -c dev --gpus A100 -i 60
```

Reach the cluster with `ssh`. `axon devenv status` updates your SSH
configuration, and VS Code Remote-SSH uses the same entry:

```sh
ssh dev
```

Tear the cluster down. Its disks are deleted:

```sh
axon devenv down dev
```

## What axon sets for the command

Before it runs a command, `axon` sets two environment variables for it,
replacing any value your shell exports:

| Variable | Value |
|---|---|
| `SKYPILOT_API_SERVER_ENDPOINT` | The address of the server your pathway uses |
| `SKYPILOT_USER` | The email `axon login` recorded |

The first keeps the client on your organisation's server whatever your shell
exports. The second makes SkyPilot's volume checks run as your email and not
as your laptop username.

## What the login does for devenv

The last stage of [`axon login`](/axon/logging-in/#what-the-login-does)
connects `axon devenv`: it installs the SkyPilot client if it is missing,
checks that the server knows you, and brings the client to the server's
version. When it succeeds, it prints a line such as:

```text
devenv: connected as <email> (SkyPilot <version>)
```

### On a direct pathway

When your session with the server is still good, the login reuses it and no
browser opens. Otherwise the login is the server's own, in a browser, as the
[direct pathway's page](/axon/pathways/direct/) describes.

A stale session is cleared for you. When the login ends with `needs_auth`,
`axon` removes `~/.sky/cookies.txt`, which holds only the API server's
cookies, and logs in once more. When `SKYPILOT_API_COOKIE_FILE` names another
file, `axon` uses that file, as SkyPilot does.

### On a Teleport pathway

The login points the SkyPilot client at the tunnel's address,
`http://localhost:46581`, when the client is pointed elsewhere. No browser
opens for this: Teleport supplies your identity. The
[Teleport pathway's page](/axon/pathways/teleport/) describes the tunnel.

## The client axon installs

`axon` uses a SkyPilot client of its own, which `axon login` installs under
`~/.axon/tools/`. It ignores any `sky` on your `PATH` and does not touch it.

The first install of the client downloads a few hundred megabytes. Before
it downloads anything, `axon` says where the client goes: under
`~/.axon/tools/`, with its own `uv` and Python, so your system's Python and
packages are not changed. Each step then takes one line, which gains a dot
every few seconds while the step runs:

```text
devenv: installing the SkyPilot client <version>, with its own uv and Python, into ~/.axon/tools.
devenv: your system's Python and packages are not changed.
  uv <uv version> 25% 50% 75% 100% done
  Python 3.12 ... done
  SkyPilot <version> ........ done
devenv: installed the SkyPilot client <version>
```

`axon` prints the real path of your home directory in place of `~`. When a
step fails, its line ends with `failed`, and the error that follows carries
the installer's own message.

### Keeping the client at the server's version

When the server runs another SkyPilot version than the client, the login
moves the client to the server's, and says between which versions:

```text
devenv: SkyPilot client <old> -> <new>, the server's version
```

A move to the server's version prints the same lines as the first install,
without the `uv` line when `uv` is already in place.

## SkyPilot's files in ~/.sky

SkyPilot keeps its own settings and its login in `~/.sky`. `axon` does not
move them. Removing `~/.axon` leaves `~/.sky` in place. A login to your
organisation's server that is already in `~/.sky`, such as one made with
`sky` by hand, is reused while it is still good.

## When something fails

The messages about the client are under
[The SkyPilot client](/axon/troubleshooting/#the-skypilot-client) on the
troubleshooting page.
