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

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
)

func migrateAccesses(db *Database) error {
	var (
		err   error
		query string
		rows  *sql.Rows
		tx    *sql.Tx

		accessId   sql.NullInt64
		code       sql.NullString
		expiration sql.NullTime
		ident      sql.NullString
		limit      sql.NullInt32
		order      sql.NullInt32
		systems    sql.NullString
	)

	formatError := errorFormatter("migration", "migrateAccesses")

	if _, err = db.Sql.Exec(`SELECT COUNT(*) FROM "rdioScannerAccesses"`); err != nil {
		return nil
	}

	log.Println("migrating accesses...")

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "_id", "code", "expiration", "ident", "limit", "order", "systems" FROM "rdioScannerAccesses"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		access := NewAccess()

		if err = rows.Scan(&accessId, &code, &expiration, &ident, &limit, &order, &systems); err != nil {
			continue
		}

		if accessId.Valid {
			access.Id = uint64(accessId.Int64)
		} else {
			continue
		}

		if code.Valid && len(code.String) > 0 {
			access.Code = escapeQuotes(code.String)
		} else {
			continue
		}

		if expiration.Valid {
			access.Expiration = uint64(expiration.Time.Unix())
		}

		if ident.Valid {
			access.Ident = escapeQuotes(ident.String)
		}

		if limit.Valid {
			access.Limit = uint(limit.Int32)
		}

		if order.Valid {
			access.Order = uint(order.Int32)
		}

		if systems.Valid {
			access.Systems = systems.String
		}

		query = fmt.Sprintf(`INSERT INTO "accesses" ("accessId", "code", "expiration", "ident", "limit", "order", "systems") VALUES (%d, '%s', %d, '%s', %d, %d, '%s')`, access.Id, access.Code, access.Expiration, access.Ident, access.Limit, access.Order, access.Systems)
		if _, err = tx.Exec(query); err != nil {
			log.Println(formatError(err, query))
		}
	}

	rows.Close()

	query = `DROP TABLE "rdioScannerAccesses"`
	if _, err = tx.Exec(query); err != nil {
		log.Println(formatError(err, query))
	}

	if err = tx.Commit(); err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	return nil
}

func migrateApikeys(db *Database) error {
	var (
		err   error
		query string
		rows  *sql.Rows
		tx    *sql.Tx

		apikeyId sql.NullInt64
		disabled sql.NullBool
		ident    sql.NullString
		key      sql.NullString
		order    sql.NullInt32
		systems  sql.NullString
	)

	formatError := errorFormatter("migration", "migrateApikeys")

	if _, err = db.Sql.Exec(`SELECT COUNT(*) FROM "rdioScannerApiKeys"`); err != nil {
		return nil
	}

	log.Println("migrating apikeys...")

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "_id", "disabled", "ident", "key", "order", "systems" FROM "rdioScannerApiKeys"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		apikey := NewApikey()

		if err = rows.Scan(&apikeyId, &disabled, &ident, &key, &order, &systems); err != nil {
			continue
		}

		if apikeyId.Valid {
			apikey.Id = uint64(apikeyId.Int64)
		} else {
			continue
		}

		if disabled.Valid {
			apikey.Disabled = disabled.Bool
		}

		if ident.Valid {
			apikey.Ident = escapeQuotes(ident.String)
		}

		if key.Valid {
			apikey.Key = escapeQuotes(key.String)
		}

		if order.Valid {
			apikey.Order = uint(order.Int32)
		}

		if systems.Valid {
			apikey.Systems = systems.String
		}

		query = fmt.Sprintf(`INSERT INTO "apikeys" ("apikeyId", "disabled", "ident", "key", "order", "systems") VALUES (%d, %t, '%s', '%s', %d, '%s')`, apikey.Id, apikey.Disabled, apikey.Ident, apikey.Key, apikey.Order, apikey.Systems)
		if _, err = tx.Exec(query); err != nil {
			log.Println(formatError(err, query))
		}
	}

	rows.Close()

	query = `DROP TABLE "rdioScannerApiKeys"`
	if _, err = tx.Exec(query); err != nil {
		log.Println(formatError(err, query))
	}

	if err = tx.Commit(); err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	return nil
}

