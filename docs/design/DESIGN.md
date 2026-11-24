# Design

> This document is the single authority for what `axon` is meant to be. It
> describes the desired state, and reads true whether or not a given piece
> is built or released.
>
> `{Q?}` marks an open question or a provisional choice; the linked file
> gives the options and the recommendation. `{D!}` marks a decision; the
> linked file gives the rationale and the alternatives. Anything this
> document does not state is unconstrained. The rules are in
> [CONTRIBUTING.md](../../CONTRIBUTING.md).

## Purpose

`axon` is the command-line entry point to a platform run by an
organization. A user installs one tool, logs in once, and reaches each part
of the platform through a command group.

A command group that passes a tool's own commands straight through is
named for the tool. A command group that hides the tool behind commands of
its own is named for the part of the platform it reaches, and what sits
behind it is free to change without the user learning a new path
[{D!}](decisions/command-group-names.md).

`axon` has one command group, `sky`, which passes SkyPilot's `sky` commands
through to the organization's SkyPilot API server: dev environments, GPUs,
jobs and volumes.

`axon` is written in Go and is a single binary that runs on the user's
laptop. It has no server component.

One `axon` serves any organization whose pathways are of the kinds below.
What makes it serve an organization is a pathway: one that `axon login`
makes from the server's address, or the pathways of a config file that the
organization supplies.

## Organizations

### Organization details

An organization's details are everything that names its deployment:

- the hostnames and addresses of its servers, including the SkyPilot API
  server;
- its Teleport proxy and the names of its Teleport applications;
- the identifiers, names and descriptions of its pathways;
- the names of its login systems and identity providers;
- the names of its networks and VPNs;
- the names of its clusters;
- its own name;
- the lifetimes its systems set, such as how long a Teleport certificate
  lasts.

The repository holds none of them. No file in it names one: not the code,
the tests, the design documents or the readme, and no commit message. An
example that needs a detail uses a made-up one under `example.org`.

`axon` takes every organization detail from a config file or from the
user's answers at `axon login`, and has no default for any of them. The name of the GitHub organization that hosts
`axon`'s own repository is not an organization detail in this sense: it
names where `axon` comes from, not whom it serves.

### Pathway kinds

A pathway is a way to reach the organization's API server. Every pathway is
one of two kinds [{D!}](decisions/pathway-kinds.md).

| Kind | Login | Reaches the API server |
|---|---|---|
| `direct` | The API server's own login, in a browser | At its address, from wherever the organization's network allows |
| `teleport` | Teleport, in a browser when the certificate is not valid | Through a local tunnel to a Teleport application |

The rest of this document describes each pathway by its kind. An
organization names its own pathways, any number of them of either kind.

### Config files

An organization's configuration is a TOML file, its config file. A
laptop holds any number of them, from one organization or several, and the
user moves between the pathways they hold
[{D!}](decisions/config-files.md). An organization that gives each
pathway its own file lets most of its members hold one file and never meet
a prompt, while a member on several pathways holds several files.

These are two complete files, one pathway each:

```toml
# campus.toml
format = 2
id = "example-campus"

[[pathway]]
id = "campus"
kind = "direct"
name = "Campus"
description = "Single sign-on in a browser. Needs the campus network or VPN."
unreachable_hint = "Connect to the campus VPN and retry."

[pathway.sky]
endpoint = "https://sky.example.org"
```

```toml
# remote.toml
format = 2
id = "example-remote"

[[pathway]]
id = "remote"
kind = "teleport"
name = "Remote"
description = "Teleport. Works from anywhere."

[pathway.teleport]
proxy = "teleport.example.org"

[pathway.sky]
app = "skypilot"
```

A file holds one or more pathways, each a `[[pathway]]` entry. In TOML a
table such as `[pathway.sky]` belongs to the `[[pathway]]` above it, so
each pathway's tables follow its own `[[pathway]]` line and come before the
next one.

| Field | Type | Holds |
|---|---|---|
| `format` | `2` | The file's format. |
| `id` | Lowercase letters, digits and hyphens, starting with a letter, at most 32 characters | The file's identifier, which names the stored file. |
| `revision` | A whole number, 0 or more, optional | The file's revision. An organization raises it each time it changes the file, and keeps the file's `id`. |

Every other field belongs to a pathway. Every pathway has the same three
parts, whatever its kind [{D!}](decisions/pathway-contract.md):

- **Identity**: what the pathway is.
- **Text for people**: labels and sentences that the organization writes and
  `axon` prints as written. `axon` never builds a sentence out of a
  configuration value.
