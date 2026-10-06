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

package rest

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rdio-scanner/server-ng/internal/application"
	"rdio-scanner/server-ng/internal/application/fakes"
	"rdio-scanner/server-ng/internal/domain"
)

// The call-upload endpoints are what an outside program posts recordings to: a radio bridge, another
// rdio-scanner server reposting what it received, and the trunk-recorder compatibility path. They answer
// with plain text and not with JSON, they are not behind an administrator session, and every failure they
// report is written in the server's own log view as well as to the uploader.

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
		fixture.hub,
		fixture.delayer,
		fixture.downstreams,
		fixture.forwarder,
		fixture.clock,
	)

	mux := http.NewServeMux()

	NewUpload(UploadDeps{
		Ingest: application.NewUploadCallHandler(fixture.apikeys, ingest),
		Parser: fixture.parser,
		Logs:   fixture.logs,
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
// The real parser is not used here because the endpoints may not import the package that holds it
// (contract-server-ng.md §1); it is wired to them by the composition root, and its reading is proven by
// internal/infrastructure/dirwatch/parsers/multipart_test.go.
type recordingUploadParser struct {
	// Fields records every field that was handed to the field reading, in the order it arrived.
	Fields []string

	// FiledNames records the file name each field arrived with, which is how the recording's own name
	// reaches the server.
	FileNames []string

	// Bodies records the contents of each field handed to the field reading.
	Bodies [][]byte

	// Sidecars records every sidecar the trunk-recorder endpoint handed to the sidecar reading.
	Sidecars [][]byte

	// Completed is the call the parser produces, and HasCall says whether it produces one at all. A
	// parser that produces nothing is a parser that read fields naming nothing a call needs, which is how
	// these tests produce an incomplete call.
	Completed domain.Call
	HasCall   bool

	// SidecarErr, when set, is what the sidecar reading reports.
	SidecarErr error
}

// produces sets the call the parser hands back for every field it is asked to read.
func (parser *recordingUploadParser) produces(call domain.Call) {
	parser.Completed = call
	parser.HasCall = true
}

func (parser *recordingUploadParser) ApplyMultipart(part *multipart.Part, data []byte, call domain.Call) domain.Call {
	parser.Fields = append(parser.Fields, part.FormName())
	parser.FileNames = append(parser.FileNames, part.FileName())
	parser.Bodies = append(parser.Bodies, data)

	if parser.HasCall {
		return parser.Completed
	}

	return call
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

// An upload the server takes is answered with the plain line the legacy server has always answered with,
// and the credential is taken out of the fields rather than handed to the parser: the parser has no
// business knowing that a key exists.
func TestCallUploadEndpointAnswersTheCapturedUpload(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()
	fixture.parser.produces(completeCall())

	rec := fixture.postCaptureUpload(t, uploadOpenKey)

	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	got := rec.Body.String()
	want := readGolden(t, "rest-upload.txt")

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

// A credential the server does not know is refused with the message the legacy server has always written,
// including the two names it always says are nothing: the message is built from the call's resolved system
// and talkgroup, and at this boundary neither has been resolved.
func TestCallUploadEndpointRefusesAKeyTheServerDoesNotKnowWithTheCapturedAnswer(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()
	fixture.parser.produces(completeCall())

	rec := fixture.postCaptureUpload(t, "not-an-apikey")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("upload status = %d, want 401: %s", rec.Code, rec.Body.String())
	}

	got := rec.Body.String()
	want := readGolden(t, "rest-upload-denied.txt")

	if got != want {
		t.Errorf("upload answer =\n  %q\nwant\n  %q", got, want)
	}

	if fixture.loggedContaining("Invalid API key") {
		t.Errorf("the refused upload is written in the log view, which the legacy server does not do: %v", fixture.logs.Stored)
	}
}

// A credential scoped to particular systems covers an upload naming one of them. The legacy server cannot
// answer this case at all: it compares the credential against the call's resolved system, and at this
// boundary the system has not been resolved, so the comparison dereferences nothing and the connection
// ends with no reply. Recorded as quirk Q51, and fixed by the owner's decision on Q20.
func TestCallUploadEndpointAcceptsAKeyScopedToTheSystemTheUploadNames(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()
	fixture.parser.produces(completeCall())

	rec := fixture.postCaptureUpload(t, uploadScopedKey)

	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if rec.Body.String() != readGolden(t, "rest-upload.txt") {
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

	// The call the parser produces names the system the upload names. What the parser makes of a field is
	// the parser's own contract and is proven in its own suite; what is proven here is that the credential
	// is decided against the references the call carries.
	fixture.parser.produces(callNaming(uploadOtherSystem, uploadTalkgroupRef))

	rec := fixture.post("/api/call-upload", contentType, body)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("upload status = %d, want 401: %s", rec.Code, rec.Body.String())
	}

	if len(fixture.calls.Saved) != 0 {
		t.Errorf("the server stored %v, want nothing for a refused upload", fixture.calls.Saved)
	}
}

// A submission that is not a call is answered with what is missing, in the legacy server's own words, and
// the same line is written in the log view.
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

	if !fixture.loggedContaining("api: Incomplete call data: no audio") {
		t.Errorf("the refused submission is not in the log view as the legacy server wrote it: %v", fixture.logs.Stored)
	}
}

// A method the endpoint does not answer is refused with a body of its own, which is unusual for this
// server and is what its clients have always been given.
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

	if fixture.loggedContaining("Unsupported method") {
		t.Errorf("the refused method is written in the log view, which the legacy server does not do: %v", fixture.logs.Stored)
	}
}

// A request that is not a multipart body is refused with the status the frozen contract fixes, and the
// reason is written in the log view.
func TestCallUploadEndpointRefusesARequestThatIsNotAMultipartBody(t *testing.T) {
	fixture := newUploadFixture()

	rec := fixture.post("/api/call-upload", "application/json", `{"key":"`+uploadOpenKey+`"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("upload status = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	if got, want := rec.Body.String(), "Not a multipart content\n"; got != want {
		t.Errorf("upload answer = %q, want %q", got, want)
	}

	if !fixture.loggedContaining("api: Not a multipart content") {
		t.Errorf("the refused request is not in the log view as the legacy server wrote it: %v", fixture.logs.Stored)
	}
}

// A Content-Type the server cannot read at all is a different refusal from a Content-Type that reads but
// is not a multipart body, and the legacy server says which one happened.
func TestCallUploadEndpointRefusesARequestWhoseContentTypeItCannotRead(t *testing.T) {
	fixture := newUploadFixture()

	rec := fixture.post("/api/call-upload", `multipart/form-data; boundary="unterminated`, "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("upload status = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	if got, want := rec.Body.String(), "Invalid content-type\n"; got != want {
		t.Errorf("upload answer = %q, want %q", got, want)
	}

	if !fixture.loggedContaining("api: Invalid content-type") {
		t.Errorf("the unreadable Content-Type is not in the log view as the legacy server wrote it: %v", fixture.logs.Stored)
	}
}

// A multipart body the server cannot read is reported with the failure status and the reader's own words,
// because someone posting to this endpoint needs to know their request was malformed and not rejected on
// its contents.
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

	if !fixture.loggedContaining("api: multipart: ") {
		t.Errorf("the unreadable body is not in the log view as the legacy server wrote it: %v", fixture.logs.Stored)
	}
}

// A body that stops being readable partway through is a failure of the connection and not of the upload,
// and the server says so rather than answering with whatever it managed to read.
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

	if !fixture.loggedContaining("api: ") {
		t.Errorf("the broken body is not in the log view as the legacy server wrote it: %v", fixture.logs.Stored)
	}
}

