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
	"strconv"
	"strings"
	"testing"
	"time"

	"rdio-scanner/server-ng/internal/domain"
)

func TestReadOptionsReportsTheOptionsTheLegacyServerStored(t *testing.T) {
	options, err := NewOptions(openLegacyDatabase(t), DialectSqlite).ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if options.Branding != "Fixture Dispatch" {
		t.Errorf("Branding = %q, want the stored branding without the JSON quotes", options.Branding)
	}

	if options.Email != "dispatch@example.com" {
		t.Errorf("Email = %q, want the stored email", options.Email)
	}

	if options.KeypadBeeps != "whistler" {
		t.Errorf("KeypadBeeps = %q, want the stored whistler, not the uniden default", options.KeypadBeeps)
	}

	if options.DimmerDelay != 5000 || options.MaxClients != 200 || options.PruneDays != 7 {
		t.Errorf("DimmerDelay/MaxClients/PruneDays = %d/%d/%d, want the stored 5000/200/7", options.DimmerDelay, options.MaxClients, options.PruneDays)
	}

	if !options.AutoPopulate || !options.ShowListenersCount || !options.Time12hFormat {
		t.Errorf("AutoPopulate/ShowListenersCount/Time12hFormat = %v/%v/%v, want the stored true for each", options.AutoPopulate, options.ShowListenersCount, options.Time12hFormat)
	}

	if options.SortTalkgroups || options.PlaybackGoesLive || options.DisableDuplicateDetection {
		t.Errorf("SortTalkgroups/PlaybackGoesLive/DisableDuplicateDetection = %v/%v/%v, want the stored false for each", options.SortTalkgroups, options.PlaybackGoesLive, options.DisableDuplicateDetection)
	}

	if !options.AdminPasswordNeedChange {
		t.Error("AdminPasswordNeedChange = false, want the stored true")
	}

	if !strings.HasPrefix(options.AdminPasswordHash, "$2a$10$") {
		t.Errorf("AdminPasswordHash = %q, want the stored bcrypt hash without the JSON quotes", options.AdminPasswordHash)
	}
}

// TestReadOptionsReadsTheStoredValuesAsJSONValues pins the fixture fact the options repository
// exists to get right: every value in the `options` table is the JSON encoding of the option, so
// `branding` is stored with its quotes and must be read out of them.
func TestReadOptionsReadsTheStoredValuesAsJSONValues(t *testing.T) {
	db := openLegacyDatabase(t)

	if stored := readOptionValue(t, db, "branding"); stored != `"Fixture Dispatch"` {
		t.Fatalf("the fixture stores branding as %s, want the quoted JSON text the legacy server wrote", stored)
	}

	if stored := readOptionValue(t, db, "maxClients"); stored != "200" {
		t.Fatalf("the fixture stores maxClients as %s, want the bare JSON number the legacy server wrote", stored)
	}

	options, err := NewOptions(db, DialectSqlite).ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if options.Branding != "Fixture Dispatch" {
		t.Errorf("Branding = %q, want the decoded string", options.Branding)
	}

	if options.MaxClients != 200 {
		t.Errorf("MaxClients = %d, want the decoded number", options.MaxClients)
	}
}

func TestReadOptionsAnswersFromTheLoadedValueRatherThanFromTheDatabase(t *testing.T) {
	db := openLegacyDatabase(t)
	options := NewOptions(db, DialectSqlite)

	before, err := options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if _, err := db.Exec(`UPDATE "options" SET "value" = '"Someone Else"' WHERE "key" = 'branding'`); err != nil {
		t.Fatalf("UPDATE: %v", err)
	}

	after, err := options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if after.Branding != before.Branding {
		t.Errorf("Branding = %q after the stored row changed, want %q: ReadOptions reports what the server is running with", after.Branding, before.Branding)
	}
}

