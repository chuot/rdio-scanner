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
- [openssl](https://openssl.org/)
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
/use/bin/openssl
/usr/bin/python
/usr/bin/sqlite3
```

> Ubuntu distro provides an old Node.js package. Make sure to use at least the [current LTS version](https://nodejs.org). Use the link above to get the latest LTS version.

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

# 3. Start Rdio Scanner

```bash
$ node run server
Server is running at http://0.0.0.0:3000
```

You can also configure systemd to start the server, see the file [docs/examples/rdio-scanner/rdio-scanner.service](./docs/examples/rdio-scanner/rdio-scanner.service) for an example.

# 4. Configure Rdio Scanner

Read the this document [docs/config.md](./config.md).

# 5. Update Rdio Scanner

If new commits have been pushed to the [Rdio Scanner GitHub repository](https://github.com/chuot/rdio-scanner), you can easily update like this:

```bash
$ node update
Pulling new version from github... done
Updating node modules... done
Building client app... done
Please restart Rdio Scanner
```

# 6. Resetting the administrator password

Besides resetting the administrator password from the administrative dashboard, you can also reset the administrator password from the command line.

```bash
$ node server reset-admin-password newpassword
Admin password has been reset.
```

If you do not specify a password after the `reset-admin-password` command, the default password will be configured, which is `rdio-scanner`.

# 7. Optional command line tools for hardcore users

Load a radioreference.com talkgroups file into a system:

```bash
$ node server load-rrdb
USAGE: load-rrdb <system_id> <input_tg_csv>
```

Load a trunk-recorder talkgroups file into a system:

```bash
$ node server load-tr
USAGE: load-tr <system_id> <input_tg_csv>
```

Generate random UUIDs

```bash
$ node server random-uuid
1e5ff9c6-fdaf-495d-b67e-95a73e6c0e06

$ node server random-uuid 5
c5ab8726-f59a-4609-8d35-7dd534b9d038
a54d5bdb-0e59-4616-9498-cfc42ac572f1
061b78a1-b2a9-47a8-91e5-8b285a46a268
5da5c824-fe4a-4cf8-8146-8a6806af4eed
92b1eafa-02ff-4993-9133-6098d6d37888
```

# 8. Finally

Remember that [Rdio Scanner](https://github.com/chuot/rdio-scanner) must be supplied with audio files from a recorder. Either you use a `dirWatch` to monitor a folder, or you use the API with an `upload script`. See the [examples folder](./examples) for some examples.

> Need help? Go to the [Gitter Rdio Scanner Lobby](https://gitter.im/rdio-scanner/Lobby).

Happy Rdio scanning !
