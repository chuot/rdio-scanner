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

# capture-autopopulate.sh rebuilds the golden auto-populate capture in
# server-ng/testdata/golden/dirwatch-autopopulate.json.
#
# It is the same kind of run as capture-dirwatch.sh with the configuration taken away: the server is given
# two watches and nothing else, and the recordings it reads name systems, talkgroups, groups, tags and
# radios it has never heard of. What the golden holds is the rows the server created while taking them in.
#
# The recordings are dropped one at a time, and the next one is not dropped until the previous one has
# become a call, because the order is what each recording is testing: the first creates a talkgroup whose
# label is its own reference written out as a number, and the second is the labeled upload that repairs
# that placeholder.
#
# Usage:
#
#   ./capture-autopopulate.sh                    capture into ../golden
#   ./capture-autopopulate.sh --keep             keep the temporary directory and say where it is
#   ./capture-autopopulate.sh --allow-legacy-changes   capture even when server/ is not the pinned tree
#
# The legacy server is run with TZ=UTC: a dsd+ recording is dated by a clock read in the server's own time
# zone, so without this the capture differs between machines.

set -euo pipefail

legacy_server_tree="02ae33d4651fee4c46a93bdcf574d0aead89e542"

listen="127.0.0.1:3132"

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
			echo "capture-autopopulate.sh: unknown argument ${arg}" >&2
			exit 2
			;;
	esac
done

# --- the legacy source the goldens are pinned to ---------------------------

if ! tree=$(git -C "${repo}" rev-parse "HEAD:server" 2>&1); then
	echo "capture-autopopulate.sh: cannot read the git tree of server/ — ${tree}" >&2
	echo "capture-autopopulate.sh: pass --allow-legacy-changes to capture without the pin" >&2
	exit 1
fi

if [[ "${tree}" != "${legacy_server_tree}" ]]; then
	echo "capture-autopopulate.sh: server/ is git tree ${tree}, the goldens are pinned to ${legacy_server_tree}" >&2

	if [[ "${allow_legacy_changes}" != true ]]; then
		echo "capture-autopopulate.sh: re-take the capture, or pass --allow-legacy-changes" >&2
		exit 1
	fi

	echo "capture-autopopulate.sh: capturing anyway, because --allow-legacy-changes was given" >&2
fi

if [[ -n "$(git -C "${repo}" status --porcelain -- server/)" ]]; then
	echo "capture-autopopulate.sh: server/ has uncommitted changes, which the pin cannot see" >&2

	if [[ "${allow_legacy_changes}" != true ]]; then
		exit 1
	fi
fi

# --- the legacy server, on a database of its own ---------------------------

work=$(mktemp -d)
base="${work}/base"
watch="${work}/watch"
db="${base}/rdio-scanner.db"
staged="${work}/staged"

cleanup() {
	if [[ -n "${pid:-}" ]] && kill -0 "${pid}" 2>/dev/null; then
		kill -INT "${pid}" 2>/dev/null || true
		wait "${pid}" 2>/dev/null || true
	fi

	if [[ "${keep}" == true ]]; then
		echo "capture-autopopulate.sh: the legacy server, its database and its log are in ${work}"
	else
		rm -rf "${work}"
	fi
}

trap cleanup EXIT

echo "capture-autopopulate.sh: building the legacy server" >&2

(cd "${repo}/server" && go build -o "${work}/rdio-scanner-legacy" .)

echo "capture-autopopulate.sh: building the sqlite helper" >&2

(cd "${server_ng}" && go build -o "${work}/sqlite" ./testdata/capture/sqlite)

mkdir -p "${base}" "${watch}/dsd/20261006" "${watch}/trunk" "${work}/dump"

if curl -fsS --connect-timeout 1 -o /dev/null "http://${listen}/" 2>/dev/null; then
	echo "capture-autopopulate.sh: something is already answering on ${listen}, and the capture would talk to it" >&2
	exit 1
fi

echo "capture-autopopulate.sh: starting it once, to make the database" >&2

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

