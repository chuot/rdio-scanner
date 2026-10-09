#!/usr/bin/env python3
# Generate a database shaped like a busy v6.6.3 deployment, big enough to time the upgrade against.
#
#   usage: python3 bigdb.py <output.db> [calls] [audio bytes]
#
# This is the seed for the large-database timing (REQ-010 / CA-010 of
# docs/specs/FEAT-011-v663-migration-integrity/spec.md): `testdata/migration/run.sh --big <calls>` writes
# one of these, feeds it to the file backend and to both real backends, and records how long the migration
# pass took on each. The generator is in the repository; the database it writes is not — a 100,000-call
# file is a hundred megabytes of audio nobody should have to check out to run a test.
#
# The schema is the v6.6.3 SQLite schema, the same statements `testdata/legacyv6/generate.sh` writes, so
# what is timed here is the same shape the correctness tests read: the same columns, the same declared
# types, the same text layout in the datetime columns, the same JSON the legacy server wrote in
# `frequencies`, `patches`, `sources`, and `systems`. Only the number of rows differs.
#
# The rows are one deployment's worth of configuration — three systems, thirty talkgroups, three hundred
# units, a handful of logs, apikeys, accesses, and watched directories — and then the calls. Every call
# resolves: its system is in the database, its talkgroup belongs to that system, its source unit belongs to
# that system. That is deliberate. The point of this fixture is the cost of carrying a hundred thousand
# rows that *can* be carried; the shapes that get archived instead are in `testdata/legacyv6`, where the
# audit can read them one at a time.
#
# The audio is a fixture, not a recording: every call holds the same number of bytes of a repeating
# pattern, and the default is 1024. The timing measures moving that many blobs through each backend, so
# the recorded numbers name the bytes per call next to the row count. Change the third argument to see how
# the pass behaves on a deployment whose calls are bigger.
#
# Everything here is deterministic: the same arguments write the same file, byte for byte, so two runs of
# the timing are comparable.

import datetime
import sqlite3
import sys

out = sys.argv[1]
calls = int(sys.argv[2]) if len(sys.argv) > 2 else 100000
audio_bytes = int(sys.argv[3]) if len(sys.argv) > 3 else 1024

if calls < 1000:
    raise SystemExit("bigdb.py: the timing is for a deployment an operator waits through, not for %d calls" % calls)

# v6 wrote datetimes as text, in the layout the Go driver reads back as a time.Time: time.Time.String().
# The calls are spread over the thirty days a deployment being upgraded actually holds, ending 2025-06-15.
first_day = datetime.datetime(2025, 5, 16, 0, 0, 0)

stamp = "2025-06-15 %02d:%02d:00.000 +0000 UTC"


def v6datetime(when):
    return "%s %02d:%02d:%02d.000 +0000 UTC" % (
        when.strftime("%Y-%m-%d"),
        when.hour,
        when.minute,
        when.second,
    )

db = sqlite3.connect(out)

# server/database.go at v6.6.3: the sqlite branches of the migration chain, in order. The same block
# generate.sh writes; a timing run against a different shape would time a different database.
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

db.executemany(
    "insert into `rdioScannerMeta` (`name`) values (?)",
    [
        ("20210115105958-new-v5.1-tables",),
        ("20220101070000-new-v6-tables",),
    ],
)

db.executemany(
    "insert into `rdioScannerGroups` (`_id`, `label`) values (?, ?)",
    [
        (1, "Dispatch"),
        (2, "Fire"),
        (3, "EMS"),
        (4, "Law"),
        (5, "Interop"),
        (6, "Unknown"),
    ],
)

db.executemany(
    "insert into `rdioScannerTags` (`_id`, `label`) values (?, ?)",
    [
        (1, "Main"),
        (2, "Talker"),
        (3, "Dispatch"),
        (4, "Simple"),
    ],
)

systems = [
    (101, 10001, "Grand Metro"),
    (102, 10002, "Harbour"),
    (103, 10003, "County Wide"),
]

db.executemany(
    "insert into `rdioScannerSystems` (`_id`, `autoPopulate`, `blacklists`, `id`, `label`, `led`, `order`) values (?, ?, ?, ?, ?, ?, ?)",
    [
        (id_, 0, "[]", system_ref, label, "Green", index)
        for index, (id_, system_ref, label) in enumerate(systems)
    ],
)

# Thirty talkgroups, ten to a system. The numbers are the ones a trunked system really carries: a base
# talkgroup id and a step.
talkgroups = []

for system_index, (id_, system_ref, label) in enumerate(systems):
    for n in range(10):
        talkgroups.append(
            (
                200 + system_index * 10 + n,
                151400000 + system_ref * 1000 + n * 100,
                1 + (system_index * 10 + n) % 6,
                1514000 + system_index * 10 + n,
                "%s %d" % (label, n),
                "Green",
                "%s %d" % (label, n),
                n,
                system_ref,
                1 + n % 4,
            )
        )

db.executemany(
    "insert into `rdioScannerTalkgroups` (`_id`, `frequency`, `groupId`, `id`, `label`, `led`, `name`, `order`, `systemId`, `tagId`) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
    talkgroups,
)

