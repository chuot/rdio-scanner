# How to compile

> RDIO SCANNER REPRESENTS A LOT OF INVESTMENT IN TIME, SUPPORT, TESTS AND MATERIALS. BY INTENTIONALLY MODIFYING ITS CODE TO REMOVE ITS IDENTITY, YOU HARM THE OPEN SOURCE PROJECT AND ITS AUTHOR. PLEASE BE RESPECTFUL, SPONSOR THIS PROJECT, USE NATIVE APPS IF POSSIBLE.

The official version of [Rdio Scanner](https://github.com/chuot/rdio-scanner) is compiled on a PC using the current version of Fedora Workstation. You should have no problem building on another platform as long as the prerequisites are available and installed.

## Install the prerequisites

Your os distribution may have all of the following prerequisites available in its own package repository. Make sure, however, that they are at the latest version. This is especially not the case with Ubuntu and its Node.js.

- latest version of git ([here](https://git-scm.com/downloads))
- latest long-term support version of Node.js ([here](https://nodejs.org/en/))
- latest version of Go ([here](https://go.dev/dl/))
- latest version of podman ([here](https://podman.io/)), for building the container image and for compiling the ffmpeg of the five targets this build compiles in a container. The macOS and FreeBSD pairs are compiled by this build on the machine that is the target, and that machine needs a C compiler, `strip`, and the make program its record names — `gmake` on FreeBSD, where ffmpeg's build asks for GNU make and the base make is not it — and needs no podman

## Compile the app

Clone the official repository on your computer and start the build process.

        git clone https://github.com/chuot/rdio-scanner.git
        cd rdio-scanner
        go run ./cmd/build

The tool compiles the two web applications before it compiles the server, because each bundle is written beside the Go package that embeds it and a server built before both existed would carry a stale one.

The scanner webapp and the administration console are two applications, in `internal/presentation/scanner/frontend` and `internal/presentation/admin/frontend`, and the modules they both use are in `internal/presentation/common/frontend`. Each is built into the bundle directory of the Go package that embeds it, so `internal/presentation/scanner/frontend/bundle` and `internal/presentation/admin/frontend/bundle` are build output and hold nothing but the `.gitkeep` that keeps a plain `go build` compiling.

        go run ./cmd/build client                 # both applications
        go run ./cmd/build scanner                # the scanner webapp only
        go run ./cmd/build admin                  # the administration console only
        go run ./cmd/build ffmpeg                 # the ffmpeg this machine's target carries
        go run ./cmd/build ffmpeg-check <target>  # the pair of one target, proved on that target's own platform
        go run ./cmd/build server                 # the server binary only, against the bundles on disk
        go run ./cmd/build dist                   # the archive of the platform this machine is
        go run ./cmd/build dist --all             # the release matrix, and the archives it writes
        go run ./cmd/build container              # the container image, built locally
        go run ./cmd/build clean                  # the release output, the client dependencies, and both bundle contents
        go run ./cmd/build version                # the version this build carries

The server binary carries both bundles. One executable serves the scanner webapp at `/` and the administration console at `/admin/`.

## The ffmpeg the server carries

A server compiled for any of the eight release targets carries its own `ffmpeg` and `ffprobe` inside its own binary, and converts audio with nothing installed on the host. Five of them — `linux-386`, `linux-amd64`, `linux-arm`, `linux-arm64` and `windows-amd64` — are compiled by this build, in a container, from the source the manifest pins. The other three — `macos-amd64`, `macos-arm64` and `freebsd-amd64` — are compiled on the platform each one runs on, and on that platform this build compiles them itself: the same source release, the same recipe, the host's own compiler and linker, and the same check. One command there compiles the pair, proves it, pins it in the manifest, and adopts it into the bundle its server embeds.

        go run ./cmd/build ffmpeg                    # the target this machine is
        go run ./cmd/build ffmpeg linux-arm64        # another target, which is how the release matrix cross-builds
        go run ./cmd/build ffmpeg-check macos-arm64  # the pair in the bundle directory, proved on a macOS arm64 host

The version, the source, the configure recipe and the toolchain of every target this build compiles are written once, in `internal/infrastructure/ffmpeg/bundle/manifest.json`. The tool fetches that release from ffmpeg.org, checks it against the sha256 the manifest records — ffmpeg.org signs its releases and publishes no checksum, so the pinned digest is what this build trusts — compiles the recipe in a container built from `Containerfile.ffmpeg`, and then runs what it produced through the converter's own argument list before accepting it. podman is what compiles it.

What it fetches and compiles is build output: `.build/ffmpeg` holds the source tarball and the compiled pair, and `internal/infrastructure/ffmpeg/bundle/<target>` holds the gzipped pair the binary embeds. Deleting either costs a rebuild and nothing else. A second build whose ffmpeg version, target and recipe are unchanged compiles nothing again.

### The pair compiled on its own host

A macOS ffmpeg and a FreeBSD ffmpeg are compiled on a Mac and on a FreeBSD machine: the machine that produces the releases runs neither program, so a release run there compiles the five it can, skips these three, and says what would have made the rest buildable. On the Mac, and on the FreeBSD box, the build does this compile itself:

        go run ./cmd/build ffmpeg   # on the machine that is the target: macos-amd64, macos-arm64, freebsd-amd64

It fetches the same source release the manifest pins, checks it against the pinned digest, unpacks it, runs the same trimmed configure recipe with the record's flags, runs the make program the record names, strips the two programs, and on macOS signs them ad-hoc after the strip, because strip rewrites the bytes the signature is computed over. There is no container and no cross-compile: the host's own compiler and linker produce the pair, which is the whole reason the pair is compiled there and not shipped from a cross-compiler.

The run then proves the pair it produced with the same gate the `ffmpeg-check` command records — the converter's own argument list, the recordings the server stores, the lengths the filters ask for, the tags the server writes — and refuses the pair it just compiled if it is not the program its target runs: the wrong architecture, a fat Mach-O, a Mach-O with no code signature, a Mach-O linking a library the host has to install, a Linux-branded ELF, a FreeBSD program with a program interpreter. The proof goes to `.build/ffmpeg/checks/<target>.json`.

Then it writes the two sha256 digests the proof recorded into that target's record in `manifest.json`, under `programs`, and prints them. The manifest is the only tracked file the build writes, and it writes them as a splice into the file's own bytes, so the diff is the two lines carrying the digests and nothing else; a record that already pinned a pair prints the digest it replaces. Commit that change: the pins are the release's promise about a pair the release machine cannot run.

Then it gzips the pair into the build's cache, publishes `ffmpeg.gz`, `ffprobe.gz` and `LICENCE.md` into the bundle directory, and removes the raw pair from it, so no server binary carries an uncompressed program. `go run ./cmd/build dist` on that host writes the archive that carries it, and the next run finds the pair in the cache and compiles nothing.

A pair built by hand is adopted the same way, and the adoption is the same gate: drop `ffmpeg` and `ffprobe` in `internal/infrastructure/ffmpeg/bundle/<target>`, prove them with `go run ./cmd/build ffmpeg-check <target>` on that host, and commit the two digests it prints into that target's record. The build adopts a pair on three things and nothing else: the digests the manifest pins, the shape of the two programs, and the proof naming that target and those digests. A pair with no proof, a proof naming other digests, and a pair that is not the pinned one are each refused, naming what is missing. A pair you dropped wins over the compile every time: it is the pair its proof was recorded for, and the build never compiles over it.

A supplied target with nothing dropped for it, on a machine that is not its platform, is not a failure. The build says so, names the directory to drop the pair in, the command that proves it, and the manifest field to fill, and builds no server and no archive for that target. In a matrix run the other archives are written and the run ends successfully.

A server built with plain `go build`, without the tool, carries no ffmpeg at all: it converts with the ffmpeg its host installs, and says so once at startup when it cannot find one.

The licence the ffmpeg is under, the release it comes from, and the recipe it was built with are written next to the bundle and travel into every release archive as `FFMPEG.txt` — the notice naming the recipe, the source digest and the command that produced the pair for the five this build compiles in a container, and the notice naming the host that compiled it, the two program digests the release pins, and the date the build's ffmpeg check ran for it there, for the other three.

## Say what version you are building

`VERSION.txt` at the repository root is the one place a version is declared. The tool reads it, asks git what it says about the commit being built, and passes the result to the applications through the environment and to the binary through the linker. No source file is rewritten to carry a version.

        8.0.0                       a commit tagged v8.0.0
        8.0.0+g1a2b3c4              a commit no tag names
        8.0.0-beta1                 go run ./cmd/build --pre beta1
        8.0.0-beta1+g1a2b3c4        both at once

A repository with no `VERSION.txt` builds as `0.0.0`, which is a version no release ever had. A `VERSION.txt` that holds something other than a bare `X.Y.Z` stops the build rather than being repaired, and a tag that names a different core than the file does stops it too.

A server built with plain `go build`, without the tool, reports `dev` — in its banner as well as in `-version` — so nobody mistakes it for a release.

The release matrix is the tool's, and it reads the same file:

        go run ./cmd/build dist              # the archive of the platform this machine is
        go run ./cmd/build dist --all        # the precompiled versions for every platform, and their archives
        go run ./cmd/build dist linux-arm64  # one target, which is how the matrix is built one at a time
        go run ./cmd/build container         # the container image, built locally and never pushed
        go run ./cmd/build clean             # the release output, the client dependencies, and both bundle contents

`dist` alone builds the one archive the machine running it can run, which is what a developer builds to try what they just built. The eight-target matrix is a release, and asking for it is a deliberate act: `--all` is the flag that asks for it, and it belongs to `dist` and to nothing else. `--all` beside a named target is refused, and a machine whose platform is in no release target is refused naming `--all`.

A phase that has nothing to do does not run. A bundle is rebuilt when a file it is built from is newer
than the bundle, and a server binary is rebuilt when a Go file or an embedded bundle file is newer than
it. A second run over unchanged sources compiles nothing.

When finished, you will find the precompiled versions for various platforms in the `dist` folder.

        rdio-scanner-freebsd-amd64-v8.0.0.zip
        rdio-scanner-linux-386-v8.0.0.zip
        rdio-scanner-linux-amd64-v8.0.0.zip
        rdio-scanner-linux-arm64-v8.0.0.zip
        rdio-scanner-linux-arm-v8.0.0.zip
        rdio-scanner-macos-amd64-v8.0.0.zip
        rdio-scanner-macos-arm64-v8.0.0.zip
        rdio-scanner-windows-amd64-v8.0.0.zip

Each archive holds the server binary for its platform and the administrator guide: one self-contained HTML file, pictures inside it, readable offline from the extracted folder. Every archive also carries the ffmpeg licence as plain text under the name `FFMPEG.txt`: the five targets this build compiles an ffmpeg for carry the notice naming the recipe, the source digest and the command that produced the pair, and the three whose pair this build compiled on the machine that is the target carry the notice naming that host, both program digests, and the date the build's ffmpeg check ran for it there.

The tool stamps nothing. The version reaches the binary through the linker flag and the bundles through the environment. The version literals the documentation carries — in `COMPILING.md`, `README.md`, and the administrator guide — are edited by hand at release.

**Happy Rdio scanning !**