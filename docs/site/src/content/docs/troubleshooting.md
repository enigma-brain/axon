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
```

GitHub answered, but named no latest release. GitHub never serves a pre-release
as the latest, so this is the case while every release of `axon` is a
pre-release. Install a named version instead, as
[Installing](/axon/installing/) shows. Nothing was written to `~/.local/bin`.

### The latest release could not be looked up

```text
axon install: could not look up the latest release at https://github.com/enigma-brain/axon/releases/latest; no release of enigma-brain/axon was found. Name one with AXON_VERSION, for example AXON_VERSION=0.1.0-rc.1.
```

The lookup of the latest release failed, usually because GitHub could not be
reached. Check your network, then run the install line again. Nothing was
written to `~/.local/bin`.

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

## Updating axon

Every failure of `axon update` ends with `axon <version> is unchanged.`: the
`axon` you ran is still in place and still works. `<version>` stands for the
release in these messages.

### axon is a development build

```text
axon: this axon is a development build (dev), and `axon update` updates only a released axon. Install a release with the install line:
```

The `axon` you ran was built from source, not installed from a release, so it
has no version to compare. Install a release with the install line that
follows the message, as [Installing](/axon/installing/) shows.

### The version is not a release version

```text
axon: "<text>" is not a release version. Name one such as 1.2.3 or 1.2.3-rc.1.
```

`--version` takes a release's version, with or without its leading `v`, such
as `1.2.3`, `v1.2.3` or `1.2.3-rc.1`. Run `axon update --version` again with
one.

### The latest release could not be found

```text
axon: could not look up the latest release at https://github.com/enigma-brain/axon/releases/latest: <reason>. Check your network connection and run `axon update` again.
axon: no release of axon was found at https://github.com/enigma-brain/axon/releases/latest. Name one with --version, such as `axon update --version 1.2.3`.
```

The reason is the short one the network gives, such as `no such host`,
`connection refused` or `timed out`. The first means GitHub could not be
reached: check your network, then run
`axon update` again. The second means GitHub answered but named no latest
release, which is the case while every release is a pre-release: name the
version you want with `--version`.

### The named release does not exist

```text
axon: there is no release v<version> of axon. Check the version, and find the releases at https://github.com/enigma-brain/axon/releases.
```

Check the version against the list of releases, then run
`axon update --version` again with one that is listed.

### A download failed

```text
axon: could not download <file> of release v<version>: <reason>.
```

The download stopped, usually because the network did; the reason is the short
one the network gives, such as `connection refused` or `timed out`. Check your network,
then run `axon update` again, with the same `--version` if you gave one.

### The release has no archive for your system

```text
axon: release v<version> has no archive for <os>_<arch>, so it cannot be installed on this machine.
```

That release was not built for your system. Name a release that was, with
`--version`, or keep the `axon` you have.

### The checksum does not match

```text
axon: the checksum of <archive> does not match checksums.txt of release v<version> (expected <checksum>, got <checksum>).
```

The archive is not the one the release published, so it was not installed.
Run `axon update` again. If it fails the same way, do not install that
release, and report it on the repository.

### axon's directory cannot be written

```text
axon: cannot write to <directory>, where axon is installed: <reason>.
axon: cannot replace <path>: <reason>.
```

`axon update` replaces the file you ran, so it needs to write to that file's
directory. Run it as the user who installed `axon` there, or install with
the install line, which puts `axon` in `~/.local/bin`.

### The new axon does not run

```text
axon: the downloaded axon <version> does not run in <directory>: <reason>.
axon: the downloaded axon <version> reports "<text>", not "axon <version>", so it was not installed.
```

The first means the new binary could not be started in the directory where
`axon` is installed. That happens when the directory is on a filesystem that
does not allow running programs; install `axon` in another directory with the
install line. The second means the release's binary is not the version it
claims to be: report the release on the repository, and keep the `axon` you
have.

## The config file

### No pathway is set up

```text
axon: no pathway is set up, and there is no terminal to ask on. Run `axon login --sky-endpoint <address>` for a SkyPilot API server you reach directly, `axon login --teleport-proxy <address>` for one through Teleport, or `axon login --config <source>` with the file your organization gives you.
```

No pathway is stored on this laptop, and `axon login` ran where it cannot ask,
such as in a script. When one was in use, `axon login` has already disconnected
it, stopping the Teleport tunnel and forgetting the logged-in email. Run
`axon login` in a terminal to answer its questions, or give the answers as the
flags the message names. See [Logging in](/axon/logging-in/#the-first-login).

### The server does not answer when you make a pathway

```text
Can't reach the SkyPilot API server at <address>: <reason>. Check the address and try again.
<address> answered, but not as a SkyPilot API server (<reason>). Check the address and try again.
Can't reach the Teleport proxy at <proxy>: <reason>. Check the address and try again.
```

The reason is the short one the network gives, such as `no such host`, which
means the name does not exist, `connection refused`, which means nothing
answers on that port, or `timed out`. For example:

```text
Can't reach the SkyPilot API server at https://sky.wrong.example: no such host. Check the address and try again.
```

`axon` checks the address before it stores the pathway, and asks for it again.
Check it for typos, and that you are on a network the server answers on. The
second form means something else answers at that address: ask your organization
for the SkyPilot API server's address. With `--sky-endpoint` or
`--teleport-proxy` the same message stops the login, ending with "run `axon login`
again".

### A pathway you add clashes with a stored one

```text
axon: add the config file: not added, since its pathway "<name>" has the same name as a stored pathway in <path>. Run the command again with --replace to replace that file.
axon: add the config file: its pathway "<name>" clashes with the stored pathway "<name>" in <path>, and there is no terminal to ask whether to replace it. Run the command again with --replace to replace it.
axon: make the pathway: it clashes with the stored pathway "<name>" in <path>, and there is no terminal to ask whether to replace it. Run the command again with --replace to replace it.
```

The first means you answered no to replacing a stored pathway of the same name.
The others mean there was no terminal to ask on. Add `--replace` to replace what
is stored, as [Replacing a stored pathway](/axon/logging-in/#replacing-a-stored-pathway)
describes, or leave things as they are.

### An older revision of a config file

```text
axon: the config file at <source> is revision <n>, older than the stored revision <m>. Using the stored file.
```

The file you added, or the one fetched at login, is older than the copy on your
laptop, and there was no terminal to ask whether to replace it. `axon` keeps the
copy it has. Tell your organization when its published file is the older one.

### A refused config file

A file that fails a check is refused whole and nothing changes. The message names the
field and the check, and begins `axon: add the config file:`. The checks are
on [Writing a config file](/axon/config-file/#what-axon-checks). Tell
your organization which field it names. Messages seen most:

| Message | What it means |
|---|---|
| `<source> is empty.` | The file, standard input or the address gave nothing, or only blank lines and comments. With `--config -`, check that the file reached standard input. |
| `format: must be 2` | The file's `format` is missing or is a number `axon` does not know. |
| `id: must be lowercase letters, digits and hyphens, start with a letter, and be at most 32 characters` | The file's `id` is not of the allowed form. |
| `pathway: the file must have at least one [[pathway]]` | The file holds no pathway. |
| `pathway "<id>": name: is required and must be text` | The pathway has no `name` (or no `description`, which gives the same message for that field). |
| `pathway "<id>": kind: must be "direct" or "teleport"` | The pathway's `kind` is missing or unknown. |
| `pathway "<id>": sky.endpoint: must be an https address, without a user name, password, query string or fragment` | A direct pathway's address is missing, does not start with `https://`, or carries a user name, a password, a query string such as `?token=...`, or a fragment. |
| `pathway "<id>": <field>: must not hold control characters or Unicode format characters` | The field holds a character that can act on a terminal: a control character, such as a line break, a tab or an escape, or a Unicode format character, such as a bidirectional override or a zero-width space. It is often pasted in unseen. The field is one of `name`, `description`, `unreachable_hint`, `sky.app` and `sky.endpoint`. |
| `pathway "<id>": sky.app: must be a Teleport application name: not starting with "-", and without white space` | A Teleport pathway's application name starts with `-` or holds a space. |
| `pathway "<id>": teleport.proxy: must be a hostname, optionally with a port` | A Teleport pathway's proxy is missing or is not a host. |
| `"<field>": not a field of a config file` | The file holds a field that the format does not have, such as a misspelling. The field is quoted, and a character in it that can act on a terminal is shown escaped, such as `\x1b`. |
| `revision: must be a whole number, 0 or more` | The file's `revision` is not a whole number. |
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

