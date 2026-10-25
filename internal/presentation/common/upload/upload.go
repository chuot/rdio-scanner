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
// see the call-upload-contract-and-legacy-divergences maintainer note.
type Endpoints struct {
	ingest *application.UploadCallHandler
	parser Parser
	logs   ports.LogRepository

	// clock is the server's one clock, injected into the upload writer.
	// see the call-upload-contract-and-legacy-divergences maintainer note.
	clock ports.Clock
}

// Deps is what the upload endpoints need: the deciding use case, the
// field parser, the log view, and the server's clock.
type Deps struct {
	Ingest *application.UploadCallHandler
	Parser Parser
	Logs   ports.LogRepository
	Clock  ports.Clock
}

// Parser is the part of the server that knows what an uploader's
// fields mean. It is declared by the endpoints that ask for it, not
// by the application layer, which must not know the wire format.
// see the call-upload-contract-and-legacy-divergences maintainer note.
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
// upload gets once the server has read it and decided. The refusal
// has always named the system and talkgroup as nothing:
// see the call-upload-contract-and-legacy-divergences maintainer note.
const (
	uploadAccepted = "Call imported successfully.\n"
	uploadDenied   = "Invalid API key for system <nil> talkgroup <nil>.\n"

	// maxUploadBytes is the ceiling on the body an upload endpoint will
	// read. The endpoints read the whole body into memory before the
	// credential is checked, so it bounds an anonymous POST.
	// see the call-upload-contract-and-legacy-divergences maintainer note.
	maxUploadBytes = 32 << 20
)

// CallUpload takes a call an outside program posted as a set of form
// fields. Every field is optional and each is read in the order it
// arrived, so a later field wins an earlier one.
// see the call-upload-contract-and-legacy-divergences maintainer note.
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
// see the call-upload-contract-and-legacy-divergences maintainer note.
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
// never reaches the parser. The reported failures are the legacy
// server's own, written out per endpoint, as the frozen contract fixes.
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

		var warning string

		switch part.FormName() {
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
				// see the call-upload-contract-and-legacy-divergences maintainer note.
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

// readFailure wraps the failure of one step of reading the posted body, the way each endpoint has always
// wrapped it.
func (upload *Endpoints) readFailure(trunkRecorder bool, step string, err error) string {
	if trunkRecorder {
		return fmt.Sprintf("%s: %s", step, err.Error())
	}

	return fmt.Sprintf("%s: %s\n", step, err.Error())
}

// answer tells the uploader what the server decided: an accepted
// upload is answered success whether or not the server then placed
// the call. What the server made of it goes to the server log.
// see the call-upload-contract-and-legacy-divergences maintainer note.
func (upload *Endpoints) answer(w http.ResponseWriter, r *http.Request, key string, call domain.Call) {
	result, err := upload.ingest.Handle(r.Context(), application.UploadCallCommand{Call: call, Key: key})

	switch result.Outcome {
	case application.UploadCallIncomplete:
		// The legacy message already ends with a line break and its error
		// helper adds one, so the uploader reads a blank line after it.
		upload.fail(w, http.StatusExpectationFailed, fmt.Sprintf("Incomplete call data: %s\n", err.Error()))

	case application.UploadCallRefused:
		// The legacy server answered this refusal in silence; it is now
		// filed with the upload endpoint. The uploader's answer is unchanged.
		// see the call-upload-contract-and-legacy-divergences maintainer note.
		upload.logRefusal("the apikey is not valid")

		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(uploadDenied))

	case application.UploadCallAccepted:
		w.Write([]byte(uploadAccepted))

	default:
		// Failing to read the configured credentials is a server failure,
		// not a refusal: the legacy server keeps credentials in memory
		// and never reaches this answer.
		upload.fail(w, http.StatusExpectationFailed, fmt.Sprintf("Call upload failed: %s", err.Error()))
	}
}

// refuseMethod answers a method these endpoints do not take, with
// the plain-text body upload clients have always been given and a
// log entry: a wrong-method poster is a standing misconfiguration.
// see the call-upload-contract-and-legacy-divergences maintainer note.
func (upload *Endpoints) refuseMethod(w http.ResponseWriter) {
	upload.logRefusal("the server does not answer this method")

	w.WriteHeader(http.StatusMethodNotAllowed)
	w.Write([]byte("Unsupported method\n"))
}

// fail writes a failure the legacy way:
// see the call-upload-contract-and-legacy-divergences maintainer note.
func (upload *Endpoints) fail(w http.ResponseWriter, status int, message string) {
	upload.logRefusal(message)

	w.WriteHeader(status)
	w.Write([]byte(fmt.Sprintf("%s\n", message)))
}

// logRefusal files one upload refusal, action then reason, trimmed so
// the log never holds the uploader's trailing line break.
// see the call-upload-contract-and-legacy-divergences maintainer note.
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

// fileTruncation files the parser's warning about an oversized
// posted list: the upload is still taken, and the operator reads
// that a poster sent more entries than the server carries.
// see the call-upload-contract-and-legacy-divergences maintainer note.
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
