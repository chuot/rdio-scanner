// Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>

package persistence

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// legacyV6Schema is the schema a v6.6.3 deployment leaves behind, recorded as the statements
// the legacy server ran to build it, and legacyV6Rows are the rows that cover every table the
// v6 → v7 migration pass reads. legacyV6PartialRows are the same rows with two of them
// carrying text in a column sqlite declared `datetime` or `integer` and does not enforce,
// which this server cannot read. They were taken from a database written by the legacy binary,
// statement by statement, and they live in the package that consumes them, which is what the
// requirement asks for: no fixture directory, no binary database file, and nothing outside the
// test that proves it.

var legacyV6Schema = []string{
	"CREATE TABLE `rdioScannerAccesses` (`_id` integer primary key autoincrement, `code` varchar(255) not null unique, `expiration` datetime, `ident` varchar(255), `limit` integer, `order` integer, `systems` text not null);",
	"CREATE TABLE `rdioScannerApiKeys` (`_id` integer primary key autoincrement, `disabled` tinyint(1) default 0, `ident` varchar(255), `key` varchar(255) not null unique, `order` integer, `systems` text not null);",
	"CREATE TABLE `rdioScannerCalls` (`id` integer primary key autoincrement, `audio` longblob not null, `audioName` varchar(255), `audioType` varchar(255), `dateTime` datetime not null, `frequencies` text not null, `frequency` integer, `patches` text not null, `source` integer, `sources` text not null, `system` integer not null, `talkgroup` integer not null);",
	"CREATE TABLE `rdioScannerConfigs` (`_id` integer primary key autoincrement, `key` varchar(255) not null unique, `val` text not null);",
	"CREATE TABLE `rdioScannerDirWatches` (`_id` integer primary key autoincrement, `delay` integer default 0, `deleteAfter` tinyint(1) default 0, `directory` varchar(255) not null unique, `disabled` tinyint(1) default 0, `extension` varchar(255), `frequency` integer, `mask` varchar(255), `order` integer, `systemId` integer, `talkgroupId` integer, `type` varchar(255), `usePolling` tinyint(1) default 0);",
	"CREATE TABLE `rdioScannerDownstreams` (`_id` integer primary key autoincrement, `apiKey` varchar(255) not null, `disabled` tinyint(1) default 0, `order` integer, `systems` text not null, `url` varchar(255) not null);",
	"CREATE TABLE `rdioScannerGroups` (`_id` integer primary key autoincrement, `label` varchar(255) not null);",
	"CREATE TABLE `rdioScannerLogs` (`_id` integer primary key autoincrement, `dateTime` datetime not null, `level` varchar(255) not null, `message` varchar(255) not null);",
	"CREATE TABLE `rdioScannerMeta` (name varchar(255) not null unique primary key);",
	"CREATE TABLE `rdioScannerSystems` (`_id` integer primary key autoincrement, `autoPopulate` tinyint(1) default 0, `blacklists` text not null, `id` integer not null unique, `label` varchar(255) not null, `led` varchar(255), `order` integer);",
	"CREATE TABLE `rdioScannerTags` (`_id` integer primary key autoincrement, `label` varchar(255) not null);",
	"CREATE TABLE `rdioScannerTalkgroups` (`_id` integer primary key autoincrement, `frequency` integer, `groupId` integer not null, `id` integer not null, `label` varchar(255) not null, `led` varchar(255), `name` varchar(255) not null, `order` integer, `systemId` integer not null, `tagId` integer not null);",
	"CREATE TABLE `rdioScannerUnits` (`_id` integer primary key autoincrement, `id` integer not null, `label` varchar(255) not null, `order` integer, `systemId` integer not null);",
	"CREATE UNIQUE INDEX `rdio_scanner_talkgroups_system_id_id` on `rdioScannerTalkgroups` (`systemId`, `id`);",
	"CREATE UNIQUE INDEX `rdio_scanner_units_system_id_id` on `rdioScannerUnits` (`systemId`, `id`);",
	"CREATE INDEX `rdio_scanner_configs_key` on `rdioScannerConfigs` (`key`);",
	"CREATE INDEX `rdio_scanner_logs_date_time_level` on `rdioScannerLogs` (`dateTime`, `level`);",
	"CREATE INDEX `rdio_scanner_calls_date_time_system_talkgroup` on `rdioScannerCalls` (`dateTime`, `system`, `talkgroup`);",
}

