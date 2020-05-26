# CSDR-AM

The purpose of this shell script is to record multiple AM frequencies from a single RTL-SDR and creates individual audio files per conversation.

## Requirements

Tested on a linux box.

* [csdr](https://github.com/ha7ilm/csdr)
* [sox](http://sox.sourceforge.net/)

## Configurations

### Within the script itself

Change the the values to you needs. Make sure the `centered_freq` and `sampling_rate` covers the `freqs`.

Note the syntax for `freqs` and `filenames`, they are arrays.

Each filename template use sox multiple output mode syntax for file numbering (`%6n`). The static part is reused by `dirWatch` to distinguish which `talkgroup` to import to.

```bash
audio_dir=./audio_files/air
device_id=00000001
center_freq=119.4e6
gain=38.6
sampling_rate=2400000
squelch=2e-4

freqs=(118.9e6 119.1e6 119.9e6)

filenames=(118900000-%6n 119100000-%6n 119900000-%6n)
```

## Rdio Scanner

Whenever a new file is being created by the `CSDR-AM` script, `Rdio Scanner` will pick it up and load it.

Since the script create a file for each frequency which will have a size of `0` until the next conversation, we have to tell the `dirWatch` entry to delay the import for `4000` milliseconds.

The system also needs to be defined.

> For simplicy, talkgroup ids are defined with the same value as for the frequency. It makes it easier to change things afterwards.

```json
"rdioScanner": {
    "dirWatch": [
		{
			"delay": 4000,
			"deleteAfter": true,
			"directory": "/home/radio/csdr/audio_files/air",
			"extension": "wav",
			"system": 61,
			"talkgroup": ".*\\/(\\d+)-.*"
		}
    ],
    "systems": [
        {
            "id": 61,
            "label": "AIRCRAFT",
            "led": "cyan",
            "talkgroups": [
                {
                    "id": 119100000,
                    "label": "YMX TOWER",
                    "name": "YMX Tower",
                    "tag": "Aircraft",
                    "group": "AIRCRAFT",
                    "frequency": 119100000
                },
                {
                    "id": 119900000,
                    "label": "YUL TOWER",
                    "name": "YUL Tower",
                    "tag": "Aircraft",
                    "group": "AIRCRAFT",
                    "frequency": 119900000
                },
                {
                    "id": 118900000,
                    "label": "YUL DA_SE",
                    "name": "YUL Dep/Arr S/E",
                    "tag": "Aircraft",
                    "group": "AIRCRAFT",
                    "frequency": 118900000
                }
            ]
        }
    ]
}
```