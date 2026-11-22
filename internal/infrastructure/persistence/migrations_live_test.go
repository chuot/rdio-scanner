//go:build live

package persistence

// The live tests prove the migration pass against backends that are not files, compiled only
// under the `live` tag: the reseed-per-test rule, and what each run proves.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/domain"
)

// liveSeedOrder is the order the rows arrive in: the tables the migration pass looks a
// reference up in before it carries the rows that name them. The v6 tables have no foreign
// keys, so the schema does not require the order; it buys that a seeding failure names the
// wrong row instead of looking like a migration that found nothing to resolve.
var liveSeedOrder = []string{
	"rdioScannerMeta",
	"rdioScannerGroups",
	"rdioScannerTags",
	"rdioScannerSystems",
	"rdioScannerTalkgroups",
	"rdioScannerUnits",
	"rdioScannerConfigs",
	"rdioScannerLogs",
	"rdioScannerDownstreams",
	"rdioScannerDirWatches",
	"rdioScannerCalls",
	"rdioScannerApiKeys",
	"rdioScannerAccesses",
}

// liveSettings reads one backend's connection out of the environment, the way the server reads
// its own database out of its configuration. Nothing here falls back to sqlite or to a default
// port: a live test that quietly reached a different database than the harness started would
// report a proof it did not produce.
func liveSettings(t *testing.T, dialect Dialect) Settings {
	t.Helper()

	prefix := map[Dialect]string{
		DialectMariadb:    "RDIO_LIVE_MARIADB",
		DialectPostgresql: "RDIO_LIVE_POSTGRESQL",
	}[dialect]

	if prefix == "" {
		t.Fatalf("liveSettings: %s has no live backend to reach", dialect)
	}

	settings := Settings{
		Dialect:  dialect,
		Host:     liveEnv(t, prefix, "HOST"),
		Username: liveEnv(t, prefix, "USER"),
		Password: liveEnv(t, prefix, "PASSWORD"),
		Name:     liveEnv(t, prefix, "NAME"),
	}

	port, err := strconv.ParseUint(liveEnv(t, prefix, "PORT"), 10, 32)
	if err != nil {
		t.Fatalf("%s_PORT: %q is not a port: %v", prefix, os.Getenv(prefix+"_PORT"), err)
	}

	settings.Port = uint(port)

	return settings
}

func liveEnv(t *testing.T, prefix, name string) string {
	t.Helper()

	value := os.Getenv(prefix + "_" + name)

	if value == "" {
		t.Fatalf("%s_%s is not set: the live tests run against a backend the developer starts, and that backend is named by %s_%s", prefix, name, prefix, name)
	}

	return value
}

// liveDatabase opens a backend and leaves it holding a freshly seeded v6.6.3 deployment.
func liveDatabase(t *testing.T, dialect Dialect) *sql.DB {
	t.Helper()

	settings := liveSettings(t, dialect)

	db, err := Open(settings)
	if err != nil {
		t.Fatalf("Open(%s): %v", dialect, err)
	}

	t.Cleanup(func() { db.Close() })

	livePing(t, db, settings)

	resetLive(t, db, dialect)

	applyStatements(t, db, liveStatements(dialect))
	seedLive(t, db, dialect)

	return db
}

// resetLive empties the backend so that every test starts from the same seeded deployment
// rather than from what the test before it left. The v8 tables have foreign keys and the
// catalog does not report them in dependency order, so the reset tells the backend to ignore
// them for its duration: MariaDB by its own switch, PostgreSQL by cascading each drop, which
// is the difference between the two because PostgreSQL has no switch and MariaDB does not
// cascade.
func resetLive(t *testing.T, db *sql.DB, dialect Dialect) {
	t.Helper()

	if dialect == DialectMariadb || dialect == DialectMysql {
		mustExec(t, db, "SET FOREIGN_KEY_CHECKS = 0")

		defer mustExec(t, db, "SET FOREIGN_KEY_CHECKS = 1")
	}

	for _, name := range tableNames(t, db, dialect) {
		if dialect == DialectPostgresql {
			mustExec(t, db, `DROP TABLE IF EXISTS "`+name+`" CASCADE`)

			continue
		}

		mustExec(t, db, `DROP TABLE IF EXISTS "`+name+`"`)
	}
}

// livePing reaches the backend, or says which one it could not reach. The deadline is the
// test's own: a driver that keeps dialing for two minutes before it admits the port is dead
// makes a live failure as hard to read as a hang.
func livePing(t *testing.T, db *sql.DB, settings Settings) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("%s at %s:%d is not reachable: %v\n\nstart a backend of that dialect on that host and port",
			settings.Dialect, settings.Host, settings.Port, err)
	}
}

// applyStatements runs a recorded seed, one statement per line. Every statement in the recorded schema is
// one line ending in a semicolon, which is the shape the legacy server ran them in: a general SQL splitter
// is not what a pinned seed needs.
func applyStatements(t *testing.T, db *sql.DB, statements []string) {
	t.Helper()

	for _, statement := range statements {
		mustExec(t, db, statement)
	}
}

func liveStatements(dialect Dialect) []string {
	switch dialect {
	case DialectMariadb, DialectMysql:
		return liveMariadbSchema
	case DialectPostgresql:
		return livePostgresqlSchema
	}

	panic(fmt.Sprintf("liveStatements: %s has no recorded v6.6.3 schema", dialect))
}

