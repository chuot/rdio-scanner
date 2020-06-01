# Access

## Description

It is possible to limit the access to your **Rdio Scanner** instance by adding access control to the file *server/config.json*.

The *rdioScanner.access* property can be as simple as a single *password* to an *array of objects*.

```js
{
    "rdioScanner": {
        "access": [
            {
                // [mandatory] The code/password for the user.
                "code": "password",

                // [mandatory] List of allowed systems and talkgroups.
                "systems": [
                    {
                        // the system ID.
                        "id": 1,

                        // the list of allowed talkgroup IDs.
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

## Some examples

### Full access without access control

```json
{
    "rdioScanner": {
        "access": null
    }
}
```

### Full access with access control

```json
{
    "rdioScanner": {
        "access": "password"
    }
}
```

```json
{
    "rdioScanner": {
        "access": [
            "password1",
            "password2"
        ]
    }
}
```

```json
{
    "rdioScanner": {
        "access": {
            "code": "password",
            "systems": "*"
        }
    }
}
```

### Restricted access to systems

```json
{
    "rdioScanner": {
        "access": {
            "code": "password",
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
        "access": {
            "code": "password",
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
        "access": [
            {
                "code": "password1",
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
                "code": "password2",
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
        "access": [
            {
                "code": "password1",
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
                "code": "password2",
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
        "access": [
            "password_for_myself", // full access
            {
                "code": "password_for_my_best_friend",
                "systems": [
                    1,
                    2,
                    3,
                    4
                ] // full access to systems 1, 2, 3 and 4 only
            },
            {
                "code": "password_for_other_friend",
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
                ] // full access to systems 1 and 2, some talkgroups from system 3
            }
        ]
    }
}
```