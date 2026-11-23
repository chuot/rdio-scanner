//go:build live

package persistence

// The live clearing tests run the clearing over the recorded damaged deployment on the backends the
// migration's live harness starts, so that the unit rows it removes and the ones it leaves are the
// same rows, named by the same stored values, on MariaDB and PostgreSQL as they are on a file.

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"testing"

	"rdio-scanner/internal/domain"
)

// damagedLiveSeedOrder is the order the recorded damaged deployment's rows are seeded in: every table
// a foreign key looks a reference up in comes before the table that names it. The sqlite fixture was
// written with enforcement switched off, so the order is what makes the same rows seedable here. The
// findings record comes last because it names the other tables without a foreign key pointing at them,
// and it is seeded because it is what the fixture's own migration pass left behind: a live clearing that
// was never given one would prove nothing about clearing it.
var damagedLiveSeedOrder = []string{
	"systems",
	"tags",
	"groups",
	"talkgroups",
	"talkgroupGroups",
	"sites",
	"units",
	"calls",
	"callFrequencies",
	"callPatches",
	"callUnits",
	"delayed",
	"accesses",
	"apikeys",
	"downstreams",
	"dirwatches",
	"logs",
	"options",
	"migrationFindings",
}

// damagedLiveUnseedableUnits are the rows of the recorded fixture a live backend cannot be handed,
// with the reason each one is left out rather than quietly dropped from the expectation.
var damagedLiveUnseedableUnits = map[uint64]string{
	308: "its system 999 is not in the deployment, and a live backend's foreign key refuses the row " +
		"the sqlite fixture was written with enforcement switched off",
}

func TestClearMigrationDamageLiveOnMariaDBRemovesTheSameUnitsAsOnSqlite(t *testing.T) {
	liveClearDamagedDeployment(t, DialectMariadb)
}

func TestClearMigrationDamageLiveOnPostgreSQLRemovesTheSameUnitsAsOnSqlite(t *testing.T) {
	liveClearDamagedDeployment(t, DialectPostgresql)
}

