# The base image is named by version. `latest` is a different image every week: the image built today
# is not the image built last week, and nothing inside the image says which one it is standing on.
# Moving this line moves the operating system the image ships, and is a change made on purpose.
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

COPY go.mod go.sum ./
COPY cmd/ cmd/
COPY internal/ internal/

# The ffmpeg this image converts with is the one compiled for linux-amd64 and embedded in the binary, so
# the image installs no ffmpeg package. The bundle has to be on disk before the image is built, which is
# what `go run ./cmd/build container` does: it builds the ffmpeg for linux-amd64 first. Saying so here is
# what keeps an image built straight from a fresh clone from shipping a server that cannot convert
# anything.
# The compile below carries by hand the flags the build tool passes when it compiles this same server: the
# linker strips the symbol table and the debug information, and the compiler is asked to record each source
# path as the module that holds it rather than as a folder on the machine building the image. An image build
# does not ask the build tool to compose its Go compile, so that posture holds only because this line says
# so.
RUN mkdir -p /app/data && \
    test -f internal/infrastructure/ffmpeg/bundle/linux-amd64/ffmpeg.gz || { \
        echo "the ffmpeg this image carries was not compiled: run go run ./cmd/build container first" >&2; exit 1; \
    } && \
    apk --no-cache --no-progress --virtual .build add go && \
    go build -trimpath -ldflags "-s -w -X main.Version=$VERSION" -o ./rdio-scanner ./cmd/rdio-scanner && \
    rm -fr cmd internal go.mod go.sum /root/.cache /root/go && \
    apk del .build && \
    apk --no-cache --no-progress add mailcap tzdata && \
    adduser -D -u 10001 rdio && \
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
