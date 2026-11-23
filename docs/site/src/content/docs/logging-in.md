---
title: Logging in
description: Add your organization's config file, choose a pathway and log in with axon login.
---

You log in once with `axon login`. It adds your organization's config file, chooses
how you reach the server, signs you in, and connects each command group.

## The first login

Your organization gives you an address or a file. Give it to `axon` with
`--config`. It takes one of three sources:

```sh
axon login --config https://example.org/axon.toml
axon login --config ~/Downloads/axon.toml
<command that prints the file> | axon login --config -
```

`axon` checks the file and stores it as `~/.axon/orgs/<id>.toml`, named by the
`id` the file holds, then goes on to log in. It prints:

```text
Stored the config file <path>
```

A file that fails a check is refused with a message that names the field, and
nothing changes. [Troubleshooting](/axon/troubleshooting/) lists the messages.

An address is fetched without credentials, and must start with `https://`.
With `--config -`, standard input is the file, so `axon login` asks its
questions on the terminal. Without a terminal, a question that has a flag,
such as which pathway, stops and names the flag; any other question is
answered no.

Later logins need no `--config`:

```sh
axon login
```

Running `axon login` again is safe. When everything is in place it changes
nothing.

## Pathways

A pathway is a way to reach your organization's API server. An organization
writes its own pathways in its file, each of one of two kinds:

- A [**direct**](/axon/pathways/direct/) pathway reaches the server at its own
  address. The server may answer only from your organization's network.
- A [**Teleport**](/axon/pathways/teleport/) pathway reaches it through
  Teleport, and works from wherever Teleport does.

One pathway is active at a time, and `~/.axon/config.toml` holds its
identifier. Later logins use it without asking.

### Choosing one

- With **one pathway** in all your stored files, `axon` uses it without asking.
- With **more than one**, the first login asks which you are on. It offers each
  by the name and description your organization wrote, file by file in the order
  of the files' identifiers. Answer with the number:

  ```text
  Which pathway are you on?
    1) Campus  Single sign-on in a browser. Needs the campus network.
    2) Remote  Teleport. Works from anywhere.
  Pathway [1-2]:
  ```

- **`--pathway <id>`** chooses without asking and makes that pathway the
  active one. It is how you switch:

  ```sh
  axon login --pathway remote
  ```

  An identifier that no stored file has is refused, with the list of those
  they have.
- **`--config`** makes a pathway of the file it adds the active one, unless the
  active pathway is already in that file. With several pathways in the file,
  it asks which.

Switching first disconnects the pathway you leave: what `axon` started for it
stops, such as a [Teleport tunnel](/axon/pathways/teleport/#the-tunnel), and
the recorded email is removed until the new login records one. `axon` goes
by what it recorded when it connected, in `~/.axon/state/pathway.toml`, so
this works even when the file that named the old pathway is gone.

## What the login does

A login runs these stages and stops at the first that fails:

1. Add the config file, when `--config` is given.
2. Determine the pathway.
3. Fetch the pathway's file again, when its source is an address.
4. Sign in on the pathway, by its kind's steps: those of a
   [direct](/axon/pathways/direct/) or a [Teleport](/axon/pathways/teleport/)
   pathway.
5. Connect each command group. For `axon sky`, its
   [page](/axon/sky/#what-the-login-does-for-sky) says
   what this installs and checks.

When it succeeds, `axon` records your email in `~/.axon/state/email`, and
says for each command group that it is connected and as whom.

## When your organization changes its file

How a change reaches your laptop depends on where the file came from:

- **An `https` address:** each `axon login` fetches again the file that holds the
  pathway you use, before it signs you in, and stores the new one when it
  passes the checks and keeps its `id`. You do nothing. Only that one file is
  fetched, and a file that `--config` added in the same login is not fetched
  twice. When the new file differs, `axon` prints `Updated the config file <path>`.
  When the fetch fails or the new file is refused, `axon` says so in one line
  and logs in with the file it has.
- **A file path or standard input:** the file is read once. Run `axon login --config`
  again with the new file.

`axon sky` never fetches a file. It uses the stored ones.

When the new file changes the servers of your active pathway, the login
disconnects it from the old ones and connects it to the new ones, and the
pathway stays active. A change to its name or text alone changes nothing.

When the new file no longer has your active pathway, the login chooses again.
With one pathway left, `axon` says which it now uses:

```text
The pathway "<id>" is no longer in its config file. Using the pathway "<id>".
```

With several left, it prints the first sentence and asks which you are on. With
`--pathway` naming the dropped pathway, it stops and lists the pathways you can
use:

```text
axon: determine the pathway: the pathway "<id>" is no longer in its config file. The stored pathways are <list>.
```

## Removing a file

Delete it from `~/.axon/orgs/`. There is no command for it. When it held the
active pathway and another valid file is stored, the next `axon login` disconnects
that pathway and chooses another. When no valid file is left, `axon login`
disconnects the pathway first: it stops what it started for the pathway, such
as a Teleport tunnel, and forgets the logged-in email. Then it stops with the
message that asks for `--config`. At each login, a stored file that fails the
checks is left out, with one line that names it.

To remove everything `axon` keeps, delete `~/.axon`, as
[Files on your laptop](/axon/files/#removing-everything) describes.

## Logging out

`axon logout` undoes what `axon login` did:

```sh
axon logout
```

It logs the SkyPilot client out, removes the API server's saved login from
SkyPilot's cookie file on a direct pathway, stops the Teleport tunnel when
one runs, ends your Teleport session on a Teleport pathway, and removes
`axon`'s record of the connection and of your email. Other servers' cookies
in the file are kept. It keeps your config files and your pathway, so
`axon login` with no flags logs you in again. It
prints one line for each step, for example on a direct pathway:

```text
sky: logged out of the SkyPilot API server
sky: removed the saved login for <endpoint>
Removed the record of the connected pathway and the logged-in email
Kept the config files in <home>/.axon/orgs; run `axon login` to log in again
```

When SkyPilot's cookie file holds no login for the server, the line about
the saved login is left out. On a Teleport pathway:

```text
sky: logged out of the SkyPilot API server
sky: stopping the Teleport tunnel on port 46581
teleport: logged out of <proxy>
Removed the record of the connected pathway and the logged-in email
Kept the config files in <home>/.axon/orgs; run `axon login` to log in again
```

A step that does not apply says why it was skipped:

```text
sky: skipped logging out of SkyPilot: the SkyPilot client is not installed
sky: skipped logging out of SkyPilot: the SkyPilot client is not logged in
teleport: skipped logging out of <proxy>: tsh is not on PATH
teleport: skipped logging out of Teleport: the record of the connected pathway is from an earlier axon. Run `tsh logout` to end a Teleport session.
```

With nothing logged in, it says so:

```text
Nothing is logged in.
```

When a step fails, `axon logout` says which one and carries on with the
others, then stops with an error. Run it again to retry:

```text
axon: could not log out of SkyPilot: <reason>
axon: logout did not finish. Run `axon logout` again.
```

To clear a shared machine completely, run `axon logout`, then delete
`~/.axon` and `~/.sky`. `~/.axon` holds your config files and the tools
`axon` installed; `~/.sky` holds SkyPilot's settings. On a Teleport pathway,
Teleport keeps your session in `~/.tsh`: when `axon logout` skipped the
Teleport step, run `tsh logout` before you leave the machine.
