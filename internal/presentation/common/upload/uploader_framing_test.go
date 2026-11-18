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
	"crypto/sha256"
	"fmt"
	"mime/multipart"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/domain"
)

// The uploaders that post from a broadcast channel write their own multipart bodies rather than leave
// the framing to a library, and they close every part with a line break of their own and write one more
// before their closing delimiter. The multipart grammar gives a reader the line break its next boundary
// line needs, so the second break arrives inside the last part's value: the bytes a field reading is
// handed are the uploader's bytes, and the server reads its answers out of those.
//
// Every other guard in this package builds its body with Go's own multipart writer, which closes a body
// with the one line break the grammar owns. Those guards read what they were framed for, and none of them
// ever posted what these uploaders post.

const (
	// uploaderBoundary is the boundary literal such an uploader writes. It is built out of dashes and
	// the uploader's own name, so every boundary line in its body begins with four dashes: the shape is
	// what the guards below read, and the name the uploader wrote inside it is not.
	uploaderBoundary = "--sdrtrunk-sdrtrunk-sdrtrunk"
)

// postedUnixSeconds is the shape of a Unix second count, which is how an uploader stamps its submission.
var postedUnixSeconds = regexp.MustCompile(`^[0-9]+$`)

// uploaderFramedBody writes a multipart body the way such an uploader writes one: one boundary line and
// one disposition line per part, the value, and the line break that uploader closes the part with; then
// after every part its own lone line break, then its closing delimiter. The file part is the recording,
// named by the uploader as audio and written after every field part, so the recording's bytes are the
// last thing before the closing delimiter when the body carries one.
func uploaderFramedBody(t *testing.T, fields ...uploadField) (string, string) {
	t.Helper()

	return uploaderFramedBodyWithLineEnd(t, "\r\n", fields...)
}

// uploaderFramedBodyWithLineEnd writes that body with each of its line endings written as the given one,
// which is how a body framed with line feeds instead of the pair the grammar names is posted: the same
// shape, with the spare break carrying the shorter ending.
func uploaderFramedBodyWithLineEnd(t *testing.T, lineEnd string, fields ...uploadField) (string, string) {
	t.Helper()

	body := bytes.Buffer{}

	for _, field := range fields {
		body.WriteString("--" + uploaderBoundary + lineEnd)

		if len(field.filename) > 0 {
			// The recording part names itself audio whatever it was posted as, and the uploader writes
			// no line break after the recording bytes it puts in it: the part's own closing break is the
			// lone one it writes before its closing delimiter.
			body.WriteString(`Content-Disposition: form-data; filename="` + field.filename + `"; name="audio"` + lineEnd + lineEnd)
			body.WriteString(field.value)

			continue
		}

		body.WriteString(`Content-Disposition: form-data; name="` + field.name + `"` + lineEnd + lineEnd)
		body.WriteString(field.value)
		body.WriteString(lineEnd)
	}

	body.WriteString(lineEnd)
	body.WriteString("--" + uploaderBoundary + "--" + lineEnd)

	return body.String(), "multipart/form-data; boundary=" + uploaderBoundary
}

// valueReading is the field reading for the guards that are about what a posted value reaches the field
// reading as: it builds the call out of the bytes each part was handed, the way the server's own reader
// does, so a value carrying a line break the uploader wrote reads as the bytes it is and not as what the
// field was meant to carry. The claim rule it applies is the one the server's reader applies, which that
// reader's own suite proves for every posted field; what these guards prove is what the upload endpoints
// hand it and what they answer from the call that reading built.
type valueReading struct {
	// Call is the call as the reading left it after the field it last read, which is the call the
	// endpoints hand the use case that decides the submission.
	Call domain.Call
}