- **Settings for machines**: a table named for the kind, holding what only
  that kind needs, and a table for each command group, holding where that
  group finds its service on this pathway. A kind with no settings of its
  own has no kind table.

| Field | Part | Type | Holds |
|---|---|---|---|
| `id` | Identity | Lowercase letters, digits and hyphens | The value of `--pathway`. |
| `kind` | Identity | `direct` or `teleport` | The pathway's kind. |
| `name` | Text | Free text, required | The pathway's name, as the prompt shows it. |
| `description` | Text | Free text, required | The prompt's line about the pathway, which is where the organization names its login system and where the pathway works. |
| `unreachable_hint` | Text | Free text, optional | A sentence `axon` prints after "Can't reach `<host>`." when a host the pathway names does not answer. |
| `teleport.proxy` | Kind table, `teleport` | Hostname, optionally with a port | The Teleport proxy. |
| `sky.endpoint` | `sky` table, `direct` | `https` address | The SkyPilot API server. |
| `sky.app` | `sky` table, `teleport` | Teleport application name | The Teleport application that serves the SkyPilot API server. |

`axon` accepts a file only when:

- `format` is `2`;
- it has an `id` of the allowed form;
- it has at least one pathway;
- every pathway has an `id` of the allowed characters, a `kind` of `direct`
  or `teleport`, a `name` and a `description`;
- every `direct` pathway has `sky.endpoint`, and it is an `https`
  address;
- every `teleport` pathway has `teleport.proxy`, a hostname, optionally
  with a port, and `sky.app`;
- `revision`, when present, is a whole number, 0 or more;
- it has no field or table that this section does not list for the file or
  for the pathway's kind;
- no pathway identifier appears twice in it.

A file of format `1`, the format of an earlier `axon`, is refused, whether it
is given with `--config` or already stored, with: "the file is in an older
format. Run `axon login --config <source>` again with your organization's
current config file." `axon` does not convert it.

A file that fails a check is refused whole, with a message that names the
field and the check. A refused file changes nothing: every stored file
stays as it was.

The config files are trusted input: they decide which servers the
user's logins and requests go to. Apart from the hosts the active pathway
names and the address its file is fetched from, `axon` contacts only the
download sources of the tools it installs for its own use, and its own
releases when the user runs `axon update`.

### Adding a config file

`axon login --config <source>` takes a file from a source, checks it, stores
it, and then logs in [{D!}](decisions/config-source.md). The
source is one of three:

| Source | Example |
|---|---|
| An `https` address | `axon login --config https://example.org/axon.toml` |
| A file path | `axon login --config ~/Downloads/axon.toml` |
| `-`, standard input | `<command that prints the file> \| axon login --config -` |

An address is fetched without credentials, so it suits an organization that
publishes its file. A file path or standard input suits one that hands the
file out by other means; with standard input, an organization's setup is
one line that fetches the file with a tool its members are already signed
in to. An address with a scheme other than `https` is refused.

With `--config -`, standard input is the file, so `axon login` asks its
questions on the terminal. When there is no terminal, a question that has a
flag, such as the pathway, is answered by the flag, and `axon` stops and
names the flag when it is missing. A question with no flag is answered
"no".

`axon` stores the file as `~/.axon/orgs/<id>.toml`, named by the file's
`id`. Beside it, in `~/.axon/state/`, it records the file's source, when the
file was stored, and the SHA-256 of its bytes. Adding a file leaves the
other stored files as they are, apart from the ones the user agrees to
replace. When it stores a file that has a `revision`, `axon` says the
revision.

Removing a file is deleting it from `~/.axon/orgs/`. There is no command
for it.

### Replacing a stored pathway

`axon` asks before an addition overwrites what is stored, and never refuses
an addition outright [{D!}](decisions/stored-file-clashes.md).

An addition of a config file whose `id` is that of a stored config file is
an update. It replaces the stored copy without a question, and the line
that reports it names the old and the new revision, unless both have a
`revision` and the new one is lower. Then `axon` asks whether to replace
the stored copy, naming both revisions. Answering no keeps the stored copy,
and the login goes on with it.

An addition, of a config file or of a hand-made pathway, clashes with a
stored pathway in another file when the two have the same name, ignoring
case, the same identifier, or the same server: the same API server address
on a `direct` pathway, or the same Teleport proxy and application on a
`teleport` pathway. For each stored file a clash touches, `axon` asks
whether to replace it, and says what replacing removes: a hand-made
pathway, or a config file whole, with the names of all its pathways. The
default answer is no.

- Yes removes what the question named, its record included, and stores
  the addition. When the active pathway is in what is removed, `axon` first
  leaves it, as a switch of pathway does.
