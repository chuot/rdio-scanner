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
	"testing"
	"time"

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// A connection check is a submission that asks only whether this server will take a recording. What it
// earns is decided by the credential it presented and the system number it named, and both are read out
// of the server's own configuration: a credential the server holds, one it has never heard of, one that
// is switched off, and none at all are four different answers, and a system number is either a row this
// server has or a number it has none for. The query answers those facts and stores nothing — a client
// that asks whether the server is listening must not be the thing that changes what it listens to.

const (
	checkSystemRef     = uint(10001)
	checkOtherSystem   = uint(10002)
	checkTalkgroupRef  = uint(1514000)
	checkOpenKey       = "check-apikey-open"
	checkScopedKey     = "check-apikey-scoped"
	checkDisabledKey   = "check-apikey-disabled"
	checkUnknownKey    = "check-apikey-not-stored"
	checkRecordingMark = "a recording long enough to be a recording"
)

// checkTime is the moment a submission names when it names one at all.
var checkTime = time.Date(2026, 11, 17, 9, 30, 0, 0, time.UTC)

// checkFixture is the server a connection check arrives at: the credentials it holds, the systems it
// holds, and the query that answers the check. No ingest use case is wired in, because a check never
// reaches one, and no log repository is wired in either: the query answers, and the upload endpoint is
// what files the answer in the server's log view.
type checkFixture struct {
	apikeys *recordedApikeys
	systems *recordedSystems

	handler *CheckUploadConnectionQueryHandler
}

func newCheckFixture() *checkFixture {
	fixture := &checkFixture{
		apikeys: &recordedApikeys{Apikeys: &fakes.Apikeys{Stored: domain.Apikeys{
			{ID: 1, Ident: "Capture open", Key: checkOpenKey, Scope: domain.AccessScope{Present: true, Wildcard: true}},
			{ID: 2, Ident: "Capture scoped", Key: checkScopedKey, Scope: domain.AccessScope{
				Present: true,
				Systems: []domain.ScopedSystem{{SystemRef: checkOtherSystem, TalkgroupsWildcard: true}},
			}},
			{ID: 3, Disabled: true, Ident: "Capture switched off", Key: checkDisabledKey, Scope: domain.AccessScope{Present: true, Wildcard: true}},
		}}},
		systems: &recordedSystems{Systems: &fakes.Systems{Stored: domain.Systems{
			{ID: 1, SystemRef: checkSystemRef, Label: "Grand Metro", Talkgroups: domain.Talkgroups{
				{ID: 10, TalkgroupRef: checkTalkgroupRef, Label: "Ground", Name: "Dispatch 1", TagID: 10},
			}},
		}}},
	}

	fixture.handler = NewCheckUploadConnectionQueryHandler(fixture.apikeys, fixture.systems)

	return fixture
}

// check asks the query what one submission means. The credential is handed beside the call, the way the
// upload endpoint holds it: it is not a field of a call, and it is never part of what the server stores.
func (fixture *checkFixture) check(key string, call domain.Call) (CheckUploadConnectionQueryResult, error) {
	return fixture.handler.Handle(context.Background(), CheckUploadConnectionQuery{Call: call, Key: key})
}

// recordedApikeys and recordedSystems name every method of the two repositories a use case reaches, so a
// use case that reads the configuration it answers from, and then writes to it, is caught by the case
// that says it may not.
type recordedApikeys struct {
	*fakes.Apikeys

	reads  []string
	writes []string
}

var _ ports.ApikeyRepository = (*recordedApikeys)(nil)

func (recorded *recordedApikeys) AllApikeys(ctx context.Context) (domain.Apikeys, error) {
	recorded.reads = append(recorded.reads, "AllApikeys")

	return recorded.Apikeys.AllApikeys(ctx)
}

func (recorded *recordedApikeys) SaveApikeys(ctx context.Context, next domain.Apikeys) error {
	recorded.writes = append(recorded.writes, "SaveApikeys")

	return recorded.Apikeys.SaveApikeys(ctx, next)
}

type recordedSystems struct {
	*fakes.Systems

	reads  []string
	writes []string
}

var _ ports.SystemRepository = (*recordedSystems)(nil)

func (recorded *recordedSystems) AllSystems(ctx context.Context) (domain.Systems, error) {
	recorded.reads = append(recorded.reads, "AllSystems")

	return recorded.Systems.AllSystems(ctx)
}

func (recorded *recordedSystems) SaveSystems(ctx context.Context, next domain.Systems) error {
	recorded.writes = append(recorded.writes, "SaveSystems")

	return recorded.Systems.SaveSystems(ctx, next)
}

func (recorded *recordedSystems) SaveSystemSettings(ctx context.Context, next domain.Systems) error {
	recorded.writes = append(recorded.writes, "SaveSystemSettings")

	return recorded.Systems.SaveSystemSettings(ctx, next)
}

func (recorded *recordedSystems) SaveSystemSites(ctx context.Context, systemID uint64, sites domain.Sites) error {
	recorded.writes = append(recorded.writes, "SaveSystemSites")

	return recorded.Systems.SaveSystemSites(ctx, systemID, sites)
}

