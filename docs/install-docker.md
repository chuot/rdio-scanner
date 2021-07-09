# Rdio Scanner Docker Image

The [Rdio Scanner Docker Image](https://hub.docker.com/r/chuot/rdio-scanner) is the easiest installation method. The only prerequisite is that your host must have Docker installed before continuing.

Before going any further, make sure you have enough knowledge about using Docker. For those new to Docker, I recommend reading at least the [Docker Overview](https://docs.docker.com/get-started/overview/).

# 1. Pull the docker image

Do `docker pull chuot/rdio-scanner:latest` like this:

```bash
$ docker pull chuot/rdio-scanner:latest
latest: Pulling from chuot/rdio-scanner
cbdbe7a5bc2a: Already exists
bd07af9ed1a4: Pull complete
3556ccf180b2: Pull complete
089d4748da74: Pull complete
d461af9c0a5f: Pull complete
b13fa0022c33: Pull complete
af86cf4eebc5: Pull complete
dbbf263c046e: Pull complete
adf8257900e9: Pull complete
9716c9a06d21: Pull complete
Digest: sha256:1264d0a9dc682e2057d7d6b89fee98164212fc0763ad3f40eb3193da75c64c75
Status: Downloaded newer image for chuot/rdio-scanner:latest
docker.io/chuot/rdio-scanner:latest
```

# 2. Start Rdio Scanner

By default, [Rdio Scanner](https://github.com/chuot/rdio-scanner) runs on port 3000. It is recommended to configure a **SSL certificate** and then to map port 3000 to port 443 using the `--publish 3000:443` option.

However, we will stick to port 3000 in this example.

> Note that it is important to make sure the data folder already exists on the Docker host the very first time you run the Docker container (`~/.rdio-scanner` in this example) or it will be created by the Docker daemon with the wrong owner. In other words, the volume you mount on `/app/data` must be owned by the same user as specified by the `--user` option.

```bash
$ docker run --detach --env TZ=America/Toronto --name rdio-scanner --publish 3000:3000 --restart always --user $(id -u):$(id -g) --volume ~/.rdio-scanner:/app/data chuot/rdio-scanner:latest
520cdbf51fca11d8bacea12d81245f1cb4d984f80d2be2e3039727b59533a6a9

$ docker logs rdio-scanner
Server is running at http://0.0.0.0:3000$ docker stop rdio-scanner
rdio-scanner
```

You can also use docker-compose to start the server, see the file [docs/examples/rdio-scanner/docker-compose.yml](./examples/rdio-scanner/docker-compose.yml) for an example.

```bash
$ docker-compose -p rdio-scanner run -d --service-ports server
Creating network "rdio-scanner_default" with the default driver
Creating rdio-scanner_server_run ... done
rdio-scanner_server_run_3e00e8f8446f
```


# 3. Configure Rdio Scanner

Read the this document [docs/config.md](./config.md).


# 4. Update Rdio Scanner

If a newer version of [Rdio Scanner Docker Image](https://hub.docker.com/r/chuot/rdio-scanner) is released, you can easily update it like this:

```bash
$ docker pull chuot/rdio-scanner:latest
...

$ docker stop rdio-scanner
rdio-scanner

$ docker rm rdio-scanner
rdio-scanner

$ docker run ... // see section 6 above
```

You can also put these commands in a shell script to simplify future updates.

# 5. Resetting the administrator password

Besides resetting the administrator password from the administrative dashboard, you can also reset the administrator password from the command line.

```bash
$ docker run -it --volume ~/.rdio-scanner:/app/data --rm --user $(id -u):$(id -g) chuot/rdio-scanner:latest reset-admin-password newpassword
Admin password has been reset.
```

If you do not specify a password after the `reset-admin-password` command, the default password will be configured, which is `rdio-scanner`.

# 6. Optional command line tools for hardcore users

Load a radioreference.com talkgroups file into a system:

```bash
$ docker run -it --rm chuot/rdio-scanner:latest load-rrdb
USAGE: load-rrdb <system_id> <input_tg_csv>
```

Load a trunk-recorder talkgroups file into a system:

```bash
$ docker run -it --rm chuot/rdio-scanner:latest load-tr
USAGE: load-tr <system_id> <input_tg_csv>
```

Generate random UUIDs

```bash
$ docker run -it --rm chuot/rdio-scanner:latest random-uuid
1e5ff9c6-fdaf-495d-b67e-95a73e6c0e06

$ docker run -it --rm chuot/rdio-scanner:latest random-uuid 5
c5ab8726-f59a-4609-8d35-7dd534b9d038
a54d5bdb-0e59-4616-9498-cfc42ac572f1
061b78a1-b2a9-47a8-91e5-8b285a46a268
5da5c824-fe4a-4cf8-8146-8a6806af4eed
92b1eafa-02ff-4993-9133-6098d6d37888
```
# 7. Finally

Remember that [Rdio Scanner](https://github.com/chuot/rdio-scanner) must be supplied with audio files from a recorder. Either you mount another volume with the `--volume /recordings:/home/radio/recordings` options and use a `dirWatch` to monitor it, or you use the API with an `upload script`. See the [examples folder](./examples) for some examples.

> Need help? Go to the [Gitter Rdio Scanner Lobby](https://gitter.im/rdio-scanner/Lobby).

Happy Rdio scanning !
