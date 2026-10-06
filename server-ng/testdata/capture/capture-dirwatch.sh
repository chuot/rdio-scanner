#!/usr/bin/env bash
# Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
#
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
#
# This program is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with this program.  If not, see <http://www.gnu.org/licenses/>.
#
# WebSocket API Access Policy:
# This WebSocket API is reserved exclusively for Saubeo Solutions and its native applications.
# Unauthorized access is strictly prohibited.
# See API_ACCESS_POLICY.md for full terms.

# capture-dirwatch.sh rebuilds the golden end-to-end capture in
# server-ng/testdata/golden/dirwatch-end-to-end.json.
#
# It is a different capture from capture.sh because it is not about what the server puts on the wire in
# answer to a request: it is about a recording a recording tool leaves in a watched directory becoming a
# call, reaching a listener, and being written to the database. The expectation is therefore both the
# frames a listener was sent and the rows the run left behind, and both come from one run of the legacy
# binary.
#
# The run is set up rather than left to chance:
#
#   - the systems and talkgroups the recordings land on are seeded, so that a call reaching a listener
#     does not depend on the server inventing them on the way (that run is the auto-populate test);
#   - audio conversion is turned off, so that the recording a watch hands over is the recording the
#     capture wrote, byte for byte, and the golden pins the server passing it along rather than pinning
#     what one version of ffmpeg makes of it;
#   - the recordings are dropped one at a time, the second only after the first has been broadcast, so
#     that the frames arrive in an order both servers can be expected to produce.
#
# Usage:
#
#   ./capture-dirwatch.sh                    capture into ../golden
#   ./capture-dirwatch.sh --keep             keep the temporary directory and say where it is
#   ./capture-dirwatch.sh --allow-legacy-changes   capture even when server/ is not the pinned tree
#
# The legacy server is run with TZ=UTC: a watched recording is dated by a clock read in the server's own
# time zone, so without this the capture differs between machines.

set -euo pipefail

# The git tree hash of server/ that the captures in ../golden were taken from, the same pin capture.sh
# uses. server/ is never changed, so this only moves if someone changes it.
legacy_server_tree="02ae33d4651fee4c46a93bdcf574d0aead89e542"

listen="127.0.0.1:3131"

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
server_ng=$(cd "${here}/../.." && pwd)
repo=$(cd "${server_ng}/.." && pwd)
golden="${server_ng}/testdata/golden"

keep=false
allow_legacy_changes=false

for arg in "$@"; do
	case "${arg}" in
		--keep)
			keep=true
			;;
		--allow-legacy-changes)
			allow_legacy_changes=true
			;;
		*)
			echo "capture-dirwatch.sh: unknown argument ${arg}" >&2
			exit 2
			;;
	esac
done

# --- the legacy source the goldens are pinned to ---------------------------

if ! tree=$(git -C "${repo}" rev-parse "HEAD:server" 2>&1); then
	echo "capture-dirwatch.sh: cannot read the git tree of server/ — ${tree}" >&2
	echo "capture-dirwatch.sh: pass --allow-legacy-changes to capture without the pin" >&2
	exit 1
fi

if [[ "${tree}" != "${legacy_server_tree}" ]]; then
	echo "capture-dirwatch.sh: server/ is git tree ${tree}, the goldens are pinned to ${legacy_server_tree}" >&2

	if [[ "${allow_legacy_changes}" != true ]]; then
		echo "capture-dirwatch.sh: re-take the capture, or pass --allow-legacy-changes" >&2
		exit 1
	fi

	echo "capture-dirwatch.sh: capturing anyway, because --allow-legacy-changes was given" >&2
fi

if [[ -n "$(git -C "${repo}" status --porcelain -- server/)" ]]; then
	echo "capture-dirwatch.sh: server/ has uncommitted changes, which the pin cannot see" >&2

	if [[ "${allow_legacy_changes}" != true ]]; then
		exit 1
	fi
fi

# --- the legacy server, on a database of its own ---------------------------

work=$(mktemp -d)
base="${work}/base"
watch="${work}/watch"
db="${base}/rdio-scanner.db"

cleanup() {
	if [[ -n "${pid:-}" ]] && kill -0 "${pid}" 2>/dev/null; then
		kill -INT "${pid}" 2>/dev/null || true
		wait "${pid}" 2>/dev/null || true
	fi

	if [[ "${keep}" == true ]]; then
		echo "capture-dirwatch.sh: the legacy server, its database and its log are in ${work}"
	else
		rm -rf "${work}"
	fi
}

trap cleanup EXIT

echo "capture-dirwatch.sh: building the legacy server" >&2

(cd "${repo}/server" && go build -o "${work}/rdio-scanner-legacy" .)

echo "capture-dirwatch.sh: building the sqlite helper" >&2

(cd "${server_ng}" && go build -o "${work}/sqlite" ./testdata/capture/sqlite)

mkdir -p "${base}" "${watch}/dsd/20261006" "${watch}/trunk" "${work}/dump"

if curl -fsS --connect-timeout 1 -o /dev/null "http://${listen}/" 2>/dev/null; then
	echo "capture-dirwatch.sh: something is already answering on ${listen}, and the capture would talk to it" >&2
	exit 1
fi

# The first boot is only there to make the server build its own database and run its own migrations, so
# that the configuration the capture seeds is written into the schema the server itself expects.
echo "capture-dirwatch.sh: starting it once, to make the database" >&2

