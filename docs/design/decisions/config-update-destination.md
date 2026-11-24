# A fetched config file that changes where the pathway in use goes is asked about first

## Context

`axon login` fetches the file that holds the pathway in use again from its
address, and stores the new copy without the user. Whoever controls that
address, or the host that serves it, then decides which server the user's
next login and every later `axon sky` command reaches, and so where the
user's credentials and work go. A legitimate move of a server is rare and
announced; a silent one is the most a compromised address can do.

What makes a pathway reach a server is its kind, its API server's address
on a `direct` pathway, and its Teleport proxy and application on a
`teleport` pathway. A file can change these for the pathway in use, or drop
the pathway and offer another with new servers in its place, which a login
then chooses without asking when it is the only one left. Names, texts and
other pathways change more often and reach no new server.

A file the user adds with `--config` is the user's own act: they chose the
source in that command.

## Rationale

A fetched file whose pathway in use has another kind, address, proxy or
application than the stored copy's, or that no longer has that pathway, is
not stored until the user agrees. `axon` prints the old and the new values
and asks whether to switch, with no as the default. No keeps the stored
copy whole and the login goes on with it. With no terminal, the stored
copy is kept, with one line that names `--replace`, and `--replace` answers
yes. Values are compared as a clash compares servers, so the same server
written another way is no change. A file added with `--config` asks nothing
new and prints the old and the new values.

- A change of server needs a person who sees both values, so a changed
  file at the address alone cannot move a user to another server.
- A dropped pathway is the same change by another route: without the
  question, the file's new pathway is chosen in its place.
- No is the safe default: the stored copy keeps working, and an accidental
  Enter changes nothing.
- A change to names, text or other pathways still reaches every laptop at
  the next login with nothing to do, and so does a server move the user
  accepts.
- A script with no terminal keeps working on the servers it knew, and
  `--replace` is the one flag a script passes to follow a move, as it
  already is for the other questions about replacing what is stored.
- `--config` already names its source in the command, so a question there
  asks the user to confirm what they just typed; printing the values still
  shows a move they did not expect.

## Alternatives considered

- **Store every fetched file, as for a change of text.** A changed file at
  the address moves every user to another server with no one seeing it.
- **Refuse a fetched file that changes where the pathway goes.** A real
  server move then needs every member to run `axon login --config`
  again, by hand, with the address they already have.
- **Ask only when the pathway's values change, not when it is dropped.** A
  file that renames the pathway and points the new one elsewhere is chosen
  without a question, which defeats the question.
- **Pin the servers a file names, or sign the file.** Either needs
  something the organization sets up and maintains beyond publishing a
  file, and changes the file format.
- **Ask with yes as the default.** An accidental Enter or a habit of
  pressing it moves the user, and the question protects nothing.
