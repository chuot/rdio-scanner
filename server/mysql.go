// Copyright (C) 2019-2026 Chrystian Huot <chrystian@huot.qc.ca>
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

var MysqlSchema = []string{
	`CREATE TABLE IF NOT EXISTS "accesses" (
    "accessId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "code" text NOT NULL,
    "expiration" bigint NOT NULL DEFAULT 0,
    "ident" text NOT NULL,
    "limit" integer NOT NULL DEFAULT 0,
    "order" integer NOT NULL DEFAULT 0,
    "systems" text NOT NULL DEFAULT ''
  );`,

	`CREATE TABLE IF NOT EXISTS "apikeys" (
    "apikeyId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "disabled" boolean NOT NULL DEFAULT false,
    "ident" text NOT NULL,
    "key" text NOT NULL,
    "order" integer NOT NULL DEFAULT 0,
    "systems" text NOT NULL DEFAULT ''
  );`,

	`CREATE TABLE IF NOT EXISTS "downstreams" (
    "downstreamId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "apikey" text NOT NULL,
    "disabled" boolean NOT NULL DEFAULT false,
    "order" integer NOT NULL DEFAULT 0,
    "systems" text NOT NULL DEFAULT '',
    "url" text NOT NULL
  );`,

	`CREATE TABLE IF NOT EXISTS "groups" (
    "groupId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "alert" text NOT NULL DEFAULT '',
    "label" text NOT NULL,
    "led" text NOT NULL DEFAULT '',
    "order" integer NOT NULL DEFAULT 0
  );`,

	`CREATE TABLE IF NOT EXISTS "tags" (
    "tagId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "alert" text NOT NULL DEFAULT '',
    "label" text NOT NULL,
    "led" text NOT NULL DEFAULT '',
    "order" integer NOT NULL DEFAULT 0
  );`,

	`CREATE TABLE IF NOT EXISTS "systems" (
    "systemId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "alert" text NOT NULL DEFAULT '',
    "autoPopulate" boolean NOT NULL DEFAULT false,
    "blacklists" text NOT NULL DEFAULT '',
    "delay" integer NOT NULL DEFAULT 0,
    "label" text NOT NULL,
    "led" text NOT NULL DEFAULT '',
    "order" integer NOT NULL DEFAULT 0,
    "systemRef" integer NOT NULL,
    "type" text NOT NULL DEFAULT ''
  );`,

	`CREATE TABLE IF NOT EXISTS "sites" (
    "siteId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "label" text NOT NULL,
    "order" integer NOT NULL DEFAULT 0,
    "siteRef" integer NOT NULL,
    "systemId" bigint NOT NULL DEFAULT 0,
    FOREIGN KEY ("systemId") REFERENCES "systems" ("systemId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "talkgroups" (
    "talkgroupId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "alert" text NOT NULL DEFAULT '',
    "delay" integer NOT NULL DEFAULT 0,
    "frequency" integer NOT NULL DEFAULT 0,
    "label" text NOT NULL,
    "led" text NOT NULL DEFAULT '',
    "name" text NOT NULL,
    "order" integer NOT NULL DEFAULT 0,
    "systemId" bigint NOT NULL,
    "tagId" bigint NOT NULL,
    "talkgroupRef" integer NOT NULL,
    "type" text NOT NULL DEFAULT '',
    FOREIGN KEY ("systemId") REFERENCES "systems" ("systemId") ON DELETE CASCADE ON UPDATE CASCADE,
    FOREIGN KEY ("tagId") REFERENCES "tags" ("tagId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "talkgroupGroups" (
    "talkgroupGroupId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "groupId" bigint NOT NULL,
    "talkgroupId" bigint NOT NULL,
    FOREIGN KEY ("groupId") REFERENCES "groups" ("groupId") ON DELETE CASCADE ON UPDATE CASCADE,
    FOREIGN KEY ("talkgroupId") REFERENCES "talkgroups" ("talkgroupId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "calls" (
    "callId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "audio" blob NOT NULL,
    "audioFilename" text NOT NULL,
    "audioMime" text NOT NULL,
    "siteRef" integer NOT NULL DEFAULT 0,
    "systemId" bigint NOT NULL,
    "talkgroupId" bigint NOT NULL,
    "timestamp" bigint NOT NULL,
    FOREIGN KEY ("systemId") REFERENCES "systems" ("systemId") ON DELETE CASCADE ON UPDATE CASCADE,
    FOREIGN KEY ("talkgroupId") REFERENCES "talkgroups" ("talkgroupId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE INDEX IF NOT EXISTS "calls_idx" ON "calls" ("systemId","siteRef","talkgroupId","timestamp");`,

	`CREATE TABLE IF NOT EXISTS "callFrequencies" (
    "callFrequencyId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "callId" bigint NOT NULL,
    "dbm" real NOT NULL DEFAULT 0,
    "errors" integer NOT NULL DEFAULT 0,
    "frequency" integer NOT NULL,
    "offset" real NOT NULL,
    "spikes" integer NOT NULL DEFAULT 0,
    FOREIGN KEY ("callId") REFERENCES "calls" ("callId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "callPatches" (
    "callPatchId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "callId" bigint NOT NULL,
    "talkgroupId" bigint NOT NULL,
    FOREIGN KEY ("callId") REFERENCES "calls" ("callId") ON DELETE CASCADE ON UPDATE CASCADE,
    FOREIGN KEY ("talkgroupId") REFERENCES "talkgroups" ("talkgroupId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "callUnits" (
    "callUnitId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "callId" bigint NOT NULL,
    "offset" real NOT NULL,
    "unitRef" integer NOT NULL,
    FOREIGN KEY ("callId") REFERENCES "calls" ("callId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "delayed" (
    "delayedId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "callID" bigint NOT NULL,
    "timestamp" bigint NOT NULL,
    FOREIGN KEY ("callId") REFERENCES "calls" ("callId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "dirwatches" (
    "dirwatchId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "delay" integer NOT NULL DEFAULT 0,
    "deleteAfter" boolean NOT NULL DEFAULT false,
    "directory" text NOT NULL,
    "disabled" boolean NOT NULL DEFAULT false,
    "extension" text NOT NULL DEFAULT '',
    "frequency" integer NOT NULL DEFAULT 0,
    "mask" text NOT NULL DEFAULT '',
    "order" integer NOT NULL DEFAULT 0,
    "siteId" bigint NOT NULL DEFAULT 0,
    "systemId" bigint NOT NULL DEFAULT 0,
    "talkgroupId" bigint NOT NULL DEFAULT 0,
    "type" text NOT NULL DEFAULT ''
  );`,

	`CREATE TABLE IF NOT EXISTS "logs" (
    "logId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "level" text NOT NULL,
    "message" text NOT NULL,
    "timestamp" bigint NOT NULL
  );`,

	`CREATE TABLE IF NOT EXISTS "options" (
    "optionId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "key" text NOT NULL,
    "value" text NOT NULL
  );`,

	`CREATE TABLE IF NOT EXISTS "units" (
    "unitId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "label" text NOT NULL,
    "order" integer NOT NULL DEFAULT 0,
    "systemId" bigint NOT NULL,
    "unitRef" integer NOT NULL DEFAULT 0,
    "unitFrom" integer NOT NULL DEFAULT 0,
    "unitTo" integer NOT NULL DEFAULT 0,
    FOREIGN KEY ("systemId") REFERENCES "systems" ("systemId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,
}
