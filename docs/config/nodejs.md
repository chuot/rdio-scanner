# Node.js

## Definition

```js
{
    "nodejs": {
        // [optional]
        // Should we run our instance in *production* or *development* mode?
        // Default is "production".
        "env": "production",

        // [optional]
        // On which IP our instance will answer?
        // Default is "0.0.0.0".
        "host": "0.0.0.0",

        // [optional]
        // On which PORT our instance will answer?
        // Default is 3000.
        "port": 3000,

        // [optional]
        // Location of the SSL certificate
        "sslCert": "./server.crt",

        // [optional]
        // Location of the SSL private key
        "sslKey": "./server.key"
    }
}
```

> **Rdio Scanner** will run in HTTPS only if both *sslCert* and *sslKey* are specified.
