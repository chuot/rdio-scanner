//go:build live

// The budgets at the scales their figures were read at: the same fixture shape, the same inventoried
// statements and the same ceilings the ordinary suite checks at ten thousand calls, on a history an
// operator would actually have. The `live` tag is what keeps both the fixture and the run out of the
// ordinary suite, which pays for neither, and it is the tag the repository's other large-database test
// already carries.

package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestHistoryReadsStayWithinTheirBudgets holds every inventoried read of a deployment's history inside the
// ceiling its shape carries at sixty thousand calls, and again at one hundred and fifty thousand — the two
// scales the figures of the read budgets were read at. The plan-shape tests read the same statements over a
// forty-call database, where a read is cheap enough that a plan choice could go unnoticed; these are the
// scales at which a read that loses a key stops being merely slower and becomes a scan of the whole history.
//
// Each scale runs as its own subtest, so the file one scale seeded is released before the next one seeds
// its own.
func TestHistoryReadsStayWithinTheirBudgets(t *testing.T) {
	for _, calls := range []int{historyBudgetScale, historyUpperBudgetScale} {
		t.Run(fmt.Sprintf("%d calls", calls), func(t *testing.T) {
			db := historyFixtureDatabase(t, calls)

			span := historySpanOfTheDatabase(t, db)

			values := historyPlanValues(t, db, span)

			inventory := callHistoryStatementInventory(span)

			assertHistoryReadsWithinTheirBudgets(t, db, inventory, values, calls)
		})
	}
}

// historyRetentionDays is the retention window the prune is measured against: thirty of the ninety days of
// calls the fixture holds, which is the window that drops two thirds of a sixty thousand call history.
const historyRetentionDays = 30

// historyPruneCeiling is the cost of one retention pass held as an absolute limit rather than as a
// speed-up over the shipped schema: the delete that discards the calls a retention window lets go has to
// stay inside two seconds on a sixty thousand call history, which is the ceiling the measured set answers
// well under and the shipped schema misses by three orders of magnitude.
const historyPruneCeiling = 2 * time.Second

// TestRetentionPruneStaysWithinItsBudget runs the retention prune over a sixty thousand call history and
// holds it inside its ceiling. It is the same fixture the read budgets are judged on, because the delete is
// the other half of what the set was bought for: the covering key over the recording instant is what finds
// the rows to discard, and the keys the call's own tables carry over the call their rows belong to are what
// each cascading child delete costs. On the shipped schema those child tables carry nothing, and deleting
// the same rows costs hundreds of seconds.
//
// The plan is read first, so the shape the measurement times is on the record before the cost of it is.
// Each timed sample deletes the same rows inside a transaction that is rolled back, and the delete the
// repository actually issues is then run once for real, which is what shows the cascades removed the rows
// they are responsible for rather than only planning to.
func TestRetentionPruneStaysWithinItsBudget(t *testing.T) {
	db := historyFixtureDatabase(t, historyBudgetScale)

	ctx := context.Background()

	s := newStore(db, DialectSqlite)

	span := historySpanOfTheDatabase(t, db)

	// The cascades exist only while the connection enforces the foreign keys, which is what the string the
	// server opens its database with asks for. Without them the delete would remove the calls and leave the
	// rows of every call it removed behind.
	if !foreignKeysEnforced(t, db) {
		t.Fatal("the fixture's connection does not enforce foreign keys, so the prune measured here is not the " +
			"one the server runs: the child rows of a deleted call would stay in the database")
	}

	cutoff := span.newest.Add(-historyRetentionDays * 24 * time.Hour)

	prune := inventoriedStatementFor(t, statementsOfTheConfigurationWrites(), "prune delete").statement

	eligible := historyRowsOlderThan(t, db, cutoff)

	// The rows the pass discards are a property of the fixture, and the cost of the pass is read against
	// them: a thirty day retention over a ninety day archive drops two thirds of the history.
	if want := historyBudgetScale * (historyFixtureDays - historyRetentionDays) / historyFixtureDays; eligible < want-1000 || eligible > want+1000 {
		t.Fatalf("the prune discards %d of the %d calls the fixture holds, want about %d: two thirds of a ninety "+
			"day history is older than a thirty day retention", eligible, historyBudgetScale, want)
	}

	// The delete narrows the covering key on the range it deletes, and each cascading delete reaches the key
	// of the table it deletes from. Those child keys are what the prune pays for: the pair keys over the
	// system and the recording instant were measured and turned down, and the plans that check whether a
	// call's child rows still have a call beside them already reach the calls table through the key the
	// shipped schema carries, so a key beside that one has nothing left to improve.
	plan := strings.Join(queryPlan(t, db, prune, millisOf(cutoff)), " / ")

	assertNothingBuiltForTheQuery(t, "prune delete", plan)

	assertPlanHasStep(t, "prune delete", plan, "SEARCH calls USING COVERING INDEX "+coveringKeyIndex+" (timestamp<?)")

	for _, key := range prunedCallChildKeys() {
		if !planSeeksIndexOn(plan, key.index, key.column) {
			t.Errorf("the prune delete would run as %s: the cascade to %s does not reach the key over the call "+
				"its rows belong to", plan, key.index)
		}
	}

	times := measureHistoryPrune(t, db, prune, cutoff, eligible)

	median, worst := middleOf(times), slices.Max(times)

	if median > historyPruneCeiling {
		t.Errorf("the retention prune discards %d calls in %s, the middle of %d samples, the slowest %s: one "+
			"retention pass over a %d call history is held inside %s",
			eligible, median, len(times), worst, historyBudgetScale, historyPruneCeiling)
	} else {
		t.Logf("the retention prune discards %d calls in %s, the middle of %d samples, the slowest %s, "+
			"held inside %s", eligible, median, len(times), worst, historyPruneCeiling)
	}

	// The pass as the repository runs it, once for real, over the same rows.
	before := countCallChildRows(t, db, cutoff)

	removed, err := NewCalls(db, DialectSqlite).DeleteCallsBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteCallsBefore: %v", err)
	}

	if removed != eligible {
		t.Errorf("the prune removes %d calls, want the %d the fixture holds older than the retention", removed, eligible)
	}

	if left := countRows(t, db, "calls"); left != historyBudgetScale-eligible {
		t.Errorf("the prune leaves %d calls, want the %d the retention keeps", left, historyBudgetScale-eligible)
	}

	for _, key := range prunedCallChildKeys() {
		total, older := before[key.table][0], before[key.table][1]

		if left := countRows(t, db, key.table); left != total-older {
			t.Errorf("the prune leaves %d rows in %s, want the %d that belong to a retained call: %d of the "+
				"%d rows it held belonged to a call the prune deleted, and a cascade that did not run would "+
				"leave them there", left, key.table, total-older, older, total)
		}
	}

	// The history the prune ran over carries the set and nothing beside it: no key over the talkgroup and
	// the recording instant of a call, no second key led by the recording instant, and no key led by the
	// system other than the one the shipped schema carries.
	assertCallsTableCarriesNoTurnedDownKey(t, s, DialectSqlite)
}

