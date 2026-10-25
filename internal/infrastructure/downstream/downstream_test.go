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

package downstream

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// receivedUpload is one call-upload a test server was handed: the request path and every
// part of the multipart body, in the order the sender wrote them.
type receivedUpload struct {
	path        string
	contentType string
	names       []string
	values      map[string]string
	filename    string
	audio       []byte
}

// newUploadServer answers like a downstream Rdio Scanner server that took the call, and
// reports what it was given.
func newUploadServer(t *testing.T, received *receivedUpload) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.path = r.URL.Path
		received.contentType = r.Header.Get("Content-Type")

		reader := multipart.NewReader(r.Body, strings.TrimPrefix(received.contentType, "multipart/form-data; boundary="))

		received.values = map[string]string{}

		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}

			if err != nil {
				t.Errorf("reading the upload parts: %v", err)

				break
			}

			body, err := io.ReadAll(part)
			if err != nil {
				t.Errorf("reading the %s part: %v", part.FormName(), err)
			}

			received.names = append(received.names, part.FormName())

			if part.FormName() == "audio" {
				received.filename = part.FileName()
				received.audio = body

				continue
			}

			received.values[part.FormName()] = string(body)

			part.Close()
		}

		w.WriteHeader(http.StatusOK)
	}))
}

// callForForwarding is a call as the ingest path hands it to the forwarder: the system and
// talkgroup already resolved, and the audio already converted.
func callForForwarding() domain.Call {
	call := domain.NewCall()

	call.Audio = []byte("a recording, long enough to be a recording")
	call.AudioFilename = "TG1514000-20261006120000.wav"
	call.AudioMime = "audio/wav"
	call.AudioLength = 3500 * time.Millisecond
	call.PatchTalkgroupRefs = []uint{1514001, 1514002}
	call.Timestamp = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

	call.System = domain.System{Label: "Grand Metro", SystemRef: 10001}
	call.Talkgroup = domain.Talkgroup{
		GroupIDs:     []uint64{2, 1},
		Label:        "Dispatch 1",
		Name:         "Dispatch 1",
		TagID:        3,
		TalkgroupRef: 1514000,
	}

	call.Frequencies = []domain.CallFrequency{{
		ID:        7,
		CallID:    42,
		Dbm:       -95,
		Errors:    1,
		Frequency: 1514000,
		Offset:    1.5,
		Spikes:    2,
	}}

	call.Units = []domain.CallUnit{{
		ID:      8,
		CallID:  42,
		Offset:  0.5,
		UnitRef: 6789,
	}}

	return call
}

// configuredGroupsAndTags is the configuration the two wire fields that carry labels are
// resolved from.
func configuredGroupsAndTags() (*fakes.Groups, *fakes.Tags) {
	return &fakes.Groups{Stored: domain.Groups{
			{ID: 1, Label: "Air"},
			{ID: 2, Label: "Ground"},
		}}, &fakes.Tags{Stored: domain.Tags{
			{ID: 3, Label: "Dispatch"},
		}}
}

