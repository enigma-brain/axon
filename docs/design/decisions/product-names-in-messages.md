# A message names a product only from the part of axon that uses it

## Context

`axon` is built on other products: SkyPilot behind `sky`, Teleport on a
`teleport` pathway. Some messages come from those parts, and others from
the general parts of `axon`: the stages of login, the pathway prompt, the
organization's configuration and the form of a failure.

A user of a `direct` pathway has no Teleport. A user who runs
`axon login` is setting up `axon`, not a product behind it. `axon` exists
so that what sits behind a command group is free to change.

## Rationale

A message names a product only when it comes from the part of `axon` that
uses that product. "SkyPilot" appears only in messages from the `sky` group;
"Teleport" only in messages about a `teleport` pathway. The general parts
name neither.

- A user meets the name of a product only where that product is the
  thing they act on, such as a Teleport session to renew.
- A user of a `direct` pathway never reads about Teleport.
- The general parts read true whichever products sit behind them.

## Alternatives considered

- **Name products wherever it reads naturally.** The general parts carry
  the names of what sits behind them, and a `direct` user meets Teleport in
  the prompt or in setup messages.
- **Never name a product.** A message about an expired Teleport session or
  a SkyPilot server that does not answer loses the word the user needs to
  act on it.