var legacyV6Rows = []string{
	"INSERT INTO \"rdioScannerAccesses\" VALUES(901,'ABC123','2025-06-15 13:00:00.000 +0000 UTC','ops',10,1,'[{\"id\":10001,\"talkgroups\":[1514000,1514001]}]');",
	"INSERT INTO \"rdioScannerAccesses\" VALUES(902,'XYZ789',NULL,'guest',0,2,'\"*\"');",
	"INSERT INTO \"rdioScannerAccesses\" VALUES(904,'','2025-06-15 13:10:00.000 +0000 UTC','no code',0,3,'[{\"id\":10001,\"talkgroups\":\"*\"}]');",
	"INSERT INTO \"rdioScannerApiKeys\" VALUES(801,0,'ops key','ops-api-key',1,'[{\"id\":10001,\"talkgroups\":\"*\"}]');",
	"INSERT INTO \"rdioScannerApiKeys\" VALUES(802,1,'test key','test-api-key',2,'\"*\"');",
	"INSERT INTO \"rdioScannerCalls\" VALUES(701,X'000102617564696F20666F722063616C6C20373031','701.wav','audio/wav','2025-06-15 12:00:00.000 +0000 UTC','[{\"errorCount\":0,\"freq\":1514000000,\"pos\":0,\"spikeCount\":0}]',1514000000,'[1514001]',12345,'[{\"pos\":1.5,\"src\":12345}]',10001,1514000);",
	"INSERT INTO \"rdioScannerCalls\" VALUES(702,X'000102617564696F20666F722063616C6C20373032','702.mp3','audio/mp3','2025-06-15 12:10:00.000 +0000 UTC','[]',1514001000,'[]',99999,'[{\"pos\":2.5,\"src\":99999}]',10001,1514001);",
	"INSERT INTO \"rdioScannerCalls\" VALUES(703,X'000102617564696F20666F722063616C6C20373033','703.wav','audio/wav','2025-06-15 12:20:00.000 +0000 UTC','',1514002000,'',0,'',10002,1514002);",
	"INSERT INTO \"rdioScannerCalls\" VALUES(704,X'000102617564696F20666F722063616C6C20373034','704.wav','audio/wav','2025-06-15 12:30:00.000 +0000 UTC','[{\"errorCount\":3,\"freq\":1514002000,\"pos\":1.25,\"spikeCount\":4}]',1514002000,'[]',0,'[]',10001,1514002);",
	"INSERT INTO \"rdioScannerCalls\" VALUES(705,X'000102617564696F20666F722063616C6C20373035','705.wav','audio/wav','2025-06-15 12:40:00.000 +0000 UTC','[]',1514003000,'[]',0,'[]',10003,1514003);",
	"INSERT INTO \"rdioScannerCalls\" VALUES(706,X'000102617564696F20666F722063616C6C20373036','706.wav','audio/wav','2025-06-15 12:50:00.000 +0000 UTC','[{\"errorCount\":0,\"freq\":1514000000,\"pos\":0,\"spikeCount\":0}]',1514000000,'[]',6789,'',10001,1514000);",
	"INSERT INTO \"rdioScannerCalls\" VALUES(710,X'','710.wav','audio/wav','2025-06-15 12:55:00.000 +0000 UTC','[{\"errorCount\":0,\"freq\":1514000000,\"pos\":0,\"spikeCount\":0}]',1514000000,'[1514001]',12345,'[{\"pos\":1.5,\"src\":12345}]',10001,1514000);",
	"INSERT INTO \"rdioScannerConfigs\" VALUES(1,'options','{\"audioConversion\":true,\"autoDetectGroups\":true,\"autoPopulate\":false,\"branding\":\"Rdio Legacy\",\"dimmerDelay\":30000,\"disableDuplicateDetection\":false,\"duplicateDetectionTimeFrame\":300,\"email\":\"ops@example.com\",\"keypadBeeps\":\"off\",\"maxClients\":5,\"playbackGoesLive\":true,\"pruneDays\":30,\"showListenersCount\":false,\"sortTalkgroups\":true,\"time12hFormat\":false,\"unknownKey\":\"dropped\"}');",
	"INSERT INTO \"rdioScannerConfigs\" VALUES(2,'secret','legacy secret value');",
	"INSERT INTO \"rdioScannerConfigs\" VALUES(3,'extra','{\"nested\":true}');",
	"INSERT INTO \"rdioScannerDirWatches\" VALUES(601,0,0,'/var/spool/dsdplus/1514000',0,'wav',1514000000,'0644',1,10001,1514000,'dsdPlus',0);",
	"INSERT INTO \"rdioScannerDirWatches\" VALUES(602,5,1,'/var/spool/default/1514002',1,'mp3',1514002000,'0755',2,10002,1514002,'default',1);",
	"INSERT INTO \"rdioScannerDirWatches\" VALUES(603,0,0,'/var/spool/dsdplus/1514999',0,'wav',1514999000,'0644',3,10001,1514999,'dsdPlus',0);",
	"INSERT INTO \"rdioScannerDownstreams\" VALUES(501,'relay-api-key',1,1,'[{\"id\":10001,\"talkgroups\":\"*\"}]','http://127.0.0.1:8080/patch');",
	"INSERT INTO \"rdioScannerDownstreams\" VALUES(503,'relay-api-key-2',0,2,'[{\"id\":10002,\"talkgroups\":[1514002]}]','');",
	"INSERT INTO \"rdioScannerGroups\" VALUES(1,'Unknown');",
	"INSERT INTO \"rdioScannerGroups\" VALUES(2,'Fire');",
	"INSERT INTO \"rdioScannerGroups\" VALUES(3,'Law');",
	"INSERT INTO \"rdioScannerGroups\" VALUES(5,'EMS');",
	"INSERT INTO \"rdioScannerGroups\" VALUES(7,'Air');",
	"INSERT INTO \"rdioScannerGroups\" VALUES(9,'Interop');",
	"INSERT INTO \"rdioScannerLogs\" VALUES(401,'2025-06-15 12:00:00.000 +0000 UTC','info','server started');",
	"INSERT INTO \"rdioScannerLogs\" VALUES(402,'2025-06-15 12:05:00.000 +0000 UTC','warn','unit O''Brien keyed');",
	"INSERT INTO \"rdioScannerLogs\" VALUES(405,'2025-06-15 12:15:00.000 +0000 UTC','info','');",
	"INSERT INTO \"rdioScannerMeta\" VALUES('20210115105958-new-v5.1-tables');",
	"INSERT INTO \"rdioScannerMeta\" VALUES('20220101070000-new-v6-tables');",
	"INSERT INTO \"rdioScannerSystems\" VALUES(101,1,'[40000, 40001]',10001,'Grand Metro','red',1);",
	"INSERT INTO \"rdioScannerSystems\" VALUES(102,0,'[]',10002,'Harbour',NULL,2);",
	"INSERT INTO \"rdioScannerTags\" VALUES(1,'Air Traffic Control');",
	"INSERT INTO \"rdioScannerTags\" VALUES(2,'Interop');",
	"INSERT INTO \"rdioScannerTags\" VALUES(4,'Fire Dispatch');",
	"INSERT INTO \"rdioScannerTags\" VALUES(6,'Untagged');",
	"INSERT INTO \"rdioScannerTags\" VALUES(8,'Marshal''s Office');",
	"INSERT INTO \"rdioScannerTalkgroups\" VALUES(201,1514000000,3,1514000,'Dispatch 1',NULL,'Dispatch 1',1,10001,1);",
	"INSERT INTO \"rdioScannerTalkgroups\" VALUES(202,1514001000,7,1514001,'Interop 9','blue','Interop 9',2,10001,4);",
	"INSERT INTO \"rdioScannerTalkgroups\" VALUES(203,1514002000,0,1514002,'Un grouped',NULL,'Un grouped',3,10002,6);",
	"INSERT INTO \"rdioScannerTalkgroups\" VALUES(207,1514007000,3,1514007,'Orphaned',NULL,'Orphaned',4,10009,1);",
	"INSERT INTO \"rdioScannerUnits\" VALUES(301,12345,'Engine 12',1,10001);",
	"INSERT INTO \"rdioScannerUnits\" VALUES(302,6789,'Car 4',2,10001);",
	"INSERT INTO \"rdioScannerUnits\" VALUES(304,55555,'Orphan Engine',3,10009);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerGroups',9);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerTags',8);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerSystems',102);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerTalkgroups',207);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerUnits',304);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerConfigs',3);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerLogs',405);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerDownstreams',503);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerDirWatches',603);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerCalls',710);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerApiKeys',802);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerAccesses',904);",
}