// TestForwardPostsTheCallToTheDownstreamsUploadEndpoint is the whole
// shape of a forwarded call: a multipart upload to the receiving
// server's call-upload endpoint, with the apikey among the form fields.
func TestForwardPostsTheCallToTheDownstreamsUploadEndpoint(t *testing.T) {
	received := &receivedUpload{}
	server := newUploadServer(t, received)

	defer server.Close()

	groups, tags := configuredGroupsAndTags()
	forwarder := NewForwarder(groups, tags)

	call := callForForwarding()

	err := forwarder.Forward(context.Background(), ports.DownstreamTarget{
		URL:    server.URL,
		Apikey: "downstream-key",
	}, call)

	if err != nil {
		t.Fatalf("Forward: %v", err)
	}

	if received.path != "/api/call-upload" {
		t.Errorf("the upload went to %q, want %q", received.path, "/api/call-upload")
	}

	if !strings.HasPrefix(received.contentType, "multipart/form-data; boundary=") {
		t.Errorf("the upload was sent as %q, want a multipart upload", received.contentType)
	}

	if received.filename != call.AudioFilename {
		t.Errorf("the audio part is named %q, want the filename the call carries", received.filename)
	}

	if string(received.audio) != string(call.Audio) {
		t.Errorf("the audio part holds %q, want the recording byte for byte", received.audio)
	}

	want := map[string]string{
		"audioFilename":   call.AudioFilename,
		"audioLength":     "3.500",
		"audioMime":       call.AudioMime,
		"dateTime":        "2026-10-06T12:00:00Z",
		"key":             "downstream-key",
		"patches":         "[1514001,1514002]",
		"system":          "10001",
		"systemLabel":     "Grand Metro",
		"talkgroup":       "1514000",
		"talkgroupGroups": "Ground,Air",
		"talkgroupLabel":  "Dispatch 1",
		"talkgroupName":   "Dispatch 1",
		"talkgroupTag":    "Dispatch",
		"timestamp":       "1791288000000",
	}

	for name, value := range want {
		if got := received.values[name]; got != value {
			t.Errorf("the %s field is %q, want %q", name, got, value)
		}
	}
}

// TestForwardWritesEveryPartInTheOrderTheLegacyWritesThem pins the part order, which is the
// part of the request a byte-for-byte comparison against the legacy server sees.
func TestForwardWritesEveryPartInTheOrderTheLegacyWritesThem(t *testing.T) {
	received := &receivedUpload{}
	server := newUploadServer(t, received)

	defer server.Close()

	groups, tags := configuredGroupsAndTags()
	forwarder := NewForwarder(groups, tags)

	if err := forwarder.Forward(context.Background(), ports.DownstreamTarget{
		URL:    server.URL,
		Apikey: "downstream-key",
	}, callForForwarding()); err != nil {
		t.Fatalf("Forward: %v", err)
	}

	want := []string{
		"audio", "audioFilename", "audioLength", "audioMime", "dateTime", "frequencies", "key",
		"patches", "system", "systemLabel", "talkgroup", "talkgroupGroups", "talkgroupLabel",
		"talkgroupName", "talkgroupTag", "timestamp", "units",
	}

	if strings.Join(received.names, ",") != strings.Join(want, ",") {
		t.Errorf("the parts arrived as %v, want the legacy order %v", received.names, want)
	}
}

// A call whose length was never measured forwards no `audioLength`
// field at all, and the receiving server measures its own way: the
// forwarder invents no zero-second recording.
func TestForwardLeavesOutTheAudioLengthWhenTheCallHasNone(t *testing.T) {
	received := &receivedUpload{}
	server := newUploadServer(t, received)

	defer server.Close()

	groups, tags := configuredGroupsAndTags()
	forwarder := NewForwarder(groups, tags)

	call := callForForwarding()
	call.AudioLength = 0

	if err := forwarder.Forward(context.Background(), ports.DownstreamTarget{
		URL:    server.URL,
		Apikey: "downstream-key",
	}, call); err != nil {
		t.Fatalf("Forward: %v", err)
	}

	for _, name := range received.names {
		if name == "audioLength" {
			t.Errorf("the parts arrived as %v, want no audioLength field for a call with no known length", received.names)

			return
		}
	}
}

// TestForwardAppendsTheUploadPathToTheConfiguredAddress pins what the configured address
// means: it is a server address, and the endpoint is added to it.
func TestForwardAppendsTheUploadPathToTheConfiguredAddress(t *testing.T) {
	received := &receivedUpload{}
	server := newUploadServer(t, received)

	defer server.Close()

	groups, tags := configuredGroupsAndTags()
	forwarder := NewForwarder(groups, tags)

	err := forwarder.Forward(context.Background(), ports.DownstreamTarget{
		URL:    server.URL + "/other",
		Apikey: "downstream-key",
	}, callForForwarding())

	if err != nil {
		t.Fatalf("Forward: %v", err)
	}

	if received.path != "/other/api/call-upload" {
		t.Errorf("the upload went to %q, want the upload path appended to the configured path", received.path)
	}
}

