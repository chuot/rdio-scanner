# The image is built in two stages: one compiles the server, the other ships it. The stage that
# compiles runs on the architecture of the machine doing the build, so it uses that machine's own
# compiler and nothing is emulated; the stage that ships is the architecture the build was asked for,
# which is the one the image runs on. One file then builds an image for any architecture the build is
# asked for. A single-stage file cannot do that: its compile and its shipped system would be the same
# architecture, so building for another one would mean installing and running a foreign compiler.

# Declared above the FROM that reads it. Below that line the build argument has no value yet, and the
# stage that compiles is pulled for the architecture being built for instead of for the machine doing
# the build: the container program says so out loud when the order is the other way round.
ARG BUILDPLATFORM

# The stage that compiles. TARGETOS and TARGETARCH are the operating system and architecture the
# container program builds for, filled in from the platform it is given, and the compile below reads
# them: the architecture is named there once, and nowhere else in this file.
FROM --platform=$BUILDPLATFORM docker.io/alpine:3.24.2 AS build

ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

COPY go.mod go.sum ./
COPY cmd/ cmd/
COPY internal/ internal/

# The ffmpeg this image converts with is the one compiled for ${TARGETOS}-${TARGETARCH} and embedded in
# the binary, so the image installs no ffmpeg package. The bundle has to be on disk before the image is
# built, which is what `go run ./cmd/build container <target>` does: it compiles that target's ffmpeg
# first. Saying so here is what keeps an image built straight from a fresh clone from shipping a server
# that cannot convert anything.
# The compile below carries by hand the flags the build tool passes when it compiles this same server:
# the linker strips the symbol table and the debug information, and the compiler is asked to record each
# source path as the module that holds it rather than as a folder on the machine building the image. An
# image build does not ask the build tool to compose its Go compile, so that posture holds only because
# this line says so. GOOS and GOARCH name the architecture the build was asked for, and CGO is turned off
# so that the compile needs no C toolchain for it.
RUN apk --no-cache --no-progress add go && \
    { test -f internal/infrastructure/ffmpeg/bundle/${TARGETOS}-${TARGETARCH}/ffmpeg.gz || { \
        echo "the ffmpeg this image carries was not compiled: run go run ./cmd/build container ${TARGETOS}-${TARGETARCH} first" >&2; exit 1; \
    }; } && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.Version=$VERSION" -o ./rdio-scanner ./cmd/rdio-scanner

# The stage that ships. No platform is named on this line: the container program builds this stage for
# the architecture the build was asked for, which is the architecture the server then runs on.
FROM docker.io/alpine:3.24.2

# The version the binary reports, and the moment the image was made. `go run ./cmd/build container`
# passes both: the version it resolved from VERSION.txt and git, and the clock reading when it asked
# for the build. An image built without the version argument reports the same development fallback a
# plain `go build` reports, which is the sign that nobody said what this image is.
ARG VERSION=dev
ARG BUILD_TIME=unknown

# The image's own account of what it is, read out of the image by the machine that pulls it. The
# version and the build time are facts the container run knows, and arrive through the arguments above:
# a label repeating a literal is a label that goes stale the moment the release moves.
LABEL org.opencontainers.image.title="rdio-scanner"
LABEL org.opencontainers.image.source="https://github.com/chuot/rdio-scanner.git"
LABEL org.opencontainers.image.documentation="https://github.com/chuot/rdio-scanner/blob/main/docs/admin-guide.html"
LABEL org.opencontainers.image.licenses="GPL-3.0-only"
LABEL org.opencontainers.image.version="$VERSION"
LABEL org.opencontainers.image.created="$BUILD_TIME"
LABEL org.opencontainers.image.ref.name="$VERSION"
LABEL maintainer="Chrystian Huot <chrystian.huot@saubeo.solutions>"

WORKDIR /app
ENV DOCKER=1

COPY --from=build /app/rdio-scanner ./rdio-scanner

# The packages the server converts with and the user it runs as, and nothing that built the binary: the
# source tree, the Go toolchain, and the compile's own scratch stay in the stage that compiled it.
RUN apk --no-cache --no-progress add mailcap tzdata && \
    adduser -D -u 10001 rdio && \
    mkdir -p /app/data && \
    chown rdio:rdio /app/data

# The server runs as this uid, not as root: it reads and deletes the
# directories its administrator configures and writes its data directory, and
# root buys it nothing it uses. The default listen address is 3000, which any
# user can bind; an operator who points ssl_listen at 443 binds a privileged
# port and needs a privileged start, which is the operator's call to make.
USER 10001

VOLUME [ "/app/data" ]
EXPOSE 3000
ENTRYPOINT [ "./rdio-scanner", "-base_dir", "/app/data" ]
