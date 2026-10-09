// Command logs-measure is the Q4 measurement for FEAT-010: the same four log searches over the same
// 120,000-entry log, once with the two indexes missing and once with them created, with the observed
// durations and the query plans printed.
//
// It lives under testdata/ on purpose. Go's package matching ignores a directory named testdata, so
// `go build ./...`, `go vet ./...` and `go test ./...` never see this file. It is a harness that is run
// on purpose, and not a test that runs on its own and compares wall-clock times it cannot pin — a
// measurement belongs in validation.md, and a suite that fails when a machine is busy is a defect.
//
//	usage: go run ./testdata/logs-measure -rows 120000 -runs 7
//
// The searches run through the real read path — persistence.NewLogs and its SearchLogs, with the
// criteria built by domain.ParseLogSearch exactly as the administrator's endpoint builds them — so what
// is timed is the server's search and not this file's idea of it.
//
// The query plans are taken over the statements the search assembles. Before anything is measured, every
// fragment this file joins is checked against internal/infrastructure/persistence/logs.go: a plan over a
// statement the server does not actually run is not evidence, and the check is what makes that a fact
// rather than an intention.

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/infrastructure/persistence"
)

const (
	defaultRows = 120000
	defaultRuns = 7

	// The log view asks for a page of 200, newest first, which is what an operator is actually doing when
	// they open the log on a deployment with a large log.
	pageLimit   = 200
	pageOffset  = 0
	pageSort    = -1
	logsSource  = "internal/infrastructure/persistence/logs.go"
	measureText = "W1ABC"

	// A fragment that appears in what the directory watchers write, for the search that filters to a source
	// that selects little of the log.
	measurePath = "/data/incoming"
)

func main() {
	rows := flag.Int("rows", defaultRows, "how many log entries to write")
	runs := flag.Int("runs", defaultRuns, "how many times each search is repeated")
	path := flag.String("db", "/tmp/opencode/rdio-logs-measure/measure.db", "the sqlite file the measurement runs over")
	flag.Parse()

	if err := run(*path, *rows, *runs); err != nil {
		fmt.Fprintf(os.Stderr, "logs-measure: %v\n", err)
		os.Exit(1)
	}
}

func run(path string, rows, runs int) error {
	if err := checkFragments(); err != nil {
		return err
	}

	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}

	ctx := context.Background()

	db, err := persistence.Open(persistence.Settings{Dialect: persistence.DialectSqlite, FilePath: path})
	if err != nil {
		return err
	}

	defer db.Close()

	// The two states a deployment passes through: the schema as ApplySchema writes it, which carries the
	// source column but no index on it, and the same database after Migrate, which is where the two
	// indexes are created. Same rows, same file, only the indexes differ.
	if err := persistence.ApplySchema(ctx, db, persistence.DialectSqlite); err != nil {
		return err
	}

	fmt.Printf("\n# %s — %d entries\n", path, rows)

	if err := seed(ctx, db, rows); err != nil {
		return err
	}

	logs := persistence.NewLogs(db, persistence.DialectSqlite)

	searches := specs()

	if err := report(ctx, db, "without the indexes"); err != nil {
		return err
	}

	if err := measure(ctx, logs, searches, runs); err != nil {
		return err
	}

	if err := persistence.Migrate(ctx, db, persistence.DialectSqlite, nil, func(_, _ string) {}); err != nil {
		return err
	}

	if err := report(ctx, db, "with the indexes"); err != nil {
		return err
	}

	if err := measure(ctx, logs, searches, runs); err != nil {
		return err
	}

	// A third pass, because the second one showed one search getting slower rather than faster, and the
	// planner decides how selective it believes a filter is from statistics it has not been given. This is
	// a measurement and not a change to the server: nothing here writes ANALYZE into a migration.
	if _, err := db.ExecContext(ctx, `ANALYZE`); err != nil {
		return fmt.Errorf("analyzing: %w", err)
	}

	fmt.Printf("\n## with the indexes, after ANALYZE\n")

	return measure(ctx, logs, searches, runs)
}

// spec is one of the four searches the measurement repeats.
type spec struct {
	name     string
	criteria map[string]any
}

func measure(ctx context.Context, logs *persistence.Logs, searches []spec, runs int) error {
	fmt.Printf("\n## durations\n\n")
	fmt.Printf("| search | matched | median | fastest | slowest |\n")
	fmt.Printf("| --- | --- | --- | --- | --- |\n")

	for _, s := range searches {
		search := domain.ParseLogSearch(s.criteria)

		durations := make([]time.Duration, 0, runs)

		var matched uint64
		var entries int

		for i := 0; i < runs; i++ {
			started := time.Now()

			page, err := logs.SearchLogs(ctx, search)
			if err != nil {
				return fmt.Errorf("%s: %w", s.name, err)
			}

			durations = append(durations, time.Since(started))

			matched = page.Count
			entries = len(page.Results)
		}

		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })

		fmt.Printf("| %s | %d entries, %d on the page | %s | %s | %s |\n",
			s.name,
			matched,
			entries,
			durations[len(durations)/2],
			durations[0],
			durations[len(durations)-1],
		)
	}

	return nil
}

// report prints what the database has, and what the query planner would do for each statement the four
// searches run.
func report(ctx context.Context, db *sql.DB, label string) error {
	fmt.Printf("\n## %s\n\n", label)

	indexes, err := listIndexes(ctx, db)
	if err != nil {
		return err
	}

	fmt.Printf("indexes on logs: %s\n", strings.Join(indexes, ", "))

	for _, s := range specs() {
		search := domain.ParseLogSearch(s.criteria)

		fmt.Printf("\n### %s\n\n", s.name)

		for _, statement := range statementsFor(search) {
			details, err := plan(ctx, db, statement)
			if err != nil {
				return err
			}

			fmt.Printf("- `%s`\n", statement.sql)

			for _, detail := range details {
				fmt.Printf("  - %s\n", detail)
			}
		}
	}

	return nil
}