func migrateCalls(db *Database) error {
	var (
		err   error
		query string
		rows  *sql.Rows
		tx    *sql.Tx

		systems    = map[int32]int32{}
		talkgroups = map[int32]map[int32]int32{}

		timestamp int64

		callId        sql.NullInt32
		audio         sql.NullString
		audioFilename sql.NullString
		audioMime     sql.NullString
		dateTime      sql.NullTime
		frequencies   sql.NullString
		frequency     sql.NullInt32
		patches       sql.NullString
		source        sql.NullInt32
		sources       sql.NullString
		systemId      sql.NullInt32
		systemRef     sql.NullInt32
		talkgroupId   sql.NullInt32
		talkgroupRef  sql.NullInt32
	)

	formatError := errorFormatter("migration", "migrateCalls")

	if _, err = db.Sql.Exec(`SELECT COUNT(*) FROM "rdioScannerCalls"`); err != nil {
		return nil
	}

	log.Println("migrating calls...")

	query = `SELECT s."systemId", s."systemRef", t."talkgroupId", t."talkgroupRef" FROM "systems" AS s LEFT JOIN "talkgroups" AS t`
	if rows, err = db.Sql.Query(query); err != nil {
		return formatError(err, query)
	}

	for rows.Next() {
		if err = rows.Scan(&systemId, &systemRef, &talkgroupId, &talkgroupRef); err != nil {
			continue
		}

		if systemId.Valid && systemRef.Valid && talkgroupId.Valid && talkgroupRef.Valid {
			if systems[systemRef.Int32] == 0 {
				systems[systemRef.Int32] = systemId.Int32
				talkgroups[systemRef.Int32] = map[int32]int32{}
			}

			talkgroups[systemRef.Int32][talkgroupRef.Int32] = talkgroupId.Int32
		}
	}

	rows.Close()

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "id", "audio", "audioName", "audioType", "dateTime", "frequencies", "frequency", "patches", "source", "sources", "system", "talkgroup" FROM "rdioScannerCalls"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		call := NewCall()

		if err = rows.Scan(&callId, &audio, &audioFilename, &audioMime, &dateTime, &frequencies, &frequency, &patches, &source, &sources, &systemRef, &talkgroupRef); err != nil {
			continue
		}

		if callId.Valid {
			call.Id = uint64(callId.Int32)
		} else {
			continue
		}

		if audio.Valid && len(audio.String) > 0 {
			call.Audio = []byte(audio.String)
		} else {
			continue
		}

		if audioFilename.Valid {
			call.AudioFilename = escapeQuotes(audioFilename.String)
		}

		if audioMime.Valid {
			call.AudioMime = audioMime.String
		}

		if dateTime.Valid {
			timestamp = dateTime.Time.UnixMilli()
		} else {
			continue
		}

		if !systemRef.Valid || systems[systemRef.Int32] == 0 {
			continue
		}

		if !talkgroupRef.Valid || talkgroups[systemRef.Int32][talkgroupRef.Int32] == 0 {
			continue
		}

		if db.Config.DbType == DbTypePostgresql {
			query = fmt.Sprintf(`INSERT INTO "calls" ("callId", "audio", "audioFilename", "audioMime", "siteRef", "systemId", "talkgroupId", "timestamp") VALUES (%d, $1, '%s', '%s', 0, %d, %d, %d)`, call.Id, call.AudioFilename, call.AudioMime, systems[systemRef.Int32], talkgroups[systemRef.Int32][talkgroupRef.Int32], timestamp)

		} else {
			query = fmt.Sprintf(`INSERT INTO "calls" ("callId", "audio", "audioFilename", "audioMime", "siteRef", "systemId", "talkgroupId", "timestamp") VALUES (%d, ?, '%s', '%s', 0, %d, %d, %d)`, call.Id, call.AudioFilename, call.AudioMime, systems[systemRef.Int32], talkgroups[systemRef.Int32][talkgroupRef.Int32], timestamp)
		}

		if _, err = tx.Exec(query, call.Audio); err == nil {
			if frequencies.Valid && len(frequencies.String) > 0 {
				var f any
				if err = json.Unmarshal([]byte(frequencies.String), &f); err == nil {
					switch v := f.(type) {
					case []any:
						for _, v := range v {
							switch m := v.(type) {
							case map[string]any:
								var (
									errorCount uint
									freq       uint
									pos        float64
									spikeCount uint
								)

								switch v := m["errorCount"].(type) {
								case float64:
									errorCount = uint(v)
								}

								switch v := m["freq"].(type) {
								case float64:
									freq = uint(v)
								}

								switch v := m["pos"].(type) {
								case float64:
									pos = v
								}

								switch v := m["spikeCount"].(type) {
								case float64:
									spikeCount = uint(v)
								}

								query = fmt.Sprintf(`INSERT INTO "callFrequencies" ("callId", "errors", "frequency", "offset", "spikes") VALUES (%d, %d, %d, %f, %d)`, call.Id, errorCount, freq, pos, spikeCount)
								if _, err = tx.Exec(query); err != nil {
									log.Println(formatError(err, query))
								}
							}
						}
					}
				}

			} else if frequency.Valid && frequency.Int32 > 0 {
				query = fmt.Sprintf(`INSERT INTO "callFrequencies" ("callId", "errors", "frequency", "offset", "spikes") VALUES (%d, 0, %d, 0, 0)`, call.Id, frequency.Int32)
				if _, err = tx.Exec(query); err != nil {
					log.Println(formatError(err, query))
				}
			}

			if patches.Valid && len(patches.String) > 0 {
				var f any
				if err = json.Unmarshal([]byte(patches.String), &f); err == nil {
					switch v := f.(type) {
					case []any:
						for _, v := range v {
							switch i := v.(type) {
							case float64:
								if i := talkgroups[systemRef.Int32][int32(i)]; i > 0 {
									query = fmt.Sprintf(`INSERT INTO "callPatches" ("callId", "talkgroupId") VALUES (%d, %d)`, call.Id, i)
									if _, err = tx.Exec(query); err != nil {
										log.Println(formatError(err, query))
									}
								}
							}
						}
					}
				}
			}

			if sources.Valid && len(sources.String) > 0 && sources.String != "[]" {
				var f any
				if err = json.Unmarshal([]byte(sources.String), &f); err == nil {
					switch v := f.(type) {
					case []any:
						for _, v := range v {
							switch m := v.(type) {
							case map[string]any:
								switch src := (m["src"]).(type) {
								case float64:
									if src > 0 {
										query = fmt.Sprintf(`INSERT INTO "callUnits" ("callId", "offset", "unitRef") VALUES (%d, %f, %f)`, call.Id, m["pos"], src)
										if _, err = tx.Exec(query); err != nil {
											log.Println(formatError(err, query))
										}
									}
								}
							}
						}
					}
				}

			} else if source.Valid && source.Int32 > 0 {
				var c int
				query = fmt.Sprintf(`SELECT COUNT(*) FROM "units" WHERE "systemId" = %d AND "unitRef" = %d`, systems[systemRef.Int32], source.Int32)
				if err = tx.QueryRow(query).Scan(&c); err == nil && c == 0 {
					query = fmt.Sprintf(`INSERT INTO "units" ("label", "systemId", "unitRef") VALUES(%d, %d, %d)`, source.Int32, systems[systemRef.Int32], source.Int32)

					if err == nil {
						query = fmt.Sprintf(`INSERT INTO "callUnits" ("callId", "offset", "unitRef") VALUES (%d, %d, %d)`, call.Id, 0, source.Int32)
						if _, err = tx.Exec(query); err != nil {
							log.Println(formatError(err, query))
						}

					} else {
						log.Println(formatError(err, query))
					}

				} else if err != nil {
					log.Println(formatError(err, query))
				}
			}

		} else {
			log.Println(formatError(err, query))
		}
	}

	rows.Close()

	query = `DROP TABLE "rdioScannerCalls"`
	if _, err = tx.Exec(query); err != nil {
		log.Println(formatError(err, query))
	}

	if err = tx.Commit(); err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	return nil
}

