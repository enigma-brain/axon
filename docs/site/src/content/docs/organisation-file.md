---
title: Writing an organisation file
description: How an organisation describes itself to axon, with a complete example for each kind of pathway.
---

`axon` names no organisation and has no default for any detail of one. It
takes every such detail from an **organisation file**: a small TOML file that
your organisation writes and gives to its members. This page is for the person
who writes it.

An organisation file holds the pathways your members use to reach your
SkyPilot API server, the addresses and names those pathways need, and the text
`axon` shows your members about them. Your members add the file with
`axon login --org`, as [Logging in](/axon/logging-in/) describes.

## What you decide first

**One file or several.** A file holds one pathway or several, and a laptop
holds any number of files.

- Give each pathway **its own file** when most of your members use only one.
  Those members hold one file with one pathway, and never meet a prompt.
- Put several pathways **in one file** when the same members use all of them.
  The first login then asks which they are on, once.

A member who uses several pathways can hold several files. Pathway
identifiers must be different across every file a laptop holds.

**An address or a file you hand out.** Your members add the file from a source:

| Source | Fits when | Members run |
|---|---|---|
| An `https` address | You can publish the file where members reach it, without credentials | `axon login --org https://example.org/axon.toml` |
| A file path | You send the file by e-mail, chat or a shared drive | `axon login --org ~/Downloads/axon.toml` |
| Standard input | You hand the file out with a tool your members are already signed in to | `<command that prints the file> \| axon login --org -` |

The address is fetched without credentials, and must start with `https://`. It
matters for updates: a file added from an address is fetched again at each
login, so a change you publish reaches your members at their next login. A
file from a path or standard input is read once; a member updates it by
running `axon login --org` again with the new file.

Whatever the source, the file is trusted input. It decides which servers your
members' logins and requests go to, so publish it where only you can change it.

## A direct pathway

A direct pathway reaches the SkyPilot API server at its own address, and
signs in with the server's own login in a browser. The server may answer only
from your organisation's network; the optional `unreachable_hint` tells your
members what to do when it does not.

```toml
# campus.toml
format = 1
id = "example-campus"

[[pathway]]
id = "campus"
kind = "direct"
name = "Campus"
description = "Single sign-on in a browser. Needs the campus network or VPN."
unreachable_hint = "Connect to the campus VPN and retry."

[pathway.devenv]
endpoint = "https://sky.example.org"
```

## A Teleport pathway

A Teleport pathway reaches the API server through a Teleport application, from
wherever Teleport works. Your members sign in to Teleport, and `axon` runs
the Teleport steps for them. `proxy` is your Teleport proxy, and `app` is the
name of the Teleport application that serves the SkyPilot API server.

```toml
# remote.toml
format = 1
id = "example-remote"

[[pathway]]
id = "remote"
kind = "teleport"
name = "Remote"
description = "Teleport. Works from anywhere."

[pathway.teleport]
proxy = "teleport.example.org"

[pathway.devenv]
app = "skypilot"
```

## Several pathways in one file

Each pathway is a `[[pathway]]` entry. A table such as `[pathway.devenv]`
belongs to the `[[pathway]]` above it, so each pathway's tables follow its own
`[[pathway]]` line and come before the next one.

```toml
# both.toml
format = 1
id = "example-both"

[[pathway]]
id = "campus"
kind = "direct"
name = "Campus"
description = "Single sign-on in a browser. Needs the campus network or VPN."
unreachable_hint = "Connect to the campus VPN and retry."

[pathway.devenv]
endpoint = "https://sky.example.org"

[[pathway]]
id = "remote"
kind = "teleport"
name = "Remote"
description = "Teleport. Works from anywhere."

[pathway.teleport]
proxy = "teleport.example.org:443"

[pathway.devenv]
app = "skypilot"
```

With these two pathways stored, a member's first login asks:

```text
Which pathway are you on?
  1) Campus  Single sign-on in a browser. Needs the campus network or VPN.
  2) Remote  Teleport. Works from anywhere.
Pathway [1-2]:
```

## The fields

At the top of the file:

| Field | Holds |
|---|---|
| `format` | The file's format. It is `1`. |
| `id` | The file's identifier: lowercase letters, digits and hyphens, starting with a letter, at most 32 characters. `axon` stores the file as `~/.axon/orgs/<id>.toml`. A file with the same `id` replaces the stored one. |

Then one `[[pathway]]` entry for each pathway. Every pathway has these:

| Field | Required | Holds |
|---|---|---|
| `id` | Yes | The pathway's identifier: lowercase letters, digits and hyphens. It is the value of `--pathway`, and no two pathways on a laptop have the same one. |
| `kind` | Yes | `direct` or `teleport`. |
| `name` | Yes | The pathway's name, as the prompt shows it. |
| `description` | Yes | The prompt's line about the pathway. Say how members sign in and where the pathway works. |
| `unreachable_hint` | No | A sentence `axon` prints after `Can't reach <host>.` when a host the pathway names does not answer. |

`name`, `description` and `unreachable_hint` are text for people. `axon` prints
them as written, and never builds a sentence out of any other field.

A direct pathway has one more table, and a Teleport pathway two:

| Field | Kind | Required | Holds |
|---|---|---|---|
| `devenv.endpoint` | `direct` | Yes | The SkyPilot API server's `https` address. |
| `teleport.proxy` | `teleport` | Yes | Your Teleport proxy: a hostname, optionally with a port. |
| `devenv.app` | `teleport` | Yes | The name of the Teleport application that serves the SkyPilot API server. |

A table that does not belong to the pathway's kind, such as `teleport` on a
direct pathway, is refused.

## What axon checks

`axon` accepts a file only when:

- `format` is `1`;
- it has an `id` of the allowed form;
- it has at least one pathway;
- every pathway has an `id` of the allowed characters, a `kind` of `direct` or
  `teleport`, a `name` and a `description`;
- every `direct` pathway has `devenv.endpoint`, and it is an `https` address;
- every `teleport` pathway has `teleport.proxy` and `devenv.app`;
- it has no field or table that this page does not list;
- no pathway identifier appears twice in it, or in any other file stored on
  the laptop, apart from the one it replaces;
- it is no larger than 1 MiB.

A file that fails a check is refused whole, with a message that names the
field and the check, and nothing changes. When a member adds the file with
`--org`, the message begins `axon: add the organisation file:`. For example:

```text
axon: add the organisation file: pathway "campus": name: is required and must be text
axon: add the organisation file: pathway "campus": devenv.endpoint: must be an https address
axon: add the organisation file: colour: not a field of an organisation file
axon: add the organisation file: format: must be 1
```

[Troubleshooting](/axon/troubleshooting/#a-refused-organisation-file) lists the
others.

## Before you hand the file out

1. Try it yourself with `axon login --org <path>`. `axon` checks the file before
   it does anything else, so a mistake is refused at once, with the field
   named.
2. Publish the file, or hand it out, with the line your members run:

   ```sh
   axon login --org https://example.org/axon.toml
   ```

Your members need to [install `axon`](/axon/installing/) first.
