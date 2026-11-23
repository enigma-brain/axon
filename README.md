# axon

`axon` is one command-line entry point to a platform that an organization
runs. A user installs one tool and logs in once, with `axon login`. Each part
of the platform is then a command group of `axon`. A group that passes a
tool's own commands straight through is named for the tool: `axon sky` runs
SkyPilot's `sky` commands against the organization's SkyPilot API server. A
group that hides its tool behind commands of its own is named for the part of
the platform it reaches, and the tool behind it can change without users
learning a new way in.

The organization decides how a laptop reaches its platform. Each way in is a
pathway, and the organization describes its pathways in a config file
that it gives its users. `axon` logs in by whichever pathway the user is on,
and names no organization itself.

The design is in [docs/design/DESIGN.md](docs/design/DESIGN.md).

The documentation site, at <https://enigma-brain.github.io/axon/>, covers
installing, logging in, dev environments, writing a config file and
troubleshooting. This readme gives the short form of each; see the site for
anything it does not cover.

## What is built today

One command group:

| Command group | Part of the platform | Tool behind it |
|---|---|---|
| `axon sky` | Dev environments, GPUs, jobs and volumes | SkyPilot |

Two kinds of pathway:

| Kind | Reaches the platform | Login |
|---|---|---|
| Direct | At the server's own address | The server's own, in a browser |
| Teleport | Through Teleport, by a tunnel that `axon` runs | Teleport's, in a browser when the certificate is not valid |

`axon sky` passes every `sky` command to `axon`'s own SkyPilot client, against
the organization's SkyPilot API server, with SkyPilot's own help. Wherever
SkyPilot's help or hints say `sky`, type `axon sky`.
`axon` installs the SkyPilot client it needs for its own use and keeps
everything it owns on the laptop in `~/.axon`.

## Installing

    curl -fsSL https://github.com/enigma-brain/axon/releases/latest/download/install.sh | sh

The line downloads the install script of the latest release and runs it. The
script installs `axon` for your platform into `~/.local/bin`, after checking
it against the release's checksums. It needs `curl` and `sha256sum` or
`shasum`, a GitHub account is not needed, and nothing needs `sudo`. When
`~/.local/bin` is not on your `PATH`, it prints the line to add to your
shell's startup file.

To update `axon`, run the same line again.

To install a named version, download that release's install script and set
`AXON_VERSION` for it. A pre-release is installed this way because GitHub
never serves a pre-release as the latest, so the address names the
release's tag. `1.2.3-rc.1` stands for the version you want:

    curl -fsSL https://github.com/enigma-brain/axon/releases/download/v1.2.3-rc.1/install.sh | AXON_VERSION=1.2.3-rc.1 sh

`axon` is released for macOS on Apple silicon, macOS on Intel and Linux on
x86-64. On Windows, install it inside WSL.

### Installing from a script

When `curl` fails, the `sh` at the end of the line reads nothing and exits
with status 0, so a calling script cannot tell that nothing was installed.
A script that installs `axon` downloads the install script to a file first,
and runs it only when the download succeeded:

    curl -fsSL -o install.sh https://github.com/enigma-brain/axon/releases/latest/download/install.sh &&
      sh install.sh

The same two steps install a named version:

    curl -fsSL -o install.sh https://github.com/enigma-brain/axon/releases/download/v1.2.3-rc.1/install.sh &&
      AXON_VERSION=1.2.3-rc.1 sh install.sh

The install script exits non-zero, with a message that names what could not
be downloaded or verified, when the installation fails.

## Organizations