# Three hundred units, a hundred to a system: the fleet a deployment that writes a hundred thousand calls
# has on the air.
units = []

for system_index, (id_, system_ref, label) in enumerate(systems):
    for n in range(100):
        units.append((300 + system_index * 100 + n, 10000 + system_index * 1000 + n, "%s Unit %02d" % (label, n), n, system_ref))

db.executemany(
    "insert into `rdioScannerUnits` (`_id`, `id`, `label`, `order`, `systemId`) values (?, ?, ?, ?, ?)",
    units,
)

# The options blob is the one row v6's configs table always holds, with the two keys v8 has no option of
# the name for, so the timing run also exercises the archive of a config row rather than only the calls.
options = (
    '{"audioConversion":true,"autoDetectGroups":true,"autoPopulate":false,'
    '"branding":"Rdio Legacy","dimmerDelay":30000,"disableDuplicateDetection":false,'
    '"duplicateDetectionTimeFrame":300,"email":"ops@example.com","keypadBeeps":"off",'
    '"maxClients":5,"playbackGoesLive":true,"pruneDays":30,"showListenersCount":false,'
    '"sortTalkgroups":true,"time12hFormat":false,"unknownKey":"dropped"}'
)

db.executemany(
    "insert into `rdioScannerConfigs` (`_id`, `key`, `val`) values (?, ?, ?)",
    [
        (1, "options", options),
        (2, "talkgroups", "{}"),
        (3, "systems", "{}"),
    ],
)

db.executemany(
    "insert into `rdioScannerLogs` (`_id`, `dateTime`, `level`, `message`) values (?, ?, ?, ?)",
    [
        (400 + n, stamp % (11, n % 60), "info" if n % 3 else "warn", "listener %d joined the deployment" % n)
        for n in range(500)
    ],
)

db.execute(
    "insert into `rdioScannerDownstreams` (`_id`, `apiKey`, `disabled`, `order`, `systems`, `url`) values (?, ?, ?, ?, ?, ?)",
    (500, "bigdb-downstream-key", 0, 0, '"*"', "https://downstream.example.com/api"),
)

db.executemany(
    "insert into `rdioScannerDirWatches` (`_id`, `delay`, `deleteAfter`, `directory`, `disabled`, `extension`, `frequency`, `mask`, `order`, `systemId`, `talkgroupId`, `type`, `usePolling`) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
    [
        (600 + n, 0, 0, "/var/spool/rdio-%d" % n, 0, ".wav", 151400000 + n, "*", n, 10001 + n, 1514000 + n, "folder", 0)
        for n in range(3)
    ],
)

db.executemany(
    "insert into `rdioScannerApiKeys` (`_id`, `disabled`, `ident`, `key`, `order`, `systems`) values (?, ?, ?, ?, ?, ?)",
    [
        (800, 0, "recorder", "bigdb-recorder-key", 0, '"*"'),
        (801, 1, "retired", "bigdb-retired-key", 1, '[{"id":10001,"talkgroups":"*"}]'),
    ],
)

db.executemany(
    "insert into `rdioScannerAccesses` (`_id`, `code`, `expiration`, `ident`, `limit`, `order`, `systems`) values (?, ?, ?, ?, ?, ?, ?)",
    [
        (900, "BIGDBAAA", "2025-06-15 13:00:00.000 +0000 UTC", "ops", 0, 0, '[{"id":10001,"talkgroups":"*"}]'),
        (901, "BIGDBBBB", None, "guest", 0, 1, '"*"'),
    ],
)

# The calls. Each one names a system, a talkgroup of that system, and a unit of that system, so the pass
# carries every row and the number measured is the cost of carrying, not the cost of archiving.
audio = bytes(bytearray((n % 251) for n in range(audio_bytes)))

rows = []

for n in range(calls):
    system_index = n % len(systems)
    id_, system_ref, label = systems[system_index]

    talkgroup = talkgroups[system_index * 10 + n % 10]
    unit = units[system_index * 100 + n % 100]

    # Spread over the thirty days: the same wall-clock shape a deployment's history has.
    offset = n * 30 * 24 * 60 * 60 // calls
    when = first_day + datetime.timedelta(seconds=offset)

    rows.append(
        (
            1000 + n,
            audio,
            "%d.wav" % (1000 + n),
            "audio/wav",
            v6datetime(when),
            '[{"errorCount":0,"freq":%d,"pos":0,"spikeCount":0}]' % talkgroup[1],
            talkgroup[1],
            "[]",
            unit[1],
            "[]",
            system_ref,
            talkgroup[3],
        )
    )

db.executemany(
    "insert into `rdioScannerCalls` (`id`, `audio`, `audioName`, `audioType`, `dateTime`, `frequencies`, `frequency`, `patches`, `source`, `sources`, `system`, `talkgroup`) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
    rows,
)

db.commit()

count = db.execute("select count(*) from rdioScannerCalls").fetchone()[0]

db.close()

print("bigdb.py: wrote %s with %d calls of %d bytes of audio each" % (out, count, audio_bytes))
