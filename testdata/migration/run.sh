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

# run.sh starts the two live backends the v6 -> v7 migration pass is proven against, and runs the
# build-tagged tests that migrate them.
#
#	usage: ./run.sh [--keep] [--backend NAME] [--big CALLS]
#
# `--backend mariadb` or `--backend postgresql` runs one backend; repeat it for both, which is the
# default. `--keep` leaves the containers up afterwards and prints how to run the tests against them a
# second time and how to take them down.
#
# `--big <calls>` is the large-database timing (REQ-010): it writes a v6.6.3-shaped database holding that
# many calls with `bigdb.py`, and runs the timing tests instead of the correctness ones, on the file
# backend and on every backend `--backend` named. The generated database is a hundred megabytes and is
# left in a scratch directory, not in the repository; `--keep` leaves it where it was written and prints
# the path.
#
# The containers hold empty databases. The seeding is not this script's job: every live test resets its
# backend to the same seeded v6.6.3 deployment before it runs, from v663-mariadb.sql /
# v663-postgres.sql and the rows the test reads out of testdata/legacyv6/rdio-scanner.db. A harness that
# seeded once for the whole suite would make each test depend on the one before it, and a migration test
# that depends on the previous test is not a proof of anything.
#
# It exits 0 when every backend it was asked for passed, 1 when a migration test failed, 2 when it could
# not start what it was asked to start.

set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "${here}/../.." && pwd)

keep=false
big_calls=""
backends=()

