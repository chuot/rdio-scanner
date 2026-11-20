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

package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
)

// errCredentialStoreUnreadable is what a credential store says when it cannot be read at all.
var errCredentialStoreUnreadable = errors.New("apikeys.read: no such table: apikeys")

// An upload is a call an outside program posted. Whether this server takes it is decided by the
// credential the uploader presented and by what that credential covers, and the two are decided before
// anything about the call is looked up in the server's own configuration.

const (
	uploadSystemRef     = uint(10001)
	uploadTalkgroupRef  = uint(1514000)
	uploadOtherSystem   = uint(10002)
	uploadOpenKey       = "capture-apikey-open"
	uploadScopedKey     = "capture-apikey-scoped"
	uploadDisabledKey   = "capture-apikey-disabled"
	uploadRecordingMark = "a recording long enough to be a recording"
)

type uploadFixture struct {
	*ingestFixture

	apikeys *fakes.Apikeys
	uploads *UploadCallHandler
}

func newUploadFixture() *uploadFixture {
	ingest := newIngestFixture()

	fixture := &uploadFixture{
		ingestFixture: ingest,
		apikeys: &fakes.Apikeys{Stored: domain.Apikeys{
			{ID: 1, Ident: "Capture open", Key: uploadOpenKey, Scope: domain.AccessScope{Present: true, Wildcard: true}},
			{ID: 2, Ident: "Capture scoped", Key: uploadScopedKey, Scope: domain.AccessScope{
				Present: true,
				Systems: []domain.ScopedSystem{{SystemRef: uploadSystemRef, TalkgroupsWildcard: true}},
			}},
			{ID: 3, Disabled: true, Ident: "Capture disabled", Key: uploadDisabledKey, Scope: domain.AccessScope{Present: true, Wildcard: true}},
		}},
	}

	fixture.uploads = NewUploadCallHandler(fixture.apikeys, ingest.handler)

	return fixture
}

// seedUploadSystem installs the system and talkgroup the captured uploads name, so that an accepted
// upload has somewhere to land.
func (fixture *uploadFixture) seedUploadSystem() {
	fixture.systems.Stored = domain.Systems{
		{ID: 1, SystemRef: uploadSystemRef, Label: "Grand Metro", Talkgroups: domain.Talkgroups{
			{ID: 10, TalkgroupRef: uploadTalkgroupRef, Label: "Ground", Name: "Dispatch 1", TagID: 10},
		}},
	}
}

// uploadedCall is the call the capture posted: a recording, a moment, and the system and talkgroup it
// names by reference.
func uploadedCall() domain.Call {
	call := domain.NewCall()

	call.Audio = make([]byte, 64)
	copy(call.Audio, uploadRecordingMark)
	call.AudioFilename = "capture.wav"
	call.AudioMime = "audio/wav"
	call.Timestamp = ingestTime
	call.Meta.SystemRef = uploadSystemRef
	call.Meta.TalkgroupRef = uploadTalkgroupRef

	return call
}

func (fixture *uploadFixture) upload(key string, call domain.Call) (UploadCallResult, error) {
	return fixture.uploads.Handle(context.Background(), UploadCallCommand{Call: call, Key: key})
}

// An upload whose credential covers the call it names is taken, and the call is stored.
func TestUploadCallHandlerTakesAnUploadItsCredentialCovers(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()

	result, err := fixture.upload(uploadOpenKey, uploadedCall())
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	if result.Outcome != UploadCallAccepted {
		t.Fatalf("outcome = %q, want accepted: %v", result.Outcome, fixture.logMessages())
	}

	if len(fixture.calls.Saved) != 1 {
		t.Fatalf("the server stored %v, want the one call the upload carried", fixture.calls.Saved)
	}

	// A label beside the number the upload names is the shape a watch whose mask reads a
	// talkgroup label beside its number sends, and it is still a complete call: the number
	// files it and the label is what the stored talkgroup is renamed to. Its own fixture,
	// because a second upload carrying the same moment and the same talkgroup is a duplicate
	// of the first, and this reading is about the call being taken at all.
	also := newUploadFixture()

	also.seedUploadSystem()

	call := uploadedCall()
	call.Meta.TalkgroupLabel = "GASTON-FIRE"

	result, err = also.upload(uploadOpenKey, call)
	if err != nil {
		t.Fatalf("upload naming a label beside its talkgroup number: %v", err)
	}

	if result.Outcome != UploadCallAccepted {
		t.Errorf("outcome = %q, want accepted: %v", result.Outcome, also.logMessages())
	}

	if len(also.calls.Saved) != 1 {
		t.Errorf("the server stored %v, want the one call that upload carried", also.calls.Saved)
	}
}

