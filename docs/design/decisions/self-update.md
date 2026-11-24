# axon updates itself with a built-in command

## Context

`axon` is installed by a line that downloads the latest release's
`install.sh` and runs it, and running the line again updates `axon`. Users do
not keep the line at hand once `axon` is installed, so updating starts with
finding it again. `axon` already keeps the clients it installs for its own
use in step with what they serve, and has no way to update itself.

A release holds an archive per platform and a checksum file, and GitHub
serves its latest full release, never a pre-release, at a fixed address.
`axon` runs on macOS and Linux, where a running program's file can be
replaced by renaming a new file over it in the same directory.

## Rationale

`axon update` downloads the release, verifies it and replaces the running
binary, in Go, inside `axon`. `--version` names another release, such as a
pre-release. `axon` contacts its releases only when the user runs the
command.

- The user runs a command `axon --help` lists, not a line kept elsewhere.
- It needs nothing on the laptop beyond `axon`: no `curl`, no shell and no
  checksum tool.
- It follows the same rules as `install.sh`, so both install the same
  archive from the same release, verified the same way.
- It prints its own lines, which name the versions it moved between and,
  on a failure, the step that failed.
- It replaces the file it runs from, wherever that is, and checks that the
  new binary runs before the rename, so a failure leaves the old binary
  working.
- Updating stays the user's choice. No command contacts the release server
  unasked, so `axon` sends no request the user did not start and a working
  `axon` does not change under the user.
- The install line keeps working, and also updates `axon`.

## Alternatives considered

- **Have `axon update` download and run the release's `install.sh`.** It is
  a few lines and reuses a tested script, but it needs `curl` and a shell,
  prints the installer's output, and replaces `~/.local/bin/axon` rather
  than the binary that ran.
- **Update automatically, or check for a new release and report it.** Every
  command would contact the release server, and `axon` would change, or
  print notices, without being asked.
- **Leave updating to the install line.** Users have to find the line again
  for each update.