- No to a clash of name or identifier: a config file is not added, and
  `axon` says so; for a hand-made pathway, `axon` asks for another name.
- No to a clash of server alone keeps both.

`axon login --replace` answers yes to every one of these questions in
advance. With no terminal to ask on, a clash stops the login with a message
that names `--replace`, and a lower revision keeps the stored copy, with
one line that says so.

A hand-made file and a config file never update each other: two such files
with the same `id` are a clash.

A file being removed is renamed with the suffix `.removing` and deleted once
the addition is stored. When `axon` stops in between, such a file is left in
`~/.axon/orgs/` or `~/.axon/state/orgs/`; `axon` never reads it, and it is
safe to delete.

A stored config file that an update replaces is first kept beside itself
with the suffix `.previous`, as a hard link or as a copy written to disk,
and stays in place until the new file replaces it in one step. When the
update fails, the copy is renamed back over the stored file; once the new
file and its record are stored, the copy is deleted. When `axon` stops in
between, the next `axon login` decides by the checksum in the record:

- The record matches the stored file: the copy is deleted.
- The record matches the copy and not the stored file: the copy is put
  back, and `axon` says so in one line.
- The stored file is missing: the copy is put back, and `axon` says so in
  one line.
- Otherwise, as with a record that holds no checksum: the stored file is
  kept and the copy is deleted.

### Hand-made pathways

A user without a config file makes a pathway at `axon login`
[{D!}](decisions/pathway-without-config-file.md). With no pathway stored,
no `--config` and a terminal, `axon login` asks, in order:

1. the kind: directly at the server's address, or through Teleport;
2. for `direct`, the SkyPilot API server's address; for `teleport`, the
   Teleport proxy's address and then the Teleport application, whose
   default is `skypilot`;
3. a name for the pathway, whose default is the server's host, or the
   proxy.

Every question with a default shows it and says that Enter accepts it. An
address is accepted with or without `https://`; `axon` adds it to the API
server's address and drops it from the proxy's.

Flags give the same answers without a question, whether or not a pathway
is stored: `--sky-endpoint <address>` for `direct`; `--teleport-proxy
<address>` with `--sky-app <name>` for `teleport`; and `--name <name>`.
Flags that contradict each other, such as `--sky-endpoint` with
`--teleport-proxy`, and any of them with `--config` or `--pathway`, are
refused with a message that names them.

Nothing is stored until the pathway's server answers: the SkyPilot API
server's health endpoint, logged in or not, for `direct`, and the Teleport
proxy's version endpoint for `teleport`. At the prompt, an address that
does not answer is reported and asked for again. With flags, `axon login`
stops with an error.

The pathway is then stored as a format 2 config file that `axon` writes
into `~/.axon/orgs/`, with one pathway and no `revision`, and its record
says that it is hand-made. The pathway's identifier is its name in
lowercase, with each run of other characters than letters and digits made
one hyphen. The file's `id` is that identifier, made to fit the form of a
file `id` and kept apart from the stored files' ids. Its `description`
says that the pathway was made on the laptop and where it goes, and it has
no `unreachable_hint`. Adding the pathway makes it the active one. From
then on it is a pathway like any other: `--pathway`, switching, `axon
login` and `axon logout` treat it alike. A hand-made file has no source to
fetch again.

### Updating a config file

At each `axon login`, once the pathway is determined and before its session
is established, the file that holds it is fetched again, when its source is
an address and `--config` did not add it in the same login. The fetched file
replaces the stored one when it passes the checks and its `id` is the stored
file's [{D!}](decisions/config-updates.md). When the fetch
fails or the file is refused, `axon` says so in one line and continues with
the stored file. No other stored file is fetched.

When the fetched file no longer has the pathway, the pathway is left. If
`--pathway` named it, `axon login` stops, says that the pathway is no
longer in its config file, and lists the identifiers the stored files
have. Otherwise `axon login` determines a pathway again from the stored
files, with no further fetch in that login, and uses the stored copy of the
newly chosen pathway's file. When it chooses without asking, because one
pathway remains, it says in one line which pathway it now uses.

The fetched file follows the rule for an update: a lower revision asks
first, and with no terminal the stored copy is kept with one line that says
so. A fetched file whose pathway newly has the name or identifier of a
pathway in another stored file is not asked about: like a refused file, it
is left out with one line, and the login uses the stored copy.

A file from a file path or standard input is read once, when `--config` is
given. Updating it is running `axon login --config` again with the new file.

`axon sky` never fetches a file. It uses the stored ones.

At each `axon login`, `axon` reads every stored file and applies the checks
above. A stored file that fails them is left out, with one line that names
it.

### No config file

