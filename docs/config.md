# Administrative Dashboard

The administrative dashboard is accessible at **http[s]://<your_instance_address>/admin**

The administrative dashboard contains these sections:

- **Config** - _Server's configurations section._
  - **Access** - _Control access to your instance._
  - **ApiKey** - _Keys for audio files ingestion._
  - **Dirwatch** - _Folder monitoring for audio files ingestion._
  - **Downstreams** - _Share your audio files with other instances._
  - **Groups** - _Talkgroup groups definitions._
  - **Options** - _Global runtime options._
  - **Systems** - _Systems and talkgroups definitions._
  - **Tags** - _Talkgroup tags definitions._
- **Logs** - _Server's log._
- **Tools** - _Useful tools._
  - **Talkgroups CSV** - _Easily import CSV files._
  - **Admin password** - _Manage admin password._
  - **Import/export config** - _Backup and restore your entire server configuration._

Each subsection contains its own built-in documentation to help you configure your [Rdio Scanner](https://github.com/chuot/rdio-scanner) instance.

# Admin Password

The default admin password is `rdio-scanner` and must should be changed ASAP. To change the admin password, go to `Administrative dashboard / Tools / Admin password`. It is also possible to reset the admin password from the command line:

```bash
$ cd ~/rdio-scanner
$ node server reset-admin-password mynewpassword
Admin password has been reset.
```

## rdio-scanner/server/config.json

```js
{
    "nodejs": {
        // (string) "development" or "production". Default is "production".
        "environment": "production",

        // (string) Which IP to bind to? Default is "0.0.0.0"
        "host": "0.0.0.0",

        // (number) Which PORT to bind to? Default is 3000
        "port": 3000,

        // (boolean) Run server in https mode
        "ssl": false,

        // (string) Path to the SSL CA certificate
        // See SSL certificates section below for more details
        "sslCA": "ca.crt",

        // (string) Path to the SSL certificate
        // See SSL certificates section below for more details
        "sslCert": "server.crt",

        // (string) Path to the SSL privat key
        // See SSL certificates section below for more details
        "sslKey": "server.key"
    },

    // Rdio Scanner uses sqlite for its default database.
    // It is highly recommended to stick to sqlite as it works very well.
    // Refer to https://sequelize.org/v5/manual/dialects.html for more details
    "sequelize": {
        "database": null,
        "dialect": "sqlite",
        "host": null,
        "password": null,
        "port": null,
        "storage": "database.sqlite",
        "username": null
    }
}
```

## SSL certificates - nodejs.sslCA / nodejs.sslCert / nodejs.sslKey

[Rdio Scanner](https://github.com/chuot//rdio-scanner) will automatically create self signed certificates for you if `openssl` is available on your system and no certificates are already defined. The server runs by default in normal HTTP mode, you still to change `nodejs.ssl` to `true` in your `config.json` and restart the server to run in HTTPS mode.

> BEAWARE THAT IOS DOESN'T ALLOW TO CONNECTIONS TO SECURE WEBSOCKET WHEN USING A SELF SIGNED CERTIFICATE. YOU MUST USE PROPERLY SIGNED CERTIFICATES.