// TestMigrateLiveOnMariaDBLeavesNoTurnedDownCallKey and its PostgreSQL twin read the keys a migrated
// deployment reports on the backends whose own schema copy declares no key over the recording-length
// column. What each of them ends holding is the key the copied statements declare, the keys the backend's
// foreign keys make for themselves, and the covering key the startup pass adds: none of the column lists
// that were measured and turned down is among them. MySQL is reached by the schema copies, which are
// compared name by name and column by column without a backend to stand on.
func TestMigrateLiveOnMariaDBLeavesNoTurnedDownCallKey(t *testing.T) {
	assertCallKeysOnAMigratedLiveDatabase(t, DialectMariadb)
}

func TestMigrateLiveOnPostgreSQLLeavesNoTurnedDownCallKey(t *testing.T) {
	assertCallKeysOnAMigratedLiveDatabase(t, DialectPostgresql)
}

func assertCallKeysOnAMigratedLiveDatabase(t *testing.T, dialect Dialect) {
	t.Helper()

	db := liveDatabase(t, dialect)

	liveMigrate(t, db, dialect)

	assertCallsTableCarriesNoTurnedDownKey(t, newStore(db, dialect), dialect)
}

// assertCallsTableCarriesNoTurnedDownKey reads every key the calls table reports on a database that has
// been through the startup pass, and rejects each column list that was measured and turned down: the pair
// key led by the talkgroup, the pair keys led by the system in the orders that were not chosen, and every
// key led by the recording instant that stops short of the covering form. It also holds that only one key
// leads with the recording instant, and only one with the system, and names which each one is.
func assertCallsTableCarriesNoTurnedDownKey(t *testing.T, s store, dialect Dialect) {
	t.Helper()

	carries, err := hasCallIndexShape(context.Background(), s, "calls")
	if err != nil {
		t.Fatalf("hasCallIndexShape(calls) on %s: %v", dialect, err)
	}

	shapes := indexShapes(carries)

	for _, rejected := range rejectedCallIndexShapes() {
		for _, index := range carries {
			if !slices.Equal(index.columns, rejected.columns) {
				continue
			}

			t.Errorf("the migrated calls table on %s holds %s over (%s), which is a column list that was "+
				"measured and turned down as %s: the keys it reports are (%s)",
				dialect, index.name, strings.Join(index.columns, ", "), rejected.name, strings.Join(shapes, ", "))
		}
	}

	timestampLed := []string{}
	systemLed := []string{}

	for _, index := range carries {
		if len(index.columns) == 0 {
			continue
		}

		if index.columns[0] == "timestamp" {
			timestampLed = append(timestampLed, index.name)
		}

		if index.columns[0] == "systemId" {
			systemLed = append(systemLed, index.name)
		}
	}

	if len(timestampLed) != 1 || !strings.EqualFold(timestampLed[0], coveringKeyIndex) {
		t.Errorf("the migrated calls table on %s leads with the recording instant in %v, want %s alone: the keys "+
			"it reports are (%s)", dialect, timestampLed, coveringKeyIndex, strings.Join(shapes, ", "))
	}

	if len(systemLed) != 1 || !strings.EqualFold(systemLed[0], shippedPairIndexName) {
		t.Errorf("the migrated calls table on %s leads with the system in %v, want the shipped %s alone: the keys "+
			"it reports are (%s)", dialect, systemLed, shippedPairIndexName, strings.Join(shapes, ", "))
	}
}

