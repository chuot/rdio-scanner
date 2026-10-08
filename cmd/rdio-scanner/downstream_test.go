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

package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// A downstream server is another Rdio Scanner this one re-posts its calls to. The forwarder and the ingest
// fan-out are pinned apart from each other; what these tests add is the arrangement a deployment actually
// runs: the downstreams are rows in the server's own database, the receiving server is a socket on the
// network, and the call that arrives there is the call this server recorded.

// repostKey is the credential these tests upload a recording with, and repostOpenKey is the credential the
// receiving server is configured to accept it under. Two different credentials, because the one names who
// may post to this server and the other names who this server posts to.
const (
	repostKey      = "a-key-that-reposts-nothing"
	repostOpenKey  = "the-credential-the-downstream-was-given"
	repostScopeKey = "a-scope-that-does-not-cover-this"
	repostGoneKey  = "a-downstream-that-is-switched-off"
)

// TestARecordedCallIsRepostedToTheOtherServersWhoseScopeCoversIt is the whole re-post on a running server:
// the downstreams are what the configuration stores, the call is what an uploader sent, and what arrives at
// the receiving server is the call this server stored — its recording, its references, its instant, and the
// credential that server was given.
func TestARecordedCallIsRepostedToTheOtherServersWhoseScopeCoversIt(t *testing.T) {
	open := newDownstreamRecorder(t, http.StatusOK)
	scoped := newDownstreamRecorder(t, http.StatusOK)
	off := newDownstreamRecorder(t, http.StatusOK)
	refused := newDownstreamRecorder(t, http.StatusInternalServerError)

	srv := testServer(t)

	happenedAt := time.Now().Add(-time.Minute).Truncate(time.Millisecond)

	seedRepostTargets(t, srv.db, open, scoped, off, refused)

	if err := srv.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	answered := srv.uploadRecording(t, uploadedRecording{
		key:       repostKey,
		systemRef: 10001,
		talkgroup: 1514000,
		filename:  "reposted.wav",
		audio:     recording(2048),
		at:        happenedAt,
	})

	if answered.Code != http.StatusOK {
		t.Fatalf("the upload was answered %d: %s", answered.Code, answered.Body.String())
	}

	got := open.waitFor(t, 1)

	if got.method != http.MethodPost {
		t.Errorf("the downstream was posted to with %s, want a POST", got.method)
	}

	// The configured address is the server, and the upload path is what the server appends to it.
	if got.path != "/api/call-upload" {
		t.Errorf("the downstream was sent to %s, want the upload endpoint of the address it was configured with", got.path)
	}

	// The recording is the one that was uploaded, byte for byte, and it is the name the call was stored
	// under: the fixture turns audio conversion off, so this is the recording and not something ffmpeg
	// made of it.
	if !bytes.Equal(got.audio, recording(2048)) {
		t.Errorf("the downstream received %d bytes of recording, want the %d bytes that were uploaded", len(got.audio), 2048)
	}

	if got.fields["audioFilename"] != "reposted.wav" {
		t.Errorf("the recording the downstream received is named %q, want the name the call was stored under", got.fields["audioFilename"])
	}

	// The references are the ones the call was resolved to, which is what the receiving server matches its
	// own systems and talkgroups against.
	for field, want := range map[string]string{
		"key":            repostOpenKey,
		"system":         "10001",
		"talkgroup":      "1514000",
		"systemLabel":    "Grand Metro",
		"talkgroupLabel": "Main",
		"talkgroupName":  "Dispatch 1",
		"timestamp":      fmt.Sprintf("%d", happenedAt.UnixMilli()),
	} {
		if got.fields[field] != want {
			t.Errorf("the %s part is %q, want %q", field, got.fields[field], want)
		}
	}

	// Every part the legacy server writes is there, and nothing else is. The order the parts are written in
	// is pinned where the body is built.
	if got, want := partNames(open), sortedPartNames(); got != want {
		t.Errorf("the downstream received the parts %s, want %s", got, want)
	}

	// A downstream that refuses the call is reported, and the call is still a call this server accepted: the
	// re-post is somebody else's server, and its answer is not this server's failure.
	if got := refused.waitFor(t, 1); got.method != http.MethodPost {
		t.Fatalf("the downstream that refuses calls was sent %v, want the call", got)
	}

	waitForLogMentioning(t, srv.db, "downstream.send: bad status: 500 Internal Server Error")

	// A downstream whose scope names a different system is not sent to at all, and a downstream that is
	// switched off is not sent to either. Both had their turn: the fan-out walks the downstreams in order,
	// and the refusal it reached is further down that list than either of these two.
	time.Sleep(200 * time.Millisecond)

	if n := scoped.count(); n != 0 {
		t.Errorf("a downstream whose scope names system 20002 was sent %d calls, want none for a call on system 10001", n)
	}

	if n := off.count(); n != 0 {
		t.Errorf("a switched-off downstream was sent %d calls, want none", n)
	}

	// And the call was told to the listeners and written to the history all the same.
	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "calls"`); rows != 1 {
		t.Errorf("the server stored %d calls, want the one it accepted", rows)
	}

	assertEveryServerLogEntry(t, srv.db)
}

// TestARepostedCallCarriesTheGroupsAndTagItsTalkgroupIsFiledUnder is the part of the re-post that is not
// simply the call: the receiving server is told the group and tag names this server's configuration gives
// the talkgroup, because that is what it files the call under.
func TestARepostedCallCarriesTheGroupsAndTagItsTalkgroupIsFiledUnder(t *testing.T) {
	open := newDownstreamRecorder(t, http.StatusOK)

	srv := testServer(t)

	seedRepostTargets(t, srv.db, open)
	seedFiledTalkgroup(t, srv.db)

	if err := srv.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	srv.uploadRecording(t, uploadedRecording{
		key:       repostKey,
		systemRef: 10001,
		talkgroup: 1514000,
		filename:  "reposted.wav",
		audio:     recording(2048),
		at:        time.Now().Add(-time.Minute),
	})

	got := open.waitFor(t, 1)

	if got.fields["talkgroupGroups"] != "Ground" {
		t.Errorf("the talkgroupGroups part is %q, want the group the talkgroup is filed under", got.fields["talkgroupGroups"])
	}

	if got.fields["talkgroupTag"] != "Dispatch" {
		t.Errorf("the talkgroupTag part is %q, want the tag the talkgroup is filed under", got.fields["talkgroupTag"])
	}
}

// seedRepostTargets installs the downstreams these tests re-post to. Each one is a row of the same shape a
// configuration save writes, so that what is being tested is the stored scope and the stored switch rather
// than an in-memory stand-in.
func seedRepostTargets(t *testing.T, db *sql.DB, targets ...*downstreamRecorder) {
	t.Helper()

	seedUploadTarget(t, db, 0, repostKey)

	if len(targets) < 1 {
		t.Fatalf("these tests re-post to at least one downstream")
	}

	// The first target covers every system, and is the one the call is expected to reach.
	seed(t, db, seedStatement{
		query: `INSERT INTO "downstreams" ("downstreamId", "apikey", "disabled", "order", "systems", "url") VALUES (1, ?, 0, 0, '"*"', ?)`,
		args:  []any{repostOpenKey, targets[0].server.URL},
	})

	if len(targets) < 4 {
		return
	}

	// The second names a system this call was not heard on. The third is switched off. The fourth is the
	// server that answers every call with a refusal.
	seed(t, db,
		seedStatement{
			query: `INSERT INTO "downstreams" ("downstreamId", "apikey", "disabled", "order", "systems", "url") VALUES (2, ?, 0, 0, '[{"id":20002,"talkgroups":"*"}]', ?)`,
			args:  []any{repostScopeKey, targets[1].server.URL},
		},
		seedStatement{
			query: `INSERT INTO "downstreams" ("downstreamId", "apikey", "disabled", "order", "systems", "url") VALUES (3, ?, 1, 0, '"*"', ?)`,
			args:  []any{repostGoneKey, targets[2].server.URL},
		},
		seedStatement{
			query: `INSERT INTO "downstreams" ("downstreamId", "apikey", "disabled", "order", "systems", "url") VALUES (4, ?, 0, 0, '"*"', ?)`,
			args:  []any{repostOpenKey, targets[3].server.URL},
		},
	)
}

// seedFiledTalkgroup files the talkgroup these tests re-post onto under one group and gives it a tag, which
// is what the receiving server is told the call belongs to.
func seedFiledTalkgroup(t *testing.T, db *sql.DB) {
	t.Helper()

	seed(t, db,
		seedStatement{
			query: `INSERT INTO "groups" ("groupId", "alert", "label", "order") VALUES (100, '', 'Ground', 0)`,
		},
		seedStatement{
			query: `INSERT INTO "talkgroupGroups" ("groupId", "talkgroupId") VALUES (100, 1)`,
		},
		seedStatement{
			query: `UPDATE "tags" SET "label" = 'Dispatch' WHERE "tagId" = 9`,
		},
	)
}

// repostPartNames are the parts the legacy server writes into a re-posted call, in the order it writes them.
func repostPartNames() []string {
	return []string{
		"audio",
		"audioFilename",
		"audioMime",
		"dateTime",
		"frequencies",
		"key",
		"patches",
		"system",
		"systemLabel",
		"talkgroup",
		"talkgroupGroups",
		"talkgroupLabel",
		"talkgroupName",
		"talkgroupTag",
		"timestamp",
		"units",
	}
}

// sortedPartNames is the legacy part list in a fixed order, for comparing with what arrived.
func sortedPartNames() string {
	names := append([]string{}, repostPartNames()...)

	sort.Strings(names)

	return strings.Join(names, " ")
}

// recordedUpload is one call a receiving server was sent.
type recordedUpload struct {
	method   string
	path     string
	fields   map[string]string
	audio    []byte
	hasAudio bool
}

// downstreamRecorder stands in for another Rdio Scanner: it answers every upload the way a receiving server
// answers, and remembers what it was sent.
type downstreamRecorder struct {
	server   *httptest.Server
	status   int
	mutex    sync.Mutex
	received []recordedUpload
}

func newDownstreamRecorder(t *testing.T, status int) *downstreamRecorder {
	t.Helper()

	recorder := &downstreamRecorder{status: status}

	recorder.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upload := recordedUpload{
			method: r.Method,
			path:   r.URL.Path,
			fields: map[string]string{},
		}

		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Errorf("the re-post to %s carries %q, want a multipart body", r.URL.Path, r.Header.Get("Content-Type"))
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		reader := multipart.NewReader(r.Body, params["boundary"])

		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}

			if err != nil {
				t.Errorf("reading the re-posted call: %v", err)
				break
			}

			body, err := io.ReadAll(part)
			if err != nil {
				t.Errorf("reading the %s part of the re-posted call: %v", part.FormName(), err)
				break
			}

			if part.FormName() == "audio" {
				upload.audio = body
				upload.hasAudio = true

				continue
			}

			upload.fields[part.FormName()] = string(body)
		}

		recorder.mutex.Lock()
		recorder.received = append(recorder.received, upload)
		recorder.mutex.Unlock()

		w.WriteHeader(recorder.status)
	}))

	t.Cleanup(recorder.server.Close)

	return recorder
}

// waitFor reports the first call this server was sent, waiting only as long as a re-post that is going to
// arrive needs.
func (recorder *downstreamRecorder) waitFor(t *testing.T, want int) recordedUpload {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for {
		recorder.mutex.Lock()
		got := append([]recordedUpload{}, recorder.received...)
		recorder.mutex.Unlock()

		if len(got) >= want && want > 0 {
			return got[0]
		}

		if time.Now().After(deadline) {
			t.Fatalf("the receiving server was sent %d calls after 5s, want %d", len(got), want)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

// count reports how many calls this server has been sent so far.
func (recorder *downstreamRecorder) count() int {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()

	return len(recorder.received)
}

// partNames lists the parts a receiving server was sent, in a fixed order, so that a test can say which
// parts arrived without depending on the order they were written in — which is pinned where the body is
// built.
func partNames(recorder *downstreamRecorder) string {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()

	if len(recorder.received) == 0 {
		return ""
	}

	names := []string{}

	if recorder.received[0].hasAudio {
		names = append(names, "audio")
	}

	for _, name := range repostPartNames() {
		if name == "audio" {
			continue
		}

		if _, found := recorder.received[0].fields[name]; found {
			names = append(names, name)
		}
	}

	for name := range recorder.received[0].fields {
		if !strings.Contains(strings.Join(repostPartNames(), " "), name) {
			names = append(names, name)
		}
	}

	sort.Strings(names)

	return strings.Join(names, " ")
}
