# CSDR-AM

The purpose of this shell script is to record multiple AM frequencies from a single RTL-SDR and creates individual audio files per conversation.

## Requirements

Tested on a Linux box.

* [csdr](https://github.com/ha7ilm/csdr)
* [sox](http://sox.sourceforge.net/)

## Configurations

### Within the script itself

Change the values to your needs. Make sure the `centered_freq` and `sampling_rate` covers the `freqs`.

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