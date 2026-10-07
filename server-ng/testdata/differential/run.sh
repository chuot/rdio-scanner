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

# run.sh runs the differential harness: the legacy server and server-ng, side by side, each on a database of
# its own, each driven through the same scenarios, and their answers and their stored rows compared.
#
#	usage: ./run.sh [--keep] [--allow-legacy-changes] [--scenario <name>]...
#
# It exits 0 when the two servers agree everywhere except where the harness expects them not to.
#
# Every scenario is run on a fresh copy of a database each server made for itself, so that what one scenario
# does cannot change what another is compared on: the call a server ingests, the row it writes, and the
# identifiers it hands out are all part of what is being compared.

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
server_ng=$(cd "${here}/../.." && pwd)
repo=$(cd "${server_ng}/.." && pwd)

# The legacy source the harness is pinned to, which is the same tree the golden captures are pinned to.
legacy_server_tree=02ae33d4651fee4c46a93bdcf574d0aead89e542

# The scenarios, in the order they are run.
all_scenarios=(
	listener-frames
	missing-call-fetch
	admin-rest
	admin-config-socket
	scoped-upload
	access-limit
	scope-normalization
	dirwatch-calls
)

# The tables the two databases are compared on, and the query that reads each one. The audio of a call is
# compared by how many bytes it is, because the harness hands both servers the same recording and the point is
# that both stored the bytes they were given. The log rows are compared by what they say rather than by when
# they were written, because the two runs do not happen at the same moment.
tables=(
	accesses
	apikeys
	callFrequencies
	callPatches
	callUnits
	calls
	delayed
	dirwatches
	groups
	logs
	options
	sites
	systems
	tags
	talkgroups
	talkgroupGroups
	units
)

keep=false
allow_legacy_changes=false
scenarios=()

while [[ $# -gt 0 ]]; do
	case "$1" in
	--keep)
		keep=true
		shift
		;;
	--allow-legacy-changes)
		allow_legacy_changes=true
		shift
		;;
	--scenario)
		scenarios+=("$2")
		shift 2
		;;
	*)
		echo "run.sh: unknown argument $1" >&2
		exit 2
		;;
	esac
done

