---
title: Troubleshooting
description: What each message from axon that asks you to act means, and what to do.
---

`axon`'s own messages go to standard error and begin with `axon:`. Each says
which step failed and what to do next. This page lists the messages that ask
you to act. Where a message holds a value that is yours, such as a host or a
path, it appears here as `<host>` or `<path>`.

A message from a step of the login may begin with the name of the stage that
failed, such as `connect devenv:` or `establish the Teleport session:`, before
the text shown here.

Most problems end with the same step: run `axon login` again.

## Installing

### `axon install: there is no axon release for <os> on <arch>.`

`axon` is released for macOS on Apple silicon, macOS on Intel and Linux on
x86-64. On Windows, install it inside WSL.

### `axon install: curl is not installed`, or neither `sha256sum` nor `shasum`

Install `curl`, and `sha256sum` or `shasum`, then run the install line again.

### `axon install: could not download <what> from <address>`

The download failed. Check your network, then run the install line again. For a
named version, check that the release exists. When the install fails,
nothing was written to `~/.local/bin`.

### `axon install: the checksum of <archive> does not match checksums.txt`

The archive is not the one the release published. Nothing was installed. Run the
install line again, and if it fails the same way, do not use the archive.

### `axon: command not found` or `command not found: axon`

Check that `~/.local/bin/axon` exists. When it does not, the install did not finish:
run the install line again. When it does, `~/.local/bin` is not on your `PATH`: add
the line the install script printed to your shell's startup file, and open a new
terminal:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

## The organisation file

### ``axon: no organisation configuration. Run `axon login --org <source>` with the address or file your organisation gives you.``

No organisation file that passes the checks is stored on this laptop. When
one was in use, `axon login` has already disconnected its pathway, stopping
the Teleport tunnel and forgetting the logged-in email. Run `axon login --org`
with the address or file your organisation gave you.

### A refused organisation file

