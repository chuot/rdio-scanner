# How to compile

> RDIO SCANNER REPRESENTS A LOT OF INVESTMENT IN TIME, SUPPORT, TESTS AND MATERIALS. BY INTENTIONALLY MODIFYING ITS CODE TO REMOVE ITS IDENTITY, YOU HARM THE OPEN SOURCE PROJECT AND ITS AUTHOR. PLEASE BE RESPECTFUL, SPONSOR THIS PROJECT, USE NATIVE APPS IF POSSIBLE.

The official version of [Rdio Scanner](https://github.com/chuot/rdio-scanner) is compiled on a PC using the current version of Fedora Workstation. You should have no problem building on another platform as long as the prerequisites are available and installed.

## Install the prerequisites

Your os distribution may have all of the following prerequisites available in its own package repository. Make sure, however, that they are at the latest version. This is especially not the case with Ubuntu and its Node.js.

- latest version of git ([here](https://git-scm.com/downloads))
- latest long-term support version of Node.js ([here](https://nodejs.org/en/))
- latest version of Go ([here](https://go.dev/dl/))
- latest version of podman ([here](https://podman.io/)), for building the container image and for compiling the ffmpeg of the five targets this build compiles in a container. The macOS and FreeBSD ffmpeg binaries are compiled by this build on a machine that runs the target's operating system — a Mac for the two macOS targets, either architecture for either, and a FreeBSD box for the FreeBSD target — and that machine needs a C compiler, `strip`, and the make program its record names — `gmake` on FreeBSD, where ffmpeg's build asks for GNU make and the base make is not it — and needs no podman

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
        go run ./cmd/build server                 # the server binary only, against the bundles on disk
        go run ./cmd/build dist                   # the archive of the platform this machine is
        go run ./cmd/build dist --all             # the release matrix, and the archives it writes
        go run ./cmd/build container              # the container image, built locally
        go run ./cmd/build clean                  # the release output, the client dependencies, and both bundle contents
        go run ./cmd/build version                # the version this build carries

The server binary carries both bundles. One executable serves the scanner webapp at `/` and the administration console at `/admin/`.

## The ffmpeg the server carries

A server compiled for any of the eight release targets carries its own `ffmpeg` and `ffprobe` inside its own binary, and converts audio with nothing installed on the host. The ffmpeg binaries are compiled for audio: the recipe tells the source `--disable-everything` and enables the audio demuxers, decoders, encoder, muxers and filters this server converts with, plus the `pipe`, `fd` and `file` protocols a recording arrives and leaves on, and `--disable-network` keeps libavformat's socket layer out of the build, so no video codec, video decoder, video encoder, and no network protocol is compiled in. Five of the eight targets — `linux-386`, `linux-amd64`, `linux-arm`, `linux-arm64` and `windows-amd64` — are compiled by this build, in a container, from the source the manifest pins. The other three — `macos-amd64`, `macos-arm64` and `freebsd-amd64` — are compiled on the platform each one runs on, and on that platform this build compiles them itself: the same source release, the same recipe, the host's own compiler and linker. One command there compiles them, runs them, and adopts what converts into the bundle its server embeds. The rule for those three is the operating system, and the architecture inside it is not: the two macOS architectures compile each other's ffmpeg binaries, so one Apple Silicon Mac compiles both macOS archives, and FreeBSD is compiled on FreeBSD.

        go run ./cmd/build ffmpeg                    # the target this machine is
        go run ./cmd/build ffmpeg linux-arm64        # another target, which is how the release matrix cross-builds
        go run ./cmd/build ffmpeg macos-amd64        # the Intel ffmpeg binaries, compiled on an Apple Silicon Mac

The version, the source, the configure recipe and the toolchain of every target this build compiles are written once, in `internal/infrastructure/ffmpeg/bundle/manifest.json`. The tool fetches that release from ffmpeg.org, checks it against the sha256 the manifest records — ffmpeg.org signs its releases and publishes no checksum, so the pinned digest is what this build trusts — compiles the recipe in a container built from `Containerfile.ffmpeg`, and then runs what it produced through the converter's own argument list before accepting it. podman is what compiles it.

The compile is the check. Before accepting the binaries the build asks them what they register — `ffmpeg -hide_banner -codecs`, read the way the program prints it — and refuses binaries that register a video decoder or a video encoder, naming them. That question is asked in the same code for every ffmpeg binary: the compiled ones answer it inside the container that can run them, under qemu for the 386 and ARM targets and under wine for Windows, and the binaries a host compiled, and the binaries it adopts, answer it on the host that compiled them. Two gates would be two standards, and the release adopting on the weaker one is not visible from outside the build.

The pinned release has no `--disable-video` switch: the option is not in that release's own `configure`, and `configure` dies on an option it does not know, so the video exclusion is what `--disable-everything` already does, and the codec probe is what reads it back. The compiled program carries the video filters ffmpeg's own program selects for its output handling — `crop`, `format`, `hflip`, `null`, `rotate`, `transpose`, `trim`, `vflip`, and the `buffer` and `buffersink` filters every filter graph is built on — and no configure switch takes them out of a build that compiles the program at all. They decode and encode nothing, so the audio-only promise is about codecs, decoders, encoders and the network, which is what the recipe decides and the codec probe reads back.

What it fetches and compiles is build output: `.build/ffmpeg` holds the source tarball and the compiled binaries, and `internal/infrastructure/ffmpeg/bundle/<target>` holds the gzipped binaries the binary embeds. Deleting either costs a rebuild and nothing else. A second build whose ffmpeg version, target and recipe are unchanged compiles nothing again. The cache directory is named by a digest of exactly those inputs — the source release, the recipe, the target's toolchain, the check environment, and for a supplied target the way it is produced, the make program and the flags its compile passes — so a recipe that gains a switch recompiles every target once.

### The ffmpeg binaries compiled on a machine that runs their operating system

A macOS ffmpeg and a FreeBSD ffmpeg are compiled on a Mac and on a FreeBSD machine: the machine that produces the releases runs neither program, so a release run there compiles the five it can, skips these three, and says what would have made the rest buildable. On the Mac, and on the FreeBSD box, the build does this compile itself:

        go run ./cmd/build ffmpeg   # on a machine that runs the target's operating system: macos-amd64, macos-arm64, freebsd-amd64

It fetches the same source release the manifest pins, checks it against the pinned digest, unpacks it, runs the same trimmed configure recipe with the record's flags, runs the make program the record names, strips the two programs, and on macOS signs them ad-hoc after the strip, because strip rewrites the bytes the signature is computed over. There is no container: the host's own compiler and linker produce the binaries, which is the whole reason they are compiled there and not shipped from a cross-compiler.

The two macOS architectures compile each other's ffmpeg binaries, and the host's compiler is told which architecture to produce when the host is not the target's architecture: `--arch=x86_64` or `--arch=aarch64` and `--enable-cross-compile` in the configure, and `-arch x86_64` or `-arch arm64` in the extra compiler and linker flags beside the record's own flags. Those flags are derived from the host and the target and are written nowhere: the manifest records what the compile is built with, and the architecture the host must produce is a property of the machine standing in front of it. The cache entry is keyed by the manifest's record and the target, so a compile across the two architectures is the entry of its own target and never shares an entry with the one the target's own host compiled. FreeBSD is compiled on FreeBSD, the one architecture FreeBSD is built for here, and a macOS host asked for FreeBSD binaries compiles nothing. The target word is the one the release matrix spells, and the build accepts the words go names a macOS machine by: `darwin-amd64` is `macos-amd64`, and `darwin-arm64` is `macos-arm64`.

The run then proves the binaries it produced with the same gate it adopts a drop under — the converter's own argument list, the recordings the server stores, the lengths the filters ask for, the tags the server writes, and what the binaries register — and refuses them if they are not the programs their target runs: a fat Mach-O, a Mach-O whose machine code is not the architecture the target runs, an ELF that is not 64-bit x86-64, an ELF branded for another operating system, binaries that register a video decoder or a video encoder. The run records the codecs the binaries register, named, beside the conversions it performed, and writes nothing down: the run is the evidence, and it lives in the build that performed it.

The run is the binaries' own execution, so it is performed by a host that runs them. An Apple Silicon Mac runs the Intel binaries through Rosetta 2, and the host the licence text names is the host that ran them. A host that compiled binaries it cannot execute — an Intel Mac that compiled the arm64 binaries — stops at the run: the binaries do not run, the operating system says so, and the build names the Mac that runs them, the command that compiles them there, and the directory they are sitting in, caches nothing, writes no licence text, and leaves `manifest.json` as it found it. The binaries stay where they were compiled, and the operator carries them to the other Mac. The build asks the host nothing about what it has installed, and asks its compiler nothing about what it can produce: binaries of the host's own architecture are refused by the shape rule that names both architectures, before a cache entry, a licence text, or an archive exists.

Then it gzips them into the build's cache, writes the licence text into that cache entry, publishes `ffmpeg.gz`, `ffprobe.gz` and `LICENCE.md` into the bundle directory, and removes the raw binaries from it, so no server binary carries an uncompressed program. `go run ./cmd/build dist` on that host writes the archive that carries them, and the next run finds them in the cache and compiles nothing. The manifest is a tracked file this build reads and never writes: nothing a build learns about binaries it ran goes into a file a later build reads back, because the build ran them.

Binaries built by hand are adopted the same way, and the adoption is the same gate: drop `ffmpeg` and `ffprobe` in `internal/infrastructure/ffmpeg/bundle/<target>`, and the build on that host runs them and adopts what converts. The route is the three targets compiled on their own platform: the five this build compiles in a container take the binaries the container produced, and a program dropped in one of their directories is embedded in the binary and used by nothing. The build adopts binaries on two things and nothing else: that the file holds the machine code the target runs, and that the binaries convert audio and register no video codec. Binaries of the other architecture, binaries that register a video codec, and binaries that convert nothing are each refused, naming what is wrong. A drop wins over the compile every time: it is what its operator built and tested on the machine that runs it, and the build never compiles over it. The adoption is accepted from a macOS host of either architecture, because it is the binaries' own execution and an Apple Silicon Mac executes an Intel program through Rosetta 2, and binaries a host of another operating system cannot execute are refused naming the host to compile them on.

A supplied target with nothing dropped for it, on a machine that is not its platform, is not a failure. The build says so, names the host that compiles the binaries, the command that compiles them there, and the directory to drop them in, and builds no server and no archive for that target. In a matrix run the other archives are written and the run ends successfully.

A server built with plain `go build`, without the tool, carries no ffmpeg at all: it converts with the ffmpeg its host installs, and says so once at startup when it cannot find one.

The licence the ffmpeg is under, the release it comes from, and the recipe it was built with are written next to the bundle and travel into every release archive as `FFMPEG.txt` — the notice naming the recipe, the source digest and the command that produced the binaries for the five this build compiles in a container, and the notice naming the host that compiled them, the command that built them there, and the codecs the run that adopted them recorded, for the other three. The notice states the posture the recipe keeps — this program is compiled for audio, no video codec, video decoder, video encoder, and no network protocol is compiled in — and that sentence is composed from the recipe, so a recipe that drops a switch stops promising the build that switch backs and the notice cannot promise what the recipe does not produce. The licence text of adopted binaries is written into the cache entry by the run that adopted them, and a run reusing that entry publishes the words that run wrote: the host it ran them on, the codecs they reported, and the conversions it performed. A cache entry older than that habit says only what the manifest says, and names this machine's cache as where the binaries came from.

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

Each archive holds the server binary for its platform and the administrator guide: one self-contained HTML file, pictures inside it, readable offline from the extracted folder. Every archive also carries the ffmpeg licence as plain text under the name `FFMPEG.txt`: the five targets this build compiles an ffmpeg for carry the notice naming the recipe, the source digest and the command that produced the binaries, and the three whose ffmpeg binaries this build compiled on a machine that runs its operating system carry the notice naming that host, the command that built them there, and the codecs the run that adopted them recorded. Each notice says the binaries are compiled for audio.

The tool stamps nothing. The version reaches the binary through the linker flag and the bundles through the environment. The version literals the documentation carries — in `COMPILING.md`, `README.md`, and the administrator guide — are edited by hand at release.

**Happy Rdio scanning !**