// specs are the four searches the measurement repeats, written the way the administrator's endpoint
// writes them: the criteria are the fields of a decoded JSON object, so every number in them is a
// float64, and the paging is the log view's — a page of 200, newest first.
func specs() []spec {
	page := map[string]any{"limit": float64(pageLimit), "offset": float64(pageOffset), "sort": float64(pageSort)}

	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}

		for k, v := range page {
			out[k] = v
		}

		for k, v := range extra {
			out[k] = v
		}

		return out
	}

	return []spec{
		{"no filter", page},
		{"level", with(map[string]any{"level": "warn"})},
		{"source", with(map[string]any{"source": "dirwatch"})},
		{"source and text", with(map[string]any{"source": "ingest", "text": measureText})},

		// A fifth search, beyond the four the criterion asks for, because the fourth one got slower with
		// the index and the reason has to be shown rather than argued: `ingest` writes a third of this log,
		// so filtering to it selects almost nothing. This one filters to a source that writes 8% of the log.
		{"a selective source and text", with(map[string]any{"source": "dirwatch", "text": measurePath})},
	}
}

// statement is one of the statements the search runs, with the values it binds. The values are needed
// because a query plan is taken over a prepared statement, and SQLite wants to know how many things are
// going to be put in the placeholders before it decides what it would do.
type statement struct {
	sql  string
	args []any
}

// statementsFor assembles the five statements SearchLogs runs for one set of criteria, in the order it
// runs them and with the criteria in the order it applies them: the two ends of the span, the total, the
// per-subsystem counts, and the page.
func statementsFor(search domain.LogSearch) []statement {
	where := []string{`TRUE`}
	args := []any{}

	if search.HasLevel() {
		where = append(where, `"level" = ?`)
		args = append(args, search.Level)
	}

	if search.HasText() {
		where = append(where, `lower("message") LIKE ? ESCAPE '!'`)
		args = append(args, "%"+strings.ToLower(search.Text)+"%")
	}

	countsWhere := append([]string{}, where...)
	countsArgs := append([]any{}, args...)

	if search.HasSource() {
		where = append(where, `"source" = ?`)
		args = append(args, string(search.Source))
	}

	filter := strings.Join(where, " AND ")
	countsFilter := strings.Join(countsWhere, " AND ")

	order := "ASC"
	if !search.Ascending {
		order = "DESC"
	}

	page := `SELECT "logId", "level", "source", "message", "timestamp" FROM "logs" WHERE ` + filter + ` ORDER BY "timestamp" ` + order + ` LIMIT ? OFFSET ?`

	return []statement{
		{`SELECT "timestamp" FROM "logs" WHERE ` + filter + ` ORDER BY "timestamp" ASC`, args},
		{`SELECT "timestamp" FROM "logs" WHERE ` + filter + ` ORDER BY "timestamp" DESC`, args},
		{`SELECT COUNT(*) FROM "logs" WHERE ` + filter, args},
		{`SELECT "source", COUNT(*) FROM "logs" WHERE ` + countsFilter + ` GROUP BY "source"`, countsArgs},
		{page, append(append([]any{}, args...), search.Limit, search.Offset)},
	}
}

// fragments are every piece of SQL this file joins into a statement. Each one has to be present,
// verbatim, in the file the real search is written in.
var fragments = []string{
	`where := []string{` + "`TRUE`" + `}`,
	`"level" = ?`,
	`lower("message") LIKE ? ESCAPE '!'`,
	`"source" = ?`,
	`SELECT "timestamp" FROM "logs" WHERE `,
	`SELECT COUNT(*) FROM "logs" WHERE `,
	`SELECT "source", COUNT(*) FROM "logs" WHERE `,
	` GROUP BY "source"`,
	`SELECT "logId", "level", "source", "message", "timestamp" FROM "logs" WHERE `,
	` ORDER BY "timestamp" `,
	` LIMIT ? OFFSET ?`,
}

func checkFragments() error {
	b, err := os.ReadFile(logsSource)
	if err != nil {
		return fmt.Errorf("reading %s: %w", logsSource, err)
	}

	source := string(b)

	for _, fragment := range fragments {
		if !strings.Contains(source, fragment) {
			return fmt.Errorf("%s no longer writes %s\nthe plans this harness would print would be plans over statements the server does not run", logsSource, fragment)
		}
	}

	return nil
}

func plan(ctx context.Context, db *sql.DB, statement statement) ([]string, error) {
	rows, err := db.QueryContext(ctx, `EXPLAIN QUERY PLAN `+statement.sql, statement.args...)
	if err != nil {
		return nil, fmt.Errorf("planning %s: %w", statement.sql, err)
	}

	defer rows.Close()

	out := []string{}

	for rows.Next() {
		var (
			seq    int
			id     int
			parent int
			detail string
		)

		if err := rows.Scan(&seq, &id, &parent, &detail); err != nil {
			return nil, err
		}

		out = append(out, detail)
	}

	return out, rows.Err()
}

func listIndexes(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT "name" FROM "sqlite_master" WHERE "type" = 'index' AND "tbl_name" = 'logs' ORDER BY "name"`)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	out := []string{}

	for rows.Next() {
		var name string

		if err := rows.Scan(&name); err != nil {
			return nil, err
		}

		out = append(out, name)
	}

	return out, rows.Err()
}

func dirOf(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}

	return "."
}
