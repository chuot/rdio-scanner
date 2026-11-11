# Contributing to [Rdio Scanner](https://github.com/chuot/rdio-scanner)

Rdio Scanner is a self-hosted call scanner: a Go server that turns watched recordings into call history and
live listeners, a listening screen and an administration console built as browser-standard custom elements, and
the documentation that carries both. Contributing here means changing any part of that, or the help people get
in using it.

Four documents hold what this guide does not restate: [how to build and verify the tree](./COMPILING.md),
[how to report a security problem](./SECURITY.md), [the code of conduct](./CODE_OF_CONDUCT.md), and
[the agreement every contribution arrives under](./ICAA.md). The server, both web applications, and every
contribution to them are under the [GNU General Public License, version 3](./LICENSE).

## Ways to contribute

- **Code**: the server, the two web applications, and the build tool.
- **Documentation**: the administrator guide, the listener guide, and the published API contract, all under
  `docs/`.
- **Answering**: a settled answer in [Discussions](https://github.com/chuot/rdio-scanner/discussions) saves
  the next person the report they were about to open.
- **Triaging**: a report that arrives with the version, the configuration, and the log line it names is a
  report a review can start from.
- **Sponsoring**: the project is funded through [GitHub Sponsors](https://github.com/sponsors/chuot), which is
  what pays for the time the release targets are checked on.

There is no roadmap a contributor can claim a slot on. A change is discussed in its own issue, and the owner
decides what the project carries.

## Where a question goes

A how-to question, a setup question, or an idea that is not yet a change belongs in
[Discussions](https://github.com/chuot/rdio-scanner/discussions). Discussions is where an answer ends up where
the next person searching will find it; an issue is where a change waits. A question opened as an issue is
closed with the `invalid` label and a pointer to the discussion it belongs in.

## Bug reports and feature requests

Three forms open from the [issue chooser](https://github.com/chuot/rdio-scanner/issues/new/choose), and blank
issues are switched off so that a report arrives with what a review needs.

- **Bug report**: the server, the administration console, the listening screen in a browser, and the
  documentation.
- **Feature request**: a change you would like this project to make.
- **Native app report**: the two native applications, which are published outside this repository on the App
  Store and Google Play.

A suspected vulnerability never goes through an issue. [The security policy](./SECURITY.md) is the only route
for one: it takes the report privately, at the address it names, and acknowledges it within 72 hours.

## An issue first

Open an issue, and have it assigned to you, before you write code. Without an assigned issue, a pull request
is closed unread: the discussion that decides whether a change belongs in this project happens in the issue,
and code written before that discussion is code someone has to throw away.

The owner assigns issues. Assignment is where a contributor learns the limits a change has to respect, and it
is the point at which the work is agreed to.

## Finding work

Two labels mark an issue a contributor can take:
[`help wanted`](https://github.com/chuot/rdio-scanner/issues?q=is%3Aissue+is%3Aopen+label%3A%22help+wanted%22),
which this repository describes as extra attention being needed, and
[`good first issue`](https://github.com/chuot/rdio-scanner/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22),
which it describes as good for newcomers. Nothing else is offered to a contributor by name, so the rest of the
work is found by reading the code and picking a report you can settle on your own.

A first change is a fix that sits in one screen or one message: a label that reads wrong, a sentence in a guide
that no longer matches the flag it describes, a report that needs the log line it is missing. A change that
spans both web applications is not a first change.

## Set up and build

The prerequisites are the list at the top of [the compile guide](./COMPILING.md), and that guide names the
versions the official build is compiled against. The official build happens on the current Fedora Workstation,
and another platform works as long as those prerequisites are installed.

A run that names no command builds this machine's whole tree. The lines a contributor works from are:

```text
go run ./cmd/build            # both applications, this machine's ffmpeg, then the server
go run ./cmd/build client     # both applications
go run ./cmd/build server     # the server binary, against the bundles on disk
go run ./cmd/build version    # the version this tree resolves
go run ./cmd/build test       # this project's suites, and nothing compiled
```

Every command the build tool answers for, the flags each one takes, what each exit code says, and how the
release matrix cross-builds are written in the compile guide, and this guide does not repeat them.

## Tests

One command runs both suites and compiles nothing:

```text
go run ./cmd/build test
```

The same suites, run by hand, are the Go suites from the repository root and the frontend suite from the npm
workspace at `internal/presentation`:

```text
go test ./...
npm test
```

The build tool runs the two suite commands in that order and stops at the first that fails, so a run that names
the test command reports the Go suites before it reports the frontend suite.

A change carries its own test, in the place its kind of code is tested.

- **Go code**: a test beside the package it covers, in that package's own folder.
- **Frontend code**: a test under the web application it covers, in that application's `frontend/tests`
  folder inside `internal/presentation`.
- **A journey the acceptance suite protects**: the journey file under `tests/acceptance` that names it, which
  is where a change to that journey's behaviour lands.
- **A command, a flag, or a document a person follows**: the guard under `tests/docs` that reads the published
  document and the source it describes side by side.

## Code standards

- Go files are `gofmt`-formatted, and `go vet ./...` answers nothing on a tree a review accepts. Both are the
  Go toolchain's own: this project configures no linter and installs no formatter.
- The layers under `internal/` point inward. `internal/domain` is the business model and imports the standard
  library only; `internal/application` is one handler per command or query and imports the domain, its own
  ports, and the standard library; `internal/infrastructure` and `internal/presentation` are the adapters, and
  the wiring that joins them sits at `internal/app`. `internal/domain/imports_test.go` and
  `internal/application/imports_test.go` are the tests that refuse an import pointing the wrong way.
- The frontends are custom elements on the browser's own APIs. No framework runs in the browser: the npm
  workspace installs one package, `esbuild`, and installs it for the build only.
- A new Go file starts with the copyright and GNU GPLv3 notice every other source file carries, and nothing
  follows that notice. `tests/docs/licence_test.go` refuses a source file that states something after it.
- A comment says what the code does and why it does it that way. Nothing a person reads carries a note about
  the work that produced the change.

## Commits and pull requests

- A subject is one descriptive sentence naming what the change does: *Add issue forms for bug reports and
  feature requests*, *Guard the call history index set against an earlier server's database*. No `type:`,
  `fix:`, or `feat:` prefix, and no issue number in the subject. The history reads this way throughout, and a
  new change follows it.
- Branch from the repository's default branch, and open the pull request against that same branch. A release line
  is where the maintainer carries accepted work, so a contributor neither branches from one nor aims a pull
  request at one.
- One pull request per issue assigned to you, small enough that one person can read it in a sitting.
- The [pull request template](./.github/pull_request_template.md) is the body a pull request opens with, and
  what you write there is what the review reads: what the change does, which issue it carries, how it was
  tested, and what the maintainer has to add around it.

## After you submit

- The owner reads the issue, assigns it, and closes it when the change lands or when the project decides not
  to make the change. There is no review queue with a promised turnaround, and no change is owed a place in a
  release.
- The [code of conduct](./CODE_OF_CONDUCT.md) is enforced by Saubeo Solutions, which owns and maintains this
  project: it may remove, edit, or reject a comment, an issue, or a change that breaks it, and a conduct report
  goes privately to the address that document names.
- An issue idle for 180 days is marked stale and closed in the same run, as a change this project does not plan;
  a discussion idle for the same number of days is closed by its own workflow. Neither rule ever marks or closes
  a pull request. A report that is still true after being closed is opened again.
- A suspected vulnerability is answered inside the window the security policy states, and nothing about it
  reaches an issue, a discussion, or a release note until the owner decides it can.
- A change that lands stays reachable: its issue and its pull request stay linked to it, and the release entry
  of the version that carries it names it.

## Contributor agreement

Read [the agreement](./ICAA.md) before writing code, not when the pull request is opened. Rdio Scanner is
owned by Saubeo Solutions, and the agreement is what lets one owner hold the rights the project needs to stay
one project.

In plain terms, a contribution submitted under it:

- assigns its copyright to Saubeo Solutions, worldwide and for the full term of those rights, including the
  right to relicense the project later;
- grants a perpetual, worldwide, royalty-free patent licence for what the contribution brings;
- waives any moral rights in it, the rights of attribution and of integrity, where the law allows that waiver,
  and asks that they not be asserted where it does not.

Submitting the pull request is the acceptance of those terms. There is no separate form to sign and send.