func (reading *valueReading) apply(part *multipart.Part, data []byte, call domain.Call) (domain.Call, string) {
	value := string(data)

	switch part.FormName() {
	case "test":
		if value == "1" || value == "true" {
			call.Meta.ConnectionCheck = true
		}

	case "system":
		if ref, err := strconv.Atoi(value); err == nil && ref > 0 {
			call.Meta.SystemRef = uint(ref)
		}

	case "systemLabel":
		// Whatever the part carried reaches the call, which is what the server's own reader does for the label
		// a submission names its system by.
		call.Meta.SystemLabel = value

	case "talkgroup":
		if ref, err := strconv.Atoi(value); err == nil && ref > 0 {
			call.Meta.TalkgroupRef = uint(ref)
		}

	case "talkgroupLabel":
		if value != "" && value != "-" {
			call.Meta.TalkgroupLabel = value
		}

	case "dateTime":
		if postedUnixSeconds.Match(data) {
			if seconds, err := strconv.Atoi(value); err == nil {
				call.Timestamp = time.Unix(int64(seconds), 0)
			}
		}

	case "timestamp":
		if millis, err := strconv.Atoi(value); err == nil {
			call.Timestamp = time.UnixMilli(int64(millis))
		}

	case "audio":
		call.Audio = data
		call.AudioFilename = part.FileName()

	case "audioFilename":
		call.AudioFilename = value

	case "audioMime":
		call.AudioMime = value
	}

	reading.Call = call

	return call, ""
}

// valuesHandedToTheReading renders the fields and the bytes the endpoints handed the field reading, in the
// order they arrived, so a failure names which value carries a line break the uploader wrote twice. The names
// it is given beside the parser are left out of the rendering: a recording's bytes are read against what was
// posted, and not written out into a failure message.
func valuesHandedToTheReading(parser *recordingUploadParser, except ...string) string {
	skip := map[string]bool{}

	for _, name := range except {
		skip[name] = true
	}

	readings := []string{}

	for at, field := range parser.Fields {
		if skip[field] {
			continue
		}

		value := ""

		if at < len(parser.Bodies) {
			value = string(parser.Bodies[at])
		}

		readings = append(readings, field+"="+strconv.Quote(value))
	}

	return strings.Join(readings, " ")
}

// bytesHandedForTheField returns the bytes the endpoints handed the field reading for one posted part, and
// whether that part reached the reading at all.
func bytesHandedForTheField(parser *recordingUploadParser, name string) ([]byte, bool) {
	for at, field := range parser.Fields {
		if field != name {
			continue
		}

		if at >= len(parser.Bodies) {
			return nil, false
		}

		return parser.Bodies[at], true
	}

	return nil, false
}

// uploaderRecordingLength is the byte count of the recording these guards post: long enough to be a recording
// rather than a bare container header, which is what the model reads a submission with less as.
const uploaderRecordingLength = 900

// uploaderRecordingBytes is that recording: bytes a container would carry, so that what these guards read about
// them is about their length and their framing and not about what they hold.
func uploaderRecordingBytes() []byte {
	audio := make([]byte, uploaderRecordingLength)

	for at := range audio {
		audio[at] = byte('A' + at%26)
	}

	return audio
}

// uploaderRecordingWithOwnBreak is the same recording with a line break of its own at the end of its bytes: the
// shape an uploader's file framing hands a reader when the recording it carries ends with a line break, which is
// a break inside the recording and not one that closes a part.
func uploaderRecordingWithOwnBreak() []byte {
	return append(uploaderRecordingBytes(), "\r\n"...)
}

// uploaderRecordingFields are the parts such an uploader posts for a recording, in the order it posts them: its
// credential, one system number, the moment the recording started as a Unix second count, one talkgroup number,
// and the labels and lists it knows about the call. The recording itself is posted after all of them.
func uploaderRecordingFields(key, moment string) []uploadField {
	return []uploadField{
		{name: "key", value: key},
		{name: "system", value: "10001"},
		{name: "dateTime", value: moment},
		{name: "talkgroup", value: "1514000"},
		{name: "source", value: "1234"},
		{name: "frequency", value: "151400000"},
		{name: "talkgroupLabel", value: "Dispatch"},
		{name: "talkgroupGroup", value: "Radio"},
		{name: "systemLabel", value: "Test System"},
		{name: "patches", value: "[]"},
	}
}