// TestReloadOptionsRestoresTheDefaultDuplicateDetectionWindow is Q1 proven against a database the
// legacy server wrote.
//
// server/options.go `Write` stores the window under `duplicateDetectionTimeFrame` (capital F) and
// `Read` looks for `duplicateDetectionTimeframe` (lowercase f), so the stored window is never
// among the rows the re-derivation applies: the fixture stores 750 and the server runs on 500 ms.
// Reproduced, not fixed — fixing the key would silently start applying a setting every existing
// deployment has been ignoring.
func TestReloadOptionsRestoresTheDefaultDuplicateDetectionWindow(t *testing.T) {
	db := openLegacyDatabase(t)

	if stored := readOptionValue(t, db, "duplicateDetectionTimeFrame"); stored != "750" {
		t.Fatalf("the fixture stores the window as %s under the capital-F key, want 750", stored)
	}

	reloaded, err := NewOptions(db, DialectSqlite).ReloadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReloadOptions: %v", err)
	}

	if reloaded.DuplicateDetectionTimeFrame != 500 {
		t.Errorf("the re-derived window = %d ms, want the 500 ms default: the stored 750 is stored under a name the reader does not look for", reloaded.DuplicateDetectionTimeFrame)
	}

	if reloaded.DuplicateDetectionWindow() != 500*time.Millisecond {
		t.Errorf("DuplicateDetectionWindow = %v, want 500ms", reloaded.DuplicateDetectionWindow())
	}

	if stored := readOptionValue(t, db, "duplicateDetectionTimeFrame"); stored != "750" {
		t.Errorf("the re-derivation rewrote the stored window as %s, want it left at 750", stored)
	}
}

// TestReloadOptionsGeneratesAFreshSecretAndLeavesTheStoredOneAlone pins the fixture's secret.
//
// The legacy `Read` accepts a stored secret only at its full length and generates a fresh random
// one otherwise — and a legacy database stores the empty string. The regenerated secret is never
// written back, so the secret that signs administrator tokens is not the one on record.
func TestReloadOptionsGeneratesAFreshSecretAndLeavesTheStoredOneAlone(t *testing.T) {
	db := openLegacyDatabase(t)

	if stored := readOptionValue(t, db, "secret"); stored != `""` {
		t.Fatalf("the fixture stores the secret as %s, want the quoted empty string the legacy server wrote", stored)
	}

	reloaded, err := NewOptions(db, DialectSqlite).ReloadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReloadOptions: %v", err)
	}

	if len(reloaded.Secret) != domain.SecretLength {
		t.Fatalf("the re-derived secret is %d characters, want the generated %d", len(reloaded.Secret), domain.SecretLength)
	}

	if reloaded.Secret == "" {
		t.Error("the re-derived secret is the stored empty string, want a generated one")
	}

	if stored := readOptionValue(t, db, "secret"); stored != `""` {
		t.Errorf("the stored secret is now %s, want it left as the empty string: the secret in use is not the one on record", stored)
	}

	again, err := NewOptions(db, DialectSqlite).ReloadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReloadOptions: %v", err)
	}

	if again.Secret == reloaded.Secret {
		t.Error("two re-derivations produced the same secret, want a fresh one each time")
	}
}

func TestReloadOptionsAcceptsAStoredSecretOnlyAtItsFullLength(t *testing.T) {
	kept := strings.Repeat("a", domain.SecretLength)

	for _, tc := range []struct {
		name   string
		stored string
		want   string
	}{
		{"the stored secret is kept", strconv.Quote(kept), kept},
		{"a short stored secret is replaced", `"short"`, ""},
		{"a stored secret that is not a string is replaced", "123", ""},
		{"a stored secret that is null is replaced", "null", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")

			const query = `INSERT INTO "options" ("key", "value") VALUES ('secret', ?)`

			if _, err := db.Exec(query, tc.stored); err != nil {
				t.Fatalf("%s: %v", query, err)
			}
			reloaded, err := NewOptions(db, DialectSqlite).ReloadOptions(context.Background())
			if err != nil {
				t.Fatalf("ReloadOptions: %v", err)
			}

			if len(reloaded.Secret) != domain.SecretLength {
				t.Fatalf("the secret re-derived from %s is %d characters, want %d", tc.stored, len(reloaded.Secret), domain.SecretLength)
			}

			if tc.want != "" && reloaded.Secret != tc.want {
				t.Errorf("the secret re-derived from %s is %q, want the stored one", tc.stored, reloaded.Secret)
			}

			if tc.want == "" && reloaded.Secret == tc.stored {
				t.Errorf("the secret re-derived from %s is the stored value, want a generated one", tc.stored)
			}
		})
	}
}

