# Systems

## Definition

Since **Rdio Scanner v4.0**, the only way to configure radio systems is from the file *server/config.json*.

```js
{
    "rdioScanner": {
        "systems": [
            {
                // [mandatory] System ID
                "id": 1,

                // [mandatory] System label shown on display
                "label": "SYSTEM",

                // [optional] Specify LED color for this system
                // Valid values are: blue, cyan, green, magenta, red, white, yellow;
                // Default is green
                "led": "cyan",

                // [mandatory] Talkgroups array
                "talkgroups": [
                    {
                        // [mandatory] Talkgroup ID
                        "id": 54241,

                        // [mandatory] if rdioScanner.useGroup is true.
                        // Used when activating/deactivating group of talkgroups on the select tgs panel
                        "group": "FIRE DISPATCH",

                        // [mandatory] Talkgroup (short) label shown buttons
                        "label": "TDB A1",

                        // [optional] Specify LED color for this talkgroup
                        // Valid values are: blue, cyan, green, magenta, red, white, yellow;
                        // Default is green
                        "led": "cyan",

                        // [mandatory] Talkgroup name
                        "name": "MRC TDB Fire Alpha 1",

                        // [mandatory] Talkgroup tag shown on the right side of the display
                        "tag": "Fire Dispatch",

                        // [optional] If you want to display unit's name/label instead of unitId.
                        "units": [
                            {
                                // [mandatory] The unit ID
                                "id": 4424001,

                                // [mandatory] The unit name/label
                                "label": "CAUCA"
                            }
                        ]
                    }
                ]
            }
        ]
    }
}
```

> **Rdio Scanner** won't import any calls that aren't configured in *rdioScanner.systems* property.

## Example

