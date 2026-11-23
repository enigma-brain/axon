# Adding a pathway that clashes with a stored one asks before replacing it

## Context

A laptop holds several config files, from one organization or several, and
hand-made pathways beside them. Two of them can name the same pathway, or
reach the same server: an organization's file arrives after the user made
the pathway by hand, or an organization renames its file. Two stored
pathways with one name are indistinguishable at the prompt that offers
them, and two with one identifier cannot both be chosen with `--pathway`.

An organization changes its file over time and hands out new copies, and a
laptop can be given an older copy than the one it holds. Nothing in a file
without a revision says which of two copies is newer.

## Rationale

An addition that has the name, the identifier or the server of a stored
pathway in another file asks whether to replace that file, saying exactly
what would be removed, and the default answer is no. A clash of name or
identifier cannot be kept, so no to it leaves the config file out, or asks a
hand-made pathway for another name; a clash of server alone keeps both.
`--replace` answers yes in advance, and with no terminal a clash stops with a
message that names it. A config file can carry a `revision`; an update of a
stored file asks only when its revision is lower than the stored copy's.

- The user decides what leaves the laptop, and sees exactly what it is
  before deciding, including the other pathways of a file that goes with
  the one that clashes.
- No is the safe default: an accidental Enter keeps a working setup.
- No addition fails outright on an identifier the user cannot see, and the
  question tells the user how to go on.
- An update of the same file, the common case, is silent, as it is when
  an organization's file is fetched again at login; only a step back in
  revision, which is likely a stale copy, asks.
- Scripts get a flag rather than a question they cannot answer.

## Alternatives considered

- **Refusing a clash.** The user has to find and delete the stored file by
  hand, and learn its path and identifier, before the addition works.
- **The organization's file silently taking over.** A pathway the user made
  or chose disappears without a word, and the active one can be left under
  the user.
- **Replacing whatever has the same file identifier, as before.** Whether
  an addition replaced or was refused depended on an identifier the user
  never sees.
