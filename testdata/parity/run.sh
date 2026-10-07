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

# run.sh puts the current scanner application and the legacy Angular one side by side so that parity.mjs can
# measure them against each other.
#
#	usage: ./run.sh [--keep] [--scratch DIR] [--only NAME]
#	usage: ./run.sh --admin --before REF [--keep] [--scratch DIR] [--only NAME]
#
# The legacy application is not in this branch any more. It is taken out of Git history, built outside the
# repository, and served by a second copy of this repository's own server: the same Go binary, the same database
# code, the same API, only a different bundle embedded in it. That is what makes the two pages comparable — the
# difference between them is the client and nothing else.
#
# `--admin` measures the administration application against itself instead: the same server around the
# administration bundle built from `--before`, and the same server around the administration bundle built from the
# working tree. Every row of that run is expected to match, because the work this harness exists to check must not
# change what the administration renders.
#
# Embedding happens at build time, so a bundle directory is swapped for the duration of one `go build` and put
# back immediately afterwards. The repository is left exactly as it was found.
#
# It exits 0 when every measured surface matches, 1 when the probe ran and found a difference, 2 when the probe
# could not measure what it was asked to measure.

set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "${here}/../.." && pwd)

scratch=${SCRATCH:-/tmp/opencode/rdio-parity}
keep=false
only=""
app=scanner
before=""

while [[ $# -gt 0 ]]; do
	case "$1" in
	--keep)
		keep=true
		shift
		;;
	--scratch)
		scratch=$2
		shift 2
		;;
	--only)
		only=$2
		shift 2
		;;
	--admin)
		app=admin
		shift
		;;
	--before)
		before=$2
		shift 2
		;;
	*)
		echo "run.sh: unknown argument $1" >&2

		exit 2
		;;
	esac
done

if [[ "${app}" == admin && -z "${before}" ]]; then
	echo "run.sh: --admin needs --before REF: the commit the administration is compared against" >&2

	exit 2
fi

scannerBundle=${repo}/internal/presentation/serving/scanner/bundle
adminBundle=${repo}/internal/presentation/serving/admin/bundle
work=$(mktemp -d)

# A bundle directory is the one thing this harness touches inside the repository, so it is put back whatever
# happens, including a failure in the middle of the swap.
swapped=none

restore_bundle() {
	case "${swapped}" in
	scanner)
		rm -rf "${scannerBundle}"
		mkdir -p "${scannerBundle}"
		cp -a "${work}/saved-bundle/." "${scannerBundle}/"
		;;
	admin)
		rm -rf "${adminBundle}"
		mkdir -p "${adminBundle}"
		cp -a "${work}/saved-bundle/." "${adminBundle}/"
		;;
	*)
		return 0
		;;
	esac

	swapped=none

	echo "run.sh: the ${app} bundle is back where it was" >&2
}

pids=()

cleanup() {
	for pid in "${pids[@]}"; do
		if kill -0 "${pid}" 2>/dev/null; then
			# A stopped server would not notice a signal until it is running again.
			kill -CONT "${pid}" 2>/dev/null || true
			kill -INT "${pid}" 2>/dev/null || true
			wait "${pid}" 2>/dev/null || true
		fi
	done

	restore_bundle

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

		exit 2
	fi

	mkdir -p "${base}" "${work}/logs"

	TZ=UTC "${binary}" \
		-base_dir "${base}" \
		-db_type sqlite \
		-db_file rdio-scanner.db \
		-listen "127.0.0.1:${port}" >"${work}/logs/${log}.log" 2>&1 &

	pids+=("$!")

	# The probe needs to be able to hold the server still while it measures a page of results that is still on its
	# way, so each server's process id is written where the probe can find it.
	echo "$!" >"${work}/${log}.pid"

	for _ in $(seq 1 80); do
		if answering "${port}"; then
			return 0
		fi

		sleep 0.25
	done

	echo "run.sh: nothing came up on 127.0.0.1:${port}" >&2
	echo "run.sh: the server's own log is in ${work}/logs/${log}.log" >&2

	exit 2
}

# --- the legacy reference, built outside the repository ----------------------

mkdir -p "${scratch}"

if [[ "${app}" == scanner ]]; then
	if [[ ! -f "${scratch}/server/webapp/index.html" ]]; then
		if [[ ! -d "${scratch}/client" ]]; then
			echo "run.sh: taking the last Angular client out of Git history" >&2

			mkdir -p "${scratch}/client"

			(cd "${repo}" && git archive e89af2b^ client) | tar -x -C "${scratch}"
		fi

		echo "run.sh: building it (npm ci and ng build, in ${scratch}/client)" >&2

		(cd "${scratch}/client" && npm ci --loglevel=error --no-progress && npm run build)
	fi

	if [[ ! -f "${scratch}/server/webapp/index.html" ]]; then
		echo "run.sh: the legacy reference is not built in ${scratch}/server/webapp" >&2

		exit 2
	fi
