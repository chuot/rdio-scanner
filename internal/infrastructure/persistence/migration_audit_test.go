package persistence

// The integrity audit is REQ-006: the proof that the pass lost nothing.
//
// It reads a v6.6.3 database twice — once before the pass, into shadow tables the pass does not touch,
// and once after — and asks of every row the deployment's database held whether the pass carried it or
// archived it. Then it asks, column by column, whether the value that arrived is the value the rule in
// docs/specs/FEAT-011-v663-migration-integrity/data-model.md names.
//
// Every failure names the v6 table, the row's identity, and the column, because the person a migration
// failure is for has to go and look at one row of one table, and a failure that says only "the archive
// does not match" sends them nowhere.

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

// auditTable names one v6 table, the column that identifies its rows, and the v8 table and column those
// identities arrive in.
type auditTable struct {
	v6, v6Identity, v8, v8Identity string
}

// auditTables is every v6 table whose rows are deployment data. `rdioScannerConfigs` is not in it,
// because its rows do not arrive one for one — the options row unpacks into up to fourteen — and it is
// audited on its own. `rdioScannerMeta` is not in it either: it is v6 bookkeeping with no v8 counterpart,
// and `TestTheMigrationDropsWhatTheV8SchemaHasNoRoomFor` is where that declared exception is proved.
var auditTables = []auditTable{
	{v6: "rdioScannerGroups", v6Identity: "_id", v8: "groups", v8Identity: "groupId"},
	{v6: "rdioScannerTags", v6Identity: "_id", v8: "tags", v8Identity: "tagId"},
	{v6: "rdioScannerSystems", v6Identity: "_id", v8: "systems", v8Identity: "systemId"},
	{v6: "rdioScannerTalkgroups", v6Identity: "_id", v8: "talkgroups", v8Identity: "talkgroupId"},
	{v6: "rdioScannerUnits", v6Identity: "_id", v8: "units", v8Identity: "unitId"},
	{v6: "rdioScannerLogs", v6Identity: "_id", v8: "logs", v8Identity: "logId"},
	{v6: "rdioScannerDownstreams", v6Identity: "_id", v8: "downstreams", v8Identity: "downstreamId"},
	{v6: "rdioScannerDirWatches", v6Identity: "_id", v8: "dirwatches", v8Identity: "dirwatchId"},
	{v6: "rdioScannerCalls", v6Identity: "id", v8: "calls", v8Identity: "callId"},
	{v6: "rdioScannerApiKeys", v6Identity: "_id", v8: "apikeys", v8Identity: "apikeyId"},
	{v6: "rdioScannerAccesses", v6Identity: "_id", v8: "accesses", v8Identity: "accessId"},
}

// auditShadow copies every v6 table before the pass runs. The pass drops the v6 tables it reads, and the
// audit has to compare the deployment's database with what the same database holds afterwards, so it
// keeps its own copy of the rows the pass is about to remove. Only this test's database ever sees them.
func auditShadow(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, table := range auditTables {
		mustExec(t, db, `CREATE TABLE "audit`+table.v6+`" AS SELECT * FROM "`+table.v6+`"`)
	}

	mustExec(t, db, `CREATE TABLE "auditrdioScannerConfigs" AS SELECT * FROM "rdioScannerConfigs"`)
	mustExec(t, db, `CREATE TABLE "auditrdioScannerMeta" AS SELECT * FROM "rdioScannerMeta"`)
}

// auditIdentities reads the identities one table holds, in identity order.
func auditIdentities(t *testing.T, db *sql.DB, table, column string) []string {
	t.Helper()

	return auditIdentitiesWhere(t, db, table, column, "")
}

// auditIdentitiesWhere reads the identities one table holds under a condition, in identity order.
//
// Every one of them has to be a number. sqlite reads a quoted name that matches no column as the word
// itself, so a column name this audit got wrong would come back as the same word on both sides of every
// comparison and read as a pass. A value that is not an identity is that mistake, and it is fatal here
// rather than a mismatch further down.
func auditIdentitiesWhere(t *testing.T, db *sql.DB, table, column, where string) []string {
	t.Helper()

	identities := []string{}

	for _, value := range auditColumnValues(t, db, table, column, where) {
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			t.Fatalf("the audit read %s.%s and got %q, which is not an identity: sqlite reads a quoted name that "+
				"matches no column as the word itself, so this is the audit naming a column the table does not have",
				table, column, value)
		}

		identities = append(identities, value)
	}

	return identities
}

// auditColumnValues reads one column of one table, in column order. The options keys are compared by name
// and not by number, so they are read here and not through the identity rule above.
func auditColumnValues(t *testing.T, db *sql.DB, table, column, where string) []string {
	t.Helper()

	query := `SELECT "` + column + `" FROM "` + table + `"`

	if where != "" {
		query += " WHERE " + where
	}

	values := []string{}

	for _, row := range readRows(t, db, query+` ORDER BY "`+column+`"`) {
		values = append(values, row[0])
	}

	return values
}

