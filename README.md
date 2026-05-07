![Rdio Scanner](./docs/images/rdio-scanner.png?raw=true)

# This fork (evilgenius79/rdio-scanner)

> This fork is just me using Claude AI to fix issues and add features.
> **USE AT YOUR OWN RISK.**

This is a community fork that adds quality-of-life features and fixes a number of
issues found in the upstream codebase. Everything here is upstream-compatible —
existing rdio-scanner.db files keep working, all original recorder integrations
are unchanged, and no new mandatory configuration is required.

## What's new in this fork

### Performance
- **Parallel call ingestion.** Upstream feeds the entire ingest channel
  through a single goroutine, so FFmpeg conversion and the call DB write
  for one upload block every other recorder behind it. The fork splits
  ingestion into a serial *prepare* stage (auto-populate, dup check —
  protects shared state) and a parallel *finalize* stage (FFmpeg + per-row
  insert + broadcast) running across `min(NumCPU, 16)` workers. Tune with
  the `ingest_workers` flag / ini key (0 = auto). The chosen worker count
  and `GOMAXPROCS` are logged at startup and in the admin Logs panel.
- **SQLite WAL mode + per-call DB concurrency.** SQLite is now opened with
  `journal_mode=WAL` and `synchronous=NORMAL`, and the global `Calls`
  mutex around `WriteCall` / `Search` / `CheckDuplicate` / `Prune` is gone.
  Searches no longer block ingestion and vice versa, and multiple ingest
  workers can write concurrently. Existing databases auto-migrate to WAL
  on first start; you'll see `rdio-scanner.db-wal` and `.db-shm` files
  appear next to the main DB — that is normal. (Helps with the symptoms
  in upstream issue [#469](https://github.com/chuot/rdio-scanner/issues/469).)
- **Hot SQL paths now use parameterized queries.** `CheckDuplicate` and
  `GetCall` previously interpolated values via `fmt.Sprintf` (defensible
  but slower because the driver has to re-prepare each time and harder
  for static analysis). They now use `?` placeholders.

### Features
- **Radio Reference talkgroup import** — Admin → Tools → *Import Talkgroups*
  now has a "Radio Reference" panel where you enter your RR username, password,
  registered app key and a system id, and the server pulls talkgroups,
  categories and tags directly from radioreference.com via SOAP. Categories and
  tags are resolved to label strings before they are dropped into the existing
  review-and-import flow. Credentials are sent only to your own server and are
  never stored on disk.
- **Database maintenance panel** — Admin → Tools → *Database maintenance* gives
  you two new actions:
  - **Compact database** — runs `VACUUM` on SQLite or `OPTIMIZE TABLE` on
    MySQL/MariaDB so disk space is actually returned to the OS after pruning
    or removing systems (addresses upstream issue [#513](https://github.com/chuot/rdio-scanner/issues/513)).
  - **Prune old calls** — manually deletes calls older than the configured
    retention window, or a number of days you specify, without waiting for
    the scheduled prune.
- **Trusted proxy mode** — new `trust_proxy` flag (CLI: `-trust_proxy=true`,
  ini: `trust_proxy = true`). When **off** (the default), `X-Forwarded-For`
  is ignored, so a direct client cannot spoof the header to dodge the per-IP
  admin login lockout. Turn it on only when rdio-scanner sits behind a
  trusted reverse proxy.
- **`ingest_workers` knob** — CLI: `-ingest_workers=N`, ini:
  `ingest_workers = N`. Sets the parallel-finalize worker count for call
  ingestion. `0` (default) picks `min(NumCPU, 16)`.

### Security & stability hardening

A separate three-pillar audit pass (server security, server concurrency,
Angular client) found and fixed a number of issues that survived the
earlier rounds:

- **Admin event-stream WebSocket no longer leaks config pre-auth.** The
  `/api/admin/config` upgrade path used to register the connection on the
  broadcast channel *before* reading the bearer token, then disabled the
  read deadline. Any client that completed the WebSocket handshake
  received the next admin config push (API keys, dirwatch paths, etc.)
  before being torn down. It now requires a valid token on the first
  frame within 10 seconds, enforces same-origin via an explicit
  `CheckOrigin`, and bounds idle reads to 5 minutes.
- **Concurrent-map panics under load.** With parallel ingest workers,
  the `Clients` map was iterated unlocked in `EmitCall`/`EmitConfig`/
  `EmitListenersCount` while `Add`/`Remove` mutated it. `Downstreams.Send`
  walked its list without holding the mutex. `Units.Add` had no locking
  at all. All three are now snapshot-under-lock + iterate-outside-lock.
  Per-listener sends are now non-blocking so one stalled client can't
  back-pressure ingest.
- **Constant-time token / API-key comparisons.** Both `admin.ValidateToken`
  and `Apikeys.GetApikey` did `==` compares in a linear scan, leaking
  per-byte timing across all candidates. Both now use
  `crypto/subtle.ConstantTimeCompare` with no early break.
- **Admin `Tokens` slice race fixed.** `LoginHandler`/`LogoutHandler`/
  `ValidateToken` mutated and read `admin.Tokens` from many goroutines
  with no lock. All access now holds `admin.mutex`.
- **Unauthenticated upload DoS closed.** `/api/call-upload` and
  `/api/trunk-recorder-call-upload` read each multipart part with
  `io.ReadAll` *before* checking the API key and had no body-size cap.
  Both endpoints now wrap `r.Body` in `http.MaxBytesReader` (256 MiB).
- **API-key delete table-name typo fixed.** The bulk-delete path
  referenced `rdioScannerApikeys` while every other query uses
  `rdioScannerApiKeys`. SQLite's case-insensitive identifier matching
  hid the bug; case-sensitive backends would 500.
- **DirWatch crash loop on bad mask fixed.** A malformed filename mask
  panicked the watcher goroutine via `regexp.MustCompile`; the deferred
  recover restarted the watcher, which immediately panicked again on
  the next event. Now uses `Compile`, logs the error, and exits cleanly.
- **WebSocket reconnect storm fixed.** Both the listener WebSocket and
  the admin config WebSocket retried on a fixed 2-second timer with no
  cap. Hundreds of mobile listeners stuck in this loop while the
  backend was offline meant a thundering herd the moment it came back.
  Both clients now use exponential backoff with jitter, capped at 60 s,
  and reset on each successful open.
- **Audio download no longer breaks past ~30 s of audio.** The "save
  call" button built a base64 `data:` URL via
  `String.fromCharCode` + `btoa`, which silently failed for recordings
  larger than the browser's ~2 MiB data-URL cap. Replaced with
  `Blob` + `URL.createObjectURL`.
- **Admin config form subscription leak fixed.** Each config push from
  the server triggered `reset()`, which subscribed to three form
  observables without disposing the previous batch. After hours of
  admin panel use this leaked dozens of subscriptions all firing on
  every form edit.

### Bug fixes
- **Admin login lockout actually works.** Upstream defined the lockout
  delay as `time.Duration(time.Duration.Minutes(10))`, which is roughly **16
  nanoseconds**, and gated the lockout with `||` so it triggered on every
  request. The fork applies a real 10-minute window, locks after the
  configured number of failures, logs the threshold once and clears the
  counter on a successful login.
- **`passwordNeedChange` is honest.** Upstream hardcoded
  `passwordNeedChange: true` in the login response. The fork returns the real
  server state.
- **Audio conversion default no longer overwrites `MaxClients`.** A
  copy-paste typo in `options.go` was assigning the audio-conversion default
  to `MaxClients`. Fixed.
- **Default `Emergency` tag** no longer has a trailing space.
- **JWT admin tokens have an `exp` claim** (24 h). Expired tokens fail
  validation automatically.
- **DirWatch `#UNIT` mask placeholder is finally derived correctly**
  (addresses upstream issue [#532](https://github.com/chuot/rdio-scanner/issues/532)). Upstream's `parseMask` only appended to
  `call.Sources` and skipped the case where it was the zero value, so the
  unit was silently dropped. The fork sets the call's primary source,
  appends to the sources list and registers the unit on the system's Units
  list via the autopopulate path so it shows up in the UI and downstream.
- **Multipart `source`/`sources` ingestion** — `call.Source` was being
  written as `int` (so the `case uint:` consumer in the downstream forwarder
  silently dropped it) and the `sources` handler had a shadowed `units`
  variable that meant tagged units were never actually added. Fixed.
- **CSV importer no longer corrupts quoted commas.** Upstream's CSV parser
  was a regex that split on `,` blindly and stripped only the outermost
  `"` characters, so any field containing a `,` inside quotes was destroyed.
  Replaced with an RFC 4180-ish parser that handles quoted fields,
  embedded commas, newlines and `""` escapes. Used by both the talkgroup
  and unit importers.
- **CSV reading uses `readAsText`** instead of the deprecated
  `readAsBinaryString`, so non-ASCII tag and group names round-trip.

## New API endpoints (admin, JWT-protected)

| Method | Path                                       | Purpose                                        |
| ------ | ------------------------------------------ | ---------------------------------------------- |
| POST   | `/api/admin/database/compact`              | Vacuum SQLite or OPTIMIZE all MySQL/MariaDB tables |
| POST   | `/api/admin/database/prune`                | Delete calls older than `{ "days": N }` (or the configured retention window if omitted) |
| POST   | `/api/admin/radio-reference/talkgroups`    | Body `{ username, password, appKey, sid }` → returns `{ talkgroups: [...] }` for review-import |

## Branch

Active development happens on `claude/code-review-api-integration-46d8E` in this
fork. Pull requests welcome.

---

# What is it ?

[Rdio Scanner](https://github.com/chuot/rdio-scanner) is an open source software that ingest and distribute audio files generated by various software-defined radio recorders. Its interface tries to reproduce the user experience of a real police scanner, while adding its own touch.

You can listen to [Rdio Scanner](https://github.com/chuot/rdio-scanner) on any modern browsers using the integrated web app.

# Recorders compatibility

[Rdio Scanner](https://github.com/chuot/rdio-scanner) works with any radio recorder, as long as they can create audio files separated by conversations or transmissions.

Here is a list of recorders known to work with [Rdio Scanner](https://github.com/chuot/rdio-scanner):

| Recorder                                                       | API | Dirwatch |
| -------------------------------------------------------------- | --- | -------- |
| [Trunk Recorder](https://github.com/robotastic/trunk-recorder) | X   | X        |
| [RTLSDR-Airband](https://github.com/szpajder/RTLSDR-Airband)   |     | X        |
| [SDRTrunk](https://github.com/DSheirer/sdrtrunk)               |     | X        |
| [voxcall](https://github.com/aaknitt/voxcall)                  | X   |          |
| [ProScan](https://www.proscan.org/)                            |     | X        |
| [DSDPlus Fast Lane](https://https://www.dsdplus.com/)          |     | X        |

# Quick start

ALWAYS DOWNLOAD THE LATEST VERSION OF [RDIO SCANNER](https://github.com/chuot/rdio-scanner) FROM ITS OFFICIAL REPOSITORY AT **[HTTPS://GITHUB.COM/CHUOT/RDIO-SCANNER](https://github.com/chuot/rdio-scanner)**.

1. Download the latest precompiled version of [Rdio Scanner](https://github.com/chuot/rdio-scanner) from the [releases tab](https://github.com/chuot/rdio-scanner/releases).

   | Operating system | Architecture | Use package                           |
   | -----------------| ------------ | ------------------------------------- |
   | FreeBSD          | amd64        | rdio-scanner-freebsd-amd64-v6.6.3.zip |
   | Linux            | 386          | rdio-scanner-linux-386-v6.6.3.zip     |
   | Linux            | amd64        | rdio-scanner-linux-amd64-v6.6.3.zip   |
   | Linux            | arm          | rdio-scanner-linux-arm-v6.6.3.zip     |
   | Linux            | arm64        | rdio-scanner-linux-arm64-v6.6.3.zip   |
   | macOS            | amd64        | rdio-scanner-macos-amd64-v6.6.3.zip   |
   | macOS            | arm64        | rdio-scanner-macos-arm64-v6.6.3.zip   |
   | Windows          | amd64        | rdio-scanner-macos-amd64-v6.6.3.zip   |

2. Extract the contents of the archive somewhere on your computer.
3. Run the [Rdio Scanner](https://github.com/chuot/rdio-scanner) executable.
4. Access the administrative dashboard to finalize the configuration.

More detailed instructions are available in the `rdio-scanner.pdf` file provided in the precompiled archives.

# Docker

As a courtesy to Docker users, [Rdio Scanner](https://github.com/chuot/rdio-scanner) is also distributed as a Docker image where a new version is generated with each new release. More information available at **[https://hub.docker.com/repository/docker/chuot/rdio-scanner](https://hub.docker.com/repository/docker/chuot/rdio-scanner)**.

# Need help ?

## GitHub Discussions 💭

Your question might already be addressed on the [Rdio Scanner GitHub Wiki](https://github.com/chuot/rdio-scanner/wiki), be sure to check it out at **[https://github.com/chuot/rdio-scanner/wiki](https://github.com/chuot/rdio-scanner/wiki)**.

## GitHub WIKI 📖

Feel free to ask your questions or share your comments on the [Rdio Scanner GitHub discussions](https://github.com/chuot/rdio-scanner/discussions) at **[https://github.com/chuot/rdio-scanner/discussions](https://github.com/chuot/rdio-scanner/discussions)**.

## Discord Server 💬

Connect with others interested in [Rdio Scanner](https://github.com/chuot/rdio-scanner) on this community [Discord server](https://discord.com/invite/pebyc3Sj2x).

# Show your appreciation, support the author

If you like [Rdio Scanner](https://github.com/chuot/rdio-scanner), **[consider starring the GitHub repository](https://github.com/chuot/rdio-scanner/stargazers)** to show you appreciation to the author for his hard work. It cost nothing but is really appreciated.

If you use [Rdio Scanner](https://github.com/chuot/rdio-scanner) for commercial purposes or derive income from it, **[sponsor the project](https://github.com/sponsors/chuot)** to help support continued development.

# Improve your experience on the go

You can enjoy your [Rdio Scanner](https://github.com/chuot/rdio-scanner) on the go on your mobile device with the native app.

[![Available on the App Store](./docs/images/app-store-badge.png?raw=true)](https://apps.apple.com/us/app/rdio-scanner/id1563065667#?platform=iphone)
[![Get it on Google Play](./docs/images/google-play-badge.png?raw=true)](https://play.google.com/store/apps/details?id=solutions.saubeo.rdioScanner)


**Important Notice:**  

> This project is licensed under the GNU GPL. However, the **WebSocket API** provided by this software is **restricted** and reserved exclusively for **Saubeo Solutions and its native applications**.  
> Unauthorized use of the WebSocket API is strictly prohibited.  
> For details, see [API_ACCESS_POLICY.md](API_ACCESS_POLICY.md).

**Happy Rdio scanning !**
