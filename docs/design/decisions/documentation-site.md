# axon's documentation is a public static site published with GitHub Pages

## Context

`axon` is used by the members of every organization that adopts it. They
have no access to anything `axon`'s maintainers keep private, and they need
to read how to install `axon`, log in and use it.

The documentation holds nothing about any one organization: an organization's
own details come from its configuration, so there is nothing in the pages that
needs hiding.

## Rationale

The documentation is a static site, public, with its source in the `axon`
repository and its pages served by GitHub Pages.

- A user reads the documentation without access to anything private.
- Navigation and search make it a user guide and not a set of developer notes.
- The site's source sits beside the code, so a change to a command and the
  change to its page go through one review.
- GitHub Pages needs no hosting outside the repository's own platform.

## Alternatives considered

- **Markdown pages in the repository only.** There is no navigation or search,
  and the pages read as developer notes and not as user documentation.
- **A private site.** It shuts out the users the documentation is for, who
  have no access to anything private.
