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
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/domain"
)

// watchedFileIn puts a recording in a named directory under the watched directory, which is
// how dsd+ writes: one directory per day, named for the day.
func watchedFileIn(t *testing.T, dir, name string) string {
	t.Helper()

	directory := filepath.Join(t.TempDir(), dir)

	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}

	path := filepath.Join(directory, name)

	if err := os.WriteFile(path, recording, 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}

	return path
}

// extractWith reads one watched file with the given parsers.
func extractWith(t *testing.T, parsers *Parsers, watch domain.Dirwatch, path string) (Extracted, bool, error) {
	t.Helper()

	return parsers.Extract(context.Background(), watch, path)
}

// id3v23Recording builds a recording carrying the ID3v2.3 tags sdr-trunk writes into the
// recordings it produces: the radio that spoke in the artist, the talkgroup in the title, and
// the run of labeled fields in the comment.
//
// The audio behind the tags is not a real recording; the parsers read the tags and the ingest
// path only asks whether there is more audio than a container header.
func id3v23Recording(artist, title, comment string) []byte {
	frames := id3v23TextFrame("TPE1", artist)
	frames += id3v23TextFrame("TIT2", title)

	// A comment frame is an encoding byte, a three-letter language, an empty description, and
	// the text — which is the shape sdr-trunk writes, and the shape the tag reader returns as
	// the comment.
	frames += id3v23Frame("COMM", "\x00eng\x00"+comment)

	header := []byte("ID3\x03\x00\x00")

	// The header's size is a synchsafe integer: seven bits per byte.
	header = append(header,
		byte(len(frames)>>21)&0x7f,
		byte(len(frames)>>14)&0x7f,
		byte(len(frames)>>7)&0x7f,
		byte(len(frames))&0x7f,
	)

	return append(append(header, frames...), recording...)
}

// id3v23TextFrame writes one ID3v2.3 text frame: an ISO-8859-1 encoding byte and the text.
func id3v23TextFrame(name, text string) string {
	return id3v23Frame(name, "\x00"+text)
}

// id3v23Frame writes one ID3v2.3 frame: its name, its size, no flags, and its contents.
func id3v23Frame(name, content string) string {
	size := make([]byte, 4)
	binary.BigEndian.PutUint32(size, uint32(len(content)))

	return name + string(size) + "\x00\x00" + content
}

// TestExtractDSDPlusReadsTheCallFromTheNameDsdPlusWrites is the whole of the dsd+ grammar at
// once: the moment from the directory and the name, the system from the type dsd+ reports, the
// talkgroup and the radio from the last two pieces of the name.
func TestExtractDSDPlusReadsTheCallFromTheNameDsdPlusWrites(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeDSDPlus

	path := watchedFileIn(t, "20261006", "120000_1514000_P25_0001.1234-5_1514000[Dispatch]_6789[Car 4].mp3")

	extracted, recognized, err := extractWith(t, newParsers(configuredSystems()), watch, path)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if !recognized {
		t.Fatal("a recording dsd+ wrote was recognized as nothing")
	}

	call := extracted.Call

	if call.AudioFilename != "120000_1514000_P25_0001.1234-5_1514000[Dispatch]_6789[Car 4].mp3" {
		t.Errorf("the audio filename is %q, want the name of the watched file", call.AudioFilename)
	}

	if call.AudioMime != "audio/mpeg" {
		t.Errorf("the audio media type is %q, want the one the extension names", call.AudioMime)
	}

	// The day is in the directory dsd+ recorded into and the clock is the first six digits of
	// the name, and the pair is read in the server's time zone.
	if want := time.Date(2026, 10, 6, 16, 0, 0, 0, time.UTC); !call.Timestamp.Equal(want) {
		t.Errorf("the call timestamp is %v, want %v", call.Timestamp, want)
	}

	// The system is the net and WACN dsd+ writes for a P25 system, read as a hexadecimal
	// number, which is what the legacy reads it as.
	if call.Meta.SystemRef != 0x1234 {
		t.Errorf("the system reference is %d, want %d: the WACN dsd+ named, read as hexadecimal", call.Meta.SystemRef, 0x1234)
	}

	if call.Meta.TalkgroupRef != 1514000 {
		t.Errorf("the talkgroup reference is %d, want the number the second-to-last piece of the name names", call.Meta.TalkgroupRef)
	}

	if call.Meta.TalkgroupLabel != "Dispatch" {
		t.Errorf("the talkgroup label is %q, want the name that follows it", call.Meta.TalkgroupLabel)
	}

	if len(call.Units) != 1 || call.Units[0].UnitRef != 6789 {
		t.Errorf("the call units are %#v, want the radio the last piece of the name names", call.Units)
	}

	if len(call.Meta.UnitRefs) != 1 || call.Meta.UnitRefs[0] != 6789 {
		t.Errorf("the unit references are %#v, want the radio paired with its name", call.Meta.UnitRefs)
	}

	if len(call.Meta.UnitLabels) != 1 || call.Meta.UnitLabels[0] != "Car 4" {
		t.Errorf("the unit labels are %#v, want the name that follows the reference", call.Meta.UnitLabels)
	}
}

