#!/usr/bin/env bash
# Regenerate testdata/legacyv6partial/rdio-scanner.db: the v6.6.3 database of
# testdata/legacyv6/rdio-scanner.db, plus the rows a damaged deployment writes into a column whose
# type sqlite does not enforce and this server therefore cannot read.
#
# The two added rows are the shape FEAT-011 REQ-002 is about. sqlite gives a column declared
# `datetime` or `integer` no more force than the value written into it, so a v6 deployment that wrote
# text where a number or a timestamp belongs keeps that row, and the migration pass reads the table
# row by row and loses the row at the scan — before it has any values to archive. The legacy server
# drops the whole v6 table at the end of the pass whatever happened, so the unread row leaves the
# database with nothing said about it and nothing left behind. This server keeps the table.
#
#   - talkgroup 206 holds text in its `frequency` column;
#   - call 709 holds text in its `dateTime` column.
#
# Everything else is the same database, so the same test can show that the readable rows of those two
# tables are still carried or archived, and that the other migrations run as they do over the clean
# fixture.
#
# Run from any directory: bash generate.sh
set -euo pipefail

cd "$(dirname "$0")"
out=rdio-scanner.db
rm -f "$out"

cp ../legacyv6/rdio-scanner.db "$out"

python3 - "$out" <<'PY'
import sqlite3
import sys

db = sqlite3.connect(sys.argv[1])

# A talkgroup of a system this database does have, in a group and under a tag it does have: the only
# reason the pass cannot carry it is that it cannot read it.
db.execute(
    'insert into `rdioScannerTalkgroups` (`_id`, `frequency`, `groupId`, `id`, `label`, `led`, `name`, `order`, `systemId`, `tagId`) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)',
    (206, 'not a frequency', 3, 1514006, 'Unreadable', None, 'Unreadable', 6, 10001, 6),
)

# A call of a system and a talkgroup this database does have, with its audio: the only reason the pass
# cannot carry it is that it cannot read its timestamp.
db.execute(
    'insert into `rdioScannerCalls` (`id`, `audio`, `audioName`, `audioType`, `dateTime`, `frequencies`, `frequency`, `patches`, `source`, `sources`, `system`, `talkgroup`) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)',
    (
        709,
        b"\x00\x01\x02audio for call 709",
        "709.wav",
        "audio/wav",
        "not a timestamp",
        "[]",
        1514006000,
        "[]",
        0,
        "[]",
        10001,
        1514000,
    ),
)

db.commit()
db.close()
PY

echo "wrote $(pwd)/$out"
