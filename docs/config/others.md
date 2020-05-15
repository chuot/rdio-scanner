# Other configuration options

## Definitions

```json
{
    "rdioScanner": {
        // [optional] Does downloading calls from the *search call panel* is allowed.
        // The default is true
        "allowDownload": true,

        // [optional] Automaticaly delete calls older than *pruneDays* from the database
        // The default is 7 days
        "pruneDays": 7,

        // [optional] Lower the display brightness when no active call
        // The default is false
        "useDimmer": false,

        // [optional] Can talkgroups be activated/disabled by groups on the *select tgs panel*
        // The default is true
        "useGroup": true,

        // [optional] Show LED on display
        // The default is true
        "useLed": true
    }
}
```