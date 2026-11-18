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

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// CheckUploadCredential is what a connection check's submission says about the credential it
// presented, read out of the server's own configuration. The four states are the four situations a
// server can be in: it holds the credential, it has never seen it, it holds it switched off, and
// nothing was presented to look at.
type CheckUploadCredential string

const (
	// CheckUploadCredentialKnown means the server holds the credential and it is switched on. The
	// identity the operator configured it under is returned with it.
	CheckUploadCredentialKnown CheckUploadCredential = "known"

	// CheckUploadCredentialUnknown means the server holds no credential with what was presented.
	CheckUploadCredentialUnknown CheckUploadCredential = "unknown"

	// CheckUploadCredentialDisabled means the server holds the credential and has it switched off.
	// It is reported as switched off rather than as absent, because the operator can switch it back
	// on and that is the fact they need to fix it.
	CheckUploadCredentialDisabled CheckUploadCredential = "disabled"

	// CheckUploadCredentialNone means nothing was presented: an empty credential is not a credential
	// this server could look at, even one its own list left with no key.
	CheckUploadCredentialNone CheckUploadCredential = "none"
)

// CheckUploadConnectionQuery asks what a submission that only asks whether this server will take a
// recording means. The call is what the upload endpoints' reader made of the submission; the
// credential is handed beside it, the way the endpoints hold it, because it is not a field of a call
// and is never part of what the server stores about one.
type CheckUploadConnectionQuery struct {
	Call domain.Call
	Key  string
}

// CheckUploadConnectionQueryResult is what the server's configuration says about the check. The
// credential state and the identity it is configured under say whether the server knows the
// submission's sender; the system reference and whether the server has that system say whether the
// number the check named is one the operator has; the deficiency is the reason the submission is
// short of being a recording the server could take, copied from the call itself so the word an
// operator reads is the word the server has always reported.
type CheckUploadConnectionQueryResult struct {
	Credential      CheckUploadCredential
	CredentialIdent string

	// SystemRef is the system the server reads the check's number as: the reference of the system it
	// found, or the number the check named when it has no system for it.
	SystemRef uint

	// SystemKnown reports whether the server has a system for that number.
	SystemKnown bool

	Deficiency string
}

// CheckUploadConnectionQueryHandler answers what a connection check means without deciding anything
// about it: it reports the credential's state and the system the check named, and it takes no
// recording, stores nothing, and creates no system the operator did not configure. A credential's
// scope is not evaluated, because a check hands over nothing: whether the server knows the sender is
// the credential being in the configuration at all, and what that credential may hand over is a
// question a check never asks. The credential is read before the submission's own deficiency, so an
// answer about a sender the server does not know says that first.
type CheckUploadConnectionQueryHandler struct {
	// apikeys is the upload credentials in effect, which is where the credential a check presented is
	// looked up.
	apikeys ports.ApikeyRepository

	// systems is the systems in effect, which is where the number a check named is looked up.
	systems ports.SystemRepository
}

// NewCheckUploadConnectionQueryHandler builds the handler from the upload credentials in effect and
// the systems in effect, which are the two configurations a connection check's answer is read out of.
func NewCheckUploadConnectionQueryHandler(
	apikeys ports.ApikeyRepository,
	systems ports.SystemRepository,
) *CheckUploadConnectionQueryHandler {
	return &CheckUploadConnectionQueryHandler{
		apikeys: apikeys,
		systems: systems,
	}
}

// Handle answers one connection check. Both configurations are read from the repositories and
// neither is written to: a submission that only asks whether this server will take a recording must
// not be the thing that changes what it listens to, including a check naming a number this server
// has no system for, whatever the auto-populate setting for uploads is.
func (handler *CheckUploadConnectionQueryHandler) Handle(ctx context.Context, query CheckUploadConnectionQuery) (CheckUploadConnectionQueryResult, error) {
	apikeys, err := handler.apikeys.AllApikeys(ctx)
	if err != nil {
		return CheckUploadConnectionQueryResult{}, err
	}

	systems, err := handler.systems.AllSystems(ctx)
	if err != nil {
		return CheckUploadConnectionQueryResult{}, err
	}

	result := CheckUploadConnectionQueryResult{
		Credential: CheckUploadCredentialNone,
		SystemRef:  query.Call.Meta.SystemRef,
	}

	if apikey, ok := apikeys.ByKey(query.Key); ok {
		if apikey.Disabled {
			result.Credential = CheckUploadCredentialDisabled
		} else {
			result.Credential = CheckUploadCredentialKnown
		}

		result.CredentialIdent = apikey.Ident
	} else if len(query.Key) > 0 {
		result.Credential = CheckUploadCredentialUnknown
	}

	if system, ok := systems.ResolveSystem(query.Call.Meta); ok {
		result.SystemRef = system.SystemRef
		result.SystemKnown = true
	}

	if err := query.Call.IsValid(); err != nil {
		result.Deficiency = err.Error()
	}

	return result, nil
}
