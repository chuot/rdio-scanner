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
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Endpoints is the surface outside programs post recordings to: radio
// bridges, capture applications, and peer servers. They are guarded
// only by the apikey the upload carries, and they answer in plain text.
type Endpoints struct {
	ingest *application.UploadCallHandler

	// check answers a submission that only asks whether this server will take
	// a recording. That question is read out of the server's configuration and
	// decides nothing, so it is a use case of its own.
	check *application.CheckUploadConnectionQueryHandler

	parser Parser
	logs   ports.LogRepository

	// clock is the server's one clock, injected into the upload writer.
	clock ports.Clock
}

// Deps is what the upload endpoints need: the deciding use case, the use
// case that answers a connection check, the field parser, the log view, and
// the server's clock.
type Deps struct {
	Ingest *application.UploadCallHandler
	Check  *application.CheckUploadConnectionQueryHandler
	Parser Parser
	Logs   ports.LogRepository
	Clock  ports.Clock
}

// Parser is the part of the server that knows what an uploader's
// fields mean. It is declared by the endpoints that ask for it, not by
// the application layer, which must not know the wire format.
type Parser interface {
	// ApplyMultipart reads one posted field into the call being built
	// and returns the call and what the reading had to say: an oversized
	// list is truncated and the endpoint files that warning where an
	// administrator reads it; an unknown field changes nothing.
	ApplyMultipart(part *multipart.Part, data []byte, call domain.Call) (domain.Call, string)

	// ApplyTrunkRecorderSidecar reads the JSON document trunk-recorder writes beside a recording.
	ApplyTrunkRecorderSidecar(call *domain.Call, sidecar []byte) error
}

// New builds the upload endpoints.
func New(deps Deps) *Endpoints {
	return &Endpoints{
		ingest: deps.Ingest,
		check:  deps.Check,
		parser: deps.Parser,
		logs:   deps.Logs,
		clock:  deps.Clock,
	}
}

// Register puts the two upload endpoints on the server's routes.
func (upload *Endpoints) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/call-upload", upload.CallUpload)
	mux.HandleFunc("/api/trunk-recorder-call-upload", upload.TrunkRecorderCallUpload)
}

// uploadAccepted and uploadDenied are the two plain-text answers an
// upload gets once the server has read it and decided. The refusal has
// always named the system and talkgroup as nothing.
const (
	uploadAccepted = "Call imported successfully.\n"
	uploadDenied   = "Invalid API key for system <nil> talkgroup <nil>.\n"

	// maxUploadBytes is the ceiling on the body an upload endpoint will
	// read. The endpoints read the whole body into memory before the
	// credential is checked, so it bounds an anonymous POST.
	maxUploadBytes = 32 << 20
)

// CallUpload takes a call an outside program posted as a set of form
// fields. Every field is optional and each is read in the order it
// arrived, so a later field wins an earlier one.
func (upload *Endpoints) CallUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		upload.refuseMethod(w)

		return
	}

	call, key, ok := upload.readCall(w, r, false)
	if !ok {
		return
	}

	upload.answer(w, r, key, call)
}

// TrunkRecorderCallUpload takes a call from trunk-recorder: a JSON
// sidecar beside the recording, read as a document and not as a form
// field, so a sidecar upload can name what the sidecar itself omits.
func (upload *Endpoints) TrunkRecorderCallUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		upload.refuseMethod(w)

		return
	}

	call, key, ok := upload.readCall(w, r, true)
	if !ok {
		return
	}

	upload.answer(w, r, key, call)
}