// TestForwardAppendsTheUploadPathEvenWhenTheAddressAlreadyNamesIt pins
// a legacy quirk an administrator can actually hit: the full endpoint
// address gets the path appended a second time, and the server answers 404.
func TestForwardAppendsTheUploadPathEvenWhenTheAddressAlreadyNamesIt(t *testing.T) {
	received := &receivedUpload{}
	server := newUploadServer(t, received)

	defer server.Close()

	groups, tags := configuredGroupsAndTags()
	forwarder := NewForwarder(groups, tags)

	err := forwarder.Forward(context.Background(), ports.DownstreamTarget{
		URL:    server.URL + "/api/call-upload",
		Apikey: "downstream-key",
	}, callForForwarding())

	if err != nil {
		t.Fatalf("Forward: %v", err)
	}

	if received.path != "/api/call-upload/api/call-upload" {
		t.Errorf("the upload went to %q, want the legacy outcome: the path appended to itself", received.path)
	}
}

// TestForwardReportsANonSuccessStatusAsAFailure pins the one status the legacy accepts, and
// the wording a deployment's log already greps for.
func TestForwardReportsANonSuccessStatusAsAFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))

	defer server.Close()

	groups, tags := configuredGroupsAndTags()
	forwarder := NewForwarder(groups, tags)

	err := forwarder.Forward(context.Background(), ports.DownstreamTarget{
		URL:    server.URL,
		Apikey: "wrong-key",
	}, callForForwarding())

	if err == nil {
		t.Fatal("a downstream that refused the call is a failure the caller is told about")
	}

	if !strings.Contains(err.Error(), "downstream.send:") {
		t.Errorf("error = %v, want it to name the operation the way the legacy's does", err)
	}

	if !strings.Contains(err.Error(), "bad status: 401") {
		t.Errorf("error = %v, want it to report the status the downstream answered with", err)
	}
}

// TestForwardReportsAnUnreachableDownstreamAsAFailure is the log line a
// deployment with a downstream that is down produces on every call.
func TestForwardReportsAnUnreachableDownstreamAsAFailure(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	address := server.URL
	server.Close()

	groups, tags := configuredGroupsAndTags()
	forwarder := NewForwarder(groups, tags)

	err := forwarder.Forward(context.Background(), ports.DownstreamTarget{
		URL:    address,
		Apikey: "downstream-key",
	}, callForForwarding())

	if err == nil {
		t.Fatal("a downstream that cannot be reached is a failure the caller is told about")
	}

	if !strings.Contains(err.Error(), "downstream.send:") {
		t.Errorf("error = %v, want it to name the operation", err)
	}
}

// TestForwardReportsAConfiguredAddressThatIsNotAnAddressAsAFailure covers the downstream an
// administrator saved with something that is not a URL.
func TestForwardReportsAConfiguredAddressThatIsNotAnAddressAsAFailure(t *testing.T) {
	groups, tags := configuredGroupsAndTags()
	forwarder := NewForwarder(groups, tags)

	err := forwarder.Forward(context.Background(), ports.DownstreamTarget{
		URL:    "http://%zz",
		Apikey: "downstream-key",
	}, callForForwarding())

	if err == nil {
		t.Fatal("a configured address that cannot be parsed is a failure the caller is told about")
	}

	if !strings.Contains(err.Error(), "downstream.send:") {
		t.Errorf("error = %v, want it to name the operation", err)
	}
}