// TestUploadEndpointsAnswerAConnectionCheckWhoseClaimCarriesASecondLineBreak proves that a connection
// check posted in the framing one uploader writes for itself is read as the connection check it says it
// is. The claim arrives as the digit one with a line break on the end of it, which is not what the claim
// rule reads; the submission is then read as a recording that names no talkgroup, and the refusal an
// operator reads is the one for a recording that was lost. The bytes the client is answered stay the
// bytes that client reads as a working server.
func TestUploadEndpointsAnswerAConnectionCheckWhoseClaimCarriesASecondLineBreak(t *testing.T) {
	for _, path := range uploadEndpointPaths {
		t.Run(strings.TrimPrefix(path, "/api/")+" connection check an uploader framed itself", func(t *testing.T) {
			reading := &valueReading{}

			fixture := newUploadFixture()

			fixture.seedUploadSystem()
			fixture.parser.Apply = reading.apply

			body, contentType := uploaderFramedBody(t, connectionCheckFields(uploadOpenKey, "10001")...)

			rec := fixture.post(path, contentType, body)

			if got, want := valuesHandedToTheReading(fixture.parser), `system="10001" test="1"`; got != want {
				t.Errorf("%s handed the field reading %s, want %s: that uploader closes every part with a line "+
					"break of its own and writes another one before its closing delimiter, and the grammar gives the "+
					"reader the break its next boundary line needs, so the spare one lands in the value of whichever "+
					"part the body ends with", path, got, want)
			}

			if rec.Code != http.StatusExpectationFailed {
				t.Fatalf("%s status = %d, want 417: %s", path, rec.Code, rec.Body.String())
			}

			if got, want := rec.Body.String(), "Incomplete call data: no talkgroup\n\n"; got != want {
				t.Errorf("%s answer = %q, want %q: a submission that only asks whether this server will take a "+
					"recording brings no recording, and it is answered for that in the bytes an uploader reads as a "+
					"working server whatever framing its body was written in", path, got, want)
			}

			assertFiledAt(t, path+" connection check", fixture.uploadEntries(), domain.LogLevelInfo,
				"the connection check was refused: no talkgroup, presented by the apikey Capture open for system 10001",
				"the server refused a submission that asked whether it will take a recording, on purpose, and says so "+
					"as a check: an operator reading this line between two upload attempts is reading the answer a client "+
					"got, and not a recording this server failed to take")

			fixture.assertStamped(t, "the connection check was refused")
		})
	}
}

// uploaderFramedCheckCase is one connection check posted in the uploader's own framing, with the part the
// duplicated line break lands on named by where that part sits in the body. Every answer and entry here is
// the one the same fields carry in the framing the multipart grammar owns, because the framing is what these
// cases are about and not the fields: what the server reads of a value must not depend on how the body that
// carried it was written.
type uploaderFramedCheckCase struct {
	// label names which part carries the duplicated break.
	label string

	// fields are the parts in the order they are posted, which is what decides which one carries the break.
	fields []uploadField

	// values are the bytes each part that reaches the field reading should reach it as. The credential is
	// the one part the endpoints read themselves and never hand over, so a case whose credential carries the
	// break cannot witness it here; it witnesses it in the answer and the entry instead.
	values string

	status int
	answer string

	// level and message are how the server files the refusal it answered.
	level   string
	message string
}

