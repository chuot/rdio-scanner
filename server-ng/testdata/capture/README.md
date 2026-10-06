# The golden wire-protocol captures

These files are what the legacy server in [`server/`](../../../server) actually put on the wire. They
are the yardstick [decision-parity-verification.md](../../../docs/specs/FEAT-003-server-ng-refactor/decision-parity-verification.md)
describes: server-ng is compared against them, not against a reading of the legacy source.

```text
testdata/capture/capture.sh   builds and runs the legacy server, then runs capture.mjs
testdata/capture/capture.mjs  the capture itself: every request, every frame, in order
testdata/capture/fixtures/    the configuration the capture installs
testdata/golden/              what the legacy server said, one file per thing it said
testdata/golden/manifest.json which legacy source these were taken from
```

There is a second capture, for the one thing a request-and-reply capture cannot reach: a recording a
recording tool leaves in a watched directory.

```text
testdata/capture/capture-dirwatch.sh   the same legacy server, on a seeded configuration, with two
                                       recordings dropped into directories it is watching
testdata/capture/capture-dirwatch.mjs  the listener side, and the recordings themselves
testdata/capture/assemble-dirwatch.mjs frames plus the database rows the run left behind, into one file
testdata/capture/sqlite/               what the script hands the database and reads back out of it
testdata/golden/dirwatch-end-to-end.json  what that run produced
```

## Running it

```console
$ cd server-ng/testdata/capture
$ ./capture.sh
```

It builds the legacy server from the source in `server/`, checks that source against the git tree
hash the goldens are pinned to, starts it on `127.0.0.1:3130` against a throwaway SQLite database in
a temporary directory, runs the capture, and stops it. Nothing under `server/` is touched and no
`rdio-scanner.ini` outside the temporary directory is read.

`--keep` leaves the temporary directory behind and says where it is, which is how a capture is
inspected when the server said something the script did not expect. `--allow-legacy-changes` captures
even when `server/` is not the pinned tree; use it only when the pin is the thing being examined.

Requirements: Go, Node 22 or newer (the global `WebSocket` is used, no npm packages), and a machine
with `ffmpeg` and `ffprobe` — see the password change below.

## The rule the goldens obey: raw bytes

A golden holds the bytes the legacy server wrote, not a re-rendering of them. Nothing in
`capture.mjs` parses a payload and writes it back out.

That matters more than it sounds. The legacy marshals its payloads from Go maps, which sort their
keys lexicographically; a JavaScript `JSON.parse` followed by `JSON.stringify` sorts integer-like
keys numerically. A round-trip through the client's own language reorders a livefeed map, and the
comparison would then be between two different byte sequences that mean the same thing. So the frames
are recorded as the strings that arrived, and a payload is cut out of a frame by scanning for the end
of its JSON value (`payloadOf` and `jsonValueEnd`), never by parsing it.

## The two things that cannot be reproduced

Everything else is byte-for-byte. Two values are replaced, and `manifest.json` names them:

- **`<token>`** — the administrator session token, which is a signed random value. It appears in
  `rest-login.json` and inside the full configuration the admin socket pushes.
- **`<datetime>`** — the moments a logs search reports: each entry's `dateTime`, and the envelope's
  `dateStart` and `dateStop`. The log *lines* are reproducible; when the server wrote them is not.

The legacy is run with `TZ=UTC`, which is not a redaction but a requirement: the server renders an
instant into a timestamp string in the time zone it runs in, so without it every captured timestamp
differs between machines.

Run the capture twice and compare: two consecutive runs produce byte-identical goldens.

## Why the capture is in this order

The order is not convenience, it is what the legacy server's own timing forces.

- **The listener-count frames are waited for explicitly.** `LSC` is broadcast on register and
  unregister through a five-second `time.AfterFunc` ([server/controller.go](../../../server/controller.go)
  589-616), so a count can arrive at any moment up to five seconds after the last listener moved.
  The capture waits for each count it wants before anything else registers or unregisters, then turns
  `showListenersCount` off and waits out the last pending debounce before the restricted phase begins,
  so a late count cannot land in a session whose frames are being recorded.
- **The options save that turns the listener count off comes before the restricted phase**, for the
  same reason.
- **The administrator password is changed last.** `Admin.ChangePassword` writes the options and then
  reads them back out of the database ([server/admin.go](../../../server/admin.go) 127-131), and
  `audioConversion` is not one of the options `Options.Write` ever stores
  ([server/options.go](../../../server/options.go) 400-415). So the password change silently restores
  the stored conversion mode, and from that point on every uploaded recording is re-encoded by
  whatever `ffmpeg` this machine has. Doing it earlier would make the captured audio depend on the
  machine doing the capture. Quirks Q53 and Q54.
