# The Teleport tunnel is a detached process that axon tracks

## Context

On a teleport pathway the API server is reached through a local tunnel,
`tsh proxy app`, that listens on a port of the laptop. The tunnel has to
stay up after `axon login` returns. `axon sky` commands use it, and so do
`ssh` to a cluster, VS Code Remote-SSH and the SkyPilot dashboard, none of
which run through `axon`.

The Teleport certificate lasts as long as the organization's Teleport sets,
typically hours. Whatever runs the tunnel, it stops working when the
certificate expires, and a login in a browser is needed before it
works again. A tunnel process also ends with a reboot, and exits at once
when its certificate is stale or its port is taken.

## Rationale

`axon login` starts the tunnel as a process detached from the terminal,
records its process ID, and sends its output to a log file.

- The tunnel outlives the command and the terminal that started it, so
  `ssh`, VS Code and the dashboard work between `axon` commands.
- "Up" is judged by whether the SkyPilot health endpoint answers through the
  tunnel. A process that exists but does not serve is treated as down.
- `axon` stops a process only when it belongs to the user and its command
  line is exactly the tunnel's command, proxy, application and port
  included. A process ID is reused after a reboot, so the recorded ID alone
  does not identify the tunnel; the command line does. A tunnel to another
  proxy, another application or another port is never touched.
- A tunnel started by hand with the same command is adopted. A user who ran
  the command themselves, or whose record was removed, is not stopped at
  login by their own tunnel.
- The log file holds what `tsh` said when a tunnel fails to start, and
  `axon` shows it.
- One implementation serves macOS, Linux and WSL.

## Alternatives considered

- **An operating-system service that keeps the tunnel running.** It needs a
  launchd implementation and a systemd one, some WSL setups have no systemd,
  and after the certificate expires the service restarts in a loop until the
  user logs in, so the login at each expiry remains.
- **A tunnel that runs only while an `axon` command runs.** `ssh`, VS Code
  Remote-SSH and the dashboard have no tunnel.
