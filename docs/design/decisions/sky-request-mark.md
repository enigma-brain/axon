# axon marks its requests with a label in SkyPilot's project configuration

## Context

An organization can hold what is launched through `axon` to rules that do not
apply to SkyPilot used directly. The rules are enforced by the admin policy of
its API server, which sees a request's task, its SkyPilot configuration and its
user, and nothing that tells which program sent it.

## Rationale

`axon sky` runs the client with a project configuration file of its own, which
sets one Kubernetes label. SkyPilot merges the file with the user's settings
and with `--config`, and sends the result with the request.

- The arguments stay unchanged, for every command.
- It works with the user's settings file and with both forms of `--config`.
- The label is on the pods too, so launches through `axon` can be found there.
- A server whose policy does not look for the label treats it as any label.

It is a mark and not a proof: a user can send a request without it, with the
SkyPilot client itself.

## Alternatives considered

- **Add `--config key=value` to the arguments.** SkyPilot refuses it beside a
  `--config` that names a file.
- **An environment variable.** SkyPilot sends it, and does not give it to the
  admin policy.
- **A setting per pathway.** Every pathway of an organization gets the same
  rules, and the policy decides what the mark means.
