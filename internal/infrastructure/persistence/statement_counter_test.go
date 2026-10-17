// Copyright (C) 2026 rdio-scanner contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
// AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE
// LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
// CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
// SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
// INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
// CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
package persistence

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	modernc "modernc.org/sqlite"
)

// statementCounter records every statement that runs on a counted connection, in
// the order it ran, so a test can say how many round trips a save costs.
type statementCounter struct {
	mutex   sync.Mutex
	queries []string

	// delay is the pause every statement takes on its way through, in
	// nanoseconds: a test arms it to watch a cache answer while a save is
	// still walking the database.
	delay atomic.Int64
}

// setDelay pauses every statement the counted connection runs by d. Zero —
// the resting state — runs them as fast as the database does.
func (c *statementCounter) setDelay(d time.Duration) {
	c.delay.Store(int64(d))
}

func (c *statementCounter) currentDelay() time.Duration {
	return time.Duration(c.delay.Load())
}

func (c *statementCounter) record(query string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.queries = append(c.queries, query)
}

// count answers how many recorded statements begin with the given text.
func (c *statementCounter) count(prefix string) int {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	found := 0

	for _, query := range c.queries {
		if strings.HasPrefix(query, prefix) {
			found++
		}
	}

	return found
}

// total answers how many statements have been recorded since the last reset: the whole cost of one
// piece of work, rather than the cost of one kind of statement inside it.
func (c *statementCounter) total() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	return len(c.queries)
}

// reset forgets what has been counted, so a test counts only the statements of the
// work it is measuring.
func (c *statementCounter) reset() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.queries = nil
}

// countedConnector opens the sqlite file through the very driver the server opens
// it with, and wraps each connection so every statement passes the counter on its
// way to the database.
type countedConnector struct {
	dsn     string
	counter *statementCounter
}

func (c countedConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := (&modernc.Driver{}).Open(c.dsn)
	if err != nil {
		return nil, err
	}

	return countedConn{conn: conn, counter: c.counter}, nil
}

func (c countedConnector) Driver() driver.Driver {
	return countedDriver{counter: c.counter}
}

type countedDriver struct{ counter *statementCounter }

func (d countedDriver) Open(name string) (driver.Conn, error) {
	conn, err := (&modernc.Driver{}).Open(name)
	if err != nil {
		return nil, err
	}

	return countedConn{conn: conn, counter: d.counter}, nil
}

// countedConn counts the statements and hands the work to the sqlite connection
// underneath, the same way the server's connection does it.
type countedConn struct {
	conn    driver.Conn
	counter *statementCounter
}

func (c countedConn) Prepare(query string) (driver.Stmt, error) {
	c.counter.record(query)

	return c.conn.Prepare(query)
}

func (c countedConn) Close() error { return c.conn.Close() }

func (c countedConn) Begin() (driver.Tx, error) { return c.conn.Begin() }

func (c countedConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if inner, ok := c.conn.(driver.ConnBeginTx); ok {
		return inner.BeginTx(ctx, opts)
	}

	return c.conn.Begin()
}

func (c countedConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	c.counter.record(query)

	if inner, ok := c.conn.(driver.ConnPrepareContext); ok {
		return inner.PrepareContext(ctx, query)
	}

	return c.conn.Prepare(query)
}

func (c countedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.counter.record(query)

	if d := c.counter.currentDelay(); d > 0 {
		time.Sleep(d)
	}

	if inner, ok := c.conn.(driver.ExecerContext); ok {
		return inner.ExecContext(ctx, query, args)
	}

	stmt, err := c.conn.Prepare(query)
	if err != nil {
		return nil, err
	}

	defer stmt.Close()

	return stmt.Exec(namedValues(args))
}

func (c countedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.counter.record(query)

	if d := c.counter.currentDelay(); d > 0 {
		time.Sleep(d)
	}

	if inner, ok := c.conn.(driver.QueryerContext); ok {
		return inner.QueryContext(ctx, query, args)
	}

	stmt, err := c.conn.Prepare(query)
	if err != nil {
		return nil, err
	}

	defer stmt.Close()

	return stmt.Query(namedValues(args))
}

// namedValues turns the driver's named arguments into the positional values the
// fallback prepared-statement path takes.
func namedValues(args []driver.NamedValue) []driver.Value {
	out := make([]driver.Value, 0, len(args))

	for _, arg := range args {
		out = append(out, arg.Value)
	}

	return out
}

// openCountedSchemaDatabase opens the schema-only fixture through the counting
// driver and hands back the database with its counter, the statements of the
// schema setup already forgotten.
func openCountedSchemaDatabase(t *testing.T) (*sql.DB, *statementCounter) {
	t.Helper()

	counter := &statementCounter{}

	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys=on&_pragma=busy_timeout%%3d10000",
		filepath.Join(t.TempDir(), "rdio-scanner.db"))

	db := sql.OpenDB(countedConnector{dsn: dsn, counter: counter})

	t.Cleanup(func() { db.Close() })

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	counter.reset()

	return db, counter
}
