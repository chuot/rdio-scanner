//go:build live

// The large-database timing: the migration pass measured on a deployment an operator
// would actually wait through, on the file backend and on both real backends.
//
// It is its own file, behind the `live` tag and behind `run.sh --big`, for two reasons. It proves nothing
// about correctness — every row it carries is a row the correctness tests already prove — and it is not
// something the ordinary suite should pay for: the fixture is a hundred megabytes and a run is minutes.
//
// What it does assert is that the pass is still one pass rather than a query per row that went unnoticed.
// The calls that came out are the calls that went in, the pass said nothing alarming while doing it, and
// the whole thing fit inside the budget written down below. An upgrade that quietly became one round trip
// per call takes an hour instead of a minute, and the person who finds that out should not be the one
// standing in front of the server.

package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/domain"
)

// largeBudget is the requirement's "a time an operator can wait through" written as a number, so that a
// regression fails a test instead of being noticed by someone watching a server start. It is deliberately
// far above what the pass actually takes on this machine: the point is the cliff, not the record.
const largeBudget = 10 * time.Minute

// largeSeedRows is how many rows one seeding statement carries. seedLive's one row per statement is the
// right shape for a forty-row deployment, because a failure names the row; a hundred thousand of them
// would measure the harness rather than the migration. A hundred rows of a call table is a statement of a
// few hundred kilobytes, which every backend here accepts at its defaults.
const largeSeedRows = 100

func TestLargeDatabaseOnFile(t *testing.T) {
	fixture, calls := largeFixture(t)

	// The pass writes into the database it is given, so the fixture is copied: what is timed is one
	// upgrade, and the file bigdb.py wrote is left the way it wrote it.
	path := filepath.Join(t.TempDir(), "rdio-scanner.db")

	copyLargeFixture(t, fixture, path)

	db, err := Open(Settings{Dialect: DialectSqlite, FilePath: path})
	if err != nil {
		t.Fatalf("Open(sqlite): %v", err)
	}

	defer db.Close()

	elapsed, lines := largeMigrate(t, db, DialectSqlite)

	reportLargeTiming(t, DialectSqlite, elapsed, calls, db, fixture, lines)
}

func TestLargeDatabaseOnMariaDB(t *testing.T) {
	largeDatabaseOnBackend(t, DialectMariadb)
}

func TestLargeDatabaseOnPostgreSQL(t *testing.T) {
	largeDatabaseOnBackend(t, DialectPostgresql)
}

func largeDatabaseOnBackend(t *testing.T, dialect Dialect) {
	t.Helper()

	fixture, calls := largeFixture(t)

	settings := liveSettings(t, dialect)

	db, err := Open(settings)
	if err != nil {
		t.Fatalf("Open(%s): %v", dialect, err)
	}

	defer db.Close()

	livePing(t, db, settings)

	resetLive(t, db, dialect)

	applyStatements(t, db, liveStatements(dialect))

	// The seeding is timed and reported separately. It is not what the requirement asks about, and a number that
	// mixes "putting a hundred thousand rows into a backend" with "carrying them across" answers neither.
	seeded := time.Now()

	seedLargeLive(t, db, dialect, fixture)

	t.Logf("%s: seeding the v6.6.3 deployment took %s", dialect, time.Since(seeded).Round(time.Millisecond))

	elapsed, lines := largeMigrate(t, db, dialect)

	reportLargeTiming(t, dialect, elapsed, calls, db, fixture, lines)
}

// largeFixture is the generated v6.6.3-shaped database, named by the harness that wrote it. The
// row count is checked against the fixture rather than trusted from the environment, because a stale
// fixture would otherwise time a smaller database and still report the number the operator asked for.
func largeFixture(t *testing.T) (path string, calls int) {
	t.Helper()

	path = os.Getenv("RDIO_LIVE_BIG_FIXTURE")

	if path == "" {
		t.Fatalf("RDIO_LIVE_BIG_FIXTURE is not set: the large-database timing runs against a generated v6.6.3-shaped fixture, named by RDIO_LIVE_BIG_FIXTURE with its row count in RDIO_LIVE_BIG_CALLS")
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("RDIO_LIVE_BIG_FIXTURE: %v", err)
	}

	wanted, err := strconv.Atoi(os.Getenv("RDIO_LIVE_BIG_CALLS"))
	if err != nil {
		t.Fatalf("RDIO_LIVE_BIG_CALLS is not a number: %q", os.Getenv("RDIO_LIVE_BIG_CALLS"))
	}

	fixture, err := Open(Settings{Dialect: DialectSqlite, FilePath: path})
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}

	defer fixture.Close()

	var held int

	if err := fixture.QueryRow(`SELECT COUNT(*) FROM "rdioScannerCalls"`).Scan(&held); err != nil {
		t.Fatalf("counting the calls of %s: %v", path, err)
	}

	if held != wanted {
		t.Fatalf("%s holds %d calls and RDIO_LIVE_BIG_CALLS says %d: regenerate the fixture at that row count", path, held, wanted)
	}

	return path, wanted
}

func copyLargeFixture(t *testing.T, from, to string) {
	t.Helper()

	source, err := os.Open(from)
	if err != nil {
		t.Fatalf("opening %s: %v", from, err)
	}

	defer source.Close()

	target, err := os.Create(to)
	if err != nil {
		t.Fatalf("creating %s: %v", to, err)
	}

	defer target.Close()

	if _, err := io.Copy(target, source); err != nil {
		t.Fatalf("copying %s: %v", from, err)
	}
}