// TestTheMigrationCarriesOrArchivesEveryRowOfTheLegacyDatabase is the conservation invariant: for every
// row of every v6 table that holds deployment data, the pass either carried it into the v8 table its rule
// names or archived it with a reason. A row in neither is the defect this feature exists to end. A row in
// both is a pass that cannot say what it did.
func TestTheMigrationCarriesOrArchivesEveryRowOfTheLegacyDatabase(t *testing.T) {
	db := openLegacyV6Database(t)

	auditShadow(t, db)

	migrateDatabase(t, db)

	auditConservation(t, db)
}

// auditConservation is the conservation invariant on a database that already holds its shadow copies and
// has already been migrated. The sqlite test and the live-backend tests are one audit run on three
// backends, which is what keeps a backend from being proven by a different set of rules than the others.
func auditConservation(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, table := range auditTables {
		read := auditIdentities(t, db, "audit"+table.v6, table.v6Identity)
		carried := auditIdentities(t, db, table.v8, table.v8Identity)
		archived := auditIdentitiesWhere(t, db, "migrationArchive", "rowId", `"tableName" = '`+table.v6+`'`)

		if len(read) == 0 {
			t.Fatalf("the fixture has no %s rows, so this table proves nothing", table.v6)
		}

		for _, identity := range read {
			inV8, inArchive := hasString(carried, identity), hasString(archived, identity)

			switch {
			case inV8 && inArchive:
				t.Errorf("%s row %s is in the v8 %s table and in the migrationArchive table: the pass counted the same row twice, "+
					"so it cannot say which one an operator should trust", table.v6, identity, table.v8)

			case !inV8 && !inArchive:
				t.Errorf("%s row %s is in neither the v8 %s table nor the migrationArchive table: the migration lost it",
					table.v6, identity, table.v8)
			}
		}

		// The archive is a record of this deployment's rows, not a place where rows appear.
		for _, identity := range archived {
			if !hasString(read, identity) {
				t.Errorf("the migrationArchive table holds %s row %s, which the v6 database does not have", table.v6, identity)
			}
		}
	}
}

// TestTheMigrationCarriesOrArchivesEveryConfigRowOfTheLegacyDatabase is the conservation invariant on the
// one v6 table whose rows do not arrive one for one.
//
// A config row that is not the options row arrives as one options row of the same key. The options row
// unpacks into up to fourteen options rows, so it is carried and archived at the same time when some of
// its keys have no v8 counterpart: the archive holds the v6 row whole, which is what makes the keys that
// were dropped recoverable. The audit therefore counts conservation per key, which is what the v8 table
// actually holds.
func TestTheMigrationCarriesOrArchivesEveryConfigRowOfTheLegacyDatabase(t *testing.T) {
	db := openLegacyV6Database(t)

	auditShadow(t, db)

	migrateDatabase(t, db)

	auditConfigConservation(t, db)
}

// auditConfigConservation is the same invariant on the configs table, which is the one table whose rows do
// not arrive one for one.
func auditConfigConservation(t *testing.T, db *sql.DB) {
	t.Helper()

	archived := auditIdentitiesWhere(t, db, "migrationArchive", "rowId", `"tableName" = 'rdioScannerConfigs'`)

	for _, row := range readRows(t, db, `SELECT "_id", "key" FROM "auditrdioScannerConfigs" ORDER BY "_id"`) {
		rowId, key := row[0], row[1]

		if key != "options" {
			if carried := auditColumnValues(t, db, "options", "key", `"key" = '`+key+`'`); !hasString(carried, key) {
				if !hasString(archived, rowId) {
					t.Errorf("rdioScannerConfigs row %s, key %q: the migration carried it into neither the options table nor the archive",
						rowId, key)
				}
			}

			continue
		}

		// Every key the v6 blob holds is either an options row or named in the archived blob.
		var blob map[string]any

		if err := json.Unmarshal([]byte(readRows(t, db, `SELECT "val" FROM "auditrdioScannerConfigs" WHERE "key" = 'options'`)[0][0]), &blob); err != nil {
			t.Fatalf("the fixture's options blob: %v", err)
		}

		carried := auditColumnValues(t, db, "options", "key", "")

		// The archived copy of the options row holds the blob as the v6 table held it, which is what makes
		// a key the pass could not carry recoverable out of the archive.
		archived := []map[string]any{}

		for _, row := range readRows(t, db, `SELECT "contents" FROM "migrationArchive" WHERE "tableName" = 'rdioScannerConfigs'`) {
			var contents struct {
				Val string `json:"val"`
			}

			if err := json.Unmarshal([]byte(row[0]), &contents); err != nil {
				t.Fatalf("an archived config row holds contents that are not the row as v6 wrote it: %v", err)
			}

			var unpacked map[string]any

			if err := json.Unmarshal([]byte(contents.Val), &unpacked); err != nil {
				continue
			}

			archived = append(archived, unpacked)
		}

		for key := range blob {
			if hasString(carried, key) {
				continue
			}

			found := false

			for _, unpacked := range archived {
				if _, ok := unpacked[key]; ok {
					found = true

					break
				}
			}

			if !found {
				t.Errorf("the options blob's key %q is in neither the options table nor an archived config row: the migration lost it",
					key)
			}
		}
	}
}

