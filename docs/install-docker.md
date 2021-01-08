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

# 2. Database and configuration initialization

We now need to initialize the config.json file and the database. But first we need to determine where these files will be located on your host. We will use `~/.rdio-scanner` in the following example, but you can choose any location you want. We will then pass this location via the `--volume` option.

> NOTE: If you are running the container in a SELINUX environement, you may need to append a `:z` to your volume definition: `--volume ~/.rdio-scanner:/app/data:z` for things to work. More inforation [here](https://docs.docker.com/storage/bind-mounts/#configure-the-selinux-label).

```bash
$ mkdir ~/.rdio-scanner

$ docker run -it --rm --user $(id -u):$(id -g) --volume ~/.rdio-scanner:/app/data chuot/rdio-scanner:latest init
Configuration and database initialized

$ ls -l ~/.rdio-scanner
-rw-r--r--. 1 radio radio  8460 10 jui 08:32 config.json
-rw-r--r--. 1 radio radio 40960 10 jui 08:32 database.sqlite
```

# 3. Configure Rdio Scanner

Read the documentation [docs/config.md](./config.md) for an explanation of all configurable parameters.

If your [Rdio Scanner](https://github.com/chuot/rdio-scanner) is already running, be sure to restart it every time you make changes to the `config.json` file.

```bash
$ docker restart rdio-scanner
rdio-scanner
```

# 4. Load a system from RadioReference.com (optional)

You may want to load your `config.json` with some systems from [RadioReference.com](https://radioreference.com/).

First download the CSV file for all talkgroups from a trunked system. Then move this file to `~/.rdio-scanner`. Here we will use the file `trs_tg_7537.csv` which corresponds to [this system](https://www.radioreference.com/apps/db/?sid=7537).

You can choose any `system_id` you want to refer to this system. If you choose an existing `system_id` in `config.json`, you will replace it with the new one.

```bash
$ mv ~/Downloads/trs_tg_7537.csv ~/.rdio-scanner

$ docker run -it --rm chuot/rdio-scanner:latest load-rrdb
USAGE: load-rrdb <system_id> <input_tg_csv>

$ docker run -it --rm --user $(id -u):$(id -g) --volume ~/.rdio-scanner:/app/data chuot/rdio-scanner:latest load-rrdb 11 trs_tg_7537.csv
File /app/data/trs_tg_7537.csv imported successfully into system 11

$ rm ~/.rdio-scanner/trs_tg_7537.csv
```

# 5. Load talkgroups from a Trunk Recorder CSV file (optional)

```bash
$ mv ~/Downloads/tgs.csv ~/.rdio-scanner

$ docker run -it --rm chuot/rdio-scanner:latest load-tr
USAGE: load-tr <system_id> <input_tg_csv>

$ docker run -it --rm --user $(id -u):$(id -g) --volume ~/.rdio-scanner:/app/data chuot/rdio-scanner:latest load-tr 12 tgs.csv
File /app/data/tgs.csv imported successfully into system 12

$ rm ~/.rdio-scanner/tgs.csv
```

# 6. Start Rdio Scanner

By default, [Rdio Scanner](https://github.com/chuot/rdio-scanner) runs on port 3000. It is recommended to configure a **SSL certificate** and then to map port 3000 to port 443 using the `--publish 3000:443` option.

However, we will stick to port 3000 in this example.

```bash
$ docker run --detach --env TZ=America/Toronto --name rdio-scanner --publish 3000:3000 --restart always --user $(id -u):$(id -g) --volume ~/.rdio-scanner:/app/data chuot/rdio-scanner:latest
520cdbf51fca11d8bacea12d81245f1cb4d984f80d2be2e3039727b59533a6a9

$ docker logs rdio-scanner
Server is running at http://0.0.0.0:3000$ docker stop rdio-scanner
rdio-scanner
```

# 7. Generate new UUIDs for your config file (optional)

Even if your instance of [Rdio Scanner](https://github.com/chuot/rdio-scanner) is preconfigured with a new random UUID for your API keys, you may want to generate others. The following command will generate a new UUID which you can copy/paste into the `config.json` file.

```bash
$ docker run -it --rm chuot/rdio-scanner:latest random-uuid
aaf366c5-fa20-40a7-b617-dc7587c792fd

$ docker run -it --rm chuot/rdio-scanner:latest random-uuid 5
003c391c-610c-4900-b17f-80f6c9af2bbc
cd32028a-ca81-469c-90d5-8fd1bfb69228
df2f76b0-be15-44e9-848a-c9a71b092707
dc50e0d6-c635-436b-bb70-b46d99f12df9
74b602c0-b4ad-49a0-bb40-00008c31f9b2
```

# 8. Update Rdio Scanner

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

# 9. Finally

Remember that [Rdio Scanner](https://github.com/chuot/rdio-scanner) must be supplied with audio files from a recorder. Either you mount another volume with the `--volume /recordings:/home/radio/recordings` options and use a `dirWatch` to monitor it, or you use the API with an `upload script`. See the [examples folder](./examples) for some examples.

> Need help? Go to the [Gitter Rdio Scanner Lobby](https://gitter.im/rdio-scanner/Lobby).

Happy Rdio scanning !