func migrateDirwatches(db *Database) error {
	var (
		err   error
		query string
		rows  *sql.Rows
		tx    *sql.Tx

		systems    = map[int32]int32{}
		talkgroups = map[int32]map[int32]int32{}

		refSystem    any
		refTalkgroup any

		delay        sql.NullInt32
		deleteAfter  sql.NullBool
		directory    sql.NullString
		dirwatchId   sql.NullInt64
		disabled     sql.NullBool
		extension    sql.NullString
		frequency    sql.NullInt32
		kind         sql.NullString
		mask         sql.NullString
		order        sql.NullInt32
		systemId     sql.NullInt32
		systemRef    sql.NullInt32
		talkgroupId  sql.NullInt32
		talkgroupRef sql.NullInt32
	)

	formatError := errorFormatter("migration", "migrateDirwatches")

	if _, err = db.Sql.Exec(`SELECT COUNT(*) FROM "rdioScannerDirwatches"`); err != nil {
		return nil
	}

	log.Println("migrating dirwatches...")

	query = `SELECT s."systemId", s."systemRef", t."talkgroupId", t."talkgroupRef" FROM "systems" AS s LEFT JOIN "talkgroups" AS t`
	if rows, err = db.Sql.Query(query); err != nil {
		return formatError(err, query)
	}

	for rows.Next() {
		if err = rows.Scan(&systemId, &systemRef, &talkgroupId, &talkgroupRef); err != nil {
			continue
		}

		if systemId.Valid && systemRef.Valid && talkgroupId.Valid && talkgroupRef.Valid {
			if systems[systemRef.Int32] == 0 {
				systems[systemRef.Int32] = systemId.Int32
				talkgroups[systemRef.Int32] = map[int32]int32{}
			}

			talkgroups[systemRef.Int32][talkgroupRef.Int32] = talkgroupId.Int32
		}
	}

	rows.Close()

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "_id", "delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "systemId", "talkgroupId", "type" FROM "rdioScannerDirwatches"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		dirwatch := NewDirwatch()

		if err = rows.Scan(&dirwatchId, &delay, &deleteAfter, &directory, &disabled, &extension, &frequency, &mask, &order, &systemRef, &talkgroupRef, &kind); err != nil {
			continue
		}

		if dirwatchId.Valid {
			dirwatch.Id = uint64(dirwatchId.Int64)
		} else {
			continue
		}

		if delay.Valid {
			dirwatch.Delay = uint(delay.Int32)
		}

		if deleteAfter.Valid {
			dirwatch.DeleteAfter = deleteAfter.Bool
		}

		if directory.Valid && len(directory.String) > 0 {
			dirwatch.Directory = escapeQuotes(directory.String)
		} else {
			continue
		}

		if disabled.Valid {
			dirwatch.Disabled = disabled.Bool
		}

		if extension.Valid {
			dirwatch.Extension = escapeQuotes(extension.String)
		}

		if frequency.Valid {
			dirwatch.Frequency = uint(frequency.Int32)
		}

		if mask.Valid && len(mask.String) > 0 {
			dirwatch.Mask = escapeQuotes(mask.String)
		}

		if kind.Valid && len(kind.String) > 0 {
			dirwatch.Kind = kind.String
		}

		if order.Valid {
			dirwatch.Order = uint(order.Int32)
		}

		if systemRef.Valid && systems[systemRef.Int32] > 0 {
			refSystem = systems[systemRef.Int32]
		} else {
			refSystem = nil
		}

		if talkgroupId.Valid {
			refTalkgroup = talkgroups[systemRef.Int32][talkgroupRef.Int32]
		} else {
			refTalkgroup = nil
		}

		query = fmt.Sprintf(`INSERT INTO "dirwatches" ("dirwatchId", "delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "systemId", "talkgroupId", "type") VALUES (%d, %d, %t, '%s', %t, '%s', %d, '%s', %d, %d, %d, '%s')`, dirwatch.Id, dirwatch.Delay, dirwatch.DeleteAfter, dirwatch.Directory, dirwatch.Disabled, dirwatch.Extension, dirwatch.Frequency, dirwatch.Mask, dirwatch.Order, refSystem, refTalkgroup, dirwatch.Kind)
		if _, err = tx.Exec(query); err != nil {
			log.Println(formatError(err, query))
		}
	}

	rows.Close()

	query = `DROP TABLE "RdioScannerDirWatches"`
	if _, err = tx.Exec(`DROP TABLE "RdioScannerDirWatches"`); err != nil {
		log.Println(formatError(err, query))
	}

	if err = tx.Commit(); err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	return nil
}

