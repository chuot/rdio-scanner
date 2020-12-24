# RTLSDR-Airband

Here's an example with RTLSDR-Airband:

## RTLSDR-Airband's configuration

The trick here is to split audio by transmission and to leverage to `filename_template` to pass the `system_id` and `talkgroup_id`, which can then be utilized by [Rdio Scanner](https://github.com/chuot/rdio-scanner)'s `dirWatch.mask`. So generated files will look like `61_118900_20200711_111311_118900000.mp3`.

``` json
filename_template = "61_118900";
split_on_transmission = true;
```

## Rdio Scanner's configuration

``` json
"dirWatch": [
      {
        "deleteAfter": true,
        "directory": "/home/radio/rtlsdr-airband/audio_files",
        "extension": "mp3",
        "mask": "#SYS_#TG_#DATE_#TIME_#HZ"
      }
]
```

The `dirWatch` metatags will then translate to:

| Metatag | Value     |
| ------- | --------- |
| #SYS    | 61        |
| #TG     | 118900    |
| #DATE   | 20200711  |
| #TIME   | 111311    |
| #FREQ   | 118900000 |
