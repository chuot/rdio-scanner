# The scanner styling parity harness

`run.sh` builds the current scanner application and a build of the legacy Angular client from this repository's
history, serves both from the same Go server on two ports, and drives both in one headless Chrome against the same
seeded database. `parity.mjs` measures the same surfaces on both pages and writes the matrix.

```console
./testdata/parity/run.sh                 # build, serve, measure, compare, clean up
./testdata/parity/run.sh --keep          # leave the databases, the profile, and the matrix behind
./testdata/parity/run.sh --only snackbar # measure the pairs whose name contains "snackbar"
```

It needs the Go toolchain, `make`, node, and `google-chrome` on `PATH`. It adds nothing to
`internal/presentation/clients/package.json`: the probe speaks the DevTools protocol over Node's own `WebSocket`.

The reference build lives outside the repository, in `/tmp/opencode/rdio-parity`, and is extracted with
`git archive e89af2b^ client`. The legacy bundle is swapped into `internal/presentation/serving/scanner/bundle/`
only long enough to compile a server binary around it, and is put back before anything is measured.

## What it writes

Everything goes in one scratch directory, named on the last line the script prints:

```text
matrix/matrix.md        one row per surface, state, and viewport: legacy value, current value, delta
matrix/values.json      every measured box and computed style, for re-analysis
matrix/*.png            one screenshot per side, state, and viewport
logs/                   the two server logs
```

## Exit codes

| code | meaning |
| --- | --- |
| 0 | every measured row matched within tolerance |
| 1 | at least one row is a gap — the expected result while the work is in progress |
| 2 | the harness itself failed: a page did not come up, a state could not be reached, or a surface is present on one side and absent on the other |

## The contract

The selectors, states, viewports, measured properties, and tolerances are the contract in
[contract-parity-probe.md](../../docs/specs/FEAT-007-scanner-styling-parity/contract-parity-probe.md), and
`surfaces.mjs` is that contract in machine-readable form. Changing one without the other is how a matrix stops
meaning anything.
