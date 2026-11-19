# Pathways are of two kinds, direct and teleport

## Context

`axon` serves any organisation that runs a SkyPilot API server, and each
organisation reaches its server in its own ways. The ways in use differ in
kind, not only in name. In one, the laptop reaches the server's URL itself,
from a network the organisation controls, and the server runs its own
browser login. In the other, the laptop logs in to a Teleport proxy and
reaches the server through a local tunnel to a Teleport application.

Each kind needs its own steps in `axon login`, its own connection check and
its own messages. What an organisation has to say about a pathway follows
from its kind: the server's address for the first, a proxy and an
application for the second.

## Rationale

`axon` knows two kinds of pathway, `direct` and `teleport`, and an
organisation's configuration names its pathways and gives each a kind with
the fields that kind needs.

- The steps of each kind are `axon`'s code; the names, hosts and wording
  are the organisation's data. No organisation detail is needed in the code
  to support another organisation with the same kinds.
- The configuration is small and checkable: each kind has a fixed set of
  required fields.
- An organisation with several pathways of one kind, or with only one
  pathway, needs nothing new.
- The tunnel's local port is a property of `axon`, not of the organisation,
  so it stays fixed and needs no field.

## Alternatives considered

- **Keep the pathways built in and replace only the hostnames.** Every
  organisation would carry another's pathway names and login systems in its
  prompt and messages.
- **A general pathway described step by step in the configuration.** It
  turns the configuration into a program, and no third kind is known to be
  needed.