`--config` takes an `https` address. Use the address your organization gave you, which
starts with `https://`.

### The address carries a user name or password

```text
axon: add the config file: <address>: an address must not carry a user name or password
```

An address is fetched without credentials. Remove them from the address. When your
organization protects the file, ask for it as a file and use `--config <path>`.

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
answers with an error is your organization's to fix.

Every message that names a config file's address prints its scheme, host and path
only, leaving out any query string, fragment or user name, which can hold a token.
The full address is still the one `axon` fetches and records.

### A stored config file is left out

```text
axon: left out <path>: <reason>
```

A stored config file fails the checks, so `axon` leaves it out of this login.
The reason is one of the refusals above. Ask your organization for a new file and add
it with `axon login --config <source>`, or delete the stored file from `~/.axon/configs/`.

### The config file could not be updated

```text
axon: could not update <path> from <address>: <reason>. Using the stored file.
axon: could not update <path>: <address> is empty. Using the stored file.
axon: could not update <path>: <reason>. Using the stored file.
```

At login, `axon` fetches again the file that holds your pathway, when it came from an
address. The first form means the fetch failed, the new file was refused, or it has a
pathway named like one in another stored file; the second means the address answered
with an empty file; the third
means `axon` could not read what it recorded about the file's source. Either way `axon`
carries on with the file it has. Nothing needs doing now; when the reason is a refusal,
tell your organization.

