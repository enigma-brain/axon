# The active pathway's file is fetched again at every login

## Context

An organization changes its configuration from time to time: a server
moves, a pathway is added or retired. Every member's laptop has to follow.
The configuration reaches a laptop from a URL, a file or standard input.
Only a URL can be read again without the user.

`axon login` is the command that already touches the network to put things
right. `axon sky` runs in scripts and makes a single request to the API
server before each command.

## Rationale

When the file that holds the pathway a login uses came from an address, that
`axon login` fetches it again, once the pathway is determined and before it
connects, and replaces the stored copy when the new one passes the checks
and keeps its `id`. Other stored files are not fetched. A file that `--config`
added in the same login is not fetched again, and when the fetched file
drops the pathway, the pathway chosen in its place is used from its stored
copy, so a login refreshes at most one stored file, apart from adding the
one `--config` gives. When the fetch fails or the file is refused, `axon` says
so in one line and logs in with the stored file. A file or standard input is
read once; the user runs `axon login --config` again when the organization
hands out a new file.

- A change the organization publishes to a file reaches each laptop at
  the next login that uses a pathway of that file and fetches it
  successfully, with nothing for the user to do.
- A login does not fail because the configuration's host is briefly out of
  reach while the stored file still works.
- `axon sky` adds no request for the configuration, and keeps its
  predictable running time.
- A login reaches only the hosts of the pathway in use; a file the user is
  not using is not fetched, and its host being down costs nothing.
- A file source is never read again behind the user's back, so a moved or
  deleted download does not silently change or break the setup.

## Alternatives considered

- **Fetch only when the user gives `--config` again.** Every change needs a
  message to every member, and laptops that miss it keep stale hosts.
- **Fetch in `axon sky` too, when the stored copy is old.** A command in
  a script gains a second network request and a failure mode that has
  nothing to do with the command.
- **Read a file source again at every login.** A file that has moved or
  been deleted turns into a warning at each login, or a silent fallback.