// pair writes one column of a value comparison: the name the v6 table gives it and the name the v8 table
// gives it. The queries below read the two side by side so that a failure can name both.
func pair(v6, v8 string) string {
	return `COALESCE(v."` + v6 + `", ''), t."` + v8 + `"`
}

// number is pair for a column the v6 table holds as a number, where the empty value is 0.
func number(v6, v8 string) string {
	return `COALESCE(v."` + v6 + `", 0), COALESCE(t."` + v8 + `", 0)`
}

// flag is a v6 flag column and its v8 counterpart. The fallback is `false` and not 0 because PostgreSQL
// stores these as a boolean and COALESCE will not mix a boolean with an integer; MariaDB and sqlite read
// the same word as the 0 and 1 their columns hold.
func flag(v6, v8 string) string {
	return `COALESCE(v."` + v6 + `", false), COALESCE(t."` + v8 + `", false)`
}

// assertCarriedValues compares each named column's v6 value with the v8 value it arrived in.
//
// The query has to read the row's identity first and then the pairs in the order `columns` names them,
// which is what lets a failure name the table, the row, and the column.
func assertCarriedValues(t *testing.T, db *sql.DB, table string, query string, columns ...string) {
	t.Helper()

	rows := readRows(t, db, query)

	if len(rows) == 0 {
		t.Fatalf("the %s comparison read no rows, so it proves nothing: %s", table, query)
	}

	for _, row := range rows {
		for i, column := range columns {
			if row[2*i+1] != row[2*i+2] {
				t.Errorf("%s row %s: %q is %q in the v6 table and %q in the v8 table",
					table, row[0], column, row[2*i+1], row[2*i+2])
			}
		}
	}
}

// TestTheMigrationCarriesEveryValueIntoTheV8ColumnItsRuleNames is the value-fidelity half of REQ-006:
// every rule of the data model's conversion table, read out of the database the pass left.
func TestTheMigrationCarriesEveryValueIntoTheV8ColumnItsRuleNames(t *testing.T) {
	db := openLegacyV6Database(t)

	auditShadow(t, db)

	migrateDatabase(t, db)

	auditValues(t, db)
}