`axon --version` and `axon --help` work without a config file.

When no file is stored and neither `--config` nor the flags of a hand-made
pathway are given, `axon login` asks for a hand-made pathway on a terminal.
With no terminal, it stops with a message that names `--sky-endpoint`,
`--teleport-proxy` and `--config`.

`axon sky` with no file stops with: "Run `axon login` first."

## Commands

| Command | Behavior |
|---|---|
| `axon login [--config <source>] [--pathway <id>] [--sky-endpoint <address> \| --teleport-proxy <address> [--sky-app <name>]] [--name <name>] [--replace]` | Adds a config file when `--config` is given, or makes a hand-made pathway, determines the pathway, establishes its session, and connects every command group. |
| `axon logout` | Logs out of every command group and the pathway's session, and removes the records of the connection. |
| `axon sky [args]` | Checks the connection, then runs `axon`'s own SkyPilot client with the same arguments. |
| `axon update [--version <version>]` | Replaces the running `axon` with the latest release, or with the release `--version` names. |
| `axon --version` | Prints the version of `axon`. |
| `axon --help` | Lists the commands. |

These are all of `axon`'s commands.

## Files and directories

`axon` keeps everything it owns on the laptop in one directory, `~/.axon`
[{D!}](decisions/file-layout.md).

| Path | Holds |
|---|---|
| `~/.axon/orgs/` | The config files, one per file added, each `<id>.toml` as its source gave it, and the hand-made files `axon` writes. |
| `~/.axon/config.toml` | The active pathway, by its identifier. |
| `~/.axon/tools/` | What `axon` installs for its own use. |
| `~/.axon/state/` | What `axon` records about running state, such as each config file's record (its source or that it is hand-made, when it was stored, and its SHA-256), the settings the pathway had when it was connected, the tunnel's process, and the logged-in email. |

`axon` writes a file in `~/.axon/orgs/` only by replacing it whole, and
never writes the active pathway into it.

Removing `~/.axon` removes everything `axon` installed for its own use and
everything it recorded, the config files included. The next `axon login`
starts with no pathway. Two things are outside the directory and remain: Teleport's
own profile and certificates in `~/.tsh`, which `tsh` writes, and a tunnel
that is running. The next login adopts that tunnel only when its command
line is exactly the active pathway's tunnel command, its proxy and
application included; a tunnel started in another form is reported as
holding the port.

The `axon` binary is not in this directory; it is in `~/.local/bin`.

## axon login

`axon login` is the one login command. It runs these stages in order and
stops at the first that fails:

1. Add the config file that `--config` gives, or make the hand-made
   pathway the flags give, when they are given.
2. Determine the pathway, after making a hand-made one at the prompt when
   none is stored.
3. Refresh the pathway's file, as "Updating a config file" sets out.
4. Establish the pathway's session.
5. Connect each command group, by that group's own steps
   [{Q?}](questions/command-group-login.md).

`axon login` is idempotent. Running it when everything is in place changes
nothing and reports each connection as good.

### Determining the pathway

One pathway is active at a time, chosen from the pathways of every stored
config file. `axon` stores the active pathway's identifier in
`~/.axon/config.toml` [{D!}](decisions/pathway-selection.md), and later
logins use it without asking.

When there is no active pathway, `axon login` chooses one. With one
pathway in all the stored files, it uses that pathway without asking. With
more, it asks which one the user is on, offering every pathway by its name
with its description, file by file in the order of the files' identifiers
and, within a file, in the file's order.

`--pathway` takes a pathway's identifier from any stored file, skips the
prompt, and makes that pathway the active one. It is how a user switches.
It works the same with a single pathway: its identifier is accepted, and
any other identifier is refused with a message that lists the identifiers
the stored files have.

`--config` without `--pathway` makes a pathway of the added file the active
one, unless the active pathway is already in that file: with one pathway in
the file, that pathway, and with several, the prompt offers the file's
pathways. A user who adds a file means to use it.

A user also leaves the active pathway when the stored files change under
it:

- its file is replaced, by `--config` or by a fetch from an address, and the
  new file does not have the pathway or has different settings for
  machines for it;
- its file has been deleted from `~/.axon/orgs/`, or fails the checks when
  it is read.

A change to the pathway's text for people alone, its `name`, `description`
or `unreachable_hint`, is not leaving it and disconnects nothing. When the
pathway is still in its file with different settings for machines, `axon`
disconnects it from the old settings and the login connects it with the new
ones, and the pathway stays active. When the pathway is no longer in any
stored file, there is no active pathway, and `axon login` chooses one as
above.

