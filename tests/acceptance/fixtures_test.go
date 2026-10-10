// Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>
//
// WebSocket API Access Policy:
// This WebSocket API is reserved exclusively for Saubeo Solutions and its native applications.
// Unauthorized access is strictly prohibited.
// See API_ACCESS_POLICY.md for full terms.

package acceptance

import (
	"encoding/json"
	"testing"
)

// capturedRuns are the runs a dirwatch server left behind, recorded table by table as the legacy server
// wrote them and keyed by the name the test asks for them under. They live in the package that consumes
// them, which is what REQ-007 asks for: no fixture directory, no manifest, and nothing outside the test
// that proves it.
//
// The runs were captured from a legacy server with its time zone set to UTC, which is the time zone every
// capture in this repository was taken in.
//
// The calls, groups, tags, talkgroups, units and systems rows of these runs are the legacy tree's bytes:
// the server created the same configuration it always did. The log rows are not, because the logging
// subsystem gave this server sentences to write that the legacy server had none of: the reworded rows
// keep the legacy levels and order, and each new row sits where the change it reports happened. The
// watched directories are written as `<base>`, because the capture is of what the run did and not of
// which temporary folder the test happened to use.
var capturedRuns = map[string]string{
	"dirwatch-autopopulate.json": `{
  "calls": [
    {
      "audioBytes": 2092,
      "audioFilename": "first.wav",
      "audioMime": "audio/wav",
      "callId": 1,
      "siteRef": 0,
      "systemId": 1,
      "talkgroupId": 1,
      "timestamp": 1791288000000
    },
    {
      "audioBytes": 2092,
      "audioFilename": "second.wav",
      "audioMime": "audio/wav",
      "callId": 2,
      "siteRef": 0,
      "systemId": 1,
      "talkgroupId": 1,
      "timestamp": 1791288060000
    },
    {
      "audioBytes": 2092,
      "audioFilename": "third.wav",
      "audioMime": "audio/wav",
      "callId": 3,
      "siteRef": 0,
      "systemId": 2,
      "talkgroupId": 2,
      "timestamp": 1791288120000
    },
    {
      "audioBytes": 2048,
      "audioFilename": "120000_1514000_P25_0001.1234-5_1514000[Dispatch]_6789[Car 4].mp3",
      "audioMime": "audio/mpeg",
      "callId": 4,
      "siteRef": 0,
      "systemId": 3,
      "talkgroupId": 3,
      "timestamp": 1791288000000
    }
  ],
  "logs": [
    {
      "level": "warn",
      "message": "the server generated a new administrator signing secret, which ends every session that was open before it"
    },
    {
      "level": "info",
      "message": "the server is watching <base>/watch/dsd"
    },
    {
      "level": "info",
      "message": "the server is watching <base>/watch/trunk"
    },
    {
      "level": "info",
      "message": "the recording first.wav created the system \"Grand Metro\" (system 1)"
    },
    {
      "level": "info",
      "message": "the recording first.wav created the talkgroup \"1514000\" (talkgroup 1514000) on the system \"Grand Metro\""
    },
    {
      "level": "info",
      "message": "call from system 1 talkgroup 1514000 success, first.wav"
    },
    {
      "level": "info",
      "message": "the recording second.wav replaced the placeholder talkgroup 1514000 with \"Main\" on the system \"Grand Metro\""
    },
    {
      "level": "info",
      "message": "call from system 1 talkgroup 1514000 success, second.wav"
    },
    {
      "level": "info",
      "message": "the recording third.wav created the system \"Harbour Bridge\" (system 2)"
    },
    {
      "level": "info",
      "message": "the recording third.wav created the group \"Marine\""
    },
    {
      "level": "info",
      "message": "the recording third.wav created the tag \"Toll\""
    },
    {
      "level": "info",
      "message": "the recording third.wav created the talkgroup \"Harbour Toll\" (talkgroup 3001) on the system \"Harbour Bridge\""
    },
    {
      "level": "info",
      "message": "call from system 2 talkgroup 3001 success, third.wav"
    },
    {
      "level": "info",
      "message": "the recording 120000_1514000_P25_0001.1234-5_1514000[Dispatch]_6789[Car 4].mp3 created the system \"System 4660\" (system 4660)"
    },
    {
      "level": "info",
      "message": "the recording 120000_1514000_P25_0001.1234-5_1514000[Dispatch]_6789[Car 4].mp3 created the talkgroup \"Dispatch\" (talkgroup 1514000) on the system \"System 4660\""
    },
    {
      "level": "info",
      "message": "the recording 120000_1514000_P25_0001.1234-5_1514000[Dispatch]_6789[Car 4].mp3 added the unit \"Car 4\" (unit 6789) to the system \"System 4660\""
    },
    {
      "level": "info",
      "message": "call from system 4660 talkgroup 1514000 success, 120000_1514000_P25_0001.1234-5_1514000[Dispatch]_6789[Car 4].mp3"
    }
  ],
  "groups": [
    {
      "alert": "",
      "groupId": 1,
      "label": "Air",
      "led": "",
      "order": 0
    },
    {
      "alert": "",
      "groupId": 2,
      "label": "EMS",
      "led": "",
      "order": 0
    },
    {
      "alert": "",
      "groupId": 3,
      "label": "Fire",
      "led": "",
      "order": 0
    },
    {
      "alert": "",
      "groupId": 4,
      "label": "Interop",
      "led": "",
      "order": 0
    },
    {
      "alert": "",
      "groupId": 5,
      "label": "Law",
      "led": "",
      "order": 0
    },
    {
      "alert": "",
      "groupId": 6,
      "label": "Unknown",
      "led": "",
      "order": 0
    },
    {
      "alert": "",
      "groupId": 7,
      "label": "Marine",
      "led": "",
      "order": 0
    }
  ],
  "tags": [
    {
      "alert": "",
      "label": "Air Traffic Control",
      "led": "",
      "order": 0,
      "tagId": 1
    },
    {
      "alert": "",
      "label": "Emergency ",
      "led": "",
      "order": 0,
      "tagId": 2
    },
    {
      "alert": "",
      "label": "Fire Dispatch",
      "led": "",
      "order": 0,
      "tagId": 3
    },
    {
      "alert": "",
      "label": "Fire Tac",
      "led": "",
      "order": 0,
      "tagId": 4
    },
    {
      "alert": "",
      "label": "Fire Talk",
      "led": "",
      "order": 0,
      "tagId": 5
    },
    {
      "alert": "",
      "label": "Interop",
      "led": "",
      "order": 0,
      "tagId": 6
    },
    {
      "alert": "",
      "label": "Security",
      "led": "",
      "order": 0,
      "tagId": 7
    },
    {
      "alert": "",
      "label": "Service",
      "led": "",
      "order": 0,
      "tagId": 8
    },
    {
      "alert": "",
      "label": "Untagged",
      "led": "",
      "order": 0,
      "tagId": 9
    },
    {
      "alert": "",
      "label": "Toll",
      "led": "",
      "order": 0,
      "tagId": 10
    }
  ],
  "talkgroups": [
    {
      "alert": "",
      "delay": 0,
      "frequency": 0,
      "label": "Main",
      "led": "",
      "name": "Dispatch 1",
      "order": 0,
      "systemId": 1,
      "tagId": 9,
      "talkgroupId": 1,
      "talkgroupRef": 1514000,
      "type": ""
    },
    {
      "alert": "",
      "delay": 0,
      "frequency": 0,
      "label": "Harbour Toll",
      "led": "",
      "name": "Bridge Toll",
      "order": 0,
      "systemId": 2,
      "tagId": 10,
      "talkgroupId": 2,
      "talkgroupRef": 3001,
      "type": ""
    },
    {
      "alert": "",
      "delay": 0,
      "frequency": 0,
      "label": "Dispatch",
      "led": "",
      "name": "Talkgroup 1514000",
      "order": 0,
      "systemId": 3,
      "tagId": 9,
      "talkgroupId": 3,
      "talkgroupRef": 1514000,
      "type": ""
    }
  ],
  "units": [
    {
      "label": "Car 4",
      "order": 0,
      "systemId": 3,
      "unitFrom": 0,
      "unitId": 1,
      "unitRef": 6789,
      "unitTo": 0
    }
  ],
  "systems": [
    {
      "alert": "",
      "autoPopulate": 0,
      "blacklists": "",
      "delay": 0,
      "label": "Grand Metro",
      "led": "",
      "order": 0,
      "systemId": 1,
      "systemRef": 1,
      "type": ""
    },
    {
      "alert": "",
      "autoPopulate": 0,
      "blacklists": "",
      "delay": 0,
      "label": "Harbour Bridge",
      "led": "",
      "order": 0,
      "systemId": 2,
      "systemRef": 2,
      "type": ""
    },
    {
      "alert": "",
      "autoPopulate": 0,
      "blacklists": "",
      "delay": 0,
      "label": "System 4660",
      "led": "",
      "order": 0,
      "systemId": 3,
      "systemRef": 4660,
      "type": ""
    }
  ]
}
`,
	"dirwatch-end-to-end.json": `{
  "frames": [
    {
      "command": "CFG"
    },
    {
      "command": "LFM",
      "payload": true
    },
    {
      "command": "CAL",
      "payload": {
        "audioName": "call.wav",
        "audioType": "audio/wav",
        "dateTime": "2026-10-06T12:00:00Z",
        "delayed": false,
        "id": 1,
        "patches": [],
        "system": 10001,
        "talkgroup": 1514000,
        "talkgroupLabel": "Main",
        "talkgroupName": "Dispatch 1",
        "audioBytes": 2092
      }
    },
    {
      "command": "CFG"
    },
    {
      "command": "CAL",
      "payload": {
        "audioName": "120000_1514000_P25_0001.1234-5_1514000[Dispatch]_6789[Car 4].mp3",
        "audioType": "audio/mpeg",
        "dateTime": "2026-10-06T12:00:00Z",
        "delayed": false,
        "id": 2,
        "patches": [],
        "sources": [
          {
            "pos": 0,
            "src": 6789
          }
        ],
        "system": 4660,
        "talkgroup": 1514000,
        "talkgroupLabel": "Dispatch",
        "talkgroupName": "Talkgroup 1514000",
        "audioBytes": 2048
      }
    }
  ],
  "calls": [
    {
      "audioBytes": 2092,
      "audioFilename": "call.wav",
      "audioMime": "audio/wav",
      "callId": 1,
      "siteRef": 0,
      "systemId": 1,
      "talkgroupId": 1,
      "timestamp": 1791288000000
    },
    {
      "audioBytes": 2048,
      "audioFilename": "120000_1514000_P25_0001.1234-5_1514000[Dispatch]_6789[Car 4].mp3",
      "audioMime": "audio/mpeg",
      "callId": 2,
      "siteRef": 0,
      "systemId": 2,
      "talkgroupId": 2,
      "timestamp": 1791288000000
    }
  ],
  "logs": [
    {
      "level": "warn",
      "message": "the server generated a new administrator signing secret, which ends every session that was open before it"
    },
    {
      "level": "info",
      "message": "the server is watching <base>/watch/dsd"
    },
    {
      "level": "info",
      "message": "the server is watching <base>/watch/trunk"
    },
    {
      "level": "info",
      "message": "a listener joined from 127.0.0.1"
    },
    {
      "level": "info",
      "message": "call from system 10001 talkgroup 1514000 success, call.wav"
    },
    {
      "level": "info",
      "message": "the recording 120000_1514000_P25_0001.1234-5_1514000[Dispatch]_6789[Car 4].mp3 added the unit \"Car 4\" (unit 6789) to the system \"System 4660\""
    },
    {
      "level": "info",
      "message": "call from system 4660 talkgroup 1514000 success, 120000_1514000_P25_0001.1234-5_1514000[Dispatch]_6789[Car 4].mp3"
    },
    {
      "level": "info",
      "message": "there is 1 listener"
    },
    {
      "level": "info",
      "message": "a listener left from 127.0.0.1"
    }
  ],
  "groups": [
    {
      "alert": "",
      "groupId": 1,
      "label": "Air",
      "led": "",
      "order": 0
    },
    {
      "alert": "",
      "groupId": 2,
      "label": "EMS",
      "led": "",
      "order": 0
    },
    {
      "alert": "",
      "groupId": 3,
      "label": "Fire",
      "led": "",
      "order": 0
    },
    {
      "alert": "",
      "groupId": 4,
      "label": "Interop",
      "led": "",
      "order": 0
    },
    {
      "alert": "",
      "groupId": 5,
      "label": "Law",
      "led": "",
      "order": 0
    },
    {
      "alert": "",
      "groupId": 6,
      "label": "Unknown",
      "led": "",
      "order": 0
    }
  ],
  "tags": [
    {
      "alert": "",
      "label": "Air Traffic Control",
      "led": "",
      "order": 0,
      "tagId": 1
    },
    {
      "alert": "",
      "label": "Emergency ",
      "led": "",
      "order": 0,
      "tagId": 2
    },
    {
      "alert": "",
      "label": "Fire Dispatch",
      "led": "",
      "order": 0,
      "tagId": 3
    },
    {
      "alert": "",
      "label": "Fire Tac",
      "led": "",
      "order": 0,
      "tagId": 4
    },
    {
      "alert": "",
      "label": "Fire Talk",
      "led": "",
      "order": 0,
      "tagId": 5
    },
    {
      "alert": "",
      "label": "Interop",
      "led": "",
      "order": 0,
      "tagId": 6
    },
    {
      "alert": "",
      "label": "Security",
      "led": "",
      "order": 0,
      "tagId": 7
    },
    {
      "alert": "",
      "label": "Service",
      "led": "",
      "order": 0,
      "tagId": 8
    },
    {
      "alert": "",
      "label": "Untagged",
      "led": "",
      "order": 0,
      "tagId": 9
    }
  ],
  "talkgroups": [
    {
      "alert": "",
      "delay": 0,
      "frequency": 0,
      "label": "Main",
      "led": "",
      "name": "Dispatch 1",
      "order": 0,
      "systemId": 1,
      "tagId": 9,
      "talkgroupId": 1,
      "talkgroupRef": 1514000,
      "type": ""
    },
    {
      "alert": "",
      "delay": 0,
      "frequency": 0,
      "label": "Dispatch",
      "led": "",
      "name": "Talkgroup 1514000",
      "order": 0,
      "systemId": 2,
      "tagId": 9,
      "talkgroupId": 2,
      "talkgroupRef": 1514000,
      "type": ""
    }
  ],
  "units": [
    {
      "label": "Car 4",
      "order": 0,
      "systemId": 2,
      "unitFrom": 0,
      "unitId": 1,
      "unitRef": 6789,
      "unitTo": 0
    }
  ],
  "systems": [
    {
      "alert": "",
      "autoPopulate": 0,
      "blacklists": "",
      "delay": 0,
      "label": "Grand Metro",
      "led": "",
      "order": 0,
      "systemId": 1,
      "systemRef": 10001,
      "type": ""
    },
    {
      "alert": "",
      "autoPopulate": 0,
      "blacklists": "",
      "delay": 0,
      "label": "System 4660",
      "led": "",
      "order": 0,
      "systemId": 2,
      "systemRef": 4660,
      "type": ""
    }
  ]
}
`,
}

