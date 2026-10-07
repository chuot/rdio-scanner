#!/usr/bin/env bash
# Regenerate testdata/legacyv6/rdio-scanner.db: a database shaped like one a v6.6.3
# deployment leaves behind, which is what the v6 -> v7 migration pass reads.
#
# The schema below is the v6.6.3 SQLite schema, copied from the sqlite branches of the
# migration chain in server/database.go at tag v6.6.3 — the create/alter/drop statements of
# migration20191028144433 through migration20220101070000, replayed in order, with the tables
# that chain renames or replaces already replaced and its indexes kept. It is the end state a
# v6.6.3 server leaves, not any single migration's statements.
#
# The rows cover what the migration pass reads:
#
#   - groups and tags whose labels sort out of id order, and a label with an apostrophe;
#   - two systems, one with a populated blacklist and a color, one with neither;
#   - talkgroups in a group, in another group, and in no group (v6 stored 0);
#   - units on both systems;
#   - a configs table holding the options blob (with a key v7 does not know) and two rows that
#     are not the options blob;
#   - logs, one of them with an apostrophe in the message;
#   - a downstream, an apikey pair, and an access pair where one has no expiration;
#   - dirwatches on both systems, and one whose talkgroup reference resolves to nothing;
#   - calls: one with everything, one whose frequency list is the empty array, one whose
#     frequency list is an empty string, one that names a talkgroup belonging to the other
#     system, one that names a system that does not exist, and one whose source unit is a unit
#     the migration had already migrated under a reference it threw away;
#   - the rdioScannerMeta bookkeeping table the meta migration drops.
#
# The last of those calls is what the legacy server cannot survive: its source unit reaches the
# branch of migrateCalls() that reports the unit insert from the branch that only runs when the
# insert succeeded, with the error it just tested for being nil, and the legacy error formatter
# calls Error() on that nil (server/error.go:30, reached at server/migrations.go:435). Running the
# legacy binary over this fixture therefore panics inside the calls migration and rolls the whole
# call set back — which is what a v6 deployment holding such a call gets today. server-ng reports
# the line instead of crashing; see TestMigrateCallsReportsNoFailureForTheUnitItCreated.
#
# Run from any directory: bash generate.sh
set -euo pipefail

cd "$(dirname "$0")"
out=rdio-scanner.db
rm -f "$out"

python3 - "$out" <<'PY'
import sqlite3
import sys

db = sqlite3.connect(sys.argv[1])

# server/database.go at v6.6.3: the sqlite branches of the migration chain, in order.
db.executescript("""
create table `rdioScannerMeta` (name varchar(255) not null unique primary key);

create table `rdioScannerGroups` (`_id` integer primary key autoincrement, `label` varchar(255) not null);
create table `rdioScannerTags` (`_id` integer primary key autoincrement, `label` varchar(255) not null);

create table `rdioScannerSystems` (`_id` integer primary key autoincrement, `autoPopulate` tinyint(1) default 0, `blacklists` text not null, `id` integer not null unique, `label` varchar(255) not null, `led` varchar(255), `order` integer);

create table `rdioScannerTalkgroups` (`_id` integer primary key autoincrement, `frequency` integer, `groupId` integer not null, `id` integer not null, `label` varchar(255) not null, `led` varchar(255), `name` varchar(255) not null, `order` integer, `systemId` integer not null, `tagId` integer not null);
create unique index `rdio_scanner_talkgroups_system_id_id` on `rdioScannerTalkgroups` (`systemId`, `id`);

create table `rdioScannerUnits` (`_id` integer primary key autoincrement, `id` integer not null, `label` varchar(255) not null, `order` integer, `systemId` integer not null);
create unique index `rdio_scanner_units_system_id_id` on `rdioScannerUnits` (`systemId`, `id`);

create table `rdioScannerConfigs` (`_id` integer primary key autoincrement, `key` varchar(255) not null unique, `val` text not null);
create index `rdio_scanner_configs_key` on `rdioScannerConfigs` (`key`);

create table `rdioScannerLogs` (`_id` integer primary key autoincrement, `dateTime` datetime not null, `level` varchar(255) not null, `message` varchar(255) not null);
create index `rdio_scanner_logs_date_time_level` on `rdioScannerLogs` (`dateTime`, `level`);

create table `rdioScannerDownstreams` (`_id` integer primary key autoincrement, `apiKey` varchar(255) not null, `disabled` tinyint(1) default 0, `order` integer, `systems` text not null, `url` varchar(255) not null);

create table `rdioScannerDirWatches` (`_id` integer primary key autoincrement, `delay` integer default 0, `deleteAfter` tinyint(1) default 0, `directory` varchar(255) not null unique, `disabled` tinyint(1) default 0, `extension` varchar(255), `frequency` integer, `mask` varchar(255), `order` integer, `systemId` integer, `talkgroupId` integer, `type` varchar(255), `usePolling` tinyint(1) default 0);

create table `rdioScannerCalls` (`id` integer primary key autoincrement, `audio` longblob not null, `audioName` varchar(255), `audioType` varchar(255), `dateTime` datetime not null, `frequencies` text not null, `frequency` integer, `patches` text not null, `source` integer, `sources` text not null, `system` integer not null, `talkgroup` integer not null);
create index `rdio_scanner_calls_date_time_system_talkgroup` on `rdioScannerCalls` (`dateTime`, `system`, `talkgroup`);

create table `rdioScannerApiKeys` (`_id` integer primary key autoincrement, `disabled` tinyint(1) default 0, `ident` varchar(255), `key` varchar(255) not null unique, `order` integer, `systems` text not null);

create table `rdioScannerAccesses` (`_id` integer primary key autoincrement, `code` varchar(255) not null unique, `expiration` datetime, `ident` varchar(255), `limit` integer, `order` integer, `systems` text not null);
""")