func (recorded *recordedSystems) SaveSystemTalkgroups(ctx context.Context, systemID uint64, talkgroups domain.Talkgroups, blacklists *domain.Blacklists) error {
	recorded.writes = append(recorded.writes, "SaveSystemTalkgroups")

	return recorded.Systems.SaveSystemTalkgroups(ctx, systemID, talkgroups, blacklists)
}

func (recorded *recordedSystems) SaveSystemUnits(ctx context.Context, systemID uint64, units domain.Units) error {
	recorded.writes = append(recorded.writes, "SaveSystemUnits")

	return recorded.Systems.SaveSystemUnits(ctx, systemID, units)
}

// postedCheck is what a capture client's connection check is read as once its body has been read: a
// system number, and no recording, no moment, and no talkgroup. Whether a submission is a check is
// decided where the submission is read, and the query is asked only about submissions that were; it
// asks nothing about the claim, because the claim decides which use case is asked, not what the answer
// says.
func postedCheck() domain.Call {
	call := domain.NewCall()

	call.Meta.SystemRef = checkSystemRef

	return call
}

// recordingCall is a submission that is a recording: a recording, a moment, and the system and talkgroup
// it names.
func recordingCall() domain.Call {
	call := domain.NewCall()

	call.Audio = make([]byte, 64)
	copy(call.Audio, checkRecordingMark)
	call.AudioFilename = "capture.wav"
	call.AudioMime = "audio/wav"
	call.Timestamp = checkTime
	call.Meta.SystemRef = checkSystemRef
	call.Meta.TalkgroupRef = checkTalkgroupRef

	return call
}

// What a check earns is the credential's own state, read from what the server holds: a credential it
// holds is known and named by the identity it is configured under, one it does not hold and no credential
// at all are two answers a server gives the same way it answers a recording with that credential, and a
// switched-off credential is found and reported as switched off rather than as absent. A credential whose
// scope names another system than the check names is still known: whether a check is honoured is the
// credential being in the server at all, and the scope is what a credential may hand over, which a check
// hands over nothing.
func TestTheConnectionCheckQueryAnswersWhatTheCredentialItPresentedMeans(t *testing.T) {
	cases := []struct {
		name      string
		key       string
		want      CheckUploadCredential
		wantIdent string
	}{
		{
			name:      "a credential this server holds",
			key:       checkOpenKey,
			want:      CheckUploadCredentialKnown,
			wantIdent: "Capture open",
		},
		{
			name:      "a credential this server does not hold",
			key:       checkUnknownKey,
			want:      CheckUploadCredentialUnknown,
			wantIdent: "",
		},
		{
			name:      "a credential that is switched off",
			key:       checkDisabledKey,
			want:      CheckUploadCredentialDisabled,
			wantIdent: "Capture switched off",
		},
		{
			name:      "no credential presented at all",
			key:       "",
			want:      CheckUploadCredentialNone,
			wantIdent: "",
		},
		{
			name:      "a credential whose scope names another system than the one the check names",
			key:       checkScopedKey,
			want:      CheckUploadCredentialKnown,
			wantIdent: "Capture scoped",
		},
	}

	for _, testCase := range cases {
		fixture := newCheckFixture()

		result, err := fixture.check(testCase.key, postedCheck())
		if err != nil {
			t.Fatalf("%s: the query failed: %v", testCase.name, err)
		}

		if result.Credential != testCase.want {
			t.Errorf("%s: credential = %q, want %q: the answer is the credential's own state, and a credential "+
				"that is switched off is one the server has, not one it does not have", testCase.name, result.Credential, testCase.want)
		}

		if result.CredentialIdent != testCase.wantIdent {
			t.Errorf("%s: credential identity = %q, want %q: a credential the server holds is named by the "+
				"identity it is configured under, and one it does not hold has no identity to name",
				testCase.name, result.CredentialIdent, testCase.wantIdent)
		}
	}
}

// The system a check names is answered in the three states it has: a number the server has a row for is
// that system, a number it has no row for is still the number the check named and is said not to be one
// the server has, and a check that named no number at all leaves nothing to name.
func TestTheConnectionCheckQueryNamesTheResolvedSystemOrSaysTheServerDoesNotHaveIt(t *testing.T) {
	cases := []struct {
		name          string
		systemRef     uint
		wantRef       uint
		wantSystemHas bool
	}{
		{name: "a number this server has a row for", systemRef: checkSystemRef, wantRef: checkSystemRef, wantSystemHas: true},
		{name: "a number this server has no row for", systemRef: checkOtherSystem, wantRef: checkOtherSystem, wantSystemHas: false},
		{name: "no number at all", systemRef: 0, wantRef: 0, wantSystemHas: false},
	}

	for _, testCase := range cases {
		fixture := newCheckFixture()

		call := postedCheck()
		call.Meta.SystemRef = testCase.systemRef

		result, err := fixture.check(checkOpenKey, call)
		if err != nil {
			t.Fatalf("%s: the query failed: %v", testCase.name, err)
		}

		if result.SystemKnown != testCase.wantSystemHas {
			t.Errorf("%s: the server has a system for %d = %t, want %t: a number the server has no row for is "+
				"named as one it does not have, and not reported as a system the check failed to name",
				testCase.name, testCase.systemRef, result.SystemKnown, testCase.wantSystemHas)
		}

		if result.SystemRef != testCase.wantRef {
			t.Errorf("%s: system = %d, want %d: the answer names the system as the server's own configuration "+
				"reads the number the check presented", testCase.name, result.SystemRef, testCase.wantRef)
		}
	}
}

