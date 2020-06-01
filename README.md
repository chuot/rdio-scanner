# Rdio Scanner v4.2

*Rdio Scanner* is a beautiful progressive web interface allowing you to listen to different audio streams from different sources. Like on a police scanner, you can choose which systems/talkgroups to listen to in live. You can also browse the archives of older audio files.

Need help?

[![Chat](https://img.shields.io/gitter/room/rdio-scanner/Lobby.svg)](https://gitter.im/rdio-scanner/Lobby?utm_source=share-link&utm_medium=link&utm_campaign=share-link)

Like it?

Please click the **star button** at the top of the page on *github* to express your appreciation to the developer.

## What's new in this version

### Version 4.2 - Small fixes and dirWatch.mask

* Fix possible race conditions
* Added websocket keepalive to client which helps mobile clients when switching from/to wifi/wan
* Better Continuous offline play mode animations and queue count
* New dirWatch.mask option to simplify meta data import

### Version 4.1 - Continuous offline play mode

Playing calls from the `SEARCH CALL` panel while `LIVE FEED` being `OFF` will play the call sequence according to the search filters. Note that the `LIVE FEED` indicator lights `yellow` when continuous offline play mode is active.

To stop continuous offline play mode, press the `STOP BUTTON` on the `SEARCH CALL` panel or press the `LIVE FEED` button.

Note that this mode does not take into account the system/talkgroup from the `SELECT TG` panel. `HOLD SYS`, `HOLD TG` and `AVOID` buttons will also be disabled.

### Version 4.0 - A a bunch of new features

* To improve performance, we abandoned *GraphQL* for a pure *WebSocket* command and control system, which also brought a lot of code refactoring.
* New `server/config.json` file that replaces the `server/.env` file. Systems/Talkgroups/Units are now configurable through that file instead of the old upload scripts.
* (much) faster access to archived calls
* On can now run *Rdio Scanner* directly in SSL mode, if certifcates are provided in the `server/config.json` file.
* Optional limited system/talkgroup access based on password
* Directory watch and automatic import
* Automatic m4a/aac file conversion for better compatibility/performance
* Selectively share systems/talkgroups to other *Rdio Scanner* instances via downstreams
* Customizable LED color by system/talkgroup
* Dimmable display based on active call

> Be sure to read [docs/upgrade-from-v3](./docs/upgrade-from-v3.md) prior to updating.

## Features

* Beautiful and functional interface inspired by police radio scanners
* Incoming calls from different sources are queued for lossless listening
* Select the talkgroups you want to listen to in live streaming mode
* Temporarily hold a single system or a single talkgroup
* Continuous offline play mode
* Easily retrieve and replay/download archived calls
* Decide who has access to what with access control, thus protecting sensitive systems/talkgroups.
* Directory watcher for automatic importing of audio files
* Automatic audio conversion to M4A/AAC on the server side. No need to prepare your audio files prior to upload.
* You can **share your precious audio calls** to other **Rdio Scanner** instances by configuring downstreams.
* Easy to install and configure

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
  * LIVE FEED: When enabled, incoming calls are place in the queue for playback. The queue will be emptied if it is disabled.
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

> Raspberry Pi users, visit [rdio-scanner-pi-setup](https://github.com/chuot/rdio-scanner-pi-setup) repository for a fully automatic installation script.

Ensure that your operating system is **fully updated** and that the prerequisites are met or installed:

* [build-essential](https://packages.ubuntu.com/search?keywords=build-essential) on *Ubuntu* or the equivalent on your distro
* [Curl](https://git-scm.com/downloads)
* [ffmpeg](https://www.ffmpeg.org/)
* [Git](https://git-scm.com/downloads)
* [Node.js v12.X or higher](https://nodejs.org/en/download/) (get it [here](https://github.com/nodesource/distributions) if your distro doesn't have the required package)
* [npm](https://www.npmjs.com/get-npm)
* [SQLite 3](https://www.sqlite.org/download.html)

> The Angular application will most likely fail to transpile if the host has less than 2 GB of RAM available.

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

Note that the first time you start *Rdio Scanner*, it will be longer to do so as it has to install required node modules and build the progressive web app. If it fails at this point, you can rerun the following command for more information on the reasons for the failure.

```bash
$ DEBUG=true node run.js
```

A default configuration file `rdio-scanner/server/config.json` will be created. A new random API key will be generated where it will have to be use in your upload scripts.

At this point, you should be able to access to *Rdio Scanner* with a browser; just enter the above URL.

## Configuration

Please refer to the file [docs/config.md](./docs/config.md);

## Post install updates

You can update your *Rdio Scanner* instance with this simple command:

```bash
$ node update.js
```