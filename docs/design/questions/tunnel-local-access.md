# What limits use of the tunnel to its owner?

## Context

On a teleport pathway the API server is reached through a local tunnel,
`tsh proxy app`, that listens on a port of the machine. The tunnel adds the
owner's Teleport identity to every request it carries, and it accepts every
local connection without asking who made it.

A process of any local account reaches the API server as the tunnel's
owner while the tunnel is up, with no password and no certificate of its
own, whether or not that account is logged in. On a machine whose other
accounts and processes the owner trusts, that is no exposure. On a machine
with other people's accounts, such as a shared workstation, it is.

`axon` has no reliable way to tell the two kinds of machine apart. A second
account that exists is not a second user who is logged in, and a shared
machine has moments with one user on it.

The tunnel also serves `ssh` to a cluster, VS Code Remote-SSH and the
SkyPilot dashboard in a browser. Any control in front of it has to let all
of them through for the owner.

## Options

- **State the limit.** A teleport pathway is for machines that only the
  user logs in to, and the design says why. Nothing is built and nothing is
  enforced; a user on a shared machine carries the exposure.
- **State the limit and warn at login.** `axon login` prints a warning when
  it sees other users logged in. The detection is a guess that is wrong in
  both directions, and a warning does not close the port.
- **An authenticating proxy in `axon`.** `axon` puts its own listener in
  front of the tunnel and lets through only requests that carry a secret
  held in a file readable by the owner. It is a real control and makes
  shared machines safe. `axon` gains a long-running component, and it is
  unverified that the SkyPilot client, the dashboard and the `ssh` path all
  send such a secret.

## Recommendation

State the limit. A teleport pathway is for a machine whose other local
accounts and processes the user trusts, a condition the user can judge and
`axon` cannot, and the proxy rests on unverified behaviour of three
clients.

## Resolution path

- An organisation needs a teleport pathway on a machine that other people
  log in to.
- Then: confirm whether the tunnel's listener is reachable by other local
  accounts on macOS, Linux and WSL as described, and test whether the
  SkyPilot client, the dashboard and `ssh` to a cluster each work through a
  listener that demands a secret.