// legacyV6PartialRows is the v6.6.3 rows with the two rows this server cannot read.
var legacyV6PartialRows = []string{
	"INSERT INTO \"rdioScannerAccesses\" VALUES(901,'ABC123','2025-06-15 13:00:00.000 +0000 UTC','ops',10,1,'[{\"id\":10001,\"talkgroups\":[1514000,1514001]}]');",
	"INSERT INTO \"rdioScannerAccesses\" VALUES(902,'XYZ789',NULL,'guest',0,2,'\"*\"');",
	"INSERT INTO \"rdioScannerAccesses\" VALUES(904,'','2025-06-15 13:10:00.000 +0000 UTC','no code',0,3,'[{\"id\":10001,\"talkgroups\":\"*\"}]');",
	"INSERT INTO \"rdioScannerApiKeys\" VALUES(801,0,'ops key','ops-api-key',1,'[{\"id\":10001,\"talkgroups\":\"*\"}]');",
	"INSERT INTO \"rdioScannerApiKeys\" VALUES(802,1,'test key','test-api-key',2,'\"*\"');",
	"INSERT INTO \"rdioScannerCalls\" VALUES(701,X'000102617564696F20666F722063616C6C20373031','701.wav','audio/wav','2025-06-15 12:00:00.000 +0000 UTC','[{\"errorCount\":0,\"freq\":1514000000,\"pos\":0,\"spikeCount\":0}]',1514000000,'[1514001]',12345,'[{\"pos\":1.5,\"src\":12345}]',10001,1514000);",
	"INSERT INTO \"rdioScannerCalls\" VALUES(702,X'000102617564696F20666F722063616C6C20373032','702.mp3','audio/mp3','2025-06-15 12:10:00.000 +0000 UTC','[]',1514001000,'[]',99999,'[{\"pos\":2.5,\"src\":99999}]',10001,1514001);",
	"INSERT INTO \"rdioScannerCalls\" VALUES(703,X'000102617564696F20666F722063616C6C20373033','703.wav','audio/wav','2025-06-15 12:20:00.000 +0000 UTC','',1514002000,'',0,'',10002,1514002);",
	"INSERT INTO \"rdioScannerCalls\" VALUES(704,X'000102617564696F20666F722063616C6C20373034','704.wav','audio/wav','2025-06-15 12:30:00.000 +0000 UTC','[{\"errorCount\":3,\"freq\":1514002000,\"pos\":1.25,\"spikeCount\":4}]',1514002000,'[]',0,'[]',10001,1514002);",
	"INSERT INTO \"rdioScannerCalls\" VALUES(705,X'000102617564696F20666F722063616C6C20373035','705.wav','audio/wav','2025-06-15 12:40:00.000 +0000 UTC','[]',1514003000,'[]',0,'[]',10003,1514003);",
	"INSERT INTO \"rdioScannerCalls\" VALUES(706,X'000102617564696F20666F722063616C6C20373036','706.wav','audio/wav','2025-06-15 12:50:00.000 +0000 UTC','[{\"errorCount\":0,\"freq\":1514000000,\"pos\":0,\"spikeCount\":0}]',1514000000,'[]',6789,'',10001,1514000);",
	"INSERT INTO \"rdioScannerCalls\" VALUES(709,X'000102617564696F20666F722063616C6C20373039','709.wav','audio/wav','not a timestamp','[]',1514006000,'[]',0,'[]',10001,1514000);",
	"INSERT INTO \"rdioScannerCalls\" VALUES(710,X'','710.wav','audio/wav','2025-06-15 12:55:00.000 +0000 UTC','[{\"errorCount\":0,\"freq\":1514000000,\"pos\":0,\"spikeCount\":0}]',1514000000,'[1514001]',12345,'[{\"pos\":1.5,\"src\":12345}]',10001,1514000);",
	"INSERT INTO \"rdioScannerConfigs\" VALUES(1,'options','{\"audioConversion\":true,\"autoDetectGroups\":true,\"autoPopulate\":false,\"branding\":\"Rdio Legacy\",\"dimmerDelay\":30000,\"disableDuplicateDetection\":false,\"duplicateDetectionTimeFrame\":300,\"email\":\"ops@example.com\",\"keypadBeeps\":\"off\",\"maxClients\":5,\"playbackGoesLive\":true,\"pruneDays\":30,\"showListenersCount\":false,\"sortTalkgroups\":true,\"time12hFormat\":false,\"unknownKey\":\"dropped\"}');",
	"INSERT INTO \"rdioScannerConfigs\" VALUES(2,'secret','legacy secret value');",
	"INSERT INTO \"rdioScannerConfigs\" VALUES(3,'extra','{\"nested\":true}');",
	"INSERT INTO \"rdioScannerDirWatches\" VALUES(601,0,0,'/var/spool/dsdplus/1514000',0,'wav',1514000000,'0644',1,10001,1514000,'dsdPlus',0);",
	"INSERT INTO \"rdioScannerDirWatches\" VALUES(602,5,1,'/var/spool/default/1514002',1,'mp3',1514002000,'0755',2,10002,1514002,'default',1);",
	"INSERT INTO \"rdioScannerDirWatches\" VALUES(603,0,0,'/var/spool/dsdplus/1514999',0,'wav',1514999000,'0644',3,10001,1514999,'dsdPlus',0);",
	"INSERT INTO \"rdioScannerDownstreams\" VALUES(501,'relay-api-key',1,1,'[{\"id\":10001,\"talkgroups\":\"*\"}]','http://127.0.0.1:8080/patch');",
	"INSERT INTO \"rdioScannerDownstreams\" VALUES(503,'relay-api-key-2',0,2,'[{\"id\":10002,\"talkgroups\":[1514002]}]','');",
	"INSERT INTO \"rdioScannerGroups\" VALUES(1,'Unknown');",
	"INSERT INTO \"rdioScannerGroups\" VALUES(2,'Fire');",
	"INSERT INTO \"rdioScannerGroups\" VALUES(3,'Law');",
	"INSERT INTO \"rdioScannerGroups\" VALUES(5,'EMS');",
	"INSERT INTO \"rdioScannerGroups\" VALUES(7,'Air');",
	"INSERT INTO \"rdioScannerGroups\" VALUES(9,'Interop');",
	"INSERT INTO \"rdioScannerLogs\" VALUES(401,'2025-06-15 12:00:00.000 +0000 UTC','info','server started');",
	"INSERT INTO \"rdioScannerLogs\" VALUES(402,'2025-06-15 12:05:00.000 +0000 UTC','warn','unit O''Brien keyed');",
	"INSERT INTO \"rdioScannerLogs\" VALUES(405,'2025-06-15 12:15:00.000 +0000 UTC','info','');",
	"INSERT INTO \"rdioScannerMeta\" VALUES('20210115105958-new-v5.1-tables');",
	"INSERT INTO \"rdioScannerMeta\" VALUES('20220101070000-new-v6-tables');",
	"INSERT INTO \"rdioScannerSystems\" VALUES(101,1,'[40000, 40001]',10001,'Grand Metro','red',1);",
	"INSERT INTO \"rdioScannerSystems\" VALUES(102,0,'[]',10002,'Harbour',NULL,2);",
	"INSERT INTO \"rdioScannerTags\" VALUES(1,'Air Traffic Control');",
	"INSERT INTO \"rdioScannerTags\" VALUES(2,'Interop');",
	"INSERT INTO \"rdioScannerTags\" VALUES(4,'Fire Dispatch');",
	"INSERT INTO \"rdioScannerTags\" VALUES(6,'Untagged');",
	"INSERT INTO \"rdioScannerTags\" VALUES(8,'Marshal''s Office');",
	"INSERT INTO \"rdioScannerTalkgroups\" VALUES(201,1514000000,3,1514000,'Dispatch 1',NULL,'Dispatch 1',1,10001,1);",
	"INSERT INTO \"rdioScannerTalkgroups\" VALUES(202,1514001000,7,1514001,'Interop 9','blue','Interop 9',2,10001,4);",
	"INSERT INTO \"rdioScannerTalkgroups\" VALUES(203,1514002000,0,1514002,'Un grouped',NULL,'Un grouped',3,10002,6);",
	"INSERT INTO \"rdioScannerTalkgroups\" VALUES(206,'not a frequency',3,1514006,'Unreadable',NULL,'Unreadable',6,10001,6);",
	"INSERT INTO \"rdioScannerTalkgroups\" VALUES(207,1514007000,3,1514007,'Orphaned',NULL,'Orphaned',4,10009,1);",
	"INSERT INTO \"rdioScannerUnits\" VALUES(301,12345,'Engine 12',1,10001);",
	"INSERT INTO \"rdioScannerUnits\" VALUES(302,6789,'Car 4',2,10001);",
	"INSERT INTO \"rdioScannerUnits\" VALUES(304,55555,'Orphan Engine',3,10009);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerGroups',9);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerTags',8);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerSystems',102);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerTalkgroups',207);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerUnits',304);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerConfigs',3);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerLogs',405);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerDownstreams',503);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerDirWatches',603);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerCalls',710);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerApiKeys',802);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerAccesses',904);",
}