Leaving a pathway, by `--pathway` or by a change to the stored files,
disconnects each command group from the pathway that is left: it stops the
tunnel and removes the logged-in email.
Disconnecting uses only what `axon` recorded when it connected, never an
config file, so it works when the file that named the pathway is
gone.

### The pathway's session

On a `direct` pathway there is no session apart from each service's own: a
service that needs a login opens it in a browser when `axon` connects to
that service.

On a `teleport` pathway the session is the Teleport login. `axon` finds
`tsh` on `PATH`, then checks the Teleport certificate for the pathway's
proxy. When the certificate is not valid, `axon` runs
`tsh login --proxy=<teleport.proxy>`, which opens a browser. A valid
certificate needs no login and opens no browser. The certificate lasts as
long as the organization's Teleport sets. The user runs no `tsh` command by
hand.

### tsh

`tsh` is a prerequisite of a `teleport` pathway. `axon` uses the `tsh` on
`PATH`, and never installs or replaces one [{D!}](decisions/tsh-install.md).
When no `tsh` is on `PATH`, `axon login` stops before any login step, and
before it leaves the pathway that was connected, with:
"this pathway needs Teleport's client, tsh, and no tsh was found on PATH.
Install it from https://goteleport.com/docs/installation/ and run `axon
login` again." A `tsh` that an earlier `axon` kept in `~/.axon/tools/` is
neither used nor removed.

Teleport supports a client whose major version is the proxy's or one behind
it. When the `tsh` on `PATH` is outside that range, `axon` stops, names the
installed version and the proxy's, and says to update `tsh`.

## axon logout

`axon logout` undoes what `axon login` did, by `axon`'s own records alone,
never by a config file, so it works when the file that named the pathway is
gone. It runs these steps in order:

1. Log the SkyPilot client out, by running `sky api logout` with `axon`'s
   own client and with `SKYPILOT_API_SERVER_ENDPOINT` unset. When the client
   is not installed, or stores no endpoint, the step is skipped; `axon
   logout` never installs the client.
2. When the recorded pathway has an API server endpoint, as a `direct`
   pathway does, remove from SkyPilot's cookie file every cookie for that
   endpoint: each cookie whose domain matches its host and whose path
   covers its path or lies below it. `sky api logout` leaves these
   cookies, and a saved cookie logs the next `axon login` in without the
   browser. Every other line of the file is kept as it was. The step runs
   whatever step 1's result, and when the client is not installed. With
   no cookie file, or no cookie for the endpoint, it is silent.
3. Stop the recorded Teleport tunnel, when it runs.
4. When the recorded pathway is a `teleport` pathway, end the Teleport
   session for its proxy with `tsh logout`
   [{D!}](decisions/logout.md), and check that the certificate is no
   longer valid. When `tsh logout` leaves it valid, as with several
   identities on the proxy, the step fails and names the `tsh logout`
   command to run. When no `tsh` is on `PATH`, the step is skipped.
5. Remove the record of the connected pathway and the logged-in email.

It keeps the config files and the active pathway, so `axon login` with no
flags logs in again. It prints one line for each step it took or skipped,
except the silent cases of step 2, and one line saying that the config
files were kept. With nothing logged in
it says so and exits with success.

A step that fails is reported, and the later steps still run. The records
of the connection are then kept, so that running `axon logout` again
retries, and `axon logout` exits with an error.

A record of the connected pathway that an earlier `axon` wrote is a
connection to log out of: its tunnel is stopped by the tunnel's own record,
the Teleport step is skipped with a line that names `tsh logout`, and the
record is removed.

On a `teleport` pathway, Teleport keeps its session in `~/.tsh`; when the
Teleport step was skipped, `tsh logout` ends it.

Clearing a shared machine completely is deleting `~/.axon` and `~/.sky`
after `axon logout`.

## sky

The `sky` command group is the user's way to the organization's SkyPilot
API server. It passes SkyPilot's own commands through. The parts of `axon`
that are specific to SkyPilot are kept together, inside `sky`. Everything
`axon` does with SkyPilot is in this section.

Each line `axon` prints about the `sky` group's progress, during `axon
login` and before a command runs, begins `sky:`.

| Pathway kind | Endpoint the SkyPilot client uses |
|---|---|
| `direct` | The pathway's `sky.endpoint` |
| `teleport` | `http://localhost:46581`, a Teleport tunnel to the pathway's `sky.app` |

The tunnel's port, 46581, is `axon`'s and is the same for every
organization.

SkyPilot's own configuration and session stay in `~/.sky/`, where SkyPilot
keeps them.

### What login does for sky

When `axon login` connects `sky`, it runs these steps in order:

