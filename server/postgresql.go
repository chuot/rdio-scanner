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

var PostgresqlSchema = []string{
	`CREATE TABLE IF NOT EXISTS "accesses" (
    "accessId" bigserial NOT NULL PRIMARY KEY,
    "code" text NOT NULL,
    "expiration" bigint NOT NULL DEFAULT 0,
    "ident" text NOT NULL,
    "limit" integer NOT NULL DEFAULT 0,
    "order" integer NOT NULL DEFAULT 0,
    "systems" text NOT NULL DEFAULT ''
  );`,

	`CREATE TABLE IF NOT EXISTS "apikeys" (
    "apikeyId" bigserial NOT NULL PRIMARY KEY,
    "disabled" boolean NOT NULL DEFAULT false,
    "ident" text NOT NULL,
    "key" text NOT NULL,
    "order" integer NOT NULL DEFAULT 0,
    "systems" text NOT NULL DEFAULT ''
  );`,

	`CREATE TABLE IF NOT EXISTS "downstreams" (
    "downstreamId" bigserial NOT NULL PRIMARY KEY,
    "apikey" text NOT NULL,
    "disabled" boolean NOT NULL DEFAULT false,
    "order" integer NOT NULL DEFAULT 0,
    "systems" text NOT NULL DEFAULT '',
    "url" text NOT NULL
  );`,

	`CREATE TABLE IF NOT EXISTS "groups" (
    "groupId" bigserial NOT NULL PRIMARY KEY,
    "alert" text NOT NULL DEFAULT '',
    "label" text NOT NULL,
    "led" text NOT NULL DEFAULT '',
    "order" integer NOT NULL DEFAULT 0
  );`,

	`CREATE TABLE IF NOT EXISTS "tags" (
    "tagId" bigserial NOT NULL PRIMARY KEY,
    "alert" text NOT NULL DEFAULT '',
    "label" text NOT NULL,
    "led" text NOT NULL DEFAULT '',
    "order" integer NOT NULL DEFAULT 0
  );`,

	`CREATE TABLE IF NOT EXISTS "systems" (
    "systemId" bigserial NOT NULL PRIMARY KEY,
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
    "siteId" bigserial NOT NULL PRIMARY KEY,
    "label" text NOT NULL,
    "order" integer NOT NULL DEFAULT 0,
    "siteRef" integer NOT NULL,
    "systemId" bigint NOT NULL DEFAULT 0,
    CONSTRAINT "sites_systemId" FOREIGN KEY ("systemId") REFERENCES "systems" ("systemId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "talkgroups" (
    "talkgroupId" bigserial NOT NULL PRIMARY KEY,
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
    "type" TEXT NOT NULL DEFAULT '',
    CONSTRAINT "talkgroups_systemId_fkey" FOREIGN KEY ("systemId") REFERENCES "systems" ("systemId") ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT "talkgroups_tagId_fkey" FOREIGN KEY ("tagId") REFERENCES "tags" ("tagId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "talkgroupGroups" (
    "talkgroupGroupId" bigserial NOT NULL PRIMARY KEY,
    "groupId" bigint NOT NULL,
    "talkgroupId" bigint NOT NULL,
    CONSTRAINT "talkgroupGroups_groupId" FOREIGN KEY ("groupId") REFERENCES "groups" ("groupId") ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT "talkgroupGroups_talkgroupId" FOREIGN KEY ("talkgroupId") REFERENCES "talkgroups" ("talkgroupId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "calls" (
    "callId" bigserial NOT NULL PRIMARY KEY,
    "audio" bytea NOT NULL,
    "audioFilename" text NOT NULL,
    "audioMime" text NOT NULL,
    "siteRef" integer NOT NULL DEFAULT 0,
    "systemId" bigint NOT NULL,
    "talkgroupId" bigint NOT NULL,
    "timestamp" bigint NOT NULL,
    CONSTRAINT "calls_systemId" FOREIGN KEY ("systemId") REFERENCES "systems" ("systemId") ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT "calls_talkgroupId" FOREIGN KEY ("talkgroupId") REFERENCES "talkgroups" ("talkgroupId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE INDEX IF NOT EXISTS "calls_idx" ON "calls" ("systemId","talkgroupId","siteRef","timestamp");`,

	`CREATE TABLE IF NOT EXISTS "callFrequencies" (
    "callFrequencyId" bigserial NOT NULL PRIMARY KEY,
    "callId" bigint NOT NULL,
    "dbm" float NOT NULL DEFAULT 0,
    "errors" integer NOT NULL DEFAULT 0,
    "frequency" integer NOT NULL,
    "offset" float NOT NULL,
    "spikes" integer NOT NULL DEFAULT 0,
    CONSTRAINT "callFrequencies_callId" FOREIGN KEY ("callId") REFERENCES "calls" ("callId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "callPatches" (
    "callPatchId" bigserial NOT NULL PRIMARY KEY,
    "callId" bigint NOT NULL,
    "talkgroupId" bigint NOT NULL,
    CONSTRAINT "callPatches_callId" FOREIGN KEY ("callId") REFERENCES "calls" ("callId") ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT "callPatches_talkgroupId" FOREIGN KEY ("talkgroupId") REFERENCES "talkgroups" ("talkgroupId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "callUnits" (
    "callUnitId" bigserial NOT NULL PRIMARY KEY,
    "callId" bigint NOT NULL,
    "offset" float NOT NULL,
    "unitRef" integer NOT NULL,
    CONSTRAINT "callUnits_callId" FOREIGN KEY ("callId") REFERENCES "calls" ("callId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "delayed" (
    "delayedId" bigserial NOT NULL PRIMARY KEY,
    "callId" bigint NOT NULL,
    "timestamp" bigint NOT NULL,
    CONSTRAINT "delayed_callId" FOREIGN KEY ("callId") REFERENCES "calls" ("callId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,

	`CREATE TABLE IF NOT EXISTS "dirwatches" (
    "dirwatchId" bigserial NOT NULL PRIMARY KEY,
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
    "logId" bigserial NOT NULL PRIMARY KEY,
    "level" text NOT NULL,
    "message" text NOT NULL,
    "timestamp" bigint NOT NULL
  );`,

	`CREATE TABLE IF NOT EXISTS "options" (
    "optionId" bigserial NOT NULL PRIMARY KEY,
    "key" text NOT NULL,
    "value" text NOT NULL
  );`,

	`CREATE TABLE IF NOT EXISTS "units" (
    "unitId" bigserial NOT NULL PRIMARY KEY,
    "label" text NOT NULL,
    "order" integer NOT NULL DEFAULT 0,
    "systemId" bigint NOT NULL,
    "unitRef" integer NOT NULL DEFAULT 0,
    "unitFrom" text NOT NULL DEFAULT 0,
    "unitTo" text NOT NULL DEFAULT 0,
    CONSTRAINT "units_systemId" FOREIGN KEY ("systemId") REFERENCES "systems" ("systemId") ON DELETE CASCADE ON UPDATE CASCADE
  );`,
}