// The damaged fixtures are databases a legacy server has already migrated, in the two states
// the legacy migration pass actually leaves behind: migrated, where the pass finished and left
// the damage in the units and the missing source links, and crashed, where the calls migration
// was rolled back and the v6 calls table is still there. They are recorded as the statements
// the migrated database holds, statement by statement, in the package that consumes them.

var damagedMigratedSchema = []string{
	"CREATE TABLE \"callFrequencies\" (\n    \"callFrequencyId\" integer not null primary key autoincrement,\n    \"callId\" integer not null,\n    \"dbm\" real not null default 0,\n    \"errors\" integer not null default 0,\n    \"frequency\" integer not null,\n    \"offset\" real not null,\n    \"spikes\" integer not null default 0,\n    foreign key (\"callId\") references \"calls\" (\"callId\") on delete cascade on update cascade\n);",
	"CREATE TABLE \"callUnits\" (\n    \"callUnitId\" integer not null primary key autoincrement,\n    \"callId\" integer not null,\n    \"offset\" real not null,\n    \"unitRef\" integer not null,\n    foreign key (\"callId\") references \"calls\" (\"callId\") on delete cascade on update cascade\n);",
	"CREATE TABLE \"calls\" (\n    \"callId\" integer not null primary key autoincrement,\n    \"audio\" blob not null,\n    \"audioFilename\" text not null,\n    \"audioMime\" text not null,\n    \"siteRef\" integer not null default 0,\n    \"systemId\" integer not null,\n    \"talkgroupId\" integer not null,\n    \"timestamp\" integer not null,\n    foreign key (\"systemId\") references \"systems\" (\"systemId\") on delete cascade on update cascade,\n    foreign key (\"talkgroupId\") references \"talkgroups\" (\"talkgroupId\") on delete cascade on update cascade\n);",
	"CREATE TABLE \"systems\" (\n    \"systemId\" integer not null primary key autoincrement,\n    \"alert\" text not null default '',\n    \"autoPopulate\" integer(1) not null default 0,\n    \"blacklists\" text not null default '',\n    \"delay\" integer not null default 0,\n    \"label\" text not null,\n    \"led\" text not null default '',\n    \"order\" integer not null default 0,\n    \"systemRef\" integer not null,\n    \"type\" text not null default ''\n);",
	"CREATE TABLE \"tags\" (\n    \"tagId\" integer not null primary key autoincrement,\n    \"alert\" text not null default '',\n    \"label\" text not null,\n    \"led\" text not null default '',\n    \"order\" integer not null default 0\n);",
	"CREATE TABLE \"talkgroups\" (\n    \"talkgroupId\" integer not null primary key autoincrement,\n    \"alert\" text not null default '',\n    \"delay\" integer not null default 0,\n    \"frequency\" integer not null default 0,\n    \"label\" text not null,\n    \"led\" text not null default '',\n    \"name\" text not null,\n    \"order\" integer not null default 0,\n    \"systemId\" integer not null,\n    \"tagId\" integer not null,\n    \"talkgroupRef\" integer not null,\n    \"type\" text not null default '',\n    foreign key (\"systemId\") references \"systems\" (\"systemId\") on delete cascade on update cascade,\n    foreign key (\"tagId\") references \"tags\" (\"tagId\") on delete cascade on update cascade\n);",
	"CREATE TABLE \"units\" (\n    \"unitId\" integer not null primary key autoincrement,\n    \"label\" text not null,\n    \"order\" integer not null default 0,\n    \"systemId\" integer not null,\n    \"unitRef\" integer not null default 0,\n    \"unitFrom\" integer not null default 0,\n    \"unitTo\" integer not null default 0,\n    foreign key (\"systemId\") references \"systems\" (\"systemId\") on delete cascade on update cascade\n);",
}

