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

# run.sh runs the checklist in docs/specs/FEAT-003-server-ng-refactor/quickstart.md §6 against a running
# server-ng, in a real browser, with the real web application embedded in the binary.
#
#	usage: ./run.sh [--keep] [--no-drop-in]
#
# It builds the server the way the release does — `make webapp-ng-server` and then the Go build — starts it on
# a database of its own, and drives it. Then it does the drop-in: the legacy server is started on a database of
# its own, given a configuration and a call, and stopped; server-ng is started on a copy of that database and
# the web application is asked whether the calls are there.
#
# It exits 0 when every item passed.

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
server_ng=$(cd "${here}/../.." && pwd)
repo=$(cd "${server_ng}/.." && pwd)

# The legacy source the drop-in is pinned to, which is the same tree the golden captures and the differential
# harness are pinned to.
legacy_server_tree=02ae33d4651fee4c46a93bdcf574d0aead89e542

keep=false
drop_in=true

while [[ $# -gt 0 ]]; do
	case "$1" in
	--keep)
		keep=true
		shift
		;;
	--no-drop-in)
		drop_in=false
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

if [[ ! -f "${server_ng}/internal/presentation/webapp/webapp/index.html" ]]; then
	echo "run.sh: building the web application into the server" >&2

	make -C "${repo}" webapp-ng-server >/dev/null
fi

echo "run.sh: building server-ng" >&2

(cd "${server_ng}" && go build -o "${work}/rdio-scanner-ng" ./cmd/rdio-scanner)

# --- the application, against a server of its own --------------------------

echo "run.sh: the web application against server-ng on 127.0.0.1:3170" >&2

start_server "${work}/rdio-scanner-ng" "${work}/main/base" 3170 main-server

mkdir -p "${work}/main/watched"

node "${here}/conformance.mjs" \
	--phase main \
	--base "http://127.0.0.1:3170" \
	--cdpPort 9333 \
	--watchDir "${work}/main/watched" \
	--work "${work}"

main_status=$?

stop_servers

if [[ "${drop_in}" != true ]]; then
	exit "${main_status}"
fi

# --- a database the legacy server wrote ------------------------------------

if ! tree=$(git -C "${repo}" rev-parse "HEAD:server" 2>&1); then
	echo "run.sh: cannot read the git tree of server/ — ${tree}" >&2

	exit 1
fi

if [[ "${tree}" != "${legacy_server_tree}" ]]; then
	echo "run.sh: server/ is git tree ${tree}, this harness is pinned to ${legacy_server_tree}" >&2

	exit 1
fi

if [[ -n "$(git -C "${repo}" status --porcelain -- server/)" ]]; then
	echo "run.sh: server/ has uncommitted changes, which the pin cannot see" >&2

	exit 1
fi

echo "run.sh: building the legacy server" >&2

(cd "${repo}/server" && go build -o "${work}/rdio-scanner-legacy" .)

echo "run.sh: the legacy server writes a database on 127.0.0.1:3171" >&2

start_server "${work}/rdio-scanner-legacy" "${work}/drop-in/legacy/base" 3171 drop-in-legacy

node "${here}/conformance.mjs" --phase seed --base "http://127.0.0.1:3171"

seed_status=$?

stop_servers

mkdir -p "${work}/drop-in/ng/base-tmp"

cp "${work}/drop-in/legacy/base"/rdio-scanner.db* "${work}/drop-in/ng/base-tmp/"

echo "run.sh: server-ng reads that database on 127.0.0.1:3172" >&2

start_server "${work}/rdio-scanner-ng" "${work}/drop-in/ng/base-tmp" 3172 drop-in-ng

node "${here}/conformance.mjs" \
	--phase drop-in \
	--base "http://127.0.0.1:3172" \
	--cdpPort 9334 \
	--work "${work}"

drop_in_status=$?

stop_servers

if [[ ${main_status} -ne 0 || ${seed_status} -ne 0 || ${drop_in_status} -ne 0 ]]; then
	exit 1
fi
