# An interrupt cancels the command, which stops in order and ends axon by the signal

## Context

A user presses Ctrl-C to stop a command that waits: on a question, a
download, a server, or a program `axon` runs. A process manager or a script
sends SIGTERM for the same purpose. A process that dies where it stands
leaves what it had half made: a staged binary beside the one it replaces, a
partial download, an unrecorded tunnel. `axon`'s commands already undo a
step that fails, by the same code that runs when the step is cancelled.

The programs `axon` runs while it waits on them share its terminal and its
process group, so the terminal delivers Ctrl-C to them as well as to
`axon`. Some of them, such as `tsh login`, change the terminal's settings
while they run and put them back as they end. A shell running a script
stops the script when a command it ran was ended by SIGINT, and not when the
command exited with a status of its own.

## Rationale

The first SIGINT or SIGTERM cancels the command's context, with the signal
as the cause, and puts the signal's default action back.

- Each step already stops when its context ends and undoes what it began,
  so a cancelled command leaves the same state a failed one does, which the
  next command handles.
- The default action, restored at once, makes a second interrupt end `axon`
  wherever it is, so a step that does not stop cannot hold the user.
- A command that fails after the signal prints only `axon: interrupted`:
  the failure is the interrupt the user asked for, and the step's own error
  would describe it as something that went wrong.
- `axon` ends by re-raising the signal with its default action, so the
  shell sees a process ended by the signal, and a script that ran it stops,
  as it does for any other program interrupted the same way.
- A command that returned without an error had finished its work and
  printed its result before it acted on the signal. It ends with success,
  since the work is done and nothing was interrupted.
- On SIGINT, `axon` sends nothing to a program it runs: the terminal has
  delivered SIGINT to it already, and a second SIGINT makes some programs
  abandon their own cleanup. On SIGTERM, which reaches `axon` alone, `axon`
  passes it on. Either way `axon` waits for the program, up to a grace
  period, so that a program on the terminal puts the terminal back before
  `axon` exits.

## Alternatives considered

- **Leave the default action, so an interrupt kills `axon` at once.** A
  killed command leaves temporary files and half-made state behind, and
  the cleanup each step has never runs.
- **Exit with 128 plus the signal's number.** The shell prints the same
  status, but a script running `axon` carries on as after an ordinary
  failure.
- **Print the interrupted step's own error.** It reads as a failure to fix,
  such as a download that could not complete, when the user stopped it.
- **End an interrupted command by the signal even when it succeeded.** The
  shell would report as interrupted a command whose work is done.
- **Send every program `axon` runs the signal `axon` received.** Programs
  on the terminal receive SIGINT twice, and a second SIGINT stops some of
  them in the middle of their own cleanup.
- **Kill every program `axon` runs when the command is cancelled.** A
  program that changed the terminal's settings ends without putting them
  back.