# v6 wrote datetimes as text, in the layout the Go driver reads back as a time.Time:
# time.Time.String().
stamp = "2025-06-15 %02d:%02d:00.000 +0000 UTC"

options = (
    '{"audioConversion":true,"autoDetectGroups":true,"autoPopulate":false,'
    '"branding":"Rdio Legacy","dimmerDelay":30000,"disableDuplicateDetection":false,'
    '"duplicateDetectionTimeFrame":300,"email":"ops@example.com","keypadBeeps":"off",'
    '"maxClients":5,"playbackGoesLive":true,"pruneDays":30,"showListenersCount":false,'
    '"sortTalkgroups":true,"time12hFormat":false,"unknownKey":"dropped"}'
)

db.executemany(
    "insert into `rdioScannerMeta` (`name`) values (?)",
    [
        ("20210115105958-new-v5.1-tables",),
        ("20220101070000-new-v6-tables",),
    ],
)

db.executemany(
    "insert into `rdioScannerGroups` (`_id`, `label`) values (?, ?)",
    [(1, "Unknown"), (2, "Fire"), (3, "Law"), (5, "EMS"), (7, "Air"), (9, "Interop")],
)

db.executemany(
    "insert into `rdioScannerTags` (`_id`, `label`) values (?, ?)",
    [
        (1, "Air Traffic Control"),
        (2, "Interop"),
        (4, "Fire Dispatch"),
        (6, "Untagged"),
        (8, "Marshal's Office"),
    ],
)

# `id` is the radio-system reference a call upload names; `_id` is the row identity.
db.executemany(
    'insert into `rdioScannerSystems` (`_id`, `autoPopulate`, `blacklists`, `id`, `label`, `led`, `order`) values (?, ?, ?, ?, ?, ?, ?)',
    [
        (101, 1, "[40000, 40001]", 10001, "Grand Metro", "red", 1),
        (102, 0, "[]", 10002, "Harbour", None, 2),
    ],
)

# `systemId` is the v6 system reference, `groupId` the v6 group row (0 for no group), `tagId` the
# v6 tag row.
db.executemany(
    'insert into `rdioScannerTalkgroups` (`_id`, `frequency`, `groupId`, `id`, `label`, `led`, `name`, `order`, `systemId`, `tagId`) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)',
    [
        (201, 1514000000, 3, 1514000, "Dispatch 1", None, "Dispatch 1", 1, 10001, 1),
        (202, 1514001000, 7, 1514001, "Interop 9", "blue", "Interop 9", 2, 10001, 4),
        (203, 1514002000, 0, 1514002, "Un grouped", None, "Un grouped", 3, 10002, 6),
    ],
)

db.executemany(
    'insert into `rdioScannerUnits` (`_id`, `id`, `label`, `order`, `systemId`) values (?, ?, ?, ?, ?)',
    [(301, 12345, "Engine 12", 1, 10001), (302, 6789, "Car 4", 2, 10001)],
)

db.executemany(
    "insert into `rdioScannerConfigs` (`_id`, `key`, `val`) values (?, ?, ?)",
    [
        (1, "options", options),
        (2, "secret", "legacy secret value"),
        (3, "extra", '{"nested":true}'),
    ],
)

