#!/usr/bin/env bash
#
# Regenerates testdata/legacydb/rdio-scanner.db — the SQLite database the persistence
# integration tests read, written entirely by the legacy server.
#
# The fixture is committed so the tests are hermetic. Run this script only when the fixture
# itself has to change, and from a checkout of the branch the legacy server is on:
#
#   server-ng/testdata/legacydb/generate.sh
#
# What it does, in order, all through the legacy server's own code paths:
#
#   1. builds the legacy server from server/
#   2. starts it against a fresh database file, which runs the legacy migrate + seed
#   3. logs in with the initial administrator password
#   4. saves a configuration that covers every configuration table
#   5. uploads a call through /api/call-upload with an apikey, which writes the call,
#      its frequencies, units, and patch, and delays one of them into the delayed table
#   6. stops the server and copies the database here
#
# Nothing writes to the database by any other route, so what the tests read is what the
# legacy server wrote.
#
# Two things about the payload are not preferences, and are worth knowing before changing it:
#
#   - The apikey used for the uploads is scoped to `"*"`. An apikey configured with an explicit
#     system list makes the legacy upload endpoint dereference a call aggregate that has not been
#     resolved yet, which takes the request down with an empty reply and never ingests the call.
#     Recorded as Q20 in
#     docs/specs/FEAT-003-server-ng-refactor/decision-preserved-quirks.md. A scoped apikey is
#     still saved, because the read tests have to read that shape too — it just cannot be used to
#     upload.
#   - `autoPopulate` is read as a JSON boolean and a `1` is silently ignored, which is how the
#     legacy server reads every boolean it accepts from this payload.
#   - Every talkgroup names a `tagId` that exists. `talkgroups.tagId` is a foreign key, and one
#     talkgroup that fails discards the whole systems section: the save is one transaction, the
#     endpoint still answers 200, and the failure is only visible in the process log — because
#     that log line names the failed statement, whose quotes break the insert into the `logs`
#     table (Q21).

set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../../.." && pwd)"

port="${PORT:-3111}"

work="$(mktemp -d)"
spool="$work/spool"

mkdir -p "$spool"

cleanup() {
	if [[ -n "${server:-}" ]] && kill -0 "$server" 2>/dev/null; then
		kill "$server" 2>/dev/null || true
		wait "$server" 2>/dev/null || true
	fi

	rm -rf "$work"
}

trap cleanup EXIT INT TERM

echo "building the legacy server from $repo/server"

(cd "$repo/server" && go build -o "$work/rdio-scanner" .)

echo "starting it on 127.0.0.1:$port against a fresh database"

"$work/rdio-scanner" \
	-db_file "$work/rdio-scanner.db" \
	-listen "127.0.0.1:$port" \
	> "$work/server.log" 2>&1 &

server=$!

for _ in {1..100}; do
	if curl -sS -o /dev/null "http://127.0.0.1:$port/api/admin/login"; then
		break
	fi

	sleep 0.1
done

base="http://127.0.0.1:$port"

token=$(
	curl -sS -X POST -H 'Content-Type: application/json' \
		-d '{"password":"rdio-scanner"}' \
		"$base/api/admin/login" |
		sed -n 's/.*"token":"\([^"]*\)".*/\1/p'
)

if [[ -z "$token" ]]; then
	echo "could not log in to the legacy server" >&2
	cat "$work/server.log" >&2
	exit 1
fi

echo "saving a configuration that reaches every configuration table"

