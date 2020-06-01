# Directory watcher

## Definition

**Rdio Scanner** can monitor folders for ny audio files and import them automatically.

The *rdioScanner.dirWatch* property an *array* of *dirWatch objects* which describes folders to watch.

```json
{
    "rdioScanner": {
        "dirWatch": [
            {
                // [optional]
                // Valid for type="default" only
                // Delay the import until this number of milliseconds without any modification of the file
                "delay": 3000,

                // [optional]
                // Whether the audio is deleted after importation (defaults to false)
                "deleteAfter": true,

                // [mandatory]
                // Full path to directory to monitor
                "directory": "/home/radio/audio_files",

                // [mandatory]
                // Audio file extension to watch
                "extension": "wav",

                // [optional]
                // Fake the frequency on the Rdio Scanner's display (in hertz)
                "frequency": 119100000,

                // [optional]
                // Possible tags are: #DATE, #TIME, #SYS, #TG, #UNIT, #HZ
                // If no system/talkgroup available from filename, you must specify them with dirWatch.system and dirWatch.talkgroup
                "mask": "#DATE_#TIMEP25ABC_#SYS_TRAFFIC__TO_#TG_FROM_#UNIT",

                // [optional or mandatory for dirWatch.type="trunk-recorder"]
                // The system ID to import to
                // It can also be a regex string, but consider using the mask option
                "system": 1,

                // [optional]
                // The talkgroup ID to import to
                // It can also be a regex string, but consider using the mask option
                "talkgroups": 1001,

                // [optional]
                // The type of the audio file (defaults to "default")
                // It can be "default" or "trunk-recorder"
                "type": "default",

                // [optional]
                // Set this to true to successfully watch files over a network
                "usePolling": true
            }
        ]
    }
}
```

> It is recommended to test your regex at [regex101](https://regex101.com/)

## Examples

### Example for SDRTrunk

> Tested with SDRTrunk 0.5.0 Alpha 1.

We have a `System P25ABC` with `Site 11` which uses an alias table configured to record specified talkgroups, either one by one or by a range. Keep in mind though that only known system/talkgroup by `config.json` will be imported.

```json
{
    "rdioScanner": {
        "dirWatch": [
            {
                "deleteAfter": true,
                "directory": "/home/radio/SDRTrunk/recordings",
                "extension": "mp3",
                "mask": "#DATE_#TIMEP25ABC_#SYS_TRAFFIC__TO_#TG_FROM_#UNIT",
            }
        ]
    }
}
```

### Example for Trunk Recorder

Importing `dirWatch.type="trunk-recorder"` will also import meta data from the `json` file.

```json
{
    "rdioScanner": {
        "dirWatch": [
            {
                "deleteAfter": true,
                "directory": "/home/radio/trunk-recorder/audio_files",
                "extension": "wav",
                "system": 11,
                "type": "trunk-recorder"
            }
        ]
    }
}
```