TZ=UTC "${work}/rdio-scanner-legacy" \
	-base_dir "${base}" \
	-db_type sqlite \
	-db_file rdio-scanner.db \
	-listen "${listen}" >"${work}/boot.log" 2>&1 &
pid=$!

sleep 3

kill -INT "${pid}" 2>/dev/null || true
wait "${pid}" 2>/dev/null || true
pid=""

# --- the configuration both servers are given ------------------------------

# The two systems and their talkgroups are seeded so that the recordings land on configuration that is
# already there: the trunk-recorder recording names its system by label and the dsd+ recording names it by
# reference, which is both way a call is matched.
"${work}/sqlite" exec "${db}" "INSERT INTO \"systems\" (\"systemId\", \"autoPopulate\", \"blacklists\", \"delay\", \"label\", \"led\", \"order\", \"systemRef\", \"type\") VALUES (1, 0, '', 0, 'Grand Metro', '', 0, 10001, '')"
"${work}/sqlite" exec "${db}" "INSERT INTO \"systems\" (\"systemId\", \"autoPopulate\", \"blacklists\", \"delay\", \"label\", \"led\", \"order\", \"systemRef\", \"type\") VALUES (2, 0, '', 0, 'System 4660', '', 0, 4660, '')"

"${work}/sqlite" exec "${db}" "INSERT INTO \"talkgroups\" (\"talkgroupId\", \"alert\", \"delay\", \"frequency\", \"label\", \"led\", \"name\", \"order\", \"systemId\", \"tagId\", \"talkgroupRef\", \"type\") VALUES (1, '', 0, 0, 'Main', '', 'Dispatch 1', 0, 1, 9, 1514000, '')"
"${work}/sqlite" exec "${db}" "INSERT INTO \"talkgroups\" (\"talkgroupId\", \"alert\", \"delay\", \"frequency\", \"label\", \"led\", \"name\", \"order\", \"systemId\", \"tagId\", \"talkgroupRef\", \"type\") VALUES (2, '', 0, 0, 'Dispatch', '', 'Talkgroup 1514000', 0, 2, 9, 1514000, '')"

"${work}/sqlite" exec "${db}" "INSERT INTO \"dirwatches\" (\"dirwatchId\", \"delay\", \"deleteAfter\", \"directory\", \"disabled\", \"extension\", \"frequency\", \"mask\", \"order\", \"siteId\", \"systemId\", \"talkgroupId\", \"type\") VALUES (1, 2000, 1, '${watch}/dsd', 0, '', 0, '', 1, 0, 0, 0, 'dsdplus')"
"${work}/sqlite" exec "${db}" "INSERT INTO \"dirwatches\" (\"dirwatchId\", \"delay\", \"deleteAfter\", \"directory\", \"disabled\", \"extension\", \"frequency\", \"mask\", \"order\", \"siteId\", \"systemId\", \"talkgroupId\", \"type\") VALUES (2, 2000, 1, '${watch}/trunk', 0, '', 0, '', 2, 0, 0, 0, 'trunk-recorder')"

"${work}/sqlite" exec "${db}" "INSERT INTO \"options\" (\"key\", \"value\") VALUES ('audioConversion', '0')"

# --- the run being captured ------------------------------------------------

echo "capture-dirwatch.sh: starting it again, on the configuration it was given" >&2

TZ=UTC "${work}/rdio-scanner-legacy" \
	-base_dir "${base}" \
	-db_type sqlite \
	-db_file rdio-scanner.db \
	-listen "${listen}" >"${work}/legacy.log" 2>&1 &
pid=$!

sleep 2

node "${here}/capture-dirwatch.mjs" \
	--url "ws://${listen}/" \
	--out "${work}/frames.json" \
	--watch-dsd "${watch}/dsd/20261006" \
	--watch-trunk "${watch}/trunk"

kill -INT "${pid}" 2>/dev/null || true
wait "${pid}" 2>/dev/null || true
pid=""

# The server notices a listener that hung up on its own schedule, and the log row for that is part of the
# capture.
sleep 1

# --- what the run left behind ----------------------------------------------

"${work}/sqlite" rows "${db}" 'SELECT "callId", "audioFilename", "audioMime", "siteRef", "systemId", "talkgroupId", "timestamp", length("audio") AS "audioBytes" FROM "calls" ORDER BY "callId"' >"${work}/dump/calls.json"

# The first two log rows are the two boots of this script: the boot that made the database, and the boot
# being captured. The test that reads this golden boots its own server once and compares what that run
# added after its own boot.
"${work}/sqlite" rows "${db}" 'SELECT "level", "message" FROM "logs" WHERE "logid" > 2 ORDER BY "logid"' >"${work}/dump/logs.json"

for table in groups tags talkgroups units systems; do
	"${work}/sqlite" rows "${db}" "SELECT * FROM \"${table}\" ORDER BY 1" >"${work}/dump/${table}.json"
done

node "${here}/assemble-dirwatch.mjs" \
	--frames "${work}/frames.json" \
	--dump "${work}/dump" \
	--out "${golden}/dirwatch-end-to-end.json"

echo "capture-dirwatch.sh: wrote ${golden}/dirwatch-end-to-end.json" >&2

echo "capture-dirwatch.sh: the legacy server said:" >&2
sed "s/^/  /" "${work}/legacy.log" >&2