1. Make sure the SkyPilot client is installed.
2. Connect and log the client in, by the pathway kind's steps below.
3. Verify that the server reports the user's identity, and record the
   logged-in email.
4. Sync the client version with the server's.

On a teleport pathway, steps 3 and 4 run before that kind's step 6.
Teleport supplies the identity through the tunnel, so neither needs
`sky api login`, and the client that runs `sky api login` is at the server's
version.

#### On a direct pathway

1. `axon` checks that the endpoint is reachable. When it is not, `axon`
   says "Can't reach `<host>`.", followed by the pathway's
   `unreachable_hint` when it has one, and stops without opening a
   browser.
2. When the existing session is good, the remaining steps are skipped.
3. `axon` runs `sky api login -e <sky.endpoint>`, which opens the
   server's login page in a browser and waits for it to complete.
4. When the login ends with the status `needs_auth`, `axon` removes
   `~/.sky/cookies.txt` and runs the login once more. The file holds only
   cookies for the API server.

`sky api login` opens the login page in the user's default browser, where
the user signs in and clicks "Authorize". `axon` chooses no browser of its
own [{D!}](decisions/direct-browser-login.md). It waits up to five minutes
for the login to complete; when the wait runs out, `axon` says that the login
was not completed in the browser and to run `axon login` again.

#### On a teleport pathway

1. When the tunnel is up and the server reports the user's identity through
   it, the tunnel is left running and steps 2 to 5 are skipped. A repeated
   login does not interrupt `ssh` or VS Code sessions.
2. `axon` runs `tsh --proxy=<teleport.proxy> apps login <sky.app>`,
   which issues a fresh application certificate.
3. `axon` stops its tunnel, if one is running.
4. `axon` confirms that port 46581 is free. When a process that is not its
   tunnel holds the port, `axon` reports the process, names the port, and
   stops.
5. `axon` starts the tunnel and waits up to 10 seconds for
   `http://localhost:46581/api/health` to answer. When it does not and the
   tunnel's process has exited, `axon` prints the end of the tunnel's log
   and stops. When the process is still running, `axon` reports the
   SkyPilot API server as not answering through the tunnel, says to retry
   later, and stops.
6. `axon` runs `sky api login -e http://localhost:46581` when the client's
   stored endpoint differs. No browser opens: the identity comes from
   Teleport.

The port is 46581 and not 46580, because `http://localhost:46580` is the
address SkyPilot reserves for an API server on the laptop itself. With that
endpoint the client skips uploading `workdir` and `file_mounts`, and starts
a local server when nothing answers.

### The Teleport tunnel

The tunnel is the process
`tsh --proxy=<teleport.proxy> proxy app <sky.app> --port 46581`. `axon`
starts it detached from the terminal, records its process ID and its command
line, and sends its output to a log file
[{D!}](decisions/tunnel-lifecycle.md). The record and the log are kept in
`~/.axon/state/`. The tunnel outlives the `axon` command and the terminal
that started it.

The tunnel is up when the health endpoint answers through it. A process that
exists and does not serve is down.

`axon` treats a process as its tunnel when the process belongs to the user
and its command line is exactly the tunnel's command, the pathway's
`teleport.proxy` and `sky.app` included, so a tunnel to another proxy is
never taken for this pathway's, even when the application names match.
Before it stops the process whose ID it recorded, `axon` confirms that the
process's command line is the one it recorded; when it does not match, the
ID has passed to another process, and `axon` leaves that process alone and
discards the record.

A tunnel started by hand with the same command, of which `axon` has no
record, is adopted: `axon` uses it when it is up and replaces it when it is
down. Any other process on port 46581 is reported and left running.

`ssh` to a cluster, VS Code Remote-SSH and the SkyPilot dashboard at
`http://localhost:46581` use the tunnel without going through `axon`.

The tunnel accepts every local connection to port 46581 and does not tell
one local process from another. A request from any account on the machine
reaches the API server with the identity of the user who ran `axon login`.
A teleport pathway is for machines that only the user logs in to
[{Q?}](questions/tunnel-local-access.md).

The tunnel does not survive the expiry of the Teleport certificate or a
reboot. `axon login` restores it, and `axon sky` restores it when the
certificate is still valid. Leaving a teleport pathway stops it, by the
recorded process ID and command line alone.

### The SkyPilot client

`axon` keeps its own SkyPilot client, separate from any other on the laptop
[{D!}](decisions/client-install-location.md). It downloads a pinned `uv`
into `~/.axon/tools/` and verifies its checksum; that `uv` installs Python
and `skypilot[kubernetes]` into the same directory, from pre-built packages
only, taking for each package the newest version that has a build for the
laptop. The install needs no
Python, no `uv` and no `sudo` on the laptop beforehand.

