# Rdio Scanner's API

There is two API endpoints available you can use to upload your audio files to [Rdio Scanner](https://github.com/chuot/rdio-scanner).

> Remember that systems/talkgroups need to be defined in `config.json` .

## Generic API - /api/call-upload

This API is used by the **downstream** feature to received audio files from other [Rdio Scanner](https://github.com/chuot/rdio-scanner) instances.

However, this API can be used for purposes other than **downstream**, as long as the API key gives access to the systems/talkgroups on which you wish to upload the audio file.

``` bash
$ curl https://other-rdio-scanner.example.com/api/call-upload \
    -F "key=d2079382-07df-4aa9-8940-8fb9e4ef5f2e" \
    -F "audio=@/recordings/audio.wav" \
    -F "dateTime=1970-01-01T00:00:00.000Z" \
    -F "frequencies=[]" \
    -F "frequency=774031250" \
    -F "source=4424000" \
    -F "sources=[]" \
    -F "system=11" \
    -F "talkgroup=54241"
Call imported successfully
```

- **key** - API key on the receiving host, see `config.json`.
- **audio** - Full path to your audio file. The path **must be prefixed** with the **@ sign**.
- **dateTime** - Audio date and time in JSON format.
- **frequencies** - (optional) A JSON string. Inspired by *Trunk Recorder*, the JSON structure is:
    ``` typescript
    {
        errorCount: number;
        freq: number;         // in hertz
        len: number;          // in seconds
        pos: number;          // in seconds
        spikeCount: number;
    }[]
    ```
- **frequency** - (optional) The frequency at which the call was recorded.
- **source** - (optional) The unit ID.
- **sources** - (optional) A JSON string. Inspired by *Trunk Recorder*. the JSON string structure is:
    ``` typescript
    {
        pos: number;          // in seconds
        src: number;          // the unit ID
    }[]
    ```
- **system** - The system ID to attach this audio file.
- **talkgroup** - The talkgroup ID to attach this audio file.

## Trunk Recorder API - /api/trunk-recorder-call-upload

This API is used by *Trunk Recorder upload-script* where a JSON file is also available for metadata.

``` bash
$ curl https://rdio-scanner.other.instance/api/trunk-recorder-call-upload \
    -F "key=d2079382-07df-4aa9-8940-8fb9e4ef5f2e" \
    -F "audio=@/recordings/audio.wav" \
    -F "meta=@/recordings/audio.json" \
    -F "system=11"
Call imported successfully
```

- **key** - API key on the receiving host, see `config.json`.
- **audio** - Full path to your audio file. The path **must be prefixed** with the **@ sign**.
- **meta** - Full path to your audio metadata. the path **must be prefixed** with the **@ sign**.
- **system** - The system ID to link this audio file.