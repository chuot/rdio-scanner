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

package upload

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
)

// The call-upload endpoints take posted recordings, answer in plain
// text, and file every refusal with the upload endpoint.

const (
	uploadSystemRef    = uint(10001)
	uploadOtherSystem  = uint(10002)
	uploadTalkgroupRef = uint(1514000)

	// uploadUnknownTalkgroupRef is a talkgroup number no row in these fixtures carries a row for, which is what an
	// uploader names when its own configuration knows a talkgroup this server has never been told about.
	uploadUnknownTalkgroupRef = uint(999999)

	uploadOpenKey     = "capture-apikey-open"
	uploadScopedKey   = "capture-apikey-scoped"
	uploadDisabledKey = "capture-apikey-disabled"
	uploadRecording   = "a recording long enough to be a recording, and then some more of it"
)

var uploadMoment = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

// uploadEndpointPaths are the two endpoints an uploader posts a call to. They answer a connection check the same
// way because they read the posted body and answer the reading through the same code.
var uploadEndpointPaths = []string{"/api/call-upload", "/api/trunk-recorder-call-upload"}

// capturedUploadAnswers are the recorded success answers, named by the endpoint they were captured from. The two
// answers are the same bytes, and a test that names the endpoint it posted at names the capture that endpoint has.
var capturedUploadAnswers = map[string]string{
	"/api/call-upload":                "rest-upload.txt",
	"/api/trunk-recorder-call-upload": "rest-trunk-recorder-upload.txt",
}

// uploadFixture is a server whose ingest runs on the in-memory stand-ins the application tests use,
// reached through the real upload endpoints.
type uploadFixture struct {
	apikeys     *fakes.Apikeys
	calls       *fakes.Calls
	systems     *fakes.Systems
	groups      *fakes.Groups
	tags        *fakes.Tags
	options     *fakes.Options
	logs        *fakes.Logs
	hub         *fakes.Hub
	delayer     *fakes.Delayer
	converter   *fakes.Converter
	prober      *fakes.Prober
	downstreams *fakes.Downstreams
	forwarder   *fakes.Forwarder
	clock       *fakes.Clock
	parser      *recordingUploadParser

	routes http.Handler
}

func newUploadFixture() *uploadFixture {
	fixture := &uploadFixture{
		apikeys: &fakes.Apikeys{Stored: domain.Apikeys{
			{ID: 1, Ident: "Capture open", Key: uploadOpenKey, Scope: domain.AccessScope{Present: true, Wildcard: true}},
			{ID: 2, Ident: "Capture scoped", Key: uploadScopedKey, Scope: domain.AccessScope{
				Present: true,
				Systems: []domain.ScopedSystem{{SystemRef: uploadSystemRef, TalkgroupsWildcard: true}},
			}},
			{ID: 3, Ident: "Capture switched off", Key: uploadDisabledKey, Disabled: true,
				Scope: domain.AccessScope{Present: true, Wildcard: true}},
		}},
		calls:       fakes.NewCalls(),
		systems:     &fakes.Systems{},
		groups:      &fakes.Groups{Stored: domain.Groups{{ID: 1, Label: "Ground"}}},
		tags:        &fakes.Tags{Stored: domain.Tags{{ID: 10, Label: "Untagged"}}},
		options:     &fakes.Options{Stored: domain.DefaultOptions()},
		logs:        &fakes.Logs{},
		hub:         fakes.NewHub(),
		delayer:     &fakes.Delayer{Now: uploadMoment},
		converter:   fakes.NewConverter(),
		prober:      fakes.NewProber(),
		downstreams: &fakes.Downstreams{},
		forwarder:   &fakes.Forwarder{},
		clock:       fakes.NewClock(uploadMoment),
		parser:      &recordingUploadParser{},
	}

	ingest := application.NewIngestCallHandler(
		fixture.calls,
		fixture.systems,
		fixture.groups,
		fixture.tags,
		fixture.options,
		fixture.logs,
		fixture.converter,
		fixture.prober,
		fixture.hub,
		fixture.delayer,
		fixture.downstreams,
		fixture.forwarder,
		fixture.clock,
	)

	mux := http.NewServeMux()

	New(Deps{
		Ingest: application.NewUploadCallHandler(fixture.apikeys, ingest),
		Check:  application.NewCheckUploadConnectionQueryHandler(fixture.apikeys, fixture.systems),
		Parser: fixture.parser,
		Logs:   fixture.logs,
		Clock:  fixture.clock,
	}).Register(mux)

	fixture.routes = mux

	return fixture
}

// seedUploadSystem installs the system and talkgroup the captured uploads name.
func (fixture *uploadFixture) seedUploadSystem() {
	fixture.systems.Stored = domain.Systems{
		{ID: 1, SystemRef: uploadSystemRef, Label: "Grand Metro", Talkgroups: domain.Talkgroups{
			{ID: 10, TalkgroupRef: uploadTalkgroupRef, Label: "Ground", Name: "Dispatch 1", TagID: 10},
		}},
	}
}

// completeCall is the call the capture's upload produced, which is what the recording parser hands back
// once the endpoint has handed it every field. The endpoints decide nothing about what a field means; that
// is the parser's job, and it is proven in the parser's own suite.
func completeCall() domain.Call {
	return callNaming(uploadSystemRef, uploadTalkgroupRef)
}

// callNaming is a call that names one system and one talkgroup, which is the least a submission has to name
// to be a call.
func callNaming(systemRef, talkgroupRef uint) domain.Call {
	call := completeRecording()

	call.Meta.SystemRef = systemRef
	call.Meta.TalkgroupRef = talkgroupRef

	return call
}

// recordingNamingNoTalkgroup is a submission that carries a recording, a moment, and a system number, and names no
// talkgroup in any form: a recording in every way but one, which the server refuses for that one thing.
func recordingNamingNoTalkgroup(systemRef uint) domain.Call {
	call := completeRecording()

	call.Meta.SystemRef = systemRef
	call.Meta.TalkgroupLabel = ""

	return call
}

// recordingCarryingTheCheckClaim is what the field reading hands back for a posted recording that also carries the
// marker a client posts when it is only asking whether the server will take an upload. The recording is what decides
// such a submission: a marker beside a recording claims nothing the recording does not already answer.
func recordingCarryingTheCheckClaim() domain.Call {
	call := completeCall()

	call.Meta.ConnectionCheck = true

	return call
}

// completeRecording is a recording with a moment and a name, which is the part of a call an upload carries
// before it names anything.
func completeRecording() domain.Call {
	call := domain.NewCall()

	call.Audio = []byte(uploadRecording)
	call.AudioFilename = "capture.wav"
	call.AudioMime = "audio/wav"
	call.Timestamp = uploadMoment
	call.Meta.TalkgroupLabel = "Ground"

	return call
}

