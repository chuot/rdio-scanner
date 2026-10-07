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

# capture.sh rebuilds the golden wire-protocol captures in server-ng/testdata/golden.
#
# It builds the legacy server from the source in server/, checks that source against the tree hash
# the goldens are pinned to, runs it on a throwaway SQLite database in a temporary directory, runs
# capture.mjs against it, and stops it again. Nothing under server/ is touched, and no rdio-scanner.ini
# outside the temporary directory is read or written.
#
# Usage:
#
#   ./capture.sh                    capture into ../golden
#   ./capture.sh --keep             keep the temporary directory and say where it is
#   ./capture.sh --allow-legacy-changes   capture even when server/ is not the pinned tree
#
# The legacy server is run with TZ=UTC: it renders an instant into a timestamp string in the time
# zone it runs in, so without this the captures differ between machines.

set -euo pipefail

# The git tree hash of server/ that the captures in ../golden were taken from. server/ is never
# changed, so this only moves if someone changes it, and then the goldens have to be re-taken.
legacy_server_tree="02ae33d4651fee4c46a93bdcf574d0aead89e542"

listen="127.0.0.1:3130"

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
			echo "capture.sh: unknown argument ${arg}" >&2
			exit 2
			;;
	esac
done

# --- the legacy source the goldens are pinned to ---------------------------

if ! tree=$(git -C "${repo}" rev-parse "HEAD:server" 2>&1); then
	echo "capture.sh: cannot read the git tree of server/ — ${tree}" >&2
	echo "capture.sh: pass --allow-legacy-changes to capture without the pin" >&2
	exit 1
fi

if [[ "${tree}" != "${legacy_server_tree}" ]]; then
	echo "capture.sh: server/ is git tree ${tree}, the goldens are pinned to ${legacy_server_tree}" >&2

	if [[ "${allow_legacy_changes}" != true ]]; then
		echo "capture.sh: re-take the captures, or pass --allow-legacy-changes" >&2
		exit 1
	fi

	echo "capture.sh: capturing anyway, because --allow-legacy-changes was given" >&2
fi

if [[ -n "$(git -C "${repo}" status --porcelain -- server/)" ]]; then
	echo "capture.sh: server/ has uncommitted changes, which the pin cannot see" >&2

	if [[ "${allow_legacy_changes}" != true ]]; then
		exit 1
	fi
fi

# --- the legacy server, on a database of its own ---------------------------

work=$(mktemp -d)

cleanup() {
	if [[ -n "${pid:-}" ]] && kill -0 "${pid}" 2>/dev/null; then
		kill "${pid}" 2>/dev/null || true
		wait "${pid}" 2>/dev/null || true
	fi

	if [[ "${keep}" == true ]]; then
		echo "capture.sh: the legacy server, its database and its log are in ${work}"
	else
		rm -rf "${work}"
	fi
}

trap cleanup EXIT

echo "capture.sh: building the legacy server" >&2

(cd "${repo}/server" && go build -o "${work}/rdio-scanner-legacy" .)

echo "capture.sh: starting it on ${listen}" >&2

if curl -fsS --connect-timeout 1 -o /dev/null "http://${listen}/" 2>/dev/null; then
	echo "capture.sh: something is already answering on ${listen}, and the capture would talk to it" >&2
	exit 1
fi

(cd "${work}" && TZ=UTC exec "${work}/rdio-scanner-legacy" \
	-base_dir "${work}" \
	-db_type sqlite \
	-db_file capture.db \
	-listen "${listen}") >"${work}/legacy.log" 2>&1 &

pid=$!

ready=false

for _ in $(seq 1 60); do
	if ! kill -0 "${pid}" 2>/dev/null; then
		echo "capture.sh: the legacy server stopped before it was ready:" >&2
		cat "${work}/legacy.log" >&2
		exit 1
	fi

	if curl -fsS -o /dev/null "http://${listen}/" 2>/dev/null; then
		ready=true
		break
	fi

	sleep 0.25
done

if [[ "${ready}" != true ]]; then
	echo "capture.sh: the legacy server did not answer on ${listen}:" >&2
	cat "${work}/legacy.log" >&2
	exit 1
fi

# --- the capture -----------------------------------------------------------

echo "capture.sh: capturing the wire protocol" >&2

node "${here}/capture.mjs" \
	--base "http://${listen}" \
	--out "${golden}" \
	--fixtures "${here}/fixtures/config.json"

cat >"${golden}/manifest.json" <<EOF
{
  "legacyServerTree": "${tree}",
  "timezone": "UTC",
  "placeholders": {
    "loginToken": "<token>",
    "logTime": "<datetime>"
  }
}
EOF

echo "capture.sh: the legacy server said:" >&2
sed "s/^/  /" "${work}/legacy.log" >&2