`axon` runs this client by absolute path and ignores any `sky` on `PATH`.

The first install downloads a few hundred megabytes. Before it downloads
anything, `axon` says where the client installs and that the system's Python
and packages are left alone. It prints one line per step, not the installer's
own output, and the line shows that a long step is working. When the install
fails, `axon` says which step failed, and the error carries the installer's
own message.

The `axon` binary carries a default client version, used for the first
install. At each login, after the server is reachable, `axon` reads the
version the server reports and, when the client's differs, reinstalls the
client at the server's version and says which versions it moved between
[{D!}](decisions/client-version-source.md). A server upgrade needs no
release of `axon`.

On a direct pathway the sync follows the login, which runs with the
installed or default client. The default client logs in to every server
version in use, so the sync always runs
[{Q?}](questions/client-version-skew.md). Each release of `axon` raises the
default to the latest SkyPilot release that it is tested against.

### Commands

`axon sky` runs `axon`'s own SkyPilot client and passes it every argument
unchanged, whatever the command, including commands that a later SkyPilot
release adds [{D!}](decisions/sky-command-coverage.md).
`axon sky launch -c dev task.yaml` runs `sky launch -c dev task.yaml`.

Four commands are refused by name, because the endpoint, the login and the
API server belong to `axon login` and `axon logout`:

| Command | Refusal |
|---|---|
| `axon sky api login` | Points to `axon login`. |
| `axon sky api logout` | Points to `axon logout`. |
| `axon sky api start` | Says that `axon` connects to the organization's API server and does not start one on the laptop. |
| `axon sky api stop` | The same as `api start`. |

A refused command is refused with `--help` too. Every other `api` command
passes through.

`axon sky` sets no task file, queue or label. What a command launches is
decided by its arguments.

### Help

`axon sky` with no arguments, and any `axon sky` command with `--help` or
`-h` before a `--`, prints SkyPilot's own help. `axon` runs the client for
it without the connection check, so help needs the installed client and no
login. SkyPilot's help and hints name `sky` commands; the user types each
as `axon sky`.

### Running a command

Before the command runs, `axon` sets two variables in its environment,
replacing any value the user's shell exports. `axon login` sets neither:
`sky api login` refuses to run while `SKYPILOT_API_SERVER_ENDPOINT` is set, so
`axon login` runs `sky api login -e` with it unset.

| Variable | Value | Effect |
|---|---|---|
| `SKYPILOT_API_SERVER_ENDPOINT` | The pathway's endpoint | The client talks to the pathway's server, whatever endpoint the shell exports. |
| `SKYPILOT_USER` | The logged-in email | The pre-launch volume checks, which run as the laptop username when the variable is unset, run as the email, so a user's own volume passes when the laptop username differs from the email's local part. |

`axon sky` never starts an API server on the laptop. SkyPilot starts one
only when the endpoint is the reserved local address, `http://localhost:46580`
or its `127.0.0.1` and `0.0.0.0` forms, and nothing answers there. No
pathway's endpoint is that address: a `direct` endpoint is `https`, and a
`teleport` endpoint is port 46581.

`axon` then replaces its own process with the `sky` process. The terminal,
the signals, the output and the exit code are `sky`'s. The output is not
rewritten, so hints that `sky` prints name `sky` commands.

### Connection check

Before it forwards a command, `axon sky` makes one request to the API
server's health endpoint and acts on the result. It repairs only what needs
no action from the user [{D!}](decisions/connection-repair.md).

| Condition | What `axon sky` does |
|---|---|
| The server answers, reports the user's identity, and its version equals the client's | Runs the command. |
| No login has run on this laptop | Stops: "Run `axon login` first." |
| Teleport: the tunnel is down and the Teleport certificate is valid | Restarts the tunnel once, says so in one line, repeats the request, and acts on the new result. |
| Teleport: the tunnel's process is running and the server does not answer after that restart | Stops: "The SkyPilot API server is not answering. Retry later." |
| Teleport: the tunnel's process exits during that restart | Stops with the end of the tunnel's log: "Couldn't restart the Teleport tunnel. Run `axon login`." |
| Teleport: the Teleport certificate has expired | Stops: "Teleport session expired. Run `axon login`." |
| Direct: the endpoint is unreachable | Stops: "Can't reach `<host>`.", followed by the pathway's `unreachable_hint` when it has one. |
| Direct: the session has expired | Stops: "SkyPilot session expired. Run `axon login`." |
| Direct: the server answers with an error that is not about the login | Stops: "The SkyPilot API server is not answering. Retry later." |
| The client's version differs from the server's | Stops: "Run `axon login` to update the SkyPilot client." |