// TestWriteOptionsMakesTheSubmittedOptionsTheOnesTheServerIsRunningWith pins §9 for this section:
// a save replaces what the server runs with, and nothing is read back — which is why the saved
// window survives until the next re-derivation and then disappears.
func TestWriteOptionsMakesTheSubmittedOptionsTheOnesTheServerIsRunningWith(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	options := NewOptions(db, DialectSqlite)

	next := domain.DefaultOptions()
	next.Branding = "Acme Dispatch"
	next.PruneDays = 30
	next.DuplicateDetectionTimeFrame = 1200

	if err := options.WriteOptions(context.Background(), next); err != nil {
		t.Fatalf("WriteOptions: %v", err)
	}

	live, err := options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if live.Branding != "Acme Dispatch" || live.PruneDays != 30 || live.DuplicateDetectionTimeFrame != 1200 {
		t.Errorf("the server is running with %q/%d/%d, want what was submitted", live.Branding, live.PruneDays, live.DuplicateDetectionTimeFrame)
	}

	if stored := readOptionValue(t, db, "duplicateDetectionTimeFrame"); stored != "1200" {
		t.Errorf("the window is stored as %s, want 1200", stored)
	}

	reloaded, err := options.ReloadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReloadOptions: %v", err)
	}

	if reloaded.DuplicateDetectionTimeFrame != 500 {
		t.Errorf("the re-derived window = %d ms, want the 500 ms default the stored row cannot give back", reloaded.DuplicateDetectionTimeFrame)
	}
}

// TestWriteOptionsStoresEveryOptionAsTheJSONTheLegacyServerStores pins the stored shape, key by
// key, against the legacy server's own key list, plus the one key the legacy list was missing.
func TestWriteOptionsStoresEveryOptionAsTheJSONTheLegacyServerStores(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")

	next := domain.DefaultOptions()
	next.Branding = "Acme Dispatch"
	next.AdminPasswordHash = "$2a$10$abcdefghijklmnopqrstuvwxyzabcdefgh"
	next.DimmerDelay = 1234

	if err := NewOptions(db, DialectSqlite).WriteOptions(context.Background(), next); err != nil {
		t.Fatalf("WriteOptions: %v", err)
	}

	for _, tc := range []struct {
		key  string
		want string
	}{
		{"adminPassword", `"$2a$10$abcdefghijklmnopqrstuvwxyzabcdefgh"`},
		{"adminPasswordNeedChange", "true"},
		{"audioConversion", "1"},
		{"autoPopulate", "true"},
		{"branding", `"Acme Dispatch"`},
		{"dimmerDelay", "1234"},
		{"disableDuplicateDetection", "false"},
		{"duplicateDetectionTimeFrame", "500"},
		{"email", `""`},
		{"keypadBeeps", `"uniden"`},
		{"maxClients", "200"},
		{"playbackGoesLive", "false"},
		{"pruneDays", "7"},
		{"secret", `""`},
		{"showListenersCount", "false"},
		{"sortTalkgroups", "false"},
		{"time12hFormat", "false"},
	} {
		if stored := readOptionValue(t, db, tc.key); stored != tc.want {
			t.Errorf("%s is stored as %s, want %s", tc.key, stored, tc.want)
		}
	}

	if count := countRows(t, db, "options"); count != 17 {
		t.Errorf("the options table has %d rows, want the 16 keys the legacy server writes plus the audio-conversion mode it never stored", count)
	}
}

