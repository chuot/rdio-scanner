# The web application conformance harness

`run.sh` drives the real web application — the one `internal/presentation/client/build.mjs` builds and
`go:embed` puts inside the server binary — in a real headless Chrome, against a real server, and works through
the checklist in [quickstart.md §6](../../../docs/specs/FEAT-003-server-ng-refactor/quickstart.md). It exits 0
when every item passed.

```console
cd testdata/conformance
./run.sh                    # the checklist
./run.sh --keep             # leave the databases, the browser profile, and the server logs behind
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

## The drop-in, and why it is not here any more

This harness used to end with the claim that matters most for a rewrite: a database the **legacy** server wrote
is served by the new server to the web application, and the application is asked whether its configuration and
its stored calls are there and playable.

Producing that database required the legacy source. It built `server/`, started it, gave it the harness seed and
one ingested call through its own administrator api, stopped it, and copied the database it wrote. `server/` is
gone, so the harness cannot make one.

The claim itself is not given up. It is answered two ways:

- **`testdata/legacydb/`, `testdata/legacyv6/`, `testdata/legacydamaged/`** are databases the legacy server
  actually wrote, kept in the repository. The migration and drop-in tests in
  `internal/infrastructure/persistence/` start the new server on copies of them and assert what it finds.
- **This harness** proves the other half: the embedded application works against a server that carries it.

What is lost is the single end-to-end run that put a legacy-written database in front of a *browser*. The
migration tests read the same databases without one.

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

## The tree the pinned fixtures point back to

The goldens under `testdata/golden/` and the databases under `testdata/legacydb/`, `testdata/legacyv6/`, and
`testdata/legacydamaged/` were taken from the legacy server at git tree
`02ae33d4651fee4c46a93bdcf574d0aead89e542`. This harness no longer pins anything: it has no legacy source to
build, and a pin on a directory that does not exist only produces an error message. fixture generators name when they explain how to obtain the source they drive. [spec.md, Baseline](../../docs/specs/FEAT-005-server-ng-cutover/spec.md#baseline) is where that tree is recorded.
