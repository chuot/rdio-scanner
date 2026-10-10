//go:build live


package persistence

// liveMariadbSchema is the v6.6.3 MariaDB schema a deployment leaves behind, as the statements a v6.6.3
// deployment ran. It is read from `server/database.go` at tag `v6.6.3` (the tree FEAT-005 pins: see
// docs/specs/FEAT-005-server-ng-cutover/spec.md), and it is the MariaDB/MySQL branch — the `else` of
// `if db.Config.DbType == DbTypeSqlite` — of the migration chain that chain replays in order. It lives
// in the package that consumes it, which is what REQ-007 asks for: no fixture directory, no file outside
// the test that proves it.

var liveMariadbSchema = []string{
	"create table `rdioScannerMeta` (name varchar(255) not null unique primary key);",
	"create table `rdioScannerAccesses` (`_id` integer primary key auto_increment, `code` varchar(255) not null unique, `expiration` datetime, `ident` varchar(255), `limit` integer, `order` integer, `systems` text not null);",
	"create table `rdioScannerApiKeys` (`_id` integer primary key auto_increment, `disabled` tinyint(1) default 0, `ident` varchar(255), `key` varchar(255) not null unique, `order` integer, `systems` text not null);",
	"create table `rdioScannerCalls` (`id` integer primary key auto_increment, `audio` longblob not null, `audioName` varchar(255), `audioType` varchar(255), `dateTime` datetime not null, `frequencies` text not null, `frequency` integer, `patches` text not null, `source` integer, `sources` text not null, `system` integer not null, `talkgroup` integer not null);",
	"create index `rdio_scanner_calls_date_time_system_talkgroup` on `rdioScannerCalls` (`dateTime`, `system`, `talkgroup`);",
	"create table `rdioScannerConfigs` (`_id` integer primary key auto_increment, `key` varchar(255) not null unique, `val` text not null);",
	"create index `rdio_scanner_configs_key` on `rdioScannerConfigs` (`key`);",
	"create table `rdioScannerDirWatches` (`_id` integer primary key auto_increment, `delay` integer default 0, `deleteAfter` tinyint(1) default 0, `directory` varchar(255) not null unique, `disabled` tinyint(1) default 0, `extension` varchar(255), `frequency` integer, `mask` varchar(255), `order` integer, `systemId` integer, `talkgroupId` integer, `type` varchar(255), `usePolling` tinyint(1) default 0);",
	"create table `rdioScannerDownstreams` (`_id` integer primary key auto_increment, `apiKey` varchar(255) not null, `disabled` tinyint(1) default 0, `order` integer, `systems` text not null, `url` varchar(255) not null);",
	"create table `rdioScannerGroups` (`_id` integer primary key auto_increment, `label` varchar(255) not null);",
	"create table `rdioScannerLogs` (`_id` integer primary key auto_increment, `dateTime` datetime not null, `level` varchar(255) not null, `message` varchar(255) not null);",
	"create index `rdio_scanner_logs_date_time_level` on `rdioScannerLogs` (`dateTime`, `level`);",
	"create table `rdioScannerSystems` (`_id` integer primary key auto_increment, `autoPopulate` tinyint(1) default 0, `blacklists` text not null, `id` integer not null unique, `label` varchar(255) not null, `led` varchar(255), `order` integer);",
	"create table `rdioScannerTags` (`_id` integer primary key auto_increment, `label` varchar(255) not null);",
	"create table `rdioScannerTalkgroups` (`_id` integer primary key auto_increment, `frequency` integer, `groupId` integer not null, `id` integer not null, `label` varchar(255) not null, `led` varchar(255), `name` varchar(255) not null, `order` integer, `systemId` integer not null, `tagId` integer not null);",
	"create unique index `rdio_scanner_talkgroups_system_id_id` on `rdioScannerTalkgroups` (`systemId`, `id`);",
	"create table `rdioScannerUnits` (`_id` integer primary key auto_increment, `id` integer not null, `label` varchar(255) not null, `order` integer, `systemId` integer not null);",
	"create unique index `rdio_scanner_units_system_id_id` on `rdioScannerUnits` (`systemId`, `id`);",
}

// livePostgresqlSchema is the same v6.6.3 schema in the shape the Postgres branch of the same chain
// leaves behind.

var livePostgresqlSchema = []string{
	`create table "rdioScannerMeta" ("name" varchar(255) not null primary key unique);`,
	`create table "rdioScannerAccesses" ("_id" serial not null primary key, "code" varchar(255) not null unique, "expiration" timestamp with time zone, "ident" varchar(255), "limit" integer, "order" integer, "systems" text not null);`,
	`create table "rdioScannerApiKeys" ("_id" serial not null primary key, "disabled" boolean not null default false, "ident" varchar(255), "key" varchar(255) not null unique, "order" integer, "systems" text not null);`,
	`create table "rdioScannerCalls" ("id" serial not null primary key, "audio" bytea not null, "audioName" varchar(255), "audioType" varchar(255), "dateTime" timestamp with time zone not null, "frequencies" text not null, "frequency" integer, "patches" text not null, "source" integer, "sources" text not null, "system" integer not null, "talkgroup" integer not null);`,
	`create index "rdio_scanner_calls_date_time_system_talkgroup" on "rdioScannerCalls" ("dateTime", "system", "talkgroup");`,
	`create table "rdioScannerConfigs" ("_id" serial not null primary key, "key" varchar(255) not null unique, "val" text not null);`,
	`create index "rdio_scanner_configs_key" on "rdioScannerConfigs" ("key");`,
	`create table "rdioScannerDirWatches" ("_id" serial not null primary key, "delay" integer default 0, "deleteAfter" boolean not null default false, "directory" varchar(255) not null unique, "disabled" boolean not null default false, "extension" varchar(255), "frequency" integer, "mask" varchar(255), "order" integer, "systemId" integer, "talkgroupId" integer, "type" varchar(255), "usePolling" boolean not null default false);`,
	`create table "rdioScannerDownstreams" ("_id" serial not null primary key, "apiKey" varchar(255) not null, "disabled" boolean not null default false, "order" integer, "systems" text not null, "url" varchar(255) not null);`,
	`create table "rdioScannerGroups" ("_id" serial not null primary key, "label" varchar(255) not null);`,
	`create table "rdioScannerLogs" ("_id" serial not null primary key, "dateTime" timestamp with time zone not null, "level" varchar(255) not null, "message" varchar(255) not null);`,
	`create index "rdio_scanner_logs_date_time_level" on "rdioScannerLogs" ("dateTime", "level");`,
	`create table "rdioScannerSystems" ("_id" serial not null primary key, "autoPopulate" boolean not null default false, "blacklists" text not null, "id" integer not null unique, "label" varchar(255) not null, "led" varchar(255), "order" integer);`,
	`create table "rdioScannerTags" ("_id" serial not null primary key, "label" varchar(255) not null);`,
	`create table "rdioScannerTalkgroups" ("_id" serial not null primary key, "frequency" integer, "groupId" integer not null, "id" integer not null, "label" varchar(255) not null, "led" varchar(255), "name" varchar(255) not null, "order" integer, "systemId" integer not null, "tagId" integer not null);`,
	`create unique index "rdio_scanner_talkgroups_system_id_id" on "rdioScannerTalkgroups" ("systemId", "id");`,
	`create table "rdioScannerUnits" ("_id" serial not null primary key, "id" integer not null, "label" varchar(255) not null, "order" integer, "systemId" integer not null);`,
	`create unique index "rdio_scanner_units_system_id_id" on "rdioScannerUnits" ("systemId", "id");`,
}
