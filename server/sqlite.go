// Copyright (C) 2019-2024 Chrystian Huot <chrystian@huot.qc.ca>
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

package main

var SqliteSchema = []string{
	`CREATE TABLE IF NOT EXISTS "accesses" (
    "accessId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "code" text NOT NULL,
    "expiration" integer NOT NULL DEFAULT 0,
    "ident" text NOT NULL,
    "limit" integer NOT NULL DEFAULT 0,
    "order" integer NOT NULL DEFAULT 0,
    "systems" text NOT NULL DEFAULT ''
  );`,

	`CREATE TABLE IF NOT EXISTS "apikeys" (
    "apikeyId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "disabled" integer(1) NOT NULL DEFAULT 0,
    "ident" text NOT NULL,
    "key" text NOT NULL,
    "order" integer NOT NULL DEFAULT 0,
    "systems" text NOT NULL DEFAULT ''
  );`,

	`CREATE TABLE IF NOT EXISTS "downstreams" (
    "downstreamId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "apikey" text NOT NULL,
    "disabled" integer(1) NOT NULL DEFAULT 0,
    "order" integer NOT NULL DEFAULT 0,
    "systems" text NOT NULL DEFAULT '',
    "url" text NOT NULL
  );`,

	`CREATE TABLE IF NOT EXISTS "groups" (
    "groupId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "alert" text NOT NULL DEFAULT '',
    "label" text NOT NULL,
    "led" text NOT NULL DEFAULT '',
    "order" integer NOT NULL DEFAULT 0
  );`,

	`CREATE TABLE IF NOT EXISTS "tags" (
    "tagId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "alert" text NOT NULL DEFAULT '',
    "label" text NOT NULL,
    "led" text NOT NULL DEFAULT '',
    "order" integer NOT NULL DEFAULT 0
  );`,

	`CREATE TABLE IF NOT EXISTS "systems" (
    "systemId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "alert" text NOT NULL DEFAULT '',
    "autoPopulate" integer(1) NOT NULL DEFAULT 0,
    "blacklists" text NOT NULL DEFAULT '',
    "delay" integer NOT NULL DEFAULT 0,
    "label" text NOT NULL,
    "led" text NOT NULL DEFAULT '',
    "order" integer NOT NULL DEFAULT 0,
    "systemRef" integer NOT NULL,
    "type" text NOT NULL DEFAULT ''
  );`,

	`CREATE TABLE IF NOT EXISTS "sites" (
    "siteId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "label" text NOT NULL,
    "order" integer NOT NULL DEFAULT 0,
    "siteRef" integer NOT NULL,
    "systemId" integer NOT NULL DEFAULT 0,
    FOREIGN KEY ("systemId") REFERENCES "systems" ("systemId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "talkgroups" (
    "talkgroupId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "alert" text NOT NULL DEFAULT '',
    "delay" integer NOT NULL DEFAULT 0,
    "frequency" integer NOT NULL DEFAULT 0,
    "label" text NOT NULL,
    "led" text NOT NULL DEFAULT '',
    "name" text NOT NULL,
    "order" integer NOT NULL DEFAULT 0,
    "systemId" integer NOT NULL,
    "tagId" integer NOT NULL,
    "talkgroupRef" integer NOT NULL,
    "type" text NOT NULL DEFAULT '',
    FOREIGN KEY ("systemId") REFERENCES "systems" ("systemId") ON DELETE CASCADE ON UPDATE CASCADE,
    FOREIGN KEY ("tagId") REFERENCES "tags" ("tagId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "talkgroupGroups" (
    "talkgroupGroupId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "groupId" integer NOT NULL,
    "talkgroupId" integer NOT NULL,
    FOREIGN KEY ("groupId") REFERENCES "groups" ("groupId") ON DELETE CASCADE ON UPDATE CASCADE,
    FOREIGN KEY ("talkgroupId") REFERENCES "talkgroups" ("talkgroupId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "calls" (
    "callId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "audio" blob NOT NULL,
    "audioFilename" text NOT NULL,
    "audioMime" text NOT NULL,
    "siteRef" integer NOT NULL DEFAULT 0,
    "systemId" integer NOT NULL,
    "talkgroupId" integer NOT NULL,
    "timestamp" integer NOT NULL,
    FOREIGN KEY ("systemId") REFERENCES "systems" ("systemId") ON DELETE CASCADE ON UPDATE CASCADE,
    FOREIGN KEY ("talkgroupId") REFERENCES "talkgroups" ("talkgroupId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE INDEX IF NOT EXISTS "calls_idx" ON "calls" ("systemId","siteRef","talkgroupId","timestamp");`,

	`CREATE TABLE IF NOT EXISTS "callFrequencies" (
    "callFrequencyId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "callId" integer NOT NULL,
    "dbm" real NOT NULL DEFAULT 0,
    "errors" integer NOT NULL DEFAULT 0,
    "frequency" integer NOT NULL,
    "offset" real NOT NULL,
    "spikes" integer NOT NULL DEFAULT 0,
    FOREIGN KEY ("callId") REFERENCES "calls" ("callId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "callPatches" (
    "callPatchId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "callId" integer NOT NULL,
    "talkgroupId" integer NOT NULL,
    FOREIGN KEY ("callId") REFERENCES "calls" ("callId") ON DELETE CASCADE ON UPDATE CASCADE,
    FOREIGN KEY ("talkgroupId") REFERENCES "talkgroups" ("talkgroupId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "callUnits" (
    "callUnitId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "callId" integer NOT NULL,
    "offset" real NOT NULL,
    "unitRef" integer NOT NULL,
    FOREIGN KEY ("callId") REFERENCES "calls" ("callId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "delayed" (
    "delayedId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "callId" integer NOT NULL,
    "timestamp" integer NOT NULL,
    FOREIGN KEY ("callId") REFERENCES "calls" ("callId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "dirwatches" (
    "dirwatchId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "delay" integer NOT NULL DEFAULT 0,
    "deleteAfter" integer(1) NOT NULL DEFAULT 0,
    "directory" text NOT NULL,
    "disabled" integer(1) NOT NULL DEFAULT 0,
    "extension" text NOT NULL DEFAULT '',
    "frequency" integer NOT NULL DEFAULT 0,
    "mask" text NOT NULL DEFAULT '',
    "order" integer NOT NULL DEFAULT 0,
    "siteId" integer NOT NULL DEFAULT 0,
    "systemId" integer NOT NULL DEFAULT 0,
    "talkgroupId" integer NOT NULL DEFAULT 0,
    "type" text NOT NULL DEFAULT ''
  );`,

	`create table if not exists "logs" (
    "logid" integer not null PRIMARY KEY AUTOINCREMENT,
    "level" text not null,
    "message" text not null,
    "timestamp" integer not null
  );`,

	`CREATE TABLE IF NOT EXISTS "options" (
    "optionId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "key" text NOT NULL,
    "value" text NOT NULL
  );`,

	`CREATE TABLE IF NOT EXISTS "units" (
    "unitId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "label" text NOT NULL,
    "order" integer NOT NULL DEFAULT 0,
    "systemId" integer NOT NULL,
    "unitRef" integer NOT NULL DEFAULT 0,
    "unitFrom" integer NOT NULL DEFAULT 0,
    "unitTo" integer NOT NULL DEFAULT 0,
    FOREIGN KEY ("systemId") REFERENCES "systems" ("systemId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,
}