`axon` takes everything about the organization it serves from an
config file, which the organization gives its members: the
pathways, the addresses of its servers, and the text `axon` shows about
them. `axon` itself names no organization. The file's format is in
[docs/design/DESIGN.md](docs/design/DESIGN.md#config-files).

A pathway is a way to reach the organization's API server. It is of one of
two kinds: a direct pathway reaches the server at its own address, and a
Teleport pathway reaches it through Teleport. A file holds one pathway or
several, and a laptop holds any number of files.

### Adding a file

The first login adds your organization's config file with `--config`, from one of
three sources:

    axon login --config https://example.org/axon.toml
    axon login --config ~/Downloads/axon.toml
    <command that prints the file> | axon login --config -

The file is checked and stored as `~/.axon/orgs/<id>.toml`, named by the
`id` it holds, and its source is recorded in `~/.axon/state/orgs/`. A file
that fails a check is refused with a message naming the field, and nothing
changes. Adding a file whose `id` is stored already replaces that file;
the other files stay as they are. Two files may not hold the same pathway
identifier.

With `--config -`, standard input is the file, so `axon login` asks its
questions on the terminal. Without a terminal, a question with a flag,
such as which pathway, stops and names the flag; any other question is
answered no.

### When your organization changes its file

How a change reaches your laptop depends on where the file came from:

- **An `https` address:** each `axon login` fetches the file that holds
  the pathway it uses again, before it connects, and stores the new one
  when it passes the checks and keeps its `id`. You do nothing; the change
  arrives at your next login. Only that one file is fetched, and a file
  that `--config` added in the same login is not fetched twice. When the
  fetch fails or the new file is refused, `axon` says so in one line and
  logs in with the file it has.
- **A file path or standard input:** the file is read once. Run
  `axon login --config` again with the new file.

`axon sky` never fetches a file; it uses the stored ones.

When the new file changes the active pathway's servers, the login
disconnects it from the old ones and connects it to the new ones, and the
pathway stays active. A change to its name or text alone changes nothing.
When the new file no longer has the active pathway, the login chooses
again, as below, saying which pathway it now uses when only one is left;
with `--pathway` naming the dropped pathway it stops and lists the
pathways you can use.

### Removing a file

Delete it from `~/.axon/orgs/`; there is no command for it. When it held
the active pathway, the next `axon login` chooses another from the stored
files and, once one is chosen, disconnects the pathway you left. When it
was the last file, `axon login` disconnects the pathway you left, stopping
its Teleport tunnel and forgetting the logged-in email, then stops and asks
for `--config`. At each login a stored file that fails the checks is left out,
with one line naming it.

## Logging in

    axon login

With no config file stored, `axon login` disconnects the pathway it
had connected, if any, then stops and asks for `--config`.
Otherwise it chooses the pathway, logs you in, and connects `axon sky`.

### Choosing the pathway

One pathway is active at a time, stored by its identifier in
`~/.axon/config.toml`; later logins use it without asking.

- **One pathway in all the stored files:** it is used without asking.
- **More than one:** the first login asks which one you are on, offering
  each by the name and description your organization wrote, file by file in
  the order of the files' identifiers. Answer with the number or the
  identifier:

      Which pathway are you on?
        1) Campus  Single sign-on in a browser. Needs the campus network.
        2) Remote  Teleport. Works from anywhere.
      Pathway [1-2]:

- **`--pathway <id>`** chooses without asking and makes that pathway the
  active one. It is how you switch. An identifier no stored file has is
  refused, with the list of those they have.
- **`--config`** makes a pathway of the file it adds the active one, unless
  the active pathway is already in that file. With several pathways in the
  file, it asks which of them.

Switching to another pathway, or leaving one because its file changed or
was removed, first disconnects the one you leave: the
tunnel of a Teleport pathway stops, and the recorded email is removed until
the new login records one. `axon` goes by what it recorded when it
connected, in `~/.axon/state/pathway.toml`, so this works when the file
that named the old pathway is gone.

Running `axon login` again is safe: when everything is in place it changes
nothing and reports the connection as good.

On success `axon` records your email in `~/.axon/state/email` and brings its
SkyPilot client to the server's version, saying which versions it moved
between.

### On a direct pathway

A direct pathway reaches the API server at its own address, which may
answer only from your organization's network.

- **Connect to the network your organization names first.** When the
  server does not answer, `axon login` says "Can't reach `<host>`.",
  followed by your organization's hint when its file gives one, and stops
  without opening a browser.
- **The login opens in your default browser.** Sign in with your
  organization's account and click "Authorize"; `axon` waits for the page
  to complete. When your session is still good, no browser opens.
