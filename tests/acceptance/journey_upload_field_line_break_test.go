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

package acceptance

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"rdio-scanner/internal/domain"
)

// A capture client that uploads from a broadcast channel writes its own multipart body instead of leaving
// the framing to a library: it closes every part with a line break of its own and writes one more before
// its closing delimiter. The multipart grammar gives a reader the line break its next boundary line needs,
// so the spare one arrives inside the value of whichever part the body ends with. When that part is the
// claim that a submission is only asking whether the server will take a recording, the claim is lost, the
// submission is read as a recording, and the answer the server puts on the wire is read by the client as an
// all-clear while the server files the refusal as a lost recording. This journey posts that body, and a
// recording framed the same way, through one real server with the real reader, and reads what each one is
// answered, what each one files, and what each one leaves in the call history.

const (
	// framedUploadBoundary is the boundary literal such a client writes, including the dashes its
	// builder puts inside the literal itself: every boundary line in its body begins with four dashes,
	// and the shape of the framing is what this journey is about.
	framedUploadBoundary = "--sdrtrunk-sdrtrunk-sdrtrunk"

	// framedRecordingFile and framedBreakRecordingFile name the two recordings this journey posts, one
	// whose bytes end in a line break and one that does not.
	framedRecordingFile      = "capture-framed-recording.wav"
	framedBreakRecordingFile = "capture-framed-recording-with-a-break.wav"
)

// framedUploadEndpoints are the two endpoints an uploader posts a call upload to: the one a capture client
// posts to, and the one its sidecar posts to. Both read the same body, and both file what they read the
// same way.
var framedUploadEndpoints = []string{"/api/call-upload", "/api/trunk-recorder-call-upload"}

// framedUploadPart is one part of such a body: the name the client gives it, the bytes it writes for it,
// and the file name when the part carries a recording rather than a value.
type framedUploadPart struct {
	name     string
	filename string
	value    string
}

// framedUploadBody writes the body the way such a client writes one: one boundary line and one disposition
// line per part, the value, and the line break that client closes the part with; then, after every part,
// its own lone line break, then its closing delimiter. The recording part is written after every value
// part and carries no line break of its own after its bytes, so the client's lone break is the one the
// reader takes as closing it.
func framedUploadBody(parts ...framedUploadPart) (string, string) {
	body := bytes.Buffer{}

	for _, part := range parts {
		body.WriteString("--" + framedUploadBoundary + "\r\n")

		if part.filename != "" {
			body.WriteString(`Content-Disposition: form-data; filename="` + part.filename + `"; name="audio"` + "\r\n\r\n")
			body.WriteString(part.value)

			continue
		}

		body.WriteString(`Content-Disposition: form-data; name="` + part.name + `"` + "\r\n\r\n")
		body.WriteString(part.value)
		body.WriteString("\r\n")
	}

	body.WriteString("\r\n")
	body.WriteString("--" + framedUploadBoundary + "--\r\n")

	return body.String(), "multipart/form-data; boundary=" + framedUploadBoundary
}

// postFramedUpload posts a body written in that framing to one of the two upload endpoints, which is how
// the client's own bytes reach this server.
func (s *journeyServer) postFramedUpload(t *testing.T, path, body, contentType string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", contentType)

	s.server.ServeHTTP(rec, req)

	return rec
}