var damagedMigratedRows = []string{
	"INSERT INTO \"sqlite_sequence\" VALUES('systems',102);",
	"INSERT INTO \"sqlite_sequence\" VALUES('tags',1);",
	"INSERT INTO \"sqlite_sequence\" VALUES('talkgroups',203);",
	"INSERT INTO \"sqlite_sequence\" VALUES('units',303);",
	"INSERT INTO \"sqlite_sequence\" VALUES('calls',706);",
	"INSERT INTO \"sqlite_sequence\" VALUES('callUnits',2);",
	"INSERT INTO \"systems\" VALUES(101,'',1,'[40000, 40001]',0,'Grand Metro','',1,10001,'');",
	"INSERT INTO \"systems\" VALUES(102,'',0,'[]',0,'Harbour','',2,10002,'');",
	"INSERT INTO \"tags\" VALUES(1,'','Dispatch','',1);",
	"INSERT INTO \"talkgroups\" VALUES(201,'',0,0,'Dispatch','','Dispatch',1,101,1,1514000,'');",
	"INSERT INTO \"talkgroups\" VALUES(202,'',0,0,'Fireground','','Fireground',2,101,1,1514001,'');",
	"INSERT INTO \"talkgroups\" VALUES(203,'',0,0,'Marine','','Marine',3,102,1,1514002,'');",
	"INSERT INTO \"units\" VALUES(301,'Engine 12',1,101,0,0,0);",
	"INSERT INTO \"units\" VALUES(302,'Car 4',2,101,0,0,0);",
	"INSERT INTO \"units\" VALUES(303,'6789',0,101,6789,0,0);",
	"INSERT INTO \"calls\" VALUES(701,X'000102617564696F20666F722063616C6C20373031','701.wav','audio/wav',0,101,201,1749988800000);",
	"INSERT INTO \"calls\" VALUES(702,X'000102617564696F20666F722063616C6C20373032','702.mp3','audio/mp3',0,101,202,1749989400000);",
	"INSERT INTO \"calls\" VALUES(706,X'000102617564696F20666F722063616C6C20373036','706.wav','audio/wav',0,101,201,1749991800000);",
	"INSERT INTO \"callUnits\" VALUES(1,701,1.5,12345);",
	"INSERT INTO \"callUnits\" VALUES(2,702,2.5,99999);",
}

