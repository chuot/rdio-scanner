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

	"rdio-scanner/server-ng/internal/application/ports"
	"rdio-scanner/server-ng/internal/domain"
)

// Options is the server's options, held as the configuration in effect.
//
// `ReadOptions` answers with what the server is running with, not with what the `options` table
// holds; `WriteOptions` replaces what the server runs with and then stores it; and
// `ReloadOptions` is the legacy server's `Options.Read` — the re-derivation the administrator
// password change runs, which resets the options to their defaults and applies the stored rows
// on top. See
// [decision-internal-ports.md](../../../../docs/specs/FEAT-003-server-ng-refactor/decision-internal-ports.md)
// §9 and §11.
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
	options.mutex.Lock()
	defer options.mutex.Unlock()

	if !options.loaded {
		next, err := options.rederive(ctx, domain.Options{})
		if err != nil {
			return domain.Options{}, err
		}

		options.live = next
		options.loaded = true
	}

	return options.live, nil
}

// WriteOptions makes the given options the ones the server runs with and then stores them.
//
// Nothing is read back afterwards, which is what the legacy server does with this one section:
// `ConfigHandler` does `FromMap` then `Write` and never `Read`, so what the administrator is
// answered after a save is what they submitted — including the duplicate-detection window the
// stored rows cannot give back (Q1).
func (options *Options) WriteOptions(ctx context.Context, next domain.Options) error {
	options.mutex.Lock()
	defer options.mutex.Unlock()

	options.live = next
	options.loaded = true

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
	options.mutex.Lock()
	defer options.mutex.Unlock()

	next, err := options.rederive(ctx, options.live)
	if err != nil {
		return domain.Options{}, err
	}

	options.live = next
	options.loaded = true

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

	return out, nil
}

// resetOptions reproduces the reset `Options.Read` performs before it applies the stored rows.
//
// The legacy reset covers every option it has a default for except three: `branding`, `email`,
// and `time12hFormat` are left alone, and so is the secret. A stored row replaces them in every
// database the legacy server wrote, so the difference only shows when a row is missing — and
// then the legacy server keeps the value it was already running with, which is what this does
// too.
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
	next.Time12hFormat = live.Time12hFormat
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

	case "dimmerDelay":
		if v, ok := row.value.(float64); ok {
			options.DimmerDelay = uint(v)
		}

	case "disableDuplicateDetection":
		if v, ok := row.value.(bool); ok {
			options.DisableDuplicateDetection = v
		}

	// Q1, reproduced rather than fixed: the window is stored under
	// `duplicateDetectionTimeFrame` (capital F) by `Options.Write` and read under
	// `duplicateDetectionTimeframe` (lowercase f) by `Options.Read`, so a re-derivation never
	// finds the stored window and restores the 500 ms default. Fixing the key would silently
	// start applying a setting that has been ignored for every existing deployment, and is left
	// to its own specification.
	case "duplicateDetectionTimeframe":
		if v, ok := row.value.(float64); ok {
			options.DuplicateDetectionTimeFrame = uint(v)
		}

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

	case "playbackGoesLive":
		if v, ok := row.value.(bool); ok {
			options.PlaybackGoesLive = v
		}

	case "pruneDays":
		if v, ok := row.value.(float64); ok {
			options.PruneDays = uint(v)
		}

	case "secret":
		// The stored secret is accepted only at its full length; anything else — including the
		// empty string a legacy database actually holds, and a value that is not a string at
		// all — gets a fresh random one. The regenerated secret is not written back, so the
		// secret in use is not the one on record until the next options save.
		if v, ok := row.value.(string); ok {
			if len(v) == domain.SecretLength {
				options.Secret = v
			} else {
				options.Secret = domain.GenerateSecret(domain.SecretLength)
			}

			break
		}

		options.Secret = domain.GenerateSecret(domain.SecretLength)

	case "showListenersCount":
		if v, ok := row.value.(bool); ok {
			options.ShowListenersCount = v
		}

	case "sortTalkgroups":
		if v, ok := row.value.(bool); ok {
			options.SortTalkgroups = v
		}

	case "time12hFormat":
		if v, ok := row.value.(bool); ok {
			options.Time12hFormat = v
		}
	}

	return options
}

// write stores the options the way the legacy `Options.Write` does: one transaction, and for
// each key an update that becomes an insert when no row carries that key. The keys and their
// order are the legacy ones.
//
// The legacy `set` helper reports nothing — a failed update is dropped and a failed insert is
// only printed — so the legacy save answers success for options that were never stored. The
// port contract is the opposite: a failed persist is returned, which is what the fakes' `SaveErr`
// knob models and what §9 requires.
func (options *Options) write(ctx context.Context, next domain.Options) error {
	tx, err := options.store.begin(ctx)
	if err != nil {
		return operationError("options", "write", err)
	}

	set := func(key string, value any) error {
		b, err := json.Marshal(value)
		if err != nil {
			return operationError("options", "write", err)
		}

		res, err := tx.exec(ctx, optionsUpdateQuery, string(b), key)
		if err != nil {
			return statementError("options", "write", err, optionsUpdateQuery)
		}

		stored, err := res.RowsAffected()
		if err != nil {
			return operationError("options", "write", err)
		}

		if stored > 0 {
			return nil
		}

		if _, err := tx.exec(ctx, optionsInsertQuery, key, string(b)); err != nil {
			return statementError("options", "write", err, optionsInsertQuery)
		}

		return nil
	}

	for _, entry := range optionRows(next) {
		if err := set(entry.key, entry.value); err != nil {
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

// optionRows is the key list the legacy `Options.Write` stores, in its order.
//
// `audioConversion` is absent from it, and that is a defect of the same family as Q1 rather than
// an omission here: `Options.Read` looks for a stored `audioConversion` row that `Options.Write`
// never writes, so the audio-conversion mode the administrator chose is not re-applied after a
// restart or a password change — every re-derivation answers the default mode. Reproduced as it
// is, and reported.
func optionRows(options domain.Options) []optionRow {
	return []optionRow{
		{"adminPassword", options.AdminPasswordHash},
		{"adminPasswordNeedChange", options.AdminPasswordNeedChange},
		{"autoPopulate", options.AutoPopulate},
		{"branding", options.Branding},
		{"dimmerDelay", options.DimmerDelay},
		{"disableDuplicateDetection", options.DisableDuplicateDetection},
		{"duplicateDetectionTimeFrame", options.DuplicateDetectionTimeFrame},
		{"email", options.Email},
		{"keypadBeeps", options.KeypadBeeps},
		{"maxClients", options.MaxClients},
		{"playbackGoesLive", options.PlaybackGoesLive},
		{"pruneDays", options.PruneDays},
		{"secret", options.Secret},
		{"showListenersCount", options.ShowListenersCount},
		{"sortTalkgroups", options.SortTalkgroups},
		{"time12hFormat", options.Time12hFormat},
	}
}