// recordingUploadParser stands in for the ingest parser. It records what the endpoints asked it to read,
// in the order they asked, because which fields an endpoint hands to the parser and in what order is the
// endpoint's own contract.
//
// The real parser is not used here because the endpoints may not import the package that holds it; it is wired to them by the composition root, and its reading is proven by
// internal/infrastructure/dirwatch/parsers/multipart_test.go.
type recordingUploadParser struct {
	// Fields records every field that was handed to the field reading, in the order it arrived.
	Fields []string

	// FileNames records the file name each field arrived with, which is how the
	// recording's own name reaches the server.
	FileNames []string

	// Bodies records the contents of each field handed to the field reading.
	Bodies [][]byte

	// Sidecars records every sidecar the trunk-recorder endpoint handed to the sidecar reading.
	Sidecars [][]byte

	// Completed is the call the parser produces, and HasCall says whether
	// it produces one at all: a parser producing nothing is how these
	// tests build an incomplete call.
	Completed domain.Call
	HasCall   bool

	// SidecarErr, when set, is what the sidecar reading reports.
	SidecarErr error

	// Truncation, when set, is the warning the reading reports for a
	// read field: the shape a list over the server's cap earns.
	Truncation string

	// Apply, when set, replaces what the stand-in makes of a field beyond recording that it arrived.
	// A guard that reads what a posted value reaches the field reading as needs a reading that builds
	// the call out of the bytes it was handed, and not one handed back a call the test chose ahead of
	// time; every guard that names the call it wants read leaves this unset and is read as before.
	Apply func(part *multipart.Part, data []byte, call domain.Call) (domain.Call, string)
}

// produces sets the call the parser hands back for every field it is asked to read.
func (parser *recordingUploadParser) produces(call domain.Call) {
	parser.Completed = call
	parser.HasCall = true
}

func (parser *recordingUploadParser) ApplyMultipart(part *multipart.Part, data []byte, call domain.Call) (domain.Call, string) {
	parser.Fields = append(parser.Fields, part.FormName())
	parser.FileNames = append(parser.FileNames, part.FileName())
	parser.Bodies = append(parser.Bodies, data)

	if parser.Apply != nil {
		return parser.Apply(part, data, call)
	}

	if parser.HasCall {
		return parser.Completed, parser.Truncation
	}

	return call, parser.Truncation
}

func (parser *recordingUploadParser) ApplyTrunkRecorderSidecar(call *domain.Call, sidecar []byte) error {
	parser.Sidecars = append(parser.Sidecars, sidecar)

	if parser.SidecarErr != nil {
		return parser.SidecarErr
	}

	if parser.HasCall {
		*call = parser.Completed
	}

	return nil
}

// uploadField is one field of a posted call. A field with a file name is the recording; a field without
// one is a plain value.
type uploadField struct {
	name     string
	filename string
	value    string
}

