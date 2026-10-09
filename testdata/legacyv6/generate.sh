#!/usr/bin/env bash
# Regenerate testdata/legacyv6/rdio-scanner.db: a database shaped like one a v6.6.3
# deployment leaves behind, which is what the v6 -> v7 migration pass reads.
#
# The schema below is the v6.6.3 SQLite schema, copied from the sqlite branches of the
# migration chain in the legacy `server/database.go` at tag `v6.6.3` — the create/alter/drop
# statements of migration20191028144433 through migration20220101070000, replayed in order, with
# the tables that chain renames or replaces already replaced and its indexes kept. It is the end
# state a v6.6.3 server leaves, not any single migration's statements. The legacy source these
# statements were read from is not in this repository; the commit and tree to read them at are
# recorded in docs/specs/FEAT-005-server-ng-cutover/spec.md.
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
#   - scopes written the way the legacy server writes them: the quoted word `"*"` its write puts in the
#     column for an access that sees everything, a list of
#     `{"id":<system reference>,"talkgroups":"*"}`, and the same with the talkgroups named one by one.
#     Those are the shapes the legacy server reads its scope columns back with
#     (v6.6.3:server/access.go:347 and :354), and they are the shapes this server's scope reads, so a scope
#     carried across keeps meaning what it meant before the upgrade;
#   - dirwatches on both systems, and one whose talkgroup reference resolves to nothing;
#   - calls: one with everything, one whose frequency list is the empty array, one whose
#     frequency list is an empty string, one that names a talkgroup belonging to the other
#     system, one that names a system that does not exist, and one whose source unit is a unit
#     the migration had already migrated under a reference it threw away;
#   - the rdioScannerMeta bookkeeping table the meta migration drops.
#
# Since FEAT-011 the rows also hold every shape the migration pass reads and cannot carry, so that
# the integrity audit and the live-backend runs read them too. Each is named in the shape map of
# docs/specs/FEAT-011-v663-migration-integrity/research.md:
#
#   - talkgroup 207 and unit 304 name system 10009, which this database does not have;
#   - call 710 has no audio;
#   - log 405 has no message;
#   - downstream 503 has no url;
#   - access 904 has no code.
#
# Three shapes in the map have no row here, because a v6 table cannot hold them:
#
#   - every v6 table declares `_id` (or `id`, for calls) its primary key, so no row can arrive with a
#     null identity;
#   - `rdioScannerCalls.dateTime` is `not null`, so no call can arrive without a timestamp — and text
#     in that column is not a call without a timestamp, it is a call the pass cannot read at all, which
#     is what testdata/legacyv6partial holds;
#   - `rdioScannerConfigs.key` is `not null unique`, so a deployment has exactly one options row: an
#     options blob that will not unmarshal, or one whose value has the wrong JSON shape, is not a row
#     shape but the state of the deployment's only options row, and it is arranged in the tests that
#     prove it rather than baked into a fixture every other test reads.
#
# The last of those calls is what the legacy server cannot survive: its source unit reaches the
# branch of migrateCalls() that reports the unit insert from the branch that only runs when the
# insert succeeded, with the error it just tested for being nil, and the legacy error formatter
# calls Error() on that nil (the legacy `server/error.go:30`, reached at
# `server/migrations.go:435`). Running the legacy binary over this fixture therefore panics
# inside the calls migration and rolls the whole call set back — which is what a v6 deployment
# holding such a call gets today. This server reports the line instead of crashing; see
# TestMigrateCallsReportsNoFailureForTheUnitItCreated.
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
        # A talkgroup of a system this database does not have: the row the legacy migration dropped
        # without a word.
        (207, 1514007000, 3, 1514007, "Orphaned", None, "Orphaned", 4, 10009, 1),
    ],
)

db.executemany(
    'insert into `rdioScannerUnits` (`_id`, `id`, `label`, `order`, `systemId`) values (?, ?, ?, ?, ?)',
    [
        (301, 12345, "Engine 12", 1, 10001),
        (302, 6789, "Car 4", 2, 10001),
        # A unit of the same system that is not in this database.
        (304, 55555, "Orphan Engine", 3, 10009),
    ],
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
        # A log row with no message: the v8 log table has nothing to show for it.
        (405, stamp % (12, 15), "info", ""),
    ],
)

db.executemany(
    "insert into `rdioScannerDownstreams` (`_id`, `apiKey`, `disabled`, `order`, `systems`, `url`) values (?, ?, ?, ?, ?, ?)",
    [
        (501, "relay-api-key", 1, 1, '[{"id":10001,"talkgroups":"*"}]', "http://127.0.0.1:8080/patch"),
        # A downstream with no url to post to. Its scope names talkgroups one by one, which is the
        # other shape the v6 administrator's system picker writes.
        (503, "relay-api-key-2", 0, 2, '[{"id":10002,"talkgroups":[1514002]}]', ""),
    ],
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
        (
            # A call whose upload left no audio behind: the column is `not null`, and a v6 deployment
            # keeps the row with nothing in it.
            710,
            b"",
            "710.wav",
            "audio/wav",
            stamp % (12, 55),
            '[{"errorCount":0,"freq":1514000000,"pos":0,"spikeCount":0}]',
            1514000000,
            "[1514001]",
            12345,
            '[{"pos":1.5,"src":12345}]',
            10001,
            1514000,
        ),
    ],
)

db.executemany(
    "insert into `rdioScannerApiKeys` (`_id`, `disabled`, `ident`, `key`, `order`, `systems`) values (?, ?, ?, ?, ?, ?)",
    [
        (801, 0, "ops key", "ops-api-key", 1, '[{"id":10001,"talkgroups":"*"}]'),
        # The wildcard a v6 access or apikey starts with. `NewAccess()` holds the bare word, and the
        # legacy write turns it into the JSON the column holds: `case "*": systems = "\"*\""`
        # (v6.6.3:server/access.go:347). The v8 scope reads that column with the same json.Unmarshal the
        # legacy read uses, so the quoted form is the one that survives the upgrade.
        (802, 1, "test key", "test-api-key", 2, '"*"'),
    ],
)

db.executemany(
    "insert into `rdioScannerAccesses` (`_id`, `code`, `expiration`, `ident`, `limit`, `order`, `systems`) values (?, ?, ?, ?, ?, ?, ?)",
    [
        (901, "ABC123", stamp % (13, 0), "ops", 10, 1, '[{"id":10001,"talkgroups":[1514000,1514001]}]'),
        (902, "XYZ789", None, "guest", 0, 2, '"*"'),
        # An access code that is the empty string: a listener could reach the server with nothing.
        (904, "", stamp % (13, 10), "no code", 0, 3, '[{"id":10001,"talkgroups":"*"}]'),
    ],
)

db.commit()
db.close()
PY

echo "wrote $(pwd)/$out"