// TestUploadSubmissionsPostedInAnUploadersOwnLineBreakFramingAreFiledAsWhatTheyAre walks the whole path one
// owner's deployment takes: a connection check and a recording, both posted in the framing an uploader that
// writes its own body uses, at both upload endpoints of one started server. The answer each submission is
// put on the wire is what the endpoints have always answered it with; what this feature changes is what the
// server files for itself, and the recording's bytes are the bytes that were posted whatever framing carried
// them.
func TestUploadSubmissionsPostedInAnUploadersOwnLineBreakFramingAreFiledAsWhatTheyAre(t *testing.T) {
	srv, _ := configuredTestServer(t)

	// The system the checks name carries a row in the configuration these runs install, and a check naming
	// a configured system is answered the clause that names it, so the switch that would invent a row is
	// turned on for that row rather than left to invent one.
	seed(t, srv.db, seedStatement{
		query: `UPDATE "systems" SET "autoPopulate" = 1 WHERE "systemRef" = 10001`,
	})

	counted := []struct{ what, query string }{
		{"the calls", `SELECT COUNT(*) FROM "calls"`},
		{"the systems", `SELECT COUNT(*) FROM "systems"`},
		{"the talkgroups", `SELECT COUNT(*) FROM "talkgroups"`},
	}

	before := map[string]int{}

	for _, table := range counted {
		before[table.what] = countRows(t, srv.db, table.query)
	}

	// The connection check such a client posts on startup: its credential, its system number, and the claim
	// that the submission is only a test, in that order, so the claim is the part the body ends with and the
	// line break the client wrote beside its closing delimiter arrives inside it.
	for _, path := range framedUploadEndpoints {
		body, contentType := framedUploadBody(
			framedUploadPart{name: "key", value: checkUploadKey},
			framedUploadPart{name: "system", value: strconv.Itoa(checkUploadSystem)},
			framedUploadPart{name: "test", value: "1"},
		)

		answered := srv.postFramedUpload(t, path, body, contentType)

		if answered.Code != http.StatusExpectationFailed {
			t.Errorf("%s was answered %d for a connection check framed as its uploader frames one: %s, want %d with the answer the checking client decides its all-clear on",
				path, answered.Code, answered.Body.String(), http.StatusExpectationFailed)
		}

		if got, want := answered.Body.String(), "Incomplete call data: no talkgroup\n\n"; got != want {
			t.Errorf("%s answered a connection check framed as its uploader frames one with %q, want %q: the bytes on the wire are the contract that client reads, and they do not move",
				path, got, want)
		}
	}

	// The same two checks, read as what they are: each one is filed in the sentence that names itself as a
	// connection check, at the level an operator reads a routine connection check at, and under the upload
	// endpoint that received it.
	for _, message := range []string{
		"the connection check was refused: no talkgroup, presented by the apikey " + checkUploadIdent + " for system 10001",
	} {
		if rows := countLogLines(t, srv.db, message); rows != 2 {
			t.Errorf("the server filed %q %d times, want it filed once for each of the two endpoints this run checked: a submission that asked whether this server will take a recording is filed as the check it is",
				message, rows)

			continue
		}

		if got := logLevelOfMessage(t, srv.db, message); got != domain.LogLevelInfo {
			t.Errorf("%q is filed at %q, want %s: a connection check is read where a routine upload problem is read, not where one is raised",
				message, got, domain.LogLevelInfo)
		}

		if got := logSourceOfMessage(t, srv.db, message); got != string(domain.LogSourceUpload) {
			t.Errorf("%q is filed under %q, want %s", message, got, domain.LogSourceUpload)
		}
	}

	// Nothing this run posted is a recording short of a talkgroup, so no entry is filed in the sentence a
	// lost recording is filed in, and the only entries naming a check are the two checks themselves.
	if rows := countLogLines(t, srv.db, "the upload was refused: Incomplete call data: no talkgroup"); rows != 0 {
		t.Errorf("the server filed the lost-recording sentence %d times over a run that posted no recording short of a talkgroup: a connection check framed as its uploader frames one is being filed as a lost recording", rows)
	}

	// The two recordings, each posted after the values the client names and framed the same way: one whose
	// bytes do not end in a line break, and one whose bytes do, which is a byte of the recording and not a
	// line break closing a part.
	for _, posted := range []struct {
		path     string
		filename string
		moment   string
		audio    []byte
	}{
		{
			path:     "/api/call-upload",
			filename: framedRecordingFile,
			moment:   "1763471911",
			audio:    recording(2048),
		},
		{
			path:     "/api/trunk-recorder-call-upload",
			filename: framedBreakRecordingFile,
			moment:   "1763471971",
			audio:    append(recording(2048), "\r\n"...),
		},
	} {
		body, contentType := framedUploadBody(
			append([]framedUploadPart{
				{name: "key", value: checkUploadKey},
				{name: "system", value: strconv.Itoa(checkUploadSystem)},
				{name: "dateTime", value: posted.moment},
				{name: "talkgroup", value: "1514000"},
			}, framedUploadPart{filename: posted.filename, value: string(posted.audio)})...,
		)

		accepted := srv.postFramedUpload(t, posted.path, body, contentType)

		if accepted.Code != http.StatusOK {
			t.Fatalf("%s answered a recording framed as its uploader frames one with %d: %s, want it accepted",
				posted.path, accepted.Code, accepted.Body.String())
		}

		if got, want := accepted.Body.String(), "Call imported successfully.\n"; got != want {
			t.Errorf("%s answered a recording framed as its uploader frames one with %q, want %q", posted.path, got, want)
		}

		var stored []byte

		if err := srv.db.QueryRow(`SELECT "audio" FROM "calls" WHERE "audioFilename" = ?`, posted.filename).Scan(&stored); err != nil {
			t.Fatalf("%s left no recording named %s in the call history: %v", posted.path, posted.filename, err)
		}

		if !bytes.Equal(stored, posted.audio) {
			t.Errorf("%s stored %d bytes for %s, want the %d bytes that were posted for it: a posted recording is the uploader's own bytes, and a line break at the end of them is a byte of the recording rather than a line break that closes a part",
				posted.path, len(stored), posted.filename, len(posted.audio))
		}

		if rows := countLogLines(t, srv.db, "call from system 10001 talkgroup 1514000 success, "+posted.filename); rows != 1 {
			t.Errorf("the server filed the taken-recording entry for %s %d times, want once: the recording this run posted is filed where every taken call is filed", posted.filename, rows)
		}
	}

	// A connection check leaves nothing behind, and the only rows this run added to the call history are the
	// two recordings it posted.
	for _, table := range counted {
		want := before[table.what]

		if table.what == "the calls" {
			want += 2
		}

		if rows := countRows(t, srv.db, table.query); rows != want {
			t.Errorf("%s holds %d rows after the run, want %d: the two checks stored nothing and the two recordings stored themselves",
				table.what, rows, want)
		}
	}

	assertEveryServerLogEntry(t, srv.db)
}