// runUploaderFramedCheckCases posts each case at both upload endpoints and reads what each one produces: the
// bytes that reach the field reading, the answer status and body, the entry the endpoint filed, and that
// nothing was stored for a submission that only asked whether this server will take a recording.
func runUploaderFramedCheckCases(t *testing.T, cases []uploaderFramedCheckCase) {
	t.Helper()

	for _, path := range uploadEndpointPaths {
		for _, testCase := range cases {
			label := strings.TrimPrefix(path, "/api/") + " connection check with " + testCase.label

			t.Run(label, func(t *testing.T) {
				reading := &valueReading{}

				fixture := newUploadFixture()

				fixture.seedUploadSystem()
				fixture.parser.Apply = reading.apply

				body, contentType := uploaderFramedBody(t, testCase.fields...)

				rec := fixture.post(path, contentType, body)

				if got, want := valuesHandedToTheReading(fixture.parser), testCase.values; got != want {
					t.Errorf("%s handed the field reading %s, want %s: the line break this uploader writes "+
						"before its closing delimiter is one break too many, and the one the grammar owns goes to "+
						"the boundary line, so the spare one arrives inside the value of whichever part the body "+
						"ends with", label, got, want)
				}

				if rec.Code != testCase.status {
					t.Fatalf("%s status = %d, want %d: %s", label, rec.Code, testCase.status, rec.Body.String())
				}

				if got, want := rec.Body.String(), testCase.answer; got != want {
					t.Errorf("%s answer = %q, want %q: the same fields framed the way the grammar frames them "+
						"are answered these bytes, so a refusal that changes with the framing a body was written "+
						"in answers a client something other than what it asked", label, got, want)
				}

				entries := fixture.uploadEntries()

				assertFiledAt(t, label, entries, testCase.level, testCase.message,
					"the level and the sentence are what an operator reads an answer as: a submission that asked "+
						"whether this server will take a recording is filed as the check it is, and a submission "+
						"whose credential this server does not accept is filed as the credential refusal it is")

				for _, entry := range entries {
					for _, field := range testCase.fields {
						if field.name == "key" && len(field.value) > 0 && strings.Contains(entry.Message, field.value) {
							t.Errorf("%s entry = %q, which carries the key string %s: an entry names who a "+
								"credential is, never the string that opens the door", label, entry.Message, field.value)
						}
					}
				}

				if len(fixture.calls.Saved) != 0 {
					t.Errorf("%s stored %v, want nothing stored for a submission that only asked whether this "+
						"server will take a recording", label, fixture.calls.Saved)
				}

				fixture.assertStamped(t, strings.SplitN(testCase.message, ", presented", 2)[0])
			})
		}
	}
}

// TestUploadEndpointsReadTheFieldCarryingAnUploadersSecondLineBreak proves that the part a body happens to
// end with is read as the value it posted, whatever that part is for. The claim, the system number, the
// credential, the talkgroup number, and the moment each carry the duplicated break in turn: each one is
// posted last in a body whose every other part arrives cleanly, and each refusal is the one the same fields
// posted in the framing the grammar owns earn.
func TestUploadEndpointsReadTheFieldCarryingAnUploadersSecondLineBreak(t *testing.T) {
	runUploaderFramedCheckCases(t, []uploaderFramedCheckCase{
		{
			label:   "the claim written as the word true",
			fields:  []uploadField{{name: "key", value: uploadOpenKey}, {name: "system", value: "10001"}, {name: "test", value: "true"}},
			values:  `system="10001" test="true"`,
			status:  http.StatusExpectationFailed,
			answer:  "Incomplete call data: no talkgroup\n\n",
			level:   domain.LogLevelInfo,
			message: "the connection check was refused: no talkgroup, presented by the apikey Capture open for system 10001",
		},
		{
			label:   "the system number written last",
			fields:  []uploadField{{name: "key", value: uploadOpenKey}, {name: "test", value: "1"}, {name: "system", value: "10001"}},
			values:  `test="1" system="10001"`,
			status:  http.StatusExpectationFailed,
			answer:  "Incomplete call data: no talkgroup\n\n",
			level:   domain.LogLevelInfo,
			message: "the connection check was refused: no talkgroup, presented by the apikey Capture open for system 10001",
		},
		{
			label:   "the credential written last",
			fields:  []uploadField{{name: "system", value: "10001"}, {name: "test", value: "1"}, {name: "key", value: uploadOpenKey}},
			values:  `system="10001" test="1"`,
			status:  http.StatusExpectationFailed,
			answer:  "Incomplete call data: no talkgroup\n\n",
			level:   domain.LogLevelInfo,
			message: "the connection check was refused: no talkgroup, presented by the apikey Capture open for system 10001",
		},
		{
			label: "a call field written last",
			fields: []uploadField{
				{name: "key", value: uploadOpenKey},
				{name: "test", value: "1"},
				{name: "system", value: "10001"},
				{name: "talkgroup", value: "1514000"},
				{name: "dateTime", value: "1763471911"},
			},
			values:  `test="1" system="10001" talkgroup="1514000" dateTime="1763471911"`,
			status:  http.StatusExpectationFailed,
			answer:  "Incomplete call data: no audio\n\n",
			level:   domain.LogLevelInfo,
			message: "the connection check was refused: no audio, presented by the apikey Capture open for system 10001",
		},
		{
			label: "the talkgroup number written last",
			fields: []uploadField{
				{name: "key", value: uploadOpenKey},
				{name: "system", value: "10001"},
				{name: "test", value: "1"},
				{name: "talkgroup", value: "1514000"},
			},
			values:  `system="10001" test="1" talkgroup="1514000"`,
			status:  http.StatusExpectationFailed,
			answer:  "Incomplete call data: no audio\n\n",
			level:   domain.LogLevelInfo,
			message: "the connection check was refused: no audio, presented by the apikey Capture open for system 10001",
		},
		{
			label: "the claim written last beside a talkgroup the server knows",
			fields: []uploadField{
				{name: "key", value: uploadOpenKey},
				{name: "system", value: "10001"},
				{name: "talkgroup", value: "1514000"},
				{name: "test", value: "1"},
			},
			values:  `system="10001" talkgroup="1514000" test="1"`,
			status:  http.StatusExpectationFailed,
			answer:  "Incomplete call data: no audio\n\n",
			level:   domain.LogLevelInfo,
			message: "the connection check was refused: no audio, presented by the apikey Capture open for system 10001",
		},
	})
}

