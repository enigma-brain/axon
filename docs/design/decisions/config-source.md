# axon login takes a config file from an address, a file or standard input

## Context

`axon` holds no organisation detail, so before a first login it knows
nothing about where to log in. The configuration has to reach the laptop
before the user can log in to anything, so it cannot come from behind any
of the organisation's logins.

Some organisations are content to publish their configuration, which holds
hostnames and no secrets. Others keep their hostnames private and hand the
file out to members through a channel the members are already signed in
to, such as a private repository or an internal page.

`axon` is one binary for every organisation, released from a public
repository.

## Rationale

`axon login --config <source>` takes a config file from an `https`
address, a file path, or standard input (`-`), checks it, adds it to the
stored files, and logs in.

- One flag on the existing login command serves every organisation. A user
  learns no other command, and the setup an organisation documents is two
  lines: the install line and the login with `--config`.
- A URL serves an organisation that publishes its file, with no tool beyond
  `axon`.
- A file path and standard input serve an organisation that keeps its file
  private. Standard input lets its setup stay one line, piping the file
  from a tool its members are already signed in to.
- `axon` fetches nothing over plain `http`, so a URL source cannot be
  altered on the way.

## Alternatives considered

- **The install line carries the URL.** It mixes installing `axon` with
  choosing an organisation, leaves a user who installed another way with no
  path, and still needs a URL reachable without a login.
- **Each organisation builds its own binary with the configuration inside.**
  Every organisation runs a release pipeline and gives up the shared
  release.
- **The user copies the file into `~/.axon` by hand.** The user has to know
  a path and a file name, and nothing checks the file until a login fails.
- **A URL only.** An organisation that keeps its hostnames private has no
  address to give.
