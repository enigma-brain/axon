---
title: Teleport
description: A Teleport pathway reaches the API server through Teleport, with tsh and a tunnel that axon runs for you.
---

A Teleport pathway reaches your organization's API server through Teleport,
and works from wherever Teleport does. `axon login` runs the Teleport steps,
so you run no `tsh` command by hand.

## Logging in

1. **It finds `tsh`**, Teleport's client, on your `PATH`
   ([below](#tsh-teleports-client)).
2. **It signs you in to Teleport** with `tsh login --proxy=<proxy>`, which opens
   your organization's sign-in page in a browser. Where no browser opens by
   itself, as on WSL, `tsh` prints a link instead: open it in a browser to
   finish the sign-in. When your Teleport certificate is still valid, nothing
   opens. The certificate lasts as long as your
   organization's Teleport sets.
3. **It brings up a tunnel** to the API server, on port 46581 of your
   laptop. The tunnel runs in the background, outlives the terminal, and keeps
   its log in `~/.axon/state/tunnel.log`. `ssh` to a cluster, VS Code
   Remote-SSH and the dashboard at `http://127.0.0.1:46581` all go through it.
4. **It points each command group at the tunnel.** What this means for
   `axon sky` is on the
   [sky page](/axon/sky/#on-a-teleport-pathway).

The tunnel is `axon`'s own: a process on port 46581 that is not `axon`'s
tunnel makes `axon login` and `axon sky` stop and name it.

## tsh, Teleport's client

A Teleport pathway needs Teleport's client, `tsh`, on your `PATH` before you
log in. Install it from Teleport's
[install instructions](https://goteleport.com/docs/installation/). `axon`
does not install `tsh`, and never replaces the one you have. With no `tsh`
on your `PATH`, `axon login` stops before any login step:

```text
axon: establish the Teleport session: this pathway needs Teleport's client, tsh, and no tsh was found on PATH. Install it from https://goteleport.com/docs/installation/ and run `axon login` again.
```

A `tsh` works when its major version is that of your organization's Teleport
or one behind it. When its version is too old or too new, `axon` names both
versions and stops; install a `tsh` whose major version is your organization's
Teleport's or one behind it, which for a `tsh` that is too new means an older
one.

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

To restore it, run `axon login`. `axon sky` also restarts a stopped tunnel
by itself while your certificate is valid, with the line:

```text
sky: restarting the Teleport tunnel
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
