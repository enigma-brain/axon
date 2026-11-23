---
title: sky
description: Run SkyPilot's commands against your organization's API server with axon sky.
---

`axon sky` runs SkyPilot's `sky` commands against your organization's
SkyPilot API server: dev environments, GPUs, managed jobs and volumes.
[Log in](/axon/logging-in/) first.

## How a command runs

`axon sky` passes every argument to a SkyPilot client of its own, unchanged:

```sh
axon sky launch -c dev task.yaml     # runs: sky launch -c dev task.yaml
```

Every `sky` command works this way, including commands that a newer SkyPilot
adds. The terminal, the output and the exit code are SkyPilot's, and the
output is not rewritten.

**Wherever SkyPilot's help, hints or documentation say `sky`, type
`axon sky`.** SkyPilot's
[command reference](https://docs.skypilot.ai/en/latest/reference/cli.html)
describes every command and flag.

## Help

`axon sky --help` lists SkyPilot's commands, and `axon sky <command> --help`
prints a command's own help. Both are SkyPilot's, and need no login, only the
client that `axon login` installs:

```sh
axon sky launch --help
```

## The commands axon refuses

Four commands manage what `axon` itself sets up, so `axon` refuses them:

```text
axon: `axon sky api login` is not available. Run `axon login` to log in.
axon: `axon sky api logout` is not available. Run `axon logout` to log out.
axon: `axon sky api start` is not available: axon connects to your organization's API server and does not start one on this computer.
axon: `axon sky api stop` is not available: axon connects to your organization's API server and does not start one on this computer.
```

Every other `axon sky api` command, such as `axon sky api info`, runs.

## Examples

Launch a cluster named `dev` from a task file, then look at it:

```sh
axon sky launch -c dev task.yaml
axon sky status
```

Launch a cluster with one A100 and run nothing on it, and have it stop after
an hour without jobs or SSH sessions:

```sh
axon sky launch -c dev --gpus A100 -i 60
```

Reach the cluster with `ssh`. `axon sky status` updates your SSH
configuration, and VS Code Remote-SSH uses the same entry:

```sh
ssh dev
```

Tear the cluster down. Its disks are deleted:

```sh
axon sky down dev
```

## What axon checks first

Before each command, `axon sky` makes one request to the API server, to
check the connection. It never opens a browser and never installs anything.
When the check fails, it stops and says what to do, and the command does not
run. The messages, and what each needs, are on the
[troubleshooting page](/axon/troubleshooting/#running-axon-sky).

When the Teleport tunnel has stopped while your Teleport certificate is still
valid, the check restarts it and says so:

```text
sky: restarting the Teleport tunnel
```

## What axon sets for the command

Before it runs a command, `axon` sets two environment variables for it,
replacing any value your shell exports:

| Variable | Value |
|---|---|
| `SKYPILOT_API_SERVER_ENDPOINT` | The address of the server your pathway uses |
| `SKYPILOT_USER` | The email `axon login` recorded |

The first keeps the client on your organization's server whatever your shell
exports. The second makes SkyPilot's volume checks run as your email and not
as your laptop username.

## What the login does for sky

The last stage of [`axon login`](/axon/logging-in/#what-the-login-does)
connects `axon sky`: it installs the SkyPilot client if it is missing,
checks that the server knows you, and brings the client to the server's
version. When it succeeds, it prints a line such as:

```text
sky: connected as <email> (SkyPilot <version>)
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
The client is installed from pre-built packages only, so your laptop needs no
compiler.

The first install of the client downloads a few hundred megabytes. Before
it downloads anything, `axon` says where the client goes: under
`~/.axon/tools/`, with its own `uv` and Python, so your system's Python and
packages are not changed. Each step then takes one line, which gains a dot
every few seconds while the step runs:

```text
sky: installing the SkyPilot client <version>, with its own uv and Python, into ~/.axon/tools.
sky: your system's Python and packages are not changed.
sky: uv <uv version> 25% 50% 75% 100% done
sky: Python 3.12 ... done
sky: SkyPilot <version> ........ done
sky: installed the SkyPilot client <version>
```

`axon` prints the real path of your home directory in place of `~`. When a
step fails, its line ends with `failed`, and the error that follows carries
the installer's own message.

When another `axon` on the laptop is installing the client at the same time,
`axon` waits for it and says so:

```text
sky: waiting for another axon to finish installing the SkyPilot client
```

### Keeping the client at the server's version

When the server runs another SkyPilot version than the client, the login
moves the client to the server's, and says between which versions:

```text
sky: SkyPilot client <old> -> <new>, the server's version
```

A move to the server's version prints the same lines as the first install,
without the `uv` line when `uv` is already in place.

## SkyPilot's files in ~/.sky

SkyPilot keeps its own settings and its login in `~/.sky`. `axon` does not
move them. Removing `~/.axon` leaves `~/.sky` in place. A login to your
organization's server that is already in `~/.sky`, such as one made with
`sky` by hand, is reused while it is still good.

## When something fails

The messages about the client are under
[The SkyPilot client](/axon/troubleshooting/#the-skypilot-client) on the
troubleshooting page.
