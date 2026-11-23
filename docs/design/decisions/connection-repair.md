# axon sky repairs only what needs no user action

## Context

Before it forwards a command, `axon sky` checks that the API server
answers and that the login is good. The check fails for several reasons: the
tunnel has stopped, the Teleport certificate or the server's own login
session has expired, the organization's network is out of reach, or the
client's version differs from the server's.

Some of these are fixed in under a second with no one present. Others need a
browser, a network or VPN connection, or a download of several minutes.
`axon sky` commands run in scripts and agent sessions as well as at a prompt.

## Rationale

`axon sky` restarts a stopped tunnel when the Teleport certificate is
still valid, says in one line that it did, and checks the connection again
before it runs the command. It restarts the tunnel once: a tunnel that is
still down after the restart stops the command. When the restarted tunnel's
process runs and the server behind it does not answer, the server is
reported as unavailable, because a login cannot fix it and would stop a
working tunnel; when the process exits, the end of its log is shown and the
user is told to log in. A stopped tunnel hides every other condition, so the
first check says nothing about the client's version; the second one does.
Every other failure stops the command with a message that names the cause
and the exact next step.

- A reboot or a closed laptop does not cost the user a manual login when
  nothing has expired.
- An `axon sky` command never opens a browser and never starts an install, so
  it does not hang in a script and its running time stays predictable.
- Each failure has its own message, so the user is told to connect to the
  network when the network is the problem and to log in when the login is.

## Alternatives considered

- **Never repair; every failure says to run `axon login`.** A user whose
  tunnel stopped with a valid certificate logs in again by hand for nothing.
- **Repair everything inline, including the browser login and the client
  reinstall.** A command in a script waits on a browser that no one sees,
  and a command that takes a second turns into an install of several minutes
  without warning.
