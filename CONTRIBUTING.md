# Contributing

This file sets out how this repository records what the system is meant to
be, how that record relates to the code and to what is running, and the
rules that keep them consistent. Read it before editing anything under
`docs/`. READMEs are operating guides and are not covered here.

## Three layers

The work spans three layers:

- **Intent**: what the system is meant to be. It lives in
  `docs/design/`, with [`DESIGN.md`](docs/design/DESIGN.md) as its
  authority.
- **Implementation**: the code and configuration in this repository that
  realize the intent.
- **World state**: what running the implementation produces. It is
  observed, not recorded.

A change starts at any layer and propagates to the others. A new
requirement starts in intent and moves into implementation; a fix found in
the code moves back into intent; an incident observed in the world moves
into both. The layers drift apart while a change propagates, and that drift
is legitimate as long as the tracker holds the work that closes it. At a
checkpoint, such as a tag or a release, all three agree.

## DESIGN

`docs/design/DESIGN.md` is the single authority for intent. It describes the
desired state, never the current moment: no "currently", no "for now", no
rollout status. It reads true whether or not a given piece is built or
deployed. Where it is silent, the choice is a free parameter, left to the
implementation.

What DESIGN states is the whole of its scope: anything it does not state is
unconstrained. DESIGN is organized however suits the system.

DESIGN opens with a short legend explaining its citations and pointing here.

## Questions and decisions

A question is a topic that needs external input, or has unknowns to resolve,
before DESIGN can settle it. A decision is a settled choice that was made
between meaningful alternatives. Each is one file:

- `docs/design/questions/<slug>.md`
- `docs/design/decisions/<slug>.md`

The slug names the topic, not the answer (`cli-transport`, not
`cli-uses-grpc`), so a question and the decision that resolves it share a
slug.

Neither is authoritative. They hold the reasoning around a topic; DESIGN
holds the outcome. They carry no status and no numbers, and they are edited
freely: git holds their history. They contain no links, to each other or to
DESIGN, so each reads on its own and deleting one breaks nothing but its
citation.

A question has these sections, all required:

- **Header**: the question itself, as the file's `#` title.
- **Context**: what makes it a question, and what constrains the answer.
- **Options**: the visible alternatives, each with its tradeoffs.
- **Recommendation**: the leaning answer with its reason, or `None`.
- **Resolution path**: the input or work that settles it.

A decision has these sections, all required:

- **Header**: the decision as a statement, as the file's `#` title.
- **Context**: the situation that prompted it and the constraints in play.
- **Rationale**: why this choice, on its own merits.
- **Alternatives considered**: each rejected alternative with a one-line
  reason.

## Citations

DESIGN cites a question as `{Q?}` and a decision as `{D!}`, each a Markdown
link to its file. What a statement means depends on its citation; here is
one topic at each stage:

| In DESIGN | Meaning |
|---|---|
| `The CLI's transport is an open question [{Q?}](questions/cli-transport.md).` | Open. Nothing is chosen yet. |
| `The CLI uses gRPC [{Q?}](questions/cli-transport.md).` | Provisional. The question gives the recommendation and the alternatives. |
| `The CLI uses gRPC [{D!}](decisions/cli-transport.md).` | Settled. The decision gives the rationale and the alternatives. |
| No statement. | Unconstrained, left to the implementation. |

A sentence carries at most one citation. Each question and decision file is
cited exactly once, at the place where DESIGN covers its topic. The citation
is the file's status: every file under `questions/` and `decisions/` is
cited, and removing a citation deletes its file in the same change.

## How a topic moves

When a topic needs external input or has unknowns to resolve, it is a
question: DESIGN states it, open or provisionally, and cites the question
file. When a choice is settled and there were meaningful alternatives, it is
a decision: DESIGN states the choice and cites the decision file. When a
choice is settled without meaningful alternatives, because it is a norm or
falls out of the architecture, it goes straight into DESIGN with no citation.

Each move happens in a single change:

- **Resolving a question** into a decision: delete the question file, create
  the decision file under the same slug, update DESIGN's statement, and
  replace `{Q?}` with `{D!}`.
- **Settling a question** whose answer has no meaningful alternatives:
  delete the question file, update DESIGN's statement to the answer, and
  remove the citation.
- **Dropping a question** that has become irrelevant: delete the file and
  remove the statement from DESIGN.
- **Revising a decision**: edit the decision file and DESIGN's statement
  together.
