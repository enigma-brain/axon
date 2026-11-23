---
title: Troubleshooting
description: What each message from axon that asks you to act means, and what to do.
---

`axon`'s own messages go to standard error and begin with `axon:`. Each says
which step failed and what to do next. This page lists the messages that ask
you to act. Where a message holds a value that is yours, such as a host or a
path, it appears here as `<host>` or `<path>`. To find a message, search this
page for its words with your browser's search.

A message from a step of the login may begin with the name of the stage that
failed, such as `connect sky:` or `establish the Teleport session:`, before
the text shown here.

Most problems end with the same step: run `axon login` again.

## Installing

### No release for your system

```text
axon install: there is no axon release for <os> on <arch>. axon is released for macOS on Apple silicon, macOS on Intel and Linux on x86-64; on Windows, install it inside WSL.
```

The installer stops before it writes anything.

### No release of axon was found

```text
axon install: no release of enigma-brain/axon was found. Name one with AXON_VERSION, for example AXON_VERSION=0.1.0-rc.1.
axon install: could not look up the latest release at https://github.com/enigma-brain/axon/releases/latest; no release of enigma-brain/axon was found. Name one with AXON_VERSION, for example AXON_VERSION=0.1.0-rc.1.
```

The installer could not tell which release is the latest. The second message
means the lookup itself failed: check your network, then run the install line
again. The first means GitHub answered but named no release, which is the case
while every release of `axon` is a pre-release, since GitHub never serves a
pre-release as the latest. Install a named version instead, as
[Installing](/axon/installing/) shows. Nothing was written to `~/.local/bin`.

### curl or a checksum tool is missing

```text
axon install: curl is not installed, and this script needs it to download axon. Install curl, then run this again.
axon install: neither sha256sum nor shasum is installed, so the download cannot be verified. Install one of them, then run this again.
```

Install `curl`, and `sha256sum` or `shasum`, then run the install line again.

### A download failed

```text
axon install: could not download <what> from <address>
```

The download failed. Check your network, then run the install line again. For a
named version, check that the release exists. When the install fails,
nothing was written to `~/.local/bin`.

### The checksum does not match

```text
axon install: the checksum of <archive> does not match checksums.txt (expected <checksum>, got <checksum>). Nothing was installed.
```

The archive is not the one the release published. Run the
install line again, and if it fails the same way, do not use the archive.

### The axon command is not found

```text
axon: command not found
command not found: axon
```

These are your shell's messages, not `axon`'s. Check that `~/.local/bin/axon` exists. When it does not, the install did not finish:
run the install line again. When it does, `~/.local/bin` is not on your `PATH`: add
the line the install script printed to your shell's startup file, and open a new
terminal:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

## The config file

### No config file is stored

```text
axon: no config file. Run `axon login --config <source>` with the address or file your organization gives you.
```

No config file that passes the checks is stored on this laptop. When
one was in use, `axon login` has already disconnected its pathway, stopping
the Teleport tunnel and forgetting the logged-in email. Run `axon login --config`
with the address or file your organisation gave you.

### A refused config file