- **The wait ends after five minutes.** If the login has not been completed
  in the browser by then, `axon` stops and asks you to run `axon login` again.
- **A stale session is cleared for you.** When the login ends with
  `needs_auth`, `axon` removes `~/.sky/cookies.txt`, which holds only the API
  server's cookies, and logs in once more. When `SKYPILOT_API_COOKIE_FILE`
  names another file, `axon` uses that file, as SkyPilot does.

### On a Teleport pathway

A Teleport pathway reaches the API server through Teleport, from anywhere.
`axon login` does what the manual Teleport steps did:

- **It finds `tsh`**, Teleport's client, on your `PATH`. `tsh` is a
  prerequisite of a Teleport pathway; `axon` does not install it.
- **It logs you in to Teleport** with `tsh login --proxy=<proxy>`, which
  opens your organization's sign-in page in a browser. Where no browser
  opens by itself, as on WSL, `tsh` prints a link instead: open it in a
  browser to finish the sign-in. When your Teleport certificate is still
  valid, nothing opens. The certificate lasts as long
  as your organization's Teleport sets.
- **It brings up the tunnel**,
  `tsh --proxy=<proxy> proxy app <application> --port 46581`, after a fresh
  application login (`tsh --proxy=<proxy> apps login <application>`), where
  the application is the one that serves the API server. The tunnel runs in
  the background, outlives the terminal, and keeps its log in
  `~/.axon/state/tunnel.log`. `axon` records the tunnel's process ID and
  command line in `~/.axon/state/tunnel.json`, and takes a process for its
  tunnel only while that command line, proxy and application included,
  still matches. A tunnel started by an older `axon`, without `--proxy`, is
  another process to it: stop that process and run `axon login` again.
  `ssh` to a cluster, VS Code Remote-SSH and the dashboard at
  `http://localhost:46581` all go through it.
- **It points the SkyPilot client at `http://localhost:46581`** with `sky api
  login`, only when the client is pointed elsewhere. No browser opens:
  Teleport supplies your identity.

#### tsh