var damagedCrashedSchema = []string{
	"CREATE TABLE \"callFrequencies\" (\n    \"callFrequencyId\" integer not null primary key autoincrement,\n    \"callId\" integer not null,\n    \"dbm\" real not null default 0,\n    \"errors\" integer not null default 0,\n    \"frequency\" integer not null,\n    \"offset\" real not null,\n    \"spikes\" integer not null default 0,\n    foreign key (\"callId\") references \"calls\" (\"callId\") on delete cascade on update cascade\n);",
	"CREATE TABLE \"callUnits\" (\n    \"callUnitId\" integer not null primary key autoincrement,\n    \"callId\" integer not null,\n    \"offset\" real not null,\n    \"unitRef\" integer not null,\n    foreign key (\"callId\") references \"calls\" (\"callId\") on delete cascade on update cascade\n);",
	"CREATE TABLE \"calls\" (\n    \"callId\" integer not null primary key autoincrement,\n    \"audio\" blob not null,\n    \"audioFilename\" text not null,\n    \"audioMime\" text not null,\n    \"siteRef\" integer not null default 0,\n    \"systemId\" integer not null,\n    \"talkgroupId\" integer not null,\n    \"timestamp\" integer not null,\n    foreign key (\"systemId\") references \"systems\" (\"systemId\") on delete cascade on update cascade,\n    foreign key (\"talkgroupId\") references \"talkgroups\" (\"talkgroupId\") on delete cascade on update cascade\n);",
	"CREATE TABLE `rdioScannerCalls` (`id` integer primary key autoincrement, `audio` longblob not null, `audioName` varchar(255), `audioType` varchar(255), `dateTime` datetime not null, `frequencies` text not null, `frequency` integer, `patches` text not null, `source` integer, `sources` text not null, `system` integer not null, `talkgroup` integer not null);",
	"CREATE TABLE \"systems\" (\n    \"systemId\" integer not null primary key autoincrement,\n    \"alert\" text not null default '',\n    \"autoPopulate\" integer(1) not null default 0,\n    \"blacklists\" text not null default '',\n    \"delay\" integer not null default 0,\n    \"label\" text not null,\n    \"led\" text not null default '',\n    \"order\" integer not null default 0,\n    \"systemRef\" integer not null,\n    \"type\" text not null default ''\n);",
	"CREATE TABLE \"tags\" (\n    \"tagId\" integer not null primary key autoincrement,\n    \"alert\" text not null default '',\n    \"label\" text not null,\n    \"led\" text not null default '',\n    \"order\" integer not null default 0\n);",
	"CREATE TABLE \"talkgroups\" (\n    \"talkgroupId\" integer not null primary key autoincrement,\n    \"alert\" text not null default '',\n    \"delay\" integer not null default 0,\n    \"frequency\" integer not null default 0,\n    \"label\" text not null,\n    \"led\" text not null default '',\n    \"name\" text not null,\n    \"order\" integer not null default 0,\n    \"systemId\" integer not null,\n    \"tagId\" integer not null,\n    \"talkgroupRef\" integer not null,\n    \"type\" text not null default '',\n    foreign key (\"systemId\") references \"systems\" (\"systemId\") on delete cascade on update cascade,\n    foreign key (\"tagId\") references \"tags\" (\"tagId\") on delete cascade on update cascade\n);",
	"CREATE TABLE \"units\" (\n    \"unitId\" integer not null primary key autoincrement,\n    \"label\" text not null,\n    \"order\" integer not null default 0,\n    \"systemId\" integer not null,\n    \"unitRef\" integer not null default 0,\n    \"unitFrom\" integer not null default 0,\n    \"unitTo\" integer not null default 0,\n    foreign key (\"systemId\") references \"systems\" (\"systemId\") on delete cascade on update cascade\n);",
}