### The config file at the address moves your pathway

```text
The config file at <address> changes where the pathway "<id>" goes:
  now: <kind>, <where>
  new: <kind>, <where>
Switch to the fetched config file? [y/N, Enter for no]:
axon: the config file at <address> changes where the pathway "<id>" goes, and there is no terminal to ask whether to switch. Using the stored file. Run `axon login --replace` to switch.
```

At login, the file fetched again from its address gives your pathway another
kind, address, Teleport proxy or application than the stored copy, or, with
`new: no longer in the file`, drops the pathway. `axon` asks before it stores
the file; no, the default, keeps the stored copy and logs in with it, and says
`Kept the stored copy of <path>.`. The last line means there was no terminal
to ask on.

When your organization told you its server moved, answer yes, or run
`axon login --replace`. When it did not, answer no and ask your organization:
a file at the address that sends you to an unexpected server is a sign that
someone else changed it.

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

Your organization changed its file, and the pathway you used is not in it. One pathway
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
browser. Your organization's hint, when its file gives one, follows the message and
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
`axon login` again. When it repeats, tell your organization.

### SkyPilot's cookie file could not be removed

```text
remove the cookies of <endpoint> from <path>: <reason>. Remove that file by hand and run `axon login` again.
```

`axon` could not remove the API server's cookies from SkyPilot's cookie file,
`~/.sky/cookies.txt`, or the file `SKYPILOT_API_COOKIE_FILE` names. Remove the
file yourself, then log in again.

### The SkyPilot login failed

```text
the SkyPilot login failed (<reason>). Run `axon login` again.
```

SkyPilot's login stopped with an error. Read the reason, and run `axon login` again.

## Signing in on a Teleport pathway

### tsh does not match the Teleport version

```text
tsh <version> at <path> does not work with the Teleport proxy <proxy>, which runs <version> and supports tsh <n> and <n-1>. Update tsh: <link>
tsh <version> at <path> is newer than the Teleport proxy <proxy> supports: it runs <version> and accepts tsh <n> or <n-1>. Install an older tsh: <link>
```

The `tsh` on your `PATH` is too old, the first form, or too new, the second, for
your organization's Teleport. `axon` never replaces a `tsh` you installed. Install
a `tsh` whose major version is the proxy's, `<n>`, or one behind it, `<n-1>`, from
the link; when yours is too new, that means an older one.

### The Teleport proxy does not answer

```text
axon: establish the Teleport session: can't reach the Teleport proxy at <proxy>: <reason>. Check your network connection and run `axon login` again.
```

The pathway's Teleport proxy gave no answer. The reason is the short one the network
gives, such as `no such host`, `connection refused` or `timed out`. When the pathway's
config file has an `unreachable_hint`, that sentence takes the place of "Check your
network connection and run `axon login` again." and says what to do, such as connecting
to a VPN.

### tsh is not on PATH

```text
axon: establish the Teleport session: this pathway needs Teleport's client, tsh, and no tsh was found on PATH. Install it from https://goteleport.com/docs/installation/ and run `axon login` again.
axon: this pathway needs Teleport's client, tsh, and no tsh was found on PATH. Install it from https://goteleport.com/docs/installation/ and run `axon login` again.
```

A Teleport pathway needs `tsh` on your `PATH`, and `axon` does not install it. Install
it from the link, then run `axon login`. The second form comes from `axon sky` when it
needs `tsh` to restart the tunnel.

### The Teleport sign-in failed

```text
Teleport login (tsh login --proxy=<proxy>): <reason>
```

The Teleport sign-in failed or was not completed. Run `axon login` again and finish the
sign-in in the browser.

### Another process holds the tunnel's port

```text
port 46581 is held by process <pid> (<command>), which is not the Teleport tunnel. Stop that process and run `axon login` again.
axon: port 46581 is held by process <pid> (<command>), which is not the Teleport tunnel. Stop that process and retry.
```

Another program uses the tunnel's port. `axon login` and every `axon sky` command check
the port first, and stop rather than send your requests to that program. Stop it, then
run the command again. A tunnel started by an older `axon` is also such a process.