A Teleport pathway logs in through Teleport's client, `tsh`, which you
install before you log in, from Teleport's
[install instructions](https://goteleport.com/docs/installation/). `axon`
uses the `tsh` on your `PATH` and never replaces it. With none, `axon login`
stops before any login step:

    axon: establish the Teleport session: this pathway needs Teleport's client, tsh, and no tsh was found on PATH. Install it from https://goteleport.com/docs/installation/ and run `axon login` again.

When the `tsh` on your `PATH` is too old or too new for the Teleport proxy,
`axon` names both versions and stops; install a `tsh` whose major version is
the proxy's or one behind it. A `tsh`
that an earlier `axon` kept in `~/.axon/tools/tsh` is not used; delete that
folder whenever you like.

#### The tunnel

When the tunnel is already up and the server knows you through it, a repeated
`axon login` leaves the tunnel running, so your `ssh` and VS Code sessions
carry on.

**What stops the tunnel:**

- **The Teleport certificate expires**, after the time your organization's
  Teleport sets.
- **The laptop restarts.**
- **`axon login` replaces it**, when the tunnel is down or the server does not
  know you through it. This interrupts sessions through it.
- **You switch pathways** with `axon login --pathway <id>`, or with
  `axon login --config` and another file.

**To restore it,** run `axon login`. `axon sky` also restarts a stopped
tunnel by itself while the certificate is still valid.

**When the tunnel does not start:**

- Another process on port 46581: `axon login` names it and stops. Stop that
  process and log in again.
- The tunnel exits, for example on a Teleport error: `axon login` prints the
  end of its log.
- The tunnel runs but nothing answers through it within 10 seconds: the API
  server, or Teleport's connection to it, is not answering. Your Teleport
  login is fine, so retry later.

## Logging out

    axon logout

`axon logout` undoes what `axon login` did, by `axon`'s own records: it runs
`sky api logout` with `axon`'s client, removes the connected API server's
cookies from SkyPilot's cookie file, which `sky api logout` leaves, stops
the Teleport tunnel, ends the Teleport session for the connected pathway's
proxy with `tsh logout`, and removes the record of the connected pathway
and the logged-in email. It keeps the config files and the active pathway,
so `axon login` logs in again. It prints one line per step, and says why it skipped a step that does
not apply, such as `tsh` not on `PATH`. A failed step is reported and the
others still run; the records are then kept, and running `axon logout` again
retries. To clear a shared machine completely, run `axon logout`, then
delete `~/.axon` and `~/.sky`. On a Teleport pathway, Teleport keeps its
session in `~/.tsh`: when `axon logout` skipped the Teleport step, run
`tsh logout` before you leave the machine.

## Running commands

    axon sky status
    axon sky launch --help

Every argument goes to SkyPilot unchanged, and `--help` prints SkyPilot's own
help. Four commands are refused, because `axon` manages what they would
change: `axon sky api login` (use `axon login`), `axon sky api logout` (use
`axon logout`), `axon sky api start` and `axon sky api stop`.

Before each command, `axon sky` checks the connection to the API server.
It never opens a browser and never installs anything; when the check fails,
it stops and says what to do:

| Message | What to do |
|---|---|
| `` Run `axon login` first. `` | No config file is stored, no login has run on this laptop, it did not finish, or the pathway's file changed since it connected. |
| `` SkyPilot session expired. Run `axon login`. `` | Log in again. |
| `` Teleport session expired. Run `axon login`. `` | Log in again; the Teleport certificate has run out. |
| `` Run `axon login` to update the SkyPilot client. `` | The server runs another SkyPilot version; the login moves the client to it. |
| `Can't reach <host>.` | The server does not answer from where you are. Your organization's hint, when it gives one, follows and says what to do. |
| `The SkyPilot API server is not answering. Retry later.` | The server itself is down; logging in again does not help. |
| `` Couldn't restart the Teleport tunnel. Run `axon login`. `` | The tunnel's log follows the message; the login starts a fresh tunnel. |

On a Teleport pathway, a stopped tunnel is restarted for you when your
Teleport certificate is still valid, with the line `sky: restarting the
Teleport tunnel`. The restart waits up to 10 seconds for the tunnel.

## Developing

`axon` is a single Go program. It needs the Go version named in `go.mod`.

Build the binary at the root of the repository:

    go build

This writes `./axon`. Its version is `dev` unless one is given at build time:

    go build -ldflags "-X main.version=1.2.3"

Compile every package, vet and test:

    go build ./...
    go vet ./...
    go test -race ./...

The tests never touch the real `~/.axon`: they run against temporary home
directories. CI runs the same checks, and a `gofmt` check, on Linux for every
pull request and every push to `main`. They run on macOS in the Mac suite,
which a maintainer starts by hand. [CONTRIBUTING.md](CONTRIBUTING.md#continuous-integration) sets out which
CI job runs when.

### The SkyPilot client install

`internal/skyclient` installs `axon`'s own SkyPilot client. A pinned `uv`,
verified against its checksum, installs a Python and `skypilot[kubernetes]`
under `~/.axon/tools/`. It installs pre-built packages only
(`--only-binary :all:`), so no compiler is needed and, for each package, it
takes the newest version that has a build for the machine:

| Path under `~/.axon/tools/` | Holds |
|---|---|
| `uv/<version>/uv` | The pinned `uv`. |
| `uv-cache/` | `uv`'s cache, reused when the client changes version. |
| `python/` | The Python that `uv` manages. |
| `skypilot/<version>/` | The client's virtual environment; `bin/sky` is the client. |
| `skypilot/current` | The installed client's version, written once an install succeeds. |

`python-bin/`, `uv-tools/`, `uv-tools-bin/` and `uv-credentials/` are other
`uv` directories, pointed there so that `uv`'s own directories are all under
`~/.axon/tools/`.

Raising the `uv` version means changing `uvVersion` and the three checksums
in `internal/skyclient/uv.go`, from the `.sha256` files beside each archive
on `uv`'s release page.

The unit tests use a fake `uv` and no network. A real install, which
downloads a few hundred megabytes into a temporary home directory, runs
with:

    AXON_REAL_INSTALL=1 go test -count=1 -timeout 20m -run '^TestRealInstall$' -v ./internal/skyclient

CI runs it on Linux when a change touches `internal/skyclient/` or the
workflow, and the Mac suite runs it on macOS on Apple silicon and on Intel.

### The smoke test

`smoke_test.go` runs the built `axon` as a user would, in an empty home
directory with an empty `PATH`: `axon login --config` adds a made-up
config file, installs the SkyPilot client for real, and stops at the
endpoint's reachability check, before the browser login. The endpoint is on
the loopback address, so the test reaches no server. It downloads the client,
so it runs only with:

    AXON_REAL_INSTALL=1 go test -count=1 -timeout 20m -run '^TestSmokeLogin$' -v .

With `AXON_SMOKE_BIN` set to a binary's path, it runs that binary instead of
building one. CI runs it on Linux when a change touches code, and the Mac
suite runs it on macOS on Apple silicon and on Intel. Before a release is
published, it runs on all three against the binary the release's
`install.sh` installed.

### tsh

`internal/tsh` finds Teleport's client on `PATH` for a Teleport pathway,
and runs it for the logins, the certificate check and the logout. It reads
the proxy's version from the proxy's `/webapi/ping` endpoint and uses the
`tsh` on `PATH` only when its major version is the proxy's or one behind.
It never downloads `tsh`. The tests use a fake proxy and a fake `tsh` on a
`PATH` that holds nothing else.

### Releasing

Pushing a tag `v<version>` runs `.github/workflows/release.yml`. GoReleaser,
configured in `.goreleaser.yaml`, builds an archive per platform,
`axon_<version>_<os>_<arch>.tar.gz`, and `checksums.txt`, and uploads them
to a draft GitHub release; the binary's `axon --version` prints `<version>`.
Before anything is published, the release's `install.sh` installs the
draft's archive on Linux, on macOS on Apple silicon and on macOS on Intel,
and the workflow checks the installed version and runs the smoke test
against the installed binary. Only when all three pass is the draft
published. A tag with a pre-release part, such as `v1.2.3-rc.1`, is
published as a pre-release, which the install line does not pick up unless
`AXON_VERSION` names it. Once the release is published, the workflow runs
the install line on Linux and checks the installed version.
[CONTRIBUTING.md](CONTRIBUTING.md#the-release-gate) says what a maintainer
does when a check fails.

For every change that touches code, CI runs GoReleaser in snapshot mode and checks that
each archive and `checksums.txt` are built, and runs `shellcheck` on
`install.sh` and on the release gate's `curl` stand-in. To build the archives locally into `dist/`:

    goreleaser release --snapshot --clean

`install_test.go` runs `install.sh` under `sh` with a fake `gh` and `uname`,
and is part of `go test ./...`.

### Layout

| Path | Holds |
|---|---|
| `main.go` | The entry point. Sends every failure through `internal/fail`. |
| `install.sh` | The install script that the install line runs. |
| `.goreleaser.yaml` | How a release is built. |
| `internal/cli` | The command tree: `login`, `logout` and `sky`. |
| `internal/fail` | Prints failures to standard error, prefixed `axon:`, and exits non-zero. |
| `internal/orgfile` | Config files: reading them from a source, checking them, and storing them in `~/.axon/orgs/`. |
| `internal/axondir` | `~/.axon`: the configuration file, `tools/` and `state/`. |
| `internal/connected` | The record of the connected pathway and its settings, in `~/.axon/state/pathway.toml`. |
| `internal/login` | `axon login` and `axon logout` (`logout.go`): adding a file, choosing the pathway, disconnecting the one left, the stages, the sky group's connect steps, and each kind's login in `direct.go` and `teleport.go`. `browser.go` holds the length of the browser wait. |
| `internal/sky` | The `axon sky` command group: its refusals, its connection check, and each pathway kind's endpoint. |
| `internal/apiserver` | The SkyPilot API server's health request, with SkyPilot's cookies and the client's version headers. |
| `internal/skyclient` | `axon`'s own SkyPilot client. |
| `internal/tsh` | Teleport's client: finding it on `PATH`, login, application login, certificate check. |
| `internal/tunnel` | The Teleport tunnel to the API server. |