// readCall reads the posted fields and hands back the call they
// describe and the credential the upload carried. The credential
// never reaches the parser. Each posted value is read with one
// trailing line break taken off it, which is the one an uploader that
// frames its own body leaves behind, and the part that carries the
// recording is read as it was posted. The reported failures are the
// legacy server's own, written out per endpoint, as the frozen contract
// fixes.
func (upload *Endpoints) readCall(w http.ResponseWriter, r *http.Request, trunkRecorder bool) (domain.Call, string, bool) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		upload.fail(w, http.StatusBadRequest, "Invalid content-type")

		return domain.Call{}, "", false
	}

	if !strings.HasPrefix(mediaType, "multipart/") {
		upload.fail(w, http.StatusBadRequest, "Not a multipart content")

		return domain.Call{}, "", false
	}

	var (
		call   = domain.NewCall()
		key    string
		reader = multipart.NewReader(http.MaxBytesReader(w, r.Body, maxUploadBytes), params["boundary"])
	)

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}

		if err != nil {
			upload.fail(w, http.StatusExpectationFailed, upload.readFailure(trunkRecorder, "multipart", err))

			return domain.Call{}, "", false
		}

		body, err := io.ReadAll(part)
		if err != nil {
			upload.fail(w, http.StatusExpectationFailed, upload.readFailure(trunkRecorder, "ioread", err))

			return domain.Call{}, "", false
		}

		name := part.FormName()

		// The uploaders that write their own bodies close every part with a line break of their own and
		// write one more before their closing delimiter. The grammar gives the reader the break the next
		// boundary line needs, so the spare one arrives inside the value of whichever part the body ends
		// with, and one trailing line break is taken off it. The part that carries the recording is read
		// as it was posted: a line break at the end of a recording is a byte of that recording, not a line
		// break that closes a part.
		if name != "audio" {
			body = trimTrailingLineEnd(body)
		}

		var warning string

		switch name {
		case "key":
			key = string(body)

		case "meta":
			if !trunkRecorder {
				call, warning = upload.parser.ApplyMultipart(part, body, call)
				upload.fileTruncation(warning)

				break
			}

			if err := upload.parser.ApplyTrunkRecorderSidecar(&call, body); err != nil {
				// The legacy server names no reason here; the real reason is logged.
				upload.fail(w, http.StatusExpectationFailed, "Invalid call data")

				return domain.Call{}, "", false
			}

		default:
			call, warning = upload.parser.ApplyMultipart(part, body, call)
			upload.fileTruncation(warning)
		}
	}

	return call, key, true
}

// trimTrailingLineEnd takes one line ending off the end of a posted value, and only one: an uploader that
// writes its own body closes the part it ends with a line break of its own, and the break the reader needs
// for its next boundary line arrives inside that value. A value that ends with no line ending, or that ends
// with more than one, is returned as it was read; the line endings inside a value are part of the value.
func trimTrailingLineEnd(value []byte) []byte {
	if len(value) == 0 || value[len(value)-1] != '\n' {
		return value
	}

	value = value[:len(value)-1]

	if len(value) > 0 && value[len(value)-1] == '\r' {
		value = value[:len(value)-1]
	}

	return value
}

// readFailure wraps the failure of one step of reading the posted body, the way each endpoint has always
// wrapped it.
func (upload *Endpoints) readFailure(trunkRecorder bool, step string, err error) string {
	if trunkRecorder {
		return fmt.Sprintf("%s: %s", step, err.Error())
	}

	return fmt.Sprintf("%s: %s\n", step, err.Error())
}

// answer tells the uploader what the server decided: an accepted
// upload is answered success whether or not the server then placed the
// call. What the server made of it goes to the server log. A submission
// that only asks whether this server will take a recording is answered
// before any call is decided, because a check is not a recording and
// leaves nothing behind.
func (upload *Endpoints) answer(w http.ResponseWriter, r *http.Request, key string, call domain.Call) {
	if call.IsConnectionCheck() {
		upload.answerCheck(w, r, key, call)

		return
	}

	result, err := upload.ingest.Handle(r.Context(), application.UploadCallCommand{Call: call, Key: key})

	switch result.Outcome {
	case application.UploadCallIncomplete:
		// The legacy message already ends with a line break and its error
		// helper adds one, so the uploader reads a blank line after it.
		upload.fail(w, http.StatusExpectationFailed, fmt.Sprintf("Incomplete call data: %s\n", err.Error()))

	case application.UploadCallRefused:
		upload.refuseKey(w)

	case application.UploadCallAccepted:
		w.Write([]byte(uploadAccepted))

	default:
		// Failing to read the configured credentials is a server failure,
		// not a refusal: the legacy server keeps credentials in memory
		// and never reaches this answer.
		upload.fail(w, http.StatusExpectationFailed, fmt.Sprintf("Call upload failed: %s", err.Error()))
	}
}