// postedBody builds the multipart body and the content type a real uploader's request carries.
func postedBody(t *testing.T, fields ...uploadField) (string, string) {
	t.Helper()

	body := bytes.Buffer{}
	writer := multipart.NewWriter(&body)

	for _, field := range fields {
		var (
			part io.Writer
			err  error
		)

		if len(field.filename) > 0 {
			part, err = writer.CreateFormFile(field.name, field.filename)
		} else {
			part, err = writer.CreateFormField(field.name)
		}

		if err != nil {
			t.Fatalf("building the %s field: %v", field.name, err)
		}

		if _, err := part.Write([]byte(field.value)); err != nil {
			t.Fatalf("writing the %s field: %v", field.name, err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("closing the multipart body: %v", err)
	}

	return body.String(), writer.FormDataContentType()
}

// captureUploadFields are the fields the capture posted to /api/call-upload, in the order it posted them.
func captureUploadFields(key string) []uploadField {
	return []uploadField{
		{name: "key", value: key},
		{name: "system", value: "10001"},
		{name: "talkgroup", value: "1514000"},
		{name: "talkgroupLabel", value: "Ground"},
		{name: "talkgroupName", value: "Dispatch 1"},
		{name: "timestamp", value: "1791288000000"},
		{name: "site", value: "101"},
		{name: "frequencies", value: `[{"dbm":-70,"errorCount":1,"freq":151400000,"pos":0.5,"spikeCount":2}]`},
		{name: "patches", value: "[1514100]"},
		{name: "sources", value: `[{"pos":0.5,"src":1001,"tag":"Car 1"},{"pos":1.5,"src":1002,"tag":"Car 2"}]`},
		{name: "audio", filename: "capture.wav", value: uploadRecording},
		{name: "audioFilename", value: "capture.wav"},
		{name: "audioMime", value: "audio/wav"},
	}
}

// connectionCheckFields are the parts a capture client posts when it is only asking whether this server will take
// an upload: its credential, one system number, and its own test marker, in the order it posts them, and no
// recording part. A row that posts nothing where the credential or the system number goes is how a client that
// names neither arrives.
func connectionCheckFields(key string, systemRef string) []uploadField {
	fields := []uploadField{}

	if len(key) > 0 {
		fields = append(fields, uploadField{name: "key", value: key})
	}

	if len(systemRef) > 0 {
		fields = append(fields, uploadField{name: "system", value: systemRef})
	}

	return append(fields, uploadField{name: "test", value: "1"})
}

// recordingNamingNoTalkgroupFields are the parts an uploader posts for a recording whose talkgroup it has no number
// for: everything the capture client posts, with the talkgroup named as the digit zero and no label or name posted
// with it. That reading claims no talkgroup at all, which the reader's own suite pins.
func recordingNamingNoTalkgroupFields(key string) []uploadField {
	kept := []uploadField{}

	for _, field := range captureUploadFields(key) {
		if field.name == "talkgroupLabel" || field.name == "talkgroupName" {
			continue
		}

		if field.name == "talkgroup" {
			field.value = "0"
		}

		kept = append(kept, field)
	}

	return kept
}

// recordingWithCheckClaimFields are the parts an uploader posts for a recording that also carries the marker a client
// posts when it is only asking whether the server will take an upload.
func recordingWithCheckClaimFields(key string) []uploadField {
	return append(captureUploadFields(key), uploadField{name: "test", value: "1"})
}

// checkNamingTalkgroupFields are the parts a client posts when it names a talkgroup as well as a system, names the
// moment it is reporting, posts its own marker, and brings no recording: the shape that is short of a recording
// alone. The system and talkgroup numbers it posts are the ones checkNamingTalkgroupCall claims.
func checkNamingTalkgroupFields(key string) []uploadField {
	return []uploadField{
		{name: "key", value: key},
		{name: "system", value: "10001"},
		{name: "dateTime", value: "1791288000000"},
		{name: "talkgroup", value: "999999"},
		{name: "talkgroupLabel", value: "Unknown Dispatch"},
		{name: "test", value: "1"},
	}
}

// connectionCheckCall is what the field reading hands back for the body above: its claim that the submission is a
// connection check, the system number the submission named when it named one, and no recording. Which claim a
// posted test marker earns is the reading's own contract, proven in the parser's own suite; what these endpoints
// make of a call that carries it is proven here.
func connectionCheckCall(systemRef uint) domain.Call {
	call := domain.NewCall()

	call.Meta.ConnectionCheck = true
	call.Meta.SystemRef = systemRef

	return call
}

// checkNamingTalkgroupCall is what the field reading hands back for the body the fields helper above posts: the claim
// a client's own marker earns, one system number, one talkgroup number, the moment the submission reported, and no
// recording. It is short of a recording alone, so the reason it is refused for is its audio.
func checkNamingTalkgroupCall() domain.Call {
	call := connectionCheckCall(uploadSystemRef)

	call.Meta.TalkgroupRef = uploadUnknownTalkgroupRef
	call.Timestamp = uploadMoment

	return call
}

func (fixture *uploadFixture) post(path, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)

	rec := httptest.NewRecorder()

	fixture.routes.ServeHTTP(rec, req)

	return rec
}

func (fixture *uploadFixture) postCaptureUpload(t *testing.T, key string) *httptest.ResponseRecorder {
	body, contentType := postedBody(t, captureUploadFields(key)...)

	return fixture.post("/api/call-upload", contentType, body)
}

// loggedContaining reports whether the server wrote a log entry mentioning the given text. The upload
// endpoints write every failure they report to the uploader into the log view as well, with the prefix the
// legacy server has always used.
func (fixture *uploadFixture) loggedContaining(fragment string) bool {
	for _, entry := range fixture.logs.Stored {
		if strings.Contains(entry.Message, fragment) {
			return true
		}
	}

	return false
}

// loggedAs reports whether the upload endpoint filed a refusal with itself. Every one of these refusals is
// about an upload, and an operator chasing a broken uploader filters the log view by the upload endpoint —
// which only works if the entry says so.
func (fixture *uploadFixture) loggedAs(source domain.LogSource, fragment string) bool {
	for _, entry := range fixture.logs.Stored {
		if entry.Source == source && strings.Contains(entry.Message, fragment) {
			return true
		}
	}

	return false
}

// uploadEntries are the entries the server filed with the upload endpoint, in the order it wrote them. A test
// reads a whole message and the level it was filed at here, rather than a fragment of one: the sentence an operator
// searches the log view for is the whole entry.
func (fixture *uploadFixture) uploadEntries() []domain.LogEntry {
	entries := []domain.LogEntry{}

	for _, entry := range fixture.logs.Stored {
		if entry.Source == domain.LogSourceUpload {
			entries = append(entries, entry)
		}
	}

	return entries
}

// entriesNamingACheck are the entries that name a connection check. A submission refused for its credential never
// reaches the question of what it was short of, so the answer it would have been given for that appears nowhere.
func (fixture *uploadFixture) entriesNamingACheck() []domain.LogEntry {
	entries := []domain.LogEntry{}

	for _, entry := range fixture.uploadEntries() {
		if strings.Contains(entry.Message, "connection check") {
			entries = append(entries, entry)
		}
	}

	return entries
}

// entriesFiledUnder are the entries one part of the server filed, in the order it wrote them. A submission that
// never reached the ingest use case leaves nothing under that use case, and nothing under the configuration the
// ingest use case changes when it auto-populates what an upload names.
func (fixture *uploadFixture) entriesFiledUnder(source domain.LogSource) []domain.LogEntry {
	entries := []domain.LogEntry{}

	for _, entry := range fixture.logs.Stored {
		if entry.Source == source {
			entries = append(entries, entry)
		}
	}

	return entries
}

// assertFiledAt insists that the upload endpoint filed exactly one entry, at the given level, whose message is
// exactly the given sentence. These endpoints file one entry per refusal they answer, so more than one entry means
// one answer wrote two, and a stored message that is not the whole sentence is one an operator cannot search for.
func assertFiledAt(t *testing.T, label string, entries []domain.LogEntry, level string, want string, why string) {
	t.Helper()

	if len(entries) != 1 {
		t.Errorf("%s filed %d entries with the upload endpoint, want the one entry this answer leaves: %v",
			label, len(entries), entries)

		return
	}

	if entries[0].Level != level {
		t.Errorf("%s entry is filed at %q, want %q: %s", label, entries[0].Level, level, why)
	}

	if entries[0].Message != want {
		t.Errorf("%s entry =\n  %q\nwant\n  %q: %s", label, entries[0].Message, want, why)
	}
}

// TestCallUploadEndpointAnswersTheCapturedUpload proves an accepted
// upload gets the captured legacy answer, credential kept out.
func TestCallUploadEndpointAnswersTheCapturedUpload(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()
	fixture.parser.produces(completeCall())

	rec := fixture.postCaptureUpload(t, uploadOpenKey)

	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	got := rec.Body.String()
	want := capturedAnswer(t, "rest-upload.txt")

	if got != want {
		t.Errorf("upload answer =\n  %q\nwant\n  %q", got, want)
	}

	if strings.Contains(rec.Header().Get("Content-Type"), "json") {
		t.Errorf("the upload answer carries Content-Type %q, want what Go works out from the bytes it is given",
			rec.Header().Get("Content-Type"))
	}

	for _, field := range fixture.parser.Fields {
		if field == "key" {
			t.Fatalf("the credential was handed to the parser as if it were part of the call: %v", fixture.parser.Fields)
		}
	}

	if len(fixture.calls.Saved) != 1 {
		t.Errorf("the server stored %v, want the one call the upload carried", fixture.calls.Saved)
	}
}

// TestCallUploadEndpointRefusesAKeyTheServerDoesNotKnowWithTheCapturedAnswer
// proves the refusal is the legacy message, whose two names are nothing.
func TestCallUploadEndpointRefusesAKeyTheServerDoesNotKnowWithTheCapturedAnswer(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()
	fixture.parser.produces(completeCall())

	rec := fixture.postCaptureUpload(t, "not-an-apikey")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("upload status = %d, want 401: %s", rec.Code, rec.Body.String())
	}

	got := rec.Body.String()
	want := capturedAnswer(t, "rest-upload-denied.txt")

	if got != want {
		t.Errorf("upload answer =\n  %q\nwant\n  %q", got, want)
	}

	// The refusal is filed with the upload endpoint, which the legacy
	// server never did: the operator must tell an unreachable server
	// from a refused upload. The uploader's answer is unchanged.
	if !fixture.loggedAs(domain.LogSourceUpload, "the upload was refused: the apikey is not valid") {
		t.Errorf("the refused credential is not filed with the upload endpoint: %v", fixture.logs.Stored)
	}

	// The refusal entry is stamped with the server's clock.
	fixture.assertStamped(t, "the upload was refused")
}

// assertStamped insists that every entry mentioning a fragment carries the instant the fixture's clock holds,
// and not the wall clock of the machine the test runs on.
//
// The server has one clock, injected into every writer, so that the entries in the log view can be put in
// order against each other. The upload endpoint was the writer that had not been given it.
func (fixture *uploadFixture) assertStamped(t *testing.T, fragment string) {
	t.Helper()

	found := 0

	for _, entry := range fixture.logs.Stored {
		if !strings.Contains(entry.Message, fragment) {
			continue
		}

		found++

		if !entry.DateTime.Equal(uploadMoment) {
			t.Errorf("the entry %q is stamped %v, want the instant the server's clock holds (%v)",
				entry.Message, entry.DateTime, uploadMoment)
		}
	}

	if found == 0 {
		t.Fatalf("the server log = %v, want an entry mentioning %s", fixture.logs.Stored, fragment)
	}
}

// TestCallUploadEndpointAcceptsAKeyScopedToTheSystemTheUploadNames proves a
// system-scoped credential covers an upload naming that system. The
// legacy server dereferences an unresolved system here and answers
// nothing: a preserved quirk, repaired by owner decision.
func TestCallUploadEndpointAcceptsAKeyScopedToTheSystemTheUploadNames(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()
	fixture.parser.produces(completeCall())

	rec := fixture.postCaptureUpload(t, uploadScopedKey)

	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if rec.Body.String() != capturedAnswer(t, "rest-upload.txt") {
		t.Errorf("upload answer = %q, want the captured success line", rec.Body.String())
	}
}