while [[ $# -gt 0 ]]; do
	case "$1" in
	--keep)
		keep=true
		shift
		;;
	--big)
		if [[ $# -lt 2 || ! "$2" =~ ^[0-9]+$ ]]; then
			echo "run.sh: --big needs a call count, as in --big 100000" >&2

			exit 2
		fi

		big_calls=$2

		shift 2
		;;
	--backend)
		if [[ $# -lt 2 ]]; then
			echo "run.sh: --backend needs a name" >&2

			exit 2
		fi

		backends+=("$2")
		shift 2
		;;
	*)
		echo "run.sh: unknown argument $1" >&2

		exit 2
		;;
	esac
done

if [[ ${#backends[@]} -eq 0 ]]; then
	backends=(mariadb postgresql)
fi

for backend in "${backends[@]}"; do
	if [[ "${backend}" != mariadb && "${backend}" != postgresql ]]; then
		echo "run.sh: --backend takes mariadb or postgresql, not ${backend}" >&2

		exit 2
	fi
done

runtime=$(command -v docker || command -v podman || true)

if [[ -z "${runtime}" ]]; then
	echo "run.sh: neither docker nor podman is on PATH, and the live backends need one" >&2

	exit 2
fi

# The names and ports are fixed so that `--keep` is usable: the printed commands have to name the
# containers they refer to. 13306 and 15432 are the usual ports plus one thousand, which is what keeps
# this harness from meeting a developer's own MariaDB or PostgreSQL on the way in.
db_name=rdioscanner
db_user=rdio
db_password=rdio-migration

mariadb_name=rdio-migration-mariadb
mariadb_port=13306

postgres_name=rdio-migration-postgres
postgres_port=15432

started=()
big_dir=""
big_env=""
big_fixture=""

stop() {
	for name in "${started[@]}"; do
		"${runtime}" rm -f "${name}" >/dev/null 2>&1 || true
	done

	# The large fixture is a scratch file the timing wrote for this run, and this cleanup only runs when
	# the script is not keeping anything: `--keep` leaves the fixture where it was, because the printed
	# command that re-runs the timing names it, and regenerating a hundred megabytes to run the timing a
	# second time is not what `--keep` means.
	if [[ -n "${big_dir}" ]]; then
		rm -rf "${big_dir}"
	fi
}

# `--keep` means the containers outlive the script, so the trap that takes them down is only set when
# the script is not keeping them.
if ! ${keep}; then
	trap stop EXIT
fi

start() {
	local backend=$1

	if [[ "${backend}" == mariadb ]]; then
		"${runtime}" run -d --name "${mariadb_name}" \
			-e MARIADB_ROOT_PASSWORD=rdio-migration-root \
			-e MARIADB_DATABASE="${db_name}" \
			-e MARIADB_USER="${db_user}" \
			-e MARIADB_PASSWORD="${db_password}" \
			-e TZ=UTC \
			-p "${mariadb_port}:3306" \
			mariadb:11 \
			--default-time-zone=+00:00 \
			--character-set-server=utf8mb4 \
			--collation-server=utf8mb4_unicode_ci >/dev/null

		started+=("${mariadb_name}")

		echo "starting ${mariadb_name} (mariadb:11) on port ${mariadb_port}"

		wait_for "${mariadb_name}" mariadb-admin ping --silent

		return
	fi

	"${runtime}" run -d --name "${postgres_name}" \
		-e POSTGRES_DB="${db_name}" \
		-e POSTGRES_USER="${db_user}" \
		-e POSTGRES_PASSWORD="${db_password}" \
		-e TZ=UTC \
		-p "${postgres_port}:5432" \
		postgres:17 >/dev/null

	started+=("${postgres_name}")

	echo "starting ${postgres_name} (postgres:17) on port ${postgres_port}"

	wait_for "${postgres_name}" pg_isready -U "${db_user}" -d "${db_name}"
}

# wait_for gives the image the time its own first-boot initialization takes. It is a readiness probe of
# the backend, not a sleep: the probe is the same one an operator would run, and the loop ends when the
# backend answers it.
wait_for() {
	local name=$1
	shift

	for _ in $(seq 1 120); do
		if "${runtime}" exec "${name}" "$@" >/dev/null 2>&1; then
			return 0
		fi

		sleep 1
	done

	echo "run.sh: ${name} did not become ready" >&2

	exit 2
}

for backend in "${backends[@]}"; do
	start "${backend}"
done

# The tests read their backend from the environment, the way the server reads its own: a host, a port, a
# name, and the credentials. Nothing else about a live run is configurable, and a live test that quietly
# fell back to sqlite would be a live test that never ran.
if [[ " ${backends[*]} " == *" mariadb "* ]]; then
	export RDIO_LIVE_MARIADB_HOST=127.0.0.1
	export RDIO_LIVE_MARIADB_PORT=${mariadb_port}
	export RDIO_LIVE_MARIADB_USER=${db_user}
	export RDIO_LIVE_MARIADB_PASSWORD=${db_password}
	export RDIO_LIVE_MARIADB_NAME=${db_name}
fi

if [[ " ${backends[*]} " == *" postgresql "* ]]; then
	export RDIO_LIVE_POSTGRESQL_HOST=127.0.0.1
	export RDIO_LIVE_POSTGRESQL_PORT=${postgres_port}
	export RDIO_LIVE_POSTGRESQL_USER=${db_user}
	export RDIO_LIVE_POSTGRESQL_PASSWORD=${db_password}
	export RDIO_LIVE_POSTGRESQL_NAME=${db_name}
fi

cd "${repo}"

echo "running the live migration tests for: ${backends[*]}"

# One backend means only that backend's tests: a MariaDB test run without a MariaDB is a failure that
# says nothing about the backend it was pointed at.
pattern=LiveOn

if [[ ${#backends[@]} -eq 1 ]]; then
	case "${backends[0]}" in
	mariadb)
		pattern=LiveOnMariaDB
		;;
	postgresql)
		pattern=LiveOnPostgreSQL
		;;
	esac
fi

# The timing run is a different set of tests, and it needs the database it times. bigdb.py writes it into
# a scratch directory: a hundred thousand calls is a hundred megabytes, and no one should have to check
# out a file like that to run a test.
if [[ -n "${big_calls}" ]]; then
	if ! command -v python3 >/dev/null; then
		echo "run.sh: --big writes its fixture with bigdb.py, and python3 is not on PATH" >&2

		exit 2
	fi

	big_dir=$(mktemp -d)
	big_fixture="${big_dir}/rdio-scanner.db"

	echo "writing a v6.6.3 deployment holding ${big_calls} calls"

	python3 "${here}/bigdb.py" "${big_fixture}" "${big_calls}"

	export RDIO_LIVE_BIG_FIXTURE=${big_fixture}
	export RDIO_LIVE_BIG_CALLS=${big_calls}

	big_env="RDIO_LIVE_BIG_FIXTURE=${big_fixture} RDIO_LIVE_BIG_CALLS=${big_calls} "

	alternatives=OnFile

	for backend in "${backends[@]}"; do
		case "${backend}" in
		mariadb)
			alternatives+='|OnMariaDB'
			;;
		postgresql)
			alternatives+='|OnPostgreSQL'
			;;
		esac
	done

	pattern="LargeDatabase(${alternatives})"
fi

go test -tags live -count=1 -v -run "${pattern}" ./internal/infrastructure/persistence/

if ${keep}; then
	cat <<EOF

kept up: ${started[*]}

	run them again:
		$(
			for backend in "${backends[@]}"; do
				case "${backend}" in
				mariadb) printf 'RDIO_LIVE_MARIADB_HOST=127.0.0.1 RDIO_LIVE_MARIADB_PORT=%s RDIO_LIVE_MARIADB_USER=%s RDIO_LIVE_MARIADB_PASSWORD=%s RDIO_LIVE_MARIADB_NAME=%s ' "${mariadb_port}" "${db_user}" "${db_password}" "${db_name}" ;;
				postgresql) printf 'RDIO_LIVE_POSTGRESQL_HOST=127.0.0.1 RDIO_LIVE_POSTGRESQL_PORT=%s RDIO_LIVE_POSTGRESQL_USER=%s RDIO_LIVE_POSTGRESQL_PASSWORD=%s RDIO_LIVE_POSTGRESQL_NAME=%s ' "${postgres_port}" "${db_user}" "${db_password}" "${db_name}" ;;
				esac
			done
		)${big_env}go test -tags live -count=1 -v -run "${pattern}" ./internal/infrastructure/persistence/

	take them down:
		${runtime} rm -f ${started[*]}
EOF

	if [[ -n "${big_fixture}" ]]; then
		cat <<EOF

	the large fixture is still at: ${big_fixture}
EOF
	fi
fi
