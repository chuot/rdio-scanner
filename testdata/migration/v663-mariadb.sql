-- The v6.6.3 MariaDB schema, as a v6.6.3 deployment leaves it.
--
-- Read from `server/database.go` at tag `v6.6.3` (the tree FEAT-005 pins: see
-- docs/specs/FEAT-005-server-ng-cutover/spec.md). Every statement below is the MariaDB/MySQL branch —
-- the `else` of `if db.Config.DbType == DbTypeSqlite` — of the migration chain that chain replays in
-- order, cited by the line it was read from. Like testdata/legacyv6/generate.sh, this file is the end
-- state of that replay, not the statements themselves: a table the chain renames or replaces appears
-- once, under its final name, with the indexes the chain leaves.
--
-- The replay, in the order the legacy server runs it:
--
--   prepareMigration                478   rdioScannerMeta
--   migration20191028144433         189   rdioScannerSystems (v6.0), unique index on `system`
--   migration20191029092201         207   rdioScannerCalls (v6.0), three indexes
--   migration20191126135515         225   drops two of those indexes
--   migration20191220093214         241   calls gain audioName/audioType, systems gain aliases
--   migration20200123094105         258   calls gain two indexes
--   migration20200428132918         278   systems dropped; calls rebuilt as Calls2 (dateTime,
--                                         frequencies, frequency, source, sources) and renamed
--   migration20210115105958         313   the v5.1 tables: accesses, apikeys, configs, dirwatches,
--                                         downstreams, groups, logs, tags, and systems rebuilt with
--                                         `_id`/`id`/`label`/`led`/`order` plus talkgroups/units JSON
--   migration20210830092027         348   systems' talkgroups/units widened to longtext, calls index
--                                         renamed
--   migration20211202094819         367   downstreams rebuilt without the unique apiKey
--   migration20220101070000         410   calls gain `patches`, systems lose talkgroups/units, and
--                                         rdioScannerTalkgroups and rdioScannerUnits are created
--
-- What that end state is, table for table, is the same set of tables and columns the sqlite branch
-- leaves — testdata/legacyv6/generate.sh holds that one — with MariaDB's own spelling of
-- `auto_increment`, `longblob`, and `tinyint(1)`.

-- prepareMigration, line 478.
create table `rdioScannerMeta` (name varchar(255) not null unique primary key);

-- migration20210115105958, line 313, unchanged by the rest of the chain.
create table `rdioScannerAccesses` (`_id` integer primary key auto_increment, `code` varchar(255) not null unique, `expiration` datetime, `ident` varchar(255), `limit` integer, `order` integer, `systems` text not null);

-- migration20210115105958, line 314.
create table `rdioScannerApiKeys` (`_id` integer primary key auto_increment, `disabled` tinyint(1) default 0, `ident` varchar(255), `key` varchar(255) not null unique, `order` integer, `systems` text not null);

-- migration20210115105958 line 315 (the rebuild that made frequencies and sources text), renamed at
-- line 319, carrying the `patches` column migration20220101070000 line 410 adds, with the index
-- migration20210830092027 line 351 leaves.
create table `rdioScannerCalls` (`id` integer primary key auto_increment, `audio` longblob not null, `audioName` varchar(255), `audioType` varchar(255), `dateTime` datetime not null, `frequencies` text not null, `frequency` integer, `patches` text not null, `source` integer, `sources` text not null, `system` integer not null, `talkgroup` integer not null);
create index `rdio_scanner_calls_date_time_system_talkgroup` on `rdioScannerCalls` (`dateTime`, `system`, `talkgroup`);

-- migration20210115105958, line 320.
create table `rdioScannerConfigs` (`_id` integer primary key auto_increment, `key` varchar(255) not null unique, `val` text not null);
create index `rdio_scanner_configs_key` on `rdioScannerConfigs` (`key`);

-- migration20210115105958, line 322. `usePolling` is the column v7 has no room for; see the declared
-- drop in docs/specs/FEAT-011-v663-migration-integrity/data-model.md.
create table `rdioScannerDirWatches` (`_id` integer primary key auto_increment, `delay` integer default 0, `deleteAfter` tinyint(1) default 0, `directory` varchar(255) not null unique, `disabled` tinyint(1) default 0, `extension` varchar(255), `frequency` integer, `mask` varchar(255), `order` integer, `systemId` integer, `talkgroupId` integer, `type` varchar(255), `usePolling` tinyint(1) default 0);

-- migration20211202094819, line 369: the rebuild that drops the unique apiKey.
create table `rdioScannerDownstreams` (`_id` integer primary key auto_increment, `apiKey` varchar(255) not null, `disabled` tinyint(1) default 0, `order` integer, `systems` text not null, `url` varchar(255) not null);

-- migration20210115105958, line 324.
create table `rdioScannerGroups` (`_id` integer primary key auto_increment, `label` varchar(255) not null);

-- migration20210115105958, line 325.
create table `rdioScannerLogs` (`_id` integer primary key auto_increment, `dateTime` datetime not null, `level` varchar(255) not null, `message` varchar(255) not null);
create index `rdio_scanner_logs_date_time_level` on `rdioScannerLogs` (`dateTime`, `level`);

-- migration20210115105958 line 327, with the longtext widening of migration20210830092027 line 348
-- undone by the two drop-column statements of migration20220101070000 lines 411-412.
create table `rdioScannerSystems` (`_id` integer primary key auto_increment, `autoPopulate` tinyint(1) default 0, `blacklists` text not null, `id` integer not null unique, `label` varchar(255) not null, `led` varchar(255), `order` integer);

-- migration20210115105958, line 328.
create table `rdioScannerTags` (`_id` integer primary key auto_increment, `label` varchar(255) not null);

-- migration20220101070000, lines 413-414.
create table `rdioScannerTalkgroups` (`_id` integer primary key auto_increment, `frequency` integer, `groupId` integer not null, `id` integer not null, `label` varchar(255) not null, `led` varchar(255), `name` varchar(255) not null, `order` integer, `systemId` integer not null, `tagId` integer not null);
create unique index `rdio_scanner_talkgroups_system_id_id` on `rdioScannerTalkgroups` (`systemId`, `id`);

-- migration20220101070000, lines 415-416.
create table `rdioScannerUnits` (`_id` integer primary key auto_increment, `id` integer not null, `label` varchar(255) not null, `order` integer, `systemId` integer not null);
create unique index `rdio_scanner_units_system_id_id` on `rdioScannerUnits` (`systemId`, `id`);