// auditValues is the value-fidelity half of REQ-006 on a migrated database that already holds its shadow
// copies: every rule of the data model's conversion table, read out of the database the pass left.
func auditValues(t *testing.T, db *sql.DB) {
	t.Helper()

	// Groups and tags keep their identity and their label, and are ordered by label from 1.
	assertCarriedValues(t, db, "rdioScannerGroups",
		`SELECT v."_id", `+pair("label", "label")+` FROM "auditrdioScannerGroups" v JOIN "groups" t ON t."groupId" = v."_id" ORDER BY v."_id"`,
		"label")

	assertCarriedValues(t, db, "rdioScannerTags",
		`SELECT v."_id", `+pair("label", "label")+` FROM "auditrdioScannerTags" v JOIN "tags" t ON t."tagId" = v."_id" ORDER BY v."_id"`,
		"label")

	assertOrderedByLabel(t, db, "auditrdioScannerGroups", "_id", "groups", "groupId", "label")
	assertOrderedByLabel(t, db, "auditrdioScannerTags", "_id", "tags", "tagId", "label")

	// Systems keep their identity, their radio-system reference, and their settings; the blacklist JSON
	// arrives stripped of the brackets v6 stored it in.
	assertCarriedValues(t, db, "rdioScannerSystems",
		`SELECT v."_id", `+number("id", "systemRef")+`, `+pair("label", "label")+`, `+pair("led", "led")+`, `+
			number("order", "order")+`, `+flag("autoPopulate", "autoPopulate")+
			`, COALESCE(REPLACE(REPLACE(v."blacklists", '[', ''), ']', ''), ''), t."blacklists" `+
			`FROM "auditrdioScannerSystems" v JOIN "systems" t ON t."systemId" = v."_id" ORDER BY v."_id"`,
		"id", "label", "led", "order", "autoPopulate", "blacklists")

	// Talkgroups keep their identity, their name, and their references; the system reference is resolved
	// to the v8 system it names.
	assertCarriedValues(t, db, "rdioScannerTalkgroups",
		`SELECT v."_id", `+number("frequency", "frequency")+`, `+pair("label", "label")+`, `+pair("led", "led")+`, `+
			pair("name", "name")+`, `+number("order", "order")+`, `+number("id", "talkgroupRef")+`, `+number("tagId", "tagId")+
			`, COALESCE(v."systemId", 0), COALESCE(s."systemRef", 0) `+
			`FROM "auditrdioScannerTalkgroups" v JOIN "talkgroups" t ON t."talkgroupId" = v."_id" `+
			`JOIN "systems" s ON s."systemId" = t."systemId" ORDER BY v."_id"`,
		"frequency", "label", "led", "name", "order", "id", "tagId", "systemId")

	// A talkgroup's group arrives as a row of talkgroupGroups, except the group of 0 that v6 used for
	// "no group" and that has no v8 row to arrive as.
	assertCarriedValues(t, db, "rdioScannerTalkgroups",
		`SELECT v."_id", v."groupId", COALESCE(g."groupId", 0) FROM "auditrdioScannerTalkgroups" v `+
			`JOIN "talkgroups" t ON t."talkgroupId" = v."_id" LEFT JOIN "talkgroupGroups" g ON g."talkgroupId" = t."talkgroupId" `+
			`WHERE v."groupId" != 0 ORDER BY v."_id"`,
		"groupId")

	// Units keep their identity, their unit number — the FEAT-004 repair — and their system.
	assertCarriedValues(t, db, "rdioScannerUnits",
		`SELECT v."_id", `+number("id", "unitRef")+`, `+pair("label", "label")+`, `+number("order", "order")+
			`, COALESCE(v."systemId", 0), COALESCE(s."systemRef", 0) `+
			`FROM "auditrdioScannerUnits" v JOIN "units" t ON t."unitId" = v."_id" `+
			`JOIN "systems" s ON s."systemId" = t."systemId" WHERE v."systemId" != 10009 ORDER BY v."_id"`,
		"id", "label", "order", "systemId")

	// Downstreams, api keys, and accesses are copied, identity first.
	assertCarriedValues(t, db, "rdioScannerDownstreams",
		`SELECT v."_id", `+pair("apiKey", "apikey")+`, `+flag("disabled", "disabled")+`, `+number("order", "order")+
			`, `+pair("systems", "systems")+`, `+pair("url", "url")+` FROM "auditrdioScannerDownstreams" v `+
			`JOIN "downstreams" t ON t."downstreamId" = v."_id" ORDER BY v."_id"`,
		"apiKey", "disabled", "order", "systems", "url")

	assertCarriedValues(t, db, "rdioScannerApiKeys",
		`SELECT v."_id", `+flag("disabled", "disabled")+`, `+pair("ident", "ident")+`, `+pair("key", "key")+`, `+
			number("order", "order")+`, `+pair("systems", "systems")+` FROM "auditrdioScannerApiKeys" v `+
			`JOIN "apikeys" t ON t."apikeyId" = v."_id" ORDER BY v."_id"`,
		"disabled", "ident", "key", "order", "systems")

	assertCarriedValues(t, db, "rdioScannerAccesses",
		`SELECT v."_id", `+pair("code", "code")+`, `+pair("ident", "ident")+`, `+number("limit", "limit")+`, `+
			number("order", "order")+`, `+pair("systems", "systems")+` FROM "auditrdioScannerAccesses" v `+
			`JOIN "accesses" t ON t."accessId" = v."_id" ORDER BY v."_id"`,
		"code", "ident", "limit", "order", "systems")

	// Dirwatches keep every column v8 has room for. Their references are the rule below, because a
	// reference the legacy lookup had no row for arrives as nothing.
	assertCarriedValues(t, db, "rdioScannerDirWatches",
		`SELECT v."_id", `+number("delay", "delay")+`, `+flag("deleteAfter", "deleteAfter")+`, `+
			pair("directory", "directory")+`, `+flag("disabled", "disabled")+`, `+pair("extension", "extension")+`, `+
			number("frequency", "frequency")+`, `+pair("mask", "mask")+`, `+number("order", "order")+`, `+
			pair("type", "type")+` FROM "auditrdioScannerDirWatches" v JOIN "dirwatches" t ON t."dirwatchId" = v."_id" `+
			`ORDER BY v."_id"`,
		"delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "type")

	assertDirwatchReferences(t, db)

	// Calls keep their identity, their audio byte for byte, and their names; the system and talkgroup
	// references are resolved, and every call of a v6 deployment that had no site arrives with siteRef 0.
	assertCarriedValues(t, db, "rdioScannerCalls",
		`SELECT v."id", v."audio", t."audio", `+pair("audioName", "audioFilename")+`, `+pair("audioType", "audioMime")+
			`, COALESCE(v."system", 0), COALESCE(s."systemRef", 0), COALESCE(v."talkgroup", 0), COALESCE(g."talkgroupRef", 0), 0, t."siteRef" `+
			`FROM "auditrdioScannerCalls" v JOIN "calls" t ON t."callId" = v."id" `+
			`JOIN "systems" s ON s."systemId" = t."systemId" JOIN "talkgroups" g ON g."talkgroupId" = t."talkgroupId" `+
			`ORDER BY v."id"`,
		"audio", "audioName", "audioType", "system", "talkgroup", "siteRef")

	// The datetimes: v6 wrote them as text, v8 counts them, and no dialect converts one to the other the
	// same way, so the audit converts them itself and names the unit it compared.
	assertCarriedTimes(t, db, "rdioScannerLogs", "dateTime", "timestamp", "milliseconds", time.Millisecond,
		`SELECT v."_id", v."dateTime", t."timestamp" FROM "auditrdioScannerLogs" v JOIN "logs" t ON t."logId" = v."_id" `+
			`WHERE v."message" != '' ORDER BY v."_id"`)

	assertCarriedTimes(t, db, "rdioScannerCalls", "dateTime", "timestamp", "milliseconds", time.Millisecond,
		`SELECT v."id", v."dateTime", t."timestamp" FROM "auditrdioScannerCalls" v JOIN "calls" t ON t."callId" = v."id" ORDER BY v."id"`)

	assertCarriedTimes(t, db, "rdioScannerAccesses", "expiration", "expiration", "seconds", time.Second,
		`SELECT v."_id", v."expiration", t."expiration" FROM "auditrdioScannerAccesses" v JOIN "accesses" t ON t."accessId" = v."_id" `+
			`WHERE v."expiration" IS NOT NULL ORDER BY v."_id"`)

	// The JSON lists of a call arrive as the three child tables, and the archived copy of a call holds the
	// same lists for the rows the pass could not carry.
	assertCarriedJSONLists(t, db)
}

// assertOrderedByLabel is the groups and tags rule: the v8 `order` is 1..n in label order, which is what
// the legacy migration computed and what the administrator's list shows.
func assertOrderedByLabel(t *testing.T, db *sql.DB, v6Table, v6Identity, v8Table, v8Identity, column string) {
	t.Helper()

	rows := readRows(t, db, `SELECT "`+v8Identity+`", "`+column+`", "order" FROM "`+v8Table+`" ORDER BY "order"`)

	if len(rows) == 0 {
		t.Fatalf("the %s table is empty, so the order rule proves nothing", v8Table)
	}

	last := ""

	for i, row := range rows {
		if want := strconv.Itoa(i + 1); row[2] != want {
			t.Errorf("%s row %s has order %q, want %q: the pass orders these by label, from 1", v8Table, row[0], row[2], want)
		}

		if i > 0 && row[1] < last {
			t.Errorf("%s row %s is ordered after %q, which is after it in the label order the pass uses", v8Table, row[1], last)
		}

		last = row[1]
	}

	if got, want := len(rows), len(readRows(t, db, `SELECT "`+v6Identity+`" FROM "`+v6Table+`"`)); got != want {
		t.Errorf("%s holds %d rows, want the %d the v6 table has", v8Table, got, want)
	}
}

// assertCarriedTimes compares the datetime text a v6 row holds with the integer the v8 table holds for it,
// in the unit the data model's rule names. v6 wrote these as text in the layout Go's time package prints;
// no dialect converts that text to an epoch count the same way, so the audit converts it here.
func assertCarriedTimes(t *testing.T, db *sql.DB, table, v6Column, v8Column, unitName string, unit time.Duration, query string) {
	t.Helper()

	rows := readRows(t, db, query)

	if len(rows) == 0 {
		t.Fatalf("the %s %s comparison read no rows, so it proves nothing: %s", table, v6Column, query)
	}

	for _, row := range rows {
		when, err := parseV6DateTime(row[1])
		if err != nil {
			t.Fatalf("%s row %s: %q is %q, which is not a datetime a v6 database writes: %v",
				table, row[0], v6Column, row[1], err)
		}

		want := strconv.FormatInt(when.UnixNano()/int64(unit), 10)

		if row[2] != want {
			t.Errorf("%s row %s: %q is %q in the v6 table and %q in the v8 table, want %q counted in %s",
				table, row[0], v6Column, row[1], row[2], want, unitName)
		}
	}
}

// assertCarriedJSONLists is the calls rule the child tables hold: the frequencies, patches, and source
// units a v6 call kept in three JSON columns arrive as rows of callFrequencies, callPatches, and
// callUnits, and the counts match what the JSON held.
func assertCarriedJSONLists(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, row := range readRows(t, db, `SELECT "id", "system", "frequencies", "frequency", "patches", "source", "sources" FROM "auditrdioScannerCalls" ORDER BY "id"`) {
		callId, system, frequencies, frequency, patches, source, sources := row[0], row[1], row[2], row[3], row[4], row[5], row[6]

		if !hasString(auditIdentitiesWhere(t, db, "calls", "callId", `"callId" = `+callId), callId) {
			continue
		}

		// The frequencies list a call wrote arrives as one callFrequencies row for each entry of it — an
		// empty list arrives as none. A call that wrote no list at all arrives with the one row for the
		// frequency the call itself names, which is what the legacy migration wrote for the calls that
		// only had a frequency.
		want := len(jsonNames(t, frequencies))

		if frequencies == "" && isNumber(frequency) && frequency != "0" {
			want = 1
		}

		if got := len(auditIdentitiesWhere(t, db, "callFrequencies", "callId", `"callId" = `+callId)); got != want {
			t.Errorf("call %s holds %q and frequency %q, which the pass reads as %d frequencies, and callFrequencies holds %d rows for it",
				callId, frequencies, frequency, want, got)
		}

		// A patch names a talkgroup of the system the call belongs to. A number that system has no row
		// for is dropped, which is what the legacy lookup left and what FEAT-003 pinned.
		want = 0

		for _, ref := range jsonNames(t, patches) {
			if resolves(t, db, system, ref) {
				want++
			}
		}

		if got := len(auditIdentitiesWhere(t, db, "callPatches", "callId", `"callId" = `+callId)); got != want {
			t.Errorf("call %s holds %q, of which %d name a talkgroup of system %s, and callPatches holds %d rows for it",
				callId, patches, want, system, got)
		}

		// The source units a call wrote arrive as one callUnits row each. A call that wrote no list
		// arrives with the one row for the unit number the call itself names — and with a unit created
		// for that number when no unit holds it, which is the FEAT-004 repair.
		want = len(sourceUnits(t, sources))

		if (sources == "" || sources == "[]") && isNumber(source) && source != "0" {
			want = 1
		}

		if got := len(auditIdentitiesWhere(t, db, "callUnits", "callId", `"callId" = `+callId)); got != want {
			t.Errorf("call %s holds %q and source %q, which the pass reads as %d source units, and callUnits holds %d rows for it",
				callId, sources, source, want, got)
		}
	}
}

// isNumber reports whether a value read out of the database is a number the pass can use, rather than the
// NULL a v6 row left in that column.
func isNumber(text string) bool {
	_, err := strconv.ParseInt(text, 10, 64)

	return err == nil
}

// sourceUnits counts the entries of a call's source list that name a unit number. An entry with no number
// is skipped, which is what the legacy migration did.
func sourceUnits(t *testing.T, raw string) []string {
	t.Helper()

	if raw == "" || raw == "[]" {
		return nil
	}

	var entries []struct {
		Src float64 `json:"src"`
	}

	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		t.Fatalf("%q is not the source list a v6 call holds: %v", raw, err)
	}

	units := []string{}

	for _, entry := range entries {
		if entry.Src > 0 {
			units = append(units, strconv.FormatFloat(entry.Src, 'f', -1, 64))
		}
	}

	return units
}