db.executemany(
    "insert into `rdioScannerLogs` (`_id`, `dateTime`, `level`, `message`) values (?, ?, ?, ?)",
    [
        (401, stamp % (12, 0), "info", "server started"),
        (402, stamp % (12, 5), "warn", "unit O'Brien keyed"),
    ],
)

db.execute(
    "insert into `rdioScannerDownstreams` (`_id`, `apiKey`, `disabled`, `order`, `systems`, `url`) values (?, ?, ?, ?, ?, ?)",
    (501, "relay-api-key", 1, 1, "[10001]", "http://127.0.0.1:8080/patch"),
)

# `systemId` and `talkgroupId` are v6 references, and both columns are nullable.
db.executemany(
    "insert into `rdioScannerDirWatches` (`_id`, `delay`, `deleteAfter`, `directory`, `disabled`, `extension`, `frequency`, `mask`, `order`, `systemId`, `talkgroupId`, `type`, `usePolling`) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
    [
        (601, 0, 0, "/var/spool/dsdplus/1514000", 0, "wav", 1514000000, "0644", 1, 10001, 1514000, "dsdPlus", 0),
        (602, 5, 1, "/var/spool/default/1514002", 1, "mp3", 1514002000, "0755", 2, 10002, 1514002, "default", 1),
        (603, 0, 0, "/var/spool/dsdplus/1514999", 0, "wav", 1514999000, "0644", 3, 10001, 1514999, "dsdPlus", 0),
    ],
)

db.executemany(
    "insert into `rdioScannerCalls` (`id`, `audio`, `audioName`, `audioType`, `dateTime`, `frequencies`, `frequency`, `patches`, `source`, `sources`, `system`, `talkgroup`) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
    [
        (
            701,
            b"\x00\x01\x02audio for call 701",
            "701.wav",
            "audio/wav",
            stamp % (12, 0),
            '[{"errorCount":0,"freq":1514000000,"pos":0,"spikeCount":0}]',
            1514000000,
            "[1514001]",
            12345,
            '[{"pos":1.5,"src":12345}]',
            10001,
            1514000,
        ),
        (
            702,
            b"\x00\x01\x02audio for call 702",
            "702.mp3",
            "audio/mp3",
            stamp % (12, 10),
            "[]",
            1514001000,
            "[]",
            99999,
            '[{"pos":2.5,"src":99999}]',
            10001,
            1514001,
        ),
        (
            703,
            b"\x00\x01\x02audio for call 703",
            "703.wav",
            "audio/wav",
            stamp % (12, 20),
            "",
            1514002000,
            "",
            0,
            "",
            10002,
            1514002,
        ),
        (
            704,
            b"\x00\x01\x02audio for call 704",
            "704.wav",
            "audio/wav",
            stamp % (12, 30),
            '[{"errorCount":3,"freq":1514002000,"pos":1.25,"spikeCount":4}]',
            1514002000,
            "[]",
            0,
            "[]",
            10001,
            1514002,
        ),
        (
            705,
            b"\x00\x01\x02audio for call 705",
            "705.wav",
            "audio/wav",
            stamp % (12, 40),
            "[]",
            1514003000,
            "[]",
            0,
            "[]",
            10003,
            1514003,
        ),
        (
            706,
            b"\x00\x01\x02audio for call 706",
            "706.wav",
            "audio/wav",
            stamp % (12, 50),
            '[{"errorCount":0,"freq":1514000000,"pos":0,"spikeCount":0}]',
            1514000000,
            "[]",
            6789,
            "",
            10001,
            1514000,
        ),
    ],
)

db.executemany(
    "insert into `rdioScannerApiKeys` (`_id`, `disabled`, `ident`, `key`, `order`, `systems`) values (?, ?, ?, ?, ?, ?)",
    [
        (801, 0, "ops key", "ops-api-key", 1, "[10001]"),
        (802, 1, "test key", "test-api-key", 2, "[10002]"),
    ],
)

db.executemany(
    "insert into `rdioScannerAccesses` (`_id`, `code`, `expiration`, `ident`, `limit`, `order`, `systems`) values (?, ?, ?, ?, ?, ?, ?)",
    [
        (901, "ABC123", stamp % (13, 0), "ops", 10, 1, "[10001]"),
        (902, "XYZ789", None, "guest", 0, 2, "[10002]"),
    ],
)

db.commit()
db.close()
PY

echo "wrote $(pwd)/$out"