// A credential the server does not know is refused, and nothing is stored: the refusal is the
// whole answer, with no log entry.
func TestUploadCallHandlerRefusesAKeyTheServerDoesNotKnow(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()

	result, err := fixture.upload("not-an-apikey", uploadedCall())
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	if result.Outcome != UploadCallRefused {
		t.Fatalf("outcome = %q, want refused", result.Outcome)
	}

	if len(fixture.calls.Saved) != 0 {
		t.Errorf("the server stored %v, want nothing for a refused upload", fixture.calls.Saved)
	}

	if len(fixture.logs.Recorded()) != 0 {
		t.Errorf("the refused upload wrote %v, want nothing: the legacy server logs nothing for a refused upload",
			fixture.logMessages())
	}
}

// A credential that is switched off is found and still refused: the same answer as a credential
// that does not exist.
func TestUploadCallHandlerRefusesADisabledCredential(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()

	result, err := fixture.upload(uploadDisabledKey, uploadedCall())
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	if result.Outcome != UploadCallRefused {
		t.Fatalf("outcome = %q, want refused: a switched-off credential covers nothing", result.Outcome)
	}
}

// A credential that names the system the upload names covers it, checked against the submitted
// call before anything is resolved.
func TestUploadCallHandlerChecksTheReferencesTheUploadNames(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()

	result, err := fixture.upload(uploadScopedKey, uploadedCall())
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	if result.Outcome != UploadCallAccepted {
		t.Fatalf("outcome = %q, want accepted: the credential names the system the upload names, and the "+
			"server has not resolved that system yet — the comparison has to be made on the references the "+
			"upload itself carries", result.Outcome)
	}
}

// The same credential refuses an upload naming a system it does not name.
func TestUploadCallHandlerRefusesAnUploadOutsideTheCredentialItNames(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()

	call := uploadedCall()
	call.Meta.SystemRef = uploadOtherSystem

	result, err := fixture.upload(uploadScopedKey, call)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	if result.Outcome != UploadCallRefused {
		t.Fatalf("outcome = %q, want refused", result.Outcome)
	}
}

// A credential naming particular talkgroups covers only those talkgroups.
func TestUploadCallHandlerChecksTheTalkgroupsACredentialNames(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()

	fixture.apikeys.Stored = domain.Apikeys{
		{ID: 4, Ident: "One talkgroup", Key: uploadScopedKey, Scope: domain.AccessScope{
			Present: true,
			Systems: []domain.ScopedSystem{{SystemRef: uploadSystemRef, TalkgroupRefs: []uint{uploadTalkgroupRef}}},
		}},
	}

	result, err := fixture.upload(uploadScopedKey, uploadedCall())
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	if result.Outcome != UploadCallAccepted {
		t.Fatalf("outcome = %q, want accepted", result.Outcome)
	}

	call := uploadedCall()
	call.Meta.TalkgroupRef = 1514100

	result, err = fixture.upload(uploadScopedKey, call)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	if result.Outcome != UploadCallRefused {
		t.Fatalf("outcome = %q, want refused: the credential names one talkgroup and the upload names another",
			result.Outcome)
	}
}

// A submission that is not a call is refused with the reason the server found; the four reasons
// are the legacy server's own words.
func TestUploadCallHandlerReportsWhatASubmissionIsMissing(t *testing.T) {
	cases := []struct {
		name  string
		call  func() domain.Call
		cause string
	}{
		{name: "no recording", call: func() domain.Call {
			call := uploadedCall()
			call.Audio = []byte("short")

			return call
		}, cause: "no audio"},
		{name: "no moment", call: func() domain.Call {
			// The moment the server was told to record is the epoch itself. A submission that names no
			// moment at all is a different thing and is accepted, which is a preserved quirk.
			call := uploadedCall()
			call.Timestamp = time.Unix(0, 0)

			return call
		}, cause: "no timestamp"},
		{name: "no system", call: func() domain.Call {
			call := uploadedCall()
			call.Meta.SystemRef = 0

			return call
		}, cause: "no system"},
		{name: "no talkgroup", call: func() domain.Call {
			call := uploadedCall()
			call.Meta.TalkgroupRef = 0

			return call
		}, cause: "no talkgroup"},
		{name: "a talkgroup named only by name", call: func() domain.Call {
			// A submission that carries a talkgroup label and no number is the shape a
			// mask that reads the talkgroup out of the recording's name sends: the name
			// is where a label belongs only after the server has filed the call.
			call := uploadedCall()
			call.Meta.TalkgroupRef = 0
			call.Meta.TalkgroupLabel = "GASTON-FIRE"

			return call
		}, cause: "no talkgroup"},
		{name: "a check that says nothing about a recording", call: func() domain.Call {
			// An ingest client testing its connection posts a credential and a system and names
			// no recording, no moment, and no talkgroup. The refusal names the last thing the
			// submission was missing, which is the answer the client reads as a working server.
			call := uploadedCall()
			call.Audio = nil
			call.Timestamp = time.Unix(0, 0)
			call.Meta.TalkgroupRef = 0

			return call
		}, cause: "no talkgroup"},
	}

	for _, testCase := range cases {
		fixture := newUploadFixture()

		fixture.seedUploadSystem()

		result, err := fixture.upload(uploadOpenKey, testCase.call())
		if result.Outcome != UploadCallIncomplete {
			t.Errorf("%s: outcome = %q, want incomplete", testCase.name, result.Outcome)

			continue
		}

		if err == nil || err.Error() != testCase.cause {
			t.Errorf("%s: reason = %v, want %q", testCase.name, err, testCase.cause)
		}

		if len(fixture.calls.Saved) != 0 {
			t.Errorf("%s: the server stored %v, want nothing", testCase.name, fixture.calls.Saved)
		}
	}
}