// assertDirwatchReferences is the dirwatch reference rule: the system and the talkgroup a v6 dirwatch
// names arrive as the v8 rows they name, and a number the legacy lookup had no row for arrives as no
// reference at all.
//
// That last branch is what the legacy pass wrote and what FEAT-003 pinned: the v6 number it replaced is
// not kept anywhere, which is recorded as a residual risk in
// docs/specs/FEAT-011-v663-migration-integrity/spec.md. The audit proves the branch only ever happens for
// a reference the deployment really had no row for.
func assertDirwatchReferences(t *testing.T, db *sql.DB) {
	t.Helper()

	rows := readRows(t, db, `SELECT v."_id", COALESCE(v."systemId", 0), COALESCE(s."systemRef", 0), `+
		`COALESCE(v."talkgroupId", 0), COALESCE(g."talkgroupRef", 0) `+
		`FROM "auditrdioScannerDirWatches" v JOIN "dirwatches" t ON t."dirwatchId" = v."_id" `+
		`LEFT JOIN "systems" s ON s."systemId" = t."systemId" `+
		`LEFT JOIN "talkgroups" g ON g."talkgroupId" = t."talkgroupId" ORDER BY v."_id"`)

	if len(rows) == 0 {
		t.Fatal("the dirwatch reference comparison read no rows, so it proves nothing")
	}

	for _, row := range rows {
		dirwatchId, system, arrivedSystem, talkgroup, arrivedTalkgroup := row[0], row[1], row[2], row[3], row[4]

		if isNumber(system) && system != "0" && arrivedSystem != "0" && system != arrivedSystem {
			t.Errorf("rdioScannerDirWatches row %s names system %s, and the v8 dirwatch row arrived naming system %s",
				dirwatchId, system, arrivedSystem)
		}

		if isNumber(talkgroup) && talkgroup != "0" && arrivedTalkgroup != "0" && talkgroup != arrivedTalkgroup {
			t.Errorf("rdioScannerDirWatches row %s names talkgroup %s, and the v8 dirwatch row arrived naming talkgroup %s",
				dirwatchId, talkgroup, arrivedTalkgroup)
		}

		if arrivedTalkgroup == "0" && isNumber(talkgroup) && talkgroup != "0" && resolves(t, db, system, talkgroup) {
			t.Errorf("rdioScannerDirWatches row %s names talkgroup %s of system %s, which the migrated database has, "+
				"and the v8 dirwatch row arrived naming no talkgroup at all", dirwatchId, talkgroup, system)
		}
	}
}

