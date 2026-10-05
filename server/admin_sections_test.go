package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v4"
)

func newSectionAdmin(t *testing.T) (*Admin, string) {
	t.Helper()

	controller := newUnitIngestController(t)

	admin := NewAdmin(controller)

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{ID: "section-test"}).SignedString([]byte(controller.Options.secret))
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}

	admin.Tokens = append(admin.Tokens, token)

	return admin, token
}

func sectionRequest(t *testing.T, admin *Admin, token string, method string, path string, body string) *httptest.ResponseRecorder {
	t.Helper()

	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Authorization", token)

	rec := httptest.NewRecorder()

	admin.SectionHandler(rec, req)

	return rec
}

func TestSectionHandlerRejectsUnknownSection(t *testing.T) {
	admin, token := newSectionAdmin(t)

	rec := sectionRequest(t, admin, token, http.MethodGet, "/api/admin/config/bogus", "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown section status = %d, want 404", rec.Code)
	}
}

func TestSectionHandlerRequiresAuth(t *testing.T) {
	admin, _ := newSectionAdmin(t)

	rec := sectionRequest(t, admin, "invalid-token", http.MethodGet, "/api/admin/config/groups", "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token status = %d, want 401", rec.Code)
	}
}

func TestSectionGetReturnsOnlyThatSection(t *testing.T) {
	admin, token := newSectionAdmin(t)

	admin.Controller.Groups.List = append(admin.Controller.Groups.List, &Group{Id: 1, Label: "Dispatch"})
	admin.Controller.Tags.List = append(admin.Controller.Tags.List, &Tag{Id: 2, Label: "Law"})

	rec := sectionRequest(t, admin, token, http.MethodGet, "/api/admin/config/groups", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("get groups status = %d, want 200", rec.Code)
	}

	var groups []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &groups); err != nil {
		t.Fatalf("decode groups body: %v", err)
	}

	if len(groups) != 1 || groups[0]["label"] != "Dispatch" {
		t.Fatalf("groups body = %v", groups)
	}

	rec = sectionRequest(t, admin, token, http.MethodGet, "/api/admin/config/tags", "")

	var tags []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &tags); err != nil {
		t.Fatalf("decode tags body: %v", err)
	}

	if len(tags) != 1 || tags[0]["label"] != "Law" {
		t.Fatalf("tags body = %v", tags)
	}
}

