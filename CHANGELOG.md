# Version 4.8

- Add downstream.system.id_as property to allow export system with a different id.
- Add system.order for system list ordering on the client side.
- Fix client main screen unscrollable overflow while in landscape.
- Fix issue 26 - date in documentation for mask isn't clear.
- The skip button now also allows you to skip the one second delay between calls.
- Node modules update.

_v4.8.1_

- Refactor panels' back button and make them fixed at the viewport top.
- Node modules update.

_v4.8.2_

- Fix dirWatch.type='sdr-trunk' metatag artist as source is now optional.
- Fix dirWatch.type='sdr-trunk' metatag title as talkgroup.id.
- Web app now running with Angular 11.
- Node modules update.

_v4.8.3_

- Add the ability to overwrite the default dirWatch extension for type sdr-trunk and trunk-recorder.
- Fix dirWatch.disabled being ignored.
- Node modules update.

# Version 4.7

- New dirWatch.type='sdr-trunk'.
- New search panel layout with new group and tag filters.
- Add load-tr to load Trunk Recorder talkgroups csv.
- Remove Config.options.allowDownloads, but the feature remains.
- Remove Config.options.useGroup, but the feature remains.
- Bug fixes.

_v4.7.1_

- Fix crash on client when access to talkgroups is restricted with a password.

_v4.7.2_

- Fix Keypad beeps not working on iOS.
- Fix pause not going off due to the above bug.

_v4.7.3_

- Fix websocket not connection on ssl.

_v4.7.4_

- Fix display width too wide when long talkgroup name.

_v4.7.5_

- Fix playback mode getting mixed up if clicking too fast on play.
- Fix side panels background color inheritance.
- Node modules update.

_v4.7.6_

- Fix search results not going back to page 1 when search filters are modified.
- Skip next button no longer emit a denied beeps sequence when pushed while there's no audio playing.
- Node modules update.

# Version 4.6

- Fix documentation in regards to load-rrd in install-github.md.
- Fix database absolute path in config.json.
- Remove config.options.useLed.
- Rename Config.options.keyBeep to Config.options.keypadBeeps.
- Config.options.keypadBeeps now with presets instead of full pattern declaration.
- Bug fixes.

# Version 4.5

- Config.options.keyBeep which by default is true.
- Bug fixes.

# Version 4.4

- Config.systems.talkgroups.patches to group many talkgroups (patches) into one talkgroup.id.
- Config.options now groups allowDownloads, disableAudioConversion, pruneDays, useDimmer, useGroup and useLed options instead of having them spread all over the config file.
- Client will always display talkgroup id on the right side instead of 0 when call is analog.
- Fix annoying bug when next call queued to play is still played even though offline continuous play mode is turned off.
- Talkgroup ID is displayed no matter what and unit ID is displayed only if known.

# Version 4.3

- Add metatags to converted audio files.
- Automatic database migration on startup.
- Client now on Angular 10 in strict mode.
- Dockerized.
- Fix downstream not being triggered when a new call imported.
- Fix dirWatch mask parser and new mask metatags.
- Fix stop button on the search panel when in offline play mode.
- Fix SSL certificate handling.
- Rewritten documentation.

# Version 4.2

- Fix possible race conditions....
- Added websocket keepalive which helps mobile clients when switching from/to wifi/wan.
- Better playback offline mode animations and queue count.
- New dirWatch.mask option to simplify meta data import.

# Version 4.1

- New offline playback mode.

# Version 4.0

- GraphQL replaced by a pure websocket command and control system.
- `server/.env` replaced by a `server/config.json`.
- Systems are now configured through `server/config.json`, which also invalidate the script `upload-system`.
- Indexes which result in much faster access to archived audio files.
- Add SSL mode.
- Restrict systems/talkgroups access with passwords.
- Directory watch and automatic audio files ingestion.
- Automatic m4a/aac file conversion for better compatibility/performance.
- Selectively share systems/talkgroups to other instances via downstreams.
- Customizable LED colors by systems/talkgroups.
- Dimmable display based on active call.

## Upgrading from version 3

- Your `server/.env` file will be used to create the new `server/config.json` file. Then the `server/.env` will be deleted.
- The `rdioScannerSystems` table will be used to create the _rdioScanner.systems_ within `server/config.json`. Then the `rdioScannerSystems` table will be purged.
- The `rdioScannerCalls` table will be rebuilt, which can be pretty long on some systems.
- It is no longer possible to upload neither your TALKGROUP.CSV nor you ALIAS.CSV files to _Rdio Scanner_. Instead, you have to define them in the `server/config.json` file.

> YOU SHOULD BACKUP YOUR `SERVER/.ENV` FILE AND YOUR DATABASE PRIOR TO UPGRADING, JUST IN CASE. WE'VE TESTED THE UPGRADE PROCESS MANY TIMES, BUT WE CAN'T KNOW FOR SURE IF IT'S GOING TO WORK WELL ON YOUR SIDE.

# Version 3.1

- Client now on Angular 9.
- Display listeners count on the server's end.

# Version 3.0

- Unit aliases support, display names instead of unit ID.
- Download calls from the search panel.
- New configuration options: _allowDownload_ and _useGroup_.

> Note that you can only update from version 2.0 and above. You have to do a fresh install if your actual version is prior to version 2.0.

# Version 2.5

- New group toggle on the select panel.

# Version 2.1

- Various speed improvements for searching stored calls.

# Version 2.0

- Ditched meteor in favour of GraphQL.

# Version 1.0

- First public version.