// resolves reports whether a v6 system number and talkgroup number name a talkgroup of that system in the
// migrated database, which is the lookup the dirwatch and call migrations make.
func resolves(t *testing.T, db *sql.DB, systemRef, talkgroupRef string) bool {
	t.Helper()

	query := `SELECT COUNT(*) FROM "talkgroups" t JOIN "systems" s ON s."systemId" = t."systemId" ` +
		`WHERE s."systemRef" = ` + systemRef + ` AND t."talkgroupRef" = ` + talkgroupRef

	return readRows(t, db, query)[0][0] != "0"
}

// jsonNames counts the entries of one of a call's JSON list columns. An empty string is a v6 deployment
// that wrote nothing there, and counts as no entries.
func jsonNames(t *testing.T, raw string) []string {
	t.Helper()

	if raw == "" {
		return nil
	}

	var entries []json.RawMessage

	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		t.Fatalf("%q is not the JSON list a v6 call holds: %v", raw, err)
	}

	names := []string{}

	for _, entry := range entries {
		names = append(names, string(entry))
	}

	return names
}

// TestTheMigrationArchivesTheAudioOfTheCallsItCouldNotCarry is the recoverable half of the archive rule:
// an archived call holds its audio base64-encoded, because a call the pass could not carry is still a call
// somebody recorded, and the audio is the only part of it that cannot be typed back in.
func TestTheMigrationArchivesTheAudioOfTheCallsItCouldNotCarry(t *testing.T) {
	db := openLegacyV6Database(t)

	auditShadow(t, db)

	migrateDatabase(t, db)

	auditArchivedCallAudio(t, db)
}