// TestExtractDSDPlusReadsTheSystemForEachTypeItReports covers the shapes each system type
// writes its reference in, which is the part of the grammar dsd+ changed most often.
func TestExtractDSDPlusReadsTheSystemForEachTypeItReports(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		wantRef  uint
	}{
		{
			name:     "a conventional P25 base",
			filename: "120000_x_ConP(BS)_7-2 [Site]_1514000[Dispatch]_6789[Car 4].mp3",
			wantRef:  7,
		},
		{
			name:     "a DMR base",
			filename: "120000_x_DMR(BS)_12-34 [Site]_1514000[Dispatch]_6789[Car 4].mp3",
			wantRef:  12,
		},
		{
			name:     "a P25 base",
			filename: "120000_x_P25(BS)_3-4 [Site]_1514000[Dispatch]_6789[Car 4].mp3",
			wantRef:  3,
		},
		{
			name:     "an NEXEDGE cluster",
			filename: "120000_x_NEXEDGE48(CS)_A12-34_1514000[Dispatch]_6789[Car 4].mp3",
			wantRef:  12,
		},
		{
			// The fallback the legacy reaches when the piece after the type is not the shape
			// it expects: the RAN it writes somewhere later in the name.
			name:     "an NEXEDGE cluster naming its RAN later in the name",
			filename: "120000_x_NEXEDGE96(TB)_nothing_RAN123_1514000[Dispatch]_6789[Car 4].mp3",
			wantRef:  123,
		},
		{
			name:     "a P25 system",
			filename: "120000_x_P25_0001.abcd-5_1514000[Dispatch]_6789[Car 4].mp3",
			wantRef:  0xabcd,
		},
		{
			name:     "a type the grammar does not name a reference for",
			filename: "120000_x_M17_1514000[Dispatch]_6789[Car 4].mp3",
			wantRef:  0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			watch := domain.NewDirwatch()
			watch.Kind = domain.DirwatchTypeDSDPlus
			watch.SystemID = 1

			extracted, _, err := extractWith(t, newParsers(configuredSystems()), watch, watchedFileIn(t, "20261006", c.filename))
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}

			if extracted.Call.Meta.SystemRef != c.wantRef {
				t.Errorf("the system reference is %d, want %d", extracted.Call.Meta.SystemRef, c.wantRef)
			}
		})
	}
}

