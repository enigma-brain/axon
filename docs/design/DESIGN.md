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
organisation. A user installs one tool, logs in once, and reaches each part
of the platform through a command group named for it. What sits behind a
command group is free to change without the user learning a new path.

`axon` has one command group, `devenv`, which is the user's way to the
organisation's SkyPilot API server: dev environments, GPUs, jobs and
volumes.

`axon` is written in Go and is a single binary that runs on the user's
laptop. It has no server component.

One `axon` serves any organisation whose pathways are of the kinds below.
What makes it serve an organisation is an organisation file that the
organisation supplies.

## Organisations

### Organisation details

An organisation's details are everything that names its deployment:

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

`axon` takes every organisation detail from an organisation file, and has no
default for any of them. The name of the GitHub organisation that hosts
`axon`'s own repository is not an organisation detail in this sense: it
names where `axon` comes from, not whom it serves.

### Pathway kinds

A pathway is a way to reach the organisation's API server. Every pathway is
one of two kinds [{D!}](decisions/pathway-kinds.md).

| Kind | Login | Reaches the API server |
|---|---|---|
| `direct` | The API server's own login, in a browser | At its address, from wherever the organisation's network allows |
| `teleport` | Teleport, in a browser when the certificate is not valid | Through a local tunnel to a Teleport application |

The rest of this document describes each pathway by its kind. An
organisation names its own pathways, any number of them of either kind.

### Organisation files

An organisation's configuration is a TOML file, an organisation file. A
laptop holds any number of them, from one organisation or several, and the
user moves between the pathways they hold
[{D!}](decisions/organisation-files.md). An organisation that gives each
pathway its own file lets most of its members hold one file and never meet
a prompt, while a member on several pathways holds several files.

These are two complete files, one pathway each:

```toml
# campus.toml
format = 1
id = "example-campus"

[[pathway]]
id = "campus"
kind = "direct"
name = "Campus"
description = "Single sign-on in a browser. Needs the campus network or VPN."
unreachable_hint = "Connect to the campus VPN and retry."

[pathway.devenv]
endpoint = "https://sky.example.org"
```

```toml
# remote.toml
format = 1
id = "example-remote"

[[pathway]]
id = "remote"
kind = "teleport"
name = "Remote"
description = "Teleport. Works from anywhere."

[pathway.teleport]
proxy = "teleport.example.org"

[pathway.devenv]
app = "skypilot"
```

A file holds one or more pathways, each a `[[pathway]]` entry. In TOML a
table such as `[pathway.devenv]` belongs to the `[[pathway]]` above it, so
each pathway's tables follow its own `[[pathway]]` line and come before the
next one.

| Field | Type | Holds |
|---|---|---|
| `format` | `1` | The file's format. |
| `id` | Lowercase letters, digits and hyphens, starting with a letter, at most 32 characters | The file's identifier, which names the stored file. |

Every other field belongs to a pathway. Every pathway has the same three
parts, whatever its kind [{D!}](decisions/pathway-contract.md):

- **Identity**: what the pathway is.
- **Text for people**: labels and sentences that the organisation writes and
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
| `description` | Text | Free text, required | The prompt's line about the pathway, which is where the organisation names its login system and where the pathway works. |
| `unreachable_hint` | Text | Free text, optional | A sentence `axon` prints after "Can't reach `<host>`." when a host the pathway names does not answer. |
| `teleport.proxy` | Kind table, `teleport` | Hostname, optionally with a port | The Teleport proxy. |
| `devenv.endpoint` | `devenv` table, `direct` | `https` address | The SkyPilot API server. |
| `devenv.app` | `devenv` table, `teleport` | Teleport application name | The Teleport application that serves the SkyPilot API server. |

`axon` accepts a file only when:

- `format` is `1`;
- it has an `id` of the allowed form;
- it has at least one pathway;
- every pathway has an `id` of the allowed characters, a `kind` of `direct`
  or `teleport`, a `name` and a `description`;
- every `direct` pathway has `devenv.endpoint`, and it is an `https`
  address;
- every `teleport` pathway has `teleport.proxy`, a hostname, optionally
  with a port, and `devenv.app`;
- it has no field or table that this section does not list for the file or
  for the pathway's kind;
- no pathway identifier appears twice in it, or in any other stored file
  apart from the one it replaces.

A file that fails a check is refused whole, with a message that names the
field and the check. A file refused because a pathway identifier is taken
names the identifier and the stored file that has it. A refused file
changes nothing: every stored file stays as it was.