// auditArchivedCallAudio is the recoverable half of the archive rule on a migrated database that already
// holds its shadow copies.
func auditArchivedCallAudio(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, row := range readRows(t, db, `SELECT "id", "audio" FROM "auditrdioScannerCalls" ORDER BY "id"`) {
		callId, audio := row[0], row[1]

		archived := archivedRow(t, db, "rdioScannerCalls", mustAtoi(t, callId))

		if archived == nil {
			continue
		}

		var contents struct {
			Audio string `json:"audio"`
		}

		if err := json.Unmarshal([]byte(archived[3]), &contents); err != nil {
			t.Fatalf("call %s is archived with contents that are not JSON: %v", callId, err)
		}

		got, err := base64.StdEncoding.DecodeString(contents.Audio)
		if err != nil {
			t.Fatalf("call %s is archived with audio that is not base64: %v", callId, err)
		}

		if string(got) != audio {
			t.Errorf("call %s is archived with %d bytes of audio, want the %d bytes the v6 table holds",
				callId, len(got), len(audio))
		}
	}
}

// TestTheMigrationLeavesNoV8RowPointingAtNothing is the reference-integrity half of REQ-006: a row that
// arrives pointing at a system, talkgroup, tag, group, or call that is not there is a row the operator
// cannot open, and a migration that produced one would look like a success.
func TestTheMigrationLeavesNoV8RowPointingAtNothing(t *testing.T) {
	db := openLegacyV6Database(t)

	migrateDatabase(t, db)

	auditReferences(t, db)
}

// auditReferences is the reference-integrity half of REQ-006 on a migrated database. It reads the migrated
// tables only, so it is the one half of the audit that needs no shadow copies.
func auditReferences(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, check := range []struct {
		says, query string
	}{
		{"a talkgroup names a system that is not there",
			`SELECT "talkgroupId", "systemId" FROM "talkgroups" WHERE "systemId" NOT IN (SELECT "systemId" FROM "systems")`},

		{"a talkgroup names a tag that is not there",
			`SELECT "talkgroupId", "tagId" FROM "talkgroups" WHERE "tagId" NOT IN (SELECT "tagId" FROM "tags")`},

		{"a unit names a system that is not there",
			`SELECT "unitId", "systemId" FROM "units" WHERE "systemId" NOT IN (SELECT "systemId" FROM "systems")`},

		{"a call names a system that is not there",
			`SELECT "callId", "systemId" FROM "calls" WHERE "systemId" NOT IN (SELECT "systemId" FROM "systems")`},

		{"a call names a talkgroup that is not there",
			`SELECT "callId", "talkgroupId" FROM "calls" WHERE "talkgroupId" NOT IN (SELECT "talkgroupId" FROM "talkgroups")`},

		{"a call's frequency row names a call that is not there",
			`SELECT "callFrequencyId", "callId" FROM "callFrequencies" WHERE "callId" NOT IN (SELECT "callId" FROM "calls")`},

		{"a call's patch row names a call that is not there",
			`SELECT "callPatchId", "callId" FROM "callPatches" WHERE "callId" NOT IN (SELECT "callId" FROM "calls")`},

		{"a call's patch row names a talkgroup that is not there",
			`SELECT "callPatchId", "talkgroupId" FROM "callPatches" WHERE "talkgroupId" NOT IN (SELECT "talkgroupId" FROM "talkgroups")`},

		{"a call's unit row names a call that is not there",
			`SELECT "callUnitId", "callId" FROM "callUnits" WHERE "callId" NOT IN (SELECT "callId" FROM "calls")`},

		{"a talkgroup group link names a group that is not there",
			`SELECT "talkgroupGroupId", "groupId" FROM "talkgroupGroups" WHERE "groupId" NOT IN (SELECT "groupId" FROM "groups")`},

		{"a talkgroup group link names a talkgroup that is not there",
			`SELECT "talkgroupGroupId", "talkgroupId" FROM "talkgroupGroups" WHERE "talkgroupId" NOT IN (SELECT "talkgroupId" FROM "talkgroups")`},
	} {
		if rows := readRows(t, db, check.query); len(rows) > 0 {
			t.Errorf("%s: %v", check.says, rows)
		}
	}
}

