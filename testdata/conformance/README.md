# The web application conformance harness

`run.sh` drives the real web application — the one `internal/presentation/client/build.mjs` builds and
`go:embed` puts inside the server binary — in a real headless Chrome, against a real server, and works through
the checklist in [quickstart.md §6](../../../docs/specs/FEAT-003-server-ng-refactor/quickstart.md). It exits 0
when every item passed.

```console
cd testdata/conformance
./run.sh                    # the checklist
./run.sh --keep             # leave the databases, the browser profile, and the server logs behind
./run.sh --seed ../legacyv6/rdio-scanner.db   # the drop-in: the checklist that reads what came across
```

It needs the Go toolchain, node, `make`, and `google-chrome` on `PATH`. It builds both the web application and
the server before anything is driven, so it takes a minute or two before the first item runs.

## What is driven, and how

`conformance.mjs` starts Chrome with `--headless=new --remote-debugging-port=…` and talks to it over the
DevTools protocol with nothing but Node's own `WebSocket` and `fetch`: no driver, no test framework, no new
dependency. Pages are opened with `Target.createTarget`, driven with `Runtime.evaluate` and
`Input.dispatchMouseEvent`, and what the page shows is read out of the page's own DOM.

The clicks are real mouse presses and releases, not `element.click()` run inside the page. That matters: the
listener's audio engine only creates its playback context on a genuine user gesture, and a synthesized click is
not one — a harness that synthesized clicks would never see a call play.

Because they are real presses, the point being pressed has to be the control. `click()` scrolls the control into
view, takes its middle, and only presses once `document.elementFromPoint` reports the control itself at that
point and the point has not moved between two samples. A drawer that is still sliding in puts its contents at
coordinates the browser would press as whatever is there — the panel, or nothing at all — and the press and the
release land on two different elements, which fires no click on either. That is not a browser quirk to work
around; it is the harness pressing the wrong thing, and it is why four items used to fail with "it never
appeared".

Alongside the browser, the same script holds a plain http client for the paths a *recording tool* uses — the
two upload endpoints — so that a call can be put into the server and then seen on the page.

Every item has a watchdog, every command the browser is given has a timeout, and a wait that fails writes down
what the page actually holds. A harness that hangs, or that says only "it never appeared", is a harness nobody
can debug a week later.

## The checklist

| Item | What it proves |
| --- | --- |
| `the-listener-opens-and-is-given-the-configuration` | the socket conversation completes, and the systems the server configured appear in the search filters |
| `the-branding-the-administrator-set-is-shown` | the branding an administrator saved reaches a listener |
| `the-livefeed-is-turned-on` | the LIVE FEED switch reads on, which is what a listener does before hearing anything, and what sends the server the talkgroups this listener wants |
| `a-call-uploaded-over-http-appears-live` | a recording posted to `/api/call-upload` is ingested and lands on the display of a listener that is already connected |
| `a-recording-from-a-trunk-recorder-becomes-a-call` | the trunk-recorder sidecar and audio pair become a call, with the names the sidecar gives it |
| `the-talkgroup-being-heard-can-be-held-and-released` | HOLD TG turns on and back off |
| `the-search-panel-returns-a-page-of-calls` | the search panel asks the server and shows the page of calls it answered |
| `a-searched-call-is-played-and-then-stopped` | a searched call plays over the socket and the button becomes a stop, which clears the display |
| `a-searched-call-is-downloaded` | the download mode turns the row buttons into downloads and the browser writes the file out |
| `the-unlock-code-gate-refuses-a-wrong-code-and-takes-the-right-one` | with an access stored, a listener is asked for a code, a wrong one is refused, the right one lets the application through |
| `the-administrator-logs-in` | the password reaches the administrator surface |
| `each-of-the-eight-sections-is-read-and-saved` | each of the eight configuration sections is opened from the server, changed with the controls that section itself offers, saved, and read back over the api |
| `a-second-administrator-page-is-updated-by-a-save` | a second page holding the same section is updated by a save it never asked for |
| `the-logs-page-shows-what-the-server-wrote` | the log the server wrote is the log the page shows |
| `the-administrator-changes-the-password-and-logs-in-with-it` | the change is reported, the old password stops working, the new one works |
| `a-token-that-is-no-good-returns-to-the-login` | a page holding a token the server refuses is put back at the login form and drops the token |
| `the-administrator-logs-out` | the logout control ends the session and leaves no token behind |

## The drop-in: `--seed`

The claim that matters most for a rewrite is the one this harness used to end with: a database the **legacy**
server wrote is served by the new server to the web application, and the application is asked whether its
configuration and its stored calls are there and playable.

