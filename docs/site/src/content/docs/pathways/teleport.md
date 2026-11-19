---
title: Teleport
description: A Teleport pathway reaches the API server through Teleport, with tsh and a tunnel that axon runs for you.
---

A Teleport pathway reaches your organisation's API server through Teleport,
and works from wherever Teleport does. `axon login` runs the Teleport steps,
so you run no `tsh` command by hand.

## Logging in

1. **It finds `tsh`**, Teleport's client, or offers to install it
   ([below](#when-tsh-is-missing)).
2. **It signs you in to Teleport** with `tsh login --proxy=<proxy>`, which opens
   your organisation's sign-in page in a browser. When your Teleport certificate
   is still valid, nothing opens. The certificate lasts as long as your
   organisation's Teleport sets.
3. **It brings up a tunnel** to the API server, on port 46581 of your
   laptop. The tunnel runs in the background, outlives the terminal, and keeps
   its log in `~/.axon/state/tunnel.log`. `ssh` to a cluster, VS Code
   Remote-SSH and the dashboard at `http://localhost:46581` all go through it.
4. **It points each command group at the tunnel.** What this means for
   `axon devenv` is on the
   [backend's page](/axon/devenv/skypilot/#on-a-teleport-pathway).

The tunnel is `axon`'s own: a process on port 46581 that is not `axon`'s
tunnel makes `axon login` stop and name it.

## When tsh is missing

When a `tsh` is on your `PATH`, `axon` uses it and never replaces it. A `tsh`
works when its major version is that of your organisation's Teleport or one
behind it. When its version is too old or too new, `axon` names both versions
and stops, and you update that `tsh`.

When there is no `tsh`, on macOS or on Linux on x86-64, `axon login` offers
to install one:

```text
Teleport's client, tsh, is not installed.
Install tsh <version> into ~/.axon/tools? [Y/n]
```

- **Press Enter to install.** `axon` downloads Teleport's archive at the
  version your organisation's Teleport runs, about 180 to 200 MB, prints its
  size and each quarter of the progress, and checks it against the checksum
  Teleport publishes. It keeps `tsh` in `~/.axon/tools/` for its own use and
  does not put it on your `PATH`. Nothing asks for `sudo` or a password.
- **Answer `n` to install it yourself.** `axon` prints Teleport's install link
  and stops. Run `axon login` again once `tsh` is on your `PATH`.
- **Later logins download nothing**, until the Teleport proxy moves to a
  version `axon`'s `tsh` does not support. Then the next login replaces it
  without asking.

## The tunnel

:::caution
Use a Teleport pathway only on a machine that only you log in to. The tunnel
accepts every connection made on your laptop and adds your Teleport identity
to it, so while it is up, a program of any account on the machine reaches
the API server as you, with no password or certificate of its own.
:::

When the tunnel is up and the server knows you through it, a repeated
`axon login` leaves it running, so your `ssh` and VS Code sessions carry on.

The tunnel stops when:

- your Teleport certificate expires;
- the laptop restarts;
- `axon login` replaces it, because it is down or the server does not know you
  through it, which interrupts sessions through it;
- you switch pathways.

To restore it, run `axon login`. `axon devenv` also restarts a stopped tunnel
by itself while your certificate is valid, with the line:

```text
axon: restarting the Teleport tunnel
```

## Teleport's files in ~/.tsh

Teleport keeps its profile and certificates in `~/.tsh`, which `tsh` writes.
Removing `~/.axon` leaves `~/.tsh` in place.

Removing `~/.axon` also leaves running a tunnel that the current `axon`
started. The next login adopts that tunnel only when its
Teleport proxy and application match the pathway it uses. Otherwise it names
the process that holds port 46581 and stops, as it does for a tunnel that an
older `axon` started without `--proxy`. Stop that process, then run
`axon login` again.

## When something fails

The messages are under
[Signing in on a Teleport pathway](/axon/troubleshooting/#signing-in-on-a-teleport-pathway)
on the troubleshooting page.