// liveClearDamagedDeployment opens the recorded damaged deployment on a running backend, clears it,
// and holds the run to the same removed rows, the same survivors, the same removal of the findings
// record the fixture's own migration pass left, and the same silence about the archive and the legacy
// tables the deployment holds none of.
func liveClearDamagedDeployment(t *testing.T, dialect Dialect) {
	t.Helper()

	db := openLiveDamagedDeployment(t, dialect)

	held := readRows(t, db, `SELECT "tableName","rowId" FROM "migrationFindings" ORDER BY "tableName","rowId"`)

	if len(held) == 0 {
		t.Fatalf("the damaged deployment on %s holds no findings, so a clearing that drops its record proves nothing", dialect)
	}

	report, err := NewMaintenance(db, dialect).ClearMigrationDamage(context.Background())
	if err != nil {
		t.Fatalf("ClearMigrationDamage on %s: %v", dialect, err)
	}

	if len(report.Units) != len(damagedDeploymentRemovedUnits) {
		t.Fatalf("the clearing on %s removed %d unit rows, want the %d it removes on sqlite: %+v",
			dialect, len(report.Units), len(damagedDeploymentRemovedUnits), report.Units)
	}

	for _, want := range damagedDeploymentRemovedUnits {
		got, ok := reportedUnit(report.Units, want.ID)
		if !ok {
			t.Errorf("the clearing on %s left unit %d (%q, unitRef %d) standing, want it removed: %+v",
				dialect, want.ID, want.Label, want.UnitRef, report.Units)

			continue
		}

		if got != want {
			t.Errorf("the clearing on %s removed %+v, want %+v: the same row named by the same stored values as on sqlite",
				dialect, got, want)
		}
	}

	ctx := context.Background()

	for _, want := range damagedDeploymentSurvivingUnits {
		if _, left := damagedLiveUnseedableUnits[want.ID]; left {
			continue
		}

		stored := readLiveUnitRow(t, db, dialect, want.ID)

		if stored != want {
			t.Errorf("unit %d reads %+v on %s after the clearing, want it stored exactly as it was: %+v",
				want.ID, stored, dialect, want)
		}

		if _, ok := reportedUnit(report.Units, want.ID); ok {
			t.Errorf("the clearing on %s reported unit %d (%q) as removed, want it kept", dialect, want.ID, want.Label)
		}
	}

	systems, err := NewSystems(db, dialect).AllSystems(ctx)
	if err != nil {
		t.Fatalf("AllSystems on %s after the clearing: %v", dialect, err)
	}

	for _, want := range []struct {
		systemID uint64
		unitIDs  []uint64
	}{
		{systemID: 101, unitIDs: []uint64{301, 302, 304}},
		{systemID: 102, unitIDs: []uint64{305, 306}},
	} {
		system, ok := systems.ByID(want.systemID)
		if !ok {
			t.Errorf("AllSystems on %s answered no system %d: %+v", dialect, want.systemID, systems)

			continue
		}

		if len(system.Units) != len(want.unitIDs) {
			t.Errorf("system %d on %s holds %d units after the clearing, want %d: %+v",
				want.systemID, dialect, len(system.Units), len(want.unitIDs), system.Units)

			continue
		}

		for i, id := range want.unitIDs {
			if system.Units[i].ID != id {
				t.Errorf("system %d on %s holds unit %d at position %d, want unit %d: %+v",
					want.systemID, dialect, system.Units[i].ID, i, id, system.Units)
			}
		}
	}

	if report.DroppedArchive || len(report.ArchiveRows) != 0 || len(report.DroppedTables) != 0 {
		t.Errorf("the clearing on %s reported dropping the archive or a legacy table (%+v) for a deployment the schema alone wrote, "+
			"which holds no archive table and none of the legacy names: the run must clear nothing and fail on nothing", dialect, report)
	}

	tables := tableNames(t, db, dialect)

	for _, live := range legacyTables {
		found := false

		for _, name := range tables {
			if name == live {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("the clearing on %s left the deployment without the %s table this server writes: %v", dialect, live, tables)
		}
	}

	// The record of what the deployment's own migration pass announced goes with the rows the run cleared,
	// on a backend as on a file: it is dropped, and the run says nothing about dropping it.
	if hasTable(t, db, dialect, migrationFindingsTable) {
		t.Errorf("the clearing on %s left the findings record standing with %d rows, want it gone with what it cleared: "+
			"it held %v", dialect, countRows(t, db, migrationFindingsTable), held)
	}

	for _, kept := range damagedDeploymentRowCounts {
		want := kept.rows

		// The rows the live seed cannot carry are the whole difference between the recorded deployment
		// and this one, named rather than quietly allowed: unit 308 is left out of the seed, so its row
		// is absent here before the clearing and after it alike.
		if kept.table == "units" {
			want -= len(damagedLiveUnseedableUnits)
		}

		if got := countRows(t, db, kept.table); got != want {
			t.Errorf("on %s the %s table holds %d rows after the clearing, want the %d the deployment held: %v",
				dialect, kept.table, got, want, tables)
		}
	}
}

// openLiveDamagedDeployment gives the backend the schema this server writes and the rows of the
// recorded damaged deployment: the deployment a legacy server migrated and left damaged, with no
// legacy table left standing and no archive table, which is what the migration's own gate leaves a
// database that never held a legacy table.
func openLiveDamagedDeployment(t *testing.T, dialect Dialect) *sql.DB {
	t.Helper()

	settings := liveSettings(t, dialect)

	db, err := Open(settings)
	if err != nil {
		t.Fatalf("Open(%s): %v", dialect, err)
	}

	t.Cleanup(func() { db.Close() })

	livePing(t, db, settings)

	resetLive(t, db, dialect)

	if err := ApplySchema(context.Background(), db, dialect); err != nil {
		t.Fatalf("ApplySchema on %s: %v", dialect, err)
	}

	// The startup pass gives a database the findings record, and this deployment is written by the schema
	// alone, so the record the fixture's migration pass left is given here before its rows are copied in.
	if err := ensureMigrationFindings(context.Background(), newStore(db, dialect)); err != nil {
		t.Fatalf("giving %s the findings record: %v", dialect, err)
	}

	seedDamagedDeploymentLive(t, db, dialect)

	return db
}

// seedDamagedDeploymentLive copies the rows of the deployment the sqlite clearing tests clear — the
// recorded fixture taken through this server's own startup pass, with the rows that test adds — into
// the backend through the same store the repositories write through, so a live run clears the same
// deployment rather than a second copy of it written out again in another language.
func seedDamagedDeploymentLive(t *testing.T, db *sql.DB, dialect Dialect) {
	t.Helper()

	fixture := openDamagedDeploymentDatabase(t)

	defer fixture.Close()

	s := newStore(db, dialect)

	for _, table := range damagedLiveSeedOrder {
		columns, _ := liveFixtureColumns(t, fixture, table)
		rows := liveFixtureRows(t, fixture, table)

		if len(rows) == 0 {
			t.Fatalf("the recorded damaged deployment holds no %s rows, so seeding %s on %s proves nothing", table, table, dialect)
		}

		if table == "units" {
			rows = withoutLiveUnseedableUnits(t, rows, columns)
		}

		spelled := liveSeedColumns(t, table, liveBackendColumns(t, s, table), columns)

		names := make([]string, 0, len(spelled))

		for _, column := range spelled {
			names = append(names, `"`+column+`"`)
		}

		holders := make([]string, 0, len(spelled))

		for range spelled {
			holders = append(holders, "?")
		}

		query := `INSERT INTO "` + table + `" (` + strings.Join(names, ", ") + `) VALUES (` + strings.Join(holders, ", ") + `)`

		flags := liveBooleanColumns(t, dialect, table)

		for _, row := range rows {
			args := make([]any, 0, len(row))

			for i, value := range row {
				if flags[spelled[i]] {
					number, _ := value.(int64)

					value = number != 0
				}

				args = append(args, value)
			}

			if _, err := s.exec(context.Background(), query, args...); err != nil {
				t.Fatalf("seeding a %s row into %s: %v", table, dialect, err)
			}
		}
	}
}

// liveBackendColumns asks the backend's own catalog for the columns it declares for one table, in the
// order it declares them.
func liveBackendColumns(t *testing.T, s store, table string) []string {
	t.Helper()

	query := `SELECT "column_name" FROM "information_schema"."columns" WHERE "table_name" = ? ORDER BY "ordinal_position"`

	if s.dialect == DialectMariadb || s.dialect == DialectMysql {
		query = `SELECT "COLUMN_NAME" FROM "information_schema"."COLUMNS" WHERE "TABLE_SCHEMA" = DATABASE() AND "TABLE_NAME" = ? ORDER BY "ORDINAL_POSITION"`
	}

	rows, err := s.query(context.Background(), query, table)
	if err != nil {
		t.Fatalf("reading the columns of %s on %s: %v", table, s.dialect, err)
	}

	defer rows.Close()

	names := []string{}

	for rows.Next() {
		var name string

		if err := rows.Scan(&name); err != nil {
			t.Fatalf("reading the name of a column of %s on %s: %v", table, s.dialect, err)
		}

		names = append(names, name)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("reading the columns of %s on %s: %v", table, s.dialect, err)
	}

	if len(names) == 0 {
		t.Fatalf("%s on %s declares no columns, so nothing can be seeded into it", table, s.dialect)
	}

	return names
}

// liveSeedColumns pairs each column of the sqlite deployment with the name this backend gives that same
// column, so that the seed writes each value into the column it belongs to on every backend. The two
// schemas do not always spell a column alike — the file writes logid where PostgreSQL and MySQL write
// logId — and a seed that spelled a column the file's way would be refused by the backend rather than
// reproducing the deployment in it. A column the deployment holds that the backend does not answer is a
// deployment this backend cannot hold, and the seed says so instead of leaving the value out.
func liveSeedColumns(t *testing.T, table string, backend, deployment []string) []string {
	t.Helper()

	spelled := map[string]string{}

	for _, name := range backend {
		spelled[strings.ToLower(name)] = name
	}

	names := make([]string, 0, len(deployment))

	for _, column := range deployment {
		name, ok := spelled[strings.ToLower(column)]
		if !ok {
			t.Fatalf("the %s table on this backend declares no column answering the deployment's %s column, which this backend cannot hold: %v",
				table, column, backend)
		}

		names = append(names, name)
	}

	return names
}

// withoutLiveUnseedableUnits leaves out the unit rows a live backend's foreign key refuses, so that a
// live run seeds the rows the backend can hold and says which recorded row it could not carry, rather
// than failing the seed or quietly narrowing what it asserts.
func withoutLiveUnseedableUnits(t *testing.T, rows [][]any, columns []string) [][]any {
	t.Helper()

	position := -1

	for i, column := range columns {
		if column == "unitId" {
			position = i

			break
		}
	}

	if position < 0 {
		t.Fatal("the recorded damaged deployment's units table holds no unitId column")
	}

	kept := [][]any{}

	for _, row := range rows {
		id, ok := row[position].(int64)
		if !ok {
			t.Fatalf("a units row reads its unitId as %v, want a number", row[position])
		}

		if reason, left := damagedLiveUnseedableUnits[uint64(id)]; left {
			t.Logf("the live seed leaves unit %d out: %s", id, reason)

			continue
		}

		kept = append(kept, row)
	}

	if len(kept) == 0 {
		t.Fatal("the recorded damaged deployment's units table holds no row a live backend can seed")
	}

	return kept
}

// liveBooleanColumns names the columns this backend's own schema declares as booleans. This server
// spells a flag as 0 or 1 in a sqlite file and as a boolean in PostgreSQL, and pgx will not read the
// file's 1 as true.
var liveBooleanColumnIn = regexp.MustCompile(`"([A-Za-z0-9_]+)" boolean`)

func liveBooleanColumns(t *testing.T, dialect Dialect, table string) map[string]bool {
	t.Helper()

	schema, err := schemaFor(dialect)
	if err != nil {
		t.Fatalf("schemaFor(%s): %v", dialect, err)
	}

	flags := map[string]bool{}

	for _, statement := range schema {
		if !strings.Contains(statement, `CREATE TABLE IF NOT EXISTS "`+table+`"`) {
			continue
		}

		for _, match := range liveBooleanColumnIn.FindAllStringSubmatch(statement, -1) {
			flags[match[1]] = true
		}
	}

	return flags
}

// readLiveUnitRow reads one unit row back through the store, which is what rewrites the placeholder
// into the shape the backend asks for.
func readLiveUnitRow(t *testing.T, db *sql.DB, dialect Dialect, id uint64) domain.Unit {
	t.Helper()

	var (
		label    string
		order    int64
		systemID int64
		unitRef  int64
		unitFrom int64
		unitTo   int64
	)

	query := `SELECT "label", "order", "systemId", "unitRef", "unitFrom", "unitTo" FROM "units" WHERE "unitId" = ?`

	if err := newStore(db, dialect).queryRow(context.Background(), query, id).
		Scan(&label, &order, &systemID, &unitRef, &unitFrom, &unitTo); err != nil {
		t.Fatalf("reading unit %d on %s: %v", id, dialect, err)
	}

	return domain.Unit{
		ID:       id,
		Label:    label,
		Order:    uint(order),
		SystemID: uint64(systemID),
		UnitRef:  uint(unitRef),
		UnitFrom: uint(unitFrom),
		UnitTo:   uint(unitTo),
	}
}

// TestClearMigrationDamageLiveNamesNoLegacyTableForABackendThatHoldsNone is the deployment the
// migration leaves behind on a backend that never held a legacy table: nothing to drop, and a run that
// says so instead of failing on a table it was never given.
func TestClearMigrationDamageLiveNamesNoLegacyTableForABackendThatHoldsNone(t *testing.T) {
	for _, dialect := range liveBackends {
		db := openLiveDamagedDeployment(t, dialect)

		for _, name := range tableNames(t, db, dialect) {
			if strings.HasPrefix(name, "rdioScanner") || name == "migrationArchive" {
				t.Fatalf("%s holds %s, which the schema this server writes does not create: %v", dialect, name, tableNames(t, db, dialect))
			}
		}

		report, err := NewMaintenance(db, dialect).ClearMigrationDamage(context.Background())
		if err != nil {
			t.Fatalf("ClearMigrationDamage on %s: %v", dialect, err)
		}

		if len(report.DroppedTables) != 0 || report.DroppedArchive || len(report.ArchiveRows) != 0 {
			t.Errorf("the clearing on %s reported %d dropped tables, the archive dropped %v, and %d archived rows, "+
				"want nothing reported for a backend holding none of the legacy names and no archive table: %+v",
				dialect, len(report.DroppedTables), report.DroppedArchive, len(report.ArchiveRows), report)
		}
	}
}
