# What does this change do?

One paragraph about what a person who uses the software notices. The files a change touches are already in the
diff, so what this answer has to carry is the behaviour: what works that did not work, or what works
differently, in the words someone who is not reading the code would use.

## Which issue does this change carry?

The link to the issue the owner assigned for this change. A change that answers a report which no one agreed
to has nothing to be reviewed against.

## How was it tested?

Which suites were run, and which test covers the change. `go run ./cmd/build test` runs both suites and
compiles nothing, and it is the whole run a reviewer can repeat.

## What does the change need from the maintainer?

Anything the change cannot carry in itself: a migration note, a chapter of the administrator guide or the
listener guide, a configuration example, a release entry.

## Agreement

- [ ] I offer this contribution under the [Individual Contributor Assignment Agreement][icaa], which I read
  before writing the change.
- [ ] I follow the [code of conduct][conduct] in this pull request and in the discussion around it.

[icaa]: https://github.com/chuot/rdio-scanner/blob/main/ICAA.md
[conduct]: https://github.com/chuot/rdio-scanner/blob/main/CODE_OF_CONDUCT.md
