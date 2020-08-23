# Rdio Scanner Configuration File

This file is at the heart of [Rdio Scanner](https://github.com/chuot/rdio-scanner).

> If your [Rdio Scanner](https://github.com/chuot/rdio-scanner) is already running, be sure to restart it every time you make changes to the `config.json` file.

[Rdio Scanner](https://github.com/chuot/rdio-scanner) will analyze this file and rewrite it each time the server is started. This is handy if you want to create a default configuration file the first time.

**Structure**

``` js
{
    "nodejs": {
        // (string) "development" or "production". Default is "production".
        "environment": "production",

        // (string) Which IP to bind to? Default is "0.0.0.0"
        "host": "0.0.0.0",

        // (number) Which PORT to bind to? Default is 3000
        "port": 3000,

        // (string) Path to the SSL CA certificate
        // See SSL certificates section below for more details
        "sslCA": null,

        // (string) Path to the SSL certificate
        // See SSL certificates section below for more details
        "sslCert": null,

        // (string) Path to the SSL privat key
        // See SSL certificates section below for more details
        "sslKey": null
    },

    "rdioScanner": {
        // (multiple types) Access control for Rdio Scanner
        // See Access control section below for more details
        "access": null,

        // (multiple types) API keys for upload scripts and downstreamers
        // Default value is a random generated UUID
        // See API keys section below for more details
        "apiKeys": "<random generated uuid>",

        // (object[]) Directory monitoring for ingestion of audio files
        // Default value is an empty array
        // See DirWatch section below for more details
        "dirWatch": []

        // (object[]) Downstream audio files to other Rdio Scanner instances
        // Default value is an empty array
        // See Downstreams section below for more details
        "downstreams": [],

        // (object) Options definitions
        "options": {
            // (boolean) Can users download archived calls. Default value is true
            "allowDownload": true,

            // (number) Delay in milliseconds before turning off the screen backlight when inactive
            // Default value is 5000 milliseconds
            "dimmerDelay": 5000,

            // (boolean) Disable the audio format conversion to m4a/aac
            // Default value is false
            "disableAudioConversion": false,

            // (boolean) Emit beeps when clicking buttons
            // Default value is 1 which is preset #1
            // See KeypadBeeps section below for more details
            "keypadBeeps": 1,

            // (number) Clear the database for audio files older than the specified number of days
            // Default value is 7
            // Specifying a value of 0 will disable this feature
            "pruneDays": 7,

            // (boolean) Also toggle talkgroups based on their group assignment
            // Default value is true
            // See Systems section below for more details on groups
            "useGroup": true
        }

        // (object[]) Systems definitions
        // See Systems section below for more details
        "systems": []
    },

    // Radio Scanner uses sqlite for its default database
    // It is recommended to stick to sqlite as it works very well
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

## SSL certificates - nodejs.sslCert / nodejs.sslKey

It is possible to run [Rdio Scanner](https://github.com/chuot/rdio-scanner) in SSL mode.

If you don't have a certificate, you can generate one yourself like this:

``` bash
$ openssl req -nodes -new -x509 -keyout server.key -out server.cert
Generating a RSA private key
..........+++++
.............................................................................+++++
writing new private key to 'server.key'
-----
You are about to be asked to enter information that will be incorporated
into your certificate request.
What you are about to enter is what is called a Distinguished Name or a DN.
There are quite a few fields but you can leave some blank
For some fields there will be a default value,
If you enter '.', the field will be left blank.
-----
Country Name (2 letter code) [XX]:
State or Province Name (full name) []:
Locality Name (eg, city) [Default City]:
Organization Name (eg, company) [Default Company Ltd]:
Organizational Unit Name (eg, section) []:
Common Name (eg, your name or your server's hostname) []:
Email Address []:
```

Then add the files to your configuration:

``` json
"nodejs": {
  "sslCert": "server.cert",
  "sslKey": "server.key"
}
```

> If you want to use a certificate provided by a certification authority, do not forget to add its trusted CA file to `nodejs.sslCA` if necessary.

## Access control - rdioScanner.access

You can decide to leave your [Rdio Scanner](https://github.com/chuot/rdio-scanner) instance unlocked and accessible to everyone, or set passwords with specific systems/talkgroups.

When used, the user will be prompted once for their password. It is then stored internally on the client side and reused thereafter.

**Structure of the rdioScanner.access object**

``` typescript
access: null | string | string[] | {
  code: string;
  systems: number | number[] | {
    id: number;
    talkgroups: number | number[];
  } | {
    id: number;
    talkgroups: number | number[];
  }[] | '*';
}
```

Default value is `null` .

**Examples**

* No access control, access to all systems/talkgroups

  

``` json
  "access": null
  ```

* Single unique password, access to all systems/talkgroups

  

``` json
  "access": "password"
  ```

  or

  

``` json
  "access": {
    "code": "password",
    "systems": "*"
  }
  ```

* Complex access control

`password1` gives access to system 11 / talkgroups 54241, 54125 and system 21 / talkgroups 60040, 60041, 50003.

`password2` and `password3` give access to all systems/talkgroups.

  

``` json
  "access": [
    {
      "code": "password1",
      "systems": [
        {
          "id": 11,
          "talkgroups": [
            54241,
            54125
          ]
        },
        {
          "id": 21,
          "talkgroups": [
            60040,
            60041,
            50003
          ]
        }
      ]
    },
    {
      "code": "password2",
      "systems": "*"
    },
    "password3"
  ]
  ```

## API keys - rdioScanner.apiKeys

API keys are used for recorder upload scripts and downstream instances. Each of them must authenticate with an API key to exchange audio files.

It is also possible to restrict these API keys to specific systems/talkgroups.

> You can generate new random UUIDs with the `random-uuid` option passed to the server. You can optionally pass the number of random uuids you want to generate as the first argument. Then copy/paste them into your `config.json` file.

**Structure of the rdioScanner.apiKeys object**

``` typescript
apiKeys: null | string | string[] | {
  key: string;
  systems: number | number[] | {
    id: number;
    talkgroups: number | number[];
  } | {
    id: number;
    talkgroups: number | number[];
  } | '*';
}
```

Default value is an empty array `[]` .

**Examples**

* Single API key which allows upload to any systems/talkgroups

  

``` json
  "apiKeys": "d2079382-07df-4aa9-8940-8fb9e4ef5f2e"
  ```

  or

  

``` json
  "access": {
    "key": "d2079382-07df-4aa9-8940-8fb9e4ef5f2e",
    "systems": "*"
  }
  ```

* Complex API keys definition

  API key `d2079382-07df-4aa9-8940-8fb9e4ef5f2e` gives access to system 11 / talkgroups 54241, 54125 and system 21 / talkgroups 60040, 60041, 50003.

  API keys `cfcfa7d5-897c-4b96-b974-2fec80c3f775` and `a3ac707e-eaf9-4951-a9d5-c52186fa5093` give access to all systems/talkgroups.

  

``` json
  "apiKeys": [
    {
      "key": "d2079382-07df-4aa9-8940-8fb9e4ef5f2e",
      "systems": [
        {
          "id": 11,
          "talkgroups": [
            54241,
            54125
          ]
        },
        {
          "id": 21,
          "talkgroups": [
            60040,
            60041,
            50003
          ]
        }
      ]
    },
    {
      "key": "cfcfa7d5-897c-4b96-b974-2fec80c3f775",
      "systems": "*"
    },
    "a3ac707e-eaf9-4951-a9d5-c52186fa5093"
  ]
  ```

## DirWatch - rdioScanner.dirWatch

You can also define a `dirWatch` to monitor new audio files from any directory.

**Structure**

``` typescript
dirWatch: {
  delay?: number;                      // optional, value is in ms
  deleteAfter?: boolean;               // default is false
  directory: string;                   // mandatory, unique
  extension: string;                   // mandatory
  frequency?: number;                  // optional, value is in hertz
  mask?: string;                       // optional, see possible values below
  system?: number | string;            // optional
  talkgroup?: number | string;         // optional
  type?: "default" | "trunk-recorder"; // optional, default is default
  usePolling?: boolean;                // optional, default is false
}[]
```

* **delay** - Depending on the recorder, audio files can be ingested too soon after the recorder has created the file. You can set a timeout value in *milliseconds* for the audio file to settle before ingesting it.
* **deleteAfter** - You may want the audio file to be deleted after being ingested. If this value is *true*, all pre-existing audio files will be ingested and deleted as soon as [Rdio Scanner](https://github.com/chuot/rdio-scanner) starts. When this parameter is *false*, pre-existing audio files are neither ingested nor deleted.
* **directory** - Absolute or relatives to [Rdio Scanner](https://github.com/chuot/rdio-scanner)'s. Path of the directory to be monitored. **This value must be unique**.
* **extension** - The audio call extension to monitor without the period. Ex.: "mp3", "wav".
* **frequency** - You may want to fake the frequency which will be displayed on *Rdio Scanner*. Let say that you are recording an AM frequency from *RTLSDR-Airband*, here you would put that frequency.
* **frequency** - You may want to simulate the frequency that will be displayed. Say you are recording an AM frequency from *RTLSDR-Airband*, here you would put that frequency.
* **mask** - Some metadata can be extracted from the file name of the audio file using specific META tags. Here is the list:
  + **#DATE** - extract the date like *20200608* or *2020-06-08*.
  + **#HZ** - extract the frequency in hertz like *119100000*.
  + **#KHZ** - extract the frequency in kilohertz like *119100*.
  + **#MHZ** - extract the frequency in megahertz like *119.100*.
  + **#TIME** - extract the *local* time like *0853439* or *08:34:39*.
  + **#SYS** - extract the system id like *11*.
  + **#TG** - extract the talkgroup id like *54241*.
  + **#UNIT** - extract the unit id like *4424001*.
  + **#ZTIME** - extract the *zulu* time like *0453439* or *04:34:39*.
* **system** - A valid system id defined in **rdioScanner.systems**.
* **talkgroup** - A valid talkgroup id defined in **rdioScanner.systems**.
* **type** - In case of *Trunk Recorder*, the metadata of the *JSON file* will be used.

> Note that [Rdio Scanner](https://github.com/chuot/rdio-scanner) must know the **system id** and the **talkgroup id** for the call to be ingested. These two values must be specified either by **dirWatch.system**, **dirWatch.talkgroup**, **dirWatch.mask** or a mix of them. **dirWatch.system** and **dirWatch.talkgroup** have priority over **dirWatch.mask**.

Default value is an empty array `[]` .

**Examples**

* Ingest audio files from **Trunk Recorder**

  

``` json
  "dirWatch": [
    {
      "deleteAfter": true,
      "directory": "/home/radio/trunk-recorder/audio_files/RSP25MTL1",
      "extension": "wav",
      "system": 11,
      "type": "trunk-recorder"
    },
    {
      "deleteAfter": true,
      "directory": "/home/radio/trunk-recorder/audio_files/SERAM",
      "extension": "wav",
      "system": 21,
      "type": "trunk-recorder"
    },
  ]
  ```

* Ingest audio files from **RTLSDR-Airband**

  

``` json
  "dirWatch": [
    {
      "deleteAfter": true,
      "directory": "/home/radio/rtlsdr-airband/audio_files",
      "extension": "wav",
      "mask": "TOWER_#DATE_#TIME_#HZ",
      "system": 61,
      "talkgroup": 61,
      "type": "trunk-recorder"
    }
  ]
  ```

* Ingest audio files from **SDRTrunk**

  

``` json
  "dirWatch": [
    {
      "deleteAfter": true,
      "directory": "/home/radio/SDRTrunk/recordings",
      "extension": "mp3",
      "mask": "#DATE_#TIMERSP25MTL_#SYS_TRAFFIC__TO_#TG_FROM_#UNIT"
    }
  ]
  ```

## Downstreams - rdioScanner.downstreams

Ingested audio calls can be sent downstream to other [Rdio Scanner](https://github.com/chuot/rdio-scanner) instances.

This is handy if you want to build a mesh of [Rdio Scanner](https://github.com/chuot/rdio-scanner). Let's say you want to exchange audio files with your friend who lives in another city. Or you want to ingest audio files from a Digital Logger through an internal instance and transfer these audio files to your public instance.

> Remember that the API key you will use must be correctly configured on the receiving host.

**Structure of the rdioScanner.downstreams object**

``` typescript
downstreams: {
  apiKey: string;
  disabled: boolean;
  systems: number | number[] | {
    id: number;
    talkgroups: number | number[] | "*" | undefined;
  } | {
    id: number;
    talkgroups: number | number[];
  }[] | "*" | undefined;
  url: string;
}[]
```

Default value is an empty array `[]` .

**Examples**

``` json
"downstreams": [
  {
    "apiKey": "d2079382-07df-4aa9-8940-8fb9e4ef5f2e",
    "disabled": false,
    "systems": [
      {
        "id": 11,
        "talkgroups": [
          54241,
          54125
        ]
      },
      {
        "id": 12
      },
      {
        "id": 15,
        "talkgroups": "*"
      },
      {
        "id": 21,
        "talkgroups": [
          60040,
          60041,
          50003
        ]
      },
      31
    ],
    "url": "http://rdio-scanner.example.com:3000/"
  },
  {
    "apiKey": "d2079382-07df-4aa9-8940-8fb9e4ef5f2e",
    "disabled": true,
    "systems": "*",
    "url": "https://scanner.other-host.com/"
  }
]
```

> Note in this example for API key `d2079382-07df-4aa9-8940-8fb9e4ef5f2e` that systems 12, 15 and 31 will downstream all of their talkgroups, unlike systems 11 and 21 which will only downstream some of their talkgroups.

## KeypadBeeps - rdioScanner.options.keypadBeeps

Each button press can emit a specific beep depending on whether a function is activated, deactivated or denied.

**Structure of the rdioScanner.options.keypadBeeps object**

``` typescript
options: {
  keypadBeeps: false | 1 | 2 | { // false = disabled, 1 = uniden, 2 = whistler, object = custom
    activate: {                  // beeps sequence when a function is activated
      begin: number;             // seconds
      end: number;               // seconds
      frequency: number;         // hertz
      type: 'sine' | 'square' | 'sawtooth' | 'triangle';
    }[],
    deactivate: {                // beeps sequence when a function is deactivates
      begin: number;             // seconds
      end: number;               // seconds
      frequency: number;         // hertz
      type: 'sine' | 'square' | 'sawtooth' | 'triangle';
    }[],
    denied: {                    // beeps sequence when a function is denied
      begin: number;             // seconds
      end: number;               // seconds
      frequency: number;         // hertz
      type: 'sine' | 'square' | 'sawtooth' | 'triangle';
    }[],
  };
}[]
```

**rdioScanner.options.keypadBeeps presets**

Setting `options.keypadBeeps` to `1` set the keypadBeeps to Uniden style. It is equivalent to:

``` json
"options": {
  "keypadBeeps": {
    "activate": [
      {
        "begin": 0,
        "end": 0.05,
        "frequency": 1200,
        "type": "square"
      }
    ],
    "deactivate": [
      {
        "begin": 0,
        "end": 0.1,
        "frequency": 1200,
        "type": "square"
      },
      {
        "begin": 0.1,
        "end": 0.2,
        "frequency": 925,
        "type": "square"
      }
    ],
    "denied": [
      {
        "begin": 0,
        "end": 0.05,
        "frequency": 925,
        "type": "square"
      },
      {
        "begin": 0.1,
        "end": 0.15,
        "frequency": 925,
        "type": "square"
      }
    ]
  }
}
```

Setting `options.keypadBeeps` to `2` set the keypadBeeps to Whistler style. It is equivalent to:

``` json
"options": {
  "keypadBeeps": {
    "activate": [
      {
        "begin": 0,
        "end": 0.05,
        "frequency": 2000,
        "type": "triangle"
      }
    ],
    "deactivate": [
      {
        "begin": 0,
        "end": 0.04,
        "frequency": 1500,
        "type": "triangle"
      },
      {
        "begin": 0.04,
        "end": 0.08,
        "frequency": 1400,
        "type": "triangle"
      }
    ],
    "denied": [
      {
        "begin": 0,
        "end": 0.04,
        "frequency": 1400,
        "type": "triangle"
      },
      {
        "begin": 0.05,
        "end": 0.09,
        "frequency": 1400,
        "type": "triangle"
      },
      {
        "begin": 0.1,
        "end": 0.14,
        "frequency": 1400,
        "type": "triangle"
      }
    ]
  }
}
```

## Systems - rdioScanner.systems

The heart of [Rdio Scanner](https://github.com/chuot/rdio-scanner) where all your systems and talkgroups are defined. Audio files to unknown systems / talkgroups will not be ingested.

**Definitions of the rdioScanner.systems object**

``` typescript
systems: {
  id: number;
  label: string;
  led?: 'blue' | 'cyan' | 'green' | 'magenta' | 'red' | 'white' | 'yellow';
  talkgroups: {
    id: number;
    label: string;
    name: string;
    patches: number[];
    tag: string;
    group?: string; // mandatory if useGroup is true, optional if not
    led?: 'blue' | 'cyan' | 'green' | 'magenta' | 'red' | 'white' | 'yellow';
  }[];
  units: {
    id: number;
    label: string;
  }[];
}[]
```

* **id** - System ID.
* **label** - System label shown on the left side of second row.
* **led** - Optional LED color for the whole system. By default the LED is *green*.
* **talkgroups** - Talkgroups for the system.
  + **id** - Talkgroup ID.
  + **label** - Talkgroup label shown on the left side of third row.
  + **name** - Talkgroup name shown on the fouth row.
  + **patches** - Array of talkgroup ID to include in this talkgroup.
  + **tag** - Talkgroup tag shown on the right side of second row.
* **units** - Unit aliases.
  + **id** - Unit ID.
  + **label** - Unit label shown on the right side of six sixth row.

## Load a system from RadioReference.com or talkgroups from Trunk Recorder CSV file

You may want to load your `server/config.json` with some systems from [RadioReference.com](https://radioreference.com/).

You can choose any `system_id` you want to refer to this system. If you choose an existing `system_id` in `config.json` , you will replace it with the new one.

See examples in the installation documents:

* [Install from the Docker Image](./install-docker.md)
* [Install from the GitHub Repository](./install-github.md)

> Note that [Rdio Scanner](https://github.com/chuot/rdio-scanner) has been designed to resemble old school radio scanners where each talkgroup has its own toggle button on the **SELECT TG** panel. Loading too many systems/talkgroups will make the web application very slow. Depending on your use case, you may want to have no more than 400 talkgroups.

Happy Rdio scanning !
