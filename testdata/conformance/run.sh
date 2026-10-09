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
#	usage: ./run.sh [--keep] [--seed <database>]
#
# It builds the server the way the release does — `go run ./cmd/build`, which compiles both web
# applications and then the server that carries them — starts it on a database of its own, and drives
# it.
#
# It exits 0 when every item passed.
#
# With --seed, the database named is copied where the server opens its own before the server is started, and the
# checklist that runs is the drop-in one: the server migrates that database on its way up, and the items ask the
# application whether what came across is there. `testdata/legacyv6/rdio-scanner.db` is the database to name
# there, and testdata/legacyv6/generate.sh says how it was made.
#
# It used to do a second thing: start the legacy server, let it write a database, and start this server on a copy
# of it. That needed the legacy source, and the legacy source is gone. A database the legacy server wrote is still
# read by this repository's tests — see testdata/legacydb/ and the migration tests that read it — but it is read
# as a pinned fixture rather than produced at harness time.

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "${here}/../.." && pwd)

keep=false
seed=""

while [[ $# -gt 0 ]]; do
	case "$1" in
	--keep)
		keep=true
		shift
		;;
	--seed)
		if [[ -z "${2:-}" ]]; then
			echo "run.sh: --seed names a database to start the server on" >&2

			exit 2
		fi

		seed=$(cd "$(dirname "$2")" && pwd)/$(basename "$2")
		shift 2
		;;
	*)
		echo "run.sh: unknown argument $1" >&2

		exit 2
		;;
	esac
done

if [[ -n "${seed}" && ! -f "${seed}" ]]; then
	echo "run.sh: there is no database at ${seed}" >&2

	exit 2
fi

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

# Both applications are built on every run, into the bundle directories of the Go packages that embed
# them. What used to be here checked a path this repository does not have — internal/presentation/
# webapp/bundle/index.html — and ran a Makefile rule that does not exist either. The script does not
# stop on a failed command, so both mistakes went unnoticed and the harness went on driving a server
# that had embedded whatever bundles a previous build happened to have left in place.
#
# A harness that compares a real browser against the real application has to be sure of which
# application it is comparing, so the two builds are run here and each one is checked.

echo "run.sh: building the two web applications" >&2

if ! (cd "${repo}" && go run ./cmd/build client); then
	echo "run.sh: the web applications did not build" >&2

	exit 1
fi

echo "run.sh: building the server" >&2

if ! (cd "${repo}" && go run ./cmd/build server -o "${work}/rdio-scanner"); then
	echo "run.sh: the server did not build" >&2

	exit 1
fi

# --- the application, against a server of its own --------------------------

if [[ -n "${seed}" ]]; then
	echo "run.sh: the web application against the server on 127.0.0.1:3170, on a copy of ${seed}" >&2

	mkdir -p "${work}/migrated/base"

	if ! cp "${seed}" "${work}/migrated/base/rdio-scanner.db"; then
		echo "run.sh: ${seed} would not go where the server opens its own" >&2

		exit 1
	fi

	start_server "${work}/rdio-scanner" "${work}/migrated/base" 3170 migrated-server

	# The point of the seed is that the server migrates it. If it did not say so, the checklist below would be
	# driving a server that made an empty database of its own.
	if ! grep -q "migrating " "${work}/logs/migrated-server.log"; then
		echo "run.sh: the server did not say it was migrating the database it was given" >&2
		echo "run.sh: its own log:" >&2
		cat "${work}/logs/migrated-server.log" >&2

		exit 1
	fi

	node "${here}/conformance.mjs" \
		--phase migrated \
		--base "http://127.0.0.1:3170" \
		--cdpPort 9333 \
		--work "${work}"

	status=$?
else
	echo "run.sh: the web application against the server on 127.0.0.1:3170" >&2

	start_server "${work}/rdio-scanner" "${work}/main/base" 3170 main-server

	mkdir -p "${work}/main/watched"

	node "${here}/conformance.mjs" \
		--base "http://127.0.0.1:3170" \
		--cdpPort 9333 \
		--watchDir "${work}/main/watched" \
		--work "${work}"

	status=$?
fi

stop_servers

exit "${status}"