fi

# --- the current application, built the way the release builds it -----------

if [[ "${app}" == scanner ]]; then
	echo "run.sh: building the current scanner application" >&2

	make -C "${repo}" scanner >/dev/null
else
	echo "run.sh: building the current administration application" >&2

	make -C "${repo}" admin >/dev/null
fi

echo "run.sh: building the server that carries it" >&2

(cd "${repo}" && go build -o "${work}/rdio-scanner-current" ./cmd/rdio-scanner)

# --- the other side of the comparison ---------------------------------------

if [[ "${app}" == scanner ]]; then
	echo "run.sh: building the same server around the legacy bundle" >&2

	mkdir -p "${work}/saved-bundle"
	cp -a "${scannerBundle}/." "${work}/saved-bundle/"
	swapped=scanner

	rm -rf "${scannerBundle}"
	mkdir -p "${scannerBundle}"
	cp -r "${scratch}/server/webapp/." "${scannerBundle}/"

	(cd "${repo}" && go build -o "${work}/rdio-scanner-legacy" ./cmd/rdio-scanner)

	restore_bundle
else
	echo "run.sh: building the administration application as it was at ${before}" >&2

	mkdir -p "${work}/before"
	(cd "${repo}" && git archive "${before}" internal/presentation/clients) | tar -x -C "${work}/before"

	# The extracted client needs the build tool it declares. Its lockfile is the one the repository installs
	# from, so the repository's own install is used instead of a second npm ci.
	ln -s "${repo}/internal/presentation/clients/node_modules" \
		"${work}/before/internal/presentation/clients/node_modules"

	(cd "${work}/before/internal/presentation/clients" && node build.mjs --production --admin)

	beforeBundle=${work}/before/internal/presentation/serving/admin/bundle

	if [[ ! -f "${beforeBundle}/index.html" ]]; then
		echo "run.sh: ${before} did not build an administration bundle" >&2

		exit 2
	fi

	mkdir -p "${work}/saved-bundle"
	cp -a "${adminBundle}/." "${work}/saved-bundle/"
	swapped=admin

	rm -rf "${adminBundle}"
	mkdir -p "${adminBundle}"
	cp -r "${beforeBundle}/." "${adminBundle}/"

	(cd "${repo}" && go build -o "${work}/rdio-scanner-legacy" ./cmd/rdio-scanner)

	restore_bundle
fi

# --- both of them, on their own databases, seeded the same way --------------

if [[ "${app}" == scanner ]]; then
	echo "run.sh: the current application on 127.0.0.1:3300" >&2

	start_server "${work}/rdio-scanner-current" "${work}/current" 3300 current

	echo "run.sh: the legacy application on 127.0.0.1:3301" >&2

	start_server "${work}/rdio-scanner-legacy" "${work}/legacy" 3301 legacy

	# A page that is not the one expected makes the whole matrix meaningless, so each side is checked before the
	# browser is pointed at it.
	if ! curl -s http://127.0.0.1:3300/ | grep -q '<rdio-app>'; then
		echo "run.sh: 127.0.0.1:3300 is not serving the current scanner application" >&2

		exit 2
	fi

	if ! curl -s http://127.0.0.1:3301/ | grep -q 'mat-sidenav-container\|<app-root'; then
		echo "run.sh: 127.0.0.1:3301 is not serving the legacy scanner application" >&2

		exit 2
	fi
else
	echo "run.sh: the administration application, built from the working tree, on 127.0.0.1:3300" >&2

	start_server "${work}/rdio-scanner-current" "${work}/current" 3300 current

	echo "run.sh: the administration application, built from ${before}, on 127.0.0.1:3301" >&2

	start_server "${work}/rdio-scanner-legacy" "${work}/legacy" 3301 legacy

	if ! curl -s http://127.0.0.1:3300/admin/ | grep -q 'rdio-admin'; then
		echo "run.sh: 127.0.0.1:3300/admin/ is not serving the administration application" >&2

		exit 2
	fi

	if ! curl -s http://127.0.0.1:3301/admin/ | grep -q 'rdio-admin'; then
		echo "run.sh: 127.0.0.1:3301/admin/ is not serving the administration application" >&2

		exit 2
	fi
fi

# --- the probe ---------------------------------------------------------------

args=(
	--app "${app}"
	--current "http://127.0.0.1:3300"
	--legacy "http://127.0.0.1:3301"
	--currentPid "${work}/current.pid"
	--legacyPid "${work}/legacy.pid"
	--out "${work}/matrix"
	--cdpPort 9444
)

if [[ -n "${only}" ]]; then
	args+=(--only "${only}")
fi

node "${here}/parity.mjs" "${args[@]}"

status=$?

exit "${status}"
