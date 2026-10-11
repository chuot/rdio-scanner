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
//
// WebSocket API Access Policy:
// This WebSocket API is reserved exclusively for Saubeo Solutions and its native applications.
// Unauthorized access is strictly prohibited.
// See API_ACCESS_POLICY.md for full terms.

package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Options is the server's options, held as the configuration in effect.
//
// `ReadOptions` answers with what the server is running with, not with what the `options` table
// holds; `WriteOptions` replaces what the server runs with and then stores it; and
// `ReloadOptions` is the legacy server's `Options.Read` — the re-derivation the administrator
// password change runs, which resets the options to their defaults and applies the stored rows
// on top.
type Options struct {
	store store

	mutex  sync.Mutex
	loaded bool
	live   domain.Options

	// initialCredential is the credential the server accepts when the stored configuration has none,
	// supplied by the composition root because making it is bcrypt's job and not this package's. See
	// StoreInitialCredential.
	initialCredential string
}

var _ ports.OptionsRepository = (*Options)(nil)

const (
	optionsSelectQuery = `SELECT "key", "value" FROM "options"`
	optionsUpdateQuery = `UPDATE "options" SET "value" = ? WHERE "key" = ?`
	optionsInsertQuery = `INSERT INTO "options" ("key", "value") VALUES (?, ?)`
)

// optionRow is one stored option, with its value already decoded out of the JSON the table
// carries. Option values are stored JSON-marshaled — `branding` is `"Fixture Dispatch"` with its
// quotes and `adminPassword` is a quoted bcrypt hash — so they are read as JSON values and not
// as raw text.
type optionRow struct {
	key   string
	value any
}

// NewOptions returns the options repository for one database.
func NewOptions(db *sql.DB, dialect Dialect) *Options {
	return &Options{store: newStore(db, dialect)}
}

// StoreInitialCredential records the credential the server accepts when its stored configuration has
// none, and keeps it in memory only.
//
// The legacy server generates a fresh hash of the default administrator password at every startup and
// puts it in place before the stored rows are applied. It never writes that hash: a deployment that has
// not changed the password has no adminPassword row at all, and the signing secret it generates at the
// same time is regenerated at the next startup as well, so a session started before a restart does not
// survive it. Writing the initial credential to make this easier would write the secret with it, and the
// server would start keeping sessions across restarts the legacy server does not keep.
//
// The credential is supplied rather than computed here because bcrypt belongs to the password adapter:
// this package can decide when a server needs a credential and hold the one it is given, but it cannot
// make one.
func (options *Options) StoreInitialCredential(credential string) {
	options.mutex.Lock()
	defer options.mutex.Unlock()

	options.initialCredential = credential
}