// TestUploadEndpointsRefuseAnUploadersCredentialThatCarriesASecondLineBreak proves that a check whose
// credential this server does not accept is refused for that credential, and not for what the submission is
// short of. Reading the claim as the claim it posted reaches the gate every well-framed body in the same
// credential state reaches today, and the answer the sender is given is the one that says the sender is the
// problem: a broadcaster with a key it got wrong reads a refusal it can act on rather than an all-clear.
func TestUploadEndpointsRefuseAnUploadersCredentialThatCarriesASecondLineBreak(t *testing.T) {
	cases := []uploaderFramedCheckCase{
		{
			label:   "a credential this server holds no row for",
			fields:  []uploadField{{name: "key", value: "an-apikey-this-server-holds-no-row-for"}, {name: "system", value: "10001"}, {name: "test", value: "1"}},
			values:  `system="10001" test="1"`,
			status:  http.StatusUnauthorized,
			answer:  capturedAnswer(t, "rest-upload-denied.txt"),
			level:   domain.LogLevelError,
			message: "the upload was refused: the apikey is not valid",
		},
		{
			label:   "a credential this server holds and has switched off",
			fields:  []uploadField{{name: "key", value: uploadDisabledKey}, {name: "system", value: "10001"}, {name: "test", value: "1"}},
			values:  `system="10001" test="1"`,
			status:  http.StatusUnauthorized,
			answer:  capturedAnswer(t, "rest-upload-denied.txt"),
			level:   domain.LogLevelError,
			message: "the upload was refused: the apikey is not valid",
		},
		{
			label:   "no credential part at all",
			fields:  []uploadField{{name: "system", value: "10001"}, {name: "test", value: "1"}},
			values:  `system="10001" test="1"`,
			status:  http.StatusUnauthorized,
			answer:  capturedAnswer(t, "rest-upload-denied.txt"),
			level:   domain.LogLevelError,
			message: "the upload was refused: the apikey is not valid",
		},
	}

	for _, path := range uploadEndpointPaths {
		for _, testCase := range cases {
			label := strings.TrimPrefix(path, "/api/") + " connection check with " + testCase.label

			t.Run(label, func(t *testing.T) {
				reading := &valueReading{}

				fixture := newUploadFixture()

				fixture.seedUploadSystem()
				fixture.parser.Apply = reading.apply

				body, contentType := uploaderFramedBody(t, testCase.fields...)

				rec := fixture.post(path, contentType, body)

				if got, want := valuesHandedToTheReading(fixture.parser), testCase.values; got != want {
					t.Errorf("%s handed the field reading %s, want %s: the claim is what tells the endpoints "+
						"whose gate to stand at, and the spare line break this uploader writes turns it into an "+
						"ordinary posted value", label, got, want)
				}

				if rec.Code != testCase.status {
					t.Fatalf("%s status = %d, want %d: %s", label, rec.Code, testCase.status, rec.Body.String())
				}

				if got, want := rec.Body.String(), testCase.answer; got != want {
					t.Errorf("%s answer = %q, want %q: a sender this server does not accept is refused for that, "+
						"in the bytes a refused upload carrying the same credential has always been given", label, got, want)
				}

				entries := fixture.uploadEntries()

				assertFiledAt(t, label, entries, testCase.level, testCase.message,
					"the credential is what stopped this submission, which is what the entry has to say")

				if named := fixture.entriesNamingACheck(); len(named) != 0 {
					t.Errorf("%s filed %v, want no entry naming a check: the server refused the credential before "+
						"it ever asked what the submission was short of", label, named)
				}

				if len(fixture.calls.Saved) != 0 {
					t.Errorf("%s stored %v, want nothing stored for a refused credential", label, fixture.calls.Saved)
				}

				fixture.assertStamped(t, "the apikey is not valid")
			})
		}
	}
}

