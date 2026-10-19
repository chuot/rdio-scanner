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
	"strconv"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/domain"
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

	if options.MaxClients != 200 || options.PruneDays != 7 {
		t.Errorf("MaxClients/PruneDays = %d/%d, want the stored 200/7", options.MaxClients, options.PruneDays)
	}

	if !options.AutoPopulate || !options.ShowListenersCount {
		t.Errorf("AutoPopulate/ShowListenersCount = %v/%v, want the stored true for each", options.AutoPopulate, options.ShowListenersCount)
	}

	if options.SortTalkgroups || options.DisableDuplicateDetection {
		t.Errorf("SortTalkgroups/DisableDuplicateDetection = %v/%v, want the stored false for each", options.SortTalkgroups, options.DisableDuplicateDetection)
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

// TestReloadOptionsRestoresTheConstantDuplicateDetectionWindow is a preserved quirk proven against a database the
// legacy server wrote.
//
// The legacy fixture stores the window as 750 under `duplicateDetectionTimeFrame`. The option is
// retired: the re-derivation ignores the stored row, the server runs on the 500 ms domain constant,
// and the legacy row is left on record untouched.
func TestReloadOptionsRestoresTheConstantDuplicateDetectionWindow(t *testing.T) {
	db := openLegacyDatabase(t)

	if stored := readOptionValue(t, db, "duplicateDetectionTimeFrame"); stored != "750" {
		t.Fatalf("the fixture stores the window as %s under the capital-F key, want 750", stored)
	}

	reloaded, err := NewOptions(db, DialectSqlite).ReloadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReloadOptions: %v", err)
	}

	if reloaded.DuplicateDetectionWindow() != 500*time.Millisecond {
		t.Errorf("DuplicateDetectionWindow = %v, want the 500 ms constant: the retired stored row is ignored", reloaded.DuplicateDetectionWindow())
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
// TestReloadOptionsLeavesNoSecretOnRecordWhenNoneIsUsable is the repaired half of a preserved quirk: the reader no
// longer invents a secret. A stored secret the server cannot sign with leaves the options without one,
// and the stored row stays where it was — putting a usable secret on record is the startup use case's
// job, not the reader's.
func TestReloadOptionsLeavesNoSecretOnRecordWhenNoneIsUsable(t *testing.T) {
	db := openLegacyDatabase(t)

	if stored := readOptionValue(t, db, "secret"); stored != `""` {
		t.Fatalf("the fixture stores the secret as %s, want the quoted empty string the legacy server wrote", stored)
	}

	reloaded, err := NewOptions(db, DialectSqlite).ReloadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReloadOptions: %v", err)
	}

	if reloaded.Secret != "" {
		t.Errorf("the re-derived secret = %q, want nothing on the options: the reader does not make secrets", reloaded.Secret)
	}

	if stored := readOptionValue(t, db, "secret"); stored != `""` {
		t.Errorf("the stored secret is now %s, want it left exactly as the legacy server wrote it", stored)
	}
}

// TestReloadOptionsAcceptsAStoredSecretOnlyAtItsFullLength keeps the legacy reader's length rule and
// drops what the legacy reader did with a value that failed it.
//
// The legacy server generated a fresh secret for anything unusable and never wrote it down, so every
// start signed with a different key and every token issued before it was dead. Here an unusable value
// is simply not taken, and the options are left without a secret for the startup use case to fill.
func TestReloadOptionsAcceptsAStoredSecretOnlyAtItsFullLength(t *testing.T) {
	kept := strings.Repeat("a", domain.SecretLength)

	for _, tc := range []struct {
		name   string
		stored string
		want   string
	}{
		{"the stored secret is kept", strconv.Quote(kept), kept},
		{"a short stored secret is not taken", `"short"`, ""},
		{"a stored secret that is not a string is not taken", "123", ""},
		{"a stored secret that is null is not taken", "null", ""},
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

			if reloaded.Secret != tc.want {
				t.Errorf("the secret re-derived from %s = %q, want %q", tc.stored, reloaded.Secret, tc.want)
			}
		})
	}
}

