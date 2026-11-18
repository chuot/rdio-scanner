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
	"net/http"
	"testing"
	"time"

	"rdio-scanner/internal/domain"
)

// A capture client checking whether the server is listening posts its credential, a system number,
// and a part that says the submission is only a test, and it posts no recording. The answer the
// client reads is the answer the server has always given a submission that names no talkgroup, and
// it has to stay that way, so the entry the server keeps for itself is the only place an operator
// can tell a client that was testing from a recording that was lost. This journey drives both
// through one real server with the real multipart reader: a connection check, a recording, a
// connection check presenting a credential the server does not have, and a connection check naming a
// talkgroup this station was never told about.

const (
	// checkUploadKey is the credential the journey posts with, and checkUploadIdent is the identity
	// the server's own configuration holds that credential under, which is what an entry names when
	// it says who presented itself.
	checkUploadKey   = "capture-apikey-open"
	checkUploadIdent = "Capture open"

	// checkUploadSystem is the system number the capture client names, and it is a system this
	// station's configuration carries a row for.
	checkUploadSystem = 10001

	// unconfiguredTalkgroupRef is a talkgroup number this configuration carries no row for, which is
	// what a capture configured for a channel this server has never been told about names.
	unconfiguredTalkgroupRef = 999999

	// unknownUploadKey is a credential no row of this configuration carries at all.
	unknownUploadKey = "a-key-this-server-was-never-told-about"

	// postedRecordingFile is the name the recording this journey posts gives itself out loud.
	postedRecordingFile = "station-recording.wav"
)

