FROM docker.io/alpine:latest
LABEL maintainer="Chrystian Huot <chrystian.huot@saubeo.solutions>"
WORKDIR /app
ENV DOCKER=1

# The version the binary reports. `make container` passes it from VERSION.txt. An image built without
# the argument reports the same development fallback a plain `go build` reports, which is the sign that
# nobody said what this image is.
ARG VERSION=dev

COPY go.mod go.sum ./
COPY cmd/ cmd/
COPY internal/ internal/

# The ffmpeg this image converts with is the one compiled for linux-amd64 and embedded in the binary, so
# the image installs no ffmpeg package. The bundle has to be on disk before the image is built, which is
# what `make container` does: it builds linux-amd64 first. Saying so here is what keeps an image built
# straight from a fresh clone from shipping a server that cannot convert anything.
RUN mkdir -p /app/data && \
    test -f internal/infrastructure/ffmpeg/bundle/linux-amd64/ffmpeg.gz || { \
        echo "the ffmpeg this image carries was not compiled: run make linux-amd64 first" >&2; exit 1; \
    } && \
    apk --no-cache --no-progress --virtual .build add go && \
    go build -ldflags "-X main.Version=$VERSION" -o ./rdio-scanner ./cmd/rdio-scanner && \
    rm -fr cmd internal go.mod go.sum /root/.cache /root/go && \
    apk del .build && \
    apk --no-cache --no-progress add mailcap tzdata
VOLUME [ "/app/data" ]
EXPOSE 3000
ENTRYPOINT [ "./rdio-scanner", "-base_dir", "/app/data" ]