cat > "$work/config.json" <<EOF
{
	"access": [
		{
			"code": "dispatch-code",
			"expiration": "2030-01-01T00:00:00Z",
			"ident": "dispatch",
			"limit": 2,
			"order": 1,
			"systems": [{"id": 10001, "talkgroups": [1514000, 1514001]}]
		},
		{
			"code": "harbour-code",
			"expiration": "2030-01-02T00:00:00Z",
			"ident": "harbour",
			"limit": 0,
			"order": 2,
			"systems": "*"
		}
	],
	"apikeys": [
		{
			"disabled": false,
			"ident": "scanner-key",
			"key": "fixture-apikey-0123456789",
			"order": 1,
			"systems": "*"
		},
		{
			"disabled": false,
			"ident": "scoped-key",
			"key": "fixture-apikey-scoped",
			"order": 2,
			"systems": [{"id": 10001, "talkgroups": [1514000]}]
		}
	],
	"dirwatch": [
		{
			"delay": 2000,
			"deleteAfter": false,
			"directory": "$spool",
			"disabled": false,
			"extension": ".wav",
			"frequency": 0,
			"mask": "",
			"order": 1,
			"siteId": 0,
			"systemId": 0,
			"talkgroupId": 0,
			"type": "dsdplus"
		}
	],
	"downstreams": [
		{
			"apikey": "fixture-apikey-0123456789",
			"disabled": true,
			"order": 1,
			"systems": [{"id": 10002, "talkgroups": [1520000]}],
			"url": "http://127.0.0.1:3999/api/call-upload"
		}
	],
	"groups": [
		{"id": 1, "label": "Air"},
		{"id": 2, "label": "EMS"},
		{"id": 3, "label": "Fire", "alert": "alert1", "led": "red"},
		{"id": 4, "label": "Interop"},
		{"id": 5, "label": "Law"},
		{"id": 6, "label": "Unknown"}
	],
	"options": {
		"audioConversion": 1,
		"autoPopulate": true,
		"branding": "Fixture Dispatch",
		"dimmerDelay": 5000,
		"disableDuplicateDetection": false,
		"duplicateDetectionTimeFrame": 750,
		"email": "dispatch@example.com",
		"keypadBeeps": "whistler",
		"maxClients": 200,
		"playbackGoesLive": false,
		"pruneDays": 7,
		"showListenersCount": true,
		"sortTalkgroups": false,
		"time12hFormat": true
	},
	"systems": [
		{
			"autoPopulate": true,
			"blacklists": "40000,40001",
			"delay": 0,
			"label": "Grand Metro",
			"order": 1,
			"sites": [{"label": "North Site", "order": 1, "siteRef": 77}],
			"systemRef": 10001,
			"talkgroups": [
				{
					"frequency": 1514000000,
					"groupIds": [1, 5],
					"label": "Dispatch 1",
					"name": "Dispatch 1",
					"order": 1,
					"tagId": 3,
					"talkgroupRef": 1514000,
					"type": "audio"
				},
				{
					"groupIds": [4],
					"label": "Interop 9",
					"name": "Interop 9",
					"order": 2,
					"tagId": 6,
					"talkgroupRef": 1514001,
					"type": "audio"
				},
				{
					"delay": 90,
					"groupIds": [],
					"label": "Delayed Talk",
					"name": "Delayed Talk",
					"order": 3,
					"tagId": 6,
					"talkgroupRef": 1514002,
					"type": "audio"
				}
			],
			"type": "talkgroup",
			"units": [{"label": "Engine 12", "order": 1, "unitRef": 12345}]
		},
		{
			"autoPopulate": false,
			"label": "Harbour",
			"order": 2,
			"systemRef": 10002,
			"talkgroups": [
				{
					"groupIds": [],
					"label": "Marine 12",
					"name": "Marine 12",
					"order": 1,
					"tagId": 6,
					"talkgroupRef": 1520000,
					"type": "audio"
				}
			],
			"type": "talkgroup"
		}
	],
	"tags": [
		{"id": 1, "label": "Air Traffic Control"},
		{"id": 2, "label": "Emergency "},
		{"id": 3, "label": "Fire Dispatch"},
		{"id": 4, "label": "Fire Tac"},
		{"id": 5, "label": "Fire Talk"},
		{"id": 6, "label": "Interop"},
		{"id": 7, "label": "Security"},
		{"id": 8, "label": "Talkgroup 12345"}
	]
}
EOF

status=$(
	curl -sS -o "$work/config-after-save.json" -w "%{http_code}" \
		-X PUT -H "Authorization: $token" -H 'Content-Type: application/json' \
		--data-binary "@$work/config.json" \
		"$base/api/admin/config"
)

if [[ "$status" != "200" ]]; then
	echo "the legacy server answered $status for the configuration save" >&2
	cat "$work/config-after-save.json" >&2
	cat "$work/server.log" >&2
	exit 1
fi

echo "uploading a call, with a frequency, a source unit, and a patch"

python3 - "$work/audio.wav" <<'PY'
import math
import struct
import sys

count = 8000

samples = struct.pack(
    "<%dh" % count,
    *(int(9000 * math.sin(2 * math.pi * 440 * n / 8000)) for n in range(count)),
)

header = b"RIFF" + struct.pack("<I", 36 + len(samples)) + b"WAVEfmt "
header += struct.pack("<IHHIIHH", 16, 1, 1, 8000, 8000, 1, 16)
header += b"data" + struct.pack("<I", len(samples))

with open(sys.argv[1], "wb") as audio:
	audio.write(header + samples)
PY

curl -sS -X POST \
	-F "key=fixture-apikey-0123456789" \
	-F "system=10001" \
	-F "talkgroup=1514000" \
	-F "dateTime=1750000000" \
	-F "site=77" \
	-F "frequency=1514000000" \
	-F 'patches=[1514001]' \
	-F 'sources=[{"pos":0,"src":12345}]' \
	-F "audio=@$work/audio.wav;type=audio/wav" \
	"$base/api/call-upload" > "$work/upload-response.txt"

cat "$work/upload-response.txt"

echo "uploading a call to a talkgroup with a delay, so it lands in the delayed table"

# The delayed call's timestamp is 2030-01-01 rather than a recent time on purpose: the legacy
# server only stores a call in the `delayed` table when its timestamp plus the talkgroup delay is
# still in the future (`Delayer.CanDelay`), and a fixed instant keeps the fixture reproducible.
curl -sS -X POST \
	-F "key=fixture-apikey-0123456789" \
	-F "system=10001" \
	-F "talkgroup=1514002" \
	-F "dateTime=1893456000" \
	-F "audio=@$work/audio.wav;type=audio/wav" \
	"$base/api/call-upload" > "$work/upload-delayed-response.txt"

cat "$work/upload-delayed-response.txt"

# The ingest channel is asynchronous: the upload endpoints hand the call to it and answer
# before the call is written. Give the server time to finish writing.
sleep 2

kill "$server"
wait "$server" 2>/dev/null || true
unset server

cp "$work/rdio-scanner.db" "$here/rdio-scanner.db"

echo "wrote $here/rdio-scanner.db"