- **Retiring a decision** whose subject has left the design: delete the file
  and remove the statement from DESIGN.

## Writing style

Everything in the repository, from code and identifiers to messages,
documentation and commit messages, uses American spelling.

DESIGN, questions and decisions are written in the present tense, as plain
statements of fact: "the tunnel listens on port 46581", not "the tunnel
should listen" or "the tunnel will listen". They avoid modal language such as
"should", "may", "might" and "could". A rule is stated as what is the case;
an open point is a question, not a hedge.

DESIGN, questions and decisions are written for any organization that uses
`axon`. A reason is one that another organization adopting `axon` would
share: a reason that depends on one organization's systems, people, plans or
accounts is not given.

## Documentation site

The public documentation site is a [Starlight](https://starlight.astro.build)
project in `docs/site`. Its pages are Markdown files in
`docs/site/src/content/docs`. The Go build, tests and release do not need
Node; only the site does.

The site needs the Node version in `docs/site/.nvmrc`. Dependencies are
pinned to exact versions in `package.json`, with a committed
`package-lock.json`. Run these from `docs/site`:

    npm ci
    npm run build
    npm run dev

`npm run build` writes the site to `docs/site/dist` and fails on a broken
internal link. `npm run dev` serves it at `http://localhost:4321/axon/`;
the site lives under the `/axon/` path, so a link to another page of the site
starts with `/axon/`.

CI builds the site for every pull request that touches `docs/site`. A push to
`main` publishes it to GitHub Pages at `https://enigma-brain.github.io/axon/`
when Pages is switched on for the repository, and skips publishing, with a
note in the run summary, when it is not.

The site holds no organization detail, under the rule in
[What stays out](#what-stays-out). It links to SkyPilot's documentation
only from the page of the `sky` command group.

## Continuous integration

Every job runs on a standard GitHub-hosted runner. The release is built for
Linux on x86-64, macOS on Apple silicon and macOS on Intel. A pull request
and a push to `main` run jobs on Linux only; a change that does not build
for a Mac still fails there, since the release build cross-compiles every
archive. The jobs on a Mac, `macos-latest` for Apple silicon and
`macos-15-intel` for Intel, run in two places: in the release, as a gate
that every release passes before it is published, and in the Mac suite,
which a maintainer starts by hand.

A job in CI that downloads real software or builds the release runs only
when the paths it tests have changed. In `.github/workflows/ci.yml`, the
first job reads the change, the pull request's diff or the commits a push to
`main` added, and sets three flags:

- **code**: any `.go` file, `go.mod`, `go.sum`, anything under `internal/`,
  `install.sh`, `.goreleaser.yaml`, anything under `.github/release-gate/`
  or `ci.yml` itself;
- **skyclient**: anything under `internal/skyclient/`, or `ci.yml`;
- **update**: anything under `internal/update/`, `update_test.go`, or
  `ci.yml`.

When the change cannot be read, as on the first push of a branch, every
flag is set. A change to documentation alone sets none.

| Workflow | Job | Runners | Runs |
|---|---|---|---|
| CI | Build and test: `gofmt`, build, vet, `go test -race` | `ubuntu-latest` | For every pull request and push to `main` |
| CI | `shellcheck` on `install.sh` and the release gate's `curl` | `ubuntu-latest` | When **code** |
| CI | GoReleaser in snapshot mode, checking the archives | `ubuntu-latest` | When **code** |
| CI | The smoke test | `ubuntu-latest` | When **code** |
| CI | The real install of the SkyPilot client | `ubuntu-latest`, in the smoke test's job | When **skyclient** |
| CI | The real update: an old `axon` runs `axon update` against the latest release | `ubuntu-latest`, in the smoke test's job | When **update** |
| Docs | The site build | `ubuntu-latest` | For every pull request that touches `docs/site` or `docs.yml` |
| Pages | The site build and publishing | `ubuntu-latest` | For every push to `main` |
| Release | GoReleaser builds the release as a draft | `ubuntu-latest` | For every pushed tag `v*` |
| Release | The gate: the release's `install.sh`, `axon --version` and the smoke test against the installed binary, and `axon update` from an old `axon` to the draft | All three | After the draft is built |
| Release | Publishing the draft | `ubuntu-latest` | When the gate passed on all three |
| Release | The install line and `axon --version`, and `axon update --version <tag>` from an old `axon`, against the published release | `ubuntu-latest` | After the release is published |
| Mac | Build and test | `macos-latest` | When started by hand |
| Mac | The real install of the SkyPilot client, and the smoke test | `macos-latest` and `macos-15-intel` | When started by hand |

A job that does not run is reported as skipped, which a required check
accepts.

The smoke test, `TestSmokeLogin` in `smoke_test.go`, runs `axon` as a user
would, in an empty home directory with an empty `PATH`: `axon login --config`
adds a made-up config file, installs the SkyPilot client for real, and
stops at the endpoint's reachability check, the step before the browser
login. The endpoint is on the loopback address where nothing listens, so the
test reaches no server. It runs only with `AXON_REAL_INSTALL=1`, as the real
installs do, and runs the binary `AXON_SMOKE_BIN` names instead of building
one when that is set.

### The Mac suite

The Mac suite, `.github/workflows/mac.yml`, runs everything CI runs on Linux
except the checks of the release build, on a Mac, whatever the branch
changes. A maintainer starts it on a branch from the repository's Actions
page, under "Mac", or with:

    gh workflow run mac.yml --ref <branch>

Run it on a pull request's branch before merging when the change touches
`install.sh`, `internal/skyclient/`, or anything that
behaves differently on macOS, and on `main` before tagging a release that
holds such a change, so that the release gate does not find the failure
first. Run it as well to reproduce a failure reported on a Mac.

### The release gate

A pushed tag `v*` is built by GoReleaser into a draft release, which only
the repository's maintainers see and the install line never serves. The
draft's archives, `checksums.txt` and `install.sh` are handed, as a workflow
artifact, to a gate job on each of the three runners. There, the release's
own `install.sh` runs unchanged with `AXON_VERSION` set to the tag and a
stand-in for `curl`, `.github/release-gate/curl`, first on `PATH`: it
serves this tag's files from the artifact and fails for any other address.
The job then checks `axon --version` and runs the smoke test against the
binary `install.sh` installed. Then it runs `TestReleaseUpdate` in
`update_test.go`: an `axon` built from the tag's commit as version `0.0.0`,
whose release address is set at build time to a server on the loopback
address that serves the draft's files as GitHub serves a release, runs
`axon update --version <tag>` and `axon update`, and each must end with the
release's binary in its place. When the gate passes on all three, the draft
is published: a full release is marked as the latest unless a published
full release has a higher version, and a pre-release never is. Last, the
install line runs on Linux against the published release, and so does
`TestRealUpdate`, with `AXON_REAL_UPDATE=1`, which updates an old `axon` to
the tag from GitHub.

When a gate job fails, nothing is published and the workflow fails, with a
note in the run summary. A maintainer reads the failing job's log. A failure
of the runner or the network is re-run; once the gate passes, the release is
published. Tags publish one at a time; when a third waits behind two, GitHub
cancels the publish that waited longer, and that run is re-run the same way. Otherwise the draft is deleted on GitHub, the tag is left where
it is, and the fix is released under a new version.

The check after publishing cannot withdraw the release; a failure there
fails the workflow. A release that does not install from GitHub is edited
on GitHub to be a pre-release, which the install line does not pick up, so
that the line installs the previous release again; the fix is released
under a new version, and the tag is never moved.

## Contributing from outside

A change is proposed as a pull request against `main`. For a change to
behavior, or anything larger than a fix, an issue on the repository comes
first, to agree the direction before the work.

A change to what `axon` does comes with its change to DESIGN, and to the
question and decision files it touches, in the same pull request.

Contributions are made under the repository's MIT license.

## Commits and pull requests

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/)
(`type(scope): subject`) and stand alone: they describe the diff and
reference nothing outside it.

A change's reasoning, what it does and the alternatives weighed, is given in
its pull request. No spec or plan is committed.

## What stays out

No file in the repository, and no commit message, holds an organization
detail. What counts as one is listed in DESIGN's section
[Organization details](docs/design/DESIGN.md#organization-details).

No file in the repository, and no commit message, carries a tracker issue
identifier. A repository file reads without access to the tracker, and issue
numbers go stale in ways the file cannot detect. Tracker references belong
in branch names and pull requests. The maintainers keep the tracker, and an
outside contributor needs no access to it.

Code comments describe the code. They do not cite DESIGN, questions or
decisions, and they do not restate what DESIGN says.

Operating instructions, procedures and handoff notes belong in READMEs, not
in DESIGN.
