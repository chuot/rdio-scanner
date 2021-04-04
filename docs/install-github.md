# Rdio Scanner from Github

Sometimes you don't want to run a Docker image, so you can follow these instructions to clone the [Rdio Scanner GitHub repository](https://github.com/chuot/rdio-scanner).

# 1. Prerequisites

Make sure your operating system is **fully updated** and that the prerequisites are installed. Also make sure that you have enough memory to compile the Angular web application.

- [build-essential](https://packages.ubuntu.com/search?keywords=build-essential) on _Ubuntu_ or its equivalent specific to your Linux distribution
- [curl](https://git-scm.com/downloads)
- [ffmpeg](https://www.ffmpeg.org/)
- [git](https://git-scm.com/downloads)
- [Node.js LTS or higher](https://nodejs.org/en/download/) (get it [here](https://github.com/nodesource/distributions) if your distro doesn't have the required package)
- [npm](https://www.npmjs.com/get-npm)
- [python](https://www.python.org/) (python is required to compile sqlite node package)
- [SQLite 3](https://www.sqlite.org/download.html)

```bash
$ which curl ffmpeg gcc g++ git make node npm python sqlite3
/usr/bin/curl
/usr/bin/ffmpeg
/usr/bin/gcc
/usr/bin/g++
/usr/bin/git
/usr/bin/make
/usr/bin/node
/usr/bin/npm
/usr/bin/python
/usr/bin/sqlite3
```

> Ubuntu distro provides an old Node.js package. Make sure to use at least the current LTS version. Use the link above to get the latest LTS version.

# 2. Clone the Rdio Scanner GitHub Repository

```bash
$ git clone https://github.com/chuot/rdio-scanner.git
Cloning into 'rdio-scanner'...
remote: Enumerating objects: 1384, done.
remote: Counting objects: 100% (1384/1384), done.
remote: Compressing objects: 100% (1284/1284), done.
remote: Total 1384 (delta 752), reused 112 (delta 24)
Receiving objects: 100% (1384/1384), 1010.15 KiB | 4.83 MiB/s, done.
Resolving deltas: 100% (752/752), done.
```

# 3. Database and configuration initialization

Note that the first time [Rdio Scanner](https://github.com/chuot/rdio-scanner) will start, it will take longer as it has to install required node modules and build the progressive web application.

```bash
$ cd rdio-scanner

$ node run init
Installing node modules... done
Building client app... done
Configuration and database initialized

$ ls -l server/config.json server/database.sqlite
-rw-r--r--. 1 radio radio  8484 10 jui 10:39 server/config.json
-rw-r--r--. 1 radio radio 40960 10 jui 10:39 server/database.sqlite
```

If there is a problem, rerun the command with `DEBUG=true` to get more details on the reasons for the failure:

```bash
$ DEBUG=true node run init
...
```

# 3. Configure Rdio Scanner

Read the documentation [docs/config.md](./config.md) for an explanation of all configurable parameters.

If your [Rdio Scanner](https://github.com/chuot/rdio-scanner) is already running, be sure to restart it every time you make changes to the `server/config.json` file.

# 4. Load a system from RadioReference.com (optional)

You may want to load your `server/config.json` with some systems from [RadioReference.com](https://radioreference.com/).

First download the CSV file for all talkgroups from a trunked system. Here we will use the file `trs_tg_7537.csv` which corresponds to [this system](https://www.radioreference.com/apps/db/?sid=7537).

You can choose any `system_id` you want to refer to this system. If you choose an existing `system_id` in `config.json`, you will replace it with the new one.

```bash
$ node server load-rrdb
USAGE: load-rrdb <system_id> <input_tg_csv>

$ node server load-rrdb 11 ~/Downloads/trs_tg_7537.csv
File /home/radio/Downloads/trs_tg_7537.csv imported successfully into system 11
```

# 5. Load talkgroups from a Trunk Recorder CSV file (optional)

```bash
$ node server load-tr
USAGE: load-tr <system_id> <input_tg_csv>

$ node server load-tr 12 ~/Downloads/tgs.csv
File /home/radio/Downloads/tgs.csv imported successfully into system 12
```

# 6. Start Rdio Scanner

```bash
$ node run server
Server is running at http://0.0.0.0:3000
```

# 7. Generate new UUIDs for your config file (optional)

Even if your instance of [Rdio Scanner](https://github.com/chuot/rdio-scanner) is preconfigured with a new random UUID for your API keys, you may want to generate others. The following command will generate a new UUID which you can copy/paste into the `server/config.json` file.

```bash
$ node server random-uuid
aaf366c5-fa20-40a7-b617-dc7587c792fd

$ node server random-uuid 5
003c391c-610c-4900-b17f-80f6c9af2bbc
cd32028a-ca81-469c-90d5-8fd1bfb69228
df2f76b0-be15-44e9-848a-c9a71b092707
dc50e0d6-c635-436b-bb70-b46d99f12df9
74b602c0-b4ad-49a0-bb40-00008c31f9b2
```

# 8. Update Rdio Scanner

If new commits have been pushed to the [Rdio Scanner GitHub repository](https://github.com/chuot/rdio-scanner), you can easily update like this:

```bash
$ node update
Pulling new version from github... done
Updating node modules... done
Building client app... done
Please restart Rdio Scanner
```

# 9. Finally

Remember that [Rdio Scanner](https://github.com/chuot/rdio-scanner) must be supplied with audio files from a recorder. Either you use a `dirWatch` to monitor a folder, or you use the API with an `upload script`. See the [examples folder](./examples) for some examples.

> Need help? Go to the [Gitter Rdio Scanner Lobby](https://gitter.im/rdio-scanner/Lobby).

Happy Rdio scanning !
