# Does one login connect every command group, or is a command group connected on its own?

## Context

`axon login` establishes the pathway's session and then connects every
command group, in one command. A user logs in once and reaches each part of
the platform through a command group. The pathway's session, such as a
Teleport login, is shared by every group reached through the pathway, so it
belongs to the login and not to any one group.

With one command group, connecting every group and connecting that group
are the same thing, and the question does not arise. With a second group,
two cases arise: a user who needs the pathway's session without connecting
a given group, and a user who needs one group connected on its own, such as
when that group's connection fails and the others are good.

The pathway's session and each group's connect step are already separate
stages of `axon login`, so a command that connects one group reuses the
stage as it is and changes nothing for a user of `axon login`.

## Options

- **One login that connects every group.** The behavior with one group.
  One command to learn, and one place where a connection is repaired. With
  several groups, a user who needs one of them waits for every group's
  connect step, and a failure in one group fails the login.
- **A login for the pathway alone, followed by a login for each group.**
  Each step is explicit and a group is connected only when the user asks.
  A user learns one command per group, and a new user who runs only the
  pathway's login reaches no group.
- **One login that connects every group, with an added command that
  connects a single group.** `axon login` stays the one command a new user
  runs; the added command repairs or connects one group without the others.
  Two commands reach the same connect step, and each has to say which
  groups it connects.

## Recommendation

Keep one login that connects every group, and add a command that connects a
single group once a second command group exists. One login keeps the first
run simple, and the added command covers the user who needs one group,
without changing what `axon login` does.

## Resolution path

- A second command group, whose connect step is independent of the first
  group's.
- Or a user who needs the pathway's session without connecting a command
  group, and describes why.