var damagedCrashedRows = []string{
	"INSERT INTO \"rdioScannerCalls\" VALUES(701,X'000102617564696F20666F722063616C6C20373031','701.wav','audio/wav','2025-06-15 12:00:00.000 +0000 UTC','[{\"errorCount\":0,\"freq\":1514000000,\"pos\":0,\"spikeCount\":0}]',1514000000,'[1514001]',12345,'[{\"pos\":1.5,\"src\":12345}]',10001,1514000);",
	"INSERT INTO \"rdioScannerCalls\" VALUES(702,X'000102617564696F20666F722063616C6C20373032','702.mp3','audio/mp3','2025-06-15 12:10:00.000 +0000 UTC','[]',1514001000,'[]',99999,'[{\"pos\":2.5,\"src\":99999}]',10001,1514001);",
	"INSERT INTO \"rdioScannerCalls\" VALUES(706,X'000102617564696F20666F722063616C6C20373036','706.wav','audio/wav','2025-06-15 12:50:00.000 +0000 UTC','[{\"errorCount\":0,\"freq\":1514000000,\"pos\":0,\"spikeCount\":0}]',1514000000,'[]',6789,'',10001,1514000);",
	"INSERT INTO \"sqlite_sequence\" VALUES('systems',102);",
	"INSERT INTO \"sqlite_sequence\" VALUES('tags',1);",
	"INSERT INTO \"sqlite_sequence\" VALUES('talkgroups',203);",
	"INSERT INTO \"sqlite_sequence\" VALUES('units',302);",
	"INSERT INTO \"sqlite_sequence\" VALUES('rdioScannerCalls',706);",
	"INSERT INTO \"systems\" VALUES(101,'',1,'[40000, 40001]',0,'Grand Metro','',1,10001,'');",
	"INSERT INTO \"systems\" VALUES(102,'',0,'[]',0,'Harbour','',2,10002,'');",
	"INSERT INTO \"tags\" VALUES(1,'','Dispatch','',1);",
	"INSERT INTO \"talkgroups\" VALUES(201,'',0,0,'Dispatch','','Dispatch',1,101,1,1514000,'');",
	"INSERT INTO \"talkgroups\" VALUES(202,'',0,0,'Fireground','','Fireground',2,101,1,1514001,'');",
	"INSERT INTO \"talkgroups\" VALUES(203,'',0,0,'Marine','','Marine',3,102,1,1514002,'');",
	"INSERT INTO \"units\" VALUES(301,'Engine 12',1,101,0,0,0);",
	"INSERT INTO \"units\" VALUES(302,'Car 4',2,101,0,0,0);",
}

// buildRecordedDatabase opens a database built from the recorded statements the way the server
// opens its own database, foreign keys enforced, so that a test which writes to it cannot
// damage the recorded statements.
func buildRecordedDatabase(t *testing.T, statements []string) *sql.DB {
	t.Helper()

	db, err := Open(Settings{Dialect: DialectSqlite, FilePath: filepath.Join(t.TempDir(), "rdio-scanner.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	t.Cleanup(func() { db.Close() })

	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("the recorded statement %s does not run: %v", firstLineOf(statement), err)
		}
	}

	return db
}

// recordedDamagedStatements returns the statements of one of the damaged fixtures by name, and stops
// when the name was never recorded.
func recordedDamagedStatements(t *testing.T, name string) []string {
	t.Helper()

	switch name {
	case "migrated":
		return append(append([]string{}, damagedMigratedSchema...), damagedMigratedRows...)
	case "crashed":
		return append(append([]string{}, damagedCrashedSchema...), damagedCrashedRows...)
	}

	t.Fatalf("the damaged fixture %s is not recorded in this package", name)
	return nil
}