// An upload the server could not place is still an upload the uploader was told it made. A call naming a
// system the server does not know is reported in the server's own log view and not to the uploader, which
// is what the legacy server's ingest queue produces and what an uploader can only act on by looking at the
// server's log view.
func TestUploadEndpointAnswersSuccessForACallTheServerCouldNotPlace(t *testing.T) {
	fixture := newUploadFixture()

	fixture.options.Stored.AutoPopulate = false

	fixture.parser.produces(completeCall())

	body, contentType := postedBody(t, captureUploadFields(uploadOpenKey)...)

	rec := fixture.post("/api/call-upload", contentType, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if rec.Body.String() != readGolden(t, "rest-upload.txt") {
		t.Errorf("upload answer = %q, want the captured success line", rec.Body.String())
	}

	if len(fixture.calls.Saved) != 0 {
		t.Errorf("the server stored %v, want nothing: the call names a system the server does not know and "+
			"auto-populate is switched off", fixture.calls.Saved)
	}

	if !fixture.loggedContaining("no matching system/talkgroup") {
		t.Errorf("the call the server could not place is not in the log view: %v", fixture.logs.Stored)
	}
}

// The trunk-recorder compatibility endpoint takes a JSON sidecar and a recording. The sidecar is read by
// the sidecar reading and not as a form field, because its contents are a document and not a value.
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
	want := readGolden(t, "rest-trunk-recorder-upload.txt")

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

// A sidecar the server cannot read is refused with the legacy server's own words, which name no reason:
// the failure text is written in the log view and the uploader is told only that the call data is invalid.
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

	if !fixture.loggedContaining("api: Invalid call data") {
		t.Errorf("the unreadable sidecar is not in the log view as the legacy server wrote it: %v", fixture.logs.Stored)
	}

	if len(fixture.calls.Saved) != 0 {
		t.Errorf("the server stored %v, want nothing", fixture.calls.Saved)
	}
}

// Both endpoints hand every posted field to the ingest parser in the order it arrived, and a field that
// arrived later is applied after one that arrived earlier — which is what decides which of two fields
// naming the same thing wins.
//
// The legacy server's trunk-recorder endpoint collects the fields into a map and applies them in whatever
// order Go walks that map in, so an upload naming both a recording and a name for it is answered
// differently on different runs. Recorded as quirk Q57, and not reproduced: both endpoints here apply the
// fields as they arrived, which is what the legacy server's own call-upload endpoint does. What a field
// means, and that a later one overwrites an earlier one, is proven by the parser's own suite in
// internal/infrastructure/dirwatch/parsers/multipart_test.go.
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
