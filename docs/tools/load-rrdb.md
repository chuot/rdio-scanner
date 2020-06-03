# SERVER / TOOLS / LOAD-RRDB

`load-rrdb` will import downloaded CSV file of all talkgroups for trunked system to `server/config.json` file.

## Usage

The first two parameters are mandatory as opposed to the third one which is optional. If not specified, the default output file is `server/config.json`.

If the output file exists, it's content will be used to create a new one and the old one will be renamed with a `.bak` as its suffix.

```bash
$ ./server/tools/load-rrdb -h
USAGE: load-rrdb <system_id> <input_tg_csv> [output_config_json]
```

> Remember to restart your `Rdio Scanner` instance when `server/config.json` is modified.

## Special note

`Rdio Scanner` was built from the ground up with the idea of a beautiful and functional interface that looks like old school police scanners.

Importing **too many** talkgroups will make the client part almost unusable on mobile devices or slow desktops. Just imagine having over 1000 butons on the `SELECT TG` panel.

The solution for these large systems would be to have an instance of `Rdio Scanner` just to ingest audio calls, then to define `downstreams` to other instances of `Rdio Scanner` where each one would have systems/talkgroups for a simple county/city/whatever... You could also have an instance with all the priority channels on it. The goal here is to have a reasonable amount of talkgroups per instance.