FROM node:12-alpine AS build-client

RUN mkdir /client && chown node:node /client

WORKDIR /client

COPY ./client/package*.json /client/

RUN npm ci

COPY ./client .

RUN npm run build

RUN npm prune --production

FROM node:12-alpine AS server-deps

RUN mkdir /server && chown node:node /server

WORKDIR /server

COPY ./server/package*.json /server/

RUN apk add --no-cache sqlite sqlite-dev sqlite-libs python g++ make gcc curl git \
    && npm ci --production

FROM node:12-alpine AS production

RUN apk add --no-cache ffmpeg

RUN mkdir /db && chmod -R a+rw /db

USER 1000

WORKDIR /app

COPY --from=build-client --chown=root:root /client/dist/rdio-scanner /app/client

COPY --chown=root:root ./server /app/server

COPY --from=server-deps --chown=root:root /server/node_modules /app/server/node_modules

COPY --chown=root:root ./entrypoint.sh .

EXPOSE 3000

ENV CLIENT_HTML_DIR=/app/client

CMD ["/app/entrypoint.sh"]
