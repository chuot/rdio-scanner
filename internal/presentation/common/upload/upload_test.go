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
// see the call-upload-tests-and-their-captures maintainer note.

const (
	uploadSystemRef    = uint(10001)
	uploadOtherSystem  = uint(10002)
	uploadTalkgroupRef = uint(1514000)
	uploadOpenKey      = "capture-apikey-open"
	uploadScopedKey    = "capture-apikey-scoped"
	uploadRecording    = "a recording long enough to be a recording, and then some more of it"
)

var uploadMoment = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

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

// TestCallUploadEndpointAnswersTheCapturedUpload proves an accepted
// upload gets the captured legacy answer, credential kept out.
// see the call-upload-tests-and-their-captures maintainer note.
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
// see the call-upload-tests-and-their-captures maintainer note.
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
	// see the call-upload-tests-and-their-captures maintainer note.
	if !fixture.loggedAs(domain.LogSourceUpload, "the upload was refused: the apikey is not valid") {
		t.Errorf("the refused credential is not filed with the upload endpoint: %v", fixture.logs.Stored)
	}

	// The refusal entry is stamped with the server's clock.
	// see the call-upload-tests-and-their-captures maintainer note.
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

// TestCallUploadEndpointAnswersWhatASubmissionIsMissing answers what is missing,
// see the call-upload-tests-and-their-captures maintainer note.
func TestCallUploadEndpointAnswersWhatASubmissionIsMissing(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()

	body, contentType := postedBody(t, uploadField{name: "key", value: uploadOpenKey})

	rec := fixture.post("/api/call-upload", contentType, body)

	if rec.Code != http.StatusExpectationFailed {
		t.Fatalf("upload status = %d, want 417: %s", rec.Code, rec.Body.String())
	}

	if got, want := rec.Body.String(), "Incomplete call data: no audio\n\n"; got != want {
		t.Errorf("upload answer = %q, want %q: the legacy server's own message already ends with a line break, "+
			"and its error helper adds one", got, want)
	}

	// The log holds the exact sentence, not a fragment: the legacy helper
	// feeds the uploader's own trailing-line-break body straight into the log.
	stored := ""

	for _, entry := range fixture.logs.Stored {
		if entry.Source == domain.LogSourceUpload {
			stored = entry.Message
		}
	}

	if stored != "the upload was refused: Incomplete call data: no audio" {
		t.Errorf("stored refusal = %q, want the refusal as one sentence filed with the upload endpoint, "+
			"without the line break the uploader's answer carries: %v", stored, fixture.logs.Stored)
	}
}

// TestCallUploadEndpointRefusesAMethodItDoesNotAnswerWithItsOwnBody refuses
// with its own body, see the call-upload-tests-and-their-captures maintainer note.
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
// see the call-upload-tests-and-their-captures maintainer note.
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
// unplaceable call is still answered success: the failure reaches only
// the server's own log view.
// see the call-upload-tests-and-their-captures maintainer note.
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
// see the call-upload-tests-and-their-captures maintainer note.
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
// see the call-upload-tests-and-their-captures maintainer note.
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
// see the call-upload-tests-and-their-captures maintainer note.
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