// ReadOptions returns the options the server is currently running with. The first call is the
// startup read: the defaults with the stored rows applied on top.
func (options *Options) ReadOptions(ctx context.Context) (domain.Options, error) {
	// The cache answers under the mutex, and the read that fills an empty cache
	// runs outside it (CA-014.4): the login handler, the retention pass, and the
	// signing secret all read through here, and holding the mutex across the
	// fetch of the stored rows stops the server's own configuration reads for
	// the length of every statement.
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

// WriteOptions makes the given options the ones the server runs with and then stores them.
//
// Nothing is read back afterwards, which is what the legacy server does with this one section:
// `ConfigHandler` does `FromMap` then `Write` and never `Read`, so what the administrator is
// answered after a save is what they submitted — including the duplicate-detection window the
// stored rows cannot give back (Q1).
func (options *Options) WriteOptions(ctx context.Context, next domain.Options) error {
	// The submitted options take effect under the mutex; the rows are written
	// outside it (CA-014.4), as in `Accesses.SaveAccesses`.
	options.mutex.Lock()
	options.live = next
	options.loaded = true
	options.mutex.Unlock()

	if err := options.write(ctx, next); err != nil {
		return err
	}

	return nil
}

// ReloadOptions re-derives the options from what is stored and makes that what the server runs
// with. It is the legacy `Options.Read`, and it is not a no-op: the re-derivation resets the
// options to their defaults before it applies the stored rows, so an option stored under a name
// the reader does not look for comes back as the default. Q15 is about exactly this.
func (options *Options) ReloadOptions(ctx context.Context) (domain.Options, error) {
	// The re-derivation reads the stored rows outside the mutex (CA-014.4);
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

// rederive is the legacy `Options.Read`: reset, then apply the stored rows in the order the
// table returns them.
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

// fetch reads the stored options.
//
// A row that cannot be scanned, or whose value is not JSON, is skipped rather than failed, as
// the legacy `Read` skips it: its loop `continue`s on a scan error and its unmarshal failures
// are discarded inside each `case`, and the function returns nil whatever happened. Only the
// query itself can fail the read.
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
			// A row whose scan fails is skipped as the legacy skips a damaged row —
			// unless the read itself failed: a cancelled context is felt at the scan,
			// and a re-derivation over the rows that happened to survive is options
			// nobody stored (CA-011.1).
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

	// A read that ended early is not a table with fewer rows (CA-011.1): the
	// re-derivation applies defaults to every key the read did not reach, and the
	// server ends up running with options nobody stored. The scan skips a damaged row
	// the way the legacy does; the read's own failure is the caller's to hear.
	if err := rows.Err(); err != nil {
		return nil, statementError("options", "read", err, optionsSelectQuery)
	}

	return out, nil
}

// resetOptions reproduces the reset `Options.Read` performs before it applies the stored rows.
//
// The legacy reset covers every option it has a default for except two: `branding` and `email`
// are left alone, and so is the secret. A stored row replaces them in every database the legacy
// server wrote, so the difference only shows when a row is missing — and then the legacy server
// keeps the value it was already running with, which is what this does too. (`time12hFormat` was
// in that leave-alone list too; the option is retired, so it is gone from the reset as well.)
//
// One value is deliberately not produced here: the legacy reset puts a freshly generated bcrypt
// hash of the default administrator password in place before the stored rows are applied, so a
// server whose options table has no `adminPassword` row still accepts the default password. bcrypt
// belongs to the password adapter (T060) and not to this package, so the reset leaves the
// credential empty and the re-derivation puts in whatever the composition root supplied through
// StoreInitialCredential.
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
	// read and dropped. The dimmer delay lives in the scanner webapp, the duplicate
	// window is the domain constant, playback always goes live, and the browser locale
	// decides the time format.

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
		// A stored secret is taken only at the length the server signs with, and a value that fails
		// that rule is not taken at all. The legacy reader generated a fresh secret for anything
		// unusable and never wrote it down, so a deployment that had never saved its configuration
		// signed with an empty key and a legacy database holding an unusable secret got a different
		// key on every start. Making a secret and putting it on record is now the startup use case's
		// job.
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
// each key an update that becomes an insert when no row carries that key. The keys and their
// order are the legacy ones, and `setOptionRow` is where the legacy helper's silence stops being
// the contract's silence.
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

// StoreSecret puts the signing secret the server issues its administrator sessions with on record.
//
// It writes the one option row and nothing else: saving the whole configuration as a side effect of
// starting the server would persist values nobody asked to save. The legacy server had no such write,
// which is why the secret it generated for an unusable stored one existed only until the process ended.
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

	// The options this adapter reports are the ones the server runs with, so the secret it just put
	// on record is the secret it reports from here on. Without this, a server that stored a secret at
	// startup would keep signing with the empty one it had already read.
	options.mutex.Lock()
	defer options.mutex.Unlock()

	options.live.Secret = secret

	return nil
}

// setOptionRow updates one option row and inserts it when no row carries that key, which is the shape
// the legacy `set` helper had.
//
// The legacy helper reported nothing: a failed update was dropped and a failed insert was only printed,
// so the legacy save answered success for options it never stored. The port contract is the opposite —
// a failed persist is returned — which is what the fakes' `SaveErr` knob models and §9 requires.
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

// optionRows is the key list the legacy `Options.Write` stores, in its order, plus the one key its
// reader was looking for.
//
// The legacy list held sixteen keys and `Options.Read` looked for a stored `audioConversion` row among
// them, so the audio-conversion mode the administrator chose was never on record: every re-derivation —
// a restart, or the re-derivation a password change runs — answered the default mode. It is stored here
// as the JSON number the reader already reads it as, in the alphabetical slot the legacy list's own
// ordering gives it. Repaired rather than preserved by the owner's decision.
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
