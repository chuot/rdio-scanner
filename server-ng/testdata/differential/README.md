# The differential harness

`run.sh` runs the legacy server and server-ng side by side and compares what they do: the frames they send,
the answers they give an administrator, and the rows they leave in their databases. It exits 0 when the two
servers agree everywhere except where this harness expects them not to.

```console
cd server-ng/testdata/differential
./run.sh
./run.sh --scenario dirwatch-calls        # one scenario
./run.sh --keep                           # leave the transcripts, dumps, and server logs behind
./run.sh --allow-legacy-changes          # only while changing server/ on purpose
```

It needs the Go toolchain, node, and `curl`. It builds both servers from the repository, so it takes a
minute or two before anything is compared.

## What is compared

Every scenario is run twice, once against each server, by the same script (`differential.mjs`) with the same
arguments apart from the address and the directories. Each run produces a **transcript** of the steps it took
and the answers it got, and each server's database is **dumped** table by table afterwards. `compare.mjs`
walks the two transcripts step by step and the two dumps table by table, and reports every difference.

A difference is only allowed when `expected.mjs` declares it, and a declared difference that does **not**
appear is a failure too: an approved divergence that quietly stopped applying is news, and this harness is
how it gets heard.

Payloads are compared key by key **in the order the keys come out**, because Go marshals a struct in the order
its fields are declared: a payload with the same keys rearranged is a different payload to anything reading
it. The audio inside a call frame is compared as the bytes it is.

## What is let differ

| What | Why |
| --- | --- |
| a session token | each login makes a new one |
| a moment inside a log search | it is the moment the server was running |
| `adminPassword` and `secret` in the options table | each deployment hashes the same password with a salt of its own, and makes a signing secret of its own |
| a watched directory path | each server is given a directory of its own, because two servers cannot watch one directory and be compared on what each watched |
| how long a step took | except where a declaration says what the time has to be |

Nothing else. Call identifiers, call timestamps, log messages, and the order rows come back in are all
compared exactly.

## The scenarios

| Scenario | What it drives |
| --- | --- |
| `listener-frames` | a listener's whole conversation: the configuration, the livefeed subscription, a call arriving from an upload, the debounced count report, the call asked for by id, and the call search |
| `missing-call-fetch` | a call that is not there, and the write that follows it |
| `admin-rest` | the administrator's http surface: the whole configuration, each section, the log search, the alerts, a section saved, an access added, the session ended |
| `admin-config-socket` | the socket the web application keeps open: a credential on the socket, a configuration saved, the whole configuration pushed back |
| `scoped-upload` | an upload with an apikey whose scope is written in the documented form |
| `access-limit` | an access with a limit of one, two listeners, the access saved again, and a third listener |
| `scope-normalization` | a scope whose talkgroups are named as neither the wildcard nor a list |
| `dirwatch-calls` | two watched directories, a trunk-recorder pair and a dsd+ recording, and a listener that subscribed before either |

Each scenario runs on a fresh copy of a database each server made for itself, so what one scenario ingests
cannot change what another is compared on.

## The five differences this harness expects

They are the approved divergences in
[decision-preserved-quirks.md](../../../docs/specs/FEAT-003-server-ng-refactor/decision-preserved-quirks.md),
and each one is *asserted* to appear, in the scenario named:

| Quirk | Scenario | What the two servers do differently |
| --- | --- | --- |
| Q51 | `scoped-upload` | the legacy server dereferences a call's unresolved system while checking an apikey scope, and the request ends with no reply at all; server-ng resolves the references first, answers, and ingests the call |
| Q56 | `missing-call-fetch` | the legacy server leaves its transaction open when the call it was asked for is not there, and the next write waits out the ten-second busy timeout and fails; server-ng rolls back and answers at once |
| Q58 | `admin-rest`, `admin-config-socket` | the legacy server reports every site as belonging to no system; server-ng gives it the system it was found under |
| Q61 | `scope-normalization` | the legacy server stores a scope it could not use exactly as it was submitted; server-ng stores the scope it understood |
| Q62 | `access-limit` | the legacy server counts the sessions holding an access by the in-memory object it holds, so saving the access again stops the limit counting and the third connection is let in; server-ng counts the access itself and refuses |

## Things the harness arranges so that it can compare anything at all

- **`TZ=UTC` for both servers.** A dsd+ recording is stamped from the directory and file name in the local
  zone, and the two servers have to be in the same zone to be compared on the moment they read out of it.
- **`sortTalkgroups` off.** The legacy server's sort renumbers the talkgroup orders it holds, so a second
  listener in the same run is told numbers the first listener's configuration invented (Q60). That is not a
  divergence this harness is set up to expect, and the sorting itself is pinned by the payload goldens.
- **`showListenersCount` off** in every scenario except the two that compare the count report, because the
  count is broadcast on a debounce and a debounced report is a log row whose number depends on how the
  connections happened to land.
- **`audioConversion` off.** The setting is not one the server ever writes to its database, so it is applied
  through the seeded configuration and stays in memory for the run. What is compared is the server passing a
  recording along, not what ffmpeg makes of it.
- **`disableDuplicateDetection` on**, so that two recordings of the same talkgroup are not folded together by
  a timer.
- **no save that adds one thing and removes another.** A save naming something new keeps every row it already
  had in the legacy server (Q33) and does not in server-ng, so every scenario that saves a list reads the
  stored list first and saves it back with the identifiers the server gave it.
- **the seed is applied through the administrator api**, the way an administrator applies a configuration, and
  is not itself compared; the paths it uses are compared in the scenarios that read and save the
  configuration.

## The pin

`server/` is pinned to git tree `02ae33d4651fee4c46a93bdcf574d0aead89e542`, the same tree the golden captures
under `testdata/capture/` are pinned to. The five differences above are what *that* source does. If the legacy
server changes, the harness says so and stops, unless `--allow-legacy-changes` is given.