// The same credential refuses an upload naming a system it does not name.
func TestCallUploadEndpointRefusesAKeyScopedAwayFromTheSystemTheUploadNames(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()
	fixture.parser.produces(completeCall())

	fields := captureUploadFields(uploadScopedKey)
	fields[1] = uploadField{name: "system", value: "10002"}

	body, contentType := postedBody(t, fields...)

	// The parser produces a call naming the system the upload names:
	// what is proven here is that the credential is decided against the
	// references the call carries, not the parser's reading of fields.
	fixture.parser.produces(callNaming(uploadOtherSystem, uploadTalkgroupRef))

	rec := fixture.post("/api/call-upload", contentType, body)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("upload status = %d, want 401: %s", rec.Code, rec.Body.String())
	}

	if len(fixture.calls.Saved) != 0 {
		t.Errorf("the server stored %v, want nothing for a refused upload", fixture.calls.Saved)
	}
}

// TestCallUploadEndpointAnswersWhatASubmissionIsMissing answers what is missing.
func TestCallUploadEndpointAnswersWhatASubmissionIsMissing(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()

	body, contentType := postedBody(t, uploadField{name: "key", value: uploadOpenKey})

	rec := fixture.post("/api/call-upload", contentType, body)

	if rec.Code != http.StatusExpectationFailed {
		t.Fatalf("upload status = %d, want 417: %s", rec.Code, rec.Body.String())
	}

	if got, want := rec.Body.String(), "Incomplete call data: no talkgroup\n\n"; got != want {
		t.Errorf("upload answer = %q, want %q: the submission names no recording, no moment, no system, and no "+
			"talkgroup, and the answer names the last thing it is missing. The legacy server's own message already "+
			"ends with a line break, and its error helper adds one", got, want)
	}

	// The log holds the exact sentence, not a fragment: the legacy helper
	// feeds the uploader's own trailing-line-break body straight into the log.
	stored := ""

	for _, entry := range fixture.logs.Stored {
		if entry.Source == domain.LogSourceUpload {
			stored = entry.Message
		}
	}

	if stored != "the upload was refused: Incomplete call data: no talkgroup" {
		t.Errorf("stored refusal = %q, want the refusal as one sentence filed with the upload endpoint, "+
			"without the line break the uploader's answer carries: %v", stored, fixture.logs.Stored)
	}
}

// TestCallUploadEndpointsAnswerTheConnectionCheckWithTheLastDeficiency answers what a
// capture client asks when it is only asking whether the server is listening: a credential,
// a system, and a test flag, with no recording in the request. The submission is short of a
// recording and of a talkgroup, and the answer names the talkgroup, which is the sentence a
// capture client reads as a working server. Both upload endpoints answer the same way because
// they refuse through the same reading. What the server keeps for itself is the same answer
// said as a check: the reason, the credential that presented itself, and the system the check
// named, filed at the level an operator reads as a normal refusal and not a lost recording. The instant that entry
// carries is read in the tests below, which post the same check at every state a check names a system in.
func TestCallUploadEndpointsAnswerTheConnectionCheckWithTheLastDeficiency(t *testing.T) {
	paths := uploadEndpointPaths

	for _, path := range paths {
		fixture := newUploadFixture()

		fixture.seedUploadSystem()
		fixture.parser.produces(connectionCheckCall(uploadSystemRef))

		body, contentType := postedBody(t, connectionCheckFields(uploadOpenKey, "10001")...)

		rec := fixture.post(path, contentType, body)

		if rec.Code != http.StatusExpectationFailed {
			t.Fatalf("%s connection check status = %d, want 417: %s", path, rec.Code, rec.Body.String())
		}

		if got, want := rec.Body.String(), "Incomplete call data: no talkgroup\n\n"; got != want {
			t.Errorf("%s connection check answer = %q, want %q: the request names a system and nothing else, "+
				"so the last thing it is missing is a talkgroup", path, got, want)
		}

		assertFiledAt(t, path+" connection check", fixture.uploadEntries(), domain.LogLevelInfo,
			"the connection check was refused: no talkgroup, presented by the apikey Capture open for system 10001",
			"the server refused a submission it read as a connection check on purpose, and the entry names what it was "+
				"refused for, which credential presented itself, and which system it named: an operator reading this "+
				"between two upload attempts is reading the answer a client got, not a recording that was lost")
	}
}

// TestTheConnectionCheckEntryNamesTheSystemTheCheckNamed reads the sentence a refused check leaves in the three
// states a check names a system in: a number this server has a row for, a number it has no row for, and nothing
// named at all. They are three different things an operator is being told about a client that keeps retrying, and
// the sentence says which one the submission did. The answer the uploader is given stays the same in all three.
func TestTheConnectionCheckEntryNamesTheSystemTheCheckNamed(t *testing.T) {
	cases := []struct {
		label  string
		posted string
		named  uint
		want   string
	}{
		{
			label:  "names a system this server has a row for",
			posted: "10001",
			named:  uploadSystemRef,
			want: "the connection check was refused: no talkgroup, presented by the apikey Capture open " +
				"for system 10001",
		},
		{
			label:  "names a number this server has no row for",
			posted: "10002",
			named:  uploadOtherSystem,
			want: "the connection check was refused: no talkgroup, presented by the apikey Capture open " +
				"for system 10002 this server does not have",
		},
		{
			label:  "names no system at all",
			posted: "",
			named:  0,
			want:   "the connection check was refused: no talkgroup, presented by the apikey Capture open",
		},
	}

	for _, path := range uploadEndpointPaths {
		for _, testCase := range cases {
			label := path + " connection check that " + testCase.label

			t.Run(strings.TrimPrefix(label, "/api/"), func(t *testing.T) {
				fixture := newUploadFixture()

				fixture.seedUploadSystem()
				fixture.parser.produces(connectionCheckCall(testCase.named))

				body, contentType := postedBody(t, connectionCheckFields(uploadOpenKey, testCase.posted)...)

				rec := fixture.post(path, contentType, body)

				if rec.Code != http.StatusExpectationFailed {
					t.Fatalf("status = %d, want 417: %s", rec.Code, rec.Body.String())
				}

				if got, want := rec.Body.String(), "Incomplete call data: no talkgroup\n\n"; got != want {
					t.Errorf("answer = %q, want %q: a check is short of a talkgroup at both endpoints whatever number "+
						"it named for a system, and the answer it is given names the last thing it was missing", got, want)
				}

				assertFiledAt(t, "the check", fixture.uploadEntries(), domain.LogLevelInfo, testCase.want,
					"the entry an operator reads is the whole sentence, and its system clause says whether the number "+
						"the check named is one this server has a row for, one it has none for, or no number at all")

				fixture.assertStamped(t, "the connection check was refused")
			})
		}
	}
}