The organisation files are trusted input: they decide which servers the
user's logins and requests go to. Apart from the hosts the active pathway
names and the address its file is fetched from, `axon` contacts only the
download sources of the tools it installs for its own use.

### Adding an organisation file

`axon login --org <source>` takes a file from a source, checks it, stores
it, and then logs in [{D!}](decisions/organisation-config-source.md). The
source is one of three:

| Source | Example |
|---|---|
| An `https` address | `axon login --org https://example.org/axon.toml` |
| A file path | `axon login --org ~/Downloads/axon.toml` |
| `-`, standard input | `<command that prints the file> \| axon login --org -` |

An address is fetched without credentials, so it suits an organisation that
publishes its file. A file path or standard input suits one that hands the
file out by other means; with standard input, an organisation's setup is
one line that fetches the file with a tool its members are already signed
in to. An address with a scheme other than `https` is refused.

With `--org -`, standard input is the file, so `axon login` asks its
questions on the terminal. When there is no terminal, a question that has a
flag, such as the pathway, is answered by the flag, and `axon` stops and
names the flag when it is missing. A question with no flag is answered
"no".

`axon` stores the file as `~/.axon/orgs/<id>.toml`, named by the file's
`id`, and records its source in `~/.axon/state/`. Adding a file leaves the
other stored files as they are. Adding a file whose `id` is already stored
replaces that one file and its source.

Removing a file is deleting it from `~/.axon/orgs/`. There is no command
for it.

### Updating an organisation file

At each `axon login`, once the pathway is determined and before its session
is established, the file that holds it is fetched again, when its source is
an address and `--org` did not add it in the same login. The fetched file
replaces the stored one when it passes the checks and its `id` is the stored
file's [{D!}](decisions/organisation-config-updates.md). When the fetch
fails or the file is refused, `axon` says so in one line and continues with
the stored file. No other stored file is fetched.

When the fetched file no longer has the pathway, the pathway is left. If
`--pathway` named it, `axon login` stops, says that the pathway is no
longer in its organisation file, and lists the identifiers the stored files
have. Otherwise `axon login` determines a pathway again from the stored
files, with no further fetch in that login, and uses the stored copy of the
newly chosen pathway's file. When it chooses without asking, because one
pathway remains, it says in one line which pathway it now uses.

A file from a file path or standard input is read once, when `--org` is
given. Updating it is running `axon login --org` again with the new file.

`axon devenv` never fetches a file. It uses the stored ones.

At each `axon login`, `axon` reads every stored file and applies the checks
above. A stored file that fails them is left out, with one line that names
it.

### No organisation file

`axon --version` and `axon --help` work without an organisation file.

When no file is stored and `--org` is not given, `axon login` stops with:
"axon: no organisation configuration. Run `axon login --org <source>` with
the address or file your organisation gives you."

`axon devenv` with no file stops with: "Run `axon login` first."

## Commands

| Command | Behaviour |
|---|---|
| `axon login [--org <source>] [--pathway <id>]` | Adds an organisation file when `--org` is given, determines the pathway, establishes its session, and connects every command group. |
| `axon devenv <command> [args]` | Checks the connection, then runs the `sky` command of the same name with the same arguments. |
| `axon --version` | Prints the version of `axon`. |
| `axon --help` | Lists the commands. |

These are all of `axon`'s commands.

## Files and directories

`axon` keeps everything it owns on the laptop in one directory, `~/.axon`
[{D!}](decisions/file-layout.md).

| Path | Holds |
|---|---|
| `~/.axon/orgs/` | The organisation files, one per file added, each `<id>.toml` as its source gave it. |
| `~/.axon/config.toml` | The active pathway, by its identifier. |
| `~/.axon/tools/` | What `axon` installs for its own use. |
| `~/.axon/state/` | What `axon` records about running state, such as each organisation file's source, the settings the pathway had when it was connected, the tunnel's process, and the logged-in email. |

`axon` writes a file in `~/.axon/orgs/` only by replacing it whole, and
never writes the active pathway into it.

Removing `~/.axon` removes everything `axon` installed for its own use and
everything it recorded, the organisation files included. The next `axon login`
needs `--org`. Two things are outside the directory and remain: Teleport's
own profile and certificates in `~/.tsh`, which `tsh` writes, and a tunnel
that is running. The next login adopts that tunnel only when its command
line is exactly the active pathway's tunnel command, its proxy and
application included; a tunnel started in another form is reported as
holding the port.

