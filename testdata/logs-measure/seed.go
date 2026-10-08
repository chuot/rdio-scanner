// The log the measurement runs over: a realistic spread of the parts of the server, their levels, and
// the sentences they write, over thirty days.
//
// It is written by plain inserts and not by the repository, because this is data and not behaviour: what
// is being measured is a read, and the read is the server's own. The rows are the shape the server
// writes — the same columns, the same level names, the same source names, a timestamp that ascends with
// the identity — so that the planner is choosing between the same options it has on a deployment.
//
// The generator is seeded, so the same -rows writes the same log twice and the two halves of the
// measurement are comparing the same thing.

package main

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"rdio-scanner/internal/domain"
)

// shape is one part of the server, how often it writes, and the sentences it writes.
type shape struct {
	source  domain.LogSource
	weight  int
	entries []entry
}

type entry struct {
	level   string
	message string
}

var shapes = []shape{
	{
		source: domain.LogSourceIngest,
		weight: 340,
		entries: []entry{
			{domain.LogLevelInfo, `the call 1514000 on 10001 was ingested`},
			{domain.LogLevelInfo, `the call 1514000 on 10001 was held for 12s`},
			{domain.LogLevelInfo, `the call 1516000 on 10003 was ingested`},
			{domain.LogLevelInfo, `the call 1516000 on 10003 was held for 4s`},
			{domain.LogLevelInfo, `the call 1518000 on 10004 was ingested`},
			{domain.LogLevelInfo, `the call 1518000 on 10004 was held for 7s`},
			{domain.LogLevelInfo, `the call 1520000 on 10006 was ingested`},
			{domain.LogLevelInfo, `the call 1520000 on 10006 was held for 3s`},
			{domain.LogLevelInfo, `the call 1522000 on 10008 was ingested`},
			{domain.LogLevelInfo, `the call 1522000 on 10008 was held for 15s`},
			{domain.LogLevelInfo, `the call 1524000 on 10010 was ingested`},
			{domain.LogLevelWarn, `the call W1ABC on 10012 was held for 9s`},
		},
	},
	{
		source: domain.LogSourceLivefeed,
		weight: 200,
		entries: []entry{
			{domain.LogLevelInfo, `a listener joined from 10.11.12.13`},
			{domain.LogLevelInfo, `a listener left from 10.11.12.13`},
			{domain.LogLevelInfo, `a listener joined from 192.168.4.20`},
			{domain.LogLevelInfo, `a listener left from 192.168.4.20`},
			{domain.LogLevelInfo, `a listener joined from 203.0.113.7`},
			{domain.LogLevelInfo, `a listener left from 203.0.113.7`},
		},
	},
	{
		source: domain.LogSourceDelayer,
		weight: 90,
		entries: []entry{
			{domain.LogLevelInfo, `the call 1514000 on 10001 was delayed for 30s`},
			{domain.LogLevelWarn, `the delayed call 1514000 on 10001 could not be re-posted: the downstream is not reachable`},
			{domain.LogLevelWarn, `the delayed call 1516000 on 10003 could not be recorded: no such file or directory`},
		},
	},
	{
		source: domain.LogSourceDirwatch,
		weight: 80,
		entries: []entry{
			{domain.LogLevelWarn, `the watcher for /data/incoming stopped: no such file or directory`},
			{domain.LogLevelWarn, `the watcher for /data/outgoing stopped: too many open files`},
			{domain.LogLevelWarn, `the directory /data/incoming could not be read: permission denied`},
			{domain.LogLevelWarn, `the file call-1514000.wav could not be handed to the ingest path: no such file or directory`},
		},
	},
	{
		source: domain.LogSourceLogin,
		weight: 70,
		entries: []entry{
			{domain.LogLevelWarn, `a login attempt from 203.0.113.7 was refused`},
			{domain.LogLevelWarn, `too many login attempts from 203.0.113.7`},
			{domain.LogLevelInfo, `a login from 10.11.12.13 was accepted`},
			{domain.LogLevelInfo, `a logout from 10.11.12.13 was recorded`},
		},
	},
	{
		source: domain.LogSourceConfig,
		weight: 40,
		entries: []entry{
			{domain.LogLevelWarn, `the configuration changed`},
			{domain.LogLevelWarn, `the groups configuration changed`},
			{domain.LogLevelWarn, `the options configuration changed`},
			{domain.LogLevelError, `the dirwatch configuration could not be saved: the directory is not readable`},
		},
	},
	{
		source: domain.LogSourceDownstream,
		weight: 35,
		entries: []entry{
			{domain.LogLevelInfo, `the call 1514000 on 10001 was re-posted to bridge-1`},
			{domain.LogLevelWarn, `the call 1514000 on 10001 could not be re-posted to bridge-1: connection refused`},
			{domain.LogLevelWarn, `the downstream bridge-2 could not be reached: the apikey is not valid`},
		},
	},
	{
		source: domain.LogSourceFFmpeg,
		weight: 25,
		entries: []entry{
			{domain.LogLevelWarn, `the call 1514000 on 10001 could not be converted: the audio is not readable`},
			{domain.LogLevelWarn, `the call 1516000 on 10003 could not be converted: the conversion failed`},
		},
	},
	{
		source: domain.LogSourceUpload,
		weight: 20,
		entries: []entry{
			{domain.LogLevelInfo, `the upload from unit O'Brien was accepted`},
			{domain.LogLevelWarn, `the upload from unit O'Brien was refused: the apikey is not valid`},
		},
	},
	{
		source: domain.LogSourcePrune,
		weight: 10,
		entries: []entry{
			{domain.LogLevelInfo, `the retention pass removed 1,200 entries older than 30 days`},
			{domain.LogLevelWarn, `the retention pass could not finish: the log could not be read`},
		},
	},
	{
		source: domain.LogSourceLogs,
		weight: 10,
		entries: []entry{
			{domain.LogLevelWarn, `the log search failed: no such column: source`},
		},
	},
	{
		source: domain.LogSourceAdmin,
		weight: 10,
		entries: []entry{
			{domain.LogLevelWarn, `the administrator password was not changed: current password is invalid`},
		},
	},
	{
		source: domain.LogSourceServer,
		weight: 10,
		entries: []entry{
			{domain.LogLevelWarn, `server started`},
			{domain.LogLevelInfo, `the server is serving /data/rdio`},
		},
	},
	{
		source: domain.LogSourceAccess,
		weight: 8,
		entries: []entry{
			{domain.LogLevelWarn, `the access code 4F2A could not be read: no such code`},
		},
	},
	{
		source: domain.LogSourceDatabase,
		weight: 6,
		entries: []entry{
			{domain.LogLevelWarn, `the migration of logs could not be finished: no such column`},
			{domain.LogLevelWarn, `the migration left 1 unit damaged: the unit it holds, it holds no system`},
		},
	},
	{
		source: domain.LogSourceCalls,
		weight: 5,
		entries: []entry{
			{domain.LogLevelInfo, `the call 1514000 on 10001 was pinned`},
		},
	},
	{
		source: domain.LogSourceLegacy,
		weight: 1,
		entries: []entry{
			{domain.LogLevelWarn, `server started`},
		},
	},
}