// TestStoreSecretPutsTheSigningSecretOnRecord is the write the legacy server never had: one option row,
// written on its own, so the secret the server signs with is the secret the database holds.
func TestStoreSecretPutsTheSigningSecretOnRecord(t *testing.T) {
	ctx := context.Background()

	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	options := NewOptions(db, DialectSqlite)

	next := domain.DefaultOptions()
	next.Branding = "Acme Dispatch"

	if err := options.WriteOptions(ctx, next); err != nil {
		t.Fatalf("WriteOptions: %v", err)
	}

	secret := strings.Repeat("k", domain.SecretLength)

	if err := options.StoreSecret(ctx, secret); err != nil {
		t.Fatalf("StoreSecret: %v", err)
	}

	if stored := readOptionValue(t, db, "secret"); stored != strconv.Quote(secret) {
		t.Errorf("the stored secret = %s, want the generated secret as the JSON the reader unmarshals", stored)
	}

	if count := countRows(t, db, "options"); count != 13 {
		t.Errorf("the options table has %d rows, want the secret row updated in place and nothing else written", count)
	}

	if branding := readOptionValue(t, db, "branding"); branding != `"Acme Dispatch"` {
		t.Errorf("the stored branding = %s, want it left alone: storing a secret is not saving the configuration", branding)
	}

	live, err := options.ReadOptions(ctx)
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if live.Secret != secret {
		t.Errorf("the secret the next read applies = %q, want the one that was stored", live.Secret)
	}
}

// TestWriteOptionsMakesTheSubmittedOptionsTheOnesTheServerIsRunningWith pins the configuration-in-effect rule for this section:
// a save replaces what the server runs with, and nothing is read back. The duplicate window is the
// domain constant, so a save cannot move it, and the retired window key is no longer stored.
func TestWriteOptionsMakesTheSubmittedOptionsTheOnesTheServerIsRunningWith(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	options := NewOptions(db, DialectSqlite)

	next := domain.DefaultOptions()
	next.Branding = "Acme Dispatch"
	next.PruneDays = 30

	if err := options.WriteOptions(context.Background(), next); err != nil {
		t.Fatalf("WriteOptions: %v", err)
	}

	live, err := options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if live.Branding != "Acme Dispatch" || live.PruneDays != 30 {
		t.Errorf("the server is running with %q/%d, want what was submitted", live.Branding, live.PruneDays)
	}

	if live.DuplicateDetectionWindow() != 500*time.Millisecond {
		t.Errorf("DuplicateDetectionWindow = %v, want the 500 ms constant", live.DuplicateDetectionWindow())
	}

	if count := countOptionRowsForKey(t, db, "duplicateDetectionTimeFrame"); count != 0 {
		t.Errorf("the retired window key has %d rows, want none written for it", count)
	}

	reloaded, err := options.ReloadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReloadOptions: %v", err)
	}

	if reloaded.DuplicateDetectionWindow() != 500*time.Millisecond {
		t.Errorf("the re-derived window = %v, want the 500 ms constant", reloaded.DuplicateDetectionWindow())
	}
}

// TestWriteOptionsStoresEveryOptionAsTheJSONTheLegacyServerStores pins the stored shape, key by
// key, against the legacy server's own key list, plus the one key the legacy list was missing.
func TestWriteOptionsStoresEveryOptionAsTheJSONTheLegacyServerStores(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")

	next := domain.DefaultOptions()
	next.Branding = "Acme Dispatch"
	next.AdminPasswordHash = "$2a$10$abcdefghijklmnopqrstuvwxyzabcdefgh"

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
		{"disableDuplicateDetection", "false"},
		{"email", `""`},
		{"keypadBeeps", `"uniden"`},
		{"maxClients", "200"},
		{"pruneDays", "7"},
		{"secret", `""`},
		{"showListenersCount", "false"},
		{"sortTalkgroups", "false"},
	} {
		if stored := readOptionValue(t, db, tc.key); stored != tc.want {
			t.Errorf("%s is stored as %s, want %s", tc.key, stored, tc.want)
		}
	}

	if count := countRows(t, db, "options"); count != 13 {
		t.Errorf("the options table has %d rows, want the 13 keys this server writes", count)
	}
}