// capturedRun hands back the recorded run under the name a test asks for it under, keeping each table as
// it was written so that it can be compared in the same canonical shape the server's own rows are put in,
// and stops when that name was never recorded: a test that asks for a capture this package does not hold
// has changed, and it would rather stop than quietly compare against nothing.
func capturedRun(t *testing.T, name string) map[string]json.RawMessage {
	t.Helper()

	body, ok := capturedRuns[name]
	if !ok {
		t.Fatalf("the captured run %s is not recorded in this package", name)
	}

	run := map[string]json.RawMessage{}

	if err := json.Unmarshal([]byte(body), &run); err != nil {
		t.Fatalf("the captured run %s is not the run a capture writes: %v", name, err)
	}

	for _, table := range []string{"calls", "logs", "groups", "tags", "talkgroups", "units", "systems"} {
		if _, ok := run[table]; !ok {
			t.Fatalf("the captured run %s has no %s to compare against", name, table)
		}
	}

	return run
}

// capturedConfigFixture is the configuration the recorded runs install, as the bytes the
// capture installed it with. The administrator calls the capture made, in the same order, so
// that the configuration this server answers in is the configuration the captured answers
// were recorded in.
const capturedConfigFixture = `{
  "access": [],
  "apikeys": [
    {
      "id": 0,
      "disabled": false,
      "ident": "Capture open",
      "key": "capture-apikey-open",
      "systems": "*"
    },
    {
      "id": 0,
      "disabled": false,
      "ident": "Capture scoped",
      "key": "capture-apikey-scoped",
      "systems": [
        {
          "id": 10001,
          "talkgroups": "*"
        }
      ]
    }
  ],
  "dirwatch": [
    {
      "id": 0,
      "delay": 1000,
      "deleteAfter": true,
      "directory": "/tmp/opencode/capture-watch",
      "disabled": true,
      "extension": ".wav",
      "frequency": 151400000,
      "mask": "#S#TG#YYYY-MM-DDTHH-mm-ss",
      "order": 1,
      "siteId": 1,
      "systemId": 1,
      "talkgroupId": 1,
      "type": "default"
    }
  ],
  "downstreams": [
    {
      "id": 0,
      "apikey": "capture-apikey-open",
      "disabled": true,
      "order": 1,
      "systems": [
        {
          "id": 10001,
          "talkgroups": [1514000]
        }
      ],
      "url": "http://127.0.0.1:39999/api/call-upload"
    }
  ],
  "groups": [
    {
      "id": 1,
      "label": "Air"
    },
    {
      "id": 2,
      "label": "EMS"
    },
    {
      "id": 3,
      "label": "Fire"
    },
    {
      "id": 4,
      "label": "Interop"
    },
    {
      "id": 5,
      "label": "Law"
    },
    {
      "id": 6,
      "label": "Unknown"
    },
    {
      "id": 0,
      "label": "Ground"
    }
  ],
  "options": {
    "audioConversion": 0,
    "autoPopulate": false,
    "branding": "Capture Dispatch",
    "dimmerDelay": 5000,
    "disableDuplicateDetection": true,
    "duplicateDetectionTimeFrame": 500,
    "email": "dispatch@example.com",
    "keypadBeeps": "uniden",
    "maxClients": 200,
    "playbackGoesLive": true,
    "pruneDays": 7,
    "showListenersCount": true,
    "sortTalkgroups": true,
    "time12hFormat": true
  },
  "systems": [
    {
      "id": 0,
      "autoPopulate": false,
      "label": "Grand Metro",
      "order": 1,
      "sites": [
        {
          "id": 0,
          "label": "Metro East",
          "siteRef": 101
        },
        {
          "id": 0,
          "label": "Metro West",
          "siteRef": 102
        }
      ],
      "systemRef": 10001,
      "talkgroups": [
        {
          "id": 0,
          "frequency": 151400000,
          "groupIds": [
            7
          ],
          "label": "Ground",
          "name": "Dispatch 1",
          "tagId": 3,
          "talkgroupRef": 1514000,
          "type": "Aggregate"
        },
        {
          "id": 0,
          "groupIds": [
            7,
            3
          ],
          "label": "Ground",
          "name": "Dispatch 2",
          "tagId": 4,
          "talkgroupRef": 1514100
        },
        {
          "id": 0,
          "groupIds": [
            1
          ],
          "label": "Air",
          "name": "Air 1",
          "tagId": 1,
          "talkgroupRef": 1514200
        }
      ],
      "units": [
        {
          "id": 0,
          "label": "Car 1",
          "unitRef": 1001
        },
        {
          "id": 0,
          "label": "Car 2",
          "unitRef": 1002
        }
      ]
    },
    {
      "id": 0,
      "autoPopulate": false,
      "label": "Harbour",
      "order": 2,
      "sites": [],
      "systemRef": 10002,
      "talkgroups": [
        {
          "id": 0,
          "groupIds": [
            6
          ],
          "label": "Marine",
          "name": "Marine 16",
          "tagId": 9,
          "talkgroupRef": 1515000
        }
      ],
      "units": []
    }
  ],
  "tags": [
    {
      "id": 1,
      "label": "Air Traffic Control"
    },
    {
      "id": 2,
      "label": "Emergency "
    },
    {
      "id": 3,
      "label": "Fire Dispatch"
    },
    {
      "id": 4,
      "label": "Fire Tac"
    },
    {
      "id": 5,
      "label": "Fire Talk"
    },
    {
      "id": 6,
      "label": "Interop"
    },
    {
      "id": 7,
      "label": "Security"
    },
    {
      "id": 8,
      "label": "Service"
    },
    {
      "id": 9,
      "label": "Untagged"
    },
    {
      "id": 0,
      "label": "Capture Tag"
    }
  ]
}
`