// TestForwardNamesTheTalkgroupsGroupsByLabelAndSkipsAGroupThatIsGone:
// the receiving server has no idea what group 2 is, so the sender
// resolves it, and a group the configuration no longer has is left out
// rather than sent as a number.
func TestForwardNamesTheTalkgroupsGroupsByLabelAndSkipsAGroupThatIsGone(t *testing.T) {
	received := &receivedUpload{}
	server := newUploadServer(t, received)

	defer server.Close()

	groups := &fakes.Groups{Stored: domain.Groups{{ID: 2, Label: "Ground"}}}
	tags := &fakes.Tags{}

	forwarder := NewForwarder(groups, tags)

	if err := forwarder.Forward(context.Background(), ports.DownstreamTarget{
		URL:    server.URL,
		Apikey: "downstream-key",
	}, callForForwarding()); err != nil {
		t.Fatalf("Forward: %v", err)
	}

	if got := received.values["talkgroupGroups"]; got != "Ground" {
		t.Errorf("talkgroupGroups is %q, want only the group the configuration still has", got)
	}

	value, present := received.values["talkgroupTag"]
	if !present {
		t.Fatal("the talkgroupTag field is missing, want it present and empty")
	}

	if value != "" {
		t.Errorf("talkgroupTag is %q, want it empty for a talkgroup whose tag is not configured", value)
	}
}

// TestForwardLeavesTheTagFieldPresentWhenTheTalkgroupHasNoTagConfigured
// pins the legacy shape: the field is written whether or not there is a
// tag to name, so the receiving server sees a call with no tag rather
// than a call that says nothing about tags.
func TestForwardLeavesTheTagFieldPresentWhenTheTalkgroupHasNoTagConfigured(t *testing.T) {
	received := &receivedUpload{}
	server := newUploadServer(t, received)

	defer server.Close()

	groups, tags := configuredGroupsAndTags()
	forwarder := NewForwarder(groups, tags)

	call := callForForwarding()
	call.Talkgroup.TagID = 999

	if err := forwarder.Forward(context.Background(), ports.DownstreamTarget{
		URL:    server.URL,
		Apikey: "downstream-key",
	}, call); err != nil {
		t.Fatalf("Forward: %v", err)
	}

	if !containsName(received.names, "talkgroupTag") {
		t.Fatalf("the parts arrived as %v, want the tag field present", received.names)
	}

	if got := received.values["talkgroupTag"]; got != "" {
		t.Errorf("talkgroupTag is %q, want it empty", got)
	}
}

// TestForwardReportsTheCallInstantTheWayTheUploadEndpointReadsIt pins
// the two fields that carry the same instant in the two vocabularies
// the endpoint accepts: the pre-v7 name as an RFC 3339 string, and the
// current name as milliseconds.
func TestForwardReportsTheCallInstantTheWayTheUploadEndpointReadsIt(t *testing.T) {
	received := &receivedUpload{}
	server := newUploadServer(t, received)

	defer server.Close()

	groups, tags := configuredGroupsAndTags()
	forwarder := NewForwarder(groups, tags)

	call := callForForwarding()
	call.Timestamp = time.Date(2026, 10, 6, 12, 0, 0, 123000000, time.UTC)

	if err := forwarder.Forward(context.Background(), ports.DownstreamTarget{
		URL:    server.URL,
		Apikey: "downstream-key",
	}, call); err != nil {
		t.Fatalf("Forward: %v", err)
	}

	if got := received.values["dateTime"]; got != "2026-10-06T12:00:00Z" {
		t.Errorf("dateTime is %q, want the instant without its fractional part, as the legacy formats it", got)
	}

	if got := received.values["timestamp"]; got != "1791288000123" {
		t.Errorf("timestamp is %q, want the instant in milliseconds", got)
	}
}

