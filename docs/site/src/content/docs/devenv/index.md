---
title: devenv
description: Run dev environments, GPUs, jobs and volumes with axon devenv.
---

`axon devenv` is the way to your organisation's API server for dev
environments, GPUs, managed jobs and volumes. [Log in](/axon/logging-in/)
first.

Its backend is [SkyPilot](/axon/devenv/skypilot/), whose page says how each
command runs and what `axon` does for it.

## The commands

| Group | Command | What it does |
|---|---|---|
| Dev environments | `launch` | Launch a cluster, or run a task on one |
| | `status` | Show clusters |
| | `down` | Tear down clusters |
| | `autostop` | Stop or tear down clusters when they are idle |
| | `exec` | Run a task or command on an existing cluster |
| | `logs` | Show the logs of a job on a cluster |
| | `queue` | Show the job queue of clusters |
| GPUs | `gpus list` | Show the GPUs and other accelerators that can be requested |
| Managed jobs | `jobs launch` | Launch a managed job |
| | `jobs queue` | Show managed jobs |
| | `jobs logs` | Show the logs of a managed job |
| | `jobs cancel` | Cancel managed jobs |
| Volumes | `volumes ls` | List volumes |

Any other command is refused:

```text
axon: axon devenv does not cover "<command>". Run "axon devenv --help" for the commands it covers
```

## Help

`axon devenv --help` lists the covered commands. `axon devenv <command> --help`
prints a command's flags and examples, for example:

```sh
axon devenv launch --help
```

## What axon checks first

Before each command, `axon devenv` makes one request to the API server, to
check the connection. It never opens a browser and never installs anything.
When the check fails, it stops and says what to do, and the command does not
run. The messages, and what each needs, are on the
[troubleshooting page](/axon/troubleshooting/#running-axon-devenv).

When your pathway relies on something `axon` keeps running, and that has
stopped while your sign-in is still valid, the check restarts it for you.

## Example

Show your clusters:

```sh
axon devenv status
```