// TestTheConnectionCheckEntryNamesTheCredentialByIdentityAndNeverTheKeyString reads the credential clause of the
// sentence a refused check leaves, and the rule that an entry says who a credential is and not what it is: the key
// string itself never appears in one. The clause names whichever credential presented itself, which is the identity
// an operator can act on in the log view. A check hands over no recording, so a credential whose scope names a
// different system than the one the check named is still one this server has, because a scope has nothing to
// withhold from a submission that brings nothing a scope could refuse.
func TestTheConnectionCheckEntryNamesTheCredentialByIdentityAndNeverTheKeyString(t *testing.T) {
	cases := []struct {
		label  string
		key    string
		posted string
		named  uint
		want   string
	}{
		{
			label:  "the credential this server holds and has not switched off",
			key:    uploadOpenKey,
			posted: "10001",
			named:  uploadSystemRef,
			want: "the connection check was refused: no talkgroup, presented by the apikey Capture open " +
				"for system 10001",
		},
		{
			label:  "the credential whose scope names the system the check names",
			key:    uploadScopedKey,
			posted: "10001",
			named:  uploadSystemRef,
			want: "the connection check was refused: no talkgroup, presented by the apikey Capture scoped " +
				"for system 10001",
		},
		{
			label:  "a credential whose scope names another system than the one the check names",
			key:    uploadScopedKey,
			posted: "10002",
			named:  uploadOtherSystem,
			want: "the connection check was refused: no talkgroup, presented by the apikey Capture scoped " +
				"for system 10002 this server does not have",
		},
	}

	for _, path := range uploadEndpointPaths {
		for _, testCase := range cases {
			label := path + " connection check with " + testCase.label

			t.Run(strings.TrimPrefix(label, "/api/"), func(t *testing.T) {
				fixture := newUploadFixture()

				fixture.seedUploadSystem()
				fixture.parser.produces(connectionCheckCall(testCase.named))

				body, contentType := postedBody(t, connectionCheckFields(testCase.key, testCase.posted)...)

				rec := fixture.post(path, contentType, body)

				if rec.Code != http.StatusExpectationFailed {
					t.Fatalf("status = %d, want 417: %s", rec.Code, rec.Body.String())
				}

				if got, want := rec.Body.String(), "Incomplete call data: no talkgroup\n\n"; got != want {
					t.Errorf("answer = %q, want %q: a check that brings nothing a scope could withhold is answered for "+
						"what it is short of as a call, not for the credential it presented", got, want)
				}

				entries := fixture.uploadEntries()

				assertFiledAt(t, "the check", entries, domain.LogLevelInfo, testCase.want,
					"the credential clause names the identity the credential is configured under, which is the one "+
						"thing an operator can act on in the log view")

				for _, apikey := range fixture.apikeys.Stored {
					for _, entry := range entries {
						if strings.Contains(entry.Message, apikey.Key) {
							t.Errorf("entry = %q, which carries the key string %s: an entry names who a credential is, "+
								"never the string that opens the door", entry.Message, apikey.Key)
						}
					}
				}

				fixture.assertStamped(t, "the connection check was refused")
			})
		}
	}
}

// TestTheConnectionCheckWithACredentialThisServerDoesNotAcceptIsRefusedAsACredential answers a check whose
// credential this server does not accept the way it answers a recording carrying the same credential: with the
// answer that refusal has always had, filed with the sentence that refusal has always had, and with no entry naming
// a check. Today that submission is answered for being short of a call before its credential is ever read, which is
// the all-clear a client reports as a working server.
func TestTheConnectionCheckWithACredentialThisServerDoesNotAcceptIsRefusedAsACredential(t *testing.T) {
	cases := []struct {
		label string
		key   string
	}{
		{label: "a key this server has never heard of", key: "not-an-apikey"},
		{label: "a key this server has and has switched off", key: uploadDisabledKey},
		{label: "no credential part at all", key: ""},
	}

	for _, path := range uploadEndpointPaths {
		for _, testCase := range cases {
			label := path + " connection check with " + testCase.label

			t.Run(strings.TrimPrefix(label, "/api/"), func(t *testing.T) {
				fixture := newUploadFixture()

				fixture.seedUploadSystem()
				fixture.parser.produces(connectionCheckCall(uploadSystemRef))

				body, contentType := postedBody(t, connectionCheckFields(testCase.key, "10001")...)

				rec := fixture.post(path, contentType, body)

				if rec.Code != http.StatusUnauthorized {
					t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
				}

				if got, want := rec.Body.String(), capturedAnswer(t, "rest-upload-denied.txt"); got != want {
					t.Errorf("answer = %q, want %q: a check whose credential this server does not accept is refused "+
						"for that credential, with the answer a refused upload carrying it has always been given", got, want)
				}

				assertFiledAt(t, "the check", fixture.uploadEntries(), domain.LogLevelError,
					"the upload was refused: the apikey is not valid",
					"the entry says the credential was refused, which is what happened, in the sentence a refused "+
						"upload has always been filed with")

				if named := fixture.entriesNamingACheck(); len(named) != 0 {
					t.Errorf("filed %v, want no entry naming a check: the server refused the credential before it "+
						"ever asked what the submission was short of", named)
				}

				if len(fixture.calls.Saved) != 0 {
					t.Errorf("stored %v, want nothing stored for a refused check", fixture.calls.Saved)
				}

				fixture.assertStamped(t, "the apikey is not valid")
			})
		}
	}
}

// TestTheTwoUploadRefusalsDoNotCollapseTheLog keeps the two ways an upload endpoint says no apart in the log view. A
// connection check and a recording that names no talkgroup are refused for the same thing, and the answer each is put
// on the wire is the same sentence with the same status: the entry the server keeps for itself is the only place an
// operator can tell a client that was testing from a recording that was lost. Both refusals are posted in one server
// run so that they are read against each other, and the recording refusal is the shape a capture client posts when it
// has no talkgroup number — the digit zero, and no label beside it. Which deficiency each reason word names is the
// model's own reading, pinned in its own suite; what is pinned here is that the level and the sentence each refusal is
// filed with do not move when the other refusal happens beside it.
func TestTheTwoUploadRefusalsDoNotCollapseTheLog(t *testing.T) {
	for _, path := range uploadEndpointPaths {
		t.Run(strings.TrimPrefix(path, "/api/")+" refused recording and refused check", func(t *testing.T) {
			fixture := newUploadFixture()

			fixture.seedUploadSystem()

			fixture.parser.produces(recordingNamingNoTalkgroup(uploadSystemRef))

			body, contentType := postedBody(t, recordingNamingNoTalkgroupFields(uploadOpenKey)...)

			rec := fixture.post(path, contentType, body)

			if rec.Code != http.StatusExpectationFailed {
				t.Fatalf("%s refused recording status = %d, want 417: %s", path, rec.Code, rec.Body.String())
			}

			if got, want := rec.Body.String(), "Incomplete call data: no talkgroup\n\n"; got != want {
				t.Errorf("%s refused recording answer = %q, want %q: a posting that carries audio, a moment, and a "+
					"system number, and names the digit zero for its talkgroup, is short of a talkgroup and is answered "+
					"for that", path, got, want)
			}

			assertFiledAt(t, path+" refused recording", fixture.uploadEntries(), domain.LogLevelError,
				"the upload was refused: Incomplete call data: no talkgroup",
				"a recording this server did not accept is reported as a lost recording, in the sentence and at the "+
					"level an operator reads it in today")

			fixture.parser.produces(connectionCheckCall(uploadSystemRef))

			body, contentType = postedBody(t, connectionCheckFields(uploadOpenKey, "10001")...)

			rec = fixture.post(path, contentType, body)

			if rec.Code != http.StatusExpectationFailed {
				t.Fatalf("%s connection check status = %d, want 417: %s", path, rec.Code, rec.Body.String())
			}

			if got, want := rec.Body.String(), "Incomplete call data: no talkgroup\n\n"; got != want {
				t.Errorf("%s connection check answer = %q, want %q: the two refusals are answered alike on purpose, "+
					"because the answer a client reads as a working server is the pinned one", path, got, want)
			}

			entries := fixture.uploadEntries()

			if len(entries) != 2 {
				t.Fatalf("%s filed %d entries with the upload endpoint for two refusals, want the one each leaves: %v",
					path, len(entries), entries)
			}

			assertFiledAt(t, path+" refused check", entries[1:], domain.LogLevelInfo,
				"the connection check was refused: no talkgroup, presented by the apikey Capture open for system 10001",
				"the two submissions were refused for the same thing, so the entry the server keeps for itself is the "+
					"only place an operator can tell them apart")

			if named := fixture.entriesNamingACheck(); len(named) != 1 || named[0].Level != domain.LogLevelInfo {
				t.Errorf("%s filed %v, want the two refusals told apart by level and by sentence: the check is the only "+
					"one of the two that names itself, and it is filed where a routine refusal is read", path, named)
			}

			fixture.assertStamped(t, "was refused")
		})

		t.Run(strings.TrimPrefix(path, "/api/")+" marker carried beside a recording", func(t *testing.T) {
			fixture := newUploadFixture()

			fixture.seedUploadSystem()
			fixture.parser.produces(recordingCarryingTheCheckClaim())

			body, contentType := postedBody(t, recordingWithCheckClaimFields(uploadOpenKey)...)

			rec := fixture.post(path, contentType, body)

			if rec.Code != http.StatusOK {
				t.Fatalf("%s status = %d, want 200: %s", path, rec.Code, rec.Body.String())
			}

			if got, want := rec.Body.String(), capturedAnswer(t, capturedUploadAnswers[path]); got != want {
				t.Errorf("%s answer =\n  %q\nwant\n  %q: a submission that carries a recording long enough to be one is "+
					"a recording, whatever marker was posted beside it, and it is answered the way that recording has "+
					"always been answered", path, got, want)
			}

			if len(fixture.calls.Saved) != 1 {
				t.Errorf("%s stored %v, want the one call the recording carried: a marker beside a recording does not "+
					"make it a check, and a check is the one submission this server keeps nothing of", path, fixture.calls.Saved)
			}

			if entries := fixture.uploadEntries(); len(entries) != 0 {
				t.Errorf("%s filed %v with the upload endpoint, want no refusal for an accepted recording: a marker says "+
					"what a submission is only when the submission brings no recording", path, entries)
			}
		})
	}
}