// TestUploadEndpointsTakeARecordingFramedByAnUploaderThatWritesItsOwnLineBreaks proves that a recording posted
// in the uploader's own framing is stored as the bytes the uploader posted them as, whether or not the recording
// itself ends in a line break. A body that carries a recording puts the recording part last, so the one line
// break the grammar owns comes off the end of the recording and every field value arrives exactly as it was
// written; a recording that ends in a line break then carries that break as its own last byte. The server stores
// the bytes, hashes to them, and answers the same bytes for them as it answers for the same recording framed any
// other way.
func TestUploadEndpointsTakeARecordingFramedByAnUploaderThatWritesItsOwnLineBreaks(t *testing.T) {
	cases := []struct {
		label    string
		moment   string
		filename string
		audio    []byte
	}{
		{
			label:    "a recording that does not end in a line break",
			moment:   "1763471911",
			filename: "call_20261118.mp3",
			audio:    uploaderRecordingBytes(),
		},
		{
			label:    "a recording whose own bytes end in a line break",
			moment:   "1763471971",
			filename: "call_20261119.mp3",
			audio:    uploaderRecordingWithOwnBreak(),
		},
	}

	for _, path := range uploadEndpointPaths {
		for _, testCase := range cases {
			label := strings.TrimPrefix(path, "/api/") + " recording with " + testCase.label

			t.Run(label, func(t *testing.T) {
				reading := &valueReading{}

				fixture := newUploadFixture()

				fixture.seedUploadSystem()
				fixture.parser.Apply = reading.apply

				fields := append(uploaderRecordingFields(uploadOpenKey, testCase.moment), uploadField{
					name:     "audio",
					filename: testCase.filename,
					value:    string(testCase.audio),
				})

				body, contentType := uploaderFramedBody(t, fields...)

				rec := fixture.post(path, contentType, body)

				want := `system="10001" dateTime="` + testCase.moment + `" talkgroup="1514000" source="1234"` +
					` frequency="151400000" talkgroupLabel="Dispatch" talkgroupGroup="Radio"` +
					` systemLabel="Test System" patches="[]"`

				if got := valuesHandedToTheReading(fixture.parser, "audio"); got != want {
					t.Errorf("%s handed the field reading %s, want %s: a body that carries a recording ends with "+
						"that recording, so the one line break the grammar owns comes off the end of it and every "+
						"field before it reaches the reading with only the break that closes its own part", label, got, want)
				}

				handed, ok := bytesHandedForTheField(fixture.parser, "audio")

				if !ok {
					t.Fatalf("%s handed the field reading no recording at all: %v", label, fixture.parser.Fields)
				}

				if got, want := sha256.Sum256(handed), sha256.Sum256(testCase.audio); got != want {
					t.Errorf("%s handed the field reading %d bytes hashing %x, want %d bytes hashing %x: a posted "+
						"recording is the uploader's own bytes, and a line break at the end of them is a byte of the "+
						"recording, not a line break that closes a part", label, len(handed), got, len(testCase.audio), want)
				}

				if got, want := fixture.parser.FileNames[len(fixture.parser.Fields)-1], testCase.filename; got != want {
					t.Errorf("%s handed the field reading a recording named %q, want %q: the uploader names its own "+
						"recording, and the server keeps that name", label, got, want)
				}

				if rec.Code != http.StatusOK {
					t.Fatalf("%s status = %d, want 200: %s", label, rec.Code, rec.Body.String())
				}

				if got, want := rec.Body.String(), capturedAnswer(t, capturedUploadAnswers[path]); got != want {
					t.Errorf("%s answer = %q, want %q: a recording this server took is answered the bytes a taken "+
						"upload has always been answered with, whatever framing its body was written in", label, got, want)
				}

				if len(fixture.calls.Saved) != 1 {
					t.Fatalf("%s stored %v, want the one call the recording describes: %v", label, fixture.calls.Saved, fixture.calls.Saved)
				}

				stored := fixture.calls.Saved[0]

				if got, want := sha256.Sum256(stored.Audio), sha256.Sum256(testCase.audio); got != want {
					t.Errorf("%s stored %d bytes hashing %x, want the %d bytes that were posted hashing %x: the bytes "+
						"an uploader posted are the bytes the server keeps", label, len(stored.Audio), got,
						len(testCase.audio), want)
				}

				if stored.System.SystemRef != uploadSystemRef {
					t.Errorf("%s stored a call on system %d, want system %d", label, stored.System.SystemRef, uploadSystemRef)
				}

				if stored.Talkgroup.TalkgroupRef != uploadTalkgroupRef {
					t.Errorf("%s stored a call on talkgroup %d, want talkgroup %d", label, stored.Talkgroup.TalkgroupRef, uploadTalkgroupRef)
				}

				if entries := fixture.uploadEntries(); len(entries) != 0 {
					t.Errorf("%s filed %v with the upload endpoint, want nothing filed for an upload it took", label, entries)
				}
			})
		}
	}
}

