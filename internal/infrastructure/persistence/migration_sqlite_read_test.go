package persistence

// The file backend is the one a deployment being upgraded is most likely sitting on, and it is the one
// where the pass cannot afford to read on one connection and write on another: sqlite locks the file
// rather than the table, and a write transaction that has filled its page cache with dirty pages has to
// flush them, which waits for every reader to leave the file. The only reader is the row loop waiting for
// that flush. A calls table big enough to fill the cache is therefore a table the pass either carries or
// stops on, and it was the 100,000-call timing (tools/migration/run.sh --big) that found the pass
// stopping on one: 2,000 calls of 1 KiB each, never finishing, with the calls insert in this package and
// `_pager_wait_on_lock` under it in the goroutine dump.
//
// This is the same shape at the size the ordinary suite can afford — five hundred calls of twenty
// kilobytes of audio each, ten megabytes written in one transaction — so that the shape stays proven
// without anyone having to generate a hundred megabytes to notice it broke.

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// largeAudioBytes is one call's worth of audio here. The number is chosen for the page cache, not for
// realism: twenty kilobytes across five hundred calls is more than sqlite keeps dirty in memory, which is
// the moment the pass used to stop at.
const largeAudioBytes = 20 * 1024

func TestMigrateCallsCarriesATableSqliteCannotReadAndWriteAtOnce(t *testing.T) {
	db := openLegacyV6Database(t)

	// The arranged calls resolve through the systems, talkgroups and units the fixture already holds. A
	// call naming something the database does not hold would be archived here rather than carried, and
	// what this test is about is carrying a hundred times more of it than the fixture holds.
	for n := range 500 {
		id := 720 + n

		if _, err := db.Exec(
			`INSERT INTO "rdioScannerCalls" ("id", "audio", "audioName", "audioType", "dateTime", "frequencies", "frequency", "patches", "source", "sources", "system", "talkgroup") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id,
			make([]byte, largeAudioBytes),
			fmt.Sprintf("%d.wav", id),
			"audio/wav",
			"2025-06-15 09:00:00.000 +0000 UTC",
			`[{"errorCount":0,"freq":1514000000,"pos":0,"spikeCount":0}]`,
			1514000000,
			"[]",
			12345,
			"[]",
			10001,
			1514000,
		); err != nil {
			t.Fatalf("arranging call %d: %v", id, err)
		}
	}

	// A pass that is waiting on itself never comes back, so the deadline is what makes this test fail
	// rather than freeze the suite. It is not a bar on speed: the pass carries this table in a fraction
	// of a second, and the deadline only ever speaks for the case where it stopped.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := Migrate(ctx, db, DialectSqlite, fixedClock{migrationInstant}, func(level, message string) {}); err != nil {
		t.Fatalf("Migrate over a calls table of %d KiB per row: %v", largeAudioBytes/1024, err)
	}

	carried := countArrangedCalls(t, db)

	if carried != 500 {
		t.Errorf("the pass carried %d of the 500 calls it was given, and stopped at %d", carried, largeAudioBytes/1024)
	}

	if hasV6Table(t, db, "rdioScannerCalls") {
		t.Errorf("the pass left rdioScannerCalls where it found it, so it did not read the whole table")
	}
}

// countArrangedCalls counts the calls this test put into the v6 table that came out in the v8 one. The
// fixture's own calls are not counted: they are the subject of every other test in this package.
func countArrangedCalls(t *testing.T, db *sql.DB) int {
	t.Helper()

	var carried int

	if err := db.QueryRow(
		`SELECT COUNT(*) FROM "calls" WHERE "callId" >= 720 AND "callId" < 1220`,
	).Scan(&carried); err != nil {
		t.Fatalf("counting the carried calls: %v", err)
	}

	return carried
}