// TestAConnectionCheckCreatesNothing proves that a connection check leaves the server holding nothing it did not hold
// before. The check names a system and a talkgroup no row in this fixture carries, and the server auto-populates what
// an accepted recording names: a check that reached the ingest use case would invent both rows, file the entry that
// creation earns, and file a call of its own. A refused check is answered by the use case that answers checks, and
// that use case stores nothing. The entry naming the refusal is read here because a submission the server never read
// as a check would prove nothing about what a check leaves behind.
func TestAConnectionCheckCreatesNothing(t *testing.T) {
	for _, path := range uploadEndpointPaths {
		t.Run(strings.TrimPrefix(path, "/api/")+" connection check names nothing the operator configured", func(t *testing.T) {
			fixture := newUploadFixture()

			fixture.parser.produces(checkNamingTalkgroupCall())

			body, contentType := postedBody(t, checkNamingTalkgroupFields(uploadOpenKey)...)

			rec := fixture.post(path, contentType, body)

			if rec.Code != http.StatusExpectationFailed {
				t.Fatalf("%s status = %d, want 417: %s", path, rec.Code, rec.Body.String())
			}

			if got, want := rec.Body.String(), "Incomplete call data: no audio\n\n"; got != want {
				t.Errorf("%s answer = %q, want %q: the check names a system, a talkgroup, and a moment, and brings no "+
					"recording, so the last thing it is missing is its audio", path, got, want)
			}

			if named := fixture.entriesNamingACheck(); len(named) != 1 {
				t.Errorf("%s filed %v, want the one entry a refused check leaves: without that entry there is no reading "+
					"of this submission as a check to measure the empty tables against", path, named)
			}

			if len(fixture.calls.Saved) != 0 || len(fixture.calls.Stored) != 0 {
				t.Errorf("%s stored %v in a history of %d, want no call at all: a check brings no recording, so there is "+
					"nothing to record", path, fixture.calls.Saved, len(fixture.calls.Stored))
			}

			if len(fixture.systems.Stored) != 0 {
				t.Errorf("%s holds systems %v, want no system row and so no talkgroup row under one: the numbers a check "+
					"names are not a request to configure them", path, fixture.systems.Stored)
			}

			if len(fixture.groups.Stored) != 1 || len(fixture.tags.Stored) != 1 {
				t.Errorf("%s holds groups %v and tags %v, want the one group and the one tag the server started with",
					path, fixture.groups.Stored, fixture.tags.Stored)
			}

			for _, source := range []domain.LogSource{domain.LogSourceIngest, domain.LogSourceConfig} {
				if entries := fixture.entriesFiledUnder(source); len(entries) != 0 {
					t.Errorf("%s filed %v under %s, want nothing there: a submission that reached the ingest use case "+
						"would file its own entry there, and file the configuration it auto-populated beside it",
						path, entries, source)
				}
			}

			fixture.assertStamped(t, "was refused")
		})
	}
}

// TestTheConnectionCheckWhoseConfigurationCannotBeReadIsAnsweredAsAFailure keeps a check from being
// answered with the bytes a client reads as an all-clear when the configuration the answer is read out
// of could not be read at all. A check and a recording are guarded by the same credential list and read
// from the same system rows, so a server that cannot read them fails both alike.
func TestTheConnectionCheckWhoseConfigurationCannotBeReadIsAnsweredAsAFailure(t *testing.T) {
	fixture := newUploadFixture()

	fixture.apikeys.Err = errors.New("the credentials cannot be read")
	fixture.parser.produces(connectionCheckCall(uploadSystemRef))

	body, contentType := postedBody(t, connectionCheckFields(uploadOpenKey, "10001")...)

	rec := fixture.post("/api/call-upload", contentType, body)

	if rec.Code != http.StatusExpectationFailed {
		t.Fatalf("status = %d, want 417: %s", rec.Code, rec.Body.String())
	}

	if got := rec.Body.String(); !strings.HasPrefix(got, "Call upload failed: ") {
		t.Errorf("answer = %q, want the answer an upload gets when the configuration guarding it cannot be read: "+
			"a check answered from that same configuration is answered with the same failure", got)
	}

	if strings.HasPrefix(rec.Body.String(), "Incomplete call data") {
		t.Errorf("answer = %q, want no deficiency answer: a client reads those bytes as an all-clear, and this "+
			"server read nothing it could answer from", rec.Body.String())
	}

	if named := fixture.entriesNamingACheck(); len(named) != 0 {
		t.Errorf("filed %v, want no entry naming a check: the server never read the configuration that says what "+
			"a submission was", fixture.logs.Stored)
	}

	if !fixture.loggedContaining("the credentials cannot be read") {
		t.Errorf("the failure to read is not in the log view: %v", fixture.logs.Stored)
	}

	if len(fixture.calls.Saved) != 0 || len(fixture.systems.Stored) != 0 {
		t.Errorf("stored calls %v and systems %v, want nothing: a check decides nothing, even when the read it "+
			"would have answered from failed", fixture.calls.Saved, fixture.systems.Stored)
	}

	fixture.assertStamped(t, "Call upload failed")
}