// seedLive copies the rows of the v6.6.3 sqlite fixture into the backend, through the same
// store the repositories write through, so a live run migrates the deployment the sqlite tests
// migrate rather than a second copy of it written out again in another language. Two values
// need converting on the way, because a sqlite file is the one place a column's declared type
// is a suggestion: a datetime column holds the text Go's time package prints, and an empty
// blob comes back as a nil slice. Both are converted by the column's declared type in the
// fixture, not by guessing at the value.
func seedLive(t *testing.T, db *sql.DB, dialect Dialect) {
	t.Helper()

	for _, table := range liveSeedOrder {
		seedLiveTable(t, db, dialect, table)
	}
}

// seedLiveTable copies one table of the recorded v6.6.3 fixture into a live backend of the given
// dialect. The v6.6.3 fixture is the recorded statements, applied to a database of this dialect's own
// schema, so that a live run seeds the same rows the sqlite tests seed, and one table at a time is
// what lets a test put a single legacy table back after the pass finished with it.
func seedLiveTable(t *testing.T, db *sql.DB, dialect Dialect, table string) {
	t.Helper()

	fixture := buildRecordedDatabase(t, append(append([]string{}, legacyV6Schema...), legacyV6Rows...))

	defer fixture.Close()

	s := newStore(db, dialect)

	columns, declared := liveFixtureColumns(t, fixture, table)
	rows := liveFixtureRows(t, fixture, table)

	if len(rows) == 0 {
		t.Fatalf("the v6.6.3 fixture holds no %s rows, so seeding %s proves nothing", table, dialect)
	}

	names := make([]string, 0, len(columns))

	for _, column := range columns {
		names = append(names, `"`+column+`"`)
	}

	holders := make([]string, 0, len(columns))

	for range columns {
		holders = append(holders, "?")
	}

	query := `INSERT INTO "` + table + `" (` + strings.Join(names, ", ") + `) VALUES (` + strings.Join(holders, ", ") + `)`

	for _, row := range rows {
		args := make([]any, 0, len(row))

		for i, value := range row {
			args = append(args, liveSeedValue(t, dialect, table, columns[i], declared[i], value))
		}

		if _, err := s.exec(context.Background(), query, args...); err != nil {
			t.Fatalf("seeding %s into %s: %v", table, dialect, err)
		}
	}
}

// liveSeedValue converts the fixture values a real backend cannot be handed as they are. Each
// conversion is chosen by the type the fixture's own schema declares for the column, because
// guessing at a value is how a seed ends up rewriting the deployment it is supposed to
// reproduce.
func liveSeedValue(t *testing.T, dialect Dialect, table, column, declared string, value any) any {
	t.Helper()

	switch {
	case declared == "datetime":
		// An access with no expiration is a row the v6 table holds with nothing in that column, and the
		// backend has to be handed nothing in it too.
		if value == nil {
			return nil
		}

		if when, ok := value.(time.Time); ok {
			return when
		}

		if blob, ok := value.([]byte); ok {
			value = string(blob)
		}

		when, err := parseV6DateTime(fmt.Sprintf("%v", value))
		if err != nil {
			t.Fatalf("%s.%s: %v is not a datetime a v6 database writes: %v", table, column, value, err)
		}

		return when

	case declared == "longblob":
		// An empty blob is call 710's missing audio: a value the row has, which happens to hold nothing.
		// The sqlite driver hands that back as a nil slice, and both backends read a nil slice as "no
		// value at all" and refuse the not-null column the way they would refuse a call with no audio
		// column — which is a different fact, and the one this seed is not allowed to invent.
		if blob, ok := value.([]byte); ok && blob != nil {
			return blob
		}

		return []byte{}

	case declared == "tinyint(1)" && dialect == DialectPostgresql:
		// v6 wrote a flag as 0 or 1 in a tinyint. This server's PostgreSQL schema spells the same thing
		// as a boolean, and pgx will not read 1 as true.
		number, _ := value.(int64)

		return number != 0
	}

	return value
}