`axon sky` never opens a browser and never starts an install.

## Failures

Every failure names the step that failed and the next action for the user.
`axon`'s own messages go to standard error and begin with `axon:`. When
`axon` stops before running a command, it exits with a non-zero code.

A message names an organization detail only by printing a host or a
text field from a config file as it is written.

A message names a product only when it comes from the part of `axon` that
uses that product [{D!}](decisions/product-names-in-messages.md).
"SkyPilot" appears only in messages from `sky`. "Teleport" appears only
in messages about a `teleport` pathway. The general parts of `axon`, which
are the stages of login, the pathway prompt, the organization's
configuration and the form of a failure, name neither.

## Release and install

`axon`'s repository is public. `axon` is released as an archive for each
of:

- macOS on Apple silicon
- macOS on Intel
- Linux on x86-64

`axon` itself runs on every platform it is released for. The platforms a
release is built for do not depend on what any command group's backend
supports.

A command group whose backend needs a client on the laptop installs that
client from pre-built packages only, and never builds one from source, so no
compiler is needed on the laptop
[{D!}](decisions/client-prebuilt-packages.md). Where a backend's client
cannot be installed on a platform, connecting that command group fails there
with a message that carries the installer's reason. The failure is that
command group's alone: `axon` itself and the pathway's session are not
affected, and no other command group depends on it.

A Windows user runs the Linux binary inside WSL. There is no native Windows
build.

A release is built by GoReleaser in GitHub Actions when a version tag is
pushed, and holds the archives, a checksum file and `install.sh`. A
release is published only after its archives install, update an older
`axon` and run on every platform it is built for. The binaries are not signed or notarized.

A user installs `axon` with one line, which downloads `install.sh` from the
latest release with `curl` and runs it [{D!}](decisions/install-method.md):

    curl -fsSL https://github.com/enigma-brain/axon/releases/latest/download/install.sh | sh

`install.sh` detects the operating system and CPU, downloads the matching
archive and the checksum file from the release with `curl`, verifies the
archive, and puts `axon` in `~/.local/bin`. Nothing needs `sudo` or a GitHub
account. When `~/.local/bin` is not on `PATH`, the script prints the line
that adds it. `AXON_VERSION` names another release to install, such as a
pre-release.

Installing requires `curl` and `sha256sum` or `shasum`.

### Updating axon

`axon update` updates `axon` to the latest release
[{D!}](decisions/self-update.md). It is built into `axon` and follows the
rules of `install.sh`: the latest release is the one GitHub serves as the
latest, which is never a pre-release; the archive for the laptop's platform
and the checksum file come from that one release; and the archive is
verified against the checksum file before anything is written.

`axon update --version <version>` installs the named release instead, a
pre-release or an older release included. The version is accepted with or
without its leading `v`.

Versions compare by semantic versioning, so a pre-release updates to its
final release. When the running version is the one to install, `axon update`
says so and changes nothing. Without `--version`, a running version newer
than the latest release is left as it is, and `axon update` names the command
that installs the latest.

`axon update` replaces the file it runs from, following a symbolic link to
the file it names, and nothing else. `~/.axon`, the login and the clients
`axon` installed are left as they are. It writes the new binary beside the
old one, runs it to check that it reports the expected version, and renames
it over the old one, so the binary is replaced in one step. A failure at any
step, such as no network, no archive for the platform, a checksum mismatch
or a directory it cannot write, leaves the old binary in place, and the
message names the step, says that the old version is unchanged, and gives
the next action.

A development build, whose version is not a release version, has no version
to compare: `axon update` refuses to replace it and gives the install line.

`axon` contacts its releases only when `axon update` runs. No other command
looks for a new release, and nothing reports that one exists.

Running the install line again also updates `axon`.

Each member of an organization chooses which version of `axon` to run
[{Q?}](questions/organization-axon-version.md).

An organization's setup for its members is the install line followed by
`axon login`, with the server's address, or with `--config <source>` for an
organization that hands out a config file. The page that tells an organization's members
how to get started, with its source for `--config`, is the organization's own
and is kept with its config files, not in this repository.

## Documentation site

`axon` has a public documentation site, separate from the readme, for the
members of any organization that uses `axon`
[{D!}](decisions/documentation-site.md).

- Its source is a [Starlight](https://starlight.astro.build) project in
  `docs/site`.
- It is served from GitHub Pages at `https://enigma-brain.github.io/axon/`.
- It holds nothing about any one organization.
- It links to SkyPilot's documentation only from the page of the `sky`
  command group.
