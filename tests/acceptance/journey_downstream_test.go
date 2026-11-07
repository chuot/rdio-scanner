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

// A downstream server is another Rdio Scanner this one re-posts
// its calls to; these tests pin the real arrangement: downstreams
// as database rows and a receiving server on the loopback.

// repostKey is the credential these tests upload with, and
// repostOpenKey the one the receiving server accepts it under:
// one names who may post here, the other who this server posts
const (
	repostKey      = "a-key-that-reposts-nothing"
	repostOpenKey  = "the-credential-the-downstream-was-given"
	repostScopeKey = "a-scope-that-does-not-cover-this"
	repostGoneKey  = "a-downstream-that-is-switched-off"
)

// The whole re-post on a running server: the call an uploader sent
// arrives at the receiver as this server stored it, recording,
// references, instant, and credential included
func TestARecordedCallIsRepostedToTheOtherServersWhoseScopeCoversIt(t *testing.T) {
	open := newDownstreamRecorder(t, http.StatusOK)
	scoped := newDownstreamRecorder(t, http.StatusOK)
	off := newDownstreamRecorder(t, http.StatusOK)
	refused := newDownstreamRecorder(t, http.StatusInternalServerError)

	srv := journeyServerIn(t, t.TempDir())

	happenedAt := time.Now().Add(-time.Minute).Truncate(time.Millisecond)

	seedRepostTargets(t, srv.db, open, scoped, off, refused)

	if err := srv.server.Start(context.Background()); err != nil {
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

	// the recording arrives byte for byte, the name the call was
	// stored under: the fixture turns conversion off, so this is
	// the uploaded recording and not ffmpeg's work
	if !bytes.Equal(got.audio, recording(2048)) {
		t.Errorf("the downstream received %d bytes of recording, want the %d bytes that were uploaded", len(got.audio), 2048)
	}

	if got.fields["audioFilename"] != "reposted.wav" {
		t.Errorf("the recording the downstream received is named %q, want the name the call was stored under", got.fields["audioFilename"])
	}

	// the references are the ones the call was resolved to, what
	// the receiving server matches its own systems against
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

	// every part the legacy server writes is there, and nothing else
	// is: the writing order is pinned elsewhere, where the body is built
	if got, want := partNames(open), sortedPartNames(); got != want {
		t.Errorf("the downstream received the parts %s, want %s", got, want)
	}

	// a downstream that refuses the call is reported, and the call
	// stays accepted here: another server's answer is not this one's
	if got := refused.waitFor(t, 1); got.method != http.MethodPost {
		t.Fatalf("the downstream that refuses calls was sent %v, want the call", got)
	}

	waitForLogMentioning(t, srv.db, "downstream.send: bad status: 500 Internal Server Error")

	// a downstream whose scope names a different system and one
	// switched off are not sent to at all: the fan-out walks the
	// downstreams in order, and the refusal it reached sits below
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

// The part of the re-post that is not the call: the receiver is told
// the group and tag names the talkgroup has
func TestARepostedCallCarriesTheGroupsAndTagItsTalkgroupIsFiledUnder(t *testing.T) {
	open := newDownstreamRecorder(t, http.StatusOK)

	srv := journeyServerIn(t, t.TempDir())

	seedRepostTargets(t, srv.db, open)
	seedFiledTalkgroup(t, srv.db)

	if err := srv.server.Start(context.Background()); err != nil {
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

// seedRepostTargets installs these tests' downstreams, each a
// row of the same shape a configuration save writes, so the
// stored scope and switch are what is tested
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

	// the second names a system this call was not heard on, the
	// third is switched off, the fourth refuses every call
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

// seedFiledTalkgroup files the re-posted talkgroup under one group
// with a tag, what the receiving server is told the call belongs to
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

// repostPartNames are the parts, in the legacy server's write order
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

// sortedPartNames is the legacy part list, re-sorted to compare arrivals
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

// downstreamRecorder stands in for another Rdio Scanner: it answers
// uploads and remembers what it was sent
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

// partNames lists the parts a receiving server was sent, in a
// fixed order, so a test can say which arrived without the
// writing order, which is pinned where the body is built
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
