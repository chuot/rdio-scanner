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

package main

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