// The reason a refused check is refused is copied from what the call itself is short of, so the word the
// operator reads is the word the server has always reported and the order the reasons are reported in
// stays where it is today.
func TestTheConnectionCheckQueryReportsTheDeficiencyTheCallIsValidReports(t *testing.T) {
	cases := []struct {
		name string
		call func() domain.Call
		want string
	}{
		{
			// The body a capture client's connection check posts, read as a call: no recording, no
			// moment, no talkgroup, and the last thing it is short of is the talkgroup.
			name: "the submission a capture client's check is read as",
			call: postedCheck,
			want: "no talkgroup",
		},
		{
			name: "a check naming a talkgroup this server has, and no recording",
			call: func() domain.Call {
				call := postedCheck()

				call.Timestamp = checkTime
				call.Meta.TalkgroupRef = checkTalkgroupRef

				return call
			},
			want: "no audio",
		},
		{
			name: "a submission that is a recording",
			call: recordingCall,
			want: "",
		},
	}

	for _, testCase := range cases {
		fixture := newCheckFixture()

		call := testCase.call()

		result, err := fixture.check(checkOpenKey, call)
		if err != nil {
			t.Fatalf("%s: the query failed: %v", testCase.name, err)
		}

		if result.Deficiency != testCase.want {
			t.Errorf("%s: deficiency = %q, want %q: the reason a check is refused is the reason the call is "+
				"short of being a submission the server can take, copied and not re-derived",
				testCase.name, result.Deficiency, testCase.want)
		}

		// The copy is what is being pinned: whatever the call itself reports is what the answer says.
		fromCall := ""
		if reason := call.IsValid(); reason != nil {
			fromCall = reason.Error()
		}

		if result.Deficiency != fromCall {
			t.Errorf("%s: deficiency = %q, want the reason the call reports, %q", testCase.name, result.Deficiency, fromCall)
		}
	}
}

// A check is a question, and a question is not answered by changing what it asks about: the query reads
// the credentials and the systems it answers from and reaches no write of either, and the configuration
// it read is the configuration the server still holds afterwards. A check that arrives with a credential
// the server does not hold, or with none, or switched off, or with a system number it has no row for,
// leaves the same configuration behind as the check it accepted.
func TestTheConnectionCheckQueryStoresNothing(t *testing.T) {
	fixture := newCheckFixture()

	fixture.apikeys.reads = nil
	fixture.apikeys.writes = nil
	fixture.systems.reads = nil
	fixture.systems.writes = nil

	calls := []domain.Call{
		postedCheck(),
		recordingCall(),
		func() domain.Call {
			call := postedCheck()

			call.Meta.SystemRef = checkOtherSystem

			return call
		}(),
		func() domain.Call {
			call := postedCheck()

			call.Meta.SystemRef = 0

			return call
		}(),
	}

	for _, key := range []string{checkOpenKey, checkScopedKey, checkUnknownKey, checkDisabledKey, ""} {
		for _, call := range calls {
			result, err := fixture.check(key, call)
			if err != nil {
				t.Fatalf("a check presented with credential %q: the query failed: %v", key, err)
			}

			if result.Credential == "" {
				t.Errorf("a check presented with credential %q was answered with no credential state at all: the "+
					"answer needs the credential's state to be filed with", key)
			}
		}
	}

	if len(fixture.apikeys.reads) == 0 || len(fixture.systems.reads) == 0 {
		t.Errorf("the query read %v and %v, want both the credentials and the systems it answers from read",
			fixture.apikeys.reads, fixture.systems.reads)
	}

	if len(fixture.apikeys.writes) != 0 || len(fixture.systems.writes) != 0 {
		t.Errorf("the query wrote %v and %v, want no write of either: a submission that only asks whether this "+
			"server will take a recording must not change the credentials or the systems it asked about",
			fixture.apikeys.writes, fixture.systems.writes)
	}

	if len(fixture.apikeys.Stored) != 3 {
		t.Errorf("the server holds %d credentials after answering checks, want the 3 it held before: %v",
			len(fixture.apikeys.Stored), fixture.apikeys.Stored)
	}

	if len(fixture.systems.Stored) != 1 || fixture.systems.Stored[0].SystemRef != checkSystemRef {
		t.Errorf("the server holds %v after answering checks, want the system it held before: a check naming a "+
			"system this server has no row for does not add one, whatever the server's auto-populate setting is",
			fixture.systems.Stored)
	}
}