func liveFixtureColumns(t *testing.T, fixture *sql.DB, table string) (names []string, declared []string) {
	t.Helper()

	rows, err := fixture.Query(`SELECT "name", "type" FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("reading the columns of %s: %v", table, err)
	}

	defer rows.Close()

	for rows.Next() {
		var name, kind string

		if err := rows.Scan(&name, &kind); err != nil {
			t.Fatalf("reading the columns of %s: %v", table, err)
		}

		names = append(names, name)
		declared = append(declared, strings.ToLower(kind))
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("reading the columns of %s: %v", table, err)
	}

	if len(names) == 0 {
		t.Fatalf("the recorded v6.6.3 statements leave no table %s", table)
	}

	return names, declared
}

func liveFixtureRows(t *testing.T, fixture *sql.DB, table string) [][]any {
	t.Helper()

	rows, err := fixture.Query(`SELECT * FROM "` + table + `"`)
	if err != nil {
		t.Fatalf("reading the rows of %s: %v", table, err)
	}

	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("reading the columns of %s: %v", table, err)
	}

	out := [][]any{}

	for rows.Next() {
		values := make([]any, len(columns))
		holders := make([]any, len(columns))

		for i := range values {
			holders[i] = &values[i]
		}

		if err := rows.Scan(holders...); err != nil {
			t.Fatalf("reading a row of %s: %v", table, err)
		}

		out = append(out, values)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("reading the rows of %s: %v", table, err)
	}

	return out
}

// liveMigrate runs the pass over a backend and returns what it said.
func liveMigrate(t *testing.T, db *sql.DB, dialect Dialect) []progressLine {
	t.Helper()

	lines := []progressLine{}

	if err := Migrate(context.Background(), db, dialect, fixedClock{migrationInstant}, func(level, message string) {
		lines = append(lines, progressLine{level: level, message: message})
	}); err != nil {
		t.Fatalf("Migrate on %s: %v", dialect, err)
	}

	return lines
}

// liveBackends is every backend the live tests are written for, so that a test that has to exist for both
// is written once.
var liveBackends = []Dialect{DialectMariadb, DialectPostgresql}

func TestMigrateLiveOnMariaDBCarriesOrArchivesEveryRowOfTheV663Deployment(t *testing.T) {
	liveConservation(t, DialectMariadb)
}

func TestMigrateLiveOnPostgreSQLCarriesOrArchivesEveryRowOfTheV663Deployment(t *testing.T) {
	liveConservation(t, DialectPostgresql)
}

// liveConservation is the conservation half of the audit on a live backend.
func liveConservation(t *testing.T, dialect Dialect) {
	t.Helper()

	db := liveDatabase(t, dialect)

	auditShadow(t, db)

	liveMigrate(t, db, dialect)

	auditConservation(t, db)
	auditConfigConservation(t, db)
	auditDeclaredDrops(t, db, dialect)
}

func TestMigrateLiveOnMariaDBCarriesEveryValueIntoTheV8ColumnItsRuleNames(t *testing.T) {
	liveValues(t, DialectMariadb)
}

func TestMigrateLiveOnPostgreSQLCarriesEveryValueIntoTheV8ColumnItsRuleNames(t *testing.T) {
	liveValues(t, DialectPostgresql)
}

// liveValues is the value-fidelity and reference-integrity half of the audit on a live
// backend, which is where the conversions that a sqlite file lets one backend get away with
// are measured: the datetimes, the byte-for-byte audio, and the resolved references.
func liveValues(t *testing.T, dialect Dialect) {
	t.Helper()

	db := liveDatabase(t, dialect)

	auditShadow(t, db)

	liveMigrate(t, db, dialect)

	auditValues(t, db)
	auditArchivedCallAudio(t, db)
	auditReferences(t, db)
}

func TestMigrateLiveOnMariaDBMigratesTheDirwatchesItNames(t *testing.T) {
	liveDirwatches(t, DialectMariadb)
}

func TestMigrateLiveOnPostgreSQLMigratesTheDirwatchesItNames(t *testing.T) {
	liveDirwatches(t, DialectPostgresql)
}

// liveDirwatches is the criterion: the proof on a backend that is case-sensitive about table
// names, which is the only place the misspelling the dirwatch migration used could ever have
// been seen, since sqlite matches table names whatever their case is. The v6 table is there
// under the name the v6 database created, the pass drops it, every dirwatch of the deployment
// arrives, and nothing dirwatch is archived.
func liveDirwatches(t *testing.T, dialect Dialect) {
	t.Helper()

	db := liveDatabase(t, dialect)

	if !hasTable(t, db, dialect, v6DirwatchesTable) {
		t.Fatalf("the seeded deployment does not hold %s, so this test proves nothing", v6DirwatchesTable)
	}

	liveMigrate(t, db, dialect)

	if hasTable(t, db, dialect, v6DirwatchesTable) {
		t.Errorf("%s survived the pass on %s: the pass reads and drops the v6 table under one name, and it is not this one",
			v6DirwatchesTable, dialect)
	}

	carried := auditIdentities(t, db, "dirwatches", "dirwatchId")

	if want := []string{"601", "602", "603", "605", "606"}; !equalStrings(carried, want) {
		t.Errorf("%s carried %v of the deployment's dirwatches, want %v: a backend that does not find the v6 table "+
			"skips the migration on every start and says nothing", dialect, carried, want)
	}

	if rows := archivedRows(t, db, v6DirwatchesTable); len(rows) > 0 {
		t.Errorf("the archive holds %v for %s, want nothing: every dirwatch of this deployment was carried",
			rows, v6DirwatchesTable)
	}
}

func TestMigrateLiveOnMariaDBFilesWhatThePassSaid(t *testing.T) {
	liveFiling(t, DialectMariadb)
}

func TestMigrateLiveOnPostgreSQLFilesWhatThePassSaid(t *testing.T) {
	liveFiling(t, DialectPostgresql)
}

// liveFiling is the half of the requirement the criterion asks for on a live backend: every
// line the pass said about a row it could not carry is stored as a warn entry under the
// `database` source, and read back through the same search the log view answers with. The
// entries are filed through the repository the server files them through, because the pass
// runs before the server has a log repository to file them in; that the server files them at
// all, in that order, is proven by TestTheMigrationPassFilesWhatItSaid.
func liveFiling(t *testing.T, dialect Dialect) {
	t.Helper()

	db := liveDatabase(t, dialect)

	ctx := context.Background()

	lines := liveMigrate(t, db, dialect)

	problems := []progressLine{}

	for _, line := range lines {
		if line.level == domain.LogLevelWarn {
			problems = append(problems, line)
		}
	}

	if len(problems) == 0 {
		t.Fatalf("the pass said nothing about a row it could not carry on %s, so filing it proves nothing", dialect)
	}

	logs := NewLogs(db, dialect)

	for _, line := range problems {
		entry := domain.LogEntry{
			DateTime: migrationInstant,
			Level:    line.level,
			Source:   domain.LogSourceDatabase,
			Message:  line.message,
		}

		if err := logs.AppendLog(ctx, entry); err != nil {
			t.Fatalf("filing %q on %s: %v", line.message, dialect, err)
		}
	}

	for _, line := range problems {
		page, err := logs.SearchLogs(ctx, domain.LogSearch{
			Level:     domain.LogLevelWarn,
			Source:    domain.LogSourceDatabase,
			Text:      line.message,
			Limit:     domain.MaxLogSearchLimit,
			Ascending: true,
		})
		if err != nil {
			t.Fatalf("searching for %q on %s: %v", line.message, dialect, err)
		}

		if page.Count != 1 {
			t.Errorf("%q is filed %d times under database/warn on %s, want once", line.message, page.Count, dialect)

			continue
		}

		if len(page.Results) != 1 || page.Results[0].Source != domain.LogSourceDatabase || page.Results[0].Level != domain.LogLevelWarn {
			t.Errorf("%q is filed on %s as %v, want one entry under database/warn", line.message, dialect, page.Results)
		}
	}
}

func TestMigrateLiveOnMariaDBChangesNothingTwice(t *testing.T) {
	liveIdempotence(t, DialectMariadb)
}

func TestMigrateLiveOnPostgreSQLChangesNothingTwice(t *testing.T) {
	liveIdempotence(t, DialectPostgresql)
}

// liveIdempotence is the requirement on a live backend: the pass that already ran leaves no v6
// table behind, so a second run finds nothing to migrate, says nothing, and changes nothing —
// not a row, not a column of the archive, not an object of the schema.
func liveIdempotence(t *testing.T, dialect Dialect) {
	t.Helper()

	db := liveDatabase(t, dialect)

	liveMigrate(t, db, dialect)

	before := liveSnapshot(t, db, dialect)

	lines := liveMigrate(t, db, dialect)

	if len(lines) > 0 {
		messages := []string{}

		for _, line := range lines {
			messages = append(messages, line.message)
		}

		t.Errorf("the second pass on %s said %v, want nothing: the first pass left no v6 table for it to read", dialect, messages)
	}

	after := liveSnapshot(t, db, dialect)

	if !equalStrings(before, after) {
		t.Errorf("the second pass on %s changed the database it had already migrated", dialect)

		for _, difference := range liveDifferences(before, after) {
			t.Errorf("  %s", difference)
		}
	}
}

// liveSnapshot is every row of every table the backend holds, in one sorted list, so that "nothing
// changed" is a comparison of the database with itself rather than a count of tables.
func liveSnapshot(t *testing.T, db *sql.DB, dialect Dialect) []string {
	t.Helper()

	rows := []string{}

	for _, table := range tableNames(t, db, dialect) {
		for _, row := range readRows(t, db, `SELECT * FROM "`+table+`"`) {
			rows = append(rows, table+" | "+strings.Join(row, " | "))
		}
	}

	sort.Strings(rows)

	return rows
}

// liveDifferences names the rows the two snapshots disagree about, up to a point: a second pass that
// duplicated a table would otherwise bury its own evidence in ten thousand lines.
func liveDifferences(before, after []string) []string {
	only := map[string]int{}

	for _, row := range before {
		only[row]++
	}

	for _, row := range after {
		only[row]--
	}

	differences := []string{}

	for row, count := range only {
		if count == 0 {
			continue
		}

		switch {
		case count > 0:
			differences = append(differences, fmt.Sprintf("%q is gone, and it was there before", row))
		default:
			differences = append(differences, fmt.Sprintf("%q appears %d more times", row, -count))
		}

		if len(differences) == 20 {
			differences = append(differences, "... and more")

			break
		}
	}

	sort.Strings(differences)

	return differences
}

// A live test that skipped because its backend was not there would leave a record that says
// the backend was proven: pointing one test at a port nothing listens on is how that is
// caught.
func TestMigrateLiveOnMariaDBReportsABackendItCannotReach(t *testing.T) {
	settings := liveSettings(t, DialectMariadb)

	settings.Port = settings.Port + 1

	db, err := Open(settings)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err == nil {
		t.Fatalf("a MariaDB answered on port %d, which is not the port the live tests expect for MariaDB", settings.Port)
	}

	t.Logf("%s at %s:%d refused the connection, which is what a live test is supposed to report",
		DialectMariadb, settings.Host, settings.Port)
}

// The duplicate-row fixture on the backend that creates the duplicates: MySQL and MariaDB
// report an unchanged option update as no changed rows, the save reads that as no row there,
// and every save writes every key again. The live pass removes the duplicates, last row
// written wins, and the unique index refuses the next one.
func TestMigrateLiveOnMariaDBDeduplicatesDuplicateOptionRows(t *testing.T) {
	db := liveDatabase(t, DialectMariadb)

	if err := ApplySchema(context.Background(), db, DialectMariadb); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	// The fixture: every key written twice, the second copy written later.
	for _, value := range []string{`"one"`, `"two"`} {
		if _, err := db.Exec(`INSERT INTO "options" ("key", "value") VALUES ('branding', ?)`, value); err != nil {
			t.Fatalf("seeding the duplicate rows: %v", err)
		}
	}

	liveMigrate(t, db, DialectMariadb)

	var rows int

	if err := db.QueryRow(`SELECT COUNT(*) FROM "options" WHERE "key" = 'branding'`).Scan(&rows); err != nil {
		t.Fatalf("counting the branding rows: %v", err)
	}

	if rows != 1 {
		t.Fatalf("the options table holds %d rows for branding, want the one row the pass keeps", rows)
	}

	var value string

	if err := db.QueryRow(`SELECT "value" FROM "options" WHERE "key" = 'branding'`).Scan(&value); err != nil {
		t.Fatalf("reading the branding row: %v", err)
	}

	if value != `"two"` {
		t.Errorf("branding is stored as %s, want the last row written", value)
	}

	if _, err := db.Exec(`INSERT INTO "options" ("key", "value") VALUES ('branding', '"three"')`); err == nil {
		t.Error("the table accepted a duplicate key after the migration, want the options_key index to refuse it")
	}
}

// Every v8 primary key is handed to the backend's own counter: AUTO_INCREMENT on MariaDB,
// bigserial on PostgreSQL. The migration pass inserts the identifiers the v6 rows already
// carry, and the two backends treat an explicitly inserted identifier differently: MariaDB moves
// AUTO_INCREMENT past it, PostgreSQL does not move the sequence behind a bigserial at all. The
// tags table is the smallest place an administrator writes to after the migration, and its v6
// identifiers start at 1, so on PostgreSQL the first tag the server creates for itself is
// assigned the identifier 1, which the migrated data is already using.
func TestMigrateLiveOnMariaDBLeavesTheTagCounterPastTheMigratedTags(t *testing.T) {
	liveTagCounter(t, DialectMariadb)
}

func TestMigrateLiveOnPostgreSQLLeavesTheTagCounterPastTheMigratedTags(t *testing.T) {
	liveTagCounter(t, DialectPostgresql)
}

// liveTagCounter migrates the deployment and then does what the administrator does with it:
// adds a tag through the server's own tag adapter, which leaves the new identifier to the
// backend. The tag lands beside the migrated ones.
func liveTagCounter(t *testing.T, dialect Dialect) {
	t.Helper()

	db := liveDatabase(t, dialect)

	liveMigrate(t, db, dialect)

	tags := NewTags(db, dialect)

	ctx := context.Background()

	stored, err := tags.AllTags(ctx)
	if err != nil {
		t.Fatalf("AllTags on %s: %v", dialect, err)
	}

	if len(stored) == 0 {
		t.Fatalf("the database migrated on %s holds no tags to add to", dialect)
	}

	highest := uint64(0)

	for _, tag := range stored {
		if tag.ID > highest {
			highest = tag.ID
		}
	}

	next := append(stored, domain.Tag{Label: "Harbor Patrol", LED: "yellow", Alert: "none", Order: uint(len(stored))})

	if err := tags.SaveTags(ctx, next); err != nil {
		t.Fatalf("adding a tag to the database migrated on %s: %v", dialect, err)
	}

	var added uint64

	if err := db.QueryRow(`SELECT "tagId" FROM "tags" WHERE "label" = 'Harbor Patrol'`).Scan(&added); err != nil {
		t.Fatalf("reading back the tag added on %s: %v", dialect, err)
	}

	if added <= highest {
		t.Errorf("the tag added on %s is stored with the identifier %d, want a number past the highest migrated tag identifier %d",
			dialect, added, highest)
	}

	counted := 0

	if err := db.QueryRow(`SELECT COUNT(*) FROM "tags"`).Scan(&counted); err != nil {
		t.Fatalf("counting the tags on %s: %v", dialect, err)
	}

	if counted != len(stored)+1 {
		t.Errorf("the tags table on %s holds %d rows after the tag was added, want the %d migrated tags and the added one",
			dialect, counted, len(stored))
	}
}

// The tag counter is the smallest instance of the problem; the general case is that
// every migrated table whose identifiers the pass wrote itself has to hand out free
// identifiers afterwards. The groups table is one the administrator edits by hand,
// its rows arrive through the same explicit-identifier insert as the tags', and the
// server's own group adapter is what writes them, so the journey here is the one the
// upgrade actually takes.
func TestMigrateLiveOnPostgreSQLAdvancesTheMigratedGroupCounter(t *testing.T) {
	db := liveDatabase(t, DialectPostgresql)

	liveMigrate(t, db, DialectPostgresql)

	var highest int

	if err := db.QueryRow(`SELECT COALESCE(MAX("groupId"), 0) FROM "groups"`).Scan(&highest); err != nil {
		t.Fatalf("reading the highest migrated group identifier on PostgreSQL: %v", err)
	}

	if highest == 0 {
		t.Fatalf("the database migrated on PostgreSQL holds no groups to collide with")
	}

	mustExec(t, db, `INSERT INTO "groups" ("label", "order") VALUES ('Harbor Patrol', 99)`)

	var added int

	if err := db.QueryRow(`SELECT "groupId" FROM "groups" WHERE "label" = 'Harbor Patrol'`).Scan(&added); err != nil {
		t.Fatalf("reading back the group added on PostgreSQL: %v", err)
	}

	if added <= highest {
		t.Errorf("the group added on PostgreSQL is stored with the identifier %d, want a number past the highest migrated group identifier %d",
			added, highest)
	}
}

// The set's probe is read against an engine rather than against a recorded list, because
// whether a foreign key's own key serves one of the set's column lists is a fact about the
// engine: on MariaDB eight of the ten lists are already carried by the keys its foreign keys
// make, and on PostgreSQL a foreign key makes no key at all. Both backends are brought to
// the state the startup step meets — the recorded v6.6.3 deployment seeded, the dialect's own
// schema copy applied, the recording-length column stage and the search index run — and what
// the probe reports as missing is what the copy and the engine's own keys do not carry.
func TestCallIndexSetProbeLiveOnMariaDBNamesTheKeysItsForeignKeysAlreadyCarry(t *testing.T) {
	liveCallIndexProbe(t, DialectMariadb)
}

func TestCallIndexSetProbeLiveOnPostgreSQLNamesTheCoveringKeyAlone(t *testing.T) {
	liveCallIndexProbe(t, DialectPostgresql)
}

func liveCallIndexProbe(t *testing.T, dialect Dialect) {
	t.Helper()

	db := liveDatabase(t, dialect)

	ctx := context.Background()

	s := newStore(db, dialect)

	if err := ApplySchema(ctx, db, dialect); err != nil {
		t.Fatalf("ApplySchema on %s: %v", dialect, err)
	}

	// The covering key indexes the recording-length column, so the stage that adds it runs
	// first here exactly as it does in the startup pass, and the search index shares the
	// table with the covering key.
	if err := ensureCallAudioLengthColumn(ctx, s); err != nil {
		t.Fatalf("ensureCallAudioLengthColumn on %s: %v", dialect, err)
	}

	if err := ensureCallsSearchIndex(ctx, s); err != nil {
		t.Fatalf("ensureCallsSearchIndex on %s: %v", dialect, err)
	}

	missing, err := missingCallIndexes(ctx, s)
	if err != nil {
		t.Fatalf("missingCallIndexes on %s: %v", dialect, err)
	}

	// The report is the set's own order, so the talkgroup pair, which the set names before
	// the covering key, is reported before it.
	want := []string{"calls_timestamp_cover"}

	if dialect == DialectMariadb || dialect == DialectMysql {
		// The talkgroup lookup pair is the one member no foreign key carries there: the key
		// the schema's reference makes covers the table's system column alone, which is a
		// prefix of the pair and not the pair.
		want = []string{"talkgroups_system_ref", "calls_timestamp_cover"}
	}

	got := missingNames(missing)

	if !slices.Equal(got, want) {
		t.Errorf("missingCallIndexes on %s reports (%s), want (%s): every other member of the set "+
			"is carried over its whole column list, by the copied statements on PostgreSQL and by "+
			"the keys the foreign keys make on MariaDB",
			dialect, strings.Join(got, ", "), strings.Join(want, ", "))
	}

	carries, err := hasCallIndexShape(ctx, s, "calls")
	if err != nil {
		t.Fatalf("hasCallIndexShape(calls) on %s: %v", dialect, err)
	}

	// The pair keys this set excludes are not there under any name, and the shipped search
	// index is found under its own.
	for _, name := range []string{"calls_system_ts", "calls_talkgroup_ts"} {
		for _, index := range carries {
			if index.name == name {
				t.Errorf("%s carries %s over (%s) on %s, a key this set does not name",
					dialect, name, strings.Join(index.columns, ", "), "calls")
			}
		}
	}

	found := false

	for _, index := range carries {
		if index.name == "calls_idx" {
			found = true
		}
	}

	if !found {
		t.Errorf("hasCallIndexShape(calls) on %s reports (%s), wanting the shipped search index "+
			"among the table's indexes", dialect, strings.Join(indexShapes(carries), ", "))
	}

	if dialect != DialectMariadb && dialect != DialectMysql {
		return
	}

	// The delayed table's key is the case the comparison has to fold: the column is declared
	// `callID` in the copy that makes the table there while its foreign key names the same
	// column `callId`, and it is that key the member is left out for.
	carries, err = hasCallIndexShape(ctx, s, "delayed")
	if err != nil {
		t.Fatalf("hasCallIndexShape(delayed) on %s: %v", dialect, err)
	}

	folded := false

	for _, index := range carries {
		if len(index.columns) == 1 && strings.EqualFold(index.columns[0], "callId") {
			folded = true
		}
	}

	if !folded {
		t.Errorf("hasCallIndexShape(delayed) on %s reports (%s), wanting a key over the call "+
			"column as the engine spells that column's name", dialect, strings.Join(indexShapes(carries), ", "))
	}
}

// A legacy-written deployment taken through the whole startup pass ends holding every member
// of the call history set. This is the measurement the dialects whose schema copy declares no
// key over the recording-length column need: what each backend carries afterwards is the nine
// keys the copy declares, the keys its own foreign keys make, and the one key the startup pass
// creates after the column stage.
func TestMigrateLiveOnMariaDBLeavesTheCallHistorySetOnTheMigratedDatabase(t *testing.T) {
	liveCallIndexSetAfterMigrate(t, DialectMariadb)
}

func TestMigrateLiveOnPostgreSQLLeavesTheCallHistorySetOnTheMigratedDatabase(t *testing.T) {
	liveCallIndexSetAfterMigrate(t, DialectPostgresql)
}

func liveCallIndexSetAfterMigrate(t *testing.T, dialect Dialect) {
	t.Helper()

	db := liveDatabase(t, dialect)

	ctx := context.Background()

	s := newStore(db, dialect)

	lines := liveMigrate(t, db, dialect)

	cover := coveringKeyMember()

	// The covering key belongs to no reference the schema declares, so neither the copied
	// statements nor a foreign key makes it on either backend: the pass names the key it adds.
	created := false

	said := []string{}

	for _, line := range lines {
		said = append(said, line.level+" "+line.message)

		if line.level == domain.LogLevelInfo && strings.Contains(line.message, cover.name) {
			created = true
		}
	}

	if !created {
		t.Errorf("the pass on %s said (%s), want the line naming the covering key it creates on the calls "+
			"table: that key indexes the recording-length column, which this deployment gains from the pass "+
			"itself, so nothing else here can create it", dialect, strings.Join(said, " | "))
	}

	missing, err := missingCallIndexes(ctx, s)
	if err != nil {
		t.Fatalf("missingCallIndexes on %s: %v", dialect, err)
	}

	if got := missingNames(missing); len(got) != 0 {
		t.Errorf("the migrated database on %s lacks (%s) of the call history set, wanting nothing missing: "+
			"every member is carried over its whole column list, by the copied statements, by the keys the "+
			"backend's own foreign keys make, or by the covering key the pass creates",
			dialect, strings.Join(got, ", "))
	}

	// The key is read under its own name and over its own column list, because a key of that
	// shape is only ever made on purpose.
	carries, err := hasCallIndexShape(ctx, s, cover.table)
	if err != nil {
		t.Fatalf("hasCallIndexShape(calls) on %s: %v", dialect, err)
	}

	found := false

	for _, index := range carries {
		if !strings.EqualFold(index.name, cover.name) {
			continue
		}

		found = true

		if !carriesColumnList(carries, cover.columns, true) {
			t.Errorf("%s carries %s on calls, and the migrated calls table reports (%s): the key the pass "+
				"creates has to carry the columns the history reads report, in the order the set names them",
				dialect, index.name, strings.Join(indexShapes(carries), ", "))
		}
	}

	if !found {
		t.Errorf("the migrated calls table on %s reports (%s), want %s among its indexes",
			dialect, strings.Join(indexShapes(carries), ", "), cover.name)
	}
}

// liveV6CallsTableDDL returns the statements that put the v6.6.3 calls table back on a live backend of
// this dialect after a migration pass has finished with it: the table as that dialect's own v6.6.3
// deployment declares it, with its key. They are the two statements of the recorded fixture's DDL for
// that table, because a deployment whose calls migration never finished has exactly those in it.
func liveV6CallsTableDDL(dialect Dialect) []string {
	statements := liveMariadbSchema

	if dialect == DialectPostgresql {
		statements = livePostgresqlSchema
	}

	for i, statement := range statements {
		if !strings.Contains(statement, "rdioScannerCalls") {
			continue
		}

		return statements[i : i+2]
	}

	return nil
}

// liveLeftoverCallsTable puts the v6.6.3 calls table back on a live backend, holding the fixture's rows.
// That is the table a deployment whose calls migration never finished still has sitting in it, and the
// server meets it on a database that already has the v8 calls table filled.
func liveLeftoverCallsTable(t *testing.T, db *sql.DB, dialect Dialect) {
	t.Helper()

	applyStatements(t, db, liveV6CallsTableDDL(dialect))

	seedLiveTable(t, db, dialect, "rdioScannerCalls")
}

// liveUnreadableRow puts a timestamp this server cannot read into one row of the v6 calls table on a
// live backend. A datetime column holds only a value its own backend's timestamp rule accepts, and the
// calls migration counts a timestamp it cannot turn into a time as a row it never read, so the column is
// first widened to text wide enough for the table's own key — the key length is why it is not a plain
// text column — with every other row's timestamp rendered in the shape the v6 databases write. The row
// that gets the unreadable value keeps every other value it had: the only thing about it the arrange
// changes is the column the pass reads.
func liveUnreadableRow(t *testing.T, db *sql.DB, dialect Dialect, rowId int) {
	t.Helper()

	widen := ""

	switch dialect {
	case DialectMariadb:
		widen = `ALTER TABLE "rdioScannerCalls" MODIFY "dateTime" varchar(64) not null`
	case DialectPostgresql:
		widen = `ALTER TABLE "rdioScannerCalls" ALTER COLUMN "dateTime" TYPE varchar(64) ` +
			`USING to_char("dateTime", 'YYYY-MM-DD HH24:MI:SS') || ' +0000 UTC'`
	default:
		t.Fatalf("liveUnreadableRow has no statement for the %s backend", dialect)
	}

	if _, err := db.ExecContext(context.Background(), widen); err != nil {
		t.Fatalf("%s: %v", widen, err)
	}

	s := newStore(db, dialect)

	if _, err := s.exec(context.Background(), `UPDATE "rdioScannerCalls" SET "dateTime" = ? WHERE "id" = ?`, "not a timestamp", rowId); err != nil {
		t.Fatalf("writing the timestamp this server cannot read: %v", err)
	}

	value := ""

	query := `SELECT "dateTime" FROM "rdioScannerCalls" WHERE "id" = ?`

	if err := s.queryRow(context.Background(), query, rowId).Scan(&value); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	if value != "not a timestamp" {
		t.Fatalf("the row's dateTime reads %q: the row has to hold the text itself, because the pass counts a "+
			"row whose timestamp it cannot read as one it never saw", value)
	}
}

// liveProgressText returns the messages a live pass reported, in the order it said them.
func liveProgressText(lines []progressLine) []string {
	messages := make([]string, 0, len(lines))

	for _, line := range lines {
		messages = append(messages, line.message)
	}

	return messages
}

// liveAnnouncedCalls reports whether a live pass announced that it was migrating calls.
func liveAnnouncedCalls(lines []progressLine) bool {
	for _, line := range lines {
		if strings.Contains(line.message, "migrating calls") {
			return true
		}
	}

	return false
}

// liveWarnedAbout returns the warning lines a live pass named one table with.
func liveWarnedAbout(lines []progressLine, table string) []string {
	warnings := []string{}

	for _, line := range lines {
		if line.level == domain.LogLevelWarn && strings.Contains(line.message, table) {
			warnings = append(warnings, line.message)
		}
	}

	return warnings
}

// On MariaDB and on PostgreSQL: a leftover calls table whose every row the live calls table already
// holds, or the migration archive already holds, is finished. Each backend gets its own database of its
// own dialect, seeded with the same recorded v6.6.3 deployment the sqlite tests seed.
func TestMigrateLiveOnMariaDBFinishesALeftoverCallsTableTheLiveCallsTableAlreadyHolds(t *testing.T) {
	liveFinishedLeftoverCallsTable(t, DialectMariadb)
}

func TestMigrateLiveOnPostgreSQLFinishesALeftoverCallsTableTheLiveCallsTableAlreadyHolds(t *testing.T) {
	liveFinishedLeftoverCallsTable(t, DialectPostgresql)
}

// liveFinishedLeftoverCallsTable runs the finished-table reading on a live backend: migrate the recorded
// v6.6.3 deployment, put its calls table back with the same rows in it, and start again. The second
// start has nothing to carry, so it says nothing about calls and drops the table it found finished.
func liveFinishedLeftoverCallsTable(t *testing.T, dialect Dialect) {
	t.Helper()

	db := liveDatabase(t, dialect)

	liveMigrate(t, db, dialect)

	carried := rowsOf(t, db, "calls", `"callId"`)
	archived := archivedRows(t, db, "rdioScannerCalls")

	liveLeftoverCallsTable(t, db, dialect)

	if held := countRows(t, db, "rdioScannerCalls"); held != len(carried)+len(archived) {
		t.Fatalf("the leftover calls table on %s holds %d rows, want the %d the migration carried plus the %d "+
			"it archived", dialect, held, len(carried), len(archived))
	}

	second := liveMigrate(t, db, dialect)

	if liveAnnouncedCalls(second) {
		t.Errorf("the %s pass with nothing to carry announced it was migrating calls: %v", dialect, liveProgressText(second))
	}

	if warnings := liveWarnedAbout(second, "rdioScannerCalls"); len(warnings) != 0 {
		t.Errorf("the %s pass said something about a finished calls table: %v", dialect, warnings)
	}

	if hasTable(t, db, dialect, "rdioScannerCalls") {
		t.Errorf("the %s pass left a calls table whose every row is accounted for: %v", dialect, liveProgressText(second))
	}

	if now := rowsOf(t, db, "calls", `"callId"`); !equalStrings(flat(now), flat(carried)) {
		t.Errorf("the %s pass over a finished deployment changed the calls table:\n  was  %v\n  now  %v",
			dialect, flat(carried), flat(now))
	}

	if now := archivedRows(t, db, "rdioScannerCalls"); !equalStrings(flat(now), flat(archived)) {
		t.Errorf("the %s pass over a finished deployment archived a row a second time:\n  was  %v\n  now  %v",
			dialect, flat(archived), flat(now))
	}
}

func TestMigrateLiveOnMariaDBLeavesACallsTableWithARowItCannotReadWithoutAnnouncingAMigration(t *testing.T) {
	liveUnreadableRowKeepsTheTable(t, DialectMariadb)
}

func TestMigrateLiveOnPostgreSQLLeavesACallsTableWithARowItCannotReadWithoutAnnouncingAMigration(t *testing.T) {
	liveUnreadableRowKeepsTheTable(t, DialectPostgresql)
}

// liveUnreadableRowKeepsTheTable runs the unread-row reading on a live backend, from the seeded v6.6.3
// deployment with one of its call timestamps rewritten into something this server cannot read. The first
// start carries the rows it can and leaves the table with the row it could not read; the start after that
// has nothing to carry, so the only thing it says about calls is the sentence naming the table and the
// row, and not a migration it is not running.
func liveUnreadableRowKeepsTheTable(t *testing.T, dialect Dialect) {
	t.Helper()

	db := liveDatabase(t, dialect)

	liveUnreadableRow(t, db, dialect, 702)

	first := liveMigrate(t, db, dialect)

	if !hasString(liveProgressText(first), "migrating calls...") {
		t.Fatalf("the %s pass that carried the readable calls did not announce them, so the row it could not "+
			"read is not the only odd thing about this database: %v", dialect, liveProgressText(first))
	}

	carried := rowsOf(t, db, "calls", `"callId"`)
	archived := archivedRows(t, db, "rdioScannerCalls")
	unreadable := readRows(t, db, `SELECT "id", "dateTime", "audioName" FROM "rdioScannerCalls" WHERE "id" = 702`)

	if len(unreadable) != 1 {
		t.Fatalf("the calls table does not hold the row 702 whose timestamp was rewritten: %v", unreadable)
	}

	second := liveMigrate(t, db, dialect)

	warn := "the migration left the calls table where it found it: a row of it could not be read"

	if !hasString(liveProgressText(second), warn) {
		t.Errorf("the %s pass that left the calls table said nothing about why: %v", dialect, liveProgressText(second))
	}

	if !hasTable(t, db, dialect, "rdioScannerCalls") {
		t.Errorf("a %s pass dropped a calls table holding the row it could not read: %v", dialect, liveProgressText(second))
	}

	if hasString(liveProgressText(second), "migrating calls...") {
		t.Errorf("the %s pass announced a calls migration for a table whose rows it does not carry: %v", dialect, liveProgressText(second))
	}

	if now := readRows(t, db, `SELECT "id", "dateTime", "audioName" FROM "rdioScannerCalls" WHERE "id" = 702`); !equalStrings(flat(now), flat(unreadable)) {
		t.Errorf("the %s pass rewrote the row it could not read:\n  was  %v\n  now  %v", dialect, flat(unreadable), flat(now))
	}

	if now := rowsOf(t, db, "calls", `"callId"`); !equalStrings(flat(now), flat(carried)) {
		t.Errorf("the %s pass left a row behind and changed which rows the calls table holds:\n  was  %v\n  now  %v",
			dialect, flat(carried), flat(now))
	}

	if now := archivedRows(t, db, "rdioScannerCalls"); !equalStrings(flat(now), flat(archived)) {
		t.Errorf("the %s pass archived a row a second time:\n  was  %v\n  now  %v", dialect, flat(archived), flat(now))
	}
}