Producing that database used to require the legacy source: build `server/`, start it, give it the harness seed and
one ingested call through its own administrator api, stop it, copy the database it wrote. `server/` is gone, so
the harness cannot make one. What it can do is start the new server on a database that is shaped like a v6.6.3
deployment's — `testdata/legacyv6/rdio-scanner.db`, which `testdata/legacyv6/generate.sh` writes from the v6.6.3
schema, the v6.6.3 defaults, and the shapes the legacy server's own code writes into its columns — and ask the
application the same questions.

```console
./run.sh --seed ../legacyv6/rdio-scanner.db
```

`run.sh` copies the named database where the server opens its own, starts the server, checks that the server said
it was migrating what it was given (a seed that was ignored would otherwise drive a checklist against an empty
database), and runs the `migrated` phase instead of the main one. Nothing in this phase configures the server:
every value the items look for is one the fixture wrote and the migration carried.

| Item | What it proves |
| --- | --- |
| `the-migrated-deployment-asks-its-listener-to-unlock` | the access codes the deployment issued are codes this server asks a listener for, so an upgraded deployment is still a restricted one |
| `the-migrated-access-code-opens-the-deployment-that-issued-it` | the carried code unlocks the socket, and the systems its carried scope names appear in the search filters — a scope this server could not read would leave a listener that unlocked the door and sees nothing |
| `the-migrated-branding-is-the-one-the-deployment-set` | the branding the deployment wrote is on the status line of a listener that never configured anything |
| `the-migrated-call-history-is-searchable-in-the-browser` | the searched page holds the calls the migration carried, with the systems they resolved onto |
| `a-migrated-call-saves-its-audio-to-the-browser` | the audio a v6 row held reaches the browser as a file of the same size |
| `the-administrator-reads-the-configuration-that-came-across` | the groups section and the access section hold the rows that came across, and not the archived ones |
| `the-logs-page-shows-what-the-migration-said` | the log page offers the migration as a part of the server, and narrowed to it its first page is the migration's own report |

The two halves still answer different questions, and neither is redundant. The migration tests in
`internal/infrastructure/persistence/` compare the values row by row and cannot see what a carried value *means*
on this side of the boundary; these items ask the running server what it answers, which is the only way to find a
row that was carried complete and is still dead. `docs/specs/FEAT-011-v663-migration-integrity/decision-fixture-scope-shape.md`
is one of those, found this way.

## Things the harness arranges

- **`TZ=UTC` for every server**, so that the moments the pages show are the moments the harness wrote into the
  calls.
- **A real audio file.** The recording this harness uploads is a second of 8 kHz mono 16-bit PCM. A comparison
  of what two servers passed along would be content with meaningless bytes; this one puts the bytes in a browser,
  and a browser only plays audio it can decode.
- **`--autoplay-policy=no-user-gesture-required`**, because a harness has no hands and the listener's audio
  context is created on the first gesture.
- **`--disable-background-timer-throttling`** and its two companions, because a page the browser treats as
  background has its timers throttled, and the listener reconnects on a timer.
- **A directory that exists** for the dir watch section to name, so that saving that section points at something
  real.
- **A page that has forgotten the unlock code**, because a listener remembers the code it was given in local
  storage and the gate is only worth checking against a listener that has not been let in yet.
- **The seed applied through the administrator api**, the way an administrator applies a configuration, using
  `fixtures/seed.json` beside this harness.
- **No seed at all in the `migrated` phase.** The deployment being driven already has a configuration, access
  codes, watched directories, and calls in its database; the harness only reads what it finds there. The
  administrator password it types is the one the server starts with, because the fixture holds no password row —
  which is what an operator upgrading actually types on the first login.
- **The log page narrowed by subsystem** in the `migrated` phase, by clicking the chip the page itself offers.
  The migration writes its report under one subsystem, and the unnarrowed page shows the login and listener
  entries on top of it.

## The tree the pinned fixtures point back to

The goldens under `testdata/golden/` and the databases under `testdata/legacydb/`, `testdata/legacyv6/`, and
`testdata/legacydamaged/` were taken from the legacy server at git tree
`02ae33d4651fee4c46a93bdcf574d0aead89e542`. This harness no longer pins anything: it has no legacy source to
build, and a pin on a directory that does not exist only produces an error message. fixture generators name when they explain how to obtain the source they drive. [spec.md, Baseline](../../docs/specs/FEAT-005-server-ng-cutover/spec.md#baseline) is where that tree is recorded.