// TestWriteOptionsStoresTheAudioConversionMode is the repaired half of a preserved quirk.
//
// The legacy `Options.Write` stored sixteen keys and `Options.Read` looked for a stored
// `audioConversion` row among them, so the conversion mode the administrator chose applied until the
// next thing that re-read the options — a restart, or the re-derivation a password change runs — and
// then reverted to the default. Storing the mode is what makes the reader's lookup find something.
// Storing the mode is what makes the reader's lookup find something.
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

// TestReloadOptionsResetsTheOptionsTheLegacyServerResets is the reload-reset half that is not the missing-key default: the
// re-derivation starts from the defaults and only the stored rows pull the values back up.
func TestReloadOptionsResetsTheOptionsTheLegacyServerResets(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	options := NewOptions(db, DialectSqlite)

	next := domain.DefaultOptions()
	next.MaxClients = 50
	next.PruneDays = 1
	next.ShowListenersCount = true
	next.AutoPopulate = false

	if err := options.WriteOptions(context.Background(), next); err != nil {
		t.Fatalf("WriteOptions: %v", err)
	}

	for _, key := range []string{"maxClients", "pruneDays", "showListenersCount", "autoPopulate"} {
		if _, err := db.Exec(`DELETE FROM "options" WHERE "key" = ?`, key); err != nil {
			t.Fatalf("DELETE: %v", err)
		}
	}

	reloaded, err := options.ReloadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReloadOptions: %v", err)
	}

	if reloaded.MaxClients != 200 || reloaded.PruneDays != 7 {
		t.Errorf("re-derived MaxClients/PruneDays = %d/%d, want the defaults 200/7", reloaded.MaxClients, reloaded.PruneDays)
	}

	if !reloaded.AutoPopulate {
		t.Error("re-derived AutoPopulate = false, want the default true")
	}

	if reloaded.ShowListenersCount {
		t.Error("re-derived ShowListenersCount = true, want the default false")
	}
}