func migrateDownstreams(db *Database) error {
	var (
		err   error
		query string
		rows  *sql.Rows
		tx    *sql.Tx

		apikey       sql.NullString
		disabled     sql.NullBool
		downstreamId sql.NullInt64
		order        sql.NullInt32
		systems      sql.NullString
		url          sql.NullString
	)

	formatError := errorFormatter("migration", "migrateDownstreams")

	if _, err = db.Sql.Exec(`SELECT COUNT(*) FROM "rdioScannerDownstreams"`); err != nil {
		return nil
	}

	log.Println("migrating downstreams...")

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "_id", "apiKey", "disabled", "order", "systems", "url" FROM "rdioScannerDownstreams"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		downstream := NewDownstream(nil)

		if err = rows.Scan(&downstreamId, &apikey, &disabled, &order, &systems, &url); err != nil {
			continue
		}

		if downstreamId.Valid {
			downstream.Id = uint64(downstreamId.Int64)
		} else {
			continue
		}

		if apikey.Valid && len(apikey.String) > 0 {
			downstream.Apikey = escapeQuotes(apikey.String)
		} else {
			continue
		}

		if disabled.Valid {
			downstream.Disabled = disabled.Bool
		}

		if order.Valid {
			downstream.Order = uint(order.Int32)
		}

		if systems.Valid && len(systems.String) > 0 {
			downstream.Systems = systems.String
		} else {
			continue
		}

		if url.Valid && len(url.String) > 0 {
			downstream.Url = escapeQuotes(url.String)
		} else {
			continue
		}

		query = fmt.Sprintf(`INSERT INTO "downstreams" ("downstreamId", "apikey", "disabled", "order", "systems", "url") VALUES (%d, '%s', %t, %d, '%s', '%s')`, downstream.Id, downstream.Apikey, downstream.Disabled, downstream.Order, downstream.Systems, downstream.Url)
		if _, err = tx.Exec(query); err != nil {
			log.Println(formatError(err, query))
		}
	}

	rows.Close()

	query = `DROP TABLE "rdioScannerDownstreams"`
	if _, err = tx.Exec(query); err != nil {
		log.Println(formatError(err, query))
	}

	if err = tx.Commit(); err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	return nil
}

