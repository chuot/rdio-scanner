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
