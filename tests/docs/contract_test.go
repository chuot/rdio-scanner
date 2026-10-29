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

package docs

import (
	"encoding/json"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/presentation/common/dto"
	mcpbackend "rdio-scanner/internal/presentation/mcp/backend"
)

// These checks hold the published contract to the server that answers it. The shapes come
// from the server's own types, so what a client is told the server mounts, sends, and
// accepts is what the server actually mounts, sends, and accepts.

// contractPaths reads the endpoints the published contract declares.
func contractPaths(t *testing.T) map[string]json.RawMessage {
	t.Helper()

	var contract struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal([]byte(repoFile(t, "docs", "openapi.json")), &contract); err != nil {
		t.Fatalf("reading the OpenAPI contract: %v", err)
	}
	return contract.Paths
}

// contractSchema reads one schema of the published contract as the properties it declares
// with the JSON type each declares for them.
func contractSchema(t *testing.T, name string) map[string]string {
	t.Helper()

	var contract struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Type json.RawMessage `json:"type"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(repoFile(t, "docs", "openapi.json")), &contract); err != nil {
		t.Fatalf("reading the OpenAPI contract: %v", err)
	}

	schema, ok := contract.Components.Schemas[name]
	if !ok {
		t.Fatalf("the published contract carries no schema named %q", name)
	}

	properties := make(map[string]string, len(schema.Properties))
	for property, declaration := range schema.Properties {
		properties[property] = declaredType(declaration.Type)
	}
	return properties
}

// declaredType names the JSON type one property declaration declares. A declaration may
// name several types at once, which reads here as the names joined in the order they are
// written.
func declaredType(raw json.RawMessage) string {
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return one
	}

	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		return strings.Join(many, "|")
	}

	return ""
}

// jsonKeysOf reads the JSON keys one Go type marshals as, straight from its field tags. An
// unexported field's type is still readable here, which is how the log page's criteria are
// compared against the contract without exporting them.
func jsonKeysOf(t *testing.T, model any) []string {
	t.Helper()
	return jsonKeysOfField(t, reflect.TypeOf(model))
}

func jsonKeysOfField(t *testing.T, fieldType reflect.Type) []string {
	t.Helper()

	for fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	if fieldType.Kind() != reflect.Struct {
		return nil
	}

	keys := make([]string, 0, fieldType.NumField())
	for i := 0; i < fieldType.NumField(); i++ {
		field := fieldType.Field(i)
		if field.PkgPath != "" {
			continue
		}

		key := strings.Split(field.Tag.Get("json"), ",")[0]
		if key == "" || key == "-" {
			continue
		}
		keys = append(keys, key)
	}

	sort.Strings(keys)
	return keys
}

// TestTheContractDeclaresTheModelContextProtocolSurface proves the contract names the
// surface the server mounts beside its router, which no router table lists.
func TestTheContractDeclaresTheModelContextProtocolSurface(t *testing.T) {
	if _, ok := contractPaths(t)[mcpbackend.EndpointPath]; !ok {
		t.Errorf("the server answers the model context protocol surface at %q, which the published contract does not declare",
			mcpbackend.EndpointPath)
	}
}

// TestTheContractDeclaresEveryLogCriterionTheServerReads proves the criteria a log client
// is told it may send are the criteria the server reads and echoes back.
func TestTheContractDeclaresEveryLogCriterionTheServerReads(t *testing.T) {
	declared := contractSchema(t, "LogsQueryRequest")

	criteria, ok := reflect.TypeOf(dto.AdminLogsPagePayload{}).FieldByName("Options")
	if !ok {
		t.Fatalf("the server's log page carries no criteria to compare the contract against")
	}

	for _, key := range jsonKeysOfField(t, criteria.Type) {
		if _, ok := declared[key]; !ok {
			t.Errorf("the server reads and echoes the log criterion %q, which the published contract does not declare", key)
		}
	}
}

// TestTheContractDeclaresEveryPartOfTheLogPageTheServerWrites proves the log page a client
// is told to expect carries everything the server writes into it.
func TestTheContractDeclaresEveryPartOfTheLogPageTheServerWrites(t *testing.T) {
	declared := contractSchema(t, "LogsQueryResponse")

	for _, key := range jsonKeysOf(t, dto.AdminLogsPagePayload{}) {
		if _, ok := declared[key]; !ok {
			t.Errorf("the server writes the log page field %q, which the published contract does not declare", key)
		}
	}
}

// TestTheContractDeclaresEveryFieldOfTheLogEntryTheServerWrites proves one server log entry
// as a client is told it looks carries every field the server writes for it.
func TestTheContractDeclaresEveryFieldOfTheLogEntryTheServerWrites(t *testing.T) {
	declared := contractSchema(t, "Log")

	for _, key := range jsonKeysOf(t, dto.AdminLogEntry{}) {
		if _, ok := declared[key]; !ok {
			t.Errorf("the server writes the log entry field %q, which the published contract does not declare", key)
		}
	}
}

// TestTheContractTypesTheLogLevelAsTheServerWritesIt proves the contract types the level of
// a server log entry the way the server writes it.
func TestTheContractTypesTheLogLevelAsTheServerWritesIt(t *testing.T) {
	declared := contractSchema(t, "Log")

	field, ok := reflect.TypeOf(dto.AdminLogEntry{}).FieldByName("Level")
	if !ok {
		t.Fatalf("the server's log entry carries no level to compare the contract against")
	}
	if field.Type.Kind() != reflect.String {
		t.Fatalf("the server's log level is a %s, not the string kind the log entry has always carried", field.Type.Kind())
	}

	if declared["level"] != "string" {
		t.Errorf("the server writes the level of a log entry as a string, while the published contract declares it as %q",
			declared["level"])
	}
}

// TestTheContractDeclaresOnlyOptionsTheServerShows proves every options key a client is
// told it may send is a key the server answers with, so no configuration client is told to
// send a key the server drops.
func TestTheContractDeclaresOnlyOptionsTheServerShows(t *testing.T) {
	declared := contractSchema(t, "Options")

	body, err := json.Marshal(dto.AdminConfigPayload{
		Snapshot: ports.ConfigSnapshot{Options: domain.DefaultOptions()},
		Version:  "8.0.0-test",
	})
	if err != nil {
		t.Fatalf("marshalling the server's own configuration payload: %v", err)
	}

	shown := map[string]any{}
	if err := json.Unmarshal(body, &shown); err != nil {
		t.Fatalf("reading back the server's own configuration payload: %v", err)
	}

	options, ok := shown["options"].(map[string]any)
	if !ok {
		t.Fatalf("the server's own configuration payload answers no options object")
	}

	for key := range declared {
		if _, ok := options[key]; !ok {
			t.Errorf("the published contract declares the options key %q, which the server never answers with", key)
		}
	}
}

// TestTheContractDeclaresEveryEntryKeyTheAdministratorAnswersWrite holds the entry
// schemas of the published contract to the entries the administrator endpoints actually
// write. Each fixture fills every field of its type, so every key the server's own
// payload code can write appears, and each written key has to be a property the schema
// for that entry declares. A key the answers write without a declaration is a key no
// client written from the contract knows to read.
func TestTheContractDeclaresEveryEntryKeyTheAdministratorAnswersWrite(t *testing.T) {
	fullyPopulatedAccess := domain.Access{
		ID:         1,
		Code:       "harbour-code",
		Expiration: 1_800_000_000,
		Ident:      "harbour",
		Limit:      2,
		Order:      1,
		Scope:      domain.AccessScope{Present: true, Wildcard: true},
	}
	fullyPopulatedSite := domain.Site{ID: 1, Label: "downtown", Order: 1, SiteRef: 100, SystemID: 1}
	fullyPopulatedTalkgroup := domain.Talkgroup{
		ID:           1,
		Alert:        "emergency",
		Delay:        1000,
		Frequency:    154_000_000,
		GroupIDs:     []uint64{1},
		Kind:         "radio",
		Label:        "dispatch",
		LED:          "red",
		Name:         "dispatch main",
		Order:        1,
		TagID:        1,
		TalkgroupRef: 101,
	}
	fullyPopulatedUnit := domain.Unit{ID: 1, Label: "car 1", Order: 1, SystemID: 1, UnitRef: 1001, UnitFrom: 1001, UnitTo: 1010}
	fullyPopulatedDirwatch := domain.Dirwatch{
		ID:          1,
		DeleteAfter: true,
		Delay:       1000,
		Directory:   "/var/spool/recordings",
		Disabled:    false,
		Extension:   "mp3",
		Frequency:   154_000_000,
		Kind:        "radio",
		Mask:        "*.mp3",
		Order:       1,
		SiteID:      1,
		SystemID:    1,
		TalkgroupID: 1,
	}
	fullyPopulatedDownstream := domain.Downstream{
		ID:       1,
		Apikey:   "upload-key",
		Disabled: false,
		Order:    1,
		Scope:    domain.AccessScope{Present: true, Wildcard: true},
		URL:      "https://other.example/api/trunk-recorder-call-upload",
	}

	answers := []struct {
		schema  string
		payload any
	}{
		{"Access", dto.AccessListPayload([]domain.Access{fullyPopulatedAccess})},
		{"Apikey", dto.ApikeyListPayload([]domain.Apikey{{ID: 1, Ident: "recorder", Key: "upload-key", Order: 1, Scope: domain.AccessScope{Present: true, Wildcard: true}}})},
		{"Dirwatch", dto.DirwatchListPayload([]domain.Dirwatch{fullyPopulatedDirwatch})},
		{"Downstream", dto.DownstreamListPayload([]domain.Downstream{fullyPopulatedDownstream})},
		{"Group", dto.GroupListPayload([]domain.Group{{ID: 1, Alert: "emergency", Label: "proc", LED: "red", Order: 1}})},
		{"Tag", dto.TagListPayload([]domain.Tag{{ID: 1, Alert: "emergency", Label: "priority", LED: "red", Order: 1}})},
		{"System", dto.SystemListPayload([]domain.System{{
			ID: 1, Alert: "emergency", AutoPopulate: true, Delay: 1000, Kind: "type25",
			Label: "county", LED: "red", Order: 1, SystemRef: 7001,
			Sites:        domain.Sites{fullyPopulatedSite},
			Talkgroups:   domain.Talkgroups{fullyPopulatedTalkgroup},
			Units:        domain.Units{fullyPopulatedUnit},
		}})},
		{"Site", dto.SiteListPayload(domain.Sites{fullyPopulatedSite})},
		{"Talkgroup", dto.TalkgroupListPayload(domain.Talkgroups{fullyPopulatedTalkgroup})},
		{"Unit", dto.UnitListPayload(domain.Units{fullyPopulatedUnit})},
	}

	for _, answer := range answers {
		body, err := json.Marshal(answer.payload)
		if err != nil {
			t.Fatalf("marshalling the server's own %s answer: %v", answer.schema, err)
		}

		entries := []map[string]any{}
		if err := json.Unmarshal(body, &entries); err != nil {
			t.Fatalf("reading back the server's own %s answer: %v", answer.schema, err)
		}
		if len(entries) != 1 {
			t.Fatalf("the server's own %s answer writes %d entries, want 1", answer.schema, len(entries))
		}

		declared := contractSchema(t, answer.schema)
		for _, key := range sortedKeys(entries[0]) {
			if _, ok := declared[key]; !ok {
				t.Errorf("the administrator answer for a %s writes the key %q, which the published contract never declares",
					strings.ToLower(answer.schema), key)
			}
		}
	}
}

// sortedKeys names one entry's keys in a stable order, so a failure lists them the
// same way every run.
func sortedKeys(entry map[string]any) []string {
	keys := make([]string, 0, len(entry))
	for key := range entry {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// deadlineAnswerCodes reads the failure answers the composition root's own deadline
// middleware writes, so the comparison below asks the server what it answers rather than
// answering for it.
func deadlineAnswerCodes(t *testing.T) []string {
	t.Helper()

	// Every route the server answers is mounted under one handler, and that handler is
	// wrapped once in the composition root: the read below confirms the wrap still covers
	// the whole route set before it compares anything.
	wiring := repoFile(t, "internal", "app", "wiring.go")
	if !strings.Contains(wiring, `tree.Handle("/", withRequestDeadline(`) {
		t.Fatalf("the composition root no longer bounds the whole route set in one place, " +
			"and this check no longer knows which routes the deadline reaches")
	}

	writes := regexp.MustCompile(`WriteHeader\((http\.Status[A-Za-z0-9]+)\)`)

	// The names the middleware writes mapped to the numbers a client sees. A name the
	// middleware starts writing without a number here fails this read, because a new
	// failure the server can answer on a handler's behalf has to be declared as well.
	numbers := map[string]string{
		"http.StatusServiceUnavailable": "503",
	}

	found := map[string]bool{}

	for _, match := range writes.FindAllStringSubmatch(wiring, -1) {
		code, ok := numbers[match[1]]
		if !ok {
			t.Fatalf("the composition root writes %s, which this check has no number for", match[1])
		}

		found[code] = true
	}

	if len(found) == 0 {
		t.Fatalf("no failure answer found in the composition root's deadline middleware")
	}

	codes := make([]string, 0, len(found))
	for code := range found {
		codes = append(codes, code)
	}

	sort.Strings(codes)

	return codes
}

// contractOperations reads the answers each published operation declares, keyed by
// "METHOD /path", so the check below can ask whether one operation's answer is missing.
func contractOperations(t *testing.T) map[string]map[string]json.RawMessage {
	t.Helper()

	operations := map[string]map[string]json.RawMessage{}

	for path, item := range contractPaths(t) {
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(item, &entries); err != nil {
			t.Fatalf("reading the contract's entry for %s: %v", path, err)
		}

		for method, entry := range entries {
			if !httpMethods[method] {
				continue
			}

			var operation struct {
				Responses map[string]json.RawMessage `json:"responses"`
			}
			if err := json.Unmarshal(entry, &operation); err != nil {
				t.Fatalf("reading the contract's %s answer for %s: %v", method, path, err)
			}

			if operation.Responses == nil {
				t.Fatalf("the contract's %s %s declares no answers at all", strings.ToUpper(method), path)
			}

			operations[strings.ToUpper(method)+" "+path] = operation.Responses
		}
	}

	return operations
}

// TestTheContractDeclaresTheDeadlineAnswerEveryOperationCanGet proves every operation a
// client is told about declares the answer the server's own request deadline writes when
// the deadline passes before the handler has answered, so no client is surprised by a
// failure the composition root can answer on its behalf.
func TestTheContractDeclaresTheDeadlineAnswerEveryOperationCanGet(t *testing.T) {
	codes := deadlineAnswerCodes(t)
	operations := contractOperations(t)

	if len(operations) == 0 {
		t.Fatalf("the published contract declares no operations to compare the deadline answer against")
	}

	names := make([]string, 0, len(operations))
	for name := range operations {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		for _, code := range codes {
			if _, ok := operations[name][code]; !ok {
				t.Errorf("the server answers %s on %s when its request deadline passes, which the published contract does not declare",
					code, name)
			}
		}
	}
}