func migrateGroups(db *Database) error {
	var (
		err   error
		query string
		rows  *sql.Rows
		tx    *sql.Tx

		groups  = []*Group{}
		groupId sql.NullInt32
		label   sql.NullString
	)

	formatError := errorFormatter("migration", "migrateGroups")

	if _, err = db.Sql.Exec(`SELECT COUNT(*) FROM "rdioScannerGroups"`); err != nil {
		return nil
	}

	log.Println("migrating groups...")

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "_id", "label" FROM "rdioScannerGroups"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		group := NewGroup()

		if err = rows.Scan(&groupId, &label); err != nil {
			continue
		}

		if groupId.Valid {
			group.Id = uint64(groupId.Int32)
		} else {
			continue
		}

		if label.Valid {
			group.Label = escapeQuotes(label.String)
		}

		groups = append(groups, group)
	}

	rows.Close()

	sort.Slice(groups, func(i int, j int) bool {
		return groups[i].Label < groups[j].Label
	})

	for i, group := range groups {
		group.Order = uint(i + 1)

		query = fmt.Sprintf(`INSERT INTO "groups" ("groupId", "label", "order") VALUES (%d, '%s', %d)`, group.Id, group.Label, group.Order)
		if _, err = tx.Exec(query); err != nil {
			log.Println(formatError(err, query))
		}
	}

	query = `DROP TABLE "rdioScannerGroups"`
	if _, err = tx.Exec(query); err != nil {
		log.Println(formatError(err, query))
	}

	if err = tx.Commit(); err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	return nil
}

func migrateLogs(db *Database) error {
	var (
		err   error
		query string
		rows  *sql.Rows
		tx    *sql.Tx

		timestamp int64

		dateTime sql.NullTime
		level    sql.NullString
		logId    sql.NullInt32
		message  sql.NullString
	)

	formatError := errorFormatter("migration", "migrateLogs")

	if _, err = db.Sql.Exec(`SELECT COUNT(*) FROM "rdioScannerLogs"`); err != nil {
		return nil
	}

	log.Println("migrating logs...")

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "_id", "dateTime", "level", "message" FROM "rdioScannerLogs"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		l := NewLog()

		if err = rows.Scan(&logId, &dateTime, &level, &message); err != nil {
			continue
		}

		if logId.Valid {
			l.Id = uint(logId.Int32)
		} else {
			continue
		}

		if dateTime.Valid {
			timestamp = dateTime.Time.UnixMilli()
		} else {
			continue
		}

		if level.Valid && len(level.String) > 0 {
			l.Level = level.String
		} else {
			continue
		}

		if message.Valid && len(message.String) > 0 {
			l.Message = escapeQuotes(message.String)
		} else {
			continue
		}

		query = fmt.Sprintf(`INSERT INTO "logs" ("logId", "level", "message", "timestamp") VALUES (%d, '%s', '%s', %d)`, l.Id, l.Level, l.Message, timestamp)
		if _, err = tx.Exec(query); err != nil {
			log.Println(formatError(err, query))
		}
	}

	rows.Close()

	query = `DROP TABLE "rdioScannerLogs"`
	if _, err = tx.Exec(query); err != nil {
		log.Println(formatError(err, query))
	}

	if err = tx.Commit(); err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	return nil
}

func migrateMeta(db *Database) error {
	formatError := errorFormatter("migration", "migrateMeta")

	if _, err := db.Sql.Exec(`SELECT COUNT(*) FROM "rdioScannerMeta"`); err != nil {
		return nil
	}

	log.Println("migrating meta...")

	query := `DROP TABLE "rdioScannerMeta"`
	if _, err := db.Sql.Exec(query); err != nil {
		return formatError(err, query)
	}

	return nil
}