```json
{
    "rdioScanner": {
        "systems": [
            {
                "id": 11,
                "label": "RSP25MTL1",
                "led": "red",
                "talkgroups": [
                    {
                        "id": 54241,
                        "label": "TDB A1",
                        "name": "MRC TDB Fire Alpha 1",
                        "tag": "Fire Dispatch",
                        "group": "FIRE DISPATCH"
                    },
                    {
                        "id": 54242,
                        "label": "TDB B1",
                        "name": "MRC TDB Fire Bravo 1",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 54243,
                        "label": "TDB B2",
                        "name": "MRC TDB Fire Bravo 2",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 54248,
                        "label": "TDB B3",
                        "name": "MRC TDB Fire Bravo 3",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 54251,
                        "label": "TDB B4",
                        "name": "MRC TDB Fire Bravo 4",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 54261,
                        "label": "TDB B5",
                        "name": "MRC TDB Fire Bravo 5",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 54244,
                        "label": "TDB B6",
                        "name": "MRC TDB Fire Bravo 6",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 54129,
                        "label": "TDB B7",
                        "name": "MRC TDB Fire Bravo 7",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 54125,
                        "label": "TDB B8",
                        "name": "MRC TDB Fire Bravo 8",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    }
                ],
                "units": [
                    {
                        "id": 4424001,
                        "label": "CAUCA"
                    }
                ]
            },
            {
                "id": 12,
                "label": "RSP25MTL2",
                "talkgroups": [
                    {
                        "id": 58121,
                        "label": "SSIAL FD",
                        "name": "SSIAL Fire Dispatch",
                        "tag": "Fire Dispatch",
                        "group": "FIRE DISPATCH"
                    },
                    {
                        "id": 58122,
                        "label": "SSIAL CH A",
                        "name": "SSIAL Radio Channel A",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 58124,
                        "label": "SSIAL CH B",
                        "name": "SSIAL Radio Channel B",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 58126,
                        "label": "SSIAL CH C",
                        "name": "SSIAL Radio Channel C",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 58139,
                        "label": "SSIAL INTEROP",
                        "name": "SSIAL Interop",
                        "tag": "Interop",
                        "group": "INTEROP"
                    }
                ],
                "tags": [
                    {
                        "id": 2006,
                        "label": "Dispatch"
                    }
                ]
            },
            {
                "id": 15,
                "label": "RSP25MTL5",
                "talkgroups": [
                    {
                        "id": 54231,
                        "label": "TB INC 1",
                        "name": "Terrebonne Fire Dispatch",
                        "tag": "Fire Dispatch",
                        "group": "FIRE DISPATCH"
                    },
                    {
                        "id": 54232,
                        "label": "TB INC 2",
                        "name": "Terrebonne Fire Operations",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    }
                ],
                "tags": [
                    {
                        "id": 4424001,
                        "label": "CAUCA"
                    }
                ]
            },
            {
                "id": 21,
                "label": "SERAM",
                "talkgroups": [
                    {
                        "id": 50001,
                        "label": "SG 1",
                        "name": "SERAM Regroupement 1",
                        "tag": "Fire Dispatch",
                        "group": "FIRE DISPATCH"
                    },
                    {
                        "id": 50002,
                        "label": "SG 2",
                        "name": "SERAM Regroupement 2",
                        "tag": "Fire Dispatch",
                        "group": "FIRE DISPATCH"
                    },
                    {
                        "id": 50003,
                        "label": "SG 3",
                        "name": "SERAM Regroupement 3",
                        "tag": "Fire Dispatch",
                        "group": "FIRE DISPATCH"
                    },
                    {
                        "id": 50004,
                        "label": "SG 4",
                        "name": "SERAM Regroupement 4",
                        "tag": "Fire Dispatch",
                        "group": "FIRE DISPATCH"
                    },
                    {
                        "id": 50005,
                        "label": "SG 5",
                        "name": "SERAM Regroupement 5",
                        "tag": "Fire Dispatch",
                        "group": "FIRE DISPATCH"
                    },
                    {
                        "id": 50006,
                        "label": "SG 6",
                        "name": "SERAM Regroupement 6",
                        "tag": "Fire Dispatch",
                        "group": "FIRE DISPATCH"
                    },
                    {
                        "id": 50007,
                        "label": "SG 7",
                        "name": "SERAM Regroupement 7",
                        "tag": "Fire Dispatch",
                        "group": "FIRE DISPATCH"
                    },
                    {
                        "id": 50008,
                        "label": "SG 8",
                        "name": "SERAM Regroupement 8",
                        "tag": "Fire Dispatch",
                        "group": "FIRE DISPATCH"
                    },
                    {
                        "id": 60051,
                        "label": "CMD 3",
                        "name": "SERAM Commandement 3",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 60052,
                        "label": "CMD 4",
                        "name": "SERAM Commandement 4",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 60053,
                        "label": "CMD 5",
                        "name": "SERAM Commandement 5",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 60054,
                        "label": "CMD 6",
                        "name": "SERAM Commandement 6",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 60055,
                        "label": "CMD 7",
                        "name": "SERAM Commandement 7",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 60056,
                        "label": "CMD 8",
                        "name": "SERAM Commandement 8",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 60059,
                        "label": "CMD 11",
                        "name": "SERAM Commandement 11",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 60091,
                        "label": "CMD 12",
                        "name": "SERAM Commandement 12",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 60092,
                        "label": "CMD 13",
                        "name": "SERAM Commandement 13",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 60057,
                        "label": "CMD 14",
                        "name": "SERAM Commandement 14",
                        "tag": "Fire Tac",
                        "group": "FIRE TAC"
                    },
                    {
                        "id": 60294,
                        "label": "DSRC MEDIA",
                        "name": "DSRC Media",
                        "tag": "Media",
                        "group": "MEDIA"
                    }
                ],
                "tags": [
                    {
                        "id": 702099,
                        "label": "Dispatch"
                    }
                ]
            },
            {
                "id": 61,
                "label": "AIRCRAFT",
                "talkgroups": [
                    {
                        "id": 1,
                        "label": "YMX TOWER",
                        "name": "YMX Tower",
                        "tag": "Aircraft",
                        "group": "AIRCRAFT",
                        "frequency": 119.1
                    },
                    {
                        "id": 2,
                        "label": "YMX GROUND",
                        "name": "YMX Ground",
                        "tag": "Aircraft",
                        "group": "AIRCRAFT",
                        "frequency": 121.8
                    }
                ]
            }
        ],
    }
}
```