// restrictedFrames are the frames the legacy server wrote during its restricted phase, in the
// order it wrote them.
var restrictedFrames = struct {
	scoped  []string
	expired []string
	limited [][]string
}{
	scoped: []string{
		`["VER",{"branding":"Capture Dispatch","email":"dispatch@example.com","version":"8.0.0"}]`,
		`["PIN"]`,
		`["PIN"]`,
		`["PIN"]`,
		`["PIN"]`,
		`["PIN"]`,
		`["CFG",{"alerts":{"alert1":[{"begin":0,"end":0.05,"frequency":3000,"type":"square"},{"begin":0.075,"end":0.125,"frequency":3000,"type":"square"},{"begin":0.15,"end":0.2,"frequency":3000,"type":"square"}],"alert2":[{"begin":0,"end":0.05,"frequency":800,"type":"square"},{"begin":0.075,"end":0.125,"frequency":1000,"type":"square"},{"begin":0.15,"end":0.2,"frequency":800,"type":"square"},{"begin":0.225,"end":0.275,"frequency":1000,"type":"square"}],"alert3":[{"begin":0,"end":0.05,"frequency":800,"type":"square"},{"begin":0.075,"end":0.125,"frequency":1000,"type":"square"},{"begin":0.15,"end":0.175,"frequency":3000,"type":"square"},{"begin":0.225,"end":0.275,"frequency":3000,"type":"square"}],"alert4":[{"begin":0,"end":0.125,"frequency":800,"type":"square"},{"begin":0.125,"end":0.25,"frequency":1000,"type":"square"},{"begin":0.375,"end":0.5,"frequency":800,"type":"square"},{"begin":0.5,"end":0.625,"frequency":1000,"type":"square"},{"begin":0.75,"end":0.875,"frequency":800,"type":"square"},{"begin":0.875,"end":1,"frequency":1000,"type":"square"},{"begin":1.125,"end":1.25,"frequency":800,"type":"square"},{"begin":1.25,"end":1.325,"frequency":1000,"type":"square"}],"alert5":[{"begin":0,"end":1.5,"frequency":1200,"type":"square"}],"alert6":[{"begin":0,"end":0.175,"frequency":1200,"type":"square"},{"begin":0.225,"end":0.3,"frequency":1200,"type":"square"}],"alert7":[{"begin":0,"end":0.2,"frequency":2000,"type":"square"},{"begin":0.2,"end":0.4,"frequency":800,"type":"square"},{"begin":0.4,"end":0.6,"frequency":2000,"type":"square"},{"begin":0.6,"end":0.8,"frequency":800,"type":"square"},{"begin":0.8,"end":1,"frequency":2000,"type":"square"},{"begin":1,"end":1.2,"frequency":800,"type":"square"}],"alert8":[{"begin":0,"end":0.03,"frequency":500,"type":"square"},{"begin":0.04,"end":0.07,"frequency":500,"type":"square"},{"begin":0.08,"end":0.11,"frequency":500,"type":"square"}],"alert9":[{"begin":0,"end":0.07,"frequency":2400,"type":"square"},{"begin":0.09,"end":0.16,"frequency":3000,"type":"square"},{"begin":0.27,"end":0.34,"frequency":2400,"type":"square"},{"begin":0.37,"end":0.44,"frequency":3000,"type":"square"}]},"branding":"Capture Dispatch","dimmerDelay":5000,"email":"dispatch@example.com","groups":{"Air":{"10001":[1514200]},"Fire":{"10001":[1514100]},"Ground":{"10001":[1514000,1514100]}},"groupsData":[{"id":1,"label":"Air"},{"id":7,"label":"Ground"},{"id":3,"label":"Fire"}],"keypadBeeps":{"activate":[{"begin":0,"end":0.05,"frequency":1200,"type":"square"}],"deactivate":[{"begin":0,"end":0.1,"frequency":1200,"type":"square"},{"begin":0.1,"end":0.2,"frequency":925,"type":"square"}],"denied":[{"begin":0,"end":0.05,"frequency":925,"type":"square"},{"begin":0.1,"end":0.15,"frequency":925,"type":"square"}]},"playbackGoesLive":true,"showListenersCount":false,"systems":[{"alert":"","id":10001,"label":"Grand Metro","led":"","order":1,"talkgroups":[{"alert":"","frequency":0,"group":"Air","groups":["Air"],"id":1514200,"label":"Air","led":"","name":"Air 1","order":1,"tag":"Air Traffic Control","type":""},{"alert":"","frequency":151400000,"group":"Ground","groups":["Ground"],"id":1514000,"label":"Ground","led":"","name":"Dispatch 1","order":2,"tag":"Fire Dispatch","type":"Aggregate"},{"alert":"","frequency":0,"group":"Fire","groups":["Fire","Ground"],"id":1514100,"label":"Ground","led":"","name":"Dispatch 2","order":3,"tag":"Fire Tac","type":""}],"type":"","units":[{"id":1001,"label":"Car 1","unitRef":1001},{"id":1002,"label":"Car 2","unitRef":1002}]}],"tags":{"Air Traffic Control":{"10001":[1514200]},"Fire Dispatch":{"10001":[1514000]},"Fire Tac":{"10001":[1514100]}},"tagsData":[{"id":1,"label":"Air Traffic Control"},{"id":3,"label":"Fire Dispatch"},{"id":4,"label":"Fire Tac"}],"time12hFormat":true}]`,
		`["CAL",{"audio":{"data":[82,73,70,70,36,0,0,0,87,65,86,69,102,109,116,32,16,0,0,0,1,0,1,0,64,31,0,0,128,62,0,0,2,0,16,0,100,97,116,97,0,0,0,0,97,32,114,101,99,111,114,100,105,110,103,32,108,111,110,103,32,101,110,111,117,103,104,32,116,111,32,98,101,32,97,32,114,101,99,111,114,100,105,110,103,44,32,97,110,100,32,116,104,101,110,32,115,111,109,101,32,109,111,114,101,32,111,102,32,105,116],"type":"Buffer"},"audioName":"capture.wav","audioType":"","dateTime":"2026-10-06T12:04:00Z","delayed":false,"id":4,"patches":null,"system":10001,"talkgroup":1514000,"talkgroupLabel":"Ground","talkgroupName":"Dispatch 1"},"d"]`,
		`["PIN"]`,
	},
	expired: []string{
		`["XPR"]`,
	},
	limited: [][]string{
		{
			`["CFG",{"alerts":{"alert1":[{"begin":0,"end":0.05,"frequency":3000,"type":"square"},{"begin":0.075,"end":0.125,"frequency":3000,"type":"square"},{"begin":0.15,"end":0.2,"frequency":3000,"type":"square"}],"alert2":[{"begin":0,"end":0.05,"frequency":800,"type":"square"},{"begin":0.075,"end":0.125,"frequency":1000,"type":"square"},{"begin":0.15,"end":0.2,"frequency":800,"type":"square"},{"begin":0.225,"end":0.275,"frequency":1000,"type":"square"}],"alert3":[{"begin":0,"end":0.05,"frequency":800,"type":"square"},{"begin":0.075,"end":0.125,"frequency":1000,"type":"square"},{"begin":0.15,"end":0.175,"frequency":3000,"type":"square"},{"begin":0.225,"end":0.275,"frequency":3000,"type":"square"}],"alert4":[{"begin":0,"end":0.125,"frequency":800,"type":"square"},{"begin":0.125,"end":0.25,"frequency":1000,"type":"square"},{"begin":0.375,"end":0.5,"frequency":800,"type":"square"},{"begin":0.5,"end":0.625,"frequency":1000,"type":"square"},{"begin":0.75,"end":0.875,"frequency":800,"type":"square"},{"begin":0.875,"end":1,"frequency":1000,"type":"square"},{"begin":1.125,"end":1.25,"frequency":800,"type":"square"},{"begin":1.25,"end":1.325,"frequency":1000,"type":"square"}],"alert5":[{"begin":0,"end":1.5,"frequency":1200,"type":"square"}],"alert6":[{"begin":0,"end":0.175,"frequency":1200,"type":"square"},{"begin":0.225,"end":0.3,"frequency":1200,"type":"square"}],"alert7":[{"begin":0,"end":0.2,"frequency":2000,"type":"square"},{"begin":0.2,"end":0.4,"frequency":800,"type":"square"},{"begin":0.4,"end":0.6,"frequency":2000,"type":"square"},{"begin":0.6,"end":0.8,"frequency":800,"type":"square"},{"begin":0.8,"end":1,"frequency":2000,"type":"square"},{"begin":1,"end":1.2,"frequency":800,"type":"square"}],"alert8":[{"begin":0,"end":0.03,"frequency":500,"type":"square"},{"begin":0.04,"end":0.07,"frequency":500,"type":"square"},{"begin":0.08,"end":0.11,"frequency":500,"type":"square"}],"alert9":[{"begin":0,"end":0.07,"frequency":2400,"type":"square"},{"begin":0.09,"end":0.16,"frequency":3000,"type":"square"},{"begin":0.27,"end":0.34,"frequency":2400,"type":"square"},{"begin":0.37,"end":0.44,"frequency":3000,"type":"square"}]},"branding":"Capture Dispatch","dimmerDelay":5000,"email":"dispatch@example.com","groups":{"Air":{"10001":[1514200]},"Fire":{"10001":[1514100]},"Ground":{"10001":[1514000,1514100]},"Unknown":{"10002":[1515000]}},"groupsData":[{"id":1,"label":"Air"},{"id":7,"label":"Ground"},{"id":3,"label":"Fire"},{"id":6,"label":"Unknown"}],"keypadBeeps":{"activate":[{"begin":0,"end":0.05,"frequency":1200,"type":"square"}],"deactivate":[{"begin":0,"end":0.1,"frequency":1200,"type":"square"},{"begin":0.1,"end":0.2,"frequency":925,"type":"square"}],"denied":[{"begin":0,"end":0.05,"frequency":925,"type":"square"},{"begin":0.1,"end":0.15,"frequency":925,"type":"square"}]},"playbackGoesLive":true,"showListenersCount":false,"systems":[{"alert":"","id":10001,"label":"Grand Metro","led":"","order":1,"talkgroups":[{"alert":"","frequency":0,"group":"Air","groups":["Air"],"id":1514200,"label":"Air","led":"","name":"Air 1","order":1,"tag":"Air Traffic Control","type":""},{"alert":"","frequency":151400000,"group":"Ground","groups":["Ground"],"id":1514000,"label":"Ground","led":"","name":"Dispatch 1","order":2,"tag":"Fire Dispatch","type":"Aggregate"},{"alert":"","frequency":0,"group":"Fire","groups":["Fire","Ground"],"id":1514100,"label":"Ground","led":"","name":"Dispatch 2","order":3,"tag":"Fire Tac","type":""}],"type":"","units":[{"id":1001,"label":"Car 1","unitRef":1001},{"id":1002,"label":"Car 2","unitRef":1002}]},{"alert":"","id":10002,"label":"Harbour","led":"","order":2,"talkgroups":[{"alert":"","frequency":0,"group":"Unknown","groups":["Unknown"],"id":1515000,"label":"Marine","led":"","name":"Marine 16","order":1,"tag":"Untagged","type":""}],"type":"","units":[]}],"tags":{"Air Traffic Control":{"10001":[1514200]},"Fire Dispatch":{"10001":[1514000]},"Fire Tac":{"10001":[1514100]},"Untagged":{"10002":[1515000]}},"tagsData":[{"id":1,"label":"Air Traffic Control"},{"id":3,"label":"Fire Dispatch"},{"id":4,"label":"Fire Tac"},{"id":9,"label":"Untagged"}],"time12hFormat":true}]`,
		},
		{
			`["CFG",{"alerts":{"alert1":[{"begin":0,"end":0.05,"frequency":3000,"type":"square"},{"begin":0.075,"end":0.125,"frequency":3000,"type":"square"},{"begin":0.15,"end":0.2,"frequency":3000,"type":"square"}],"alert2":[{"begin":0,"end":0.05,"frequency":800,"type":"square"},{"begin":0.075,"end":0.125,"frequency":1000,"type":"square"},{"begin":0.15,"end":0.2,"frequency":800,"type":"square"},{"begin":0.225,"end":0.275,"frequency":1000,"type":"square"}],"alert3":[{"begin":0,"end":0.05,"frequency":800,"type":"square"},{"begin":0.075,"end":0.125,"frequency":1000,"type":"square"},{"begin":0.15,"end":0.175,"frequency":3000,"type":"square"},{"begin":0.225,"end":0.275,"frequency":3000,"type":"square"}],"alert4":[{"begin":0,"end":0.125,"frequency":800,"type":"square"},{"begin":0.125,"end":0.25,"frequency":1000,"type":"square"},{"begin":0.375,"end":0.5,"frequency":800,"type":"square"},{"begin":0.5,"end":0.625,"frequency":1000,"type":"square"},{"begin":0.75,"end":0.875,"frequency":800,"type":"square"},{"begin":0.875,"end":1,"frequency":1000,"type":"square"},{"begin":1.125,"end":1.25,"frequency":800,"type":"square"},{"begin":1.25,"end":1.325,"frequency":1000,"type":"square"}],"alert5":[{"begin":0,"end":1.5,"frequency":1200,"type":"square"}],"alert6":[{"begin":0,"end":0.175,"frequency":1200,"type":"square"},{"begin":0.225,"end":0.3,"frequency":1200,"type":"square"}],"alert7":[{"begin":0,"end":0.2,"frequency":2000,"type":"square"},{"begin":0.2,"end":0.4,"frequency":800,"type":"square"},{"begin":0.4,"end":0.6,"frequency":2000,"type":"square"},{"begin":0.6,"end":0.8,"frequency":800,"type":"square"},{"begin":0.8,"end":1,"frequency":2000,"type":"square"},{"begin":1,"end":1.2,"frequency":800,"type":"square"}],"alert8":[{"begin":0,"end":0.03,"frequency":500,"type":"square"},{"begin":0.04,"end":0.07,"frequency":500,"type":"square"},{"begin":0.08,"end":0.11,"frequency":500,"type":"square"}],"alert9":[{"begin":0,"end":0.07,"frequency":2400,"type":"square"},{"begin":0.09,"end":0.16,"frequency":3000,"type":"square"},{"begin":0.27,"end":0.34,"frequency":2400,"type":"square"},{"begin":0.37,"end":0.44,"frequency":3000,"type":"square"}]},"branding":"Capture Dispatch","dimmerDelay":5000,"email":"dispatch@example.com","groups":{"Air":{"10001":[1514200]},"Fire":{"10001":[1514100]},"Ground":{"10001":[1514000,1514100]},"Unknown":{"10002":[1515000]}},"groupsData":[{"id":1,"label":"Air"},{"id":7,"label":"Ground"},{"id":3,"label":"Fire"},{"id":6,"label":"Unknown"}],"keypadBeeps":{"activate":[{"begin":0,"end":0.05,"frequency":1200,"type":"square"}],"deactivate":[{"begin":0,"end":0.1,"frequency":1200,"type":"square"},{"begin":0.1,"end":0.2,"frequency":925,"type":"square"}],"denied":[{"begin":0,"end":0.05,"frequency":925,"type":"square"},{"begin":0.1,"end":0.15,"frequency":925,"type":"square"}]},"playbackGoesLive":true,"showListenersCount":false,"systems":[{"alert":"","id":10001,"label":"Grand Metro","led":"","order":1,"talkgroups":[{"alert":"","frequency":0,"group":"Air","groups":["Air"],"id":1514200,"label":"Air","led":"","name":"Air 1","order":1,"tag":"Air Traffic Control","type":""},{"alert":"","frequency":151400000,"group":"Ground","groups":["Ground"],"id":1514000,"label":"Ground","led":"","name":"Dispatch 1","order":2,"tag":"Fire Dispatch","type":"Aggregate"},{"alert":"","frequency":0,"group":"Fire","groups":["Fire","Ground"],"id":1514100,"label":"Ground","led":"","name":"Dispatch 2","order":3,"tag":"Fire Tac","type":""}],"type":"","units":[{"id":1001,"label":"Car 1","unitRef":1001},{"id":1002,"label":"Car 2","unitRef":1002}]},{"alert":"","id":10002,"label":"Harbour","led":"","order":2,"talkgroups":[{"alert":"","frequency":0,"group":"Unknown","groups":["Unknown"],"id":1515000,"label":"Marine","led":"","name":"Marine 16","order":1,"tag":"Untagged","type":""}],"type":"","units":[]}],"tags":{"Air Traffic Control":{"10001":[1514200]},"Fire Dispatch":{"10001":[1514000]},"Fire Tac":{"10001":[1514100]},"Untagged":{"10002":[1515000]}},"tagsData":[{"id":1,"label":"Air Traffic Control"},{"id":3,"label":"Fire Dispatch"},{"id":4,"label":"Fire Tac"},{"id":9,"label":"Untagged"}],"time12hFormat":true}]`,
		},
		{
			`["MAX"]`,
		},
	},
}