A file that fails a check is refused whole and nothing changes. The message names the
field and the check, and begins `axon: add the organisation file:`. The checks are
on [Writing an organisation file](/axon/organisation-file/#what-axon-checks). Tell
your organisation which field it names. Messages seen most:

| Message | What it means |
|---|---|
| `format: must be 1` | The file's `format` is missing or is another number. |
| `id: must be lowercase letters, digits and hyphens, start with a letter, and be at most 32 characters` | The file's `id` is not of the allowed form. |
| `pathway: the file must have at least one [[pathway]]` | The file holds no pathway. |
| `pathway "<id>": name: is required and must be text` | The pathway has no `name` (or no `description`, which gives the same message for that field). |
| `pathway "<id>": kind: must be "direct" or "teleport"` | The pathway's `kind` is missing or unknown. |
| `pathway "<id>": devenv.endpoint: must be an https address` | A direct pathway's address is missing or does not start with `https://`. |
| `pathway "<id>": teleport.proxy: must be a hostname, optionally with a port` | A Teleport pathway's proxy is missing or is not a host. |
| `<field>: not a field of an organisation file` | The file holds a field that the format does not have, such as a misspelling. |
| `pathway "<id>": id: already used by the stored file <path>` | Another stored file has a pathway with the same identifier. |
| `not valid TOML: <reason> (line <n>)` | The file is not valid TOML. |

### `axon: add the organisation file: <address>: an address must start with https://`

`--org` takes an `https` address. Use the address your organisation gave you, which
starts with `https://`.

### `an address must not carry a user name or password`

An address is fetched without credentials. Remove them from the address. When your
organisation protects the file, ask for it as a file and use `--org <path>`.

### `axon: add the organisation file: read <path>: no such file or directory`

The path is wrong. Check it, and quote it when it holds a space.

### `axon: add the organisation file: fetch <address>: ...`, or `the server answered <status>`

The address could not be fetched. Check your network and the address. A server that
answers with an error is your organisation's to fix.

### `axon: left out <path>: <reason>`

A stored organisation file fails the checks, so `axon` leaves it out of this login.
The reason is one of the refusals above. Ask your organisation for a new file and add
it with `axon login --org <source>`, or delete the stored file from `~/.axon/orgs/`.

### ``axon: could not update <path> from <address>: <reason>. Using the stored file.``

### ``axon: could not update <path>: <reason>. Using the stored file.``

At login, `axon` fetches again the file that holds your pathway, when it came from an
address. The first form means the fetch failed or the new file was refused; the second
means `axon` could not read what it recorded about the file's source. Either way `axon`
carries on with the file it has. Nothing needs doing now; when the reason is a refusal,
tell your organisation.

## Choosing a pathway

### `axon: determine the pathway: no stored organisation file has the pathway "<id>". The stored pathways are <list>.`

`--pathway` names an identifier that no stored file has. Use one from the list.

### ``axon: determine the pathway: no pathway chosen. Run `axon login --pathway <id>` with one of <list>.``

`axon login` had to ask which pathway you are on and got no answer, because the
answer was empty or there is no terminal. Run it with `--pathway <id>`, naming one
of the list.

### `The pathway "<id>" is no longer in its organisation file. Using the pathway "<id>".`

Your organisation changed its file, and the pathway you used is not in it. One pathway
is stored, so `axon` uses it and says which. With several stored, the message ends after
the first sentence and `axon` asks which you are on.

### `axon: determine the pathway: the pathway "<id>" is no longer in its organisation file. The stored pathways are <list>.`

You passed `--pathway` with a pathway that your organisation's file no longer has. Use
one from the list.

### ``axon: disconnect the previous pathway: <reason>. Run `axon login --pathway <id>` again.``

`axon` could not disconnect the pathway you left, for example because it could not
stop its tunnel. The active pathway is unchanged. Fix the reason, then run the
command it names.

## Signing in on a direct pathway

### `Can't reach <host>.`

The server does not answer from where you are. `axon` stops without opening a
browser. Your organisation's hint, when its file gives one, follows the message and
says what to do, such as connecting to a network. Then run `axon login` again.

### ``axon: connect devenv: the login was not completed in the browser within 5 minutes. Run `axon login` again.``

The browser sign-in was not finished. Run `axon login` again, sign in and click
"Authorize".

### ``the SkyPilot login ended with needs_auth twice. Run `axon login` again.``

The server did not accept the login after `axon` cleared the stale session. Run
`axon login` again. When it repeats, tell your organisation.

### ``remove <path>: <reason>. Remove it by hand and run `axon login` again.``

`axon` could not remove SkyPilot's cookie file, `~/.sky/cookies.txt`, or the file
`SKYPILOT_API_COOKIE_FILE` names. Remove it yourself, then log in again.

### ``the SkyPilot login failed (<reason>). Run `axon login` again.``

SkyPilot's login stopped with an error. Read the reason, and run `axon login` again.

## Signing in on a Teleport pathway

### `tsh <version> at <path> does not work with the Teleport proxy <proxy>, which runs <version> and supports tsh <n> and <n-1>. Update tsh: <link>`

The `tsh` on your `PATH` is too old or too new for your organisation's Teleport. `axon`
never replaces a `tsh` you installed. Update it, or remove it from your `PATH` so that
`axon` installs its own.

### ``Teleport's client, tsh, is not on PATH. Install it from <link> and run `axon login` again``

`axon` installs `tsh` only on macOS and on Linux on x86-64. Install it from the link,
then run `axon login` again.

### ``tsh was not installed. Install it from <link> and run `axon login` again``

You answered `n` to the offer to install `tsh`. Install it from the link, or run
`axon login` again and press Enter at the question.

### `install tsh <version>: ...` or `download Teleport <version>: ...`

The download or its check failed: a network error, a stall of 30 seconds with no data,
or a checksum that did not match. Check your network and run `axon login` again.

### `Teleport login (tsh login --proxy=<proxy>): <reason>`

The Teleport sign-in failed or was not completed. Run `axon login` again and finish the
sign-in in the browser.

### ``port 46581 is held by process <pid> (<command>), which is not the Teleport tunnel. Stop that process and run `axon login` again.``

Another program uses the tunnel's port. Stop it, then run `axon login` again. A tunnel
started by an older `axon` is also such a process.

### ``could not stop the Teleport tunnel, process <pid>. Stop it with `kill -9 <pid>` and try again``

`axon` could not stop its own tunnel. Run the command the message gives, then run
`axon login` again.

### ``the SkyPilot API server is not answering through the Teleport tunnel. Retry `axon login` later.``

The tunnel is running and your Teleport login is fine, but nothing answered through it
within 10 seconds. The API server, or Teleport's connection to it, is not answering.
Retry later; logging in again at once does not help.

### The end of the tunnel's log, then ``Run `axon login` again.``

The tunnel exited, for example on a Teleport error. The message ends with the tail of
the log, which is in `~/.axon/state/tunnel.log`. Run `axon login` again; when it repeats,
the log says why.

### ``the SkyPilot API server did not report your Teleport identity through the tunnel. Run `axon login` again.``

The tunnel is up but the server did not know you through it. Run `axon login` again,
which signs you in to Teleport and starts a fresh tunnel.

## The SkyPilot client

### ``install the SkyPilot client: <reason>. Check your network and run `axon login` again.``

The client's install failed. The reason says which step failed, `uv`, Python or the
client, and ends with the installer's own message. Check your network and run `axon login`
again.

### ``update the SkyPilot client from <old> to <new>: <reason>. Check your network and run `axon login` again.``

The server runs another SkyPilot version, and moving the client to it failed. Check your
network and run `axon login` again.

### `the SkyPilot client cannot be installed on <platform>`

`axon` installs its client on macOS on Apple silicon and Intel, and on Linux on x86-64.

### ``read SkyPilot's configuration <path>: <reason>. Fix or remove the file and run `axon login` again.``

SkyPilot's configuration file is not valid. Fix it or remove it, then run `axon login`.

## Running `axon devenv`

`axon devenv` checks the connection before each command, and stops with one of these
when the check fails. Each message states the action for that failure.

### ``axon: Run `axon login` first.``

No organisation file is stored, no login has run on this laptop, it did not finish, or
the file of your pathway changed since you logged in. Run `axon login`.

### ``axon: SkyPilot session expired. Run `axon login`.``

Your session on a direct pathway has run out. Run `axon login`.

### ``axon: Teleport session expired. Run `axon login`.``

Your Teleport certificate has run out. Run `axon login`.

### ``axon: Run `axon login` to update the SkyPilot client.``

The server runs another SkyPilot version than your client. The login moves the client
to the server's.

### `axon: Can't reach <host>.`

As for a login: the server does not answer from where you are. Follow your
organisation's hint, when the message has one.

### `axon: The SkyPilot API server is not answering. Retry later.`

The server itself is down, or on a Teleport pathway does not answer after a restart of
the tunnel. Logging in again does not help. Retry later.

### ``axon: Couldn't restart the Teleport tunnel. Run `axon login`.``

`axon devenv` tried to restart a stopped tunnel, and the tunnel's process exited. The tail
of the tunnel's log follows the message. Run `axon login`, which starts a fresh tunnel.

### ``axon: the SkyPilot client is not installed. Run `axon login` first.``

### ``axon: Teleport's client, tsh, is not found. Run `axon login`.``

The client or `tsh` that `axon` keeps in `~/.axon/tools/` is missing. Run `axon login`,
which installs it.

### `axon: axon devenv does not cover "<command>".`

`axon devenv` runs only the commands on the [devenv page](/axon/devenv/#the-commands).
Run `axon devenv --help` for the list.
