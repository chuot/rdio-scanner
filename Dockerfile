FROM --platform=${BUILDPLATFORM:-amd64} node:lts-alpine as client-build
WORKDIR /app/client
COPY client/. . 
RUN NG_CLI_ANALYTICS=0 npm ci --loglevel=error --no-progress && \
    npm run build

FROM node:lts-alpine as server-build
WORKDIR /app/server
COPY server/. .
RUN apk add --no-cache --no-progress --quiet python make gcc g++ linux-headers && \
    npm config set jobs max && \
    npm ci --loglevel=error --no-progress && \
    npm run build && \
    npm prune --production

FROM node:lts-alpine
LABEL maintainer="Chrystian Huot <chrystian.huot@saubeo.solutions>"
WORKDIR /app
ENV APP_DATA=/app/data \
    APP_PORT=3000
RUN apk --no-cache --no-progress add ffmpeg sqlite tzdata && \
    mkdir -p "${APP_DATA}"
COPY LICENSE .
COPY --from=client-build /app/client/dist/rdio-scanner/. client/.
COPY --from=server-build /app/server/node_modules/. server/node_modules/.
COPY --from=server-build /app/server/dist/index.js server/.
VOLUME [ "/app/data" ]
EXPOSE ${APP_PORT}
ENTRYPOINT [ "node", "server" ]