// lockedFrames are the answers the legacy server gave to five access-code attempts on one
// connection, in the order it gave them.
var lockedFrames = []string{
	`["PIN"]`,
	`["PIN"]`,
	`["PIN"]`,
	`["PIN"]`,
	`["PIN"]`,
}

// capturedRestConfig is the configuration answer the legacy server gave an administrator, as
// the bytes it wrote.
const capturedRestConfig = `{"config":{"access":[],"apikeys":[{"disabled":false,"id":1,"ident":"Capture open","key":"capture-apikey-open","systems":"*"},{"disabled":false,"id":2,"ident":"Capture scoped","key":"capture-apikey-scoped","systems":[{"id":10001,"talkgroups":"*"}]}],"dirwatch":[{"delay":1000,"deleteAfter":true,"directory":"/tmp/opencode/capture-watch","disabled":true,"extension":".wav","frequency":151400000,"id":1,"mask":"#S#TG#YYYY-MM-DDTHH-mm-ss","order":1,"siteId":1,"systemId":1,"talkgroupId":1,"type":"default"}],"downstreams":[{"apikey":"capture-apikey-open","disabled":true,"id":1,"order":1,"systems":[{"id":10001,"talkgroups":[1514000]}],"url":"http://127.0.0.1:39999/api/call-upload"}],"groups":[{"id":1,"label":"Air"},{"id":2,"label":"EMS"},{"id":3,"label":"Fire"},{"id":4,"label":"Interop"},{"id":5,"label":"Law"},{"id":6,"label":"Unknown"},{"id":7,"label":"Ground"}],"options":{"audioConversion":0,"autoPopulate":false,"branding":"Capture Dispatch","dimmerDelay":5000,"disableDuplicateDetection":true,"duplicateDetectionTimeFrame":500,"email":"dispatch@example.com","keypadBeeps":"uniden","maxClients":200,"playbackGoesLive":true,"pruneDays":7,"showListenersCount":true,"sortTalkgroups":true,"time12hFormat":true},"systems":[{"autoPopulate":false,"id":1,"label":"Grand Metro","order":1,"sites":[{"id":1,"label":"Metro East","siteRef":101,"systemId":0},{"id":2,"label":"Metro West","siteRef":102,"systemId":0}],"systemRef":10001,"talkgroups":[{"frequency":151400000,"groupIds":[7],"id":1,"label":"Ground","name":"Dispatch 1","tagId":3,"talkgroupRef":1514000,"type":"Aggregate"},{"groupIds":[3,7],"id":2,"label":"Ground","name":"Dispatch 2","tagId":4,"talkgroupRef":1514100},{"groupIds":[1],"id":3,"label":"Air","name":"Air 1","tagId":1,"talkgroupRef":1514200}],"units":[{"id":1001,"label":"Car 1","unitRef":1001},{"id":1002,"label":"Car 2","unitRef":1002}]},{"autoPopulate":false,"id":2,"label":"Harbour","order":2,"sites":[],"systemRef":10002,"talkgroups":[{"groupIds":[6],"id":4,"label":"Marine","name":"Marine 16","tagId":9,"talkgroupRef":1515000}],"units":[]}],"tags":[{"id":1,"label":"Air Traffic Control"},{"id":2,"label":"Emergency "},{"id":3,"label":"Fire Dispatch"},{"id":4,"label":"Fire Tac"},{"id":5,"label":"Fire Talk"},{"id":6,"label":"Interop"},{"id":7,"label":"Security"},{"id":8,"label":"Service"},{"id":9,"label":"Untagged"},{"id":10,"label":"Capture Tag"}],"version":"8.0.0"},"passwordNeedChange":true}`
