-- The v6.6.3 schema, in PostgreSQL.
--
-- This one is a translation, not an extraction, and the difference has to be said. v6.6.3 has no
-- PostgreSQL backend at all: `server/config.go` at tag `v6.6.3` declares three database types — mariadb,
-- mysql, sqlite (lines 32-34) — and rejects any other name (line 177), and `server/database.go` at that
-- tag has no PostgreSQL branch anywhere in its migration chain. So no v6.6.3 deployment ever wrote these
-- tables in PostgreSQL.
--
-- What this file is for is the other half of REQ-008: this server does have a PostgreSQL backend, a
-- deployment that ran v6 on MariaDB can be moved to PostgreSQL at the same time it is upgraded, and the
-- migration pass has to be proven against the backend it will actually meet. The tables, columns,
-- nullability, uniqueness, and indexes are the ones `v663-mariadb.sql` reads out of the legacy source;
-- only the types are PostgreSQL's own, chosen to match what this server's PostgreSQL schema
-- (`internal/infrastructure/persistence/schema_postgres.go`) already chooses for the same kinds of value:
--
--   integer primary key auto_increment   ->  serial primary key
--   datetime                             ->  timestamp with time zone
--   longblob                             ->  bytea
--   tinyint(1) default 0                 ->  boolean not null default false
--   text / longtext                      ->  text
--
-- `timestamp with time zone` rather than `timestamp`: the pass reads these columns as instants, and a
-- backend that stores an instant without a zone makes the Unix-milliseconds conversion depend on the
-- session's timezone. The seed writes explicit UTC instants for the same reason.
--
-- `serial` rather than `identity` because it is what `bigserial` in this server's own PostgreSQL schema
-- is, and because it is the spelling that makes the thing worth knowing about this file visible: a
-- `serial` column has a sequence, the migration pass inserts the v6 identities into the v8 tables
-- explicitly, and a sequence is not advanced by an insert that names its own value.

create table "rdioScannerMeta" ("name" varchar(255) not null primary key unique);

create table "rdioScannerAccesses" ("_id" serial not null primary key, "code" varchar(255) not null unique, "expiration" timestamp with time zone, "ident" varchar(255), "limit" integer, "order" integer, "systems" text not null);

create table "rdioScannerApiKeys" ("_id" serial not null primary key, "disabled" boolean not null default false, "ident" varchar(255), "key" varchar(255) not null unique, "order" integer, "systems" text not null);

create table "rdioScannerCalls" ("id" serial not null primary key, "audio" bytea not null, "audioName" varchar(255), "audioType" varchar(255), "dateTime" timestamp with time zone not null, "frequencies" text not null, "frequency" integer, "patches" text not null, "source" integer, "sources" text not null, "system" integer not null, "talkgroup" integer not null);
create index "rdio_scanner_calls_date_time_system_talkgroup" on "rdioScannerCalls" ("dateTime", "system", "talkgroup");

create table "rdioScannerConfigs" ("_id" serial not null primary key, "key" varchar(255) not null unique, "val" text not null);
create index "rdio_scanner_configs_key" on "rdioScannerConfigs" ("key");

create table "rdioScannerDirWatches" ("_id" serial not null primary key, "delay" integer default 0, "deleteAfter" boolean not null default false, "directory" varchar(255) not null unique, "disabled" boolean not null default false, "extension" varchar(255), "frequency" integer, "mask" varchar(255), "order" integer, "systemId" integer, "talkgroupId" integer, "type" varchar(255), "usePolling" boolean not null default false);

create table "rdioScannerDownstreams" ("_id" serial not null primary key, "apiKey" varchar(255) not null, "disabled" boolean not null default false, "order" integer, "systems" text not null, "url" varchar(255) not null);

create table "rdioScannerGroups" ("_id" serial not null primary key, "label" varchar(255) not null);

create table "rdioScannerLogs" ("_id" serial not null primary key, "dateTime" timestamp with time zone not null, "level" varchar(255) not null, "message" varchar(255) not null);
create index "rdio_scanner_logs_date_time_level" on "rdioScannerLogs" ("dateTime", "level");

create table "rdioScannerSystems" ("_id" serial not null primary key, "autoPopulate" boolean not null default false, "blacklists" text not null, "id" integer not null unique, "label" varchar(255) not null, "led" varchar(255), "order" integer);

create table "rdioScannerTags" ("_id" serial not null primary key, "label" varchar(255) not null);

create table "rdioScannerTalkgroups" ("_id" serial not null primary key, "frequency" integer, "groupId" integer not null, "id" integer not null, "label" varchar(255) not null, "led" varchar(255), "name" varchar(255) not null, "order" integer, "systemId" integer not null, "tagId" integer not null);
create unique index "rdio_scanner_talkgroups_system_id_id" on "rdioScannerTalkgroups" ("systemId", "id");

create table "rdioScannerUnits" ("_id" serial not null primary key, "id" integer not null, "label" varchar(255) not null, "order" integer, "systemId" integer not null);
create unique index "rdio_scanner_units_system_id_id" on "rdioScannerUnits" ("systemId", "id");
