# **Rdio Scanner** configuration

Since **Rdio Scanner v4.0**, you have to configure your **Rdio Scanner** instance through the file located at `server/config.json`.

```js
{
    "nodejs": {
        // Node.js specifics
    },
    "sequelize": {
        // Sequelize specifics
    },
    "rdioScanner": {
        "access": [
            // [optional] Rdio Scanner access control
        ],
        "allowDownload": true, // see config/others.md
        "apiKeys": [
            // [optional] RdioScanner API keys for uploaders
        ],
        "dirWatch": [
            // [optional] Directory watchers for automaric call audio importation
        ],
        "downstream": [
            // [optional] Re-export imported calls to other Rdio Scanner instances
        ],
        "pruneDays": 7, // see config/others.md
        "systems": [
            // [mandatory] Systems and talkgroups definitions
        ],
        "useGroup": true // see config/others.md
    }
}
```

You can refer to those documents for each sections:

* [nodejs](./config/nodejs.md) instructions

* [sequelize](./config/sequelize.md) instructions

* rdioScanner instructions

  * [access](./config/access.md) section
  * [apiKeys](./config/api-keys.md) section
  * [dirWatch](./config/dir-watch.md) section
  * [downstreams](./config/downstreams.md) section
  * [systems](./config/systems.md) section
  * [other](./config/others.md) parameters