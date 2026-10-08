# How to compile

> RDIO SCANNER REPRESENTS A LOT OF INVESTMENT IN TIME, SUPPORT, TESTS AND MATERIALS. BY INTENTIONALLY MODIFYING ITS CODE TO REMOVE ITS IDENTITY, YOU HARM THE OPEN SOURCE PROJECT AND ITS AUTHOR. PLEASE BE RESPECTFUL, SPONSOR THIS PROJECT, USE NATIVE APPS IF POSSIBLE.

The official version of [Rdio Scanner](https://github.com/chuot/rdio-scanner) is compiled on a PC using the current version of Fedora Workstation. You should have no problem building on another platform as long as the prerequisites are available and installed.

## Install the prerequisites

Your os distribution may have all of the following prerequisites available in its own package repository. Make sure, however, that they are at the latest version. This is especially not the case with Ubuntu and its Node.js.

- latest version of git ([here](https://git-scm.com/downloads))
- latest version of gnu make ([here](https://www.gnu.org/software/make/))
- latest long-term support version of Node.js ([here](https://nodejs.org/en/))
- latest version of Go ([here](https://go.dev/dl/))
- latest version of Pandoc with pandoc-pdf ([here](https://pandoc.org/installing.html))
- latest version of TeX Live ([here](https://www.tug.org/texlive/))
- latest version of Info-Zip ([here](http://infozip.sourceforge.net/))
- latest version of podman ([here](https://podman.io/)), for building the container image and for compiling the ffmpeg each target carries

## Compile the app

Clone the official repository on your computer and start the build process.

        git clone https://github.com/chuot/rdio-scanner.git
        cd rdio-scanner
        go run ./cmd/build

The tool compiles the two web applications before it compiles the server, because each bundle is written beside the Go package that embeds it and a server built before both existed would carry a stale one.

The listener application and the administration console are two applications, in `internal/presentation/clients/scanner` and `internal/presentation/clients/admin`, and the modules they both use are in `internal/presentation/clients/shared`. Each is built into the bundle directory of the Go package that embeds it, so `internal/presentation/serving/scanner/bundle` and `internal/presentation/serving/admin/bundle` are build output and hold nothing else.

        go run ./cmd/build client     # both applications
        go run ./cmd/build scanner    # the listener application only
        go run ./cmd/build admin      # the administration console only
        go run ./cmd/build ffmpeg     # the ffmpeg this machine's target carries
        go run ./cmd/build server     # the server binary only, against the bundles on disk
        go run ./cmd/build version    # the version this build carries

The server binary carries both bundles. One executable serves the listener application at `/` and the administration console at `/admin/`.

## The ffmpeg the server carries

A server compiled for `linux-386`, `linux-amd64`, `linux-arm`, `linux-arm64` or `windows-amd64` carries its own `ffmpeg` and `ffprobe` inside its own binary, and converts audio with nothing installed on the host.

        go run ./cmd/build ffmpeg             # the target this machine is
        go run ./cmd/build ffmpeg linux-arm64 # another target, which is how the release matrix cross-builds

The version, the source, the configure recipe and the toolchain of every target are written once, in `internal/infrastructure/ffmpeg/bundle/manifest.json`. The tool fetches that release from ffmpeg.org, checks it against the sha256 the manifest records — ffmpeg.org signs its releases and publishes no checksum, so the pinned digest is what this build trusts — compiles the recipe in a container built from `Containerfile.ffmpeg`, and then runs what it produced through the converter's own argument list before accepting it. podman is what compiles it.

What it fetches and compiles is build output: `.build/ffmpeg` holds the source tarball and the compiled pair, and `internal/infrastructure/ffmpeg/bundle/<target>` holds the gzipped pair the binary embeds. Deleting either costs a rebuild and nothing else. A second build whose ffmpeg version, target and recipe are unchanged compiles nothing again.

A server whose target carries none — `macos-amd64`, `macos-arm64`, `freebsd-amd64`, and a plain `go build` a developer runs — converts with the ffmpeg its host installs, and says so once at startup when it cannot find one.

The licence the compiled ffmpeg is under, the release it was compiled from, and the recipe it was compiled with are written next to the bundle and travel into each release archive as `FFMPEG.md`.

## Say what version you are building

`VERSION.txt` at the repository root is the one place a version is declared. The tool reads it, asks git what it says about the commit being built, and passes the result to the applications through the environment and to the binary through the linker. No source file is rewritten to carry a version.

        8.0.0                       a commit tagged v8.0.0
        8.0.0+g1a2b3c4              a commit no tag names
        8.0.0-beta1                 go run ./cmd/build --pre beta1
        8.0.0-beta1+g1a2b3c4        both at once

A repository with no `VERSION.txt` builds as `0.0.0`, which is a version no release ever had. A `VERSION.txt` that holds something other than a bare `X.Y.Z` stops the build rather than being repaired, and a tag that names a different core than the file does stops it too.

A server built with plain `go build`, without the tool, reports `dev` — in its banner as well as in `-version` — so nobody mistakes it for a release.

The release matrix is still the `Makefile`'s, and it reads the same file:

        make dist          # the precompiled versions for every platform, and their archives
        make container     # the container image

When finished, you will find the precompiled versions for various platforms in the `dist` folder.

        rdio-scanner-freebsd-amd64-v8.0.0.zip
        rdio-scanner-linux-386-v8.0.0.zip
        rdio-scanner-linux-amd64-v8.0.0.zip
        rdio-scanner-linux-arm64-v8.0.0.zip
        rdio-scanner-linux-arm-v8.0.0.zip
        rdio-scanner-macos-amd64-v8.0.0.zip
        rdio-scanner-macos-arm64-v8.0.0.zip
        rdio-scanner-windows-amd64-v8.0.0.zip

**Happy Rdio scanning !**