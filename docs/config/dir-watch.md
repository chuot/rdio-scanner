# Directory watcher

## Definition

**Rdio Scanner** can monitor folders for ny audio files and import them automatically.

The *rdioScanner.dirWatch* property an *array* of *dirWatch objects* which describes folders to watch.

```json
{
    "rdioScanner": {
        "dirWatch": [
            {
                // [optional] Whether the audio is deleted after importation (defaults to false)
                "deleteAfter": true,

                // [mandatory] Full path to directory to monitor
                "directory": "/home/radio/audio_files",

                // [mandatory] Audio file extension to watch
                "extension": "wav",

                // [optional] Fake the frequency on the Rdio Scanner's display (in hertz)
                "frequency": 119100000,

                // [mandatory] The system ID to import to. It can also be a regex string 
                "system": 1,

                // [mandatory] for type=default and type=trunk-recorder
                // [optional] for type=sdrtrunk
                // The talkgroup ID to import to. It can also be a regex string 
                "talkgroups": 1001,

                // [optional] The type of the audio file
                // It can be "default", "sdrtrunk" or "trunk-recorder"
                "type": "default",

                // [optional] Set this to true to successfully watch files over a network
                "usePolling": true
            }
        ]
    }
}
```

> It is recommended to test you regex at [regex101](https://regex101.com/)

## Examples

### Example for SDRTrunk

**Rdio Scanner** will import SDRTrunk *wav* and *mbe* files.

It is recommended to only record calls for **known** aliases in accordance to **[Rdio Scanner systems](./systems.md)**

```json
{
    "rdioScanner": {
        "dirWatch": [
            {
                "deleteAfter": true,
                "directory": "/home/radio/SDRTrunk/recordings",
                "extension": "wav",
                "system": 4,
                "type": "sdrtrunk"
            }
        ]
    }
}
```

### Example for Trunk Recorder

**Rdio Scanner** will import Trunk Recorder *wav* and *json* files.

It is recommended to only record calls for **known** aliases in accordance to *[Rdio Scanner systems](./systems.md)*

```json
{
    "rdioScanner": {
        "dirWatch": [
            {
                "deleteAfter": true,
                "directory": "/home/radio/trunk-recorder/audio_files",
                "extension": "wav",
                "system": ".*[^\\d](\\d_)-.*",
                "type": "trunk-recorder"
            }
        ]
    }
}
```

Another one with one *Trunk Recorder* per system.

```json
{
    "rdioScanner": {
        "dirWatch": [
            {
                "deleteAfter": true,
                "directory": "/home/radio/trunk-recorder/audio_files/RSP25MTL1",
                "extension": "wav",
                "system": 11,
                "type": "trunk-recorder"
            },
            {
                "deleteAfter": true,
                "directory": "/home/radio/trunk-recorder/audio_files/RSP25MTL5",
                "extension": "wav",
                "system": 15,
                "type": "trunk-recorder"
            }
        ]
    }
}
```