The `axon` binary is not in this directory; it is in `~/.local/bin`.

## axon login

`axon login` is the one login command. It runs these stages in order and
stops at the first that fails:

1. Add the organisation file that `--org` gives, when it is given.
2. Determine the pathway.
3. Refresh the pathway's file, as "Updating an organisation file" sets out.
4. Establish the pathway's session.
5. Connect each command group, by that group's own steps
   [{Q?}](questions/command-group-login.md).

`axon login` is idempotent. Running it when everything is in place changes
nothing and reports each connection as good.

### Determining the pathway

One pathway is active at a time, chosen from the pathways of every stored
organisation file. `axon` stores the active pathway's identifier in
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

`--org` without `--pathway` makes a pathway of the added file the active
one, unless the active pathway is already in that file: with one pathway in
the file, that pathway, and with several, the prompt offers the file's
pathways. A user who adds a file means to use it.

A user also leaves the active pathway when the stored files change under
it:

- its file is replaced, by `--org` or by a fetch from an address, and the
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
disconnects each command group from the pathway that is left.
Disconnecting uses only what `axon` recorded when it connected, never an
organisation file, so it works when the file that named the pathway is
gone.

### The pathway's session

On a `direct` pathway there is no session apart from each service's own: a
service that needs a login opens it in a browser when `axon` connects to
that service.

On a `teleport` pathway the session is the Teleport login. `axon` makes sure
`tsh` is present, then checks the Teleport certificate for the pathway's
proxy. When the certificate is not valid, `axon` runs
`tsh login --proxy=<teleport.proxy>`, which opens a browser. A valid
certificate needs no login and opens no browser. The certificate lasts as
long as the organisation's Teleport sets. The user runs no `tsh` command by
hand.

### tsh

`axon` uses the `tsh` on `PATH` when there is one, and never replaces it.
Teleport supports a client whose major version is the proxy's or one behind
it. When the `tsh` on `PATH` is outside that range, `axon` stops, names the
installed version and the proxy's, and says to update `tsh`.

When no `tsh` is on `PATH` and `axon` has not installed one, `axon login`
installs it at the version the pathway's proxy reports
[{D!}](decisions/tsh-install.md).

- On Linux, `axon` downloads Teleport's archive, verifies its checksum, and
  keeps `tsh` in `~/.axon/tools/`. Nothing needs `sudo`.
- On macOS, `axon` downloads Teleport's archive for the laptop's
  architecture, verifies its checksum, and keeps `tsh.app` intact in
  `~/.axon/tools/`, running the binary inside the bundle. Nothing needs `sudo`
  or a password [{D!}](decisions/tsh-macos-install.md).

When the install fails or the user declines it, `axon` prints Teleport's
install link and stops.

## devenv

The `devenv` command group is the user's way to the organisation's
SkyPilot API server. Its backend is SkyPilot, and it has one backend. The
parts of `axon` that are specific to SkyPilot are kept together, inside
`devenv`, and no swappable backend is built. Everything `axon` does with
SkyPilot is in this section.

| Pathway kind | Endpoint the SkyPilot client uses |
|---|---|
| `direct` | The pathway's `devenv.endpoint` |
| `teleport` | `http://localhost:46581`, a Teleport tunnel to the pathway's `devenv.app` |

The tunnel's port, 46581, is `axon`'s and is the same for every
organisation.

SkyPilot's own configuration and session stay in `~/.sky/`, where SkyPilot
keeps them.

### What login does for devenv

When `axon login` connects `devenv`, it runs these steps in order:

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
3. `axon` runs `sky api login -e <devenv.endpoint>`, which opens the
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
2. `axon` runs `tsh --proxy=<teleport.proxy> apps login <devenv.app>`,
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
`tsh --proxy=<teleport.proxy> proxy app <devenv.app> --port 46581`. `axon`
starts it detached from the terminal, records its process ID and its command
line, and sends its output to a log file
[{D!}](decisions/tunnel-lifecycle.md). The record and the log are kept in
`~/.axon/state/`. The tunnel outlives the `axon` command and the terminal
that started it.

The tunnel is up when the health endpoint answers through it. A process that
exists and does not serve is down.

`axon` treats a process as its tunnel when the process belongs to the user
and its command line is exactly the tunnel's command, the pathway's
`teleport.proxy` and `devenv.app` included, so a tunnel to another proxy is
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
reboot. `axon login` restores it, and `axon devenv` restores it when the
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