// TestWriteOptionsStoresTheAudioConversionMode is the repaired half of Q53.
//
// The legacy `Options.Write` stored sixteen keys and `Options.Read` looked for a stored
// `audioConversion` row among them, so the conversion mode the administrator chose applied until the
// next thing that re-read the options — a restart, or the re-derivation a password change runs — and
// then reverted to the default. Storing the mode is what makes the reader's lookup find something.
// See docs/specs/FEAT-004-legacy-defect-repairs/decision-audio-conversion-stored.md.
func TestWriteOptionsStoresTheAudioConversionMode(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	options := NewOptions(db, DialectSqlite)

	next := domain.DefaultOptions()
	next.AudioConversion = domain.AudioConversionEnabledLoudNorm

	if err := options.WriteOptions(context.Background(), next); err != nil {
		t.Fatalf("WriteOptions: %v", err)
	}

	live, err := options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if live.AudioConversion != domain.AudioConversionEnabledLoudNorm {
		t.Errorf("the server is running with conversion mode %d, want the submitted 3", live.AudioConversion)
	}

	var stored string

	lookup := `SELECT "value" FROM "options" WHERE "key" = 'audioConversion'`

	if err := db.QueryRow(lookup).Scan(&stored); err != nil {
		t.Fatalf("%s: %v, want the submitted mode on record", lookup, err)
	}

	if stored != "3" {
		t.Errorf("the conversion mode is stored as %s, want the JSON number 3", stored)
	}

	reloaded, err := options.ReloadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReloadOptions: %v", err)
	}

	if reloaded.AudioConversion != domain.AudioConversionEnabledLoudNorm {
		t.Errorf("the re-derived conversion mode = %d, want the stored 3: the mode the administrator saved has to survive the re-derivation", reloaded.AudioConversion)
	}
}

// TestReloadOptionsResetsTheOptionsTheLegacyServerResets is the Q15 half that is not Q1: the
// re-derivation starts from the defaults and only the stored rows pull the values back up.
func TestReloadOptionsResetsTheOptionsTheLegacyServerResets(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	options := NewOptions(db, DialectSqlite)

	next := domain.DefaultOptions()
	next.MaxClients = 50
	next.DimmerDelay = 100
	next.PruneDays = 1
	next.ShowListenersCount = true
	next.AutoPopulate = false

	if err := options.WriteOptions(context.Background(), next); err != nil {
		t.Fatalf("WriteOptions: %v", err)
	}

	for _, key := range []string{"maxClients", "dimmerDelay", "pruneDays", "showListenersCount", "autoPopulate"} {
		if _, err := db.Exec(`DELETE FROM "options" WHERE "key" = ?`, key); err != nil {
			t.Fatalf("DELETE: %v", err)
		}
	}

	reloaded, err := options.ReloadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReloadOptions: %v", err)
	}

	if reloaded.MaxClients != 200 || reloaded.DimmerDelay != 5000 || reloaded.PruneDays != 7 {
		t.Errorf("re-derived MaxClients/DimmerDelay/PruneDays = %d/%d/%d, want the defaults 200/5000/7", reloaded.MaxClients, reloaded.DimmerDelay, reloaded.PruneDays)
	}

	if !reloaded.AutoPopulate {
		t.Error("re-derived AutoPopulate = false, want the default true")
	}

	if reloaded.ShowListenersCount {
		t.Error("re-derived ShowListenersCount = true, want the default false")
	}
}

// TestReloadOptionsKeepsTheBrandingEmailAndTwelveHourFormatTheServerIsRunningWith pins the part of
// the legacy reset that is not a reset.
//
// server/options.go `Read` resets every option it has a default for except `branding`, `email`,
// and `time12hFormat` — and not the secret either. A stored row replaces them in every database
// the legacy server wrote; when the row is missing, the value the server was already running with
// survives the re-derivation instead of falling back to the default.
func TestReloadOptionsKeepsTheBrandingEmailAndTwelveHourFormatTheServerIsRunningWith(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	options := NewOptions(db, DialectSqlite)

	next := domain.DefaultOptions()
	next.Branding = "Acme Dispatch"
	next.Email = "dispatch@example.com"
	next.Time12hFormat = true

	if err := options.WriteOptions(context.Background(), next); err != nil {
		t.Fatalf("WriteOptions: %v", err)
	}

	for _, key := range []string{"branding", "email", "time12hFormat"} {
		if _, err := db.Exec(`DELETE FROM "options" WHERE "key" = ?`, key); err != nil {
			t.Fatalf("DELETE: %v", err)
		}
	}

	reloaded, err := options.ReloadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReloadOptions: %v", err)
	}

	if reloaded.Branding != "Acme Dispatch" {
		t.Errorf("Branding = %q after the stored row was removed, want the value the server was running with", reloaded.Branding)
	}

	if reloaded.Email != "dispatch@example.com" {
		t.Errorf("Email = %q after the stored row was removed, want the value the server was running with", reloaded.Email)
	}

	if !reloaded.Time12hFormat {
		t.Error("Time12hFormat = false after the stored row was removed, want the value the server was running with")
	}
}

