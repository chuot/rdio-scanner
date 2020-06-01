# API keys

## Description

Uploading calls to **Rdio Scanner** requires to have a valid *API key* to do so.

The *rdioScanner.apiKeys* property can be as simple as a single *key* to an *array of objects*.

If you are already familiar with the *rdioScanner.access* property, it works prety much the same.

```js
{
    "rdioScanner": {
        "apiKey": [
            {
                // [mandatory] The API key for the uploader.
                "key": "b29eb8b9-9bcd-4e6e-bb4f-d244ada12736",

                // [mandatory] the list of allowed systems to upload to.
                "systems": [
                    {
                        // the system ID.
                        "id": 1,

                        // the list of talkgroup IDs.
                        "talkgroups": [
                            1001,
                            1002
                        ]
                    }
                ]
            }
        ]
    }
}
```

> It is recommended to use UUIDs for *apiKeys* to provide a good enough password.
> You can use `cat /proc/sys/kernel/random/uuid` on some Linux distro.

## Some examples

### Full access without access control

```json
{
    "rdioScanner": {
        "apiKeys": "b29eb8b9-9bcd-4e6e-bb4f-d244ada12736"
    }
}
```

```json
{
    "rdioScanner": {
        "apiKey": [
            "b29eb8b9-9bcd-4e6e-bb4f-d244ada12736",
            "44c313eb-273e-44a8-ab6c-c97161a61b5f"
        ]
    }
}
```

```json
{
    "rdioScanner": {
        "apiKey": [
            {
                "key": "b29eb8b9-9bcd-4e6e-bb4f-d244ada12736",
                "systems": "*",
            },
            {
                "key": "44c313eb-273e-44a8-ab6c-c97161a61b5f",
                "systems": "*"
            }
        ]
    }
}
```

### Restricted access to systems

```json
{
    "rdioScanner": {
        "apiKeys": {
            "key": "b29eb8b9-9bcd-4e6e-bb4f-d244ada12736",
            "systems": [
                1,
                3,
            ]
        }
    }
}
```

```json
{
    "rdioScanner": {
        "apiKeys": {
            "key": "b29eb8b9-9bcd-4e6e-bb4f-d244ada12736",
            "systems": [
                {
                    "id": "1",
                    "talkgroups": "*"
                },
                {
                    "id": "3",
                    "talkgroups": "*"
                }
            ]
        }
    }
}
```

```json
{
    "rdioScanner": {
        "apiKeys": [
            {
                "key": "b29eb8b9-9bcd-4e6e-bb4f-d244ada12736",
                "systems": [
                    {
                        "id": "1",
                        "talkgroups": "*"
                    },
                    {
                        "id": "3",
                        "talkgroups": "*"
                    }
                ]
            },
            {
                "key": "44c313eb-273e-44a8-ab6c-c97161a61b5f",
                "systems": [
                    {
                        "id": "2",
                        "talkgroups": "*"
                    },
                    {
                        "id": "4",
                        "talkgroups": "*"
                    }
                ]
            },
        ]
    }
}
```

### Restricted access to systems and talkgroups

```json
{
    "rdioScanner": {
        "apiKeys": [
            {
                "key": "b29eb8b9-9bcd-4e6e-bb4f-d244ada12736",
                "systems": [
                    {
                        "id": "1",
                        "talkgroups": [
                            1001,
                            1002
                        ]
                    },
                    {
                        "id": "3",
                        "talkgroups": [
                            3003,
                            3004
                        ]
                    }
                ]
            },
            {
                "key": "44c313eb-273e-44a8-ab6c-c97161a61b5f",
                "systems": [
                    {
                        "id": "2",
                        "talkgroups": [
                            2001,
                            2003
                        ]
                    },
                    {
                        "id": "4",
                        "talkgroups": [
                            4002,
                            4004
                        ]
                    }
                ]
            },
        ]
    }
}
```

### A mix of all the possibilities

```json
{
    "rdioScanner": {
        "apiKeys": [
            "key": "b29eb8b9-9bcd-4e6e-bb4f-d244ada12736", // can upload to any
            {
                "key": "44c313eb-273e-44a8-ab6c-c97161a61b5f",
                "systems": [
                    1,
                    2,
                    3,
                    4
                ] // My best friend can upload to systems 1, 2, 3 and 4 only
            },
            {
                "key": "ff6aa2e2-afd7-4e05-949e-731a42f3391e",
                "systems": [
                    1,
                    2,
                    {
                        "id": 3,
                        "talkgroups": [
                            3001,
                            3003,
                            3018
                        ]
                    }
                ] // My other friend and only upload to systems 1 and 2, some talkgroups from system 3
            }
        ]
    }
}
```