// answerCheck answers a submission that only asks whether this server
// will take a recording. What the server reads the submission's credential
// and system number as decides the answer: a sender this server does not
// accept is refused as a sender, and a submission that is short of being a
// recording is refused for what it is short of, in the same bytes it has
// always been given. The check never reaches the use case that decides a
// call, so it stores nothing.
func (upload *Endpoints) answerCheck(w http.ResponseWriter, r *http.Request, key string, call domain.Call) {
	result, err := upload.check.Handle(r.Context(), application.CheckUploadConnectionQuery{Call: call, Key: key})
	if err != nil {
		// Failing to read the configuration a check is answered from is a
		// server failure, not a refusal: the same answer an upload gets when
		// the credentials it is guarded by cannot be read.
		upload.fail(w, http.StatusExpectationFailed, fmt.Sprintf("Call upload failed: %s", err.Error()))

		return
	}

	if result.Credential != application.CheckUploadCredentialKnown {
		upload.refuseKey(w)

		return
	}

	upload.logCheckRefusal(result)

	// The legacy message already ends with a line break and the helper that
	// wrote it added another one, so the uploader reads a blank line after it
	// and reads those same bytes here.
	w.WriteHeader(http.StatusExpectationFailed)
	w.Write([]byte(fmt.Sprintf("Incomplete call data: %s\n\n", result.Deficiency)))
}

// refuseKey answers an upload whose credential this server does not accept,
// whichever submission carried it. The legacy server answered this refusal in
// silence; it is now filed with the upload endpoint, and the uploader's answer
// is unchanged.
func (upload *Endpoints) refuseKey(w http.ResponseWriter) {
	upload.logRefusal("the apikey is not valid")

	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(uploadDenied))
}

// refuseMethod answers a method these endpoints do not take, with the
// plain-text body upload clients have always been given and a log
// entry: a wrong-method poster is a standing misconfiguration.
func (upload *Endpoints) refuseMethod(w http.ResponseWriter) {
	upload.logRefusal("the server does not answer this method")

	w.WriteHeader(http.StatusMethodNotAllowed)
	w.Write([]byte("Unsupported method\n"))
}

// fail writes a failure the legacy way.
func (upload *Endpoints) fail(w http.ResponseWriter, status int, message string) {
	upload.logRefusal(message)

	w.WriteHeader(status)
	w.Write([]byte(fmt.Sprintf("%s\n", message)))
}

// logRefusal files one upload refusal, action then reason, trimmed so
// the log never holds the uploader's trailing line break.
func (upload *Endpoints) logRefusal(reason string) {
	if upload.logs == nil {
		return
	}

	upload.logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: upload.clock.Now(),
		Level:    domain.LogLevelError,
		Source:   domain.LogSourceUpload,
		Message:  fmt.Sprintf("the upload was refused: %s", strings.TrimSpace(reason)),
	})
}

// logCheckRefusal files one refused connection check at the level a routine
// refusal is read at, and in a sentence that names itself as a check. An
// operator reading it between two upload attempts is reading what a client
// that keeps asking whether the server is listening was refused for, and not
// a recording this server failed to take.
func (upload *Endpoints) logCheckRefusal(result application.CheckUploadConnectionQueryResult) {
	if upload.logs == nil {
		return
	}

	upload.logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: upload.clock.Now(),
		Level:    domain.LogLevelInfo,
		Source:   domain.LogSourceUpload,
		Message:  checkRefusalSentence(result),
	})
}

// checkRefusalSentence says what a refused check was refused for, which
// credential presented itself, and which system the check named. Only a
// credential this server holds and has not switched off reaches this
// sentence, so the identity it is configured under is always the one to name;
// the system clause tells apart a number the operator configured, a number no
// one did, and no number at all.
func checkRefusalSentence(result application.CheckUploadConnectionQueryResult) string {
	sentence := fmt.Sprintf("the connection check was refused: %s, presented by the apikey %s",
		result.Deficiency, result.CredentialIdent)

	if result.SystemRef == 0 {
		return sentence
	}

	if !result.SystemKnown {
		return fmt.Sprintf("%s for system %d this server does not have", sentence, result.SystemRef)
	}

	return fmt.Sprintf("%s for system %d", sentence, result.SystemRef)
}

// fileTruncation files the parser's warning about an oversized posted
// list: the upload is still taken, and the operator reads that a
// poster sent more entries than the server carries.
func (upload *Endpoints) fileTruncation(warning string) {
	if len(warning) == 0 || upload.logs == nil {
		return
	}

	upload.logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: upload.clock.Now(),
		Level:    domain.LogLevelWarn,
		Source:   domain.LogSourceUpload,
		Message:  warning,
	})
}
