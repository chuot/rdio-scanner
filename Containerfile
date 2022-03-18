FROM docker.io/alpine:latest
LABEL maintainer="Chrystian Huot <chrystian.huot@saubeo.solutions>"
WORKDIR /app
ENV DOCKER=1
COPY server/. server/.
RUN mkdir -p /app/data && \
    apk --no-cache --no-progress --virtual .build add go && \
    cd server && \
    go build -o ../rdio-scanner && \
    cd .. && \
    rm -fr server /root/.cache /root/go && \
    apk del .build && \
    apk --no-cache --no-progress add ffmpeg mailcap tzdata
VOLUME [ "/app/data" ]
EXPOSE 3000
ENTRYPOINT [ "./rdio-scanner", "-base_dir", "/app/data" ]