// prunedCallChildKeys names the four tables that hold rows belonging to a call, the key each one carries
// over the call those rows belong to, and the column that key is built on.
func prunedCallChildKeys() []struct{ table, index, column string } {
	return []struct{ table, index, column string }{
		{table: "delayed", index: delayedKeyIndex, column: "callId"},
		{table: "callUnits", index: "callunits_call", column: "callId"},
		{table: "callPatches", index: "callpatches_call", column: "callId"},
		{table: "callFrequencies", index: "callfrequencies_call", column: "callId"},
	}
}

// foreignKeysEnforced reads the pragma the server's own connection string turns on.
func foreignKeysEnforced(t *testing.T, db *sql.DB) bool {
	t.Helper()

	var enforced int

	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&enforced); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}

	return enforced != 0
}

// historyRowsOlderThan counts the calls the retention discards at a given cutoff.
func historyRowsOlderThan(t *testing.T, db *sql.DB, cutoff time.Time) int {
	t.Helper()

	var older int

	if err := db.QueryRow(`SELECT COUNT(*) FROM "calls" WHERE "timestamp" < ?`, millisOf(cutoff)).Scan(&older); err != nil {
		t.Fatalf("counting the calls older than the retention: %v", err)
	}

	return older
}

// countCallChildRows answers, for each table that holds rows belonging to a call, how many rows it holds in
// all and how many of them belong to a call the retention discards. Those two figures are what the cascades
// are held to afterwards: the rows that go are the ones that belonged to a deleted call, and the rows that
// stay are the ones that did not.
func countCallChildRows(t *testing.T, db *sql.DB, cutoff time.Time) map[string][2]int {
	t.Helper()

	counts := map[string][2]int{}

	for _, key := range prunedCallChildKeys() {
		total := countRows(t, db, key.table)

		query := `SELECT COUNT(*) FROM "` + key.table + `" WHERE "callId" IN (SELECT "callId" FROM "calls" WHERE "timestamp" < ?)`

		var older int

		if err := db.QueryRow(query, millisOf(cutoff)).Scan(&older); err != nil {
			t.Fatalf("counting the rows of %s that belong to a call older than the retention: %v", key.table, err)
		}

		counts[key.table] = [2]int{total, older}
	}

	return counts
}

// measureHistoryPrune takes the samples the prune's ceiling is judged against. Each sample discards the same
// rows inside a transaction that is then rolled back, so every sample deletes the history the measurement
// was written against rather than what the sample before it left. The rollback is outside the timing: it is
// the fixture being put back, not part of what a retention pass costs.
func measureHistoryPrune(t *testing.T, db *sql.DB, statement string, cutoff time.Time, wantRows int) []time.Duration {
	t.Helper()

	const rounds = 3
	const samplesPerRound = 5

	times := []time.Duration{}

	for range rounds {
		for range samplesPerRound {
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatalf("beginning the transaction that samples the retention prune: %v", err)
			}

			started := time.Now()

			res, err := tx.ExecContext(context.Background(), statement, millisOf(cutoff))

			elapsed := time.Since(started)

			if err != nil {
				tx.Rollback()
				t.Fatalf("sampling the retention prune: %v", err)
			}

			removed, err := res.RowsAffected()
			if err != nil {
				tx.Rollback()
				t.Fatalf("reading how many calls the sampled prune discarded: %v", err)
			}

			tx.Rollback()

			if int(removed) != wantRows {
				t.Fatalf("the sampled prune discards %d calls, want the %d the fixture holds older than the "+
					"retention: the sample did not delete the history the figure is read against", removed, wantRows)
			}

			times = append(times, elapsed)

			// A pass that costs more than twice its ceiling has missed it, and there is nothing to learn
			// from timing the rest of the samples: a retention pass over a history that lost the keys it
			// is meant to run through costs minutes, not milliseconds.
			if elapsed > 2*historyPruneCeiling {
				t.Errorf("a sampled retention pass costs %s, more than twice the %s it is held inside: the "+
					"samples stop here because the pass has already missed its budget", elapsed, historyPruneCeiling)

				return times
			}
		}
	}

	return times
}

// middleOf answers the middle of a list of samples, which is the figure a budget is judged against: the
// spread between two samples of one pass is the noise floor every figure here is read against.
func middleOf(times []time.Duration) time.Duration {
	sorted := slices.Clone(times)

	slices.Sort(sorted)

	return sorted[len(sorted)/2]
}