### The Teleport tunnel could not be stopped

```text
stop the Teleport tunnel: process <pid> did not exit. Stop it with `kill -9 <pid>` and try again
```

`axon` could not stop its own tunnel. The message begins with the command that tried,
such as `axon: could not` from `axon logout`. Run the `kill` command it gives, then run
the command again.

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

### The server did not recognize your Teleport identity

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

### The download of uv stopped

```text
install the SkyPilot client: download uv <version>: the download stopped because no data arrived for 30 seconds. Check your network and run `axon login` again.
```

`axon` downloads its own `uv` before it installs the client, and ends the
download when no data arrives for 30 seconds. A slow download that keeps moving
is not cut off. Check your network, then run `axon login` again; the download
starts over.

### The download of uv was too large

```text
install the SkyPilot client: download uv <version>: the download was refused because it is larger than 64 MiB. Check your network and run `axon login` again.
```

`uv`'s archive is far smaller than that, so what answered was not the archive,
which happens behind a proxy or captive portal that answers in its place.
Check your network, then run `axon login` again.

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
help; the person who sets `axon` up for your organization can tell whether the server's
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
organization's hint, when the message has one.

### The API server is not answering

```text
axon: The SkyPilot API server is not answering. Retry later.
```

The server itself is down, or on a Teleport pathway does not answer after a restart of
the tunnel. Logging in again does not help. Retry later.

### The Teleport tunnel could not be checked

```text
axon: Couldn't check the Teleport tunnel: <reason>. Run `axon login`.
```

`axon sky` could not find out which process holds the tunnel's port, so it did not run
the command. Run `axon login`; when the message repeats, the reason says what failed.

### The Teleport tunnel could not be restarted

```text
axon: Couldn't restart the Teleport tunnel. Run `axon login`.
```

`axon sky` tried to restart a stopped tunnel, and the tunnel's process exited. The tail
of the tunnel's log follows the message. Run `axon login`, which starts a fresh tunnel.

### The cluster is in another workspace

```text
The cluster '<cluster>' is in workspace '<a>', but the active workspace is '<b>'.
```

This message is SkyPilot's. The cluster was launched in workspace `<a>`, and the
command ran in your active workspace, `<b>`. Run the command again in the cluster's
workspace, for example:

```sh
axon sky down -y --config active_workspace=<a> <cluster>
```

`axon sky launch`, `status` and `down` all take `--config active_workspace=<workspace>`.
SkyPilot may suggest `--purge`; that is not the fix. It ignores errors and can
remove the cluster from SkyPilot's records while its resources keep running.

### The SkyPilot client is missing

```text
axon: the SkyPilot client is not installed. Run `axon login` first.
```

The SkyPilot client that `axon` keeps in `~/.axon/tools/` is missing. Run `axon login`,
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

## Logging out

### A step of the logout failed

```text
axon: could not log out of SkyPilot: <reason>
axon: could not remove the saved SkyPilot login: <reason>
axon: could not stop the Teleport tunnel: <reason>
axon: could not log out of Teleport: <reason>
axon: could not log out of Teleport: the Teleport session is still valid after tsh logout. Run `tsh logout --proxy=<proxy> --user=<user>` for each user tsh lists
axon: could not remove the records of the connection: <reason>
axon: logout did not finish. Run `axon logout` again.
```

One step of `axon logout` failed, and the others ran. `axon` keeps its record of the
connection, so running `axon logout` again retries the step. The reason is the step's
own error, such as the output of `sky api logout` or `tsh logout`.
When `axon` could not remove the saved SkyPilot login, the reason names SkyPilot's
cookie file, `~/.sky/cookies.txt` or the file `SKYPILOT_API_COOKIE_FILE` names;
check that you can write to its directory.

## Interrupting axon

### axon was interrupted

```text
axon: interrupted
```

You pressed Ctrl-C, or something sent `axon` SIGTERM, and `axon` stopped the
command. What the command had not finished is undone: a download leaves no
file behind, `axon update` leaves the `axon` you ran in place, the SkyPilot
client stays at the version it was, a stored config file is as it was or as
the command was making it, and a Teleport tunnel that the login started
either runs or does not. A program that `axon` was running, such as the
browser login or Teleport's login, received your Ctrl-C from the terminal
too, or SIGTERM from `axon`, and `axon` waited for it to end. Run the command again when you want it.

Pressing Ctrl-C a second time while `axon` stops ends it at once, and can
leave a temporary file whose name begins with a dot beside the file `axon`
was writing, such as `.axon-update-…` beside `axon` itself. It does nothing,
and you can delete it.
