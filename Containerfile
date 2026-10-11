FROM docker.io/alpine:latest
LABEL maintainer="Chrystian Huot <chrystian.huot@saubeo.solutions>"
WORKDIR /app
ENV DOCKER=1

# The version the binary reports. `go run ./cmd/build container` passes it, resolved from VERSION.txt
# and git. An image built without the argument reports the same development fallback a plain `go build`
# reports, which is the sign that nobody said what this image is.
ARG VERSION=dev

COPY go.mod go.sum ./
COPY cmd/ cmd/
COPY internal/ internal/

# The ffmpeg this image converts with is the one compiled for linux-amd64 and embedded in the binary, so
# the image installs no ffmpeg package. The bundle has to be on disk before the image is built, which is
# what `go run ./cmd/build container` does: it builds the ffmpeg for linux-amd64 first. Saying so here is
# what keeps an image built straight from a fresh clone from shipping a server that cannot convert
# anything.
RUN mkdir -p /app/data && \
    test -f internal/infrastructure/ffmpeg/bundle/linux-amd64/ffmpeg.gz || { \
        echo "the ffmpeg this image carries was not compiled: run go run ./cmd/build container first" >&2; exit 1; \
    } && \
    apk --no-cache --no-progress --virtual .build add go && \
    go build -ldflags "-X main.Version=$VERSION" -o ./rdio-scanner ./cmd/rdio-scanner && \
    rm -fr cmd internal go.mod go.sum /root/.cache /root/go && \
    apk del .build && \
    apk --no-cache --no-progress add mailcap tzdata && \
    adduser -D -u 10001 rdio && \
    chown rdio:rdio /app/data

# The server runs as this uid, not as root (CA-019.1): it reads and deletes the
# directories its administrator configures and writes its data directory, and
# root buys it nothing it uses. The default listen address is 3000, which any
# user can bind; an operator who points ssl_listen at 443 binds a privileged
# port and needs a privileged start, which is the operator's call to make.
USER 10001

VOLUME [ "/app/data" ]
EXPOSE 3000
ENTRYPOINT [ "./rdio-scanner", "-base_dir", "/app/data" ]