// TestForwardMarshalsFrequenciesAndUnitsWithTheLegacyFieldNames pins
// the collision: the sender writes the Go field names of its own
// structs, and the upload endpoint reads `dbm`, `errorCount`, `freq`,
// `pos`, `spikeCount`, `id`, `label`, and `offset`, so a forwarded call
// arrives with its quality measurements and its units dropped. This is
// reproduced rather than repaired.
func TestForwardMarshalsFrequenciesAndUnitsWithTheLegacyFieldNames(t *testing.T) {
	received := &receivedUpload{}
	server := newUploadServer(t, received)

	defer server.Close()

	groups, tags := configuredGroupsAndTags()
	forwarder := NewForwarder(groups, tags)

	if err := forwarder.Forward(context.Background(), ports.DownstreamTarget{
		URL:    server.URL,
		Apikey: "downstream-key",
	}, callForForwarding()); err != nil {
		t.Fatalf("Forward: %v", err)
	}

	wantFrequencies := `[{"CallId":42,"Dbm":-95,"Errors":1,"Frequency":1514000,"Id":7,"Offset":1.5,"Spikes":2}]`
	if got := received.values["frequencies"]; got != wantFrequencies {
		t.Errorf("frequencies is %s, want the legacy field names: %s", got, wantFrequencies)
	}

	wantUnits := `[{"CallId":42,"Id":8,"Offset":0.5,"UnitRef":6789}]`
	if got := received.values["units"]; got != wantUnits {
		t.Errorf("units is %s, want the legacy field names: %s", got, wantUnits)
	}
}

// TestForwardSendsEmptyCollectionsAsEmptyArrays is what a call with no frequencies, units,
// or patches looks like on the wire: an empty array, not a missing field and not a null.
func TestForwardSendsEmptyCollectionsAsEmptyArrays(t *testing.T) {
	received := &receivedUpload{}
	server := newUploadServer(t, received)

	defer server.Close()

	groups, tags := configuredGroupsAndTags()
	forwarder := NewForwarder(groups, tags)

	call := domain.NewCall()

	call.Audio = []byte("a recording, long enough to be a recording")
	call.Timestamp = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

	if err := forwarder.Forward(context.Background(), ports.DownstreamTarget{
		URL:    server.URL,
		Apikey: "downstream-key",
	}, call); err != nil {
		t.Fatalf("Forward: %v", err)
	}

	for _, name := range []string{"frequencies", "patches", "units"} {
		if got := received.values[name]; got != "[]" {
			t.Errorf("%s is %q, want an empty array", name, got)
		}
	}

	if got := received.values["talkgroupGroups"]; got != "" {
		t.Errorf("talkgroupGroups is %q, want it empty for a call whose talkgroup names no groups", got)
	}
}

// TestForwardHonoursTheCallersContext is the deliberate departure from
// the legacy send, which builds its own client and cannot be
// interrupted: a shutting-down server does not wait out the full
// request timeout for every downstream it forwards to.
func TestForwardHonoursTheCallersContext(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))

	defer server.Close()

	groups, tags := configuredGroupsAndTags()
	forwarder := NewForwarder(groups, tags)

	ctx, cancel := context.WithCancel(context.Background())

	cancel()

	err := forwarder.Forward(ctx, ports.DownstreamTarget{
		URL:    server.URL,
		Apikey: "downstream-key",
	}, callForForwarding())

	if err == nil {
		t.Fatal("a cancelled forward is a failure the caller is told about")
	}

	if !strings.Contains(err.Error(), "downstream.send:") {
		t.Errorf("error = %v, want it to name the operation", err)
	}
}

// TestForwardReportsAFailedConfigurationReadAsAFailure covers the two wire fields that come
// from the configuration rather than from the call.
func TestForwardReportsAFailedConfigurationReadAsAFailure(t *testing.T) {
	groups := &fakes.Groups{Err: errors.New("the groups table cannot be read")}
	tags := &fakes.Tags{}

	forwarder := NewForwarder(groups, tags)

	err := forwarder.Forward(context.Background(), ports.DownstreamTarget{
		URL:    "http://downstream.invalid",
		Apikey: "downstream-key",
	}, callForForwarding())

	if err == nil {
		t.Fatal("a configuration the forwarder cannot read is a failure the caller is told about")
	}

	if !strings.Contains(err.Error(), "the groups table cannot be read") {
		t.Errorf("error = %v, want it to carry the reason the read gave", err)
	}
}

// containsName reports whether a part of the upload carries the given field name.
func containsName(names []string, name string) bool {
	for _, candidate := range names {
		if candidate == name {
			return true
		}
	}

	return false
}