// valuesTheCallHolds renders the parts of the call the endpoints hand the use case that a value the field
// reading was handed decides: the claim, the system number, and the label the submission named its system by.
func valuesTheCallHolds(call domain.Call) string {
	return fmt.Sprintf("claim=%v system=%d label=%q",
		call.Meta.ConnectionCheck, call.Meta.SystemRef, call.Meta.SystemLabel)
}

// TestUploadEndpointsRemoveOneTrailingLineBreakFromAPostedValueAndNothingElse proves how wide the reading rule
// is. One trailing line break comes off a posted value, and that is all: a claim whose value carries a space, a
// leading break, or a second break is no more a claim than the same value without a break beside it; a number
// with a space in front of its break is no number; a label keeps a break that sits inside it. A value that
// reaches the reading as the bytes it was posted as is what every answer and every stored call is decided from,
// so the width of this one rule is the width of what the server takes a submission to be.
func TestUploadEndpointsRemoveOneTrailingLineBreakFromAPostedValueAndNothingElse(t *testing.T) {
	cases := []struct {
		label   string
		field   string
		value   string
		lineEnd string
		handed  string
		call    string
	}{
		{
			label:   `a claim posted as the digit one, one line break arriving with it`,
			field:   "test",
			value:   "1",
			lineEnd: "\r\n",
			handed:  "1",
			call:    `claim=true system=10001 label=""`,
		},
		{
			label:   `a claim posted as the digit one, one line feed arriving with it`,
			field:   "test",
			value:   "1",
			lineEnd: "\n",
			handed:  "1",
			call:    `claim=true system=10001 label=""`,
		},
		{
			label:   `a claim carrying a space before the break that arrives with it`,
			field:   "test",
			value:   "1 ",
			lineEnd: "\r\n",
			handed:  "1 ",
			call:    `claim=false system=10001 label=""`,
		},
		{
			label:   `a claim leading with a line break`,
			field:   "test",
			value:   "\r\n1",
			lineEnd: "\r\n",
			handed:  "\r\n1",
			call:    `claim=false system=10001 label=""`,
		},
		{
			label:   `a claim carrying a line break of its own beside the one that arrives with it`,
			field:   "test",
			value:   "1\r\n",
			lineEnd: "\r\n",
			handed:  "1\r\n",
			call:    `claim=false system=10001 label=""`,
		},
		{
			label:   `a claim posted as the digit zero`,
			field:   "test",
			value:   "0",
			lineEnd: "\r\n",
			handed:  "0",
			call:    `claim=false system=10001 label=""`,
		},
		{
			label:   `a claim posted as the word false`,
			field:   "test",
			value:   "false",
			lineEnd: "\r\n",
			handed:  "false",
			call:    `claim=false system=10001 label=""`,
		},
		{
			label:   `a claim posted as a word that is not the one the rule reads`,
			field:   "test",
			value:   "yes",
			lineEnd: "\r\n",
			handed:  "yes",
			call:    `claim=false system=10001 label=""`,
		},
		{
			label:   `a claim posted as nothing at all`,
			field:   "test",
			value:   "",
			lineEnd: "\r\n",
			handed:  "",
			call:    `claim=false system=10001 label=""`,
		},
		{
			label:   `a system number posted as digits, one line break arriving with it`,
			field:   "system",
			value:   "10001",
			lineEnd: "\r\n",
			handed:  "10001",
			call:    `claim=true system=10001 label=""`,
		},
		{
			label:   `a system number carrying a space before the break that arrives with it`,
			field:   "system",
			value:   "10001 ",
			lineEnd: "\r\n",
			handed:  "10001 ",
			call:    `claim=true system=0 label=""`,
		},
		{
			label:   `a system number carrying a line break of its own beside the one that arrives with it`,
			field:   "system",
			value:   "10001\r\n",
			lineEnd: "\r\n",
			handed:  "10001\r\n",
			call:    `claim=true system=0 label=""`,
		},
		{
			label:   `a system label posted as letters, one line break arriving with it`,
			field:   "systemLabel",
			value:   "Dispatch",
			lineEnd: "\r\n",
			handed:  "Dispatch",
			call:    `claim=true system=10001 label="Dispatch"`,
		},
		{
			label:   `a system label carrying a line break inside it`,
			field:   "systemLabel",
			value:   "Dispatch\nRadio",
			lineEnd: "\r\n",
			handed:  "Dispatch\nRadio",
			call:    `claim=true system=10001 label="Dispatch\nRadio"`,
		},
	}

	for _, path := range uploadEndpointPaths {
		for _, testCase := range cases {
			label := strings.TrimPrefix(path, "/api/") + " " + testCase.label

			t.Run(label, func(t *testing.T) {
				reading := &valueReading{}

				fixture := newUploadFixture()

				fixture.seedUploadSystem()
				fixture.parser.Apply = reading.apply

				fields := []uploadField{{name: "key", value: uploadOpenKey}}

				switch testCase.field {
				case "test":
					fields = append(fields, uploadField{name: "system", value: "10001"},
						uploadField{name: "test", value: testCase.value})

				case "system":
					fields = append(fields, uploadField{name: "test", value: "1"},
						uploadField{name: "system", value: testCase.value})

				default:
					fields = append(fields, uploadField{name: "system", value: "10001"},
						uploadField{name: "test", value: "1"},
						uploadField{name: testCase.field, value: testCase.value})
				}

				body, contentType := uploaderFramedBodyWithLineEnd(t, testCase.lineEnd, fields...)

				fixture.post(path, contentType, body)

				handed, ok := bytesHandedForTheField(fixture.parser, testCase.field)

				if !ok {
					t.Fatalf("%s handed the field reading no %s part at all: %v", label, testCase.field, fixture.parser.Fields)
				}

				if got, want := string(handed), testCase.handed; got != want {
					t.Errorf("%s left the %s part as %s, want %s: one trailing line break is the whole of what "+
						"comes off a posted value, and every other byte of it is the value", label, testCase.field,
						strconv.Quote(got), strconv.Quote(want))
				}

				if got, want := valuesTheCallHolds(reading.Call), testCase.call; got != want {
					t.Errorf("%s left the call as %s, want %s: what the server takes a submission to be is decided "+
						"by the bytes its reading of a value stops at, which is why the rule cannot reach further "+
						"than the end of it", label, got, want)
				}

				if len(fixture.calls.Saved) != 0 {
					t.Errorf("%s stored %v, want nothing stored: every case here posts a submission with no "+
						"recording in it", label, fixture.calls.Saved)
				}
			})
		}
	}
}