func seed(ctx context.Context, db *sql.DB, rows int) error {
	total := 0

	for _, s := range shapes {
		total += s.weight
	}

	random := rand.New(rand.NewSource(20261008))

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer tx.Rollback()

	const batch = 250

	from := time.Now().Add(-30 * 24 * time.Hour).UnixMilli()
	to := time.Now().UnixMilli()

	step := (to - from) / int64(rows)

	values := make([]any, 0, batch*5)

	for i := 1; i <= rows; i++ {
		s := pick(random.Intn(total))

		e := s.entries[random.Intn(len(s.entries))]

		values = append(values, i, e.level, string(s.source), e.message, from+int64(i)*step)

		if len(values) == batch*5 {
			if err := insert(ctx, tx, values); err != nil {
				return err
			}

			values = values[:0]
		}
	}

	if len(values) > 0 {
		if err := insert(ctx, tx, values); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	var count int64

	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "logs"`).Scan(&count); err != nil {
		return err
	}

	if int(count) != rows {
		return fmt.Errorf("seeded %d entries, want %d", count, rows)
	}

	// What the four searches are being asked to find, so the durations can be read against a match set
	// instead of against a number nobody knows.
	for _, criterion := range []string{
		`"level" = 'warn'`,
		`"source" = 'dirwatch'`,
		`"source" = 'ingest' AND lower("message") LIKE '%` + strings.ToLower(measureText) + `%'`,
		`"source" = 'dirwatch' AND lower("message") LIKE '%` + strings.ToLower(measurePath) + `%'`,
	} {
		var n int64

		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "logs" WHERE `+criterion).Scan(&n); err != nil {
			return err
		}

		fmt.Printf("  %-58s %6d of %d\n", criterion, n, count)
	}

	return nil
}

func insert(ctx context.Context, tx *sql.Tx, values []any) error {
	groups := len(values) / 5

	builder := strings.Builder{}

	builder.WriteString(`INSERT INTO "logs" ("logid", "level", "source", "message", "timestamp") VALUES `)

	for i := 0; i < groups; i++ {
		if i > 0 {
			builder.WriteString(", ")
		}

		builder.WriteString("(?, ?, ?, ?, ?)")
	}

	_, err := tx.ExecContext(ctx, builder.String(), values...)

	return err
}

// pick chooses a part of the server, weighted by how often it writes.
func pick(n int) shape {
	for _, s := range shapes {
		if n < s.weight {
			return s
		}

		n -= s.weight
	}

	return shapes[len(shapes)-1]
}
