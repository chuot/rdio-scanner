# How to compile

> RDIO SCANNER REPRESENTS A LOT OF INVESTMENT IN TIME, SUPPORT, TESTS AND MATERIALS. BY INTENTIONALLY MODIFYING ITS CODE TO REMOVE ITS IDENTITY, YOU HARM THE OPEN SOURCE PROJECT AND ITS AUTHOR. PLEASE BE RESPECTFUL, SPONSOR THIS PROJECT, USE NATIVE APPS IF POSSIBLE.

The official version of [Rdio Scanner](https://github.com/chuot/rdio-scanner) is compiled on a PC using the current version of Fedora Workstation. You should have no problem building on another platform as long as the prerequisites are available and installed.

## Install the prerequisites

Your os distribution may have all of the following prerequisites available in its own package repository. Make sure, however, that they are at the latest version. This is especially not the case with Ubuntu and its Node.js.

- latest version of git ([here](https://git-scm.com/downloads))
- latest long-term support version of Node.js ([here](https://nodejs.org/en/))
- latest version of Go ([here](https://go.dev/dl/))
- podman ([here](https://podman.io/)) **or** docker ([here](https://www.docker.com/)), for building the container image and for compiling the ffmpeg: the ffmpeg of every release target is compiled in a container, the FreeBSD target and the two macOS targets among them. The build asks the machine which one it has and uses podman when it has both; `RDIO_CONTAINER_PROGRAM=podman` or `RDIO_CONTAINER_PROGRAM=docker` names the one you want. A container compile is sized by the cores of the machine that program's containers run on, and the build asks each program that question in that program's own words: `info --format {{.NCPU}}` for docker, `info --format {{.Host.CPUs}}` for podman. The program it spoke to, and the form it asked with, are both named when the answer does not arrive. A Mac compiling a macOS pair with the Mac's own toolchain instead needs no container program there, and needs the host's C compiler, `make`, and `strip`
- a container builder that can build a stage naming another operating system: the FreeBSD target's image starts from a stage `FROM --platform=freebsd/amd64`, and only a BuildKit-capable builder builds that line. A Homebrew Docker CLI gets BuildKit from the buildx plugin, `brew install docker-buildx`; podman builds that line on its own. The section on the compile container below says what the failure looks like when the builder cannot

## What to type

Clone the official repository on your computer and start the build process.

        git clone https://github.com/chuot/rdio-scanner.git
        cd rdio-scanner
        go run ./cmd/build

A run that names no command builds this machine's whole tree: both applications, then the ffmpeg this machine's target carries, then the server that carries both. Every other line names one phase, and those phases are the ones that whole run walks, in that order.

        go run ./cmd/build client                 # both applications
        go run ./cmd/build scanner                # the scanner webapp only
        go run ./cmd/build admin                  # the administration console only
        go run ./cmd/build ffmpeg                 # the ffmpeg this machine's target carries
        go run ./cmd/build ffmpeg linux-arm64     # another target, which is how the release matrix cross-builds
        go run ./cmd/build ffmpeg macos-amd64     # the Intel ffmpeg binaries: in a container here, on the Mac's own toolchain on a Mac
        go run ./cmd/build server                 # the server binary only, against the bundles on disk
        go run ./cmd/build dist                   # the archive of the platform this machine is
        go run ./cmd/build dist --all             # the release matrix, and the archives it writes
        go run ./cmd/build dist linux-arm64       # one target, which is how the matrix is built one at a time
        go run ./cmd/build container              # the container image, built locally and never pushed
        go run ./cmd/build clean                  # the release output, the client dependencies, and both bundle contents
        go run ./cmd/build test                   # this project's suites, and nothing compiled
        go run ./cmd/build version                # the version this build carries

The ffmpeg line compiles one target's pair in a container, on any machine that has a container program, the FreeBSD target and the two macOS targets among them. A Mac is the one host that compiles a target's pair with its own compiler, linker and signer instead, and it does that for the two macOS targets, of either architecture:

        go run ./cmd/build ffmpeg   # this machine's target, in a container, or a macOS pair, on a Mac, with the Mac's own toolchain

### The flags the tool accepts

        --pre beta1     the prerelease name the version carries
        -o PATH         where the server binary is written: the whole-tree run and the server run
        --all           the release matrix: the dist run, and nothing else
        --allow-dirty   release a tree that has uncommitted work in it: the dist run

A flag a run cannot honour is refused with the command line that would have worked, and the run exits `2` without building anything.

### The environment the tool reads

        RDIO_BUILD_JOBS        how many server compiles a release run may have going at once
        RDIO_CONTAINER_PROGRAM podman, or docker: the container program the run speaks to

The release run's bound is the machine's cores, the targets it is ready to build, and `RDIO_BUILD_JOBS`, whichever is the fewest. An ffmpeg that run compiles inside a container is sized on its own, by the cores of the machine that program's containers run on, counted by asking that program in its own form: `info --format {{.NCPU}}` when the run speaks to docker, `info --format {{.Host.CPUs}}` when it speaks to podman. Asking one program the other's question answers nothing, and a container compile whose cores cannot be counted is refused in words naming the program and the form it was asked with. An ffmpeg compiled with the host's own toolchain, as a Mac does for its own macOS pair, is sized by that host's cores, because that host is the machine compiling. The build asks the machine which container program it has and speaks to podman when it has both; naming a program the machine does not have, and naming something that is not a container program, are both refused.

### What the exit code says

        0   the run did what it was asked to do
        1   a step failed, or the tree was refused
        2   the command line is not one the tool accepts
        3   a release run wrote fewer archives than the matrix it was asked for

`go run` reports its own exit code, so a `3` asked through it arrives as `1`. The compiled tool carries the code itself: `go build -o buildtool ./cmd/build`. A release run that ends `3` wrote the archives it wrote, and says which targets wrote none.

### Where the lines go

The run's own lines — the version it resolved, the notes, and every `label  result` line — reach standard output, and a program the run started writes its own output there too. A failure, and a program's own complaints, reach standard error: the line that says the build stopped is not a line of the build's progress, and a log of progress that carries it reads as if the build got there. A command line the tool does not accept is refused there too, with the usage line naming the commands the tool has, and the run builds nothing.

## How to verify a build

The suites are the verification, and the tool runs both of them and compiles nothing:

        go run ./cmd/build test

They are the same suites, run by hand:

        go test ./...
        npm test

Then ask the artifact what it says it is:

        go run ./cmd/build version                    # the version this tree resolves
        ./dist/linux-amd64/rdio-scanner -version      # the version the binary carries
        podman inspect rdio-scanner:latest            # the image's own labels, the version among them
        docker inspect rdio-scanner:latest            # the same, from the program that built it

A release archive carries the server binary for its platform, the administrator guide as one self-contained HTML file, and `FFMPEG.txt`.

A second run over unchanged sources compiles nothing: a run that says `reusing the ffmpeg compiled for` — `reusing the ffmpeg built for` on the one host that compiles with its own toolchain — and prints no `server` line built nothing, and that is the phase rule doing its job.

## Why it is this way

### The two applications, and the order they are built in

The tool compiles the two web applications before it compiles the server, because each bundle is written beside the Go package that embeds it and a server built before both existed would carry a stale one.

The scanner webapp and the administration console are two applications, in `internal/presentation/scanner/frontend` and `internal/presentation/admin/frontend`, and the modules they both use are in `internal/presentation/common/frontend`. Each is built into the bundle directory of the Go package that embeds it, so `internal/presentation/scanner/frontend/bundle` and `internal/presentation/admin/frontend/bundle` are build output and hold nothing but the `.gitkeep` that keeps a plain `go build` compiling.

The server binary carries both bundles. One executable serves the scanner webapp at `/` and the administration console at `/admin/`.

### The ffmpeg the server carries

A server compiled for any of the eight release targets carries its own `ffmpeg` and `ffprobe` inside its own binary, and converts audio with nothing installed on the host. The ffmpeg binaries are compiled for audio: the recipe tells the source `--disable-everything` and enables the audio demuxers, decoders, encoder, muxers and filters this server converts with, plus the `pipe`, `fd` and `file` protocols a recording arrives and leaves on, and `--disable-network` keeps libavformat's socket layer out of the build, so no video codec, video decoder, video encoder, and no network protocol is compiled in. The ffmpeg of every release target is compiled in a container, by this build, on the machine you run it on, from the source release and the toolchain its record pins: the four Linux targets and Windows amd64 as programs of the operating systems they run on, `freebsd-amd64` against a FreeBSD sysroot copied out of the FreeBSD project's own toolchain image, and the two macOS targets against an Apple SDK tree prepared outside this repository. The machine that compiles them runs no FreeBSD program and no macOS program, so those three pairs are compiled and not run: the build reads them out of the files their own link wrote, and the archive's `FFMPEG.txt` says which of the two proofs it is describing. A Mac is the one host that compiles a pair with its own toolchain instead, and the two macOS architectures compile each other's ffmpeg binaries, so one Apple Silicon Mac compiles both macOS archives.

The version, the source, the configure recipe and the toolchain of every target this build compiles are written once, in `internal/infrastructure/ffmpeg/bundle/manifest.json`. The tool fetches that release from ffmpeg.org, checks it against the sha256 the manifest records — ffmpeg.org signs its releases and publishes no checksum, so the pinned digest is what this build trusts — compiles the recipe in a container built from `Containerfile.ffmpeg`, and for `freebsd-amd64` from `Containerfile.ffmpeg.freebsd`, and then proves what it produced. A container program is what compiles it: the build asks the machine for podman, and for docker when podman is not there. Every image the build compiles or proves inside is pinned in the manifest at the digest of its index, which is the digest that cannot move under a tag, and the run resolves the image that index names before it starts: the image it resolved is what the licence text names, and what the cache entry is keyed on.

The compile is the check. Before accepting the binaries the build asks them what they register — `ffmpeg -hide_banner -codecs`, read the way the program prints it — and refuses binaries that register a video decoder or a video encoder, naming them. That question is asked in the same code for every ffmpeg binary the build can start: the four Linux pairs and the Windows pair answer it inside the container that compiled them, under qemu for the 386 and ARM targets and under wine in the second container the record pins, and the binaries a Mac compiled with its own toolchain, and the binaries that Mac adopts, answer it on the Mac that ran them. What a record names decides how a pair is proved, and a record naming a run proof and a read proof at once is refused. `freebsd-amd64`, `macos-amd64` and `macos-arm64` name reads, because no machine this build reaches runs their operating system, and what the build reads out of the produced files is what their own link wrote into them: for the FreeBSD pair, a 64-bit ELF executable of x86-64 machine code, the operating-system byte a FreeBSD sysroot stamps on everything it links, program headers that name no loader to start the pair under and no dynamic section to ask one for, and a comment section naming the linker the record's own flags name beside a FreeBSD clang; for a macOS pair, a Mach-O executable, the load command naming the entry point its loader starts at, the one naming the dynamic loader every macOS program starts under, the one loading the system library, and — in the arm64 record, because that is the architecture Apple's loader refuses to start unsigned — the ad-hoc code signature its link wrote. A read says how a program was linked and what it was linked for, and it does not say what the program does: the licence text names which of the two proofs it describes, and names plainly that these two programs were compiled and never started. The binaries a build adopts answer the same question as the binaries it compiles, because there is one gate for the binaries it can run: two gates would be two standards, and the release adopting on the weaker one is not visible from outside the build.

The pinned release has no `--disable-video` switch: the option is not in that release's own `configure`, and `configure` dies on an option it does not know, so the video exclusion is what `--disable-everything` already does, and the codec probe is what reads it back. The compiled program carries the video filters ffmpeg's own program selects for its output handling — `crop`, `format`, `hflip`, `null`, `rotate`, `transpose`, `trim`, `vflip`, and the `buffer` and `buffersink` filters every filter graph is built on — and no configure switch takes them out of a build that compiles the program at all. They decode and encode nothing, so the audio-only promise is about codecs, decoders, encoders and the network, which is what the recipe decides and the codec probe reads back.

What it fetches and compiles is build output: `.build/ffmpeg` holds the source tarball and the compiled binaries, and `internal/infrastructure/ffmpeg/bundle/<target>` holds the gzipped binaries the binary embeds. Deleting either costs a rebuild and nothing else. A second build whose ffmpeg version, target and recipe are unchanged compiles nothing again. The cache directory is named by a digest of exactly those inputs — the source release, the recipe, the target's toolchain and the digests its images are pinned at, the environment the pair is proved in and the digest that environment is pinned at, the Containerfile the compile container is built from, the sysroot tree a record pins with the digest its preparation recorded and the tool that prepared it, and the flags and make program of the host compile a record names — so a recipe that gains a switch recompiles every target once, and a toolchain image or a prepared SDK tree that moves moves that target's entry with it.

### The container every target's ffmpeg is compiled in

Every target's pair is compiled in a container, on the machine you run the build on, by that container's own compiler and linker. There is no target this build compiles with the host's toolchain, because a host's toolchain carries its own operating system's headers, libraries and startup files and a container carries the ones the target runs on. Three of the eight compiles read an operating system that is not the container's own, and all three read a tree they were handed and run none of it.

`freebsd-amd64` is compiled by the container `Containerfile.ffmpeg.freebsd` builds. That file starts from the FreeBSD project's own toolchain image at the digest the record pins, and copies its whole tree — includes, libraries and startup files, arranged as the FreeBSD project arranges them — into the container the compiler actually works in, where clang and `lld` build the pair with the record's cross switches and its `-static` link. The FreeBSD stage of that file is copied out of and never run: its own instructions, executed on a Linux host, exit `139`, which is an operating system saying that it is not this machine's operating system. Nothing in this route boots FreeBSD, starts a FreeBSD program, or installs a FreeBSD package on the machine running the build, and nothing that image carries is written into this repository or into an image this build produces.

The two macOS targets are compiled by the container `Containerfile.ffmpeg` builds, with clang, `lld`, `llvm-ar` and `llvm-ranlib` from the packages their records name, and they link against an Apple SDK tree mounted read-only at the path each record spells into its own compiler and linker flags. That tree is the next section's subject, and the compile is what reads it. A Darwin compile archives with the LLVM archiver and publishes the pair exactly as the link wrote them, with no strip step touching them: strip rewrites the bytes an ad-hoc code signature is computed over, and on the arm64 target the signature that link wrote is one of the things this build reads back out of the program it produced.

What the machine that compiled those three pairs can and cannot say about them is the read the section above describes, and the archive's `FFMPEG.txt` is what says it out loud: `compiled and not run` is the compile step's own line, and the notice names the properties the build read and states that a read does not say what a program does.

Both images above are built on the machine running the build, and the builder there has to be BuildKit-capable. A Docker CLI without the buildx plugin hands the build to the classic builder, and the classic builder cannot build `FROM --platform=freebsd/amd64` at all: it answers `operating system is not supported` for that stage, at `Step 4/11` for the `Containerfile.ffmpeg.freebsd` this repository carries. It names no file and no instruction beyond that step number, which is why the failure reads as a FreeBSD problem rather than a builder problem. `DOCKER_BUILDKIT=0 docker build -f Containerfile.ffmpeg.freebsd .build/ffmpeg/context` brings the failure back without asking a release run anything, and the same command with BuildKit enabled copies the sysroot stage and reports it `CACHED`. The step number counts only the instructions ahead of that stage, so it moves whenever the Containerfile gains a line: match the message and the instruction, not the number.

### The Apple SDK a macOS compile links against

The two macOS records name the SDK version their link reads, the digest the preparation of that SDK tree recorded, and the tool that prepared the tree. The tree is not something this project can carry: an Apple SDK is Apple's headers, its libraries and its stub libraries, and it belongs in neither repository. The build looks for it in

        ~/.cache/rdio-scanner/ffmpeg-darwin-sdk/<sdk>/MacOSX<sdk>.sdk

or in the directory `RDIO_FFMPEG_DARWIN_SDK` names, and it accepts what it found only when two things hold: the tree carries `usr/lib/libSystem.tbd`, the stub library a macOS link reads, and the directory holding the tree carries a file named `sha256` whose contents are the digest the record names. Preparing one is therefore three steps on the machine that compiles: obtain Apple's SDK for the version the record names, lay it out under that directory as the SDK's own tree, and write the tree's own sha256 into that file beside it. It stays build input: the build never writes it, no run commits it, and no image this build produces carries it.

A compile whose tree is not there is refused before anything is compiled, with the sentence naming the target, the SDK version its record pins, the directory the build looked in, and the variable that can name another one. A pair the build's cache already holds is not a compile and asks for no tree: the run says it is reusing the ffmpeg it compiled for that target, and publishes the licence text that compile wrote. An empty directory, a half-prepared tree, and a tree of another version are the same refusal, because the build asks the tree for the library a macOS link reads. A tree standing where the pinned tree is kept and recording beside it a digest that is not the one the record names, or recording no digest at all, is refused naming the directory, the digest the record names, and what the tree records instead of it. A run asked for that one target's ffmpeg ends `1` there, because the one thing it was asked for is the thing it cannot produce. A `dist` run answers in the shape a release can answer: it names that target as writing none, writes the archives whose compiles it can start, says in its own summary line which targets wrote none, and ends `3` — a release run that stopped at the first target missing an input would ship nothing at all, on the machine that produces every archive this project publishes.

### The macOS pair a Mac compiles with its own toolchain

A Mac is the one host that compiles a target's pair with its own toolchain, and it does it for the two macOS targets, of either architecture. There is no container in that route and no SDK tree mounted in it: the host's own compiler, linker and signer produce the pair, because a Mac is the one machine this build reaches that has Apple's toolchain and the SDK that toolchain ships with. `go run ./cmd/build ffmpeg` on that Mac fetches the same source release the manifest pins, checks it against the pinned digest, unpacks it, runs the same trimmed configure recipe with the record's flags, runs the make program its record names, strips the two programs, and signs them ad-hoc after the strip, because strip rewrites the bytes the signature is computed over.

The two macOS architectures compile each other's ffmpeg binaries, and the host's compiler is told which architecture to produce when the host is not the target's architecture: `--arch=x86_64` or `--arch=aarch64` and `--enable-cross-compile` in the configure, and `-arch x86_64` or `-arch arm64` in the extra compiler and linker flags beside the record's own flags. Those flags are derived from the host and the target and are written nowhere: the manifest records what the compile is built with, and the architecture the host must produce is a property of the machine standing in front of it. The cache entry is keyed by the manifest's record and the target — the host flags and the make program the record names are among its inputs — so a compile across the two architectures is the entry of its own target and never shares an entry with another target's. A Mac asked for one of the other six targets compiles it the way every other machine compiles it, in a container, and asks its own toolchain nothing. The target word is the one the release matrix spells, and the build accepts the words go names a macOS machine by: `darwin-amd64` is `macos-amd64`, and `darwin-arm64` is `macos-arm64`.

The run then proves the binaries it produced through the gate it adopts a drop under, and that gate reads the file before it starts it: the Mach-O header and the load commands their target's own linker writes, and then the converter's own argument list, the recordings the server stores, the lengths the filters ask for, the tags the server writes, and what the binaries register. A pair is refused for the shape of the file as much as for what it converts: a universal binary holding two architectures, a 32-bit Mach-O, a Mach-O whose machine code is not the architecture the target runs, one that is not a program its loader can start, one with no entry point named in it, one that names no dynamic loader or no system library, one with its code signature absent where its record asks for one, and binaries that register a video decoder or a video encoder. The run records the codecs the binaries register, named, beside the conversions it performed, and writes nothing down: the run is the evidence, and it lives in the build that performed it.

The run is the binaries' own execution, so it is performed by a host that runs them. An Apple Silicon Mac runs the Intel binaries through Rosetta 2, and the host the licence text names is the host that ran them. A host that compiled binaries it cannot execute — an Intel Mac that compiled the arm64 binaries — stops at the run: the binaries do not run, the operating system says so, and the build names the Mac that runs them, the command that compiles them there, and the directory they are sitting in, caches nothing, writes no licence text, and leaves `manifest.json` as it found it. The binaries stay where they were compiled, and the operator carries them to the other Mac. The build asks the host nothing about what it has installed, and asks its compiler nothing about what it can produce: binaries of the host's own architecture are refused by the shape rule that names both architectures, before a cache entry, a licence text, or an archive exists.

Then it gzips them into the build's cache, writes the licence text into that cache entry, publishes `ffmpeg.gz`, `ffprobe.gz` and `LICENCE.md` into the bundle directory, and removes the raw binaries from it, so no server binary carries an uncompressed program. `go run ./cmd/build dist` on that host writes the archive that carries them, and the next run finds them in the cache and compiles nothing. The manifest is a tracked file this build reads and never writes: nothing a build learns about binaries it ran goes into a file a later build reads back, because the build ran them.

Binaries built by hand are adopted the same way, and the adoption is the same gate: drop `ffmpeg` and `ffprobe` in `internal/infrastructure/ffmpeg/bundle/<target>`, and the build on that host reads them, runs them and adopts what converts. The route is the two macOS targets on a host that runs macOS: every other target's compile is a container compile wherever the build runs, and a program dropped in one of their directories is embedded in the binary and used by nothing. The build adopts binaries on two things and nothing else: that the file holds the machine code the target runs, and that the binaries convert audio and register no video codec. Binaries of the other architecture, binaries that register a video codec, and binaries that convert nothing are each refused, naming what is wrong. A drop wins over the compile every time: it is what its operator built and tested on the machine that runs it, and the build never compiles over it. The adoption is accepted from a macOS host of either architecture, because it is the binaries' own execution and an Apple Silicon Mac executes an Intel program through Rosetta 2, and binaries a host of another operating system cannot execute are refused naming the host that can run them, the command that compiles them there, and the directory the refused pair is sitting in.

There is no target whose binaries a release run adopts from a machine it cannot compile them on: the drop is read, run and adopted on the Mac that adopted it, and every other target's pair comes out of the container that compiled it. The run asked for a macOS target's ffmpeg on a machine with no prepared SDK tree is refused naming the input that is missing, and it compiles nothing for that target; a `dist` run asked for several targets carries on to the archives whose compiles it can start and names the targets that wrote none in the line it ends with.

A server built with plain `go build`, without the tool, carries no ffmpeg at all: it converts with the ffmpeg its host installs, and says so once at startup when it cannot find one.

The licence the ffmpeg is under, the release it comes from, and the recipe it was built with are written next to the bundle and travel into every release archive as `FFMPEG.txt`. Every one of those notices states the posture the recipe keeps — this program is compiled for audio, no video codec, video decoder, video encoder, and no network protocol is compiled in — and names the codecs the recipe selects, and both sentences are composed from the recipe itself, so a recipe that drops a switch stops promising the build that switch backs and the notice cannot promise what the recipe does not produce. For the binaries this build compiled, the notice names the container image and the digest its index pins, the Containerfile that image was built from, the source release and its digest, the configure recipe, and the command that produced the pair, all of which sit in this repository and are reproducible from it. Where the target's operating system is not the one the compile ran on, it names the sysroot the link read as well: for `freebsd-amd64`, the FreeBSD project's toolchain image and the digest it is pinned at; for the two macOS targets, Apple's SDK version, the tool that prepared that tree outside these repositories, and the digest that preparation recorded, beside the sentence that the compile reaches the tree through a mount that exists only while the container that compiled them runs, and that the image carries no copy of it. And the notice names the proof the build performed, which is not something a reader can tell from a program: that the pair was started in the container that compiled them, or started under a named runner in a container its record pins, or that nothing was started anywhere and what stands in place of a run is the list of things the build read out of the two programs, in the order it read them, with the sentence that says a read describes how a program was linked and what it was linked for, and not what it does. For binaries a host adopted, the notice names the host that ran them, the codecs that run recorded, and the conversions it performed: the licence text of adopted binaries is written into the cache entry by the run that adopted them, and a run reusing that entry publishes the words that run wrote. A cache entry older than that habit says only what the manifest says, and names this machine's cache as where the binaries came from.

### Say what version you are building

`VERSION.txt` at the repository root is the one place a version is declared. The tool reads it, asks git what it says about the commit being built, and passes the result to the applications through the environment and to the binary through the linker. No source file is rewritten to carry a version.

        8.0.0                       a commit tagged v8.0.0
        8.0.0+g1a2b3c4              a commit no tag names
        8.0.0-beta1                 go run ./cmd/build --pre beta1
        8.0.0-beta1+g1a2b3c4        both at once

A repository with no `VERSION.txt` builds as `0.0.0`, which is a version no release ever had. A `VERSION.txt` that holds something other than a bare `X.Y.Z` stops the build rather than being repaired, and a tag that names a different core than the file does stops it too.

A server built with plain `go build`, without the tool, reports `dev` — in its banner as well as in `-version` — so nobody mistakes it for a release.

### The release matrix, and what a release run refuses

`dist` alone builds the one archive the machine running it can run, which is what a developer builds to try what they just built. The eight-target matrix is a release, and asking for it is a deliberate act: `--all` is the flag that asks for it, and it belongs to `dist` and to nothing else. `--all` beside a named target is refused, and a machine whose platform is in no release target is refused naming `--all`.

A phase that has nothing to do does not run. A bundle is rebuilt when a file it is built from is newer than the bundle, and a server binary is rebuilt when a Go file or an embedded bundle file is newer than it. A second run over unchanged sources compiles nothing.

When finished, you will find the precompiled versions for various platforms in the `dist` folder.

        rdio-scanner-freebsd-amd64-v8.0.0.zip
        rdio-scanner-linux-386-v8.0.0.zip
        rdio-scanner-linux-amd64-v8.0.0.zip
        rdio-scanner-linux-arm64-v8.0.0.zip
        rdio-scanner-linux-arm-v8.0.0.zip
        rdio-scanner-macos-amd64-v8.0.0.zip
        rdio-scanner-macos-arm64-v8.0.0.zip
        rdio-scanner-windows-amd64-v8.0.0.zip

Each archive holds the server binary for its platform and the administrator guide: one self-contained HTML file, pictures inside it, readable offline from the extracted folder. Every archive also carries the ffmpeg licence as plain text under the name `FFMPEG.txt`, and every one of those notices names the recipe, the source release and its digest, the command that produced the pair, and the toolchain that compiled it: the container image and the digest that image's index pins, and, where the target's operating system was not the one the compile ran on, the sysroot the link read. Each says the binaries are compiled for audio, and each says which proof the build performed — the pair started in the container that compiled them, started under qemu or under wine in the container its record names, or read out of the two files the compile left and never started anywhere. A notice written by a Mac that adopted binaries it did not compile names the host that ran them, the codecs that run recorded, and the conversions it performed.

The tool stamps nothing. The version reaches the binary through the linker flag and the bundles through the environment. The version literals the documentation carries — in `COMPILING.md`, `README.md`, and the administrator guide — are edited by hand at release.

**Happy Rdio scanning !**
