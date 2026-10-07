#!/usr/bin/env bash
# Regenerate testdata/legacydamaged/migrated.db and testdata/legacydamaged/crashed.db: two
# databases a legacy server has already migrated, in the two states the legacy migration pass
# actually leaves behind.
#
#   migrated.db — the migration finished. Every v6 table is gone, the v7 tables hold the migrated
#                rows, and the damage is exactly the shape the legacy statements wrote:
#
#                  - units 301 and 302 hold no radio reference, because the legacy unit insert named
#                    an identity the unit object never had in the position the reference belongs in;
#                  - unit 303 is labelled with the number it was created for, because the legacy call
#                    migration could not find unit 302 by a reference it had just thrown away;
#                  - call 706 has no callUnits row, because the legacy migration writes that row only
#                    when the unit insert has failed.
#
#                Nothing here is recoverable. The numbers the migrated units were read for are not
#                in the database any more, and the server-ng repair invents none.
#
#   crashed.db  — the migration did not finish. The units migration committed with the same lost
#                references, the calls migration was rolled back, and the v6 calls table is still
#                there holding the real source numbers. This is the state a deployment whose upgrade
#                took the legacy server down is left in, and it is the half the repair can recover:
#                the calls and their unit links come back out of the surviving v6 table.
#
# The v7 statements below are copied from
# server-ng/internal/infrastructure/persistence/schema_sqlite.go, the same way
# ../legacyv6/generate.sh copies the v6 schema from server/database.go. Only the tables this fixture
# fills are written here; Migrate applies the rest of the schema itself. The v6 statements are copied
# from ../legacyv6/generate.sh.
#
# The damage is written as SQL that mirrors the legacy statements of server/migrations.go rather than
# by running the legacy binary, because the legacy binary cannot produce migrated.db at all: it
# panics on the call that needs a unit created, which is crashed.db.
#
# Only rdioScannerCalls survives in crashed.db. A real crashed deployment still has the v6 apikey and
# access tables too, because those migrations come after the calls one and never ran; they are left
# out here because the recovery and the report these fixtures are for are about calls and units.
#
# Run from any directory: bash generate.sh
set -euo pipefail

cd "$(dirname "$0")"

rm -f migrated.db crashed.db

python3 - <<'PY'
import datetime
import sqlite3

STAMP = "2025-06-15 %02d:%02d:00.000 +0000 UTC"


def stamp(hour, minute):
    """The v6 dateTime text, and the milliseconds since the epoch the migration stores from it."""

    text = STAMP % (hour, minute)

    # The v6 text carries its zone twice, "+0000 UTC"; the offset is the one that parses.
    parsed = datetime.datetime.strptime(text.replace(" UTC", ""), "%Y-%m-%d %H:%M:%S.%f %z")

    return text, int(parsed.timestamp() * 1000)