func migrateOptions(db *Database) error {
	var (
		err   error
		query string
		rows  *sql.Rows
		tx    *sql.Tx

		key   sql.NullString
		value sql.NullString
	)

	formatError := errorFormatter("migration", "migrateOptions")

	if _, err = db.Sql.Exec(`SELECT COUNT(*) FROM "rdioScannerConfigs"`); err != nil {
		return nil
	}

	log.Println("migrating options...")

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "key", "val" FROM "rdioScannerConfigs"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		if err = rows.Scan(&key, &value); err != nil {
			continue
		}

		if !key.Valid || !value.Valid {
			continue
		}

		if key.String == "options" {
			var m map[string]any

			if err = json.Unmarshal([]byte(value.String), &m); err == nil {
				switch v := m["audioConversion"].(type) {
				case bool:
					if b, err := json.Marshal(v); err == nil {
						query = fmt.Sprintf(`INSERT INTO "options" ("key", "value") VALUES ('%s', '%s')`, "audioConversion", string(b))
						if _, err = tx.Exec(query); err != nil {
							log.Println(formatError(err, query))
						}
					}
				}
				switch v := m["autoPopulate"].(type) {
				case bool:
					if b, err := json.Marshal(v); err == nil {
						query = fmt.Sprintf(`INSERT INTO "options" ("key", "value") VALUES ('%s', '%s')`, "autoPopulate", string(b))
						if _, err = tx.Exec(query); err != nil {
							log.Println(formatError(err, query))
						}
					}
				}
				switch v := m["branding"].(type) {
				case string:
					if b, err := json.Marshal(v); err == nil {
						query = fmt.Sprintf(`INSERT INTO "options" ("key", "value") VALUES ('%s', '%s')`, "branding", escapeQuotes(string(b)))
						if _, err = tx.Exec(query); err != nil {
							log.Println(formatError(err, query))
						}
					}
				}
				switch v := m["dimmerDelay"].(type) {
				case float64:
					if b, err := json.Marshal(v); err == nil {
						query = fmt.Sprintf(`INSERT INTO "options" ("key", "value") VALUES ('%s', '%s')`, "dimmerDelay", string(b))
						if _, err = tx.Exec(query); err != nil {
							log.Println(formatError(err, query))
						}
					}
				}
				switch v := m["disableDuplicateDetection"].(type) {
				case bool:
					if b, err := json.Marshal(v); err == nil {
						query = fmt.Sprintf(`INSERT INTO "options" ("key", "value") VALUES ('%s', '%s')`, "disableDuplicateDetection", string(b))
						if _, err = tx.Exec(query); err != nil {
							log.Println(formatError(err, query))
						}
					}
				}
				switch v := m["duplicateDetectionTimeFrame"].(type) {
				case float64:
					if b, err := json.Marshal(v); err == nil {
						query = fmt.Sprintf(`INSERT INTO "options" ("key", "value") VALUES ('%s', '%s')`, "duplicateDetectionTimeFrame", string(b))
						if _, err = tx.Exec(query); err != nil {
							log.Println(formatError(err, query))
						}
					}
				}
				switch v := m["email"].(type) {
				case string:
					if b, err := json.Marshal(v); err == nil {
						query = fmt.Sprintf(`INSERT INTO "options" ("key", "value") VALUES ('%s', '%s')`, "email", escapeQuotes(string(b)))
						if _, err = tx.Exec(query); err != nil {
							log.Println(formatError(err, query))
						}
					}
				}
				switch v := m["keypadBeeps"].(type) {
				case string:
					if b, err := json.Marshal(v); err == nil {
						query = fmt.Sprintf(`INSERT INTO "options" ("key", "value") VALUES ('%s', '%s')`, "keypadBeeps", string(b))
						if _, err = tx.Exec(query); err != nil {
							log.Println(formatError(err, query))
						}
					}
				}
				switch v := m["maxClients"].(type) {
				case float64:
					if b, err := json.Marshal(v); err == nil {
						query = fmt.Sprintf(`INSERT INTO "options" ("key", "value") VALUES ('%s', '%s')`, "maxClients", string(b))
						if _, err = tx.Exec(query); err != nil {
							log.Println(formatError(err, query))
						}
					}
				}
				switch v := m["playbackGoesLive"].(type) {
				case bool:
					if b, err := json.Marshal(v); err == nil {
						query = fmt.Sprintf(`INSERT INTO "options" ("key", "value") VALUES ('%s', '%s')`, "playbackGoesLive", string(b))
						if _, err = tx.Exec(query); err != nil {
							log.Println(formatError(err, query))
						}
					}
				}
				switch v := m["pruneDays"].(type) {
				case float64:
					if b, err := json.Marshal(v); err == nil {
						query = fmt.Sprintf(`INSERT INTO "options" ("key", "value") VALUES ('%s', '%s')`, "pruneDays", string(b))
						if _, err = tx.Exec(query); err != nil {
							log.Println(formatError(err, query))
						}
					}
				}
				switch v := m["showListenersCount"].(type) {
				case bool:
					if b, err := json.Marshal(v); err == nil {
						query = fmt.Sprintf(`INSERT INTO "options" ("key", "value") VALUES ('%s', '%s')`, "showListenersCount", string(b))
						if _, err = tx.Exec(query); err != nil {
							log.Println(formatError(err, query))
						}
					}
				}
				switch v := m["sortTalkgroups"].(type) {
				case bool:
					if b, err := json.Marshal(v); err == nil {
						query = fmt.Sprintf(`INSERT INTO "options" ("key", "value") VALUES ('%s', '%s')`, "sortTalkgroups", string(b))
						if _, err = tx.Exec(query); err != nil {
							log.Println(formatError(err, query))
						}
					}
				}
				switch v := m["time12hFormat"].(type) {
				case bool:
					if b, err := json.Marshal(v); err == nil {
						query = fmt.Sprintf(`INSERT INTO "options" ("key", "value") VALUES ('%s', '%s')`, "time12hFormat", string(b))
						if _, err = tx.Exec(query); err != nil {
							log.Println(formatError(err, query))
						}
					}
				}
			}

		} else {
			query = fmt.Sprintf(`INSERT INTO "options" ("key", "value") VALUES ('%s', '%s')`, escapeQuotes(key.String), escapeQuotes(value.String))
			if _, err = tx.Exec(query); err != nil {
				log.Println(formatError(err, query))
			}
		}
	}

	rows.Close()

	query = `DROP TABLE "rdioScannerConfigs"`
	if _, err = tx.Exec(query); err != nil {
		log.Println(formatError(err, query))
	}

	if err = tx.Commit(); err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	return nil
}

