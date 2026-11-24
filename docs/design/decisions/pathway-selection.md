# The active pathway is chosen at login and stored

## Context

A user reaches an organization's SkyPilot API server by one of the pathways
named in the config files stored on the laptop. A pathway works only
from where its kind allows: a direct pathway from the network the
organization's server answers on, a teleport pathway from anywhere. Most
users have an account on one pathway, and it rarely changes. `axon login`
runs different steps on each kind, so it has to know which pathway applies
before it does anything else.

## Rationale

When no pathway is active, `axon login` asks which of the stored pathways
the user is on, offering each by its name with the one-line description its
file gives, and saves the answer in `axon`'s own settings file. A user
who follows the setup instructions with no prior knowledge gets a question
they are able to answer, in the organization's own words, not an error. The
answer is asked for once, because it is a fact about the user and not about
the session. When the stored files hold a single pathway there is nothing
to ask.

The `--pathway` flag takes a pathway's identifier from any stored file,
skips the prompt and makes that pathway the active one. One mechanism
serves scripted setup, a wrong first answer, and a user who moves from one
pathway to another.

`axon login --config` without `--pathway` makes a pathway of the added file
active, unless the active pathway is already in that file. A new user sets
up with one command, and `--pathway` is left for moving between pathways
already stored.

## Alternatives considered

- **Flag only.** The first `axon login` fails until `--pathway` is given, so
  every new user meets an error before anything works.
- **Detect the pathway from an existing `tsh` profile.** A teleport user who
  has no `tsh` profile yet is sent down a direct pathway without being told,
  where login hangs off the organization's network.
- **Adding a file never changes the active pathway.** A user who adds a
  file to use it runs a second command, `--pathway`, to reach it.
- **Fixed pathway names in `axon`.** Every organization's users meet another
  organization's names in the prompt and in `--pathway`.