// TestExtractDSDPlusRefusesANameThatNamesNeitherATalkgroupNorASource is quirk Q37. The talkgroup
// and the radio are read from the last two pieces of the name, and the legacy reads them
// unguarded: a name with no underscore has no second-to-last piece, and the legacy takes the
// whole server down with an index-out-of-range panic when the file arrives while it is running.
// Here the name is refused and reported, which is the outcome the legacy already produces for
// the same file when it is found at startup.
func TestExtractDSDPlusRefusesANameThatNamesNeitherATalkgroupNorASource(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeDSDPlus

	_, recognized, err := extractWith(t, newParsers(configuredSystems()), watch, watchedFileIn(t, "20261006", "recording.mp3"))

	if recognized {
		t.Error("a name naming neither a talkgroup nor a radio was ingested, want it refused")
	}

	if err == nil || !strings.Contains(err.Error(), "recording") {
		t.Errorf("Extract = %v, want the reason the name was refused, naming the file", err)
	}
}

// TestExtractDSDPlusIgnoresALabelThatIsOnlyPunctuation is how dsd+ writes that it has no name
// for a talkgroup or a radio: the name slot is filled with separators, and a slot full of
// separators is not a name.
func TestExtractDSDPlusIgnoresALabelThatIsOnlyPunctuation(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeDSDPlus
	watch.SystemID = 1

	extracted, _, err := extractWith(t, newParsers(configuredSystems()), watch,
		watchedFileIn(t, "20261006", "120000_x_DMR(BS)_1-2_1514000[ ]_6789[.].mp3"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if extracted.Call.Meta.TalkgroupRef != 1514000 {
		t.Errorf("the talkgroup reference is %d, want the number before the empty name slot", extracted.Call.Meta.TalkgroupRef)
	}

	if extracted.Call.Meta.TalkgroupLabel != "" {
		t.Errorf("the talkgroup label is %q, want separators to name no talkgroup", extracted.Call.Meta.TalkgroupLabel)
	}

	if len(extracted.Call.Units) != 1 || extracted.Call.Units[0].UnitRef != 6789 {
		t.Fatalf("the call units are %#v, want the radio the name names", extracted.Call.Units)
	}

	if len(extracted.Call.Meta.UnitLabels) != 0 {
		t.Errorf("the unit labels are %#v, want separators to name no radio", extracted.Call.Meta.UnitLabels)
	}
}

// TestExtractDSDPlusDoesNotNameARadioBeyondItsReference covers the other way dsd+ says it has
// no name for a radio: it repeats the reference where the name goes.
func TestExtractDSDPlusDoesNotNameARadioBeyondItsReference(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeDSDPlus
	watch.SystemID = 1

	extracted, _, err := extractWith(t, newParsers(configuredSystems()), watch,
		watchedFileIn(t, "20261006", "120000_x_DMR(BS)_1-2_1514000[Dispatch]_6789[6789].mp3"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if len(extracted.Call.Units) != 1 || extracted.Call.Units[0].UnitRef != 6789 {
		t.Fatalf("the call units are %#v, want the radio the name names", extracted.Call.Units)
	}

	if len(extracted.Call.Meta.UnitLabels) != 0 || len(extracted.Call.Meta.UnitRefs) != 0 {
		t.Errorf("the unit metadata is %#v %#v, want a repeated reference to name no radio",
			extracted.Call.Meta.UnitRefs, extracted.Call.Meta.UnitLabels)
	}
}

// TestExtractDSDPlusWithoutADatedDirectoryRecordsTheCallAtTheYear1 is quirk Q42. The only place
// the dsd+ grammar reads a moment from is the directory, and the guard that is supposed to
// refuse a call with no moment asks whether the timestamp is the Unix epoch — which an unset
// time is not. So a dsd+ watch pointed at a directory that is not named for the day ingests
// every recording, dated to the year 1.
func TestExtractDSDPlusWithoutADatedDirectoryRecordsTheCallAtTheYear1(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeDSDPlus

	extracted, recognized, err := extractWith(t, newParsers(configuredSystems()), watch,
		watchedFileIn(t, "recordings", "120000_x_DMR(BS)_1-2_1514000[Dispatch]_6789[Car 4].mp3"))

	if err != nil {
		t.Fatalf("Extract: %v, want the call to be accepted, as the legacy accepts it", err)
	}

	if !recognized {
		t.Fatal("a dsd+ recording in an undated directory was refused, want it ingested")
	}

	if !extracted.Call.Timestamp.IsZero() {
		t.Errorf("the call timestamp is %v, want the unset moment the call was built with: the guard asks for the Unix epoch and an unset time is not that", extracted.Call.Timestamp)
	}

	if extracted.Call.Timestamp.UnixMilli() == 0 {
		t.Errorf("the call timestamp reports the Unix epoch, which is the one value the guard does refuse")
	}
}

// TestExtractDSDPlusReadsTheExtensionTheWatchConfigured covers the recording format dsd+ is
// told to write, which is mp3 unless the watch says otherwise.
func TestExtractDSDPlusReadsTheExtensionTheWatchConfigured(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeDSDPlus
	watch.Extension = "opus"
	watch.SystemID = 1

	_, recognized, err := extractWith(t, newParsers(configuredSystems()), watch,
		watchedFileIn(t, "20261006", "120000_x_DMR(BS)_1-2_1514000[Dispatch]_6789[Car 4].opus"))

	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if !recognized {
		t.Error("a recording with the configured extension was not read, want it read")
	}

	_, recognized, err = extractWith(t, newParsers(configuredSystems()), watch,
		watchedFileIn(t, "20261006", "120000_x_DMR(BS)_1-2_1514000[Dispatch]_6789[Car 4].mp3"))

	if recognized || err != nil {
		t.Errorf("Extract = %v %v for a recording of the wrong kind, want it left alone", recognized, err)
	}
}

// TestExtractSdrTrunkReadsTheTagsTheRecordingCarries is the sdr-trunk grammar: everything is in
// the audio tags, and the comment is one run of labeled fields.
func TestExtractSdrTrunkReadsTheTagsTheRecordingCarries(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeSdrTrunk

	path := watchedFile(t, "call.mp3")

	if err := os.WriteFile(path, id3v23Recording(
		"6789 Car 4",
		`1514000 "Dispatch 1"`,
		"Date:2026-10-06 12:00:00.000; Frequency:1514000; System:Grand Metro;",
	), 0o644); err != nil {
		t.Fatalf("writing the recording: %v", err)
	}

	extracted, recognized, err := extractWith(t, newParsers(configuredSystems()), watch, path)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if !recognized {
		t.Fatal("an sdr-trunk recording was recognized as nothing")
	}

	call := extracted.Call

	if call.AudioMime != "audio/mpeg" {
		t.Errorf("the audio media type is %q, want the one the extension names", call.AudioMime)
	}

	if len(call.Units) != 1 || call.Units[0].UnitRef != 6789 {
		t.Errorf("the call units are %#v, want the radio the artist names", call.Units)
	}

	if len(call.Meta.UnitLabels) != 1 || call.Meta.UnitLabels[0] != "Car 4" {
		t.Errorf("the unit labels are %#v, want the name the artist carries after the reference", call.Meta.UnitLabels)
	}

	if want := time.Date(2026, 10, 6, 16, 0, 0, 0, time.UTC); !call.Timestamp.Equal(want) {
		t.Errorf("the call timestamp is %v, want %v: the comment's time, read in the server's time zone", call.Timestamp, want)
	}

	if len(call.Frequencies) != 1 || call.Frequencies[0].Frequency != 1514000 {
		t.Errorf("the call carriers are %#v, want the carrier the comment names", call.Frequencies)
	}

	if call.System.ID != 1 {
		t.Errorf("the call belongs to system %#v, want the configured system the comment names by label", call.System)
	}

	if call.Meta.TalkgroupRef != 1514000 {
		t.Errorf("the talkgroup reference is %d, want the number the title names", call.Meta.TalkgroupRef)
	}

	if call.Meta.TalkgroupLabel != "Dispatch 1" || call.Meta.TalkgroupName != "Dispatch 1" {
		t.Errorf("the talkgroup names are %q %q, want the quoted name the title carries",
			call.Meta.TalkgroupLabel, call.Meta.TalkgroupName)
	}
}

// TestExtractSdrTrunkClaimsTheNextReferenceForASystemTheConfigurationDoesNotHave is the same
// auto-populate claim the default mask makes, from the other parser that names a system.
func TestExtractSdrTrunkClaimsTheNextReferenceForASystemTheConfigurationDoesNotHave(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeSdrTrunk

	path := watchedFile(t, "call.mp3")

	if err := os.WriteFile(path, id3v23Recording(
		"6789",
		"1514000",
		"System:Airport;",
	), 0o644); err != nil {
		t.Fatalf("writing the recording: %v", err)
	}

	extracted, _, err := extractWith(t, newParsers(configuredSystems()), watch, path)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if extracted.Call.Meta.SystemLabel != "Airport" {
		t.Errorf("the call claims the system label %q, want the name the comment carries", extracted.Call.Meta.SystemLabel)
	}

	if extracted.Call.Meta.SystemRef != 1 {
		t.Errorf("the call claims system reference %d, want the lowest reference the configuration is not using", extracted.Call.Meta.SystemRef)
	}
}

// TestExtractSdrTrunkReadsTheTalkgroupsTheCallWasPatchedTo covers the JSON array sdr-trunk
// writes into the title beside the talkgroup it recorded.
func TestExtractSdrTrunkReadsTheTalkgroupsTheCallWasPatchedTo(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeSdrTrunk

	path := watchedFile(t, "call.mp3")

	if err := os.WriteFile(path, id3v23Recording(
		"6789",
		`1514000 "Dispatch 1" [1514001,0,1514002]`,
		"System:Grand Metro;",
	), 0o644); err != nil {
		t.Fatalf("writing the recording: %v", err)
	}

	extracted, _, err := extractWith(t, newParsers(configuredSystems()), watch, path)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if len(extracted.Call.PatchTalkgroupRefs) != 2 ||
		extracted.Call.PatchTalkgroupRefs[0] != 1514001 ||
		extracted.Call.PatchTalkgroupRefs[1] != 1514002 {
		t.Errorf("the patched talkgroups are %#v, want both references the title names and a zero left out",
			extracted.Call.PatchTalkgroupRefs)
	}
}

// TestExtractSdrTrunkReportsARecordingWithoutTags is the watch pointed at recordings that are
// not sdr-trunk's: the tags are not there, and the parser says so rather than ingesting a call
// that names nothing.
func TestExtractSdrTrunkReportsARecordingWithoutTags(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeSdrTrunk

	_, recognized, err := extractWith(t, newParsers(configuredSystems()), watch, watchedFile(t, "call.mp3"))

	if recognized {
		t.Error("a recording with no tags was ingested, want it refused")
	}

	if err == nil {
		t.Fatal("a recording with no tags is a failure the watcher is told about")
	}
}

// TestExtractSdrTrunkIgnoresAFileThatIsNotAnMp3 pins that this parser has no configurable
// extension: sdr-trunk writes mp3, and that is what it reads.
func TestExtractSdrTrunkIgnoresAFileThatIsNotAnMp3(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeSdrTrunk
	watch.Extension = "wav"

	_, recognized, err := extractWith(t, newParsers(configuredSystems()), watch, watchedFile(t, "call.wav"))

	if recognized || err != nil {
		t.Errorf("Extract = %v %v for a recording of the wrong kind, want it left whatever the watch says", recognized, err)
	}
}

// TestExtractTrunkRecorderReadsThePairTheRecordingToolWrites is the trunk-recorder grammar: the
// sidecar describes the call, the recording sits beside it, and both are the call's files.
func TestExtractTrunkRecorderReadsThePairTheRecordingToolWrites(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeTrunkRecorder

	directory := t.TempDir()

	sidecar := filepath.Join(directory, "call.json")
	audio := filepath.Join(directory, "call.wav")

	if err := os.WriteFile(audio, recording, 0o644); err != nil {
		t.Fatalf("writing the recording: %v", err)
	}

	if err := os.WriteFile(sidecar, []byte(`{
		"short_name": "Grand Metro",
		"talkgroup": 1514000,
		"talkgroup_description": "Dispatch 1",
		"talkgroup_group": "Ground",
		"talkgroup_group_tag": "Dispatch",
		"talkgroup_tag": "Main",
		"timestamp": 1791288000000
	}`), 0o644); err != nil {
		t.Fatalf("writing the sidecar: %v", err)
	}

	extracted, recognized, err := extractWith(t, newParsers(configuredSystems()), watch, sidecar)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if !recognized {
		t.Fatal("a trunk-recorder sidecar was recognized as nothing")
	}

	call := extracted.Call

	if len(extracted.Files) != 2 || extracted.Files[0] != sidecar || extracted.Files[1] != audio {
		t.Errorf("the extracted call names %v as its files, want the sidecar and the recording beside it", extracted.Files)
	}

	// The recording is named for the audio file, not for the sidecar the watcher noticed: the
	// sidecar is metadata, and the call is the recording.
	if call.AudioFilename != "call.wav" {
		t.Errorf("the audio filename is %q, want the recording's name", call.AudioFilename)
	}

	if call.AudioMime != "audio/wav" {
		t.Errorf("the audio media type is %q, want the one the recording's extension names", call.AudioMime)
	}

	if call.Meta.SystemLabel != "Grand Metro" {
		t.Errorf("the call claims the system label %q, want the short name the sidecar carries", call.Meta.SystemLabel)
	}

	if call.Meta.TalkgroupRef != 1514000 {
		t.Errorf("the talkgroup reference is %d, want the one the sidecar names", call.Meta.TalkgroupRef)
	}

	if call.Meta.TalkgroupName != "Dispatch 1" {
		t.Errorf("the talkgroup name is %q, want the description the sidecar carries", call.Meta.TalkgroupName)
	}

	if len(call.Meta.TalkgroupGroup) != 1 || call.Meta.TalkgroupGroup[0] != "Ground" {
		t.Errorf("the talkgroup groups are %#v, want the group the sidecar names", call.Meta.TalkgroupGroup)
	}

	if call.Meta.TalkgroupTag != "Dispatch" {
		t.Errorf("the talkgroup tag is %q, want the group tag the sidecar carries", call.Meta.TalkgroupTag)
	}

	if call.Meta.TalkgroupLabel != "Main" {
		t.Errorf("the talkgroup label is %q, want the tag the sidecar carries", call.Meta.TalkgroupLabel)
	}

	if !call.Timestamp.Equal(time.UnixMilli(1791288000000)) {
		t.Errorf("the call timestamp is %v, want the millisecond the sidecar names", call.Timestamp)
	}
}

// TestExtractTrunkRecorderReadsTheCarriersAndRadiosTheSidecarNames covers the two lists a
// trunk-recorder sidecar carries, in the names trunk-recorder writes.
func TestExtractTrunkRecorderReadsTheCarriersAndRadiosTheSidecarNames(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeTrunkRecorder

	directory := t.TempDir()

	sidecar := filepath.Join(directory, "call.json")

	if err := os.WriteFile(filepath.Join(directory, "call.wav"), recording, 0o644); err != nil {
		t.Fatalf("writing the recording: %v", err)
	}

	if err := os.WriteFile(sidecar, []byte(`{
		"short_name": "Grand Metro",
		"talkgroup": 1514000,
		"freqList": [
			{"error_count": 1, "freq": 1514000, "pos": 1.5, "spike_count": 2},
			{"freq": 0, "pos": -1}
		],
		"srcList": [
			{"pos": 0.5, "src": 6789},
			{"pos": -1, "src": 0}
		],
		"patched_talkgroups": [1514001, 0],
		"timestamp": 1791288000000
	}`), 0o644); err != nil {
		t.Fatalf("writing the sidecar: %v", err)
	}

	extracted, _, err := extractWith(t, newParsers(configuredSystems()), watch, sidecar)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if len(extracted.Call.Frequencies) != 2 {
		t.Fatalf("the call carriers are %#v, want both entries the sidecar names", extracted.Call.Frequencies)
	}

	first := extracted.Call.Frequencies[0]

	if first.Errors != 1 || first.Frequency != 1514000 || first.Offset != 1.5 || first.Spikes != 2 {
		t.Errorf("the first carrier is %#v, want every measurement the sidecar names", first)
	}

	if second := extracted.Call.Frequencies[1]; second.Frequency != 0 || second.Offset != 0 {
		t.Errorf("the second carrier is %#v, want a carrier of zero and a negative position to be read as nothing", second)
	}

	if len(extracted.Call.Units) != 2 {
		t.Fatalf("the call units are %#v, want both entries the sidecar names", extracted.Call.Units)
	}

	if extracted.Call.Units[0].UnitRef != 6789 || extracted.Call.Units[0].Offset != 0.5 {
		t.Errorf("the first unit is %#v, want the radio and the moment it spoke", extracted.Call.Units[0])
	}

	if extracted.Call.Units[1].UnitRef != 0 || extracted.Call.Units[1].Offset != 0 {
		t.Errorf("the second unit is %#v, want a radio of zero and a negative position to be read as nothing", extracted.Call.Units[1])
	}

	if len(extracted.Call.PatchTalkgroupRefs) != 1 || extracted.Call.PatchTalkgroupRefs[0] != 1514001 {
		t.Errorf("the patched talkgroups are %#v, want the one the sidecar names and a zero left out", extracted.Call.PatchTalkgroupRefs)
	}
}

// TestExtractTrunkRecorderDropsASidecarWhoseRecordingIsNotThere is a legacy silence worth
// knowing about: trunk-recorder writes the sidecar and the recording as a pair, but not at the
// same instant, and a sidecar whose recording has not arrived yet is dropped without a word and
// never comes back.
func TestExtractTrunkRecorderDropsASidecarWhoseRecordingIsNotThere(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeTrunkRecorder

	sidecar := watchedFile(t, "call.json")

	_, recognized, err := extractWith(t, newParsers(configuredSystems()), watch, sidecar)

	if recognized {
		t.Error("a sidecar whose recording is not there was ingested, want it dropped")
	}

	if err != nil {
		t.Errorf("Extract = %v, want the legacy's silence: a pair that is not a pair is no failure", err)
	}
}

// TestExtractTrunkRecorderReportsASidecarItCannotRead is the other half: a sidecar that is
// there and cannot be read is reported, because that is a watch that will never ingest
// anything.
func TestExtractTrunkRecorderReportsASidecarItCannotRead(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeTrunkRecorder

	directory := t.TempDir()

	sidecar := filepath.Join(directory, "call.json")

	if err := os.WriteFile(filepath.Join(directory, "call.wav"), recording, 0o644); err != nil {
		t.Fatalf("writing the recording: %v", err)
	}

	if err := os.WriteFile(sidecar, []byte("not json"), 0o644); err != nil {
		t.Fatalf("writing the sidecar: %v", err)
	}

	_, recognized, err := extractWith(t, newParsers(configuredSystems()), watch, sidecar)

	if recognized {
		t.Error("a sidecar that cannot be read was ingested, want it refused")
	}

	if err == nil {
		t.Fatal("a sidecar that cannot be read is a failure the watcher is told about")
	}
}

// TestExtractTrunkRecorderRecordsTheCallAtTheMomentTheSidecarWasRead is quirk Q39: the sidecar's
// recorded instant is read and then replaced with the moment the file was read, by a debug line
// the legacy server has carried since the field was written. A deployment watching
// trunk-recorder's output records every call that does not also name `timestamp` as a call that
// happened just now.
func TestExtractTrunkRecorderRecordsTheCallAtTheMomentTheSidecarWasRead(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeTrunkRecorder

	directory := t.TempDir()

	sidecar := filepath.Join(directory, "call.json")

	if err := os.WriteFile(filepath.Join(directory, "call.wav"), recording, 0o644); err != nil {
		t.Fatalf("writing the recording: %v", err)
	}

	if err := os.WriteFile(sidecar, []byte(`{
		"short_name": "Grand Metro",
		"talkgroup": 1514000,
		"start_time": 1791288000
	}`), 0o644); err != nil {
		t.Fatalf("writing the sidecar: %v", err)
	}

	extracted, _, err := extractWith(t, newParsers(configuredSystems()), watch, sidecar)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if !extracted.Call.Timestamp.Equal(recordedAt) {
		t.Errorf("the call timestamp is %v, want %v: the recorded instant the sidecar names, replaced with the moment it was read",
			extracted.Call.Timestamp, recordedAt)
	}
}

// TestExtractTrunkRecorderKeepsTheInstantWhenTheSidecarNamesTimestamp pins the field that saves
// a trunk-recorder call from the debug line above: `timestamp` is read later in the same pass
// and wins.
func TestExtractTrunkRecorderKeepsTheInstantWhenTheSidecarNamesTimestamp(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeTrunkRecorder

	directory := t.TempDir()

	sidecar := filepath.Join(directory, "call.json")

	if err := os.WriteFile(filepath.Join(directory, "call.wav"), recording, 0o644); err != nil {
		t.Fatalf("writing the recording: %v", err)
	}

	if err := os.WriteFile(sidecar, []byte(`{
		"short_name": "Grand Metro",
		"talkgroup": 1514000,
		"start_time": 1791288000,
		"timestamp": 1791288000123
	}`), 0o644); err != nil {
		t.Fatalf("writing the sidecar: %v", err)
	}

	extracted, _, err := extractWith(t, newParsers(configuredSystems()), watch, sidecar)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if !extracted.Call.Timestamp.Equal(time.UnixMilli(1791288000123)) {
		t.Errorf("the call timestamp is %v, want the millisecond the sidecar names", extracted.Call.Timestamp)
	}
}

// TestExtractTrunkRecorderReadsTheRecordingExtensionTheWatchConfigured covers the pair's other
// half: the recording extension is the watch's, and without one it is wav.
func TestExtractTrunkRecorderReadsTheRecordingExtensionTheWatchConfigured(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeTrunkRecorder
	watch.Extension = "mp3"

	directory := t.TempDir()

	sidecar := filepath.Join(directory, "call.json")

	if err := os.WriteFile(filepath.Join(directory, "call.mp3"), recording, 0o644); err != nil {
		t.Fatalf("writing the recording: %v", err)
	}

	if err := os.WriteFile(sidecar, []byte(`{"short_name":"Grand Metro","talkgroup":1514000,"timestamp":1791288000000}`), 0o644); err != nil {
		t.Fatalf("writing the sidecar: %v", err)
	}

	extracted, _, err := extractWith(t, newParsers(configuredSystems()), watch, sidecar)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if extracted.Call.AudioFilename != "call.mp3" {
		t.Errorf("the audio filename is %q, want the recording the watch's extension names", extracted.Call.AudioFilename)
	}

	if extracted.Call.AudioMime != "audio/mpeg" {
		t.Errorf("the audio media type is %q, want the one that extension names", extracted.Call.AudioMime)
	}
}

// TestExtractTrunkRecorderIgnoresAFileThatIsNotASidecar is the watched directory holding the
// recordings as well as the sidecars: the parser reads the sidecars and leaves the recordings
// for the sidecars to find.
func TestExtractTrunkRecorderIgnoresAFileThatIsNotASidecar(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Kind = domain.DirwatchTypeTrunkRecorder

	_, recognized, err := extractWith(t, newParsers(configuredSystems()), watch, watchedFile(t, "call.wav"))

	if recognized || err != nil {
		t.Errorf("Extract = %v %v for a recording, want it left alone", recognized, err)
	}
}