if [[ ${#scenarios[@]} -eq 0 ]]; then
	scenarios=("${all_scenarios[@]}")
fi

for scenario in "${scenarios[@]}"; do
	if [[ ! " ${all_scenarios[*]} " == *" ${scenario} "* ]]; then
		echo "run.sh: no scenario named ${scenario}" >&2

		exit 2
	fi
done

# --- the legacy source the harness is pinned to ----------------------------

if ! tree=$(git -C "${repo}" rev-parse "HEAD:server" 2>&1); then
	echo "run.sh: cannot read the git tree of server/ — ${tree}" >&2
	echo "run.sh: pass --allow-legacy-changes to run the harness without the pin" >&2

	exit 1
fi

if [[ "${tree}" != "${legacy_server_tree}" ]]; then
	echo "run.sh: server/ is git tree ${tree}, the harness is pinned to ${legacy_server_tree}" >&2

	if [[ "${allow_legacy_changes}" != true ]]; then
		echo "run.sh: the legacy server has changed, and the divergences this harness expects are the ones that" >&2
		echo "run.sh: source produces" >&2

		exit 1
	fi

	echo "run.sh: running anyway, because --allow-legacy-changes was given" >&2
fi

if [[ -n "$(git -C "${repo}" status --porcelain -- server/)" ]]; then
	echo "run.sh: server/ has uncommitted changes, which the pin cannot see" >&2

	if [[ "${allow_legacy_changes}" != true ]]; then
		exit 1
	fi
fi

# --- the two servers -------------------------------------------------------

work=$(mktemp -d)

pids=()

cleanup() {
	for pid in "${pids[@]}"; do
		if kill -0 "${pid}" 2>/dev/null; then
			kill -INT "${pid}" 2>/dev/null || true
			wait "${pid}" 2>/dev/null || true
		fi
	done

	if [[ "${keep}" == true ]]; then
		echo "run.sh: everything the harness wrote is in ${work}" >&2
	else
		rm -rf "${work}"
	fi
}

trap cleanup EXIT

# answering is how the harness checks a port before it claims one, and how it waits for a server to be ready.
answering() {
	curl -s -o /dev/null --connect-timeout 1 "http://127.0.0.1:${1}/" 2>/dev/null
}

wait_for_server() {
	for _ in $(seq 1 80); do
		if answering "${1}"; then
			return 0
		fi

		sleep 0.25
	done

	echo "run.sh: nothing came up on 127.0.0.1:${1}" >&2
	echo "run.sh: the server's own log is ${work}/logs" >&2

	exit 1
}

echo "run.sh: building the legacy server" >&2

(cd "${repo}/server" && go build -o "${work}/rdio-scanner-legacy" .)

echo "run.sh: building server-ng" >&2

(cd "${server_ng}" && go build -o "${work}/rdio-scanner-ng" ./cmd/rdio-scanner)

echo "run.sh: building the sqlite helper" >&2

(cd "${server_ng}" && go build -o "${work}/sqlite" ./testdata/capture/sqlite)

mkdir -p "${work}/transcripts" "${work}/dumps" "${work}/logs"

# Each server makes the database it expects for itself, once, and every scenario starts from a copy of it.
# Both servers write the same schema and the same starting rows, which is the first thing this harness proves.
for flavor in legacy ng; do
	binary="${work}/rdio-scanner-legacy"

	if [[ "${flavor}" == ng ]]; then
		binary="${work}/rdio-scanner-ng"
	fi

	base="${work}/template/${flavor}/base"
	port=$((3138))

	mkdir -p "${base}"

	if answering "${port}"; then
		echo "run.sh: something is already answering on 127.0.0.1:${port}" >&2

		exit 1
	fi

	echo "run.sh: starting the ${flavor} server once, to make its database" >&2

	TZ=UTC "${binary}" \
		-base_dir "${base}" \
		-db_type sqlite \
		-db_file rdio-scanner.db \
		-listen "127.0.0.1:${port}" >"${work}/logs/template-${flavor}.log" 2>&1 &
	pid=$!
	pids+=("${pid}")

	wait_for_server "${port}"

	kill -INT "${pid}" 2>/dev/null || true
	wait "${pid}" 2>/dev/null || true

	pids=()
done

# --- the scenarios ---------------------------------------------------------

index=0

for scenario in "${scenarios[@]}"; do
	index=$((index + 1))

	legacy_port=$((3140 + index))
	ng_port=$((3150 + index))

	echo "run.sh: scenario ${scenario} — legacy on ${legacy_port}, server-ng on ${ng_port}" >&2

	dirs=()

	for flavor in legacy ng; do
		base="${work}/run/${scenario}/${flavor}/base"
		watch="${work}/run/${scenario}/${flavor}/watch"

		mkdir -p "${base}" "${watch}/dsd/20261006" "${watch}/trunk"

		cp "${work}/template/${flavor}/base/rdio-scanner.db" "${base}/rdio-scanner.db"

		dirs+=("${flavor}:${base}:${watch}")
	done

	if answering "${legacy_port}" || answering "${ng_port}"; then
		echo "run.sh: something is already answering on one of the harness ports" >&2

		exit 1
	fi

	started=()

	for flavor in legacy ng; do
		binary="${work}/rdio-scanner-legacy"
		port=${legacy_port}

		if [[ "${flavor}" == ng ]]; then
			binary="${work}/rdio-scanner-ng"
			port=${ng_port}
		fi

		base="${work}/run/${scenario}/${flavor}/base"

		TZ=UTC "${binary}" \
			-base_dir "${base}" \
			-db_type sqlite \
			-db_file rdio-scanner.db \
			-listen "127.0.0.1:${port}" >"${work}/logs/${scenario}-${flavor}.log" 2>&1 &
		pid=$!
		pids+=("${pid}")
		started+=("${pid}")

		wait_for_server "${port}"
	done

	for flavor in legacy ng; do
		binary_port=${legacy_port}

		if [[ "${flavor}" == ng ]]; then
			binary_port=${ng_port}
		fi

		watch="${work}/run/${scenario}/${flavor}/watch"

		node "${here}/differential.mjs" \
			--scenario "${scenario}" \
			--base "http://127.0.0.1:${binary_port}" \
			--out "${work}/transcripts/${scenario}.${flavor}.json" \
			--watch-dsd "${watch}/dsd" \
			--watch-trunk "${watch}/trunk"
	done

	# Both servers are left running while the debounced reports they scheduled land, so that a scenario that
	# connected a listener is compared on the log row that report writes.
	sleep 6

	for pid in "${started[@]}"; do
		kill -INT "${pid}" 2>/dev/null || true
		wait "${pid}" 2>/dev/null || true
	done

	pids=()

	mkdir -p "${work}/dumps/${scenario}/legacy" "${work}/dumps/${scenario}/ng"

	for flavor in legacy ng; do
		db="${work}/run/${scenario}/${flavor}/base/rdio-scanner.db"

		for table in "${tables[@]}"; do
			query="SELECT * FROM \"${table}\" ORDER BY 1"

			case "${table}" in
			calls)
				query='SELECT "callId", "audioFilename", "audioMime", "siteRef", "systemId", "talkgroupId", "timestamp", length("audio") AS "audioBytes" FROM "calls" ORDER BY "callId"'
				;;
			logs)
				query='SELECT "level", "message" FROM "logs" ORDER BY "logid"'
				;;
			options)
				query='SELECT "key", "value" FROM "options" ORDER BY "key"'
				;;
			esac

			"${work}/sqlite" rows "${db}" "${query}" >"${work}/dumps/${scenario}/${flavor}/${table}.json"
		done
	done
done

# --- the comparison --------------------------------------------------------

node "${here}/compare.mjs" --work "${work}"