A file that fails a check is refused whole and nothing changes. The message names the
field and the check, and begins `axon: add the config file:`. The checks are
on [Writing a config file](/axon/config-file/#what-axon-checks). Tell
your organisation which field it names. Messages seen most:

| Message | What it means |
|---|---|
| `format: must be 2` | The file's `format` is missing or is a number `axon` does not know. |
| `id: must be lowercase letters, digits and hyphens, start with a letter, and be at most 32 characters` | The file's `id` is not of the allowed form. |
| `pathway: the file must have at least one [[pathway]]` | The file holds no pathway. |
| `pathway "<id>": name: is required and must be text` | The pathway has no `name` (or no `description`, which gives the same message for that field). |
| `pathway "<id>": kind: must be "direct" or "teleport"` | The pathway's `kind` is missing or unknown. |
| `pathway "<id>": sky.endpoint: must be an https address` | A direct pathway's address is missing or does not start with `https://`. |
| `pathway "<id>": teleport.proxy: must be a hostname, optionally with a port` | A Teleport pathway's proxy is missing or is not a host. |
| `<field>: not a field of a config file` | The file holds a field that the format does not have, such as a misspelling. |
| `pathway "<id>": id: already used by the stored file <path>` | Another stored file has a pathway with the same identifier. |
| `not valid TOML: <reason> (line <n>)` | The file is not valid TOML. |

### A config file in an older format

```text
axon: add the config file: the file is in an older format. Run `axon login --config <source>` again with your organization's current config file.
axon: left out <path>: the file is in an older format. Run `axon login --config <source>` again with your organization's current config file.
```

The file is of format `1`, which an earlier `axon` used. `axon` does not convert it.
Run `axon login --config` with the address or file your organization gives you now; the
second form is a stored file, which the new file replaces when its `id` is the same.

### The address does not start with https

```text
axon: add the config file: <address>: an address must start with https://
```

`--config` takes an `https` address. Use the address your organisation gave you, which
starts with `https://`.

### The address carries a user name or password

```text
axon: add the config file: <address>: an address must not carry a user name or password
```

An address is fetched without credentials. Remove them from the address. When your
organisation protects the file, ask for it as a file and use `--config <path>`.

### The config file's path does not exist

```text
axon: add the config file: read <path>: no such file or directory
```

The path is wrong. Check it, and quote it when it holds a space.

### The config file could not be fetched

```text
axon: add the config file: fetch <address>: <reason>
axon: add the config file: fetch <address>: the server answered <status>
```

The address could not be fetched. Check your network and the address. A server that
answers with an error is your organisation's to fix.

### A stored config file is left out

```text
axon: left out <path>: <reason>
```

A stored config file fails the checks, so `axon` leaves it out of this login.
The reason is one of the refusals above. Ask your organisation for a new file and add
it with `axon login --config <source>`, or delete the stored file from `~/.axon/orgs/`.

### The config file could not be updated

```text
axon: could not update <path> from <address>: <reason>. Using the stored file.
axon: could not update <path>: <reason>. Using the stored file.
```

At login, `axon` fetches again the file that holds your pathway, when it came from an
address. The first form means the fetch failed or the new file was refused; the second
means `axon` could not read what it recorded about the file's source. Either way `axon`
carries on with the file it has. Nothing needs doing now; when the reason is a refusal,
tell your organisation.

## Choosing a pathway

### The named pathway is not stored

```text
axon: determine the pathway: no stored config file has the pathway "<id>". The stored pathways are <list>.
```

`--pathway` names an identifier that no stored file has. Use one from the list.

### No pathway was chosen

```text
axon: determine the pathway: no pathway chosen. Run `axon login --pathway <id>` with one of <list>.
```

`axon login` had to ask which pathway you are on and got no answer, because the
answer was empty or there is no terminal. Run it with `--pathway <id>`, naming one
of the list.

### Your pathway left its file and another is used

```text
The pathway "<id>" is no longer in its config file. Using the pathway "<id>".
```

Your organisation changed its file, and the pathway you used is not in it. One pathway
is stored, so `axon` uses it and says which. With several stored, the message ends after
the first sentence and `axon` asks which you are on.

### The named pathway left its config file

```text
axon: determine the pathway: the pathway "<id>" is no longer in its config file. The stored pathways are <list>.
```

You passed `--pathway` with a pathway that your organization's config file no longer has. Use
one from the list.

### The previous pathway could not be disconnected

```text
axon: disconnect the previous pathway: <reason>. Run `axon login --pathway <id>` again.
```

`axon` could not disconnect the pathway you left, for example because it could not
stop its tunnel. The active pathway is unchanged. Fix the reason, then run the
command it names.

## Signing in on a direct pathway

### The server cannot be reached at login

```text
Can't reach <host>.
```

The server does not answer from where you are. `axon` stops without opening a
browser. Your organisation's hint, when its file gives one, follows the message and
says what to do, such as connecting to a network. Then run `axon login` again.

### The browser login timed out

```text
axon: connect sky: the login was not completed in the browser within 5 minutes. Run `axon login` again.
```

The browser sign-in was not finished. Run `axon login` again, sign in and click
"Authorize".

### The login was not accepted after a retry

```text
the SkyPilot login ended with needs_auth twice. Run `axon login` again.
```

The server did not accept the login after `axon` cleared the stale session. Run
`axon login` again. When it repeats, tell your organisation.

### SkyPilot's cookie file could not be removed

```text
remove <path>: <reason>. Remove it by hand and run `axon login` again.
```

`axon` could not remove SkyPilot's cookie file, `~/.sky/cookies.txt`, or the file
`SKYPILOT_API_COOKIE_FILE` names. Remove it yourself, then log in again.

### The SkyPilot login failed

```text
the SkyPilot login failed (<reason>). Run `axon login` again.
```

SkyPilot's login stopped with an error. Read the reason, and run `axon login` again.

## Signing in on a Teleport pathway

### tsh does not match the Teleport version

```text
tsh <version> at <path> does not work with the Teleport proxy <proxy>, which runs <version> and supports tsh <n> and <n-1>. Update tsh: <link>
```

The `tsh` on your `PATH` is too old or too new for your organisation's Teleport. `axon`
never replaces a `tsh` you installed. Update it, or remove it from your `PATH` so that
`axon` installs its own.

### tsh is not on PATH

```text
Teleport's client, tsh, is not on PATH. Install it from <link> and run `axon login` again
```

`axon` installs `tsh` only on macOS and on Linux on x86-64. Install it from the link,
then run `axon login` again.

### You declined to install tsh

```text
tsh was not installed. Install it from <link> and run `axon login` again
```

You answered `n` to the offer to install `tsh`. Install it from the link, or run
`axon login` again and press Enter at the question.

### tsh could not be downloaded

```text
install tsh <version>: <reason>. Install tsh from <link> and run `axon login` again
download Teleport <version>: <reason>. Install tsh from <link> and run `axon login` again
```

The download or its check failed: a network error, a stall of 30 seconds with no data,
or a checksum that did not match. Check your network and run `axon login` again.

### The Teleport sign-in failed

```text
Teleport login (tsh login --proxy=<proxy>): <reason>
```

The Teleport sign-in failed or was not completed. Run `axon login` again and finish the
sign-in in the browser.

### Another process holds the tunnel's port

```text
port 46581 is held by process <pid> (<command>), which is not the Teleport tunnel. Stop that process and run `axon login` again.
```

Another program uses the tunnel's port. Stop it, then run `axon login` again. A tunnel
started by an older `axon` is also such a process.

### The Teleport tunnel could not be stopped

```text
could not stop the Teleport tunnel, process <pid>. Stop it with `kill -9 <pid>` and try again
```

`axon` could not stop its own tunnel. Run the command the message gives, then run
`axon login` again.

### The API server does not answer through the tunnel

```text
the SkyPilot API server is not answering through the Teleport tunnel. Retry `axon login` later.
```

The tunnel is running and your Teleport login is fine, but nothing answered through it
within 10 seconds. The API server, or Teleport's connection to it, is not answering.
Retry later; logging in again at once does not help.

### The tunnel exited

```text
<the end of the tunnel's log>
Run `axon login` again.
```

The tunnel exited, for example on a Teleport error. The message ends with the tail of
the log, which is in `~/.axon/state/tunnel.log`. Run `axon login` again; when it repeats,
the log says why.

### The server did not recognise your Teleport identity

```text
the SkyPilot API server did not report your Teleport identity through the tunnel. Run `axon login` again.
```

The tunnel is up but the server did not know you through it. Run `axon login` again,
which signs you in to Teleport and starts a fresh tunnel.

## The SkyPilot client

### The SkyPilot client could not be installed

```text
install the SkyPilot client: <reason>. Check your network and run `axon login` again.
```

The client's install failed. The reason says which step failed, `uv`, Python or the
client, and ends with the installer's own message. Check your network and run `axon login`
again.

### The SkyPilot client could not be updated

```text
update the SkyPilot client from <old> to <new>: <reason>. Check your network and run `axon login` again.
```

The server runs another SkyPilot version, and moving the client to it failed. Check your
network and run `axon login` again.

### No pre-built package of the SkyPilot client exists

```text
install the SkyPilot client: <reason>
update the SkyPilot client from <old> to <new>: <reason>
```

When a message ends with the installer's reason and does not mention your network, the
client cannot be installed as asked: the installer found no pre-built package, for your
platform, of the client or of something it needs, at the version asked for. `axon` never
builds one from source. The reason names the package. Running `axon login` again does not
help; the person who sets `axon` up for your organisation can tell whether the server's
SkyPilot version has packages for your platform.

### Your platform has no SkyPilot client

```text
install the SkyPilot client: the SkyPilot client cannot be installed on <platform>; axon supports macOS on Apple silicon and Intel, and Linux on x86-64
```

`axon` installs its client only on those platforms.

### SkyPilot's configuration file is not valid

```text
read SkyPilot's configuration <path>: <reason>. Fix or remove the file and run `axon login` again.
```

SkyPilot's configuration file is not valid. Fix it or remove it, then run `axon login`.

## Running `axon sky`

`axon sky` checks the connection before each command, and stops with one of these
when the check fails. Each message states the action for that failure.

### You have not logged in

```text
axon: Run `axon login` first.
```

No config file is stored, no login has run on this laptop, it did not finish, or
the file of your pathway changed since you logged in. Run `axon login`.

### The SkyPilot session expired

```text
axon: SkyPilot session expired. Run `axon login`.
```

Your session on a direct pathway has run out. Run `axon login`.

### The Teleport session expired

```text
axon: Teleport session expired. Run `axon login`.
```

Your Teleport certificate has run out. Run `axon login`.

### The SkyPilot client is out of date

```text
axon: Run `axon login` to update the SkyPilot client.
```

The server runs another SkyPilot version than your client. The login moves the client
to the server's.

### The server cannot be reached

```text
axon: Can't reach <host>.
```

As for a login: the server does not answer from where you are. Follow your
organisation's hint, when the message has one.

### The API server is not answering

```text
axon: The SkyPilot API server is not answering. Retry later.
```

The server itself is down, or on a Teleport pathway does not answer after a restart of
the tunnel. Logging in again does not help. Retry later.

### The Teleport tunnel could not be restarted

```text
axon: Couldn't restart the Teleport tunnel. Run `axon login`.
```

`axon sky` tried to restart a stopped tunnel, and the tunnel's process exited. The tail
of the tunnel's log follows the message. Run `axon login`, which starts a fresh tunnel.

### A client axon keeps is missing

```text
axon: the SkyPilot client is not installed. Run `axon login` first.
axon: Teleport's client, tsh, is not found. Run `axon login`.
```

The client or `tsh` that `axon` keeps in `~/.axon/tools/` is missing. Run `axon login`,
which installs it.

### The command is not available in axon sky

```text
axon: `axon sky api login` is not available. Run `axon login` to log in.
axon: `axon sky api logout` is not available. Run `axon logout` to log out.
axon: `axon sky api start` is not available: axon connects to your organization's API server and does not start one on this computer.
axon: `axon sky api stop` is not available: axon connects to your organization's API server and does not start one on this computer.
```

These four commands manage what `axon` itself sets up. Log in with `axon login` and
log out with `axon logout`. Every other command runs; see the
[sky page](/axon/sky/#the-commands-axon-refuses).
