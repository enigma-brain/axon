# A laptop holds several organisation files and switches between their pathways

## Context

An organisation's configuration reaches a laptop as a file, and an
organisation can split its pathways across several files, for instance one
pathway per file, so that each member receives only the file for their
pathway and never meets a prompt. Some people use more than one pathway, of
one organisation or of several, and move between them from time to time.

Each pathway is chosen by its identifier with `--pathway`, and only one
pathway is active at a time.

## Rationale

`axon` stores every organisation file it is given in `~/.axon/orgs/`, one
file each, named by an `id` that the file carries. Adding a file leaves the
others in place; adding one with a stored `id` replaces that one. The
pathways of all stored files form one set to choose from, so pathway
identifiers are unique across the files, and a file that would break that
is refused without changing anything. Removing a file is deleting it.

- A person on more than one pathway switches with `--pathway`, without
  setting up again or fetching a file each time.
- An organisation can give each pathway its own file, so most members hold
  one file with one pathway and never see a prompt.
- A file's own `id` decides which stored file it replaces, so running
  `--org` again with a newer copy updates it in place, whatever its source.
- Unique pathway identifiers keep `--pathway` a single, unambiguous name.
- Deleting a file needs no command, because the directory is the list.

## Alternatives considered

- **One stored file, replaced by each `--org`.** A person on two pathways
  in different files sets up again at every switch.
- **Several files named by their source.** The same file from a new
  address, or from a download, would be stored twice.
- **Pathway identifiers qualified by the file, such as `file/pathway`.**
  Every `--pathway` grows longer for the uncommon case of a clash, which the
  organisations can avoid by choosing their identifiers.
- **A command to list and remove files.** It adds commands and their
  failure cases for an operation that is deleting a file.
