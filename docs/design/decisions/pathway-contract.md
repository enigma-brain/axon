# Every pathway has identity, text for people and settings for machines

## Context

An organisation's configuration describes each of its pathways. Some of
what it says identifies the pathway, some is shown to the user, and some
tells `axon` where to connect. Pathways of different kinds need different
settings. Several command groups can use one pathway, and each finds its
own service on it.

Text that `axon` shows comes from many organisations, written in their own
words, and an organisation knows best how to tell its users what to do.

## Rationale

Every pathway, of any kind, has the same three parts:

- **Identity**: `id` and `kind`.
- **Text for people**: `name`, `description` and an optional
  `unreachable_hint`, each a whole label or sentence that the organisation
  writes and `axon` prints as written.
- **Settings for machines**: a table named for the kind, holding what only
  that kind needs, and a table for each command group, holding where that
  group finds its service on the pathway.

Reasons:

- A pathway is a fact about the user that every command group uses. The
  API server's address and the Teleport application are facts about where
  `sky`'s service is, not about the pathway, so they sit in `sky`'s
  table. A second command group adds its own table and changes nothing
  else.
- `axon` never builds a sentence out of a configuration value, so no
  organisation's wording has to fit a sentence `axon` wrote, and no
  message reads wrongly because a value was a fragment of the wrong shape.
- One shape for every kind makes the checks uniform: each field has one
  type and one place.
- A kind with no settings of its own has no kind table, so nothing is
  empty.

## Alternatives considered

- **Flat fields on each pathway.** The fields of every command group and
  every kind mix in one list, and nothing shows which belong to which.
- **A field that names a network, set into `axon`'s own sentence.** Each
  organisation's value has to fit `axon`'s grammar, and a value that does
  not reads as a broken message.
- **Settings for each command group grouped under the kind.** The address
  of a service would read as a property of the way the user logs in.
