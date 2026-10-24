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
	"sync"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Options is the server's options, held as the configuration in effect: `ReadOptions`
// answers with what the server runs with, not what the `options` table holds, and
// `WriteOptions` replaces what it runs with and then stores it, per the live configuration
// in effect maintainer note.
type Options struct {
	store store

	mutex  sync.Mutex
	loaded bool
	live   domain.Options

	// initialCredential is the credential the server accepts when the stored configuration has
	// none, supplied by the composition root because making it is bcrypt's job.
	initialCredential string
}

var _ ports.OptionsRepository = (*Options)(nil)

const (
	optionsSelectQuery = `SELECT "key", "value" FROM "options"`
	optionsUpdateQuery = `UPDATE "options" SET "value" = ? WHERE "key" = ?`
	optionsInsertQuery = `INSERT INTO "options" ("key", "value") VALUES (?, ?)`
)

// optionRow is one stored option, its value already decoded out of the JSON the table
// carries: option values are stored JSON-marshaled, so they are read as JSON values and
// not as raw text.
type optionRow struct {
	key   string
	value any
}

// NewOptions returns the options repository for one database.
func NewOptions(db *sql.DB, dialect Dialect) *Options {
	return &Options{store: newStore(db, dialect)}
}

// StoreInitialCredential records the credential the server accepts when its stored
// configuration has none, and keeps it in memory only: writing it would persist the signing
// secret with it, per the live configuration in effect maintainer note. The credential is
// supplied rather than computed because bcrypt belongs to the password adapter.
func (options *Options) StoreInitialCredential(credential string) {
	options.mutex.Lock()
	defer options.mutex.Unlock()

	options.initialCredential = credential
}

// ReadOptions returns the options the server is currently running with. The first call is the
// startup read: the defaults with the stored rows applied on top.
func (options *Options) ReadOptions(ctx context.Context) (domain.Options, error) {
	// The cache answers under the mutex, and the fetch that fills an empty cache
	// runs outside it: holding the mutex across stored-row statements stops the
	// server's own configuration reads for the length of every statement.
	options.mutex.Lock()

	if options.loaded {
		next := options.live

		options.mutex.Unlock()

		return next, nil
	}

	options.mutex.Unlock()

	next, err := options.rederive(ctx, domain.Options{})
	if err != nil {
		return domain.Options{}, err
	}

	options.mutex.Lock()
	options.live = next
	options.loaded = true
	options.mutex.Unlock()

	return next, nil
}

// WriteOptions makes the given options the ones the server runs with and then stores them,
// and the save answers with what was submitted: this section is never read back, per the
// live configuration in effect maintainer note.
func (options *Options) WriteOptions(ctx context.Context, next domain.Options) error {
	// The submitted options take effect under the mutex; the rows are written
	// outside it, as in `Accesses.SaveAccesses`.
	options.mutex.Lock()
	options.live = next
	options.loaded = true
	options.mutex.Unlock()

	if err := options.write(ctx, next); err != nil {
		return err
	}

	return nil
}

// ReloadOptions re-derives the options from what is stored and makes that what the server
// runs with: the re-derivation resets to the defaults before applying the stored rows, so
// an option stored under a name the reader does not look for comes back as the default.
func (options *Options) ReloadOptions(ctx context.Context) (domain.Options, error) {
	// The re-derivation reads the stored rows outside the mutex;
	// the options it produces take effect under it.
	options.mutex.Lock()
	from := options.live
	options.mutex.Unlock()

	next, err := options.rederive(ctx, from)
	if err != nil {
		return domain.Options{}, err
	}

	options.mutex.Lock()
	options.live = next
	options.loaded = true
	options.mutex.Unlock()

	return next, nil
}