func TestSectionPutGroupsUpdatesOnlyGroups(t *testing.T) {
	admin, token := newSectionAdmin(t)

	// the fresh test database is seeded with six default groups (ids 1..6);
	// replacing the section with a single existing id must rename it and drop
	// the others, while leaving the seeded tags untouched.
	rec := sectionRequest(t, admin, token, http.MethodPut, "/api/admin/config/groups",
		`[{"id":1,"label":"Dispatch Renamed","led":"green"}]`)

	if rec.Code != http.StatusOK {
		t.Fatalf("put groups status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	var groups []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &groups); err != nil {
		t.Fatalf("decode groups response: %v", err)
	}

	if len(groups) != 1 || groups[0]["label"] != "Dispatch Renamed" {
		t.Fatalf("groups response = %v", groups)
	}

	if err := admin.Controller.Tags.Read(admin.Controller.Database); err != nil {
		t.Fatalf("reload tags: %v", err)
	}

	if len(admin.Controller.Tags.List) == 0 {
		t.Fatal("tags section was clobbered by groups put")
	}

	// persistence: a fresh read from the database must show the renamed group
	if err := admin.Controller.Groups.Read(admin.Controller.Database); err != nil {
		t.Fatalf("reload groups: %v", err)
	}

	if len(admin.Controller.Groups.List) != 1 || admin.Controller.Groups.List[0].Label != "Dispatch Renamed" {
		t.Fatalf("persisted groups = %#v", admin.Controller.Groups.List)
	}
}

func TestSectionPutOtherSectionKeepsDirwatches(t *testing.T) {
	admin, token := newSectionAdmin(t)

	// seed a dirwatch row through the dirwatch section endpoint
	rec := sectionRequest(t, admin, token, http.MethodPut, "/api/admin/config/dirwatch",
		`[{"directory":"/tmp/opencode/dirwatch-test","mask":"","delay":1000,"deleteAfter":true,"disabled":false,"type":"default"}]`)

	if rec.Code != http.StatusOK {
		t.Fatalf("put dirwatch status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	// Dirwatches.Stop clears the in-memory list on every section save; a save
	// of an unrelated section must reload the list from the database before
	// restarting the watchers, otherwise later dirwatch GET/PUT cycles delete
	// rows that are still persisted.
	rec = sectionRequest(t, admin, token, http.MethodPut, "/api/admin/config/groups",
		`[{"id":1,"label":"Dispatch Renamed","led":"green"}]`)

	if rec.Code != http.StatusOK {
		t.Fatalf("put groups status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	if len(admin.Controller.Dirwatches.List) != 1 {
		t.Fatalf("in-memory dirwatches after unrelated section put = %#v, want one entry", admin.Controller.Dirwatches.List)
	}

	rec = sectionRequest(t, admin, token, http.MethodGet, "/api/admin/config/dirwatch", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("get dirwatch status = %d, want 200", rec.Code)
	}

	var dirwatches []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &dirwatches); err != nil {
		t.Fatalf("decode dirwatch response: %v", err)
	}

	if len(dirwatches) != 1 || dirwatches[0]["directory"] != "/tmp/opencode/dirwatch-test" {
		t.Fatalf("dirwatch response = %v", dirwatches)
	}
}

func TestSectionPutSystemsPersistsAndReturnsSection(t *testing.T) {
	admin, token := newSectionAdmin(t)

	if err := admin.Controller.Tags.Read(admin.Controller.Database); err != nil {
		t.Fatalf("load seeded tags: %v", err)
	}

	tagId := admin.Controller.Tags.List[0].Id

	rec := sectionRequest(t, admin, token, http.MethodPut, "/api/admin/config/systems",
		fmt.Sprintf(`[{"label":"Test System","systemRef":10,"talkgroups":[{"label":"TG1","talkgroupRef":101,"order":1,"tagId":%d}]}]`, tagId))

	if rec.Code != http.StatusOK {
		t.Fatalf("put systems status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	var systems []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &systems); err != nil {
		t.Fatalf("decode systems response: %v", err)
	}

	if len(systems) != 1 || systems[0]["label"] != "Test System" {
		t.Fatalf("systems response = %v", systems)
	}

	if err := admin.Controller.Systems.Read(admin.Controller.Database); err != nil {
		t.Fatalf("reload systems: %v", err)
	}

	if len(admin.Controller.Systems.List) != 1 || len(admin.Controller.Systems.List[0].Talkgroups.List) != 1 {
		t.Fatalf("persisted systems = %#v", admin.Controller.Systems.List)
	}
}

func TestSectionPutOptionsUpdatesOnlyOptions(t *testing.T) {
	admin, token := newSectionAdmin(t)

	admin.Controller.Groups.List = append(admin.Controller.Groups.List, &Group{Id: 1, Label: "Dispatch"})

	rec := sectionRequest(t, admin, token, http.MethodPut, "/api/admin/config/options",
		`{"branding":"My Scanner","maxClients":42}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("put options status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	var options map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &options); err != nil {
		t.Fatalf("decode options response: %v", err)
	}

	if options["branding"] != "My Scanner" {
		t.Fatalf("options response = %v", options)
	}

	if len(admin.Controller.Groups.List) != 1 || admin.Controller.Groups.List[0].Label != "Dispatch" {
		t.Fatalf("groups section was clobbered by options put: %#v", admin.Controller.Groups.List)
	}
}

func TestSectionPutRejectsUndecodableBody(t *testing.T) {
	admin, token := newSectionAdmin(t)

	rec := sectionRequest(t, admin, token, http.MethodPut, "/api/admin/config/groups", `{"label":"not an array"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body status = %d, want 400", rec.Code)
	}
}

func TestSectionHandlerRejectsMethodAndWebsocketUpgrade(t *testing.T) {
	admin, token := newSectionAdmin(t)

	rec := sectionRequest(t, admin, token, http.MethodDelete, "/api/admin/config/groups", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE status = %d, want 405", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/config/groups", nil)
	req.Header.Set("Upgrade", "websocket")
	rec = httptest.NewRecorder()
	admin.SectionHandler(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("websocket upgrade on section path status = %d, want 405", rec.Code)
	}
}

func TestLegacyWholeConfigEndpointStillWorks(t *testing.T) {
	admin, token := newSectionAdmin(t)

	// the exact legacy route must keep its whole-config semantics (CA-019.3)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/config", strings.NewReader(`{"groups":[{"id":1,"label":"Legacy Renamed"}]}`))
	req.Header.Set("Authorization", token)

	rec := httptest.NewRecorder()
	admin.ConfigHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("legacy put status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	if err := admin.Controller.Groups.Read(admin.Controller.Database); err != nil {
		t.Fatalf("reload groups: %v", err)
	}

	if len(admin.Controller.Groups.List) != 1 || admin.Controller.Groups.List[0].Label != "Legacy Renamed" {
		t.Fatalf("legacy put groups = %#v", admin.Controller.Groups.List)
	}
}