// TestTheMigrationDropsWhatTheV8SchemaHasNoRoomFor is the declared-exception half of REQ-006: the two
// things the pass does remove, and does not archive, because the v8 schema has no place for them and they
// are not deployment data.
//
// `rdioScannerMeta` is v6's own migration bookkeeping. `usePolling` is a v6 dirwatch column v7 removed
// with the polling feature: the dirwatch row itself is carried, and the one value of it v8 cannot hold is
// said to be dropped here rather than left to be discovered.
func TestTheMigrationDropsWhatTheV8SchemaHasNoRoomFor(t *testing.T) {
	db := openLegacyV6Database(t)

	auditShadow(t, db)

	migrateDatabase(t, db)

	auditDeclaredDrops(t, db, DialectSqlite)
}

// hasTable reports whether a database holds a table, read out of the catalog that backend itself offers.
// sqlite, MariaDB, and PostgreSQL each list their tables out of a view of their own, and an audit that
// asks one backend in the way another one answers reports a table missing because of the question.
func hasTable(t *testing.T, db *sql.DB, dialect Dialect, table string) bool {
	t.Helper()

	return hasString(tableNames(t, db, dialect), table)
}

// tableNames lists the tables a database holds, in name order.
func tableNames(t *testing.T, db *sql.DB, dialect Dialect) []string {
	t.Helper()

	switch dialect {
	case DialectSqlite:
		return readTableNames(t, db)

	case DialectMariadb, DialectMysql:
		return flat(readRows(t, db, "SHOW TABLES"))

	case DialectPostgresql:
		return flat(readRows(t, db, `SELECT "tablename" FROM "pg_tables" WHERE "schemaname" = current_schema() ORDER BY 1`))
	}

	t.Fatalf("tableNames: %s is not a backend this audit can read its tables from", dialect)

	return nil
}

// auditDeclaredDrops is the declared-exception half of REQ-006 on a migrated database that already holds
// its shadow copies.
func auditDeclaredDrops(t *testing.T, db *sql.DB, dialect Dialect) {
	t.Helper()

	if hasTable(t, db, dialect, "rdioScannerMeta") {
		t.Error("the v6 rdioScannerMeta table survived the pass, which is v6 bookkeeping with no v8 counterpart")
	}

	if rows := readRows(t, db, `SELECT "rowId" FROM "migrationArchive" WHERE "tableName" = 'rdioScannerMeta'`); len(rows) > 0 {
		t.Errorf("the archive holds %v for rdioScannerMeta, want nothing: its rows are not deployment data", rows)
	}

	if got := len(readRows(t, db, `SELECT "name" FROM "auditrdioScannerMeta"`)); got == 0 {
		t.Fatal("the fixture has no rdioScannerMeta rows, so dropping it proves nothing")
	}

	// The dirwatch rows are carried, and the polling flag is the one value of them v8 has no column for.
	if got, want := len(auditIdentities(t, db, "dirwatches", "dirwatchId")), len(auditIdentities(t, db, "auditrdioScannerDirWatches", "_id")); got != want {
		t.Errorf("the v8 dirwatches table holds %d rows, want the %d the v6 table has: usePolling is dropped, the row is not",
			got, want)
	}

	if rows := readRows(t, db, `SELECT "rowId", "contents" FROM "migrationArchive" WHERE "tableName" = 'rdioScannerDirWatches' AND "contents" LIKE '%usePolling%'`); len(rows) > 0 {
		t.Errorf("the archive holds %v for dirwatches with a usePolling value in it, want nothing: v8 has no column to put it back in",
			rows)
	}
}

// mustAtoi reads an identity the audit got out of the database back into the number the archive looks a
// row up by.
func mustAtoi(t *testing.T, text string) int {
	t.Helper()

	n, err := strconv.Atoi(text)
	if err != nil {
		t.Fatalf("%q is not the identity the archive holds rows by: %v", text, err)
	}

	return n
}
