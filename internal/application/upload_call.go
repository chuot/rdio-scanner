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

package application

import (
	"context"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// UploadCallCommand is one call an outside program posted to the server, and the credential it posted
// with. The call is already what the server's ingest parsers made of the submission: which fields an
// uploader names, and what each one means, is a fact about the upload endpoints and not about calls.
type UploadCallCommand struct {
	Call domain.Call
	Key  string
}

// UploadCallOutcome is what the server decided about an upload.
type UploadCallOutcome string

const (
	// UploadCallAccepted means the credential covers the call and the server has taken it. It says
	// nothing about what the server made of the call afterwards, because an uploader is not told that.
	UploadCallAccepted UploadCallOutcome = "accepted"

	// UploadCallIncomplete means the submission is not a call at all. The reason is returned as the
	// error, because the upload endpoints answer the uploader with what is missing.
	UploadCallIncomplete UploadCallOutcome = "incomplete"

	// UploadCallRefused means the credential is not one that covers this call. A credential the server
	// does not know and one that is switched off are the same answer, which is what the legacy server's
	// single message for both produces.
	UploadCallRefused UploadCallOutcome = "refused"

	// UploadCallFailed means the server could not complete a step it depends on.
	UploadCallFailed UploadCallOutcome = "failed"
)

// UploadCallResult is what the server decided, and the identifier of the call it took, when it took one.
type UploadCallResult struct {
	Outcome UploadCallOutcome
	CallID  uint64
}

// UploadCallHandler decides whether this server takes a call an outside program posted.
//
// It is a use case of its own rather than a step of the ingest use case, because the two questions are
// different: the ingest use case decides what a call becomes, and this one decides who may hand the
// server a call at all. A call found in a watched directory is not asked the second question.
type UploadCallHandler struct {
	apikeys ports.ApikeyRepository

	// ingest is the use case that stores and delivers the call. It is a use case and not a port: the
	// upload endpoints and the directory watchers reach the same ingest step, and the composition root
	// has no choice to make between them.
	ingest *IngestCallHandler
}

// NewUploadCallHandler builds the handler from the credentials in effect and the ingest use case.
func NewUploadCallHandler(apikeys ports.ApikeyRepository, ingest *IngestCallHandler) *UploadCallHandler {
	return &UploadCallHandler{
		apikeys: apikeys,
		ingest:  ingest,
	}
}

// Handle decides whether the server takes an uploaded call.
//
// The order is the legacy order, and it is what an uploader is told. The submission is checked for being
// a call at all before the credential is looked at, so someone posting a broken call is told what is
// missing rather than that their credential is wrong. The credential is decided before the call is matched
// against the server's own configuration, so an uploader is never told anything about the systems and
// talkgroups the server knows about.
//
// What the server makes of the call afterwards is not the uploader's business. The legacy server hands
// the call to its ingest queue and answers success whatever that queue makes of it: a call naming a system
// the server does not know, a call already recorded, and a call the owning system refuses are each
// reported in the server's own log view, and none of them are reported to the uploader. An uploader that
// was told its call was imported and then cannot find it has nothing to work with, which is why the
// answer stays success.
func (handler *UploadCallHandler) Handle(ctx context.Context, cmd UploadCallCommand) (UploadCallResult, error) {
	if err := cmd.Call.IsValid(); err != nil {
		return UploadCallResult{Outcome: UploadCallIncomplete}, err
	}

	apikeys, err := handler.apikeys.AllApikeys(ctx)
	if err != nil {
		return UploadCallResult{Outcome: UploadCallFailed}, err
	}

	if !apikeys.AcceptsUpload(cmd.Key, cmd.Call) {
		return UploadCallResult{Outcome: UploadCallRefused}, nil
	}

	// The ingest outcome is deliberately not carried into the answer. Every way an ingest can end is
	// already written in the server's log view by the ingest use case itself, and the legacy server
	// reports none of them to the uploader.
	result, _ := handler.ingest.Handle(ctx, IngestCallCommand{Call: cmd.Call})

	return UploadCallResult{
		CallID:  result.CallID,
		Outcome: UploadCallAccepted,
	}, nil
}