Each `axon devenv` command runs the `sky` command of the same name and
passes its arguments and flags through unchanged, including flags that
`axon`'s help does not mention [{D!}](decisions/devenv-command-mapping.md).
`axon devenv launch -c dev task.yaml` runs `sky launch -c dev task.yaml`.

`axon devenv` accepts the commands in this table and no others
[{D!}](decisions/devenv-command-coverage.md).

| Group | `axon devenv` command | Runs |
|---|---|---|
| Dev environments | `launch` | `sky launch` |
| | `status` | `sky status` |
| | `down` | `sky down` |
| | `autostop` | `sky autostop` |
| | `exec` | `sky exec` |
| | `logs` | `sky logs` |
| | `queue` | `sky queue` |
| GPUs | `gpus list` | `sky gpus list` |
| Managed jobs | `jobs launch` | `sky jobs launch` |
| | `jobs queue` | `sky jobs queue` |
| | `jobs logs` | `sky jobs logs` |
| | `jobs cancel` | `sky jobs cancel` |
| Volumes | `volumes ls` | `sky volumes ls` |

Any other command is refused with a message that `axon devenv` does not
cover it. SkyPilot's `api` commands are never covered: the endpoint, the
login and the local API server belong to `axon login`.

`axon devenv` sets no task file, queue or label. What a command launches is
decided by its arguments.

### Help and documentation

`axon devenv --help` lists the covered commands. `axon devenv <command>
--help` prints help written for `axon`; it does not print SkyPilot's. The
help names no organisation detail.

The help text of every command is kept in the repository in one place, and
the command reference is produced from it.

### Running a command

Before the command runs, `axon` sets two variables in its environment,
replacing any value the user's shell exports. `axon login` sets neither:
`sky api login` refuses to run while `SKYPILOT_API_SERVER_ENDPOINT` is set, so
`axon login` runs `sky api login -e` with it unset.

| Variable | Value | Effect |
|---|---|---|
| `SKYPILOT_API_SERVER_ENDPOINT` | The pathway's endpoint | The client talks to the pathway's server, whatever endpoint the shell exports. |
| `SKYPILOT_USER` | The logged-in email | The pre-launch volume checks, which run as the laptop username when the variable is unset, run as the email, so a user's own volume passes when the laptop username differs from the email's local part. |

`axon devenv` never starts an API server on the laptop. SkyPilot starts one
only when the endpoint is the reserved local address, `http://localhost:46580`
or its `127.0.0.1` and `0.0.0.0` forms, and nothing answers there. No
pathway's endpoint is that address: a `direct` endpoint is `https`, and a
`teleport` endpoint is port 46581.

`axon` then replaces its own process with the `sky` process. The terminal,
the signals, the output and the exit code are `sky`'s. The output is not
rewritten, so hints that `sky` prints name `sky` commands.

### Connection check

Before it forwards a command, `axon devenv` makes one request to the API
server's health endpoint and acts on the result. It repairs only what needs
no action from the user [{D!}](decisions/connection-repair.md).

| Condition | What `axon devenv` does |
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

`axon devenv` never opens a browser and never starts an install.

## Failures

Every failure names the step that failed and the next action for the user.
`axon`'s own messages go to standard error and begin with `axon:`. When
`axon` stops before running a command, it exits with a non-zero code.

A message names an organisation detail only by printing a host or a
text field from an organisation file as it is written.

A message names a product only when it comes from the part of `axon` that
uses that product [{D!}](decisions/product-names-in-messages.md).
"SkyPilot" appears only in messages from `devenv`. "Teleport" appears only
in messages about a `teleport` pathway. The general parts of `axon`, which
are the stages of login, the pathway prompt, the organisation's
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
pushed, and holds the archives, a checksum file and `install.sh`. The
binaries are not signed or notarised.

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

Updating `axon` is running the same line again.

An organisation's setup for its members is the install line followed by
`axon login --org <source>`. The page that tells an organisation's members
how to get started, with its source for `--org`, is the organisation's own
and is kept with its organisation files, not in this repository.

## Documentation site

`axon` has a public documentation site, separate from the readme, for the
members of any organisation that uses `axon`
[{D!}](decisions/documentation-site.md).

- Its source is a [Starlight](https://starlight.astro.build) project in
  `docs/site`.
- It is served from GitHub Pages at `https://enigma-brain.github.io/axon/`.
- It holds nothing about any one organisation, and links to no SkyPilot
  documentation.