// TestCallUploadEndpointRefusesAMethodItDoesNotAnswerWithItsOwnBody refuses with
// its own body.
func TestCallUploadEndpointRefusesAMethodItDoesNotAnswerWithItsOwnBody(t *testing.T) {
	fixture := newUploadFixture()

	req := httptest.NewRequest(http.MethodGet, "/api/call-upload", nil)

	rec := httptest.NewRecorder()

	fixture.routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", rec.Code)
	}

	if got, want := rec.Body.String(), "Unsupported method\n"; got != want {
		t.Errorf("GET answer = %q, want %q", got, want)
	}

	// A wrong-method poster is a standing misconfiguration, and the log
	// view is where an operator learns the server was reached at all:
	// the legacy server answered this refusal in silence.
	if !fixture.loggedAs(domain.LogSourceUpload, "the upload was refused: the server does not answer this method") {
		t.Errorf("the refused method is not filed with the upload endpoint: %v", fixture.logs.Stored)
	}
}

// TestCallUploadEndpointRefusesARequestThatIsNotAMultipartBody proves the
// non-multipart request is refused with the contract's status, logged.
func TestCallUploadEndpointRefusesARequestThatIsNotAMultipartBody(t *testing.T) {
	fixture := newUploadFixture()

	rec := fixture.post("/api/call-upload", "application/json", `{"key":"`+uploadOpenKey+`"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("upload status = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	if got, want := rec.Body.String(), "Not a multipart content\n"; got != want {
		t.Errorf("upload answer = %q, want %q", got, want)
	}

	if !fixture.loggedAs(domain.LogSourceUpload, "the upload was refused: Not a multipart content") {
		t.Errorf("the refused request is not filed with the upload endpoint naming what it refused: %v", fixture.logs.Stored)
	}
}

// TestCallUploadEndpointRefusesARequestWhoseContentTypeItCannotRead
// proves it names which of the two refusals happened.
func TestCallUploadEndpointRefusesARequestWhoseContentTypeItCannotRead(t *testing.T) {
	fixture := newUploadFixture()

	rec := fixture.post("/api/call-upload", `multipart/form-data; boundary="unterminated`, "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("upload status = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	if got, want := rec.Body.String(), "Invalid content-type\n"; got != want {
		t.Errorf("upload answer = %q, want %q", got, want)
	}

	if !fixture.loggedAs(domain.LogSourceUpload, "the upload was refused: Invalid content-type") {
		t.Errorf("the unreadable Content-Type is not filed with the upload endpoint: %v", fixture.logs.Stored)
	}
}

// TestCallUploadEndpointReportsAMultipartBodyItCannotRead proves an
// unreadable body is reported with the reader's own words.
func TestCallUploadEndpointReportsAMultipartBodyItCannotRead(t *testing.T) {
	fixture := newUploadFixture()

	rec := fixture.post("/api/call-upload", `multipart/form-data; boundary="----capture"`, "--wrong-boundary\r\n\r\n")

	if rec.Code != http.StatusExpectationFailed {
		t.Fatalf("upload status = %d, want 417: %s", rec.Code, rec.Body.String())
	}

	got := rec.Body.String()

	if !strings.HasPrefix(got, "multipart: ") || !strings.HasSuffix(got, "\n\n") {
		t.Errorf("upload answer = %q, want the reader's own words wrapped the way the legacy server wraps them", got)
	}

	if !fixture.loggedAs(domain.LogSourceUpload, "the upload was refused: multipart: ") {
		t.Errorf("the unreadable body is not filed with the upload endpoint naming the step that failed: %v", fixture.logs.Stored)
	}
}

// TestCallUploadEndpointReportsABodyThatStopsBeingRead proves a body that
// stops being readable is refused as a failure, never answered half-read.
func TestCallUploadEndpointReportsABodyThatStopsBeingRead(t *testing.T) {
	fixture := newUploadFixture()

	body, contentType := postedBody(t, uploadField{name: "key", value: uploadOpenKey}, uploadField{name: "audio", filename: "capture.wav", value: uploadRecording})

	req := httptest.NewRequest(http.MethodPost, "/api/call-upload", &halfBrokenReader{rest: body})
	req.Header.Set("Content-Type", contentType)

	rec := httptest.NewRecorder()

	fixture.routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusExpectationFailed {
		t.Fatalf("upload status = %d, want 417: %s", rec.Code, rec.Body.String())
	}

	got := rec.Body.String()

	if !strings.HasPrefix(got, "multipart: ") && !strings.HasPrefix(got, "ioread: ") {
		t.Errorf("upload answer = %q, want it to name the step that failed", got)
	}

	if !fixture.loggedAs(domain.LogSourceUpload, "the upload was refused: ") {
		t.Errorf("the broken body is not filed with the upload endpoint: %v", fixture.logs.Stored)
	}
}

// TestUploadEndpointAnswersSuccessForACallTheServerCouldNotPlace proves an
// unplaceable call is still answered success: the failure reaches only the
// server's own log view.
func TestUploadEndpointAnswersSuccessForACallTheServerCouldNotPlace(t *testing.T) {
	fixture := newUploadFixture()

	fixture.options.Stored.AutoPopulate = false

	fixture.parser.produces(completeCall())

	body, contentType := postedBody(t, captureUploadFields(uploadOpenKey)...)

	rec := fixture.post("/api/call-upload", contentType, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if rec.Body.String() != capturedAnswer(t, "rest-upload.txt") {
		t.Errorf("upload answer = %q, want the captured success line", rec.Body.String())
	}

	if len(fixture.calls.Saved) != 0 {
		t.Errorf("the server stored %v, want nothing: the call names a system the server does not know and "+
			"auto-populate is switched off", fixture.calls.Saved)
	}

	if !fixture.loggedContaining("the recording capture.wav does not match any system or talkgroup the server knows") {
		t.Errorf("the call the server could not place is not in the log view: %v", fixture.logs.Stored)
	}
}

// TestTrunkRecorderUploadEndpointAnswersTheCapturedUpload proves the
// sidecar is read as a document and not as a posted field.
func TestTrunkRecorderUploadEndpointAnswersTheCapturedUpload(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()
	fixture.parser.produces(completeCall())

	body, contentType := postedBody(t,
		uploadField{name: "key", value: uploadOpenKey},
		uploadField{name: "meta", value: `{"freqList":[{"error_count":1,"freq":151400000,"pos":0.5,"spike_count":2}],` +
			`"patched_talkgroups":[1514100],"short_name":"Grand Metro","srcList":[{"pos":0.5,"src":1001}],` +
			`"talkgroup":1514000,"talkgroup_description":"Dispatch 1","talkgroup_group":"Ground",` +
			`"talkgroup_group_tag":"Fire Dispatch","talkgroup_tag":"Ground","timestamp":1791288060000}`},
		uploadField{name: "audio", filename: "capture.wav", value: uploadRecording},
	)

	rec := fixture.post("/api/trunk-recorder-call-upload", contentType, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("trunk-recorder upload status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	got := rec.Body.String()
	want := capturedAnswer(t, "rest-trunk-recorder-upload.txt")

	if got != want {
		t.Errorf("trunk-recorder upload answer =\n  %q\nwant\n  %q", got, want)
	}

	if len(fixture.parser.Sidecars) != 1 {
		t.Fatalf("the sidecar reading was asked %d times, want once", len(fixture.parser.Sidecars))
	}

	for _, field := range fixture.parser.Fields {
		if field == "meta" {
			t.Fatalf("the sidecar was handed to the field reading as if it were a value: %v", fixture.parser.Fields)
		}
	}
}

// TestTrunkRecorderUploadEndpointRefusesASidecarItCannotRead proves the
// refusal is the reasonless legacy words, the text going to the log.
func TestTrunkRecorderUploadEndpointRefusesASidecarItCannotRead(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()
	fixture.parser.produces(completeCall())
	fixture.parser.SidecarErr = errSidecarUnreadable

	body, contentType := postedBody(t,
		uploadField{name: "key", value: uploadOpenKey},
		uploadField{name: "meta", value: `{"talkgroup":`},
		uploadField{name: "audio", filename: "capture.wav", value: uploadRecording},
	)

	rec := fixture.post("/api/trunk-recorder-call-upload", contentType, body)

	if rec.Code != http.StatusExpectationFailed {
		t.Fatalf("trunk-recorder upload status = %d, want 417: %s", rec.Code, rec.Body.String())
	}

	if got, want := rec.Body.String(), "Invalid call data\n"; got != want {
		t.Errorf("trunk-recorder upload answer = %q, want %q", got, want)
	}

	if !fixture.loggedAs(domain.LogSourceUpload, "the upload was refused: Invalid call data") {
		t.Errorf("the unreadable sidecar is not filed with the upload endpoint: %v", fixture.logs.Stored)
	}

	if len(fixture.calls.Saved) != 0 {
		t.Errorf("the server stored %v, want nothing", fixture.calls.Saved)
	}
}

// TestUploadEndpointsHandTheParserEveryFieldInTheOrderTheyArrived proves
// both endpoints apply posted fields in arrival order, so a later field
// wins an earlier one.
func TestUploadEndpointsHandTheParserEveryFieldInTheOrderTheyArrived(t *testing.T) {
	cases := []struct {
		path   string
		fields []uploadField
		want   []string
		names  []string
	}{
		{
			path: "/api/call-upload",
			fields: []uploadField{
				{name: "key", value: uploadOpenKey},
				{name: "audio", filename: "first.wav", value: uploadRecording},
				{name: "audioFilename", value: "second.wav"},
			},
			want:  []string{"audio", "audioFilename"},
			names: []string{"first.wav", ""},
		},
		{
			path: "/api/call-upload",
			fields: []uploadField{
				{name: "key", value: uploadOpenKey},
				{name: "audioFilename", value: "second.wav"},
				{name: "audio", filename: "first.wav", value: uploadRecording},
			},
			want:  []string{"audioFilename", "audio"},
			names: []string{"", "first.wav"},
		},
		{
			path: "/api/trunk-recorder-call-upload",
			fields: []uploadField{
				{name: "key", value: uploadOpenKey},
				{name: "meta", value: `{"talkgroup":1514000}`},
				{name: "audio", filename: "first.wav", value: uploadRecording},
				{name: "audioFilename", value: "second.wav"},
			},
			want:  []string{"audio", "audioFilename"},
			names: []string{"first.wav", ""},
		},
	}

	for _, testCase := range cases {
		fixture := newUploadFixture()

		fixture.seedUploadSystem()

		body, contentType := postedBody(t, testCase.fields...)

		rec := fixture.post(testCase.path, contentType, body)

		if rec.Code != http.StatusExpectationFailed {
			t.Fatalf("%s status = %d, want 417 for a submission that names no system or talkgroup: %s",
				testCase.path, rec.Code, rec.Body.String())
		}

		if got := strings.Join(fixture.parser.Fields, ","); got != strings.Join(testCase.want, ",") {
			t.Errorf("%s handed the parser %s, want the fields in the order they arrived: %s",
				testCase.path, got, strings.Join(testCase.want, ","))
		}

		if got := strings.Join(fixture.parser.FileNames, ","); got != strings.Join(testCase.names, ",") {
			t.Errorf("%s handed the parser file names %s, want %s: the name a recording was posted under is "+
				"how the server names the recording it stores", testCase.path, got, strings.Join(testCase.names, ","))
		}
	}
}

var errSidecarUnreadable = errors.New("unexpected end of JSON input")

// halfBrokenReader hands back a body and then fails, which is what a connection that goes away partway
// through an upload looks like to the server.
type halfBrokenReader struct {
	rest string
	read int
}

func (reader *halfBrokenReader) Read(p []byte) (int, error) {
	if reader.read == 0 {
		reader.read = len(reader.rest) / 2

		return copy(p, reader.rest[:reader.read]), nil
	}

	if reader.read >= len(reader.rest) {
		return 0, io.EOF
	}

	rest := reader.rest[reader.read:]

	reader.read = len(reader.rest)

	return copy(p, rest[:1]), errBodyCutShort
}

var errBodyCutShort = &bodyCutShortError{}

type bodyCutShortError struct{}

func (err *bodyCutShortError) Error() string {
	return "the connection was closed while the body was being read"
}

// TestCallUploadEndpointRefusesABodyBeyondTheUploadCap proves a body over
// the cap is refused with the existing read-failure answer: the only
// thing between an anonymous POST and the server's memory.
func TestCallUploadEndpointRefusesABodyBeyondTheUploadCap(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()
	fixture.parser.produces(completeCall())

	big := bytes.Repeat([]byte{0x01}, 33*1024*1024)

	body, contentType := postedBody(t, append(captureUploadFields(uploadOpenKey),
		uploadField{name: "audio", filename: "big.wav", value: string(big)})...)

	rec := fixture.post("/api/call-upload", contentType, body)

	if rec.Code != http.StatusExpectationFailed {
		t.Fatalf("upload status = %d, want 417 for a body over the cap: %s", rec.Code, rec.Body.String())
	}

	if got := rec.Body.String(); !strings.HasPrefix(got, "multipart: ") && !strings.HasPrefix(got, "ioread: ") {
		t.Errorf("upload answer = %q, want it to name the step that failed", got)
	}
}

// TestCallUploadEndpointAcceptsABodyUnderTheUploadCap proves the cap
// costs nothing real uploads carry: such a body is answered as before.
func TestCallUploadEndpointAcceptsABodyUnderTheUploadCap(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()
	fixture.parser.produces(completeCall())

	recording := bytes.Repeat([]byte{0x01}, 1024*1024)

	body, contentType := postedBody(t, append(captureUploadFields(uploadOpenKey),
		uploadField{name: "audio", filename: "big.wav", value: string(recording)})...)

	rec := fixture.post("/api/call-upload", contentType, body)

	if rec.Code != http.StatusOK {
		t.Errorf("upload status = %d, want 200 for a body under the cap: %s", rec.Code, rec.Body.String())
	}
}

// TestCallUploadEndpointFilesAnOversizedListTruncation proves the
// endpoint files the parser's list truncation in the operator's log:
// the upload is still taken, just bounded.
func TestCallUploadEndpointFilesAnOversizedListTruncation(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()
	fixture.parser.produces(completeCall())
	fixture.parser.Truncation = "the frequencies list was truncated to 4096 entries"

	entries := make([]any, 5000)
	for i := range entries {
		entries[i] = map[string]any{"freq": float64(1514000 + i)}
	}

	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshaling the frequencies list: %v", err)
	}

	body, contentType := postedBody(t, append(captureUploadFields(uploadOpenKey),
		uploadField{name: "frequencies", value: string(raw)})...)

	rec := fixture.post("/api/call-upload", contentType, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want 200 for an upload whose oversized list is bounded: %s",
			rec.Code, rec.Body.String())
	}

	if !fixture.loggedAs(domain.LogSourceUpload, "frequencies list was truncated to 4096") {
		t.Errorf("the truncation is not filed with the upload endpoint: %v", fixture.logs.Stored)
	}
}
