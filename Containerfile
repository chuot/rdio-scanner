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
RUN mkdir -p /app/data && \
    apk --no-cache --no-progress --virtual .build add go && \
    go build -ldflags "-X main.Version=$VERSION" -o ./rdio-scanner ./cmd/rdio-scanner && \
    rm -fr cmd internal go.mod go.sum /root/.cache /root/go && \
    apk del .build && \
    apk --no-cache --no-progress add ffmpeg mailcap tzdata
VOLUME [ "/app/data" ]
EXPOSE 3000
ENTRYPOINT [ "./rdio-scanner", "-base_dir", "/app/data" ]
