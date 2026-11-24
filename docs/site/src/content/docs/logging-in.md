---
title: Logging in
description: Make a pathway or add your organization's config file, and log in with axon login.
---

You log in once with `axon login`. It sets up how you reach the server, signs
you in, and connects each command group.

## The first login

Run `axon login` with nothing else. With no pathway set up, it asks how you
reach the SkyPilot API server, its address, and a name for the pathway:

```text
No pathway is set up on this laptop yet. Answer a few questions to make one, or press Ctrl-C and run `axon login --config <source>` with the file your organization gives you.
How do you reach the SkyPilot API server?
  1) Directly, at its address
  2) Through Teleport
Kind [1-2]: 1
SkyPilot API server address, such as https://sky.example.org: sky.example.org
Checking https://sky.example.org
Name for this pathway [Enter for sky.example.org]:
Stored the pathway "sky.example.org" in <home>/.axon/configs/sky-example-org.toml
```

Through Teleport, it asks for the Teleport proxy's address and the Teleport
application that serves SkyPilot, `skypilot` unless your organization says
otherwise. Press Enter to take a default shown in brackets.

`axon` stores nothing until the server answers. An address that does not answer
is reported and asked for again.

The same answers can be given as flags, which ask nothing, for a script or a
second pathway:

```sh
axon login --sky-endpoint https://sky.example.org --name Campus
axon login --teleport-proxy teleport.example.org --sky-app skypilot --name Remote
```

`--name` is optional, and `--sky-app` is `skypilot` when left out. With flags,
an address that does not answer stops the login with an error. With no terminal
and no pathway set up, `axon login` stops and names these flags and `--config`.

`axon` writes the pathway into `~/.axon/configs/` as a
[config file](/axon/config-file/) of its own, a hand-made one. Its identifier,
for `--pathway`, is the name in lowercase with dashes: `Campus` is `campus`. From
then on it is a pathway like any other.

### With your organization's config file

An organization with many members can give them a config file instead, which
sets up every member's pathways in the organization's own words, and keeps them
up to date. Give it to `axon` with `--config`. It takes one of three sources:

```sh
axon login --config https://example.org/axon.toml
axon login --config ~/Downloads/axon.toml
<command that prints the file> | axon login --config -
```

`axon` checks the file and stores it as `~/.axon/configs/<id>.toml`, named by the
`id` the file holds, then goes on to log in. It prints the line below, with the
file's revision when it has one:

```text
Stored the config file <path> (revision 3)
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

A pathway is a way to reach your organization's API server. You make one at
the first login, or an organization writes its own in its file. Each is of one
of two kinds:

- A [**direct**](/axon/pathways/direct/) pathway reaches the server at its own
  address. The server may answer only from your organization's network.
- A [**Teleport**](/axon/pathways/teleport/) pathway reaches it through
  Teleport, and works from wherever Teleport does.

One pathway is active at a time, and `~/.axon/settings.toml` holds its
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

1. Add the config file, when `--config` is given, or make the pathway the
   flags give.
2. Determine the pathway, after asking for one when none is set up.
3. Fetch the pathway's file again, when its source is an address.
4. Sign in on the pathway, by its kind's steps: those of a
   [direct](/axon/pathways/direct/) or a [Teleport](/axon/pathways/teleport/)
   pathway.
5. Connect each command group. For `axon sky`, its
   [page](/axon/sky/#what-the-login-does-for-sky) says
   what this installs and checks.

When it succeeds, `axon` records your email in `~/.axon/state/email`, and
says for each command group that it is connected and as whom.

## Replacing a stored pathway

When a pathway you add, from a config file or by hand, has the name or the
server of one already stored, `axon` asks before it replaces anything, and says
what would go: a hand-made pathway, or a whole config file with all its
pathways.

```text
The stored pathway "Campus" has the same name as the pathway you are adding.
Replacing it removes the config file <path> and all its pathways: Campus and Remote.
Replace it? [y/N, Enter for no]:
```

The answer is no unless you type `y`.

- **Yes** removes what the question named and stores the new one. When the
  pathway removed is the one you are on, `axon` first disconnects it, as a
  switch does.
- **No**, when the name is the same: a config file is not added, and a hand-made
  pathway asks for another name.
- **No**, when only the server is the same: both are kept.

`axon login --replace` answers yes to every such question. With no terminal,
`axon` stops at the question and names `--replace`.

Adding a newer copy of a stored config file, with the same `id`, replaces it
without a question. When both copies have a `revision` and the one you add is
older, `axon` asks first, and no keeps the stored copy.

## When your organization changes its file

How a change reaches your laptop depends on where the file came from:

- **An `https` address:** each `axon login` fetches again the file that holds the
  pathway you use, before it signs you in, and stores the new one when it
  passes the checks and keeps its `id`. You do nothing. Only that one file is
  fetched, and a file that `--config` added in the same login is not fetched
  twice. When the new file differs, `axon` prints `Updated the config file <path>`,
  with the old and new revisions when the file has them. A fetched file with an
  older revision than the stored copy asks first, and with no terminal the
  stored copy is kept. When the fetch fails, or the new file is refused or has a
  pathway named like one in another stored file, `axon` says so in one line and
  logs in with the file it has.
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

## Seeing what is stored

`axon config list` lists every stored config file and its pathways, and says
which pathway is in use:

```text
The pathway in use is Remote (remote), from remote.

example-campus
  from https://example.org/axon.toml
  revision 3, stored 2026-11-20 14:03 CET

    Campus  --pathway campus
      direct, https://sky.example.org

remote
  made by hand at `axon login`
  no revision, stored 2026-11-21 09:12 CET

  * Remote  --pathway remote
      teleport, proxy teleport.example.org, application skypilot
```

For each file it shows the `id`, where the file came from or that it was made
by hand at `axon login`, its revision, and when it was stored. Under each file
are its pathways: the name, the identifier that `--pathway` takes, the kind and
the server. The pathway in use is marked `*`. A file from an address is shown
without its query string, which can hold a token.

It reads only files on your laptop: it contacts no server and changes nothing.
A stored file that cannot be read is listed with its path and the reason, the
rest are still listed, and the command exits with an error.

## Removing a file

Run `axon config list` to find the file's `id`, and delete `<id>.toml` from
`~/.axon/configs/`. There is no command for it. When it held the
active pathway and another valid file is stored, the next `axon login` disconnects
that pathway and chooses another. When no valid file is left, `axon login`
disconnects the pathway first: it stops what it started for the pathway, such
as a Teleport tunnel, and forgets the logged-in email. Then, on a terminal, it
asks for a pathway, as at the first login. At each login, a stored file that fails the
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
Kept the config files in <home>/.axon/configs; run `axon login` to log in again
```

When SkyPilot's cookie file holds no login for the server, the line about
the saved login is left out. On a Teleport pathway:

```text
sky: logged out of the SkyPilot API server
sky: stopping the Teleport tunnel on port 46581
teleport: logged out of <proxy>
Removed the record of the connected pathway and the logged-in email
Kept the config files in <home>/.axon/configs; run `axon login` to log in again
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