// TestReloadOptionsKeepsTheBrandingAndEmailTheServerIsRunningWith pins the part of the legacy
// reset that is not a reset.
//
// The legacy `server/options.go` `Read` resets every option it has a default for except `branding`
// and `email` — and not the secret either. (`time12hFormat` was in that leave-alone list too; the
// option is retired.) A stored row replaces them in every database the legacy server wrote; when
// the row is missing, the value the server was already running with survives the re-derivation
// instead of falling back to the default.
func TestReloadOptionsKeepsTheBrandingAndEmailTheServerIsRunningWith(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	options := NewOptions(db, DialectSqlite)

	next := domain.DefaultOptions()
	next.Branding = "Acme Dispatch"
	next.Email = "dispatch@example.com"

	if err := options.WriteOptions(context.Background(), next); err != nil {
		t.Fatalf("WriteOptions: %v", err)
	}

	for _, key := range []string{"branding", "email"} {
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
// row still accepts the default password. bcrypt belongs to the password adapter and not to
// this package, so this server leaves the credential empty and the composition root has to
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

// countOptionRowsForKey reports how many rows the options table holds for one key. A
// retired key must have none.
func countOptionRowsForKey(t *testing.T, db *sql.DB, key string) int {
	t.Helper()

	query := `SELECT COUNT(*) FROM "options" WHERE "key" = ?`

	var count int
	if err := db.QueryRow(query, key).Scan(&count); err != nil {
		t.Fatalf("%s (%s): %v", query, key, err)
	}

	return count
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

// TestWriteOptionsTwiceLeavesOneRowPerKey: saving the
// configuration is an update of the stored rows, not a fresh copy of them. On MySQL the
// update reports no changed rows when the value is unchanged, the insert fires for
// every key, and the table ends up with two rows per key — the failure this repair removes. On every
// dialect the table holds exactly one row per key after two saves.
func TestWriteOptionsTwiceLeavesOneRowPerKey(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")

	options := NewOptions(db, DialectSqlite)

	next := domain.DefaultOptions()
	next.Branding = "Acme Dispatch"

	for i := 0; i < 2; i++ {
		if err := options.WriteOptions(context.Background(), next); err != nil {
			t.Fatalf("WriteOptions %d: %v", i, err)
		}
	}

	var rows int

	if err := db.QueryRow(`SELECT COUNT(*) FROM "options"`).Scan(&rows); err != nil {
		t.Fatalf("counting the option rows: %v", err)
	}

	if want := len(optionRows(next)); rows != want {
		t.Errorf("the options table holds %d rows after two saves, want the %d keys the save writes", rows, want)
	}
}

// TestReloadOptionsReportsAContextCancelledMidRead: re-deriving
// the options reads every stored row. A read cancelled mid-iteration is a read that
// did not finish; today the fetch loop skips the rows whose scan failed and the
// re-derivation answers success with defaults where stored values were dropped — the
// server ends up running with options nobody stored.
func TestReloadOptionsReportsAContextCancelledMidRead(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")

	defer db.Close()

	// Two hundred-odd thousand keys: the read takes hundreds of milliseconds, and
	// the cancel lands while it is running. The table is filled by doubling — one
	// statement per doubling, every key distinct.
	if _, err := db.Exec(`INSERT INTO "options" ("key", "value") VALUES ('probeKey000000', '"x"')`); err != nil {
		t.Fatalf("seeding the option rows: %v", err)
	}

	for i := 0; i < 18; i++ {
		if _, err := db.Exec(`INSERT INTO "options" ("key", "value") SELECT "key" || 'x', "value" FROM "options"`); err != nil {
			t.Fatalf("doubling the option rows: %v", err)
		}
	}

	options := NewOptions(db, DialectSqlite)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)

		cancel()
	}()

	if _, err := options.ReloadOptions(ctx); err == nil {
		t.Fatal("ReloadOptions = nil for a read cancelled mid-iteration, want the failure of the read it did not finish")
	}
}

// TestReadOptionsAnswersFromTheCacheWhileASaveIsStillWalkingTheDatabase: the options cache answers the server's own reads — the login
// handler, the retention pass, the signing secret — and today a configuration
// save holds the cache's mutex across the thirteen rows it writes. The cache
// answers while the save is in flight.
func TestReadOptionsAnswersFromTheCacheWhileASaveIsStillWalkingTheDatabase(t *testing.T) {
	db, counted := openCountedSchemaDatabase(t)

	options := NewOptions(db, DialectSqlite)

	options.StoreInitialCredential("initial-credential")

	running, err := options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	counted.setDelay(200 * time.Millisecond)

	finished := make(chan struct{})

	go func() {
		defer close(finished)

		running.PruneDays = 30

		if err := options.WriteOptions(context.Background(), running); err != nil {
			t.Errorf("WriteOptions: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	started := time.Now()

	cached, err := options.ReadOptions(context.Background())
	elapsed := time.Since(started)

	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if elapsed > 250*time.Millisecond {
		t.Errorf("the cache answer took %v with a save in flight, want the running options back without waiting on the save's statements", elapsed)
	}

	if cached.AdminPasswordHash == "" {
		t.Error("the cache answered with options that name no administrator credential, want the running options")
	}

	<-finished

	counted.setDelay(0)

	after, err := options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if after.PruneDays != 30 {
		t.Errorf("the options after the save read PruneDays %d, want 30: the save takes effect when it is written", after.PruneDays)
	}
}