func TestReloadOptionsIgnoresAStoredValueOfTheWrongType(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	options := NewOptions(db, DialectSqlite)

	next := domain.DefaultOptions()
	next.PruneDays = 30
	next.AutoPopulate = false

	if err := options.WriteOptions(context.Background(), next); err != nil {
		t.Fatalf("WriteOptions: %v", err)
	}

	for _, tc := range []struct {
		key   string
		value string
	}{
		{"pruneDays", `"30"`},
		{"autoPopulate", "1"},
	} {
		if _, err := db.Exec(`UPDATE "options" SET "value" = ? WHERE "key" = ?`, tc.value, tc.key); err != nil {
			t.Fatalf("UPDATE: %v", err)
		}
	}

	reloaded, err := options.ReloadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReloadOptions: %v", err)
	}

	if reloaded.PruneDays != 7 {
		t.Errorf("PruneDays = %d from a stored value of \"30\", want the 7 default: the legacy reader only takes a JSON number", reloaded.PruneDays)
	}

	if !reloaded.AutoPopulate {
		t.Error("AutoPopulate = false from a stored value of 1, want the true default: the legacy reader only takes a JSON boolean")
	}
}

// TestReloadOptionsReportsTheNeedToChangeFromTheStoredRow is where the administrator's answer
// comes from after a password change: the re-derived configuration, not the submitted one.
func TestReloadOptionsReportsTheNeedToChangeFromTheStoredRow(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	options := NewOptions(db, DialectSqlite)

	next := domain.DefaultOptions()
	next.AdminPasswordNeedChange = false

	if err := options.WriteOptions(context.Background(), next); err != nil {
		t.Fatalf("WriteOptions: %v", err)
	}

	live, err := options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if live.AdminPasswordNeedChange {
		t.Error("the server is running with AdminPasswordNeedChange = true, want the submitted false")
	}

	reloaded, err := options.ReloadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReloadOptions: %v", err)
	}

	if reloaded.AdminPasswordNeedChange {
		t.Error("the re-derived AdminPasswordNeedChange = true, want the stored false")
	}
}

// TestReloadOptionsLeavesNoCredentialOnRecordWhenNoneIsStored records a deviation rather than a
// parity rule.
//
// The legacy `Read` puts a freshly generated bcrypt hash of the default administrator password in
// place before it applies the stored rows, so a server whose options table has no `adminPassword`
// row still accepts the default password. bcrypt belongs to the password adapter (T060) and not to
// this package, so server-ng leaves the credential empty and the composition root (T042) has to
// store the initial one. Every database the legacy server wrote has the row, including the
// fixture, so this only shows on a database with no options at all.
func TestReloadOptionsLeavesNoCredentialOnRecordWhenNoneIsStored(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")

	reloaded, err := NewOptions(db, DialectSqlite).ReloadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReloadOptions: %v", err)
	}

	if reloaded.AdminPasswordHash != "" {
		t.Errorf("AdminPasswordHash = %q with no stored row, want it empty: the legacy server would have a bcrypt hash of the default password here", reloaded.AdminPasswordHash)
	}

	if !reloaded.AdminPasswordNeedChange {
		t.Error("AdminPasswordNeedChange = false, want the default true")
	}
}

func TestWriteOptionsKeepsTheSubmittedOptionsInEffectWhenTheStoreRejectsTheWrite(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	options := NewOptions(db, DialectSqlite)

	if _, err := options.ReadOptions(context.Background()); err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if _, err := db.Exec(`DROP TABLE "options"`); err != nil {
		t.Fatalf("DROP TABLE: %v", err)
	}

	next := domain.DefaultOptions()
	next.Branding = "Unstored Dispatch"

	if err := options.WriteOptions(context.Background(), next); err == nil {
		t.Fatal("a save against a database that cannot be written is a failure the caller is told about")
	}

	live, err := options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if live.Branding != "Unstored Dispatch" {
		t.Errorf("the server is running with %q, want the submitted branding: the legacy server replaces the options before it stores them", live.Branding)
	}
}

