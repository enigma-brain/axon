# axon is installed by a script downloaded from the release with curl

## Context

`axon` is a single binary, released for several platforms from a public
GitHub repository. Its users are members of organizations that adopt it,
with no account in the GitHub organization that hosts it. A public
release's files have plain download addresses, and the latest release's
files have addresses that do not change between releases.

The binaries are not signed or notarized. macOS blocks an unsigned binary
that arrives through a browser and runs one that is fetched from a
terminal.

Updates to `axon` are infrequent, because the SkyPilot client version
follows the server without an `axon` release.

## Rationale

Each release carries `install.sh` beside its archives. The readme gives one
line that downloads the latest release's `install.sh` with `curl` and runs
it. The script detects the operating system and CPU, downloads the matching
archive and the checksum file from the release with `curl`, verifies the
archive, and puts `axon` in `~/.local/bin`.

- One line serves every platform; the user makes no choice.
- It needs no account and no tool beyond `curl` and a checksum tool, which
  macOS and common Linux distributions have.
- The script comes from the same release as the archives, so it always
  matches them.
- The download is verified before anything is installed.
- Nothing needs `sudo`, and the script says what to add when the directory
  is not on `PATH`.
- Fetching from the terminal avoids the macOS block on unsigned downloads.
- Updating is running the same line again, so there is one procedure to
  document and support.

## Alternatives considered

- **Fetch the script with `gh` from the repository.** It requires the GitHub
  CLI, signed in, for a repository that needs no credentials.
- **Download the script from the default branch.** The script can change
  ahead of the release it installs.
- **A Homebrew tap.** Most Linux and WSL users have no Homebrew, so a second
  method is needed anyway.
- **`go install`.** It needs Go on the laptop and builds without the
  release's version.
- **A built-in upgrade command beside the script.** It adds a command and
  its failure cases for an operation that is rare.