# Two watches and nothing else. The systems, talkgroups, groups, tags and radios the run ends up with are
# the thing being captured.
"${work}/sqlite" exec "${db}" "INSERT INTO \"dirwatches\" (\"dirwatchId\", \"delay\", \"deleteAfter\", \"directory\", \"disabled\", \"extension\", \"frequency\", \"mask\", \"order\", \"siteId\", \"systemId\", \"talkgroupId\", \"type\") VALUES (1, 2000, 1, '${watch}/dsd', 0, '', 0, '', 1, 0, 0, 0, 'dsdplus')"
"${work}/sqlite" exec "${db}" "INSERT INTO \"dirwatches\" (\"dirwatchId\", \"delay\", \"deleteAfter\", \"directory\", \"disabled\", \"extension\", \"frequency\", \"mask\", \"order\", \"siteId\", \"systemId\", \"talkgroupId\", \"type\") VALUES (2, 2000, 1, '${watch}/trunk', 0, '', 0, '', 2, 0, 0, 0, 'trunk-recorder')"

"${work}/sqlite" exec "${db}" "INSERT INTO \"options\" (\"key\", \"value\") VALUES ('audioConversion', '0')"

node "${here}/fixtures-autopopulate.mjs" --out "${staged}"

# --- the run being captured ------------------------------------------------

echo "capture-autopopulate.sh: starting it again, with two watches and no configuration" >&2

TZ=UTC "${work}/rdio-scanner-legacy" \
	-base_dir "${base}" \
	-db_type sqlite \
	-db_file rdio-scanner.db \
	-listen "${listen}" >"${work}/legacy.log" 2>&1 &
pid=$!

sleep 2

# The sidecar goes in before the recording it describes, and the recording is what the watch is looking
# for.
drop_trunk_recording() {
	"${work}/sqlite" rows "${db}" 'SELECT COUNT(*) AS "n" FROM "calls"' >/dev/null

	cp "${staged}/trunk/${1}.json" "${watch}/trunk/${1}.json"
	cp "${staged}/trunk/${1}.wav" "${watch}/trunk/${1}.wav"

	echo "capture-autopopulate.sh: dropped the ${1} trunk-recorder pair" >&2
}

count_calls() {
	"${work}/sqlite" rows "${db}" 'SELECT COUNT(*) AS "n" FROM "calls"' |
		grep -o '"n": [0-9]*' |
		cut -d' ' -f2
}

wait_for_calls() {
	local want=$1

	for _ in $(seq 1 120); do
		if [[ "$(count_calls)" == "${want}" ]]; then
			return 0
		fi

		sleep 0.25
	done

	echo "capture-autopopulate.sh: the server never made ${want} calls" >&2
	cat "${work}/legacy.log" >&2

	exit 1
}

drop_trunk_recording first
wait_for_calls 1

drop_trunk_recording second
wait_for_calls 2

drop_trunk_recording third
wait_for_calls 3

cp "${staged}/dsd/120000_1514000_P25_0001.1234-5_1514000[Dispatch]_6789[Car 4].mp3" \
	"${watch}/dsd/20261006/120000_1514000_P25_0001.1234-5_1514000[Dispatch]_6789[Car 4].mp3"

echo "capture-autopopulate.sh: dropped the dsd+ recording" >&2

wait_for_calls 4

kill -INT "${pid}" 2>/dev/null || true
wait "${pid}" 2>/dev/null || true
pid=""

sleep 1

# --- what the run left behind ----------------------------------------------

"${work}/sqlite" rows "${db}" 'SELECT "callId", "audioFilename", "audioMime", "siteRef", "systemId", "talkgroupId", "timestamp", length("audio") AS "audioBytes" FROM "calls" ORDER BY "callId"' >"${work}/dump/calls.json"

"${work}/sqlite" rows "${db}" 'SELECT "level", "message" FROM "logs" WHERE "logid" > 2 ORDER BY "logid"' >"${work}/dump/logs.json"

for table in groups tags talkgroups units systems; do
	"${work}/sqlite" rows "${db}" "SELECT * FROM \"${table}\" ORDER BY 1" >"${work}/dump/${table}.json"
done

node "${here}/assemble-dirwatch.mjs" \
	--dump "${work}/dump" \
	--out "${golden}/dirwatch-autopopulate.json"

echo "capture-autopopulate.sh: wrote ${golden}/dirwatch-autopopulate.json" >&2

echo "capture-autopopulate.sh: the legacy server said:" >&2
sed "s/^/  /" "${work}/legacy.log" >&2