- **The fetch of a call that does not exist is the last thing the capture does**, in a session of its
  own. `Calls.GetCall` returns without rolling back on that path
  ([server/call.go](../../../server/call.go) 284, 291), which leaves a transaction open and makes the
  next write to that SQLite database wait out the ten-second `busy_timeout`. Quirk Q56. Nothing is
  allowed to need a database write after it.
- **The scoped denial uses a key the server does not know**, not a key scoped to a system. A scoped
  key dereferences the upload's unresolved system, the request panics, and the connection closes with
  no reply at all — which is a real behavior of the legacy server (Q51) but is not the denial an
  integrator needs a golden for. Quirk Q52 hides it from the log as well.

## What is covered

Every command in [contract-server-protocol.md](../../../docs/specs/FEAT-002-vanilla-client-ng/contract-server-protocol.md)
§2 to §4, except `IOS`, `PID`, and `SRV`, which the contract itself lists as native-app only.

| Golden | What it pins |
| --- | --- |
| `payload-ver.json` | `VER` reply, before any access code |
| `payload-cfg.json`, `payload-cfg-scoped.json` | `CFG` on an open server, and the scoped configuration an access code unlocks |
| `payload-cal-live.json`, `payload-cal-fetch.json` | a pushed call, and a fetched one with the flag it was asked with |
| `payload-lcl.json` | the `LCL` envelope and a page of results |
| `ws-frames-open.json` | an open listener's whole session, including the `LSC` counts and the `LFM` acks for a livefeed map and for stopping it |
| `ws-frames-restricted.json` | `PIN` before unlocking, `PIN` for a wrong code, `XPR` for an expired code, `MAX` for a connection over the limit, and no reply at all for a call the access does not cover |
| `ws-frames-locked.json` | the fifth wrong access code, and what the server says after it |
| `ws-frames-missing-call.json` | a fetch for a call that is not there: no reply |
| `rest-login.json`, `rest-logout-status.txt`, `rest-config-unauthorized-status.txt` | the administrator session, and the 401 with no body |
| `rest-config.json`, `rest-section-*.json`, `rest-section-options-put.json` | the whole configuration, each of the eight sections as `GET`, and a per-section `PUT` reply |
| `rest-alerts.json`, `rest-logs.json`, `rest-password.json` | alerts, a filtered log search, and the password change |
| `rest-upload.txt`, `rest-upload-denied.txt`, `rest-trunk-recorder-upload.txt` | both upload endpoints, and the denial an unknown apikey gets |
| `adminws-config.json`, `adminws-bad-token.json` | the admin config socket's push — a bare configuration object, with no command framing — and the close 1000 an unrecognized token gets |

`PUT /api/admin/config` and `GET /api/admin/config` answer with the same payload (`SendConfig` in
both paths), so `rest-config.json` covers both.

## The end-to-end capture

`./capture-dirwatch.sh` captures a different thing, so it is set up differently. The golden it writes is
what [server-ng/cmd/rdio-scanner/dirwatch_test.go](../../../server-ng/cmd/rdio-scanner/dirwatch_test.go)
compares against, frame for frame and column for column.

Three choices make the run reproducible, and each one is a choice about what the golden is allowed to pin:

- **The systems and talkgroups the recordings land on are seeded.** The run is about a recording becoming
  a call and reaching a listener. What a server creates when it has never seen a system before is a
  different run, with its own test, and mixing them would make a failure in one look like a failure in the
  other. The seeded configuration also names the system references the listener subscribes to, which it
  has to know before the recordings arrive: a server broadcasts a call only to a listener that asked for
  that talkgroup, and a talkgroup that does not exist yet cannot be asked for.
- **Audio conversion is turned off**, by a stored `audioConversion` option of `0`. The recording a watch
  hands over is then the recording the capture wrote, byte for byte, and the golden pins the server passing
  it along whole rather than pinning what one version of `ffmpeg` makes of it. Conversion is compared where
  conversion is the subject.
- **The recordings are dropped one at a time**, the second only after the first has been broadcast. Two
  recordings dropped together arrive in whatever order two directory watchers happen to notice them in.

The recordings are written by the capture script rather than kept as fixture files, and they are the same
bytes the server-ng test writes, so the two runs are the same run.

The log rows the golden holds are the rows the captured boot added after its own `server started`, which is
why the script boots the server twice — once to let it build and migrate its own database, once to be
captured — and drops the first two log rows.

The listener stays connected past the five-second listener-count report and hangs up afterwards, which is
the order the log rows in the golden are in. Hanging up earlier would put a different count in the log: the
report counts the listeners that are there when it is written, not the ones that were there when it was
scheduled.

## Re-capturing

The goldens are pinned to the git tree hash in `manifest.json`. `server/` is not modified by this
feature, so the pin should never move. If it does — because `server/` changed — the goldens have to be
re-taken, the pin in `capture.sh` and `manifest.json` updated, and every differential test that reads
these files re-run.
