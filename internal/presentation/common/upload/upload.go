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

// Endpoints is the surface an outside program posts recordings to: a radio bridge, a capture application,
// and another rdio-scanner server reposting what it received downstream.
//
// These endpoints are not behind an administrator session and not behind a listener's access code. What
// lets an upload through is the credential the upload carries, and what decides it is the credential's
// scope — which is the thing quirk Q51 made impossible in the legacy server and which the owner asked to
// fix.
//
// They answer with plain text and not with JSON, because that is what every program posting to them has
// been reading since these endpoints existed.
type Endpoints struct {
	ingest *application.UploadCallHandler
	parser Parser
	logs   ports.LogRepository

	// clock is the server's one clock. This endpoint used to stamp its refusals with the wall clock, which
	// is the wrong instant on the one machine where an operator is chasing a capture box that stopped
	// uploading.
	clock ports.Clock
}

// Deps is what the upload endpoints need: the use case that decides whether an upload is taken, the
// reading that says what each posted field means, the server's log view, and the server's clock.
type Deps struct {
	Ingest *application.UploadCallHandler
	Parser Parser
	Logs   ports.LogRepository
	Clock  ports.Clock
}

// Parser is the part of the server that knows what an uploader's fields mean.
//
// It is declared by the endpoints that ask for it, rather than being a port of the application layer,
// because what is being read is the wire format of these endpoints and not a fact about calls: the
// application layer must not know that an upload is a multipart body, and these endpoints have no business
// importing the package that implements this (contract-server-ng.md §1). The composition root wires the
// server's ingest parser here, which is the same parser the directory watchers use for the files they read
// — the trunk-recorder sidecar in particular is read the same way from a watched directory and from an
// upload.
type Parser interface {
	// ApplyMultipart reads one posted field into the call being built, and hands back the call. A field
	// the parser does not know leaves the call as it was.
	ApplyMultipart(part *multipart.Part, data []byte, call domain.Call) domain.Call

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

// uploadAccepted and uploadDenied are the two answers an upload can be given when the server has read it
// and decided.
//
// The refusal names a system and a talkgroup and says both are nothing. That is not shorthand for
// something else: the legacy server builds this message from the call's resolved system and talkgroup, and
// at this boundary neither has been resolved — the resolution happens later, in ingest — so the message an
// uploader has always been given has always said <nil> for both. What actually decided the answer is the
// credential, and the two names in the message are an accident of the order the legacy server does its work
// in.
const (
	uploadAccepted = "Call imported successfully.\n"
	uploadDenied   = "Invalid API key for system <nil> talkgroup <nil>.\n"
)

// CallUpload takes a call an outside program posted as a set of form fields.
//
// Every field is optional and each one is read in the order it arrived, so a field that arrives later wins
// over one that arrived earlier. That ordering is the legacy server's own for this endpoint, and it is what
// the trunk-recorder endpoint does not have (Q57) — see
// docs/specs/FEAT-003-server-ng-refactor/decision-preserved-quirks.md.
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

// TrunkRecorderCallUpload takes a call from trunk-recorder: a JSON sidecar beside the recording.
//
// The sidecar is read as a document and not as a form field, and everything else the upload carries is read
// as an ordinary field, which is what lets a trunk-recorder upload name things the sidecar itself does not.
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

// readCall reads the posted fields and hands back the call they describe and the credential the upload
// carried.
//
// The credential is taken out here and never reaches the parser: the parser knows what a field means for a
// call, and a credential is not part of a call.
//
// The reported failures are the legacy server's own, including the way its error helper wraps them. The two
// endpoints wrap the multipart reader's failure differently — this endpoint's message already ends with a
// line break and the trunk-recorder endpoint's does not — which is why the wrapping is written out per
// endpoint rather than shared.
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
		reader = multipart.NewReader(r.Body, params["boundary"])
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

		switch part.FormName() {
		case "key":
			key = string(body)

		case "meta":
			if !trunkRecorder {
				call = upload.parser.ApplyMultipart(part, body, call)

				break
			}

			if err := upload.parser.ApplyTrunkRecorderSidecar(&call, body); err != nil {
				// The legacy server names no reason here. The uploader is told the call data is invalid,
				// and the reason the sidecar could not be read is written in the server's own log view.
				upload.fail(w, http.StatusExpectationFailed, "Invalid call data")

				return domain.Call{}, "", false
			}

		default:
			call = upload.parser.ApplyMultipart(part, body, call)
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

// answer tells the uploader what the server decided.
//
// An upload the server took is answered the same whether the server then managed to place the call or not:
// the legacy server hands the call to its ingest queue and answers success whatever the queue makes of it,
// and an uploader that was told its call was imported and then cannot find it has nothing to act on. What
// the server made of the call is written in the server's own log view.
func (upload *Endpoints) answer(w http.ResponseWriter, r *http.Request, key string, call domain.Call) {
	result, err := upload.ingest.Handle(r.Context(), application.UploadCallCommand{Call: call, Key: key})

	switch result.Outcome {
	case application.UploadCallIncomplete:
		// The legacy server's own message already ends with a line break, and its error helper adds one,
		// so an uploader reading this line gets a blank line after it.
		upload.fail(w, http.StatusExpectationFailed, fmt.Sprintf("Incomplete call data: %s\n", err.Error()))

	case application.UploadCallRefused:
		// The legacy server answers this refusal and writes nothing about it, which sends an operator
		// chasing a capture box that stopped uploading to guess whether the server was ever reached. The
		// refusal is now filed with the upload endpoint. The answer the uploader receives is unchanged.
		upload.logRefusal("the apikey is not valid")

		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(uploadDenied))

	case application.UploadCallAccepted:
		w.Write([]byte(uploadAccepted))

	default:
		// The credentials the server checks an upload against are the configuration in effect, and a read
		// of them that fails is a failure of the server and not a refusal of the upload. The legacy server
		// keeps its credentials in memory and has no path that reaches this answer.
		upload.fail(w, http.StatusExpectationFailed, fmt.Sprintf("Call upload failed: %s", err.Error()))
	}
}

// refuseMethod answers a method these endpoints do not take. The body is unusual for this server — most of
// it answers an unsupported method with nothing — and it is what the upload clients have always been given.
//
// It is also filed with the upload endpoint. A client posting with the wrong method is a standing
// misconfiguration, and the log view is the only place an operator can find out that the server was reached
// at all.
func (upload *Endpoints) refuseMethod(w http.ResponseWriter) {
	upload.logRefusal("the server does not answer this method")

	w.WriteHeader(http.StatusMethodNotAllowed)
	w.Write([]byte("Unsupported method\n"))
}

// fail writes a failure the way the legacy server's error helper does: the reason is filed with the upload
// endpoint at error level, and then given to the uploader with a line break of its own.
func (upload *Endpoints) fail(w http.ResponseWriter, status int, message string) {
	upload.logRefusal(message)

	w.WriteHeader(status)
	w.Write([]byte(fmt.Sprintf("%s\n", message)))
}

// logRefusal files one refusal of an upload with the upload endpoint, in the shape every other writer uses:
// what the server did, then the reason.
//
// The reason is trimmed. The legacy error helper builds the uploader's body out of the same string, and one
// of its call sites passes a message that already ends with a line break, so without this the log holds the
// answer's line break instead of a sentence.
//
// An upload endpoint with no log repository is the one this server builds when it is run to do something
// else; a refusal that cannot be filed is still a refusal the uploader is told about.
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
