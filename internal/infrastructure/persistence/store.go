// Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
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

package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// store is the database a repository adapter works on, together with the dialect it was opened
// with. Every adapter in this package is built on one, so that the dialect is decided once — when
// the server starts — and never inferred again inside a query.
type store struct {
	db      *sql.DB
	dialect Dialect

	// clock is the server's clock, set by the migration pass for the rows it archives. Every other
	// adapter leaves it nil: they store the instants their callers hand them, and only the migration
	// stamps anything itself.
	clock ports.Clock
}

func newStore(db *sql.DB, dialect Dialect) store {
	return store{db: db, dialect: dialect}
}

// query runs a statement that returns rows.
func (s store) query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.bind(query), args...)
	if err != nil {
		return nil, err
	}

	return rows, nil
}

func (s store) queryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return s.db.QueryRowContext(ctx, s.dialect.bind(query), args...)
}

func (s store) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, s.dialect.bind(query), args...)
}

// begin starts the single transaction a reconciling save runs in. Every legacy
// `Write` is one transaction, and a save that left half of a collection stored
// is one the next save would read back as the configuration.
//
// It is begun with the write posture the sqlite connection string carries — the
// write lock is held before the save reads, which is what a save needs. A read
// belongs here as little as a save belongs in beginRead.
func (s store) begin(ctx context.Context) (transaction, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return transaction{}, err
	}

	return transaction{tx: tx, store: s}, nil
}

// beginRead starts the transaction a read runs in — one that reads several
// tables, needs what it read to agree with itself, and writes nothing.
//
// It asks the driver for a read-only transaction, and the driver begins one
// asked for that way without the connection string's write posture. So this
// is for reads only: a save that used it would be a save allowed to read
// before it held the write lock.
func (s store) beginRead(ctx context.Context) (transaction, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return transaction{}, err
	}

	return transaction{tx: tx, store: s}, nil
}

// transaction is a database transaction whose statements are written for the store's dialect.
type transaction struct {
	tx    *sql.Tx
	store store
}

func (t transaction) query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, t.store.dialect.bind(query), args...)
}

func (t transaction) queryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, t.store.dialect.bind(query), args...)
}

func (t transaction) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, t.store.dialect.bind(query), args...)
}

func (t transaction) commit() error {
	return t.tx.Commit()
}

func (t transaction) rollback() {
	t.tx.Rollback()
}

// bind writes one query for the dialect it is about to run on: every query in
// this package is written once with `?` placeholders, and only PostgreSQL needs
// them numbered, so an adapter never carries two copies of a statement; why that
// replaces the legacy server's per-backend `fmt.Sprintf`
//
// It is a scan rather than a parser, and it relies on one invariant pinned by a
// test: SQL writes a quote inside a literal as a doubled quote, so a doubled
// pair ends one quoted span and starts the next — a literal like `it”s?x` is
// copied whole, and the numbering counts only the placeholders outside literals.
func (d Dialect) bind(query string) string {
	if d != DialectPostgresql {
		return query
	}

	out := strings.Builder{}

	parameter := 0

	for i := 0; i < len(query); i++ {
		switch quote := query[i]; quote {
		case '\'', '"':
			out.WriteByte(quote)

			for i++; i < len(query); i++ {
				out.WriteByte(query[i])

				if query[i] == quote {
					break
				}
			}

		case '?':
			parameter++

			out.WriteByte('$')
			out.WriteString(strconv.Itoa(parameter))

		default:
			out.WriteByte(query[i])
		}
	}

	return out.String()
}

// statementError reports a statement that failed, in the shape the legacy
// server's log lines always had: `section.operation: cause in <statement>`.
// The statement named is the parameterized one, not the legacy string with
// its values already inside it.
func statementError(section, operation string, err error, query string) error {
	return fmt.Errorf("%s.%s: %s in %s", section, operation, err, query)
}

// operationError reports a failure that is not one statement's: a transaction that could not
// begin, a result set that could not be read, a value that could not be converted.
func operationError(section, operation string, err error) error {
	return fmt.Errorf("%s.%s: %s", section, operation, err)
}

// placeholders is the right number of `?` for a list of values bound as
// parameters; the legacy server put the same list's values inside the
// statement by turning a JSON array's brackets into parentheses.
func placeholders(count int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", count), ", ")
}

// instantOf converts the integer a legacy table stores to the instant it means. Every timestamp in
// this schema is milliseconds since the Unix epoch, in the server's local zone.
func instantOf(millis int64) time.Time {
	return time.UnixMilli(millis)
}

func millisOf(instant time.Time) int64 {
	return instant.UnixMilli()
}

// durationOf converts the integer the `audioLength` column stores to the length it means.
// The column counts nanoseconds, the same count the server carries in a `time.Duration`,
// and the zero it defaults to is the server's own "never measured".
func durationOf(nanoseconds int64) time.Duration {
	return time.Duration(nanoseconds)
}

// nanosecondsOf is the count the `audioLength` column stores for a length: nanoseconds,
// clamped at zero because a negative length is no length, and the column's default is the
// same zero the server means by "never measured".
func nanosecondsOf(length time.Duration) int64 {
	if length < 0 {
		return 0
	}

	return int64(length)
}

// scopeText is the text a `systems` column carries: the scope as JSON, which is what the legacy
// server stores — it marshals the scope it decoded out of the configuration payload and writes the
// result. A scope that was never supplied is stored as `null`, and an empty one as `[]`.
func scopeText(scope domain.AccessScope) string {
	b, err := json.Marshal(scope.Value())
	if err != nil {
		return "null"
	}

	return string(b)
}

// parseScope reads a `systems` column back. An empty column and a column that is not JSON both
// mean the same thing they mean in the legacy server: no scope was supplied, which is the state
// the PIN gate treats as an unauthenticated client.
func parseScope(text string) domain.AccessScope {
	if len(text) == 0 {
		return domain.AccessScope{}
	}

	var value any

	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return domain.AccessScope{}
	}

	scope, _ := domain.ParseAccessScope(value)

	return scope
}

// storedIdentities asks the table, in one statement, which of the submitted
// identities it already holds: one IN list where the legacy server ran one
// COUNT per submitted row. An id of zero is a row the table will assign, so
// it is not asked about, and a list with none costs no statement at all.
func storedIdentities(ctx context.Context, tx transaction, section, operation, table, idColumn string, ids []int64) (map[uint64]bool, error) {
	stored := map[uint64]bool{}

	if len(ids) == 0 {
		return stored, nil
	}

	marks := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))

	for _, id := range ids {
		marks = append(marks, "?")
		args = append(args, id)
	}

	query := `SELECT "` + idColumn + `" FROM "` + table + `" WHERE "` + idColumn + `" IN (` + strings.Join(marks, ", ") + `)`

	rows, err := tx.query(ctx, query, args...)
	if err != nil {
		return nil, statementError(section, operation, err, query)
	}

	for rows.Next() {
		var id int64

		if err := rows.Scan(&id); err != nil {
			rows.Close()

			return nil, operationError(section, operation, err)
		}

		stored[uint64(id)] = true
	}

	rows.Close()

	if err := rows.Err(); err != nil {
		return nil, operationError(section, operation, err)
	}

	return stored, nil
}
