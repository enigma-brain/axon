# axon login makes a pathway from the server's address, with no config file

## Context

A config file reaches a laptop from an address, a file or standard input.
An organization that keeps its hostnames private hands the file out through
a tool or an account its members are already signed in to. A new member,
or a member of a small organization, does not always have that tool or
account on the day they start, and a small organization has no file to
hand out at all. What such a user does have is the server's address, or the
Teleport proxy's.

A pathway needs little: its kind, the server's address or the proxy and
application, and a name. Everything else in a config file is text for
people. The rest of `axon`, from choosing the pathway to `axon logout`,
works on stored config files.

## Rationale

With no pathway stored and a terminal, `axon login` asks for the kind, the
address and a name, with a default for each question that has one, and
takes the same answers from flags for scripts. It checks that the server
answers before it stores anything, and stores the pathway as a config file
of the same format, written by `axon` into the same directory, with a record
that says it is hand-made.

- A user who has the address logs in with one command and no file, and a
  first login on a terminal leads with questions that user is able to
  answer.
- A config file stays the way for an organization with many members: it
  gives every member the same pathways, in the organization's own words,
  and updates them from an address.
- A pathway stored as a config file is a pathway like any other: choosing,
  switching, refreshing and logging out need no second code path, and a
  user can read and copy the file.
- Checking the server first means that a typo is caught while the user is
  still answering, and nothing broken is left stored.
- The prompt appears only when nothing is stored, so a user with a pathway
  never meets it, and a login in a script never waits on it.

## Alternatives considered

- **A config file as the only way in.** A user without the tool or account
  the organization hands its file out through cannot log in at all, and a
  small organization has to write and host a file for one server.
- **A separate store for hand-made pathways.** Every reader of the stored
  pathways, from choosing to logging out, would read two stores with two
  formats, and moving a pathway between them would be a conversion.
- **Flags only, with no prompt.** A first-time user has to learn the flags
  from the documentation before the first login works.
