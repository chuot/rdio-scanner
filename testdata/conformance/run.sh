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

# run.sh runs the checklist in docs/specs/FEAT-003-server-ng-refactor/quickstart.md §6 against a running server,
# in a real browser, with the real web application embedded in the binary.
#
#	usage: ./run.sh [--keep]
#
# It builds the server the way the release does — `make webapp` and then the Go build — starts it on a database
# of its own, and drives it.
#
# It exits 0 when every item passed.
#
# It used to do a second thing: start the legacy server, let it write a database, and start this server on a copy
# of it. That needed the legacy source, and the legacy source is gone. A database the legacy server wrote is still
# read by this repository's tests — see testdata/legacydb/ and the migration tests that read it — but it is read
# as a pinned fixture rather than produced at harness time.

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "${here}/../.." && pwd)

keep=false

while [[ $# -gt 0 ]]; do
	case "$1" in
	--keep)
		keep=true
		shift
		;;
	*)
		echo "run.sh: unknown argument $1" >&2

		exit 2
		;;
	esac
done

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
		echo "run.sh: everything this harness wrote is in ${work}" >&2
	else
		rm -rf "${work}"
	fi
}

trap cleanup EXIT

answering() {
	curl -s -o /dev/null --connect-timeout 1 "http://127.0.0.1:${1}/" 2>/dev/null
}

start_server() {
	local binary=$1 base=$2 port=$3 log=$4

	if answering "${port}"; then
		echo "run.sh: something is already answering on 127.0.0.1:${port}" >&2

		exit 1
	fi

	mkdir -p "${base}" "${work}/logs"

	TZ=UTC "${binary}" \
		-base_dir "${base}" \
		-db_type sqlite \
		-db_file rdio-scanner.db \
		-listen "127.0.0.1:${port}" >"${work}/logs/${log}.log" 2>&1 &

	pids+=("$!")

	for _ in $(seq 1 80); do
		if answering "${port}"; then
			return 0
		fi

		sleep 0.25
	done

	echo "run.sh: nothing came up on 127.0.0.1:${port}" >&2
	echo "run.sh: the server's own log is in ${work}/logs/${log}.log" >&2

	exit 1
}

stop_servers() {
	# The server is left running for a moment first, so that the debounced reports it scheduled land before it
	# goes away.
	sleep 3

	for pid in "${pids[@]}"; do
		if kill -0 "${pid}" 2>/dev/null; then
			kill -INT "${pid}" 2>/dev/null || true
			wait "${pid}" 2>/dev/null || true
		fi
	done

	pids=()
}

# --- the server, built the way the release builds it -----------------------

if [[ ! -f "${repo}/internal/presentation/webapp/bundle/index.html" ]]; then
	echo "run.sh: building the web application into the server" >&2

	make -C "${repo}" webapp >/dev/null
fi

echo "run.sh: building the server" >&2

(cd "${repo}" && go build -o "${work}/rdio-scanner" ./cmd/rdio-scanner)

# --- the application, against a server of its own --------------------------

echo "run.sh: the web application against the server on 127.0.0.1:3170" >&2

start_server "${work}/rdio-scanner" "${work}/main/base" 3170 main-server

mkdir -p "${work}/main/watched"

node "${here}/conformance.mjs" \
	--base "http://127.0.0.1:3170" \
	--cdpPort 9333 \
	--watchDir "${work}/main/watched" \
	--work "${work}"

main_status=$?

stop_servers

exit "${main_status}"
