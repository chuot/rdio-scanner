# Rdio Scanner v2.5

*Rdio Scanner* is a progressive web interface designed to resemble an old school radio scanner. It integrates all frontend / backend components to manage audio files from different sources.

For now, only [Trunk Recorder](https://github.com/robotastic/trunk-recorder) software files can be used, but other audio sources can be added later on request.

Need help?

[![Chat](https://img.shields.io/gitter/room/rdio-scanner/Lobby.svg)](https://gitter.im/rdio-scanner/Lobby?utm_source=share-link&utm_medium=link&utm_campaign=share-link)

## What's new in this version

Version 2.5 add a new feature where you can toggle talkgroups according to their defined group (emergency, fire, law, transport, ...).

Version 2.0 is a major rewrite in which Meteor has been replaced by [The Apollo Data Graph Platform](https://www.apollographql.com/) for its API. MongoDB is also replaced by [SQLite](https://www.sqlite.org/) to facilitate the entire installation process. It is still possible to use another database by changing [Sequelize ORM](https://sequelize.org/) settings accordingly.

These changes bring many *performance benefits* to *Rdio scanner* and make it *much easier to install*.

For those of you who are already running a previous version of *Rdio Scanner*, there is no way to upgrade your current database to the new version of *Rdio Scanner*.

## Features

* Designed to look like a real police radio scanner
* Incoming calls are queued for lossless listening
* Temporarily hold a single system or a single talkgroup
* Select the talkgroups you want to listen to in live streaming mode
* Easily retrieve and replay past calls
* Easy to install and configure

## Demo

There is no demo site for Rdio Scanner yet, but we are working on it. We will update this section with an actual URL as it becomes available. For now, you can refer to the following section to get an idea of what is *Rdio Scanner*.

## Screenshots

### Main screen

Here's where incoming calls are displayed, as long as *LIVE FEED* is enabled.

![Main Screen](./docs/images/rdio_scanner_main.png?raw=true "Main Screen")

* LED area
  * is *ON* when playing a call
  * is *Blinking* when pause is *ON* and a call is currently playing

* Display area
  * Real time clock
  * (Q :) Number of calls in the listening queue
  * System name, alpha tag, tag, group, call duration and focus group description
  * (F :) Call frequency
  * (S :) Spike errors
  * (E :) Decoding errors
  * (TGID :) Talkgroup ID
  * (UID :) Unit ID
  * Call history of the last five calls
  * Double clicking or tapping this area will toggle fullscreen display

* Control area
  * LIVE FEED: When enabled, incoming calls are place in the queue for playback. The queue will be emptied if it is disabled
  * HOLD SYS: Temporarily hold the current call system, depending on the current talkgroups selection
  * HOLD TG: Same has HOLD SYS, but for the current talkgroup
  * REPLAY LAST: Replay the current call or the previous call if there is none playing
  * SKIP NEXT: Ignore the current call and immediately play the next call in queue
  * AVOID: Toggle the current talkgroup from the talkgroups selection. Calls from avoided talkgroups will be removed from the call queue
  * SEARCH CALL: Brings the search call screen
  * PAUSE: Pause playing of queued calls
  * SELECT TG: Bring the talkgroups selection screen

Note that if you change the talkgroups selection while holding a system or a talkgroup, it will replace the current talkgroups selection by those that are currently holded.

### Talkgroups selection screen

The talkgroups selection screen is where you select the talkgroups. You can either select them individually, by system, by group or globally.

Enabled talkgroup calls will be queued for listening.

![Talkgroups Selection](./docs/images/rdio_scanner_select.png?raw=true "Talkgroups Selection")

### Search call screen

This screen allows you to browse past calls. You can filter the list by date, system and talkgroups.

![Call Search](./docs/images/rdio_scanner_search.png?raw=true "Call Search")

Search filters at the bottom of this screen are self explanatory.

## Quick start

It is fairly easy to have *Rdio Scanner* up and running.

Ensure that your operating system is fully updated and that the prerequisites are installed:

* [Git v2.23.0 or higher](https://git-scm.com/downloads)
* [Node.js v10.9.0 or higher](https://nodejs.org/en/download/)
* [npm v6.2.0 or higher](https://www.npmjs.com/get-npm)

Then clone the *Rdio Scanner* code and run it:

```bash
$ git clone https://github.com/chuot/rdio-scanner.git
Cloning into 'rdio-scanner'...
remote: Enumerating objects: 1384, done.
remote: Counting objects: 100% (1384/1384), done.
remote: Compressing objects: 100% (1284/1284), done.
remote: Total 1384 (delta 752), reused 112 (delta 24)
Receiving objects: 100% (1384/1384), 1010.15 KiB | 4.83 MiB/s, done.
Resolving deltas: 100% (752/752), done.

$ cd rdio-scanner

$ node run.js
Default configuration created at /home/radio/rdio-scanner/server/.env
Make sure your upload scripts use this API key: 1b0800a0-2b5d-422c-a4e3-972d5c1d32ff
Building client app... done
Creating SQLITE database at /home/radio/rdio-scanner/server/database.sqlite... done
Rdio Scanner is running at http://0.0.0.0:3000/
```

Note that the first time you start *Rdio Scanner*, it will be longer to do so as it has to install required node modules and build the progressive web app.

A default configuration file `rdio-scanner/server/.env` will be created. A new random API key will be generated where it will have to be use in your upload scripts.

At this point, you should be able to access to *Rdio Scanner* with a browser; just enter the above URL.

However, you won't see anything as you need first to upload your configuration (systems/talkgroup) to *Rdio Scanner*. For that, you can refer the to the upload scripts from the *examples section* below.

## Configuration file

*Rdio Scanner* configuration file is located at `rdio-scanner/server/.env`.

You can setup the following variables to suit your needs.

### Node environment parameters

```bash
#
# Default values are:
#
#   NODE_ENV=production
#   NODE_HOST=0.0.0.0
#   NODE_PORT=3000
#

# Node environment
NODE_ENV=development

# Node host
NODE_HOST=127.0.0.1

# Node port
NODE_PORT=3000
```

### Database related parameters

```bash
#
# Default values are:
#
#   DB_DIALECT=sqlite
#   DB_STORAGE=database.sqlite
#

# Database host
DB_HOST=127.0.0.1

# Database port
DB_PORT=3306

# Sequelize ORM dialect
DB_DIALECT=mariadb

# Database name
DB_NAME=rdio_scanner

# Database user
DB_USER=rdio_scanner

# Database password
DB_PASS=password

# Database storage location
DB_STORAGE=
```

### Rdio Scanner parameters

```bash
#
# Default values are:
#
#   RDIO_APIKEYS=["b29eb8b9-9bcd-4e6e-bb4f-d244ada12736"] (randomly generated)
#   RDIO_PRUNEDAYS=7
#

# Rdio Scanner API KEYS
#
# Note: The value has to be in JSON parsable format.
#
# You can either provide an array of API keys. This will allow the uploader to upload to any system/talkgroup
RDIO_APIKEYS=["b29eb8b9-9bcd-4e6e-bb4f-d244ada12736"]
# Or an array of object that tells Rdio Scanner the systems the uploader can upload.
RDIO_APIKEYS=[{"key":"b29eb8b9-9bcd-4e6e-bb4f-d244ada12736","systems":[11,15,21]}]
# Or an array of object that tells Rdio Scanner the systems and the talkgroup the uploader can upload.
RDIO_APIKEYS=[{"key":"b29eb8b9-9bcd-4e6e-bb4f-d244ada12736","systems":[{"system":11,"talkgroups":[54125,54129,54241]}]}]
# You can also provide an array of the 3 different formats mixed all together.

# Rdio Scanner database pruning
#
# Calls older than this number of days will be expunge from the database.
RDIO_PRUNEDAYS=30
```

## Updating Rdio Scanner thereafter

With simplicity, you can update *Rdio Scanner* with one command:

```bash
$ node update.js
Pulling new version from github... done
Updating node modules... done
Migrating database... done
Building client app... done
Please restart Rdio Scanner
```

## Examples

Those examples files are provided as-is to help you with *Rdio Scanner* system integration.

### docs/examples/rdio-scanner/dotenv

If the automaticaly generated `server/.env` file is not enough for you, this file contains all the parameters you can tweak to your needs.

### docs/examples/rdio-scanner/rdio-scanner.service

If you want *Rdio Scanner* to starts automaticaly upon reboots, this systemd unit file will help you with that. Just copy it as root to `/etc/systemd/system` and modify it to match your own configuration. Make sure that the paths, user and group are the correct one.

Then activate it and start it.

```bash
$ sudo systemctl daemon-reload
$ sudo systemctl enable rdio-scanner
$ sudo systemctl start rdio-scanner
```

### docs/examples/trunk-recorder

The folowing files assumes that you're using this folder layout. It isn't mandatory, but it helps keepings things well organized.

```
~/trunk-recorder
~/trunk-recorder/audio_files
~/trunk-recorder/configs/*.json
~/trunk-recorder/scripts/upload-*.sh
~/trunk-recorder/talkgroups/*.csv
```

### docs/examples/trunk-recorder/configs

Those files are provided only for example purposes.

However, it is important to note that each system is configured to **not record** unknown talkgroups (not listed in related CSV file).

Also, notice how we pass the *arbitrary* system number to the `uploadScript`.

```bash
{
    "systems": [{
        ...
            "recordUnknown": false
            ...
            "uploadScript": "scripts/upload-call.sh 11"
        ...
    }]
}
```

### docs/examples/trunk-recorder/scripts/upload-call.sh

This is the upload script for *Trunk Recorder*, It needs to be called with a **system number as the first argument**, then the **full path to the audio file as the second argument**.

```bash
$ upload-call.sh 11 .../trunk-recorder/audio_files/...
```

This script use [fdkaac](https://github.com/nu774/fdkaac) to convert audio files. It should be avaiable from your linux distro (debian and redhat based). Same goes for `curl` which is use to upload *Trunk Recorder* data to *Rdio Scanner*.

**Please change the API key inside it for the one that has been created above within the *quick start section*. Same for the URL where to upload to Rdio Scanner instance**

```bash
curl -s http://127.0.0.1:3000/api/trunk-recorder-call-upload \
     ...
     -F "key=b29eb8b9-9bcd-4e6e-bb4f-d244ada12736" \
     ...
```

### docs/examples/trunk-recorder/scripts/upload-systems.sh

Much like the previous script, this one is in charge of feeding *Rdio Scanner* with *Trunk Recorder* systems and talkgroups.

You need to upload systems and talkgroups definitions to *Rdio Scanner*, the first time you have installed it, and each time you change any *Trunk Recorder*'s configuration.

Note that if you want to remove a system from the *Rdio Scanner* configuration, simply upload an empty CSV file.

**Same here, please change the API key inside it for the one that has been created above within the *quick start section*. Same for the URL where to upload to Rdio Scanner instance**

Once uploaded, you should have your systems and talkgroups listed on *Rdio Scanner* talkgroups selection screen.

### docs/examples/trunk-recorder/systemd

Provided as complimentary files, `trunk-recorder@.service` and `trunk-recorder@.timer` can be use to manage multiple *Trunk Recorder* instances. For example, if you have more that 1 system that requires different *gains* / *tuners* each.

you can either just enable the `trunk-recorder@[config_name].service` or the `trunk-recorder@[config_name].timer`. The later one starts *Trunk Recorder* a minute after the system has booted up. This allows the *rtl-sdr* dongle to warm up a little bit before going crazy (less control channel decoding rate errors in my case).

### docs/examples/trunk-recorder/talkgroups

Since *Trunk Recorder* should only records **knowed** talkgroups, they should be well configured in each CSV files.

**Not having talkgroups well defined will make *Rdio Scanner* display calls with much less informations on screen.**