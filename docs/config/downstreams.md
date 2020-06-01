# Downstream

## Definition

Since **Rdio Scanner v4.0**, imported calls can be re-exported to other instances of **Rdio Scanner**. This means that you can build a mesh of **Rdio Scanner** with you and your friends so you could have your dream scanner!

```js
{
    "rdioScanner": {
        "downstreams": [
            {
                // [mandatory] This API key of the downstream instance to export to
                "apiKey": "b29eb8b9-9bcd-4e6e-bb4f-d244ada12736",

                // [optional] Which systems/talkgroups to export
                // Same syntax/structure as for the rdioScanner.access property
                // Default is "*"
                "systems": "*",

                // [mandatory] The URL of the downstream instance where to export
                "url": "http://downstream-root-url/"
            }
        ]
    }
}
```

## Example

Let say you live in *city A* and your friend in *city B*. Since you used to lived in *city B* and would like to listen to radio waves from that city, you friend could export some or all calls from its instance to yours. For that to happens, you have to give a valid *apiKey* to your friend. And your friend would configure a *downstream* to your instance with that *apiKey*.

### Your configuration (city A)

```json
{
    "rdioScanner": {
        "apiKeys": [
            {
                // Your friend at *city B* will be able to upload to system #8
                "key": "44c313eb-273e-44a8-ab6c-c97161a61b5f",
                "systems": 8
            }
        ],
        "systems": [
            // Remember to define the system in your own config file to have the upload working
            {
                "id": 8,
                "label": "CITY B",
                "talkgroups": [
                    {
                        "id": 8001,
                        "group": "POLICE DISPATCH",
                        "label": "CITY-B PD",
                        "name": "City B Police Department",
                        "tag": "Police Dispatch",
                    },
                    {
                        "id": 8002,
                        "group": "FIRE DISPATCH",
                        "label": "CITY-B FD 1",
                        "name": "City B Fire Department",
                        "tag": "Fire Dispatch",
                    },
                    {
                        "id": 8003,
                        "group": "FIRE TAC",
                        "label": "CITY-B FD 2",
                        "name": "City B Fire Operations",
                        "tag": "Fire Tactical",
                    }
                ]
            }
        ]
    }
}
```

### Your friend's configuration (city B)

```json
{
    "rdioScanner": {
        "downstreams": [
            {
                "key": "44c313eb-273e-44a8-ab6c-c97161a61b5f",
                "systems": [
                    {
                        "id": 8,
                        "talkgroups": [
                            8001,
                            8002,
                            8003
                        ]
                    }
                ],
                "url": "http://your-friend-at-city-a/"
            }
        ],
        "systems": [
            {
                "id": 8,
                "label": "CITY B",
                "talkgroups": [
                    {
                        "id": 8001,
                        "group": "POLICE DISPATCH",
                        "label": "CITY-B PD",
                        "name": "City B Police Department",
                        "tag": "Police Dispatch",
                    },
                    {
                        "id": 8002,
                        "group": "FIRE DISPATCH",
                        "label": "CITY-B FD 1",
                        "name": "City B Fire Department",
                        "tag": "Fire Dispatch",
                    },
                    {
                        "id": 8003,
                        "group": "FIRE TAC",
                        "label": "CITY-B FD 2",
                        "name": "City B Fire Operations",
                        "tag": "Fire Tactical",
                    },
                    {
                        "id": 8004,
                        "group": "FIRE TALK",
                        "label": "CITY-B FD 3",
                        "name": "City B Fire Training",
                        "tag": "Fire Talk",
                    }
                ]
            }
        ]
    }
}
```