func migrateSystems(db *Database) error {
	var (
		err   error
		query string
		rows  *sql.Rows
		tx    *sql.Tx

		autoPopulate sql.NullBool
		blacklists   sql.NullString
		label        sql.NullString
		led          sql.NullString
		order        sql.NullInt32
		systemId     sql.NullInt64
		systemRef    sql.NullInt32
	)

	formatError := errorFormatter("migration", "migrateSystems")

	if _, err = db.Sql.Exec(`SELECT COUNT(*) FROM "rdioScannerSystems"`); err != nil {
		return nil
	}

	log.Println("migrating systems...")

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "_id", "autoPopulate", "blacklists", "id", "label", "led", "order" FROM "rdioScannerSystems"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		system := NewSystem()

		if err = rows.Scan(&systemId, &autoPopulate, &blacklists, &systemRef, &label, &led, &order); err != nil {
			continue
		}

		if systemId.Valid {
			system.Id = uint64(systemId.Int64)
		} else {
			continue
		}

		if autoPopulate.Valid {
			system.AutoPopulate = autoPopulate.Bool
		}

		if blacklists.Valid {
			system.Blacklists = Blacklists(strings.ReplaceAll(strings.ReplaceAll(blacklists.String, "[", ""), "]", ""))
		}

		if label.Valid {
			system.Label = escapeQuotes(label.String)
		}

		if led.Valid {
			system.Led = led.String
		}

		if order.Valid {
			system.Order = uint(order.Int32)
		}

		if systemRef.Valid {
			system.SystemRef = uint(systemRef.Int32)
		}

		query = fmt.Sprintf(`INSERT INTO "systems" ("systemId", "autoPopulate", "blacklists", "label", "led", "order", "systemRef") VALUES (%d, %t, '%s', '%s', '%s', %d, %d)`, system.Id, system.AutoPopulate, system.Blacklists, system.Label, system.Led, system.Order, system.SystemRef)
		if _, err = tx.Exec(query); err != nil {
			log.Println(formatError(err, query))
		}
	}

	rows.Close()

	query = `DROP TABLE "rdioScannerSystems"`
	if _, err = tx.Exec(query); err != nil {
		log.Println(formatError(err, query))
	}

	if err = tx.Commit(); err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	return nil
}

func migrateTags(db *Database) error {
	var (
		err   error
		query string
		rows  *sql.Rows
		tx    *sql.Tx

		label sql.NullString
		tags  = []*Tag{}
		tagId sql.NullInt32
	)

	formatError := errorFormatter("migration", "migrateTags")

	if _, err = db.Sql.Exec(`SELECT COUNT(*) FROM "rdioScannerTags"`); err != nil {
		return nil
	}

	log.Println("migrating tags...")

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "_id", "label" FROM "rdioScannerTags"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		tag := NewTag()

		if err = rows.Scan(&tagId, &label); err != nil {
			continue
		}

		if tagId.Valid {
			tag.Id = uint64(tagId.Int32)
		} else {
			continue
		}

		if label.Valid {
			tag.Label = escapeQuotes(label.String)
		}

		tags = append(tags, tag)
	}

	rows.Close()

	sort.Slice(tags, func(i int, j int) bool {
		return tags[i].Label < tags[j].Label
	})

	for i, tag := range tags {
		tag.Order = uint(i + 1)

		query = fmt.Sprintf(`INSERT INTO "tags" ("tagId", "label", "order") VALUES (%d, '%s', %d)`, tag.Id, tag.Label, tag.Order)
		if _, err = tx.Exec(query); err != nil {
			log.Println(formatError(err, query))
		}
	}

	query = `DROP TABLE "rdioScannerTags"`
	if _, err = tx.Exec(query); err != nil {
		log.Println(formatError(err, query))
	}

	if err = tx.Commit(); err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	return nil
}

