# Security Policy

## Supported Versions

Security updates are provided for the latest version of **rdio-scanner**.  
Older versions may not receive patches unless the issue is critical.

## Reporting a Vulnerability

If you discover a security vulnerability, we appreciate your help in responsibly disclosing it.

**Please do NOT open a public GitHub issue.**

Instead, contact us privately at: **<rdio-scanner@saubeo.solutions>**

Include as much detail as possible:

- Steps to reproduce
- Impact assessment
- Affected versions
- Any proof-of-concept code (if applicable)

We will acknowledge your report within **72 hours**.

## Disclosure Process

1. We investigate the report and confirm the vulnerability.
2. We work with you (if needed) to understand the issue fully.
3. We prepare a fix and coordinate a release.
4. We publish a security advisory once the fix is available.

We aim to resolve validated security issues within **14 days**, depending on severity and complexity.

## Scope

This policy applies to:

- The rdio-scanner codebase
- Documentation
- Any related scripts or tooling included in the repository
- The two mobile applications, the one on Google Play and the one on the App Store, whose reports these forms track

It does **not** cover:

- Third-party dependencies
- External services or infrastructure not maintained by Saubeo Solutions

## The ffmpeg binaries the build adopts

Every server binary carries its own `ffmpeg` and `ffprobe` inside it. The pair of every release target is
compiled by this build, in a container, on the machine the build runs on, from the source release the manifest
pins and against the digest the manifest records: the `freebsd-amd64` pair against the FreeBSD project's own
toolchain image used as a sysroot, and the two macOS pairs against an Apple SDK tree its maintainer prepares.
That host starts none of those three pairs, so what they are held to is read out of the files the compile
produced. The one compile a host does with its own toolchain is a Mac compiling a macOS pair, and that Mac
starts the pair it produced.

A hand-built drop is a route for the two macOS targets, and only on a macOS host: `ffmpeg` and `ffprobe`
dropped in that target's bundle directory are run there, and what converts is adopted.

The adoption is a gate, and the gate is a run: the build executes the binaries through the converter's
own argument list, on the machine standing there, with the recordings the server stores, and asks them
what they register. Binaries that convert nothing, binaries that register a video decoder or a video
encoder, and binaries whose file does not hold the machine code the target runs are refused, naming what
is wrong.

**What the gate does not check is the trust boundary.** It reads what the file is and what the program
does. It cannot read who built the binaries, what compiler they were built with, what the person who
built them was told to enable, or what was in the source they were built from. The pinned source and the
pinned digest cover the compiled route only: a hand-built drop carries no digest, and there is no digest
published for it to match. The gate is a run of the binaries, and a run of a program is a program the
machine executed.

So the boundary is the operator's own judgement of the binaries they dropped. Adopting a binary is
trusting the person who built it, on the machine that built it, and the build cannot widen that trust:
it can refuse a binary that fails the gate, and it cannot vouch for one that passes.

Two things follow from that, and both are in the release:

- The archive carries `FFMPEG.txt`, and which of the two notices it is depends on where the pair came from. A
  pair this build compiled in a container carries the notice naming the pinned source and digest, the recipe,
  and the toolchain and image the record pins; for the three pairs a compile host cannot start, that notice
  also names what the build read out of the produced files in place of running them. A pair a Mac compiled with
  its own toolchain, or a maintainer built and dropped, carries the notice naming the host that compiled them,
  the command that built them there, and the codecs the run recorded. The person who downloads the archive can
  see which of the two the binaries in it are. The notice is the record; the trust is the reader's to place.
- A program dropped into a target's bundle directory is consulted only on a macOS host, for one of the two
  macOS targets. Anywhere else `//go:embed all:` takes it into the server binary and nothing loads it: not a
  route, not refused, a payload in an archive. The drop route is the two macOS targets on a macOS host, and a
  drop the build never consults is a mistake that ships.

A server built with plain `go build`, without this build, carries no ffmpeg at all and converts with the
ffmpeg its host installs. That is a third source of the same program, chosen by whoever installed the
host, and it is outside this build's gate entirely.

## Responsible Disclosure

We ask that you:

- Give us reasonable time to fix the issue before public disclosure
- Avoid actions that could harm users or data
- Follow ethical security research practices

We greatly appreciate your contribution to the safety and reliability of rdio-scanner.