func TestReadOptionsReportsTheLegacyFailureShape(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")

	if _, err := db.Exec(`DROP TABLE "options"`); err != nil {
		t.Fatalf("DROP TABLE: %v", err)
	}

	_, err := NewOptions(db, DialectSqlite).ReadOptions(context.Background())

	if err == nil {
		t.Fatal("the failure is reported")
	}

	if !strings.HasPrefix(err.Error(), "options.read: ") {
		t.Errorf("error = %v, want the legacy options.read shape", err)
	}
}

func TestWriteOptionsReportsTheLegacyFailureShape(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")

	if _, err := db.Exec(`DROP TABLE "options"`); err != nil {
		t.Fatalf("DROP TABLE: %v", err)
	}

	err := NewOptions(db, DialectSqlite).WriteOptions(context.Background(), domain.DefaultOptions())

	if err == nil {
		t.Fatal("the failure is reported")
	}

	if !strings.HasPrefix(err.Error(), "options.write: ") {
		t.Errorf("error = %v, want the legacy options.write shape", err)
	}
}

// readOptionValue reads one stored option value exactly as the table holds it.
func readOptionValue(t *testing.T, db *sql.DB, key string) string {
	t.Helper()

	query := `SELECT "value" FROM "options" WHERE "key" = ?`

	var value string
	if err := db.QueryRow(query, key).Scan(&value); err != nil {
		t.Fatalf("%s (%s): %v", query, key, err)
	}

	return value
}

// The legacy server puts a freshly generated hash of the default administrator password in place at
// every startup, before it applies the stored rows, and writes nothing. A deployment that has never
// changed the password has no adminPassword row at all, and its signing secret is regenerated at every
// startup as well — which is why the initial credential has to stay out of the database.
func TestStoreInitialCredentialIsWhatTheServerAcceptsUntilAStoredCredentialArrives(t *testing.T) {
	db := openSchemaDatabase(t)
	options := NewOptions(db, DialectSqlite)

	options.StoreInitialCredential("$2a$10$theinitialcredential")

	live, err := options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if live.AdminPasswordHash != "$2a$10$theinitialcredential" {
		t.Errorf("AdminPasswordHash = %q, want the initial credential the composition root supplied", live.AdminPasswordHash)
	}

	if rows := countOptionRows(t, db); rows != 0 {
		t.Errorf("the options table holds %d rows after an initial credential, want none: the legacy server "+
			"keeps this credential in memory, and writing it would write the signing secret with it", rows)
	}
}

// A stored credential is the configuration the administrator chose, and it wins.
func TestStoreInitialCredentialYieldsToTheCredentialInTheStoredConfiguration(t *testing.T) {
	db := openLegacyDatabase(t)
	options := NewOptions(db, DialectSqlite)

	stored, err := options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if stored.AdminPasswordHash == "" {
		t.Fatalf("the fixture stores no administrator credential, want one to compare against")
	}

	options.StoreInitialCredential("$2a$10$theinitialcredential")

	live, err := options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if live.AdminPasswordHash != stored.AdminPasswordHash {
		t.Errorf("AdminPasswordHash = %q, want the stored credential, not the initial one", live.AdminPasswordHash)
	}
}

// The password change re-derives the options from what is stored, and the initial credential is part
// of that reset rather than a one-time patch: a server that has not chosen a credential still needs
// one after the re-derivation.
func TestStoreInitialCredentialSurvivesTheReDerivationAPasswordChangeRuns(t *testing.T) {
	db := openSchemaDatabase(t)
	options := NewOptions(db, DialectSqlite)

	options.StoreInitialCredential("$2a$10$theinitialcredential")

	reloaded, err := options.ReloadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReloadOptions: %v", err)
	}

	if reloaded.AdminPasswordHash != "$2a$10$theinitialcredential" {
		t.Errorf("AdminPasswordHash after the re-derivation = %q, want the initial credential to be put back "+
			"as the legacy server puts it back on every read", reloaded.AdminPasswordHash)
	}
}

// countOptionRows is how many options the table holds.
func countOptionRows(t *testing.T, db *sql.DB) int {
	t.Helper()

	const query = `SELECT COUNT(*) FROM "options"`

	var rows int
	if err := db.QueryRow(query).Scan(&rows); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	return rows
}