func migrateTalkgroups(db *Database) error {
	var (
		err   error
		query string
		rows  *sql.Rows
		tx    *sql.Tx

		systems = map[int64]int64{}

		frequency    sql.NullInt32
		groupId      sql.NullInt64
		label        sql.NullString
		led          sql.NullString
		name         sql.NullString
		order        sql.NullInt32
		systemId     sql.NullInt64
		tagId        sql.NullInt64
		talkgroupId  sql.NullInt64
		talkgroupRef sql.NullInt32
	)

	formatError := errorFormatter("migration", "migrateTalkgroups")

	if _, err = db.Sql.Exec(`SELECT COUNT(*) FROM "rdioScannerTalkgroups"`); err != nil {
		return nil
	}

	log.Println("migrating talkgroups...")

	query = `SELECT "systemId", "systemRef" FROM "systems"`
	if rows, err = db.Sql.Query(query); err != nil {
		return formatError(err, query)
	}

	for rows.Next() {
		if err = rows.Scan(&systemId, &talkgroupRef); err != nil {
			continue
		}

		if systemId.Valid && talkgroupRef.Valid {
			systems[int64(talkgroupRef.Int32)] = systemId.Int64
		}
	}

	rows.Close()

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "_id", "frequency", "groupId", "id", "label", "led", "name", "order", "systemId", "tagId" FROM "rdioScannerTalkgroups"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		talkgroup := NewTalkgroup()

		if err = rows.Scan(&talkgroupId, &frequency, &groupId, &talkgroupRef, &label, &led, &name, &order, &systemId, &tagId); err != nil {
			continue
		}

		if talkgroupId.Valid {
			talkgroup.Id = uint64(talkgroupId.Int64)
		} else {
			continue
		}

		if frequency.Valid {
			talkgroup.Frequency = uint(frequency.Int32)
		}

		if groupId.Valid {
			talkgroup.GroupIds = []uint64{uint64(groupId.Int64)}
		}

		if label.Valid {
			talkgroup.Label = escapeQuotes(label.String)
		}

		if led.Valid {
			talkgroup.Led = led.String
		}

		if name.Valid {
			talkgroup.Name = escapeQuotes(name.String)
		}

		if order.Valid {
			talkgroup.Order = uint(order.Int32)
		}

		if !systemId.Valid || systems[systemId.Int64] == 0 {
			continue
		}

		if talkgroupRef.Valid {
			talkgroup.TalkgroupRef = uint(talkgroupRef.Int32)
		}

		if tagId.Valid {
			talkgroup.TagId = uint64(tagId.Int64)
		}

		query = fmt.Sprintf(`INSERT INTO "talkgroups" ("talkgroupId", "frequency", "label", "led", "name", "order", "systemId", "tagId", "talkgroupRef") VALUES (%d, %d, '%s', '%s', '%s', %d, %d, %d, %d)`, talkgroup.Id, talkgroup.Frequency, talkgroup.Label, talkgroup.Led, talkgroup.Name, talkgroup.Order, systems[systemId.Int64], talkgroup.TagId, talkgroup.TalkgroupRef)
		if _, err = tx.Exec(query); err == nil {
			query = fmt.Sprintf(`INSERT INTO "talkgroupGroups" ("groupId", "talkgroupId") VALUES (%d, %d)`, talkgroup.GroupIds[0], talkgroup.Id)
			if _, err = tx.Exec(query); err != nil {
				log.Println(formatError(err, query))
			}

		} else {
			log.Println(formatError(err, query))
		}
	}

	rows.Close()

	query = `DROP TABLE "rdioScannerTalkgroups"`
	if _, err = tx.Exec(query); err != nil {
		log.Println(formatError(err, query))
	}

	if err = tx.Commit(); err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	return nil
}

func migrateUnits(db *Database) error {
	var (
		err   error
		query string
		rows  *sql.Rows
		tx    *sql.Tx

		systems = map[int32]int32{}

		label    sql.NullString
		order    sql.NullInt32
		systemId sql.NullInt32
		unitId   sql.NullInt64
		unitRef  sql.NullInt32
	)

	formatError := errorFormatter("migration", "migrateUnits")

	if _, err = db.Sql.Exec(`SELECT COUNT(*) FROM "rdioScannerUnits"`); err != nil {
		return nil
	}

	log.Println("migrating units...")

	query = `SELECT "systemId", "systemRef" FROM "systems"`
	if rows, err = db.Sql.Query(query); err != nil {
		return formatError(err, query)
	}

	for rows.Next() {
		if err = rows.Scan(&systemId, &unitRef); err != nil {
			continue
		}

		if systemId.Valid && unitRef.Valid {
			systems[unitRef.Int32] = systemId.Int32
		}
	}

	rows.Close()

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "_id", "id", "label", "order", "systemId" FROM "rdioScannerUnits"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		unit := NewUnit()

		if err = rows.Scan(&unitId, &unitRef, &label, &order, &systemId); err != nil {
			continue
		}

		if !unitId.Valid {
			continue
		}

		if !systemId.Valid || systems[systemId.Int32] == 0 {
			continue
		}

		if label.Valid {
			unit.Label = escapeQuotes(label.String)
		}

		if order.Valid {
			unit.Order = uint(order.Int32)
		}

		if unitRef.Valid {
			unit.UnitRef = uint(unitRef.Int32)
		}

		query = fmt.Sprintf(`INSERT INTO "units" ("unitId", "label", "order", "systemId", "unitRef") VALUES (%d, '%s', %d, %d, %d)`, unitId.Int64, unit.Label, unit.Order, systems[systemId.Int32], unit.Id)
		if _, err = tx.Exec(query); err != nil {
			log.Println(formatError(err, query))
		}
	}

	rows.Close()

	query = `DROP TABLE "rdioScannerUnits"`
	if _, err = tx.Exec(query); err != nil {
		log.Println(formatError(err, query))
	}

	if err = tx.Commit(); err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	return nil
}