// rederive reproduces the legacy `Options.Read`: reset, then apply the stored rows in the
// order the table returns them.
func (options *Options) rederive(ctx context.Context, from domain.Options) (domain.Options, error) {
	rows, err := options.fetch(ctx)
	if err != nil {
		return domain.Options{}, err
	}

	next := resetOptions(from)

	next.AdminPasswordHash = options.initialCredential

	for _, row := range rows {
		next = applyOptionRow(next, row)
	}

	return next, nil
}

// fetch reads the stored options: a row that cannot be scanned, or whose value is not JSON,
// is skipped rather than failed, as the legacy `Read` skips it. Only the query itself can
// fail the read.
func (options *Options) fetch(ctx context.Context) ([]optionRow, error) {
	rows, err := options.store.query(ctx, optionsSelectQuery)
	if err != nil {
		return nil, statementError("options", "read", err, optionsSelectQuery)
	}

	defer rows.Close()

	out := []optionRow{}

	for rows.Next() {
		var key, value sql.NullString

		if err := rows.Scan(&key, &value); err != nil {
			// A damaged row is skipped as the legacy skips it, but a cancelled
			// context is not: re-deriving over the rows that happened to survive
			// is options nobody stored.
			if readErr := rows.Err(); readErr != nil {
				return nil, statementError("options", "read", readErr, optionsSelectQuery)
			}

			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, statementError("options", "read", ctxErr, optionsSelectQuery)
			}

			continue
		}

		if !key.Valid || !value.Valid {
			continue
		}

		var decoded any

		if err := json.Unmarshal([]byte(value.String), &decoded); err != nil {
			continue
		}

		out = append(out, optionRow{key: key.String, value: decoded})
	}

	// A read that ended early is not a table with fewer rows: the re-derivation
	// would apply defaults to every key the read did not reach, so the read's own
	// failure is the caller's to hear.
	if err := rows.Err(); err != nil {
		return nil, statementError("options", "read", err, optionsSelectQuery)
	}

	return out, nil
}

// resetOptions reproduces the reset the legacy `Options.Read` performs before it applies
// the stored rows: every option it has a default for is reset, except `branding`, `email`,
// and the secret, which keep what the server was already running with. The credential stays
// empty here — bcrypt belongs to the password adapter — and the re-derivation puts in what
// StoreInitialCredential supplied, per the live configuration in effect maintainer note.
func resetOptions(live domain.Options) domain.Options {
	next := domain.DefaultOptions()

	next.Branding = live.Branding
	next.Email = live.Email
	next.Secret = live.Secret

	return next
}

// applyOptionRow applies one stored row to the options, with the legacy server's read keys and
// the legacy server's type rules: a row whose value is not the type the option needs is
// ignored, and the option keeps whatever it had before.
func applyOptionRow(options domain.Options, row optionRow) domain.Options {
	switch row.key {
	case "adminPassword":
		if v, ok := row.value.(string); ok {
			options.AdminPasswordHash = v
		}

	case "adminPasswordNeedChange":
		if v, ok := row.value.(bool); ok {
			options.AdminPasswordNeedChange = v
		}

	case "audioConversion":
		if v, ok := row.value.(float64); ok {
			options.AudioConversion = uint(v)
		}

	case "autoPopulate":
		if v, ok := row.value.(bool); ok {
			options.AutoPopulate = v
		}

	case "branding":
		if v, ok := row.value.(string); ok {
			options.Branding = v
		}

	case "disableDuplicateDetection":
		if v, ok := row.value.(bool); ok {
			options.DisableDuplicateDetection = v
		}

	// Retired options: `dimmerDelay`, `duplicateDetectionTimeframe`,
	// `playbackGoesLive` and `time12hFormat` rows in a migrated legacy database are
	// read and dropped — the scanner webapp owns the dimmer delay and the browser
	// locale the time format, the duplicate window is the domain constant, and
	// playback always goes live.

	case "email":
		if v, ok := row.value.(string); ok {
			options.Email = v
		}

	case "keypadBeeps":
		if v, ok := row.value.(string); ok {
			options.KeypadBeeps = v
		}

	case "maxClients":
		if v, ok := row.value.(float64); ok {
			options.MaxClients = uint(v)
		}

	case "pruneDays":
		if v, ok := row.value.(float64); ok {
			options.PruneDays = uint(v)
		}

	case "secret":
		// A stored secret is taken only at the length the server signs with, and a
		// value that fails that rule is not taken at all: the reader no longer
		// invents a secret, per the live configuration in effect maintainer note.
		if v, ok := row.value.(string); ok {
			options = options.AcceptSecret(v)
		}

	case "showListenersCount":
		if v, ok := row.value.(bool); ok {
			options.ShowListenersCount = v
		}

	case "sortTalkgroups":
		if v, ok := row.value.(bool); ok {
			options.SortTalkgroups = v
		}
	}

	return options
}