# server-ng/internal/infrastructure/persistence/schema_sqlite.go, the tables this fixture fills.
V7_SCHEMA = """
create table if not exists "systems" (
    "systemId" integer not null primary key autoincrement,
    "alert" text not null default '',
    "autoPopulate" integer(1) not null default 0,
    "blacklists" text not null default '',
    "delay" integer not null default 0,
    "label" text not null,
    "led" text not null default '',
    "order" integer not null default 0,
    "systemRef" integer not null,
    "type" text not null default ''
);

create table if not exists "tags" (
    "tagId" integer not null primary key autoincrement,
    "alert" text not null default '',
    "label" text not null,
    "led" text not null default '',
    "order" integer not null default 0
);

create table if not exists "talkgroups" (
    "talkgroupId" integer not null primary key autoincrement,
    "alert" text not null default '',
    "delay" integer not null default 0,
    "frequency" integer not null default 0,
    "label" text not null,
    "led" text not null default '',
    "name" text not null,
    "order" integer not null default 0,
    "systemId" integer not null,
    "tagId" integer not null,
    "talkgroupRef" integer not null,
    "type" text not null default '',
    foreign key ("systemId") references "systems" ("systemId") on delete cascade on update cascade,
    foreign key ("tagId") references "tags" ("tagId") on delete cascade on update cascade
);

create table if not exists "units" (
    "unitId" integer not null primary key autoincrement,
    "label" text not null,
    "order" integer not null default 0,
    "systemId" integer not null,
    "unitRef" integer not null default 0,
    "unitFrom" integer not null default 0,
    "unitTo" integer not null default 0,
    foreign key ("systemId") references "systems" ("systemId") on delete cascade on update cascade
);

create table if not exists "calls" (
    "callId" integer not null primary key autoincrement,
    "audio" blob not null,
    "audioFilename" text not null,
    "audioMime" text not null,
    "siteRef" integer not null default 0,
    "systemId" integer not null,
    "talkgroupId" integer not null,
    "timestamp" integer not null,
    foreign key ("systemId") references "systems" ("systemId") on delete cascade on update cascade,
    foreign key ("talkgroupId") references "talkgroups" ("talkgroupId") on delete cascade on update cascade
);

create table if not exists "callFrequencies" (
    "callFrequencyId" integer not null primary key autoincrement,
    "callId" integer not null,
    "dbm" real not null default 0,
    "errors" integer not null default 0,
    "frequency" integer not null,
    "offset" real not null,
    "spikes" integer not null default 0,
    foreign key ("callId") references "calls" ("callId") on delete cascade on update cascade
);

create table if not exists "callUnits" (
    "callUnitId" integer not null primary key autoincrement,
    "callId" integer not null,
    "offset" real not null,
    "unitRef" integer not null,
    foreign key ("callId") references "calls" ("callId") on delete cascade on update cascade
);
"""

# ../legacyv6/generate.sh, the one v6 table that survives a calls migration rolled back.
V6_CALLS_SCHEMA = """
create table `rdioScannerCalls` (`id` integer primary key autoincrement, `audio` longblob not null, `audioName` varchar(255), `audioType` varchar(255), `dateTime` datetime not null, `frequencies` text not null, `frequency` integer, `patches` text not null, `source` integer, `sources` text not null, `system` integer not null, `talkgroup` integer not null);
"""

# The three calls the v6 fixture holds on Grand Metro, in both shapes: as the migration pass reads
# them, and as it leaves them. Call 706 is the one whose source unit is unit 302, migrated under a
# reference the legacy unit migration threw away.
CALLS = [
    {
        "id": 701,
        "audio": b"\x00\x01\x02audio for call 701",
        "name": "701.wav",
        "mime": "audio/wav",
        "at": (12, 0),
        "frequencies": '[{"errorCount":0,"freq":1514000000,"pos":0,"spikeCount":0}]',
        "frequency": 1514000000,
        "patches": "[1514001]",
        "source": 12345,
        "sources": '[{"pos":1.5,"src":12345}]',
        "system": 10001,
        "talkgroup": 1514000,
        "systemId": 101,
        "talkgroupId": 201,
    },
    {
        "id": 702,
        "audio": b"\x00\x01\x02audio for call 702",
        "name": "702.mp3",
        "mime": "audio/mp3",
        "at": (12, 10),
        "frequencies": "[]",
        "frequency": 1514001000,
        "patches": "[]",
        "source": 99999,
        "sources": '[{"pos":2.5,"src":99999}]',
        "system": 10001,
        "talkgroup": 1514001,
        "systemId": 101,
        "talkgroupId": 202,
    },
    {
        "id": 706,
        "audio": b"\x00\x01\x02audio for call 706",
        "name": "706.wav",
        "mime": "audio/wav",
        "at": (12, 50),
        "frequencies": '[{"errorCount":0,"freq":1514000000,"pos":0,"spikeCount":0}]',
        "frequency": 1514000000,
        "patches": "[]",
        "source": 6789,
        "sources": "",
        "system": 10001,
        "talkgroup": 1514000,
        "systemId": 101,
        "talkgroupId": 201,
    },
]

# The callUnits rows the legacy migration writes: the multi-source calls only. Call 706 names its
# source in the single-source column, and that is the branch that writes no row at all.
CALLED_UNITS = [
    (701, 1.5, 12345),
    (702, 2.5, 99999),
]


