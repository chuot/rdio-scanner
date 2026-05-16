# API

There is two API endpoints available you can use to upload your audio files to [Rdio Scanner](https://github.com/chuot/rdio-scanner).

## Endpoint: /api/call-upload

This API is used by the **downstream** feature to received audio files from other [Rdio Scanner](https://github.com/chuot/rdio-scanner) instances.

However, this API can be used for purposes other than **downstream**, as long as the API key gives access to the systems/talkgroups on which you wish to upload the audio file.

```bash
$ curl https://other-rdio-scanner.example.com/api/call-upload \
    -F "audio=@/recordings/audio.wav"             \
    -F "audioFileName=audio.wav"                  \
    -F "audioMime=audio/x-wav"                    \
    -F "dateTime=1970-01-01T00:00:00.000Z"        \
    -F "frequencies=[]"                           \
    -F "frequency=774031250"                      \
    -F "key=d2079382-07df-4aa9-8940-8fb9e4ef5f2e" \
    -F "patches=[]"                               \
    -F "site=1"                                   \
    -F "source=4424000"                           \
    -F "sources=[]"                               \
    -F "system=11"                                \
    -F "systemLabel=RSP25MTL"                     \
    -F "talkgroup=54241"                          \
    -F "talkgroupGroup=Fire"                      \
    -F "talkgroupLabel=TDB A1"                    \
    -F "talkgroupTag=Fire dispatch"               \
    -F "timestamp=1669740338000"
Call imported successfully
```

- **audio** - full path to your audio file. The path **must be prefixed** with the **@ sign**.
- **audioName** - [optional] file name (it can be derived from the audio field).
- **audioType** - [optional] mime type. (it can be derived from the audio field).
- **dateTime** - [or timestamp] date and time in RFC3339 or unix time format. Timestamp is however more precise and works better with call duplicates detection.
- **frequencies** - [optional] JSON array of objects for frequency changes throughout the conversation.

        {
          dbm: number;        // in decibel per one milliwatt
          errorCount: number;
          freq: number;       // in hertz
          len: number;        // in seconds
          pos: number;        // in seconds
          spikeCount: number;
        }[];

- **frequency** - [optional] the frequency on which the audio file was recorded.
- **key** - API key on the receiving host.
- **patches** - [optional] JSON array of talkgroup IDs.
- **site** - [optional] site ID.
- **system** - [optional] system ID.
- **systemLabel** - [optional] system label.
- **talkgroup** - talkgroup ID.
- **talkgroupGroup** - [optional] talkgroup group.
- **talkgroupLabel** - [optional] talkgroup label.
- **talkgroupTag** - [optional] talkgroup tag.
- **timestamp** - [or dateTime] date ad time in Unix milliseconds format.
- **unit** - [optional] unit ID.
- **units** - [optional] JSON array of objects for unit ID changes throughout the conversation.

        {
          id: number;     // unit ID
          label: number;  // [optional] unit tag
          offset: number; // in seconds
        }[];