// write stores the options the way the legacy `Options.Write` does: one transaction, and for
// each key an update that becomes an insert when no row carries that key, in the legacy key
// order. `setOptionRow` is where the legacy helper's silence stops being the contract's.
func (options *Options) write(ctx context.Context, next domain.Options) error {
	tx, err := options.store.begin(ctx)
	if err != nil {
		return operationError("options", "write", err)
	}

	for _, entry := range optionRows(next) {
		if err := setOptionRow(ctx, tx, "write", entry.key, entry.value); err != nil {
			tx.rollback()
			return err
		}
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError("options", "write", err)
	}

	return nil
}

// StoreSecret puts the signing secret the server issues its administrator sessions with on
// record, writing the one option row and nothing else: saving the whole configuration as a
// side effect of starting would persist values nobody asked to save.
func (options *Options) StoreSecret(ctx context.Context, secret string) error {
	const section, operation = "options", "storeSecret"

	tx, err := options.store.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	if err := setOptionRow(ctx, tx, operation, "secret", secret); err != nil {
		tx.rollback()
		return err
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	// The adapter reports the options the server runs with, so the secret just put
	// on record is the secret it reports from here on.
	options.mutex.Lock()
	defer options.mutex.Unlock()

	options.live.Secret = secret

	return nil
}

// setOptionRow updates one option row and inserts it when no row carries that key, the shape
// the legacy `set` helper had, but unlike it a failed persist is returned: the legacy save
// answered success for options it never stored, and the port contract is the opposite.
func setOptionRow(ctx context.Context, tx transaction, operation, key string, value any) error {
	const section = "options"

	b, err := json.Marshal(value)
	if err != nil {
		return operationError(section, operation, err)
	}

	res, err := tx.exec(ctx, optionsUpdateQuery, string(b), key)
	if err != nil {
		return statementError(section, operation, err, optionsUpdateQuery)
	}

	stored, err := res.RowsAffected()
	if err != nil {
		return operationError(section, operation, err)
	}

	if stored > 0 {
		return nil
	}

	if _, err := tx.exec(ctx, optionsInsertQuery, key, string(b)); err != nil {
		return statementError(section, operation, err, optionsInsertQuery)
	}

	return nil
}

// optionRows is the key list the legacy `Options.Write` stores, in its order, plus
// `audioConversion`, which the legacy reader looked for but the legacy writer never stored —
// its mode was never on record. Repaired rather than preserved by the owner's decision.
func optionRows(options domain.Options) []optionRow {
	return []optionRow{
		{"adminPassword", options.AdminPasswordHash},
		{"adminPasswordNeedChange", options.AdminPasswordNeedChange},
		{"audioConversion", options.AudioConversion},
		{"autoPopulate", options.AutoPopulate},
		{"branding", options.Branding},
		{"disableDuplicateDetection", options.DisableDuplicateDetection},
		{"email", options.Email},
		{"keypadBeeps", options.KeypadBeeps},
		{"maxClients", options.MaxClients},
		{"pruneDays", options.PruneDays},
		{"secret", options.Secret},
		{"showListenersCount", options.ShowListenersCount},
		{"sortTalkgroups", options.SortTalkgroups},
	}
}