// TestUploadConnectionCheckIsFiledAsACheckAndNotAsALostRecording is the whole path: a connection
// check and a recording reach one server through the endpoints and the reader a capture actually
// posts to, and each answer and each entry is read with the level and the subsystem it was filed
// under. The check's answer on the wire is what it has always been; what the server keeps for itself
// is what this feature changes.
func TestUploadConnectionCheckIsFiledAsACheckAndNotAsALostRecording(t *testing.T) {
	srv, _ := configuredTestServer(t)

	// The station the recorded runs were captured in has auto populate switched off everywhere. A
	// check that names a number this configuration carries no row for is only a reading about a
	// server that would have invented that row, so the switch is turned on for the system the checks
	// name before anything is posted.
	seed(t, srv.db, seedStatement{
		query: `UPDATE "systems" SET "autoPopulate" = 1 WHERE "systemRef" = 10001`,
	})

	counted := []struct{ what, query string }{
		{"the systems", `SELECT COUNT(*) FROM "systems"`},
		{"the talkgroups", `SELECT COUNT(*) FROM "talkgroups"`},
		{"the talkgroup groups", `SELECT COUNT(*) FROM "groups"`},
		{"the talkgroup tags", `SELECT COUNT(*) FROM "tags"`},
	}

	before := map[string]int{}

	for _, table := range counted {
		before[table.what] = countRows(t, srv.db, table.query)
	}

	// The connection check a capture client posts when it wants to know whether the server is
	// listening: its credential, its system number, and the part that says the submission is only a
	// test, and nothing else.
	answered := srv.uploadConnectionCheck(t, uploadedConnectionCheck{
		key:       checkUploadKey,
		systemRef: checkUploadSystem,
		testValue: "1",
	})

	if answered.Code != http.StatusExpectationFailed {
		t.Errorf("a connection check was answered %d: %s, want %d with the answer the client reads as an all-clear", answered.Code, answered.Body.String(), http.StatusExpectationFailed)
	}

	if got := answered.Body.String(); got != "Incomplete call data: no talkgroup\n\n" {
		t.Errorf("a connection check was answered with %q, want the bytes the checking client decides on: %q", got, "Incomplete call data: no talkgroup\n\n")
	}

	// A recording the same capture uploads for real, posted the way it posts one: its credential,
	// its system number, its talkgroup number, the moment it happened, and the audio.
	happenedAt := time.Now().Truncate(time.Millisecond)

	accepted := srv.uploadRecording(t, uploadedRecording{
		key:       checkUploadKey,
		systemRef: checkUploadSystem,
		talkgroup: 1514000,
		filename:  postedRecordingFile,
		audio:     recording(2048),
		at:        happenedAt,
	})

	if accepted.Code != http.StatusOK {
		t.Fatalf("the recording was answered %d: %s, want it accepted", accepted.Code, accepted.Body.String())
	}

	if got := accepted.Body.String(); got != "Call imported successfully.\n" {
		t.Errorf("the recording was answered with %q, want %q", got, "Call imported successfully.\n")
	}

	// A connection check presenting a credential this server does not have: the server refuses the
	// credential, and it says nothing about a check, because a submission it will not accept from
	// that credential is not answered as a check at all.
	unauthorized := srv.uploadConnectionCheck(t, uploadedConnectionCheck{
		key:       unknownUploadKey,
		systemRef: checkUploadSystem,
		testValue: "1",
	})

	if unauthorized.Code != http.StatusUnauthorized {
		t.Errorf("a connection check presenting %s was answered %d: %s, want %d: the credential is refused before anything the submission is short of is asked about", unknownUploadKey, unauthorized.Code, unauthorized.Body.String(), http.StatusUnauthorized)
	}

	if got := unauthorized.Body.String(); got != "Invalid API key for system <nil> talkgroup <nil>.\n" {
		t.Errorf("a connection check presenting %s was answered with %q, want the answer that credential has always been given", unknownUploadKey, got)
	}

	// A connection check naming a talkgroup number this station was never told about, and the moment
	// the client stamps its submission with: it is short of a recording and nothing else, and auto
	// populate is switched on for the system it names.
	naming := srv.uploadConnectionCheck(t, uploadedConnectionCheck{
		key:       checkUploadKey,
		systemRef: checkUploadSystem,
		talkgroup: unconfiguredTalkgroupRef,
		at:        happenedAt,
		testValue: "1",
	})

	if naming.Code != http.StatusExpectationFailed {
		t.Errorf("a connection check naming talkgroup %d was answered %d: %s, want %d with the answer naming what it is short of", unconfiguredTalkgroupRef, naming.Code, naming.Body.String(), http.StatusExpectationFailed)
	}

	if got := naming.Body.String(); got != "Incomplete call data: no audio\n\n" {
		t.Errorf("a connection check naming talkgroup %d was answered with %q, want %q", unconfiguredTalkgroupRef, got, "Incomplete call data: no audio\n\n")
	}

	// Each entry this run earns, read whole, with the level and the subsystem it was filed under. The
	// two refused checks are filed where an operator reads a routine upload problem, in the sentence
	// that names itself as a check; the recording is filed where every accepted call is filed; and the
	// refused credential is filed as it has always been filed.
	for _, entry := range []struct {
		level   string
		source  domain.LogSource
		message string
	}{
		{domain.LogLevelInfo, domain.LogSourceUpload, "the connection check was refused: no talkgroup, presented by the apikey " + checkUploadIdent + " for system 10001"},
		{domain.LogLevelInfo, domain.LogSourceUpload, "the connection check was refused: no audio, presented by the apikey " + checkUploadIdent + " for system 10001"},
		{domain.LogLevelInfo, domain.LogSourceIngest, "call from system 10001 talkgroup 1514000 success, " + postedRecordingFile},
		{domain.LogLevelError, domain.LogSourceUpload, "the upload was refused: the apikey is not valid"},
	} {
		if !hasLogMessage(t, srv.db, entry.message) {
			t.Errorf("the log view does not hold %q, the entry this run files at %s under %s", entry.message, entry.level, entry.source)

			continue
		}

		if got := logLevelOfMessage(t, srv.db, entry.message); got != entry.level {
			t.Errorf("%q is filed at %q, want %s: an operator reads a routine connection check where a routine upload problem is read, not where a lost recording is", entry.message, got, entry.level)
		}

		if got := logSourceOfMessage(t, srv.db, entry.message); got != string(entry.source) {
			t.Errorf("%q is filed under %q, want %s", entry.message, got, entry.source)
		}
	}

	// The two refusals this run holds are told apart by the entry the server keeps for itself, because
	// they are not told apart by the answer they are put on the wire. Nothing here is a recording short
	// of a talkgroup, so no entry is filed in the sentence a lost recording is filed in, and the only
	// entries naming a check are the two the server filed for itself.
	if rows := countLogLines(t, srv.db, "the upload was refused: Incomplete call data: no talkgroup"); rows != 0 {
		t.Errorf("the log view files the lost-recording sentence %d times over a run that posted no recording short of a talkgroup: the check and the recording refusal are being filed in one sentence", rows)
	}

	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "logs" WHERE "message" LIKE '%connection check%'`); rows != 2 {
		t.Errorf("the log view names a check %d times, want the two entries this run's two refused checks leave: a connection check says what it was, and a run posting two of them says it twice", rows)
	}

	// A connection check leaves nothing behind. The only call this server holds is the recording that
	// was uploaded for real, and the numbers only the checks named created no rows, whatever auto
	// populate is switched to do for an accepted recording.
	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "calls"`); rows != 1 {
		t.Errorf("the call history holds %d rows, want the one recording this run posted: a connection check is not stored", rows)
	}

	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "calls" WHERE "audioFilename" = '`+postedRecordingFile+`'`); rows != 1 {
		t.Errorf("the call history holds %d rows named %s, want the one recording this run posted", rows, postedRecordingFile)
	}

	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "talkgroups" WHERE "talkgroupRef" = 999999`); rows != 0 {
		t.Errorf("the talkgroup configuration holds %d rows for talkgroup %d, want none: a connection check names a channel this server has never been told about and invents nothing", rows, unconfiguredTalkgroupRef)
	}

	for _, table := range counted {
		if rows := countRows(t, srv.db, table.query); rows != before[table.what] {
			t.Errorf("%s holds %d rows after the run, want the %d it held before it: the only submission this run stored was a recording naming rows that were already configured", table.what, rows, before[table.what])
		}
	}

	assertEveryServerLogEntry(t, srv.db)
}