// seedLargeLive streams the fixture into the backend, in batches, through the same conversions seedLive
// applies to a value a real backend cannot be handed as sqlite stored it.
func seedLargeLive(t *testing.T, db *sql.DB, dialect Dialect, path string) {
	t.Helper()

	fixture, err := Open(Settings{Dialect: DialectSqlite, FilePath: path})
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}

	defer fixture.Close()

	s := newStore(db, dialect)

	for _, table := range liveSeedOrder {
		columns, declared := liveFixtureColumns(t, fixture, table)

		names := make([]string, 0, len(columns))

		for _, column := range columns {
			names = append(names, `"`+column+`"`)
		}

		holders := make([]string, 0, len(columns))

		for range columns {
			holders = append(holders, "?")
		}

		oneRow := "(" + strings.Join(holders, ", ") + ")"

		rows, err := fixture.Query(`SELECT * FROM "` + table + `"`)
		if err != nil {
			t.Fatalf("reading the rows of %s: %v", table, err)
		}

		batch := []any{}
		held := 0
		total := 0

		flush := func() {
			if held == 0 {
				return
			}

			groups := make([]string, 0, held)

			for range held {
				groups = append(groups, oneRow)
			}

			query := `INSERT INTO "` + table + `" (` + strings.Join(names, ", ") + `) VALUES ` + strings.Join(groups, ", ")

			if _, err := s.exec(context.Background(), query, batch...); err != nil {
				rows.Close()

				t.Fatalf("seeding %s into %s: %v", table, dialect, err)
			}

			batch = batch[:0]
			held = 0
		}

		for rows.Next() {
			values := make([]any, len(columns))
			scans := make([]any, len(columns))

			for i := range values {
				scans[i] = &values[i]
			}

			if err := rows.Scan(scans...); err != nil {
				rows.Close()

				t.Fatalf("reading a row of %s: %v", table, err)
			}

			for i, value := range values {
				batch = append(batch, liveSeedValue(t, dialect, table, columns[i], declared[i], value))
			}

			held++
			total++

			if held == largeSeedRows {
				flush()
			}
		}

		if err := rows.Err(); err != nil {
			rows.Close()

			t.Fatalf("reading the rows of %s: %v", table, err)
		}

		rows.Close()

		flush()

		if total == 0 {
			t.Fatalf("the large fixture holds no %s rows, so seeding %s proves nothing", table, dialect)
		}
	}
}

// largeMigrate is liveMigrate with the clock the timing is about.
func largeMigrate(t *testing.T, db *sql.DB, dialect Dialect) (time.Duration, []progressLine) {
	t.Helper()

	lines := []progressLine{}

	started := time.Now()

	if err := Migrate(context.Background(), db, dialect, fixedClock{migrationInstant}, func(level, message string) {
		lines = append(lines, progressLine{level: level, message: message})
	}); err != nil {
		t.Fatalf("Migrate on %s: %v", dialect, err)
	}

	return time.Since(started), lines
}

// reportLargeTiming is the record the criterion asks for: the duration, the size of what was migrated, and the
// two facts that make a duration mean anything — that the rows arrived, and that the pass finished what it
// started rather than stopping partway and leaving a v6 table behind.
func reportLargeTiming(t *testing.T, dialect Dialect, elapsed time.Duration, wanted int, db *sql.DB, fixture string, lines []progressLine) {
	t.Helper()

	size, err := os.Stat(fixture)
	if err != nil {
		t.Fatalf("RDIO_LIVE_BIG_FIXTURE: %v", err)
	}

	carried := countLargeTable(t, db, "calls")
	archived := countLargeTable(t, db, "migrationArchive")

	left := []string{}

	for _, table := range tableNames(t, db, dialect) {
		if strings.HasPrefix(table, "rdioScanner") {
			left = append(left, table)
		}
	}

	sort.Strings(left)

	warnings := []progressLine{}

	for _, line := range lines {
		if line.level != domain.LogLevelInfo {
			warnings = append(warnings, line)
		}
	}

	t.Logf(
		"%s: %d calls from a %s v6.6.3 database migrated in %s — %d rows in the v8 calls table, %d archived rows, %d v6 tables left",
		dialect,
		wanted,
		humanBytes(size.Size()),
		elapsed.Round(time.Millisecond),
		carried,
		archived,
		len(left),
	)

	for _, line := range warnings {
		t.Logf("  %s: %s", line.level, line.message)
	}

	if carried != int64(wanted) {
		t.Errorf("the %s run carried %d of the %d calls it was given", dialect, carried, wanted)
	}

	if len(left) > 0 {
		t.Errorf("the %s run left %s where it found it, so it did not finish reading %s", dialect, strings.Join(left, ", "), dialect)
	}

	if elapsed > largeBudget {
		t.Errorf("%s took %s, over the %s budget the requirement records as a time an operator can wait through", dialect, elapsed.Round(time.Second), largeBudget)
	}
}

func countLargeTable(t *testing.T, db *sql.DB, table string) int64 {
	t.Helper()

	var count int64

	if err := db.QueryRow(`SELECT COUNT(*) FROM "` + table + `"`).Scan(&count); err != nil {
		t.Fatalf("counting the rows of %s: %v", table, err)
	}

	return count
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KiB", float64(n)/(1<<10))
	}

	return fmt.Sprintf("%d bytes", n)
}