def build(path, migrated):
    db = sqlite3.connect(path)

    db.executescript(V7_SCHEMA)

    db.executescript("""
    insert into "systems" ("systemId", "autoPopulate", "blacklists", "label", "order", "systemRef") values
        (101, 1, '[40000, 40001]', 'Grand Metro', 1, 10001),
        (102, 0, '[]', 'Harbour', 2, 10002);

    insert into "tags" ("tagId", "label", "order") values (1, 'Dispatch', 1);

    insert into "talkgroups" ("talkgroupId", "label", "name", "order", "systemId", "tagId", "talkgroupRef") values
        (201, 'Dispatch', 'Dispatch', 1, 101, 1, 1514000),
        (202, 'Fireground', 'Fireground', 2, 101, 1, 1514001),
        (203, 'Marine', 'Marine', 3, 102, 1, 1514002);

    -- The damage both databases carry, and the shape the legacy unit migration leaves in every
    -- database it runs over: the v6 rows, with the radio reference replaced by nothing.
    insert into "units" ("unitId", "label", "order", "systemId", "unitRef") values
        (301, 'Engine 12', 1, 101, 0),
        (302, 'Car 4', 2, 101, 0);
    """)

    if migrated:
        # The calls migration ran to the end. It created a unit for the number it could not find,
        # labelled with that number because the legacy statement put the number straight into the
        # label column, and it left the call that needed the unit without its source row.
        db.execute(
            'insert into "units" ("unitId", "label", "order", "systemId", "unitRef") values (303, ?, 0, 101, 6789)',
            ("6789",),
        )

        for call in CALLS:
            _, at = stamp(*call["at"])

            db.execute(
                'insert into "calls" ("callId", "audio", "audioFilename", "audioMime", "siteRef", "systemId", "talkgroupId", "timestamp") values (?, ?, ?, ?, 0, ?, ?, ?)',
                (call["id"], call["audio"], call["name"], call["mime"], call["systemId"], call["talkgroupId"], at),
            )

        for call_id, offset, unit_ref in CALLED_UNITS:
            db.execute(
                'insert into "callUnits" ("callId", "offset", "unitRef") values (?, ?, ?)',
                (call_id, offset, unit_ref),
            )

    else:
        # The calls migration was rolled back: nothing in the v7 call tables, and the v6 table still
        # holding the calls and the source numbers the recovery reads.
        db.executescript(V6_CALLS_SCHEMA)

        for call in CALLS:
            at, _ = stamp(*call["at"])

            db.execute(
                'insert into `rdioScannerCalls` (`id`, `audio`, `audioName`, `audioType`, `dateTime`, `frequencies`, `frequency`, `patches`, `source`, `sources`, `system`, `talkgroup`) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)',
                (
                    call["id"],
                    call["audio"],
                    call["name"],
                    call["mime"],
                    at,
                    call["frequencies"],
                    call["frequency"],
                    call["patches"],
                    call["source"],
                    call["sources"],
                    call["system"],
                    call["talkgroup"],
                ),
            )

    db.commit()
    db.close()


build("migrated.db", migrated=True)
build("crashed.db", migrated=False)
PY

# What the fixtures claim, read back out of the databases that were just written.
python3 - <<'PY'
import sqlite3

for name in ("migrated.db", "crashed.db"):
    db = sqlite3.connect(name)

    print(name)

    for row in db.execute('select "unitId", "label", "systemId", "unitRef" from "units" order by "unitId"'):
        print("  unit   %s" % (row,))

    for row in db.execute('select "callId", "unitRef" from "callUnits" order by "callId"'):
        print("  callUnit %s" % (row,))

    tables = {row[0] for row in db.execute("select name from sqlite_master where type = 'table'")}

    print("  calls table   %d rows" % db.execute('select count(*) from "calls"').fetchone()[0])
    print("  v6 calls      %s" % ("present, %d rows" % db.execute("select count(*) from rdioScannerCalls").fetchone()[0] if "rdioScannerCalls" in tables else "gone"))

    db.close()
PY

echo "wrote $(pwd)/migrated.db and $(pwd)/crashed.db"