// An upload is answered before the server has worked out which system and talkgroup the call
// belongs to, and an upload the server could not complete is still answered as a success.
func TestUploadCallHandlerAnswersSuccessForAnUploadTheServerCouldNotComplete(t *testing.T) {
	fixture := newUploadFixture()

	// No system is stored, and auto-populate is switched off, so the ingest step has nowhere to put the
	// call. With auto-populate on — which is what the server runs with unless an administrator says
	// otherwise — the server invents the system and the call is stored.
	fixture.options.Stored.AutoPopulate = false

	result, err := fixture.upload(uploadOpenKey, uploadedCall())
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	if result.Outcome != UploadCallAccepted {
		t.Fatalf("outcome = %q, want accepted: the credential covers the call, and what the server makes of "+
			"the call afterwards is not the uploader's business", result.Outcome)
	}

	if len(fixture.calls.Saved) != 0 {
		t.Errorf("the server stored %v, want nothing: the call names a system the server does not know",
			fixture.calls.Saved)
	}

	if !fixture.loggedAs(domain.LogSourceIngest, "the recording capture.wav does not match any system or talkgroup the server knows") {
		t.Errorf("the call the server could not place is not in the log view: %v", fixture.logMessages())
	}
}

// A credential store that cannot be read is a failure and not a refusal: a refusal says the
// credential is wrong, a failure says the server could not tell.
func TestUploadCallHandlerReportsACredentialStoreItCannotRead(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()

	fixture.apikeys.Err = errCredentialStoreUnreadable

	result, err := fixture.upload(uploadOpenKey, uploadedCall())
	if err == nil {
		t.Fatalf("upload answered %v, want the failure the credential store reported", result.Outcome)
	}

	if result.Outcome != UploadCallFailed {
		t.Fatalf("outcome = %q, want failed", result.Outcome)
	}

	if len(fixture.calls.Saved) != 0 {
		t.Errorf("the server stored %v, want nothing", fixture.calls.Saved)
	}
}

// An upload is not a listener: it is not told about the call it posted, and it is not counted
// as a listener.
func TestUploadCallHandlerDeliversTheUploadedCallToTheListenersWatchingIt(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()

	fixture.hub.Open("listener", "10.0.0.1")
	fixture.hub.SetLivefeed("listener", domain.ParseLivefeed(map[string]any{
		"10001": map[string]any{"1514000": true},
	}))

	if _, err := fixture.upload(uploadOpenKey, uploadedCall()); err != nil {
		t.Fatalf("upload: %v", err)
	}

	broadcast := fixture.hub.DeliveredCalls["listener"]

	if len(broadcast) != 1 {
		t.Fatalf("the listener was sent %v, want the one call it is watching", broadcast)
	}
}

// A row stored with no key — saved by an older server, before the apply-time rule — must not
// answer a keyless upload: the answer to no credential is the refusal for an unknown credential.
func TestUploadCallHandlerRefusesAKeylessUpload(t *testing.T) {
	fixture := newUploadFixture()

	fixture.seedUploadSystem()

	fixture.apikeys.Stored = append(fixture.apikeys.Stored, domain.Apikey{
		ID:    4,
		Ident: "nameless",
		Scope: domain.AccessScope{Present: true, Wildcard: true},
	})

	result, err := fixture.upload("", uploadedCall())
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	if result.Outcome != UploadCallRefused {
		t.Fatalf("outcome = %q, want refused: an empty key must match nothing", result.Outcome)
	}

	if len(fixture.calls.Saved) != 0 {
		t.Errorf("the server stored %v, want nothing for a keyless upload", fixture.calls.Saved)
	}
}