// TestForwardRefusesAnAddressThatIsNotAnHttpAddress: the forwarder
// re-checks the scheme at forward time, because an address stored by an
// older server is read by this one. The refusal names the address and
// the schemes it must be, and costs the addressed server nothing.
func TestForwardRefusesAnAddressThatIsNotAnHttpAddress(t *testing.T) {
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++

		w.WriteHeader(http.StatusOK)
	}))

	defer server.Close()

	groups, tags := configuredGroupsAndTags()
	forwarder := NewForwarder(groups, tags)

	// The address names this test's server over a refused scheme: a forward that did
	// not refuse it would be seen by the server.
	err := forwarder.Forward(context.Background(), ports.DownstreamTarget{
		URL:    "ftp://" + server.Listener.Addr().String(),
		Apikey: "downstream-key",
	}, callForForwarding())

	if err == nil {
		t.Fatal("Forward = nil for an ftp:// address, want it refused")
	}

	if !strings.Contains(err.Error(), "http") {
		t.Errorf("Forward error = %q, want it to name the http schemes the address must be", err)
	}

	if requests != 0 {
		t.Errorf("the server was handed %d requests, want none: a refused address is never posted to", requests)
	}
}

// TestForwardReportsARedirectAsAFailureWithoutReplayingTheBody: a
// downstream that answers 301 points the upload somewhere else, and
// today the client follows it. The redirect is a failure the log names,
// and the body is handed to nobody else.
func TestForwardReportsARedirectAsAFailureWithoutReplayingTheBody(t *testing.T) {
	replayed := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/call-upload" {
			w.Header().Set("Location", "/somewhere-else")

			w.WriteHeader(http.StatusMovedPermanently)

			return
		}

		body, _ := io.ReadAll(r.Body)

		if len(body) > 0 {
			replayed++
		}

		w.WriteHeader(http.StatusOK)
	}))

	defer server.Close()

	groups, tags := configuredGroupsAndTags()
	forwarder := NewForwarder(groups, tags)

	err := forwarder.Forward(context.Background(), ports.DownstreamTarget{
		URL:    server.URL,
		Apikey: "downstream-key",
	}, callForForwarding())

	if err == nil {
		t.Fatal("Forward = nil for a 301 downstream, want the redirect reported as a failure")
	}

	if !strings.Contains(err.Error(), "301") {
		t.Errorf("Forward error = %q, want it to name the status the downstream answered", err)
	}

	if replayed != 0 {
		t.Errorf("the redirect target was handed %d bodies, want none: the recording and the apikey go to the address the administrator named, not to wherever a redirect points", replayed)
	}
}

// TestForwardDrainsTheBodyOfARefusedUpload: a refused upload's body is
// read out before the connection is handed back, or every later call to
// the same downstream pays for a fresh connection - a deployment with a
// refusing downstream turns its call log into a connection storm.
func TestForwardDrainsTheBodyOfARefusedUpload(t *testing.T) {
	connections := 0

	requests := 0

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++

		w.WriteHeader(http.StatusInternalServerError)

		w.Write([]byte("the receiving server said no, and its answer has bytes in it"))
	}))

	server.Config.ConnState = func(conn net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections++
		}
	}

	server.Start()

	defer server.Close()

	groups, tags := configuredGroupsAndTags()
	forwarder := NewForwarder(groups, tags)

	for i := 0; i < 2; i++ {
		if err := forwarder.Forward(context.Background(), ports.DownstreamTarget{
			URL:    server.URL,
			Apikey: "downstream-key",
		}, callForForwarding()); err == nil {
			t.Fatal("Forward = nil for a 500 downstream, want it reported as a failure")
		}
	}

	if requests != 2 {
		t.Fatalf("the server saw %d requests, want the two uploads", requests)
	}

	if connections != 1 {
		t.Errorf("the server saw %d connections for two refused uploads, want 1: the refused body was left unread and the connection thrown away", connections)
	}
}
