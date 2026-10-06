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

package parsers

import (
	"bytes"
	"io"
	"mime/multipart"
	"testing"
	"time"

	"rdio-scanner/server-ng/internal/domain"
)

// The call-upload endpoints declare the reading they need themselves, because they may not import this
// package (contract-server-ng.md §1) and the composition root is what wires it to them. The shape is
// asserted here so that a change to this parser is caught by this suite rather than by a build of the
// composition root.
var _ interface {
	ApplyMultipart(part *multipart.Part, data []byte, call domain.Call) domain.Call
	ApplyTrunkRecorderSidecar(call *domain.Call, sidecar []byte) error
} = (*Parsers)(nil)

// uploadField builds one field of a call upload and hands back the part and its contents,
// which is the pair the upload endpoint hands the parser for each field it reads.
func uploadField(t *testing.T, name, value string) (*multipart.Part, []byte) {
	t.Helper()

	return uploadFileField(t, name, "", value)
}

// uploadFileField builds one field of a call upload, with a filename when the field is the
// recording itself.
func uploadFileField(t *testing.T, name, filename, value string) (*multipart.Part, []byte) {
	t.Helper()

	body := bytes.Buffer{}
	writer := multipart.NewWriter(&body)

	var (
		part io.Writer
		err  error
	)

	if len(filename) > 0 {
		part, err = writer.CreateFormFile(name, filename)
	} else {
		part, err = writer.CreateFormField(name)
	}

	if err != nil {
		t.Fatalf("CreateFormField(%s): %v", name, err)
	}

	if _, err := part.Write([]byte(value)); err != nil {
		t.Fatalf("writing the %s field: %v", name, err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reader := multipart.NewReader(&body, writer.Boundary())

	field, err := reader.NextPart()
	if err != nil {
		t.Fatalf("NextPart: %v", err)
	}

	data, err := io.ReadAll(field)
	if err != nil {
		t.Fatalf("reading the %s field: %v", name, err)
	}

	return field, data
}

// applyUploadFields reads a list of fields into one call, in the order an upload carries them.
func applyUploadFields(t *testing.T, call domain.Call, fields ...[2]string) domain.Call {
	t.Helper()

	for _, field := range fields {
		part, data := uploadField(t, field[0], field[1])

		call = ApplyMultipart(part, data, call)
	}

	return call
}

// TestApplyMultipartUnitsPairEachSourceWithItsLabel is the suite the legacy server's own
// tests cover, ported: the units heard during a call are the call's own history, and the
// labels are metadata paired with the references they describe.
func TestApplyMultipartUnitsPairEachSourceWithItsLabel(t *testing.T) {
	call := applyUploadFields(t, domain.NewCall(),
		[2]string{"units", `[{"id":710618,"label":"Gastonia PD Dispatch","offset":0.03}]`},
	)

	if len(call.Units) != 1 || call.Units[0].UnitRef != 710618 || call.Units[0].Offset != 0.03 {
		t.Fatalf("the call units are %#v, want one unit at reference 710618, 0.03 in", call.Units)
	}

	if len(call.Meta.UnitRefs) != 1 || call.Meta.UnitRefs[0] != 710618 {
		t.Errorf("the unit references are %#v, want it paired with the label", call.Meta.UnitRefs)
	}

	if len(call.Meta.UnitLabels) != 1 || call.Meta.UnitLabels[0] != "Gastonia PD Dispatch" {
		t.Errorf("the unit labels are %#v, want the label the upload named", call.Meta.UnitLabels)
	}
}

// TestApplyMultipartUnlabeledUnitDoesNotInventMetadata is the other half of the pairing: a
// radio the upload names without naming it is recorded, and nothing is invented for it.
func TestApplyMultipartUnlabeledUnitDoesNotInventMetadata(t *testing.T) {
	call := applyUploadFields(t, domain.NewCall(),
		[2]string{"units", `[{"id":710618,"offset":0.03}]`},
	)

	if len(call.Units) != 1 || call.Units[0].UnitRef != 710618 || call.Units[0].Offset != 0.03 {
		t.Fatalf("the call units are %#v, want the unit the upload named", call.Units)
	}

	if len(call.Meta.UnitRefs) != 0 || len(call.Meta.UnitLabels) != 0 {
		t.Errorf("an unlabeled unit produced metadata: references %#v labels %#v, want none",
			call.Meta.UnitRefs, call.Meta.UnitLabels)
	}
}

// TestApplyMultipartUnitsPairEachLabelOnce keeps a label from being claimed twice for the
// same radio, which is what an application that reports the same unit for every chunk of one
// call would otherwise produce.
func TestApplyMultipartUnitsPairEachLabelOnce(t *testing.T) {
	call := applyUploadFields(t, domain.NewCall(),
		[2]string{"units", `[
			{"id":100,"label":"Engine 1","offset":0.01},
			{"id":200,"label":"Engine 2","offset":0.02},
			{"id":100,"label":"Engine 1 duplicate","offset":0.03}
		]`},
	)

	if len(call.Units) != 3 {
		t.Fatalf("the call units are %#v, want all three times the radio spoke", call.Units)
	}

	wantRefs := []uint{100, 200}
	if len(call.Meta.UnitRefs) != len(wantRefs) {
		t.Fatalf("the unit references are %#v, want %#v", call.Meta.UnitRefs, wantRefs)
	}

	for i, ref := range wantRefs {
		if call.Meta.UnitRefs[i] != ref {
			t.Errorf("the unit references are %#v, want %#v", call.Meta.UnitRefs, wantRefs)
		}
	}

	wantLabels := []string{"Engine 1", "Engine 2"}
	if len(call.Meta.UnitLabels) != len(wantLabels) {
		t.Fatalf("the unit labels are %#v, want %#v", call.Meta.UnitLabels, wantLabels)
	}

	for i, label := range wantLabels {
		if call.Meta.UnitLabels[i] != label {
			t.Errorf("the unit labels are %#v, want %#v", call.Meta.UnitLabels, wantLabels)
		}
	}
}

// TestApplyMultipartUnitsMixLabeledAndUnlabeled keeps the pairing lined up when only some of
// the times a radio spoke came with a name.
func TestApplyMultipartUnitsMixLabeledAndUnlabeled(t *testing.T) {
	call := applyUploadFields(t, domain.NewCall(),
		[2]string{"units", `[
			{"id":100,"label":"Engine 1","offset":0.01},
			{"id":200,"offset":0.02},
			{"id":300,"label":"Engine 3","offset":0.03}
		]`},
	)

	if len(call.Units) != 3 {
		t.Fatalf("the call units are %#v, want all three times a radio spoke", call.Units)
	}

	if len(call.Meta.UnitRefs) != 2 || call.Meta.UnitRefs[0] != 100 || call.Meta.UnitRefs[1] != 300 {
		t.Errorf("the unit references are %#v, want 100 and 300", call.Meta.UnitRefs)
	}

	if len(call.Meta.UnitLabels) != 2 || call.Meta.UnitLabels[0] != "Engine 1" || call.Meta.UnitLabels[1] != "Engine 3" {
		t.Errorf("the unit labels are %#v, want Engine 1 and Engine 3", call.Meta.UnitLabels)
	}
}

// TestApplyMultipartMalformedUnitLabelsDoNotBreakHistory is the reason the pairing is a
// pairing and not a position: a label that is not a label at all costs the call its name for
// that radio, and nothing else about the call.
func TestApplyMultipartMalformedUnitLabelsDoNotBreakHistory(t *testing.T) {
	call := applyUploadFields(t, domain.NewCall(),
		[2]string{"units", `[
			{"id":100,"label":123,"offset":0.01},
			{"id":200,"label":"","offset":0.02},
			{"id":300,"label":null,"offset":0.03},
			{"id":400,"label":"Valid","offset":0.04}
		]`},
	)

	if len(call.Units) != 4 {
		t.Fatalf("the call units are %#v, want all four times a radio spoke", call.Units)
	}

	if len(call.Meta.UnitRefs) != 1 || call.Meta.UnitRefs[0] != 400 {
		t.Errorf("the unit references are %#v, want only 400", call.Meta.UnitRefs)
	}

	if len(call.Meta.UnitLabels) != 1 || call.Meta.UnitLabels[0] != "Valid" {
		t.Errorf("the unit labels are %#v, want only Valid", call.Meta.UnitLabels)
	}
}

// TestApplyMultipartLegacySourceAndSourcesRemainCompatible is the pre-v7 vocabulary: an
// application that still sends `source` and `sources` is read, and the units it names are
// recorded the same way the current vocabulary records them.
func TestApplyMultipartLegacySourceAndSourcesRemainCompatible(t *testing.T) {
	call := applyUploadFields(t, domain.NewCall(),
		[2]string{"source", "42"},
		[2]string{"sources", `[
			{"src":42,"pos":0.01},
			{"src":43,"pos":0.02,"tag":"Legacy Unit"}
		]`},
	)

	if len(call.Units) != 3 {
		t.Fatalf("the call units are %#v, want the unit from source and the two from sources", call.Units)
	}

	if call.Units[0].UnitRef != 42 || call.Units[1].UnitRef != 42 || call.Units[2].UnitRef != 43 {
		t.Errorf("the call units are %#v, want references 42, 42, 43", call.Units)
	}

	if len(call.Meta.UnitRefs) != 1 || call.Meta.UnitRefs[0] != 43 {
		t.Errorf("the unit references are %#v, want only 43", call.Meta.UnitRefs)
	}

	if len(call.Meta.UnitLabels) != 1 || call.Meta.UnitLabels[0] != "Legacy Unit" {
		t.Errorf("the unit labels are %#v, want only Legacy Unit", call.Meta.UnitLabels)
	}
}

// TestApplyMultipartChannelTypeKeepsTheTwoNamesItAccepts pins the field that says what kind
// of channel the call was on, and that anything else is not a channel type.
func TestApplyMultipartChannelTypeKeepsTheTwoNamesItAccepts(t *testing.T) {
	call := applyUploadFields(t, domain.NewCall(),
		[2]string{"channelType", " conventional "},
		[2]string{"talkgroupLabel", "GASTON-FIRE"},
	)

	if call.Meta.ChannelType != "conventional" {
		t.Errorf("the channel type is %q, want conventional, trimmed and lowercased", call.Meta.ChannelType)
	}

	if call.Meta.TalkgroupLabel != "GASTON-FIRE" {
		t.Errorf("the talkgroup label is %q, want GASTON-FIRE", call.Meta.TalkgroupLabel)
	}

	other := applyUploadFields(t, domain.NewCall(), [2]string{"channelType", "frequency-ish"})

	if other.Meta.ChannelType != "" {
		t.Errorf("the channel type is %q, want a name the server does not know to be read as nothing", other.Meta.ChannelType)
	}
}

// TestApplyMultipartAudioPartCarriesTheRecordingAndItsFilename is the field every upload
// sends: the recording, under the filename the uploading application gave it.
func TestApplyMultipartAudioPartCarriesTheRecordingAndItsFilename(t *testing.T) {
	part, data := uploadFileField(t, "audio", "TG1514000-20261006120000.wav", "a recording")

	call := ApplyMultipart(part, data, domain.NewCall())

	if call.AudioFilename != "TG1514000-20261006120000.wav" {
		t.Errorf("the audio filename is %q, want the name the upload gave the recording", call.AudioFilename)
	}

	if string(call.Audio) != "a recording" {
		t.Errorf("the audio is %q, want the recording byte for byte", call.Audio)
	}
}

// TestApplyMultipartAudioFilenameDerivesTheMediaTypeFromIt is how a call gets a media type
// when the upload only names a filename — and why an upload naming both says what the last
// one said.
func TestApplyMultipartAudioFilenameDerivesTheMediaTypeFromIt(t *testing.T) {
	call := applyUploadFields(t, domain.NewCall(),
		[2]string{"audioName", "call.mp3"},
	)

	if call.AudioFilename != "call.mp3" {
		t.Errorf("the audio filename is %q, want the name the upload gave the recording", call.AudioFilename)
	}

	if call.AudioMime != "audio/mpeg" {
		t.Errorf("the audio media type is %q, want the one the filename's extension names", call.AudioMime)
	}

	call = applyUploadFields(t, call,
		[2]string{"audioType", "audio/ogg"},
	)

	if call.AudioMime != "audio/ogg" {
		t.Errorf("the audio media type is %q, want the one the later field named", call.AudioMime)
	}
}

// TestApplyMultipartReadsTheInstantFromEveryNameTheEndpointAccepts pins the three fields that
// carry the moment the call happened, in the two vocabularies the endpoint has carried.
func TestApplyMultipartReadsTheInstantFromEveryNameTheEndpointAccepts(t *testing.T) {
	fromSeconds := applyUploadFields(t, domain.NewCall(), [2]string{"dateTime", "1791288000"})

	if !fromSeconds.Timestamp.Equal(time.Unix(1791288000, 0)) {
		t.Errorf("dateTime as a second count is %v, want that moment", fromSeconds.Timestamp)
	}

	fromText := applyUploadFields(t, domain.NewCall(), [2]string{"dateTime", "2026-10-06T12:00:00Z"})

	if !fromText.Timestamp.Equal(time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("dateTime as an instant is %v, want 2026-10-06T12:00:00Z", fromText.Timestamp)
	}

	fromMillis := applyUploadFields(t, domain.NewCall(), [2]string{"timestamp", "1791288000123"})

	if !fromMillis.Timestamp.Equal(time.UnixMilli(1791288000123)) {
		t.Errorf("timestamp is %v, want the millisecond the upload named", fromMillis.Timestamp)
	}

	unreadable := applyUploadFields(t, domain.NewCall(), [2]string{"dateTime", "not a moment"})

	if !unreadable.Timestamp.IsZero() {
		t.Errorf("dateTime is %v, want a value that is neither a second count nor an instant to leave the call undated", unreadable.Timestamp)
	}
}

// TestApplyMultipartFrequenciesReadEveryQualityMeasurementAndReplaceTheList pins the carrier
// list, and that a second frequency field replaces the first rather than adding to it.
func TestApplyMultipartFrequenciesReadEveryQualityMeasurementAndReplaceTheList(t *testing.T) {
	call := applyUploadFields(t, domain.NewCall(),
		[2]string{"frequency", "1514000"},
		[2]string{"frequencies", `[
			{"dbm":-95,"errorCount":1,"freq":1514000,"pos":1.5,"spikeCount":2},
			{"dbm":72,"freq":1514001},
			{"freq":0,"pos":-1}
		]`},
	)

	if len(call.Frequencies) != 3 {
		t.Fatalf("the call carries %d frequencies, want the three the list named and not the one the single field named", len(call.Frequencies))
	}

	first := call.Frequencies[0]

	// The signal level is the one measurement the parser refuses to store as the upload
	// named it: a level is negative in the unit the recording tools report it in, and the
	// parser only stores a level that is not. See quirk Q41.
	if first.Dbm != 0 {
		t.Errorf("the first frequency's signal level is %d, want the negative level the upload named to be left at zero", first.Dbm)
	}

	if first.Errors != 1 || first.Frequency != 1514000 || first.Offset != 1.5 || first.Spikes != 2 {
		t.Errorf("the first frequency is %#v, want every other measurement the upload named", first)
	}

	if second := call.Frequencies[1]; second.Dbm != 72 || second.Frequency != 1514001 {
		t.Errorf("the second frequency is %#v, want a level that is not negative to be stored", second)
	}

	if third := call.Frequencies[2]; third.Frequency != 0 || third.Offset != 0 {
		t.Errorf("the third frequency is %#v, want a carrier of zero and a negative position to be read as nothing", third)
	}
}

// TestApplyMultipartFrequenciesThatAreNotAListLeaveTheCallAlone covers an upload whose JSON is
// broken or is an object: the field is read as nothing, and the rest of the upload still lands.
func TestApplyMultipartFrequenciesThatAreNotAListLeaveTheCallAlone(t *testing.T) {
	call := applyUploadFields(t, domain.NewCall(),
		[2]string{"frequency", "1514000"},
		[2]string{"frequencies", `{"freq":1514000}`},
		[2]string{"talkgroup", "1514000"},
	)

	if len(call.Frequencies) != 1 {
		t.Errorf("the call carries %d frequencies, want the one the single field named", len(call.Frequencies))
	}

	if call.Meta.TalkgroupRef != 1514000 {
		t.Errorf("the talkgroup reference is %d, want the field after the broken one to still land", call.Meta.TalkgroupRef)
	}
}

// TestApplyMultipartTalkgroupFieldsIgnoreTheDashRecordingToolsWrite pins the value every
// recording tool writes when it has nothing to say, and that it is not a name.
func TestApplyMultipartTalkgroupFieldsIgnoreTheDashRecordingToolsWrite(t *testing.T) {
	call := applyUploadFields(t, domain.NewCall(),
		[2]string{"talkgroupGroup", "-"},
		[2]string{"talkgroupLabel", "-"},
		[2]string{"talkgroupName", "-"},
		[2]string{"talkgroupTag", "-"},
	)

	if len(call.Meta.TalkgroupGroup) != 0 {
		t.Errorf("the talkgroup groups are %#v, want a dash to name no group", call.Meta.TalkgroupGroup)
	}

	if call.Meta.TalkgroupLabel != "" || call.Meta.TalkgroupName != "" || call.Meta.TalkgroupTag != "" {
		t.Errorf("the talkgroup names are %q %q %q, want a dash to name none of them",
			call.Meta.TalkgroupLabel, call.Meta.TalkgroupName, call.Meta.TalkgroupTag)
	}

	named := applyUploadFields(t, domain.NewCall(),
		[2]string{"talkgroupGroups", "Ground,Air"},
	)

	if len(named.Meta.TalkgroupGroup) != 2 || named.Meta.TalkgroupGroup[0] != "Ground" || named.Meta.TalkgroupGroup[1] != "Air" {
		t.Errorf("the talkgroup groups are %#v, want the comma-separated list split", named.Meta.TalkgroupGroup)
	}
}

// TestApplyMultipartPatchesAcceptBothNames is the field that says which talkgroups the call
// was patched to, under the name v7 writes and the name the older applications send.
func TestApplyMultipartPatchesAcceptBothNames(t *testing.T) {
	call := applyUploadFields(t, domain.NewCall(),
		[2]string{"patched_talkgroups", "[1514001,0]"},
		[2]string{"patches", "[1514002]"},
	)

	if len(call.PatchTalkgroupRefs) != 2 || call.PatchTalkgroupRefs[0] != 1514001 || call.PatchTalkgroupRefs[1] != 1514002 {
		t.Errorf("the patched talkgroups are %#v, want both names read and a zero reference left out", call.PatchTalkgroupRefs)
	}
}

// TestApplyMultipartSiteNamesTheSiteTheUploadHeardAt pins the one site the call carries: the
// reference the upload names, which is stored with the call and shown to clients.
func TestApplyMultipartSiteNamesTheSiteTheUploadHeardAt(t *testing.T) {
	call := applyUploadFields(t, domain.NewCall(), [2]string{"site", "7"})

	if call.SiteRef != 7 {
		t.Errorf("the site reference is %d, want the one the upload named", call.SiteRef)
	}

	unknown := applyUploadFields(t, domain.NewCall(), [2]string{"site", "somewhere"})

	if unknown.SiteRef != 0 {
		t.Errorf("the site reference is %d, want a value that is not a reference to name no site", unknown.SiteRef)
	}
}

// TestApplyMultipartUnknownFieldIsReadAsNothing is what lets the endpoint grow: an application
// sending a field this server has never heard of is not refused, and the fields it knows still
// land.
func TestApplyMultipartUnknownFieldIsReadAsNothing(t *testing.T) {
	call := applyUploadFields(t, domain.NewCall(),
		[2]string{"talkgroupLabel", "Dispatch 1"},
		[2]string{"someFutureField", "whatever"},
		[2]string{"talkgroup", "1514000"},
	)

	if call.Meta.TalkgroupLabel != "Dispatch 1" || call.Meta.TalkgroupRef != 1514000 {
		t.Errorf("the call names talkgroup %d %q, want the fields around the unknown one to land",
			call.Meta.TalkgroupRef, call.Meta.TalkgroupLabel)
	}
}

// The order the fields arrive in decides which of two fields naming the same thing wins, because each one
// writes over what an earlier one wrote.
//
// This is the legacy server's own behaviour for its call-upload endpoint. Its trunk-recorder endpoint
// collects the fields into a map and applies them in whatever order Go walks that map in, so the same upload
// is read differently on different runs — quirk Q57, and not reproduced: both endpoints apply the fields as
// they arrived.
func TestApplyMultipartAppliesFieldsInTheOrderTheyArrived(t *testing.T) {
	call := domain.NewCall()

	part, data := uploadFileField(t, "audio", "first.wav", "a recording")
	call = ApplyMultipart(part, data, call)

	part, data = uploadField(t, "audioFilename", "second.wav")
	call = ApplyMultipart(part, data, call)

	if call.AudioFilename != "second.wav" {
		t.Errorf("the recording is named %q, want the name that arrived last", call.AudioFilename)
	}

	reversed := domain.NewCall()

	part, data = uploadField(t, "audioFilename", "second.wav")
	reversed = ApplyMultipart(part, data, reversed)

	part, data = uploadFileField(t, "audio", "first.wav", "a recording")
	reversed = ApplyMultipart(part, data, reversed)

	if reversed.AudioFilename != "first.wav" {
		t.Errorf("the recording is named %q, want the name that arrived last", reversed.AudioFilename)
	}
}

// The dash some recording tools write in place of a name is a value for a system and not for a talkgroup.
// The asymmetry is the legacy server's own, and it decides whether a call naming "-" is matched to a system
// by label: a system label of "-" is looked up as a system actually named "-", and a talkgroup label of "-"
// is read as no label at all.
func TestApplyMultipartSystemLabelTakesTheDashRecordingToolsWrite(t *testing.T) {
	call := applyUploadFields(t, domain.NewCall(),
		[2]string{"systemLabel", "-"},
		[2]string{"talkgroupLabel", "-"},
	)

	if call.Meta.SystemLabel != "-" {
		t.Errorf("the call names system label %q, want the dash the upload carried: the legacy server reads a "+
			"system label with no guard on it at all", call.Meta.SystemLabel)
	}

	if len(call.Meta.TalkgroupLabel) != 0 {
		t.Errorf("the call names talkgroup label %q, want nothing: the talkgroup fields ignore the dash",
			call.Meta.TalkgroupLabel)
	}
}

// A timestamp of zero is the one moment this guard refuses. A submission that names no moment at all is
// accepted and recorded at the year 1, which is quirk Q42 and is preserved; an upload that explicitly says
// the epoch is the case the guard was written for, and it does catch it.
func TestApplyMultipartTimestampOfZeroIsTheMomentTheServerRefuses(t *testing.T) {
	const recording = "a recording long enough to be a recording, and then some more of it"

	stamped := applyUploadFields(t, domain.NewCall(),
		[2]string{"audio", recording},
		[2]string{"system", "10001"},
		[2]string{"talkgroup", "1514000"},
		[2]string{"timestamp", "0"},
	)

	if err := stamped.IsValid(); err == nil || err.Error() != "no timestamp" {
		t.Errorf("a call stamped at the epoch reports %v, want the refusal the guard was written for", err)
	}

	unstamped := applyUploadFields(t, domain.NewCall(),
		[2]string{"audio", recording},
		[2]string{"system", "10001"},
		[2]string{"talkgroup", "1514000"},
	)

	if err := unstamped.IsValid(); err != nil {
		t.Errorf("a call naming no moment reports %v, want it accepted: quirk Q42 is preserved", err)
	}

	if unstamped.Timestamp.Year() != 1 {
		t.Errorf("a call naming no moment is dated to the year %d, want the year 1 the guard was written to "+
			"catch and does not", unstamped.Timestamp.Year())
	}
}
