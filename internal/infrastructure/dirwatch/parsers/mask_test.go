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

package parsers

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
)

// recordedAt is the moment the parsers run at, in a time zone that is
// not UTC, so local-time and UTC-time parsers cannot pass by accident.
var recordedAt = time.Date(2026, 10, 6, 12, 0, 0, 0, time.FixedZone("TEST", -4*60*60))

// recording is the audio of a watched file: long enough to be a recording rather than a
// container header.
var recording = []byte(strings.Repeat("a recording long enough to be a recording", 4))

// newParsers builds the parsers for a server running with the given systems, at a fixed
// moment.
func newParsers(systems *fakes.Systems) *Parsers {
	return New(systems, fakes.NewClock(recordedAt))
}

// watchedFile puts a recording in a watched directory and reports the path the watcher would
// have been notified about.
func watchedFile(t *testing.T, name string) string {
	t.Helper()

	directory := t.TempDir()

	path := filepath.Join(directory, name)

	if err := os.WriteFile(path, recording, 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}

	return path
}

// configuredSystems is the configuration the two mask tokens that name things by label are
// resolved against.
func configuredSystems() *fakes.Systems {
	return &fakes.Systems{Stored: domain.Systems{
		{
			ID:        1,
			Label:     "Grand Metro",
			Sites:     domain.Sites{{ID: 5, Label: "North", SiteRef: 7}},
			SystemRef: 10001,
		},
		{
			ID:        2,
			Label:     "Harbour",
			SystemRef: 10002,
		},
	}}
}

// extract reads one watched file with a default watch.
func extract(t *testing.T, watch domain.Dirwatch, name string) (Extracted, bool, error) {
	t.Helper()

	return newParsers(configuredSystems()).Extract(context.Background(), watch, watchedFile(t, name))
}

// TestExtractDefaultReadsTheCallTheMaskNames: the filename says the
// system, talkgroup, radio, and moment, and the mask says where each sits.
func TestExtractDefaultReadsTheCallTheMaskNames(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Mask = "#SYS-#TG-#UNIT-#DATE-#TIME"

	extracted, recognized, err := extract(t, watch, "10001-1514000-6789-20261006-120000.wav")

	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if !recognized {
		t.Fatal("a recording with the extension a default watch reads was recognized as nothing")
	}

	call := extracted.Call

	if len(extracted.Files) != 1 || filepath.Base(extracted.Files[0]) != "10001-1514000-6789-20261006-120000.wav" {
		t.Errorf("the extracted call names %v as its files, want the recording the watcher noticed", extracted.Files)
	}

	if call.AudioFilename != "10001-1514000-6789-20261006-120000.wav" {
		t.Errorf("the audio filename is %q, want the name of the watched file", call.AudioFilename)
	}

	if call.AudioMime != "audio/wav" {
		t.Errorf("the audio media type is %q, want the one the extension names", call.AudioMime)
	}

	if string(call.Audio) != string(recording) {
		t.Errorf("the audio is %d bytes, want the recording byte for byte", len(call.Audio))
	}

	if call.Meta.SystemRef != 10001 {
		t.Errorf("the system reference is %d, want the one the mask named", call.Meta.SystemRef)
	}

	if call.Meta.TalkgroupRef != 1514000 {
		t.Errorf("the talkgroup reference is %d, want the one the mask named", call.Meta.TalkgroupRef)
	}

	if len(call.Units) != 1 || call.Units[0].UnitRef != 6789 {
		t.Errorf("the call units are %#v, want the radio the mask named", call.Units)
	}

	// The mask names a local time, and the server's own time zone is what makes it a moment.
	if want := time.Date(2026, 10, 6, 16, 0, 0, 0, time.UTC); !call.Timestamp.Equal(want) {
		t.Errorf("the call timestamp is %v, want %v: the local time the filename names, in the server's time zone", call.Timestamp, want)
	}
}

// TestExtractDefaultWithoutAMaskNamesNoSystemAndIsRefused: a default
// watch with no mask, system, or talkgroup names nothing in the filename, and
// refuses every recording.
func TestExtractDefaultWithoutAMaskNamesNoSystemAndIsRefused(t *testing.T) {
	_, recognized, err := extract(t, domain.NewDirwatch(), "recording.wav")

	if recognized {
		t.Error("a call naming no system was handed to the ingest path, want it refused")
	}

	if err == nil || !strings.Contains(err.Error(), "no talkgroup") {
		t.Errorf("Extract = %v, want the reason the call was refused: the watch named no system and no "+
			"talkgroup, and the refusal names the last thing the recording was missing", err)
	}
}

// TestExtractDefaultReadsTheWatchsOwnSystemAndTalkgroup: every recording
// belongs to one configured system and talkgroup, with no mask needed.
func TestExtractDefaultReadsTheWatchsOwnSystemAndTalkgroup(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.SystemID = 1
	watch.TalkgroupID = 2

	extracted, recognized, err := extract(t, watch, "recording.wav")

	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if !recognized {
		t.Fatal("a watch that names its system and talkgroup produced no call")
	}

	if extracted.Call.Meta.SystemID != 1 || extracted.Call.Meta.TalkgroupID != 2 {
		t.Errorf("the call claims system %d talkgroup %d, want the ones the watch names",
			extracted.Call.Meta.SystemID, extracted.Call.Meta.TalkgroupID)
	}

	// With no date tokens in the mask, the call is dated at the moment it was noticed.
	if !extracted.Call.Timestamp.Equal(recordedAt.UTC()) {
		t.Errorf("the call timestamp is %v, want the moment the recording was noticed", extracted.Call.Timestamp)
	}
}

// TestExtractDefaultAppliesTheWatchSystemAndTalkgroupToWhatTheMaskClaimed:
// both claims land, and the ingest path resolves the identifier claim first.
func TestExtractDefaultAppliesTheWatchSystemAndTalkgroupToWhatTheMaskClaimed(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Mask = "#SYS-#TG"
	watch.SystemID = 1
	watch.TalkgroupID = 2

	extracted, _, err := extract(t, watch, "10001-1514000.wav")

	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if extracted.Call.Meta.SystemID != 1 || extracted.Call.Meta.SystemRef != 10001 {
		t.Errorf("the call claims system id %d reference %d, want both claims, the identifier first for the resolver",
			extracted.Call.Meta.SystemID, extracted.Call.Meta.SystemRef)
	}

	if extracted.Call.Meta.TalkgroupID != 2 || extracted.Call.Meta.TalkgroupRef != 1514000 {
		t.Errorf("the call claims talkgroup id %d reference %d, want both claims",
			extracted.Call.Meta.TalkgroupID, extracted.Call.Meta.TalkgroupRef)
	}
}

// TestExtractDefaultIgnoresAFileThatIsNotTheWatchsExtension: the watch
// reads its own extension and says nothing about the rest.
func TestExtractDefaultIgnoresAFileThatIsNotTheWatchsExtension(t *testing.T) {
	_, recognized, err := extract(t, domain.NewDirwatch(), "recording.mp3")

	if recognized {
		t.Error("a recording of the wrong kind was ingested, want it left alone")
	}

	if err != nil {
		t.Errorf("Extract = %v, want a file this parser does not read to be no failure at all", err)
	}
}

// TestExtractDefaultReadsTheExtensionTheWatchConfigured, without its dot, ignoring case —
// which is what the administrator types into the field.
func TestExtractDefaultReadsTheExtensionTheWatchConfigured(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Extension = "m4a"
	watch.SystemID = 1
	watch.TalkgroupID = 2

	_, recognized, err := extract(t, watch, "recording.M4A")

	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if !recognized {
		t.Error("a recording with the configured extension was not read, want it read whatever its case")
	}
}

// TestExtractDefaultAddsTheFrequencyTheWatchNames is the watch-level carrier: every recording
// in the directory was heard on one carrier, and it is the call's carrier when the mask names
// no carrier of its own. A mask that does name one replaces it, which is the reading the pair
// of tests that follow takes.
func TestExtractDefaultAddsTheFrequencyTheWatchNames(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.SystemID = 1
	watch.TalkgroupID = 2
	watch.Frequency = 1514000

	extracted, _, err := extract(t, watch, "recording.wav")

	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if len(extracted.Call.Frequencies) != 1 || extracted.Call.Frequencies[0].Frequency != 1514000 {
		t.Errorf("the call carriers are %#v, want the carrier the watch names", extracted.Call.Frequencies)
	}
}

// TestExtractDefaultReadsTheTalkgroupFromEachShapeTheMaskNames covers the five ways a
// recording tool writes down which talkgroup a call was on, and the number each one names at
// the widths a recording tool writes it to: the carrier and the reference drawn from it are
// the number in the filename, whichever of the three units the mask names.
func TestExtractDefaultReadsTheTalkgroupFromEachShapeTheMaskNames(t *testing.T) {
	cases := []struct {
		name     string
		mask     string
		filename string
		wantRef  uint
		wantFreq uint
	}{
		{name: "a plain reference", mask: "#SYS-#TG", filename: "10001-1514000.wav", wantRef: 1514000},
		{name: "an Alpha-Net talkgroup", mask: "#SYS-#TGAFS", filename: "10001-12-345.wav", wantRef: 12<<7 | 34<<3 | 5},
		{name: "a carrier in hertz", mask: "#SYS-#TGHZ", filename: "10001-1514000.wav", wantRef: 1514, wantFreq: 1514000},
		{name: "a hertz carrier past twenty-four bits", mask: "#SYS-#TGHZ", filename: "10001-123456789.wav", wantRef: 123456, wantFreq: 123456789},
		{name: "a carrier in kilohertz", mask: "#SYS-#TGKHZ", filename: "10001-1514.000.wav", wantRef: 1514, wantFreq: 1514000},
		{name: "a kilohertz carrier to the thousandth", mask: "#SYS-#TGKHZ", filename: "10001-1514.001.wav", wantRef: 1514, wantFreq: 1514001},
		{name: "a kilohertz carrier below one megahertz", mask: "#SYS-#TGKHZ", filename: "10001-851.112.wav", wantRef: 851, wantFreq: 851112},
		{name: "a carrier in megahertz", mask: "#SYS-#TGMHZ", filename: "10001-151.4000.wav", wantRef: 151400, wantFreq: 151400000},
		{name: "a megahertz carrier to four places", mask: "#SYS-#TGMHZ", filename: "10001-851.1125.wav", wantRef: 851112, wantFreq: 851112500},
		{name: "a megahertz carrier below one gigahertz", mask: "#SYS-#TGMHZ", filename: "10001-999.999.wav", wantRef: 999999, wantFreq: 999999000},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			watch := domain.NewDirwatch()
			watch.Mask = c.mask

			extracted, _, err := extract(t, watch, c.filename)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}

			if extracted.Call.Meta.TalkgroupRef != c.wantRef {
				t.Errorf("the talkgroup reference is %d, want %d", extracted.Call.Meta.TalkgroupRef, c.wantRef)
			}

			if c.wantFreq == 0 {
				return
			}

			if len(extracted.Call.Frequencies) != 1 || extracted.Call.Frequencies[0].Frequency != c.wantFreq {
				t.Errorf("the call carriers are %#v, want the carrier the talkgroup was named by", extracted.Call.Frequencies)
			}
		})
	}
}

// TestExtractDefaultReadsTheAgencyFleetSubfleetFormTheDisplayWrites pairs each form the listener
// is shown for an agency-fleet-subfleet talkgroup with the reference a recording named by that same
// form must reach: a call filed under the name the listener was shown is the call the listener was
// shown. The two ends split the eleven bits the same way, so each pair is one row here and one row
// beside the display formatter.
func TestExtractDefaultReadsTheAgencyFleetSubfleetFormTheDisplayWrites(t *testing.T) {
	cases := []struct {
		form    string
		wantRef uint
		// The lowest form the eleven bits write names zero, and zero is no talkgroup: the
		// recording that carries it is refused rather than filed under a talkgroup that is not
		// there. The pair is on the table so the two ends say the same thing about it.
		wantsErr string
	}{
		{form: "00-000", wantsErr: "no talkgroup"},
		{form: "00-001", wantRef: 1},
		{form: "00-010", wantRef: 8},
		{form: "11-061", wantRef: 1457},
		{form: "15-157", wantRef: 2047},
	}

	for _, c := range cases {
		t.Run(c.form, func(t *testing.T) {
			watch := domain.NewDirwatch()
			watch.Mask = "#SYS-#TGAFS"

			extracted, _, err := extract(t, watch, "10001-"+c.form+".wav")

			if c.wantsErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantsErr) {
					t.Fatalf("the recording named by %s was read as %v, want the refusal naming %q",
						c.form, err, c.wantsErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("Extract: %v", err)
			}

			if extracted.Call.Meta.TalkgroupRef != c.wantRef {
				t.Errorf("the talkgroup reference read from %s is %d, want %d", c.form, extracted.Call.Meta.TalkgroupRef, c.wantRef)
			}
		})
	}
}

// TestExtractDefaultReadsTheCarrierFromEachUnitTheMaskNames covers the three units a recording
// tool writes a carrier in, each at a value whose exact number only a full-width read keeps: a
// carrier is filed as the number the recording tool wrote down, not as the nearest number a
// narrow read lands on.
func TestExtractDefaultReadsTheCarrierFromEachUnitTheMaskNames(t *testing.T) {
	cases := []struct {
		name     string
		mask     string
		filename string
		want     uint
	}{
		{name: "hertz", mask: "#SYS-#TG-#HZ", filename: "10001-1514000-1514000.wav", want: 1514000},
		{name: "hertz past twenty-four bits", mask: "#SYS-#TG-#HZ", filename: "10001-1514000-123456789.wav", want: 123456789},
		{name: "kilohertz", mask: "#SYS-#TG-#KHZ", filename: "10001-1514000-1514.000.wav", want: 1514000},
		{name: "kilohertz to the thousandth", mask: "#SYS-#TG-#KHZ", filename: "10001-1514000-151400.001.wav", want: 151400001},
		{name: "megahertz", mask: "#SYS-#TG-#MHZ", filename: "10001-1514000-151.4000.wav", want: 151400000},
		{name: "megahertz to four places", mask: "#SYS-#TG-#MHZ", filename: "10001-1514000-851.1125.wav", want: 851112500},
		{name: "megahertz below one gigahertz", mask: "#SYS-#TG-#MHZ", filename: "10001-1514000-999.999.wav", want: 999999000},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			watch := domain.NewDirwatch()
			watch.Mask = c.mask

			extracted, _, err := extract(t, watch, c.filename)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}

			if len(extracted.Call.Frequencies) != 1 || extracted.Call.Frequencies[0].Frequency != c.want {
				t.Errorf("the call carriers are %#v, want one carrier at %d", extracted.Call.Frequencies, c.want)
			}
		})
	}
}

// TestExtractDefaultReadsTheWholeHertzPieceAtATalkgroupCarrierToken covers what the
// whole-hertz #TGHZ pattern captures, including the two names where a decimal point moves
// where the mask anchors. A name the pattern cannot read whole is read at the whole-hertz
// piece 6.6.3 read, whatever it looks like.
func TestExtractDefaultReadsTheWholeHertzPieceAtATalkgroupCarrierToken(t *testing.T) {
	cases := []struct {
		name     string
		mask     string
		filename string
		wantFreq uint
		wantRef  uint
		wantSys  uint
	}{
		{
			name:     "a date and time mask over a decimal carrier",
			mask:     "laurentides_#DATE_#TIME_#TGHZ",
			filename: "laurentides_20201231_085343_151.4000.wav",
			wantFreq: 151,
		},
		{
			name:     "the mask re-anchors on the digit after the decimal point",
			mask:     "#TGHZ_#SYS",
			filename: "151.4_10001.wav",
			wantFreq: 4,
			wantSys:  10001,
		},
		{
			name:     "the decimal piece is dropped",
			mask:     "#SYS-#TGHZ",
			filename: "10001-151.4.wav",
			wantFreq: 151,
			wantSys:  10001,
		},
		{
			name:     "a whole-hertz carrier with a fraction on it",
			mask:     "#SYS-#TGHZ",
			filename: "10001-151399993.5.wav",
			wantFreq: 151399993,
			wantRef:  151399,
			wantSys:  10001,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			watch := domain.NewDirwatch()
			watch.Mask = c.mask

			// The watch names its own system and talkgroup: a carrier below one kilohertz
			// derives no talkgroup reference, and this is a reading of what the mask read.
			watch.SystemID = 1
			watch.TalkgroupID = 2

			extracted, _, err := extract(t, watch, c.filename)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}

			if len(extracted.Call.Frequencies) != 1 || extracted.Call.Frequencies[0].Frequency != c.wantFreq {
				t.Errorf("the call carriers are %#v, want one carrier at %d", extracted.Call.Frequencies, c.wantFreq)
			}

			if c.wantRef != 0 && extracted.Call.Meta.TalkgroupRef != c.wantRef {
				t.Errorf("the talkgroup reference is %d, want %d", extracted.Call.Meta.TalkgroupRef, c.wantRef)
			}

			if c.wantSys != 0 && extracted.Call.Meta.SystemRef != c.wantSys {
				t.Errorf("the system reference is %d, want %d: the token after the carrier reads the number the "+
					"whole-hertz pattern left it", extracted.Call.Meta.SystemRef, c.wantSys)
			}
		})
	}
}

// TestExtractDefaultReadsATalkgroupReferenceForBothTalkgroupTokensOnOneName: the #TG token
// names the talkgroup and the carrier token only captures, in both builds. The anchor a
// whole-hertz pattern moves is on the record as what the call shows.
func TestExtractDefaultReadsATalkgroupReferenceForBothTalkgroupTokensOnOneName(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Mask = "#TGHZ-#TG"
	watch.SystemID = 1

	extracted, _, err := extract(t, watch, "151.4-2000.wav")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if extracted.Call.Meta.TalkgroupRef != 2000 {
		t.Errorf("the talkgroup reference is %d, want the one the #TG token named", extracted.Call.Meta.TalkgroupRef)
	}

	if len(extracted.Call.Frequencies) != 0 {
		t.Errorf("the call carriers are %#v, want none: a token that only captures a carrier names no carrier "+
			"when the mask also names the talkgroup itself", extracted.Call.Frequencies)
	}
}

// TestExtractDefaultReadsAPublishedShapeTalkgroupCarrierTheSameInBothBuilds is the shape
// the administrator guide publishes: a whole-hertz name needs no decimal point, and the
// carrier and the reference it derives are exact in both builds.
func TestExtractDefaultReadsAPublishedShapeTalkgroupCarrierTheSameInBothBuilds(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Mask = "#SYS-#TGHZ"

	extracted, _, err := extract(t, watch, "10001-119100000.wav")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if len(extracted.Call.Frequencies) != 1 || extracted.Call.Frequencies[0].Frequency != 119100000 {
		t.Errorf("the call carriers are %#v, want one carrier at 119100000", extracted.Call.Frequencies)
	}

	if extracted.Call.Meta.TalkgroupRef != 119100 {
		t.Errorf("the talkgroup reference is %d, want 119100", extracted.Call.Meta.TalkgroupRef)
	}
}

// TestExtractDefaultFilesOnlyTheCarrierTheMaskNamesWhenItNamesOne: the watch's own carrier
// says where in the directory the recordings came from, and a mask that names a carrier
// replaces it rather than adding a second one. Both carriers are whole hertz, so the only
// divergence these two cases read is how many carriers the call files.
func TestExtractDefaultFilesOnlyTheCarrierTheMaskNamesWhenItNamesOne(t *testing.T) {
	cases := []struct {
		name     string
		mask     string
		filename string
	}{
		{name: "a unit the mask names on its own", mask: "#SYS-#TG-#HZ", filename: "10001-1514000-7890000.wav"},
		{name: "a talkgroup named by its carrier", mask: "#SYS-#TGHZ", filename: "10001-7890000.wav"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			watch := domain.NewDirwatch()
			watch.Mask = c.mask
			watch.Frequency = 1514000

			extracted, _, err := extract(t, watch, c.filename)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}

			if len(extracted.Call.Frequencies) != 1 || extracted.Call.Frequencies[0].Frequency != 7890000 {
				t.Errorf("the call carriers are %#v, want only the carrier the mask names: the recording was "+
					"heard on one carrier, and the filename says which", extracted.Call.Frequencies)
			}
		})
	}
}

// TestExtractDefaultKeepsTheWatchCarrierWhenTheMaskNamesNoCarrier is the other half of that
// reading: a mask that names the system and talkgroup and no carrier leaves the directory's
// own carrier on the call.
func TestExtractDefaultKeepsTheWatchCarrierWhenTheMaskNamesNoCarrier(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Mask = "#SYS-#TG"
	watch.Frequency = 1514000

	extracted, _, err := extract(t, watch, "10001-1514000.wav")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if len(extracted.Call.Frequencies) != 1 || extracted.Call.Frequencies[0].Frequency != 1514000 {
		t.Errorf("the call carriers are %#v, want the carrier the watch names", extracted.Call.Frequencies)
	}
}

// TestExtractDefaultRefusesAFileWhoseMaskNamesOnlyATalkgroupLabel: a mask that names the
// talkgroup by its label alone claims no talkgroup the server can file the call on, and the
// recording is left where it is.
func TestExtractDefaultRefusesAFileWhoseMaskNamesOnlyATalkgroupLabel(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Mask = "#SYSLBL-#TGLBL"
	watch.SystemID = 1

	_, recognized, err := extract(t, watch, "Grand Metro-Dispatch 1.wav")

	if recognized {
		t.Error("a recording that named only a talkgroup label was handed to the ingest path, want it refused")
	}

	if err == nil || !strings.Contains(err.Error(), "no talkgroup") {
		t.Errorf("Extract = %v, want the reason the call was refused: the mask named a talkgroup label and no "+
			"talkgroup number", err)
	}
}

// TestExtractDefaultReadsTheInstantFromTheDateAndTimeTokens pins the two time tokens, which
// differ by whether the filename's time is the server's local time or already UTC.
func TestExtractDefaultReadsTheInstantFromTheDateAndTimeTokens(t *testing.T) {
	local := domain.NewDirwatch()
	local.Mask = "#SYS-#TG-#DATE-#TIME"

	extracted, _, err := extract(t, local, "10001-1514000-20261006-120000.wav")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if want := time.Date(2026, 10, 6, 16, 0, 0, 0, time.UTC); !extracted.Call.Timestamp.Equal(want) {
		t.Errorf("the call timestamp is %v, want %v: a local time read in the server's time zone", extracted.Call.Timestamp, want)
	}

	utc := domain.NewDirwatch()
	utc.Mask = "#SYS-#TG-#DATE-#ZTIME"

	extracted, _, err = extract(t, utc, "10001-1514000-20261006-120000.wav")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if want := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC); !extracted.Call.Timestamp.Equal(want) {
		t.Errorf("the call timestamp is %v, want %v: a UTC time read as UTC", extracted.Call.Timestamp, want)
	}
}

// TestExtractDefaultReadsSeparatorsInTheDateAndTimeTheMaskNames: dashes
// and colons match where the token patterns are runs of digits.
func TestExtractDefaultReadsSeparatorsInTheDateAndTimeTheMaskNames(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Mask = "#SYS-#TG-#DATE-#TIME"

	extracted, _, err := extract(t, watch, "10001-1514000-2026_10_06-12:00:00.wav")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if want := time.Date(2026, 10, 6, 16, 0, 0, 0, time.UTC); !extracted.Call.Timestamp.Equal(want) {
		t.Errorf("the call timestamp is %v, want %v", extracted.Call.Timestamp, want)
	}
}

// TestExtractDefaultReadsABareDateAsAUnixSecondCount: a mask naming
// #DATE and neither time token reads the date digits as a moment in 1970,
// the legacy behavior and the one config dating calls four decades early.
func TestExtractDefaultReadsABareDateAsAUnixSecondCount(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Mask = "#SYS-#TG-#DATE"

	extracted, _, err := extract(t, watch, "10001-1514000-20261006.wav")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if want := time.Unix(20261006, 0).UTC(); !extracted.Call.Timestamp.Equal(want) {
		t.Errorf("the call timestamp is %v, want %v: the digits of the date read as a second count",
			extracted.Call.Timestamp, want)
	}
}

// TestExtractDefaultNamesTheSystemItsLabelReadsAndLeavesTheSiteToTheIngestPath: the label
// resolves, which is what the parser has always done, and the site the mask names is no longer
// looked up there. A site belongs to the system a call lands on, and that system is settled
// after the parser has run.
func TestExtractDefaultNamesTheSystemItsLabelReadsAndLeavesTheSiteToTheIngestPath(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Mask = "#SYSLBL-#SITE"
	watch.TalkgroupID = 2

	extracted, _, err := extract(t, watch, "Grand Metro-7.wav")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if extracted.Call.System.ID != 1 || extracted.Call.System.Label != "Grand Metro" {
		t.Errorf("the call belongs to system %#v, want the configured system the mask named by label", extracted.Call.System)
	}

	if extracted.Call.SiteRef != 0 {
		t.Errorf("the call site reference is %d, want the parser to file no site on the call: the site is looked up "+
			"under the system the call lands on, which is settled after the parser has run", extracted.Call.SiteRef)
	}

	if extracted.Call.Meta.SiteRef != 7 || extracted.Call.Meta.SiteID != 0 || extracted.Call.Meta.SiteLabel != "" {
		t.Errorf("the call claims site %d/%d/%q, want the reference the mask named claimed for the ingest path to "+
			"resolve under the system the call lands on",
			extracted.Call.Meta.SiteID, extracted.Call.Meta.SiteRef, extracted.Call.Meta.SiteLabel)
	}
}

// TestExtractDefaultReadsTheSiteTokensTheSameWayWhicheverWayTheMaskNamesItsSystem is the half
// of the site tokens that has never worked: a mask naming its system by reference dropped both
// tokens, because the system it named resolves only later, in the ingest path. Every way a mask
// names its system settles that system in the ingest path, so the parser reads the site tokens the
// same way for all of them: it files no site on the call, and it claims for the ingest path exactly
// what the token named, the way a system named by reference is claimed and resolved later.
func TestExtractDefaultReadsTheSiteTokensTheSameWayWhicheverWayTheMaskNamesItsSystem(t *testing.T) {
	cases := []struct {
		name        string
		mask        string
		filename    string
		wantSiteRef uint
		wantSiteLbl string
	}{
		{name: "a system named by reference", mask: "#SYS-#SITE", filename: "10001-7.wav", wantSiteRef: 7},
		{name: "a system named by label", mask: "#SYSLBL-#SITE", filename: "Grand Metro-7.wav", wantSiteRef: 7},
		{name: "a site named by label under a system named by label", mask: "#SYSLBL-#SITELBL", filename: "Grand Metro-North.wav", wantSiteLbl: "North"},
		{name: "a site named by label under a system the configuration does not have", mask: "#SYSLBL-#SITELBL", filename: "Airport-North.wav", wantSiteLbl: "North"},
	}

	for _, testCase := range cases {
		watch := domain.NewDirwatch()
		watch.Mask = testCase.mask
		watch.TalkgroupID = 2

		extracted, _, err := extract(t, watch, testCase.filename)
		if err != nil {
			t.Fatalf("%s: Extract: %v", testCase.name, err)
		}

		if extracted.Call.SiteRef != 0 {
			t.Errorf("%s: the call site reference is %d, want the parser to file no site on the call, the way it "+
				"already does for a system it names by reference: a site is looked up under the system the call lands "+
				"on, and that lookup belongs after the parser", testCase.name, extracted.Call.SiteRef)
		}

		meta := extracted.Call.Meta

		if meta.SiteRef != testCase.wantSiteRef {
			t.Errorf("%s: the call claims site reference %d, want %d: the reference the mask named, claimed for the "+
				"ingest path to resolve under the system the call lands on", testCase.name, meta.SiteRef, testCase.wantSiteRef)
		}

		if meta.SiteLabel != testCase.wantSiteLbl {
			t.Errorf("%s: the call claims site label %q, want %q: the name the mask named, claimed for the ingest "+
				"path to resolve under the system the call lands on", testCase.name, meta.SiteLabel, testCase.wantSiteLbl)
		}

		if meta.SiteID != 0 {
			t.Errorf("%s: the call claims site id %d, want no database identifier: a filename names a reference or "+
				"a name, never a row", testCase.name, meta.SiteID)
		}
	}
}

// TestAWatchsSiteSelectClaimsASiteForItsDirectoryUnlessItsMaskNamesOne: the site an
// administrator selected for a watch is a claim for every recording in that directory, which is
// what the select has always looked like it should do. A mask that names a site of its own is the
// exception: the recording's own name is more specific than the directory's setting, the way a
// recording's own talkgroup label is more specific than the watch's, so the mask's claim stands
// alone. A watch with no site selected claims nothing, which is every watch that came from 6.6.3.
func TestAWatchsSiteSelectClaimsASiteForItsDirectoryUnlessItsMaskNamesOne(t *testing.T) {
	cases := []struct {
		name        string
		mask        string
		filename    string
		siteID      uint64
		wantSiteID  uint64
		wantSiteRef uint
		wantSiteLbl string
	}{
		{name: "the select, with no site token in the mask", mask: "#TG", filename: "1514000.wav", siteID: 5, wantSiteID: 5},
		{name: "a reference the mask names beside the select", mask: "#TG-#SITE", filename: "1514000-7.wav", siteID: 5, wantSiteRef: 7},
		{name: "a name the mask names beside the select", mask: "#SITELBL", filename: "North.wav", siteID: 5, wantSiteLbl: "North"},
		{name: "no select and no site token", mask: "#TG", filename: "1514000.wav", wantSiteID: 0},
	}

	for _, testCase := range cases {
		watch := domain.NewDirwatch()
		watch.Mask = testCase.mask
		watch.SystemID = 1
		watch.TalkgroupID = 2
		watch.SiteID = testCase.siteID

		extracted, _, err := extract(t, watch, testCase.filename)
		if err != nil {
			t.Fatalf("%s: Extract: %v", testCase.name, err)
		}

		meta := extracted.Call.Meta

		if meta.SiteID != testCase.wantSiteID || meta.SiteRef != testCase.wantSiteRef || meta.SiteLabel != testCase.wantSiteLbl {
			t.Errorf("%s: the call claims site %d/%d/%q, want %d/%d/%q: the site the recording names, or the watch's "+
				"select when the recording names none",
				testCase.name, meta.SiteID, meta.SiteRef, meta.SiteLabel,
				testCase.wantSiteID, testCase.wantSiteRef, testCase.wantSiteLbl)
		}

		if extracted.Call.SiteRef != 0 {
			t.Errorf("%s: the call site reference is %d, want the parser to file no site on the call: the claim is "+
				"resolved under the system the call lands on, in the ingest path", testCase.name, extracted.Call.SiteRef)
		}
	}
}

// TestExtractDefaultClaimsTheNextFreeReferenceForASystemTheConfigurationDoesNotHave:
// the claim auto-populate creates a system from: the read name and a free reference.
func TestExtractDefaultClaimsTheNextFreeReferenceForASystemTheConfigurationDoesNotHave(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Mask = "#SYSLBL"
	watch.TalkgroupID = 2

	extracted, _, err := extract(t, watch, "Airport.wav")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if extracted.Call.Meta.SystemLabel != "Airport" {
		t.Errorf("the call claims the system label %q, want the name the mask read", extracted.Call.Meta.SystemLabel)
	}

	// the reference is the lowest one nothing is using, computed from the
	// configuration as the legacy computes it: for systems 10001 and 10002, it is 1.
	if extracted.Call.Meta.SystemRef != 1 {
		t.Errorf("the call claims system reference %d, want the lowest reference the configuration is not using", extracted.Call.Meta.SystemRef)
	}
}

// TestExtractDefaultReadsTheUnitLabelTokenWhole: the token table applies
// longest-first, so a mask naming only #UNITLBL has the label replaced
// whole; it used to have #UNIT replaced inside it, leaving a literal LBL.
func TestExtractDefaultReadsTheUnitLabelTokenWhole(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Mask = "#SYS-#TG-#UNITLBL"

	extracted, _, err := extract(t, watch, "10001-1514000-Engine 7.wav")
	if err != nil {
		t.Fatalf("Extract: %v, want the label token replaced whole and the mask to match", err)
	}

	if extracted.Call.Meta.SystemRef != 10001 || extracted.Call.Meta.TalkgroupRef != 1514000 {
		t.Errorf("the call claims system %d talkgroup %d, want the tokens around the label read as the mask names them",
			extracted.Call.Meta.SystemRef, extracted.Call.Meta.TalkgroupRef)
	}

	// the pairing rule is the legacy's: a name lands on the unit the #UNIT
	// token added, and this mask names no unit, so the label has nothing to pair with.
	if len(extracted.Call.Units) != 0 {
		t.Errorf("the call units are %#v, want none: the mask names no unit token", extracted.Call.Units)
	}

	if len(extracted.Call.Meta.UnitLabels) != 0 {
		t.Errorf("the unit labels are %#v, want none: there is no unit for the name to pair with", extracted.Call.Meta.UnitLabels)
	}
}

// TestExtractDefaultPairsTheUnitLabelWithTheUnitTheMaskNamed: one unit
// and one name however many the filename holds, and the pairing works when
// the mask names the unit before the label.
func TestExtractDefaultPairsTheUnitLabelWithTheUnitTheMaskNamed(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Mask = "#SYS-#TG-#UNIT-#UNITLBL"

	extracted, _, err := extract(t, watch, "10001-1514000-6789-Car 4.wav")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if len(extracted.Call.Units) != 1 || extracted.Call.Units[0].UnitRef != 6789 {
		t.Fatalf("the call units are %#v, want the radio the mask named", extracted.Call.Units)
	}

	if len(extracted.Call.Meta.UnitRefs) != 1 || extracted.Call.Meta.UnitRefs[0] != 6789 {
		t.Errorf("the unit references are %#v, want the label paired with the reference the mask named", extracted.Call.Meta.UnitRefs)
	}

	if len(extracted.Call.Meta.UnitLabels) != 1 || extracted.Call.Meta.UnitLabels[0] != "Car 4" {
		t.Errorf("the unit labels are %#v, want the name the mask named", extracted.Call.Meta.UnitLabels)
	}

	paired := domain.NewDirwatch()
	paired.Mask = "#SYS-#TG-#UNIT"

	extracted, _, err = extract(t, paired, "10001-1514000-6789.wav")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if len(extracted.Call.Meta.UnitLabels) != 0 || len(extracted.Call.Meta.UnitRefs) != 0 {
		t.Errorf("the unit metadata is %#v %#v, want none for a mask that names no label",
			extracted.Call.Meta.UnitLabels, extracted.Call.Meta.UnitRefs)
	}
}

// TestExtractDefaultReadsTheGroupAndTagAndIgnoresTheDashRecordingToolsWrite covers the two
// name tokens a recording tool fills with a dash when it has nothing to say.
func TestExtractDefaultReadsTheGroupAndTagAndIgnoresTheDashRecordingToolsWrite(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Mask = "#SYS-#TG-#GROUP-#TAG-#TGLBL"

	extracted, _, err := extract(t, watch, "10001-1514000-Ground-Dispatch-Dispatch 1.wav")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if len(extracted.Call.Meta.TalkgroupGroup) != 1 || extracted.Call.Meta.TalkgroupGroup[0] != "Ground" {
		t.Errorf("the talkgroup groups are %#v, want the group the mask named", extracted.Call.Meta.TalkgroupGroup)
	}

	if extracted.Call.Meta.TalkgroupTag != "Dispatch" {
		t.Errorf("the talkgroup tag is %q, want the tag the mask named", extracted.Call.Meta.TalkgroupTag)
	}

	if extracted.Call.Meta.TalkgroupLabel != "Dispatch 1" {
		t.Errorf("the talkgroup label is %q, want the label the mask named", extracted.Call.Meta.TalkgroupLabel)
	}

	// A recording tool with nothing to say about the tag writes a dash, which shows up in the
	// filename as two separators in a row.
	dashes := domain.NewDirwatch()
	dashes.Mask = "#SYS-#TG-#GROUP-#TAG"

	extracted, _, err = extract(t, dashes, "10001-1514000-Ground--.wav")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if len(extracted.Call.Meta.TalkgroupGroup) != 1 || extracted.Call.Meta.TalkgroupGroup[0] != "Ground" {
		t.Errorf("the talkgroup groups are %#v, want the group before the dash", extracted.Call.Meta.TalkgroupGroup)
	}

	if extracted.Call.Meta.TalkgroupTag != "" {
		t.Errorf("the talkgroup tag is %q, want a dash to name no tag", extracted.Call.Meta.TalkgroupTag)
	}
}

// TestExtractDefaultReadsNothingFromAFilenameTheMaskDoesNotMatch: the
// watch looks configured, the names are not what the mask describes, and all
// recordings are refused.
func TestExtractDefaultReadsNothingFromAFilenameTheMaskDoesNotMatch(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Mask = "#SYS-#TG"

	_, _, err := extract(t, watch, "recording.wav")

	if err == nil || !strings.Contains(err.Error(), "no talkgroup") {
		t.Errorf("Extract = %v, want a filename the mask does not match to name no system and no talkgroup, "+
			"refused for the last thing it was missing", err)
	}
}

// TestExtractDefaultReportsARecordingItCannotRead: the recording went
// away between the notification and the read, which the watcher checks for.
func TestExtractDefaultReportsARecordingItCannotRead(t *testing.T) {
	path := watchedFile(t, "recording.wav")

	if err := os.Remove(path); err != nil {
		t.Fatalf("removing the recording: %v", err)
	}

	_, recognized, err := newParsers(configuredSystems()).Extract(context.Background(), domain.NewDirwatch(), path)

	if recognized {
		t.Error("a recording that is not there was ingested, want it refused")
	}

	if err == nil {
		t.Fatal("a recording that cannot be read is a failure the watcher is told about")
	}
}

// TestExtractDefaultReportsAConfigurationItCannotRead keeps a database that will not answer
// from looking like a mask that named no system.
func TestExtractDefaultReportsAConfigurationItCannotRead(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Mask = "#SYSLBL"

	path := watchedFile(t, "Grand Metro.wav")

	_, _, err := New(&fakes.Systems{Err: os.ErrPermission}, fakes.NewClock(recordedAt)).
		Extract(context.Background(), watch, path)

	if err == nil {
		t.Fatal("a configuration the parser cannot read is a failure the watcher is told about")
	}

	if !strings.Contains(err.Error(), "permission") {
		t.Errorf("Extract = %v, want it to carry the reason the read gave", err)
	}
}

// maskShape is one filename the mask token table has to read, and the values the token it stands
// for names in it. The table is read a token at a time: each shape names the one token it stands
// for beside the system and talkgroup every recording needs, so a token whose pattern stops
// working, or stops being substituted, shows up as the value it stops handing over.
type maskShape struct {
	token     string
	mask      string
	filename  string
	systemID  uint64
	systemRef uint
	siteRef   uint
	siteLabel string
	tgRef     uint
	tgLabel   string
	tgTag     string
	tgGroup   string
	unitRef   uint
	unitLabel string
	hertz     uint
	timestamp time.Time
}

// wholeTableShapes are the shapes the mask token table has to read, one per token the table
// names, each written the way a recording tool writes it.
var wholeTableShapes = []maskShape{
	{token: "#DATE", mask: "#SYS-#TG-#DATE", filename: "10001-1514000-20261006.wav", systemRef: 10001, tgRef: 1514000, timestamp: time.Unix(20261006, 0).UTC()},
	{token: "#GROUP", mask: "#SYS-#TG-#GROUP", filename: "10001-1514000-1-3.wav", systemRef: 10001, tgRef: 1514000, tgGroup: "1-3"},
	{token: "#HZ", mask: "#SYS-#TG-#HZ", filename: "10001-1514000-1514000.wav", systemRef: 10001, tgRef: 1514000, hertz: 1514000},
	{token: "#KHZ", mask: "#SYS-#TG-#KHZ", filename: "10001-1514000-1514.000.wav", systemRef: 10001, tgRef: 1514000, hertz: 1514000},
	{token: "#MHZ", mask: "#SYS-#TG-#MHZ", filename: "10001-1514000-151.4000.wav", systemRef: 10001, tgRef: 1514000, hertz: 151400000},
	{token: "#SITE", mask: "#SYS-#TG-#SITE", filename: "10001-1514000-7.wav", systemRef: 10001, tgRef: 1514000, siteRef: 7},
	{token: "#SITELBL", mask: "#SYS-#TG-#SITELBL", filename: "10001-1514000-North.wav", systemRef: 10001, tgRef: 1514000, siteLabel: "North"},
	{token: "#SYS", mask: "#SYS-#TG", filename: "10001-1514000.wav", systemRef: 10001, tgRef: 1514000},
	{token: "#SYSLBL", mask: "#SYSLBL-#TG", filename: "Grand Metro-1514000.wav", systemID: 1, tgRef: 1514000},
	{token: "#TAG", mask: "#SYS-#TG-#TAG", filename: "10001-1514000-Z3.wav", systemRef: 10001, tgRef: 1514000, tgTag: "Z3"},
	{token: "#TG", mask: "#SYS-#TG", filename: "10001-1514000.wav", systemRef: 10001, tgRef: 1514000},
	{token: "#TGAFS", mask: "#SYS-#TGAFS", filename: "10001-12-345.wav", systemRef: 10001, tgRef: 12<<7 | 34<<3 | 5},
	{token: "#TGHZ", mask: "#SYS-#TGHZ", filename: "10001-1514000.wav", systemRef: 10001, tgRef: 1514, hertz: 1514000},
	{token: "#TGKHZ", mask: "#SYS-#TGKHZ", filename: "10001-1514.000.wav", systemRef: 10001, tgRef: 1514, hertz: 1514000},
	{token: "#TGLBL", mask: "#SYS-#TG-#TGLBL", filename: "10001-1514000-Dispatch.wav", systemRef: 10001, tgRef: 1514000, tgLabel: "Dispatch"},
	{token: "#TGMHZ", mask: "#SYS-#TGMHZ", filename: "10001-151.4000.wav", systemRef: 10001, tgRef: 151400, hertz: 151400000},
	{token: "#TIME", mask: "#SYS-#TG-#DATE-#TIME", filename: "10001-1514000-20261006-120000.wav", systemRef: 10001, tgRef: 1514000, timestamp: time.Date(2026, 10, 6, 16, 0, 0, 0, time.UTC)},
	{token: "#UNIT", mask: "#SYS-#TG-#UNIT", filename: "10001-1514000-4500.wav", systemRef: 10001, tgRef: 1514000, unitRef: 4500},
	{token: "#UNITLBL", mask: "#SYS-#TG-#UNIT-#UNITLBL", filename: "10001-1514000-4500-Dispatch.wav", systemRef: 10001, tgRef: 1514000, unitRef: 4500, unitLabel: "Dispatch"},
	{token: "#ZTIME", mask: "#SYS-#TG-#DATE-#ZTIME", filename: "10001-1514000-20261006-120000.wav", systemRef: 10001, tgRef: 1514000, timestamp: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)},
}

// bracketedTableShapes are the same shapes with every token of the mask wrapped in brackets the
// mask escapes, which is how a mask that means the brackets a recording tool wrote into the file
// name is written: the brackets stay the filename's own, and each token still hands its piece to
// the value it names.
var bracketedTableShapes = []maskShape{
	{token: "#DATE", mask: "\\[#SYS\\]-\\[#TG\\]-\\[#DATE\\]", filename: "[10001]-[1514000]-[20261006].wav", systemRef: 10001, tgRef: 1514000, timestamp: time.Unix(20261006, 0).UTC()},
	{token: "#GROUP", mask: "\\[#SYS\\]-\\[#TG\\]-\\[#GROUP\\]", filename: "[10001]-[1514000]-[1-3].wav", systemRef: 10001, tgRef: 1514000, tgGroup: "1-3"},
	{token: "#HZ", mask: "\\[#SYS\\]-\\[#TG\\]-\\[#HZ\\]", filename: "[10001]-[1514000]-[1514000].wav", systemRef: 10001, tgRef: 1514000, hertz: 1514000},
	{token: "#KHZ", mask: "\\[#SYS\\]-\\[#TG\\]-\\[#KHZ\\]", filename: "[10001]-[1514000]-[1514.000].wav", systemRef: 10001, tgRef: 1514000, hertz: 1514000},
	{token: "#MHZ", mask: "\\[#SYS\\]-\\[#TG\\]-\\[#MHZ\\]", filename: "[10001]-[1514000]-[151.4000].wav", systemRef: 10001, tgRef: 1514000, hertz: 151400000},
	{token: "#SITE", mask: "\\[#SYS\\]-\\[#TG\\]-\\[#SITE\\]", filename: "[10001]-[1514000]-[450].wav", systemRef: 10001, tgRef: 1514000, siteRef: 450},
	{token: "#SITELBL", mask: "\\[#SYS\\]-\\[#TG\\]-\\[#SITELBL\\]", filename: "[10001]-[1514000]-[North].wav", systemRef: 10001, tgRef: 1514000, siteLabel: "North"},
	{token: "#SYS", mask: "\\[#SYS\\]-\\[#TG\\]", filename: "[10001]-[1514000].wav", systemRef: 10001, tgRef: 1514000},
	{token: "#SYSLBL", mask: "\\[#SYSLBL\\]-\\[#TG\\]", filename: "[Grand Metro]-[1514000].wav", systemID: 1, tgRef: 1514000},
	{token: "#TAG", mask: "\\[#SYS\\]-\\[#TG\\]-\\[#TAG\\]", filename: "[10001]-[1514000]-[Z3].wav", systemRef: 10001, tgRef: 1514000, tgTag: "Z3"},
	{token: "#TG", mask: "\\[#SYS\\]-\\[#TG\\]", filename: "[10001]-[1514000].wav", systemRef: 10001, tgRef: 1514000},
	{token: "#TGAFS", mask: "\\[#SYS\\]-\\[#TGAFS\\]", filename: "[10001]-[12-345].wav", systemRef: 10001, tgRef: 12<<7 | 34<<3 | 5},
	{token: "#TGHZ", mask: "\\[#SYS\\]-\\[#TGHZ\\]", filename: "[10001]-[1514000].wav", systemRef: 10001, tgRef: 1514, hertz: 1514000},
	{token: "#TGKHZ", mask: "\\[#SYS\\]-\\[#TGKHZ\\]", filename: "[10001]-[1514.000].wav", systemRef: 10001, tgRef: 1514, hertz: 1514000},
	{token: "#TGLBL", mask: "\\[#SYS\\]-\\[#TG\\]-\\[#TGLBL\\]", filename: "[10001]-[1514000]-[Dispatch].wav", systemRef: 10001, tgRef: 1514000, tgLabel: "Dispatch"},
	{token: "#TGMHZ", mask: "\\[#SYS\\]-\\[#TGMHZ\\]", filename: "[10001]-[151.4000].wav", systemRef: 10001, tgRef: 151400, hertz: 151400000},
	{token: "#TIME", mask: "\\[#SYS\\]-\\[#TG\\]-\\[#DATE\\]-\\[#TIME\\]", filename: "[10001]-[1514000]-[20261006]-[120000].wav", systemRef: 10001, tgRef: 1514000, timestamp: time.Date(2026, 10, 6, 16, 0, 0, 0, time.UTC)},
	{token: "#UNIT", mask: "\\[#SYS\\]-\\[#TG\\]-\\[#UNIT\\]", filename: "[10001]-[1514000]-[4500].wav", systemRef: 10001, tgRef: 1514000, unitRef: 4500},
	{token: "#UNITLBL", mask: "\\[#SYS\\]-\\[#TG\\]-\\[#UNIT\\]-\\[#UNITLBL\\]", filename: "[10001]-[1514000]-[4500]-[Dispatch].wav", systemRef: 10001, tgRef: 1514000, unitRef: 4500, unitLabel: "Dispatch"},
	{token: "#ZTIME", mask: "\\[#SYS\\]-\\[#TG\\]-\\[#DATE\\]-\\[#ZTIME\\]", filename: "[10001]-[1514000]-[20261006]-[120000].wav", systemRef: 10001, tgRef: 1514000, timestamp: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)},
}

// maskTableToken is one token of the table the mask parser substitutes with, as the tests that
// read the whole table need it.
type maskTableToken struct {
	text    string
	pattern string
}

// productionMaskTokens are the mask tokens, read from the code that substitutes them, so that a
// token added to the table is a token these tests have to read.
func productionMaskTokens() []maskTableToken {
	tokens := []maskTableToken{}

	for _, token := range domain.MaskTokens {
		tokens = append(tokens, maskTableToken{text: token.Text, pattern: token.Pattern})
	}

	return tokens
}

// checkEveryMaskTokenHasAShape reports a token of the production table that no shape names, which
// is a token nothing reads.
func checkEveryMaskTokenHasAShape(t *testing.T, shapes []maskShape) {
	t.Helper()

	for _, token := range productionMaskTokens() {
		covered := false

		for _, shape := range shapes {
			if shape.token == token.text {
				covered = true

				break
			}
		}

		if !covered {
			t.Errorf("the mask token table names %s and no shape reads it, want every token of the table read", token.text)
		}
	}
}

// readMaskShapes reads each shape through a default watch and reports every value its mask names
// that the read did not hand to the field the token it sits under owns.
func readMaskShapes(t *testing.T, shapes []maskShape) {
	t.Helper()

	for _, shape := range shapes {
		watch := domain.NewDirwatch()
		watch.Mask = shape.mask

		extracted, recognized, err := extract(t, watch, shape.filename)

		if err != nil {
			t.Errorf("the mask %q did not read %q: %v, want the mask read as it is written", shape.mask, shape.filename, err)

			continue
		}

		if !recognized {
			t.Errorf("the mask %q did not read %q as a recording, want the file handed to the ingest path", shape.mask, shape.filename)

			continue
		}

		checkMaskShapeRead(t, shape, extracted.Call)
	}
}

// checkMaskShapeRead reports each value the shape's mask names that the call it read does not
// carry.
func checkMaskShapeRead(t *testing.T, shape maskShape, call domain.Call) {
	t.Helper()

	if shape.systemID > 0 && call.System.ID != shape.systemID {
		t.Errorf("%s: the call belongs to system %#v, want the configured system the mask named by name", shape.token, call.System)
	}

	if shape.systemRef > 0 && call.Meta.SystemRef != shape.systemRef {
		t.Errorf("%s: the call claims system reference %d, want %d", shape.token, call.Meta.SystemRef, shape.systemRef)
	}

	if call.Meta.SiteRef != shape.siteRef || call.Meta.SiteLabel != shape.siteLabel {
		t.Errorf("%s: the call claims site %d/%q, want %d/%q", shape.token, call.Meta.SiteRef, call.Meta.SiteLabel, shape.siteRef, shape.siteLabel)
	}

	if shape.tgRef > 0 && call.Meta.TalkgroupRef != shape.tgRef {
		t.Errorf("%s: the call claims talkgroup reference %d, want %d", shape.token, call.Meta.TalkgroupRef, shape.tgRef)
	}

	if call.Meta.TalkgroupLabel != shape.tgLabel {
		t.Errorf("%s: the call claims the talkgroup name %q, want %q", shape.token, call.Meta.TalkgroupLabel, shape.tgLabel)
	}

	if call.Meta.TalkgroupTag != shape.tgTag {
		t.Errorf("%s: the call claims the talkgroup tag %q, want %q", shape.token, call.Meta.TalkgroupTag, shape.tgTag)
	}

	wantGroup := []string{}
	if shape.tgGroup != "" {
		wantGroup = []string{shape.tgGroup}
	}

	if len(call.Meta.TalkgroupGroup) != len(wantGroup) || (len(wantGroup) > 0 && call.Meta.TalkgroupGroup[0] != wantGroup[0]) {
		t.Errorf("%s: the call claims the group %v, want %v", shape.token, call.Meta.TalkgroupGroup, wantGroup)
	}

	if shape.unitRef > 0 && (len(call.Units) != 1 || call.Units[0].UnitRef != shape.unitRef) {
		t.Errorf("%s: the call units are %#v, want the one unit %d", shape.token, call.Units, shape.unitRef)
	}

	if shape.unitLabel != "" && (len(call.Meta.UnitLabels) != 1 || call.Meta.UnitLabels[0] != shape.unitLabel) {
		t.Errorf("%s: the call claims the unit names %v, want the one name %q", shape.token, call.Meta.UnitLabels, shape.unitLabel)
	}

	if shape.hertz > 0 && (len(call.Frequencies) != 1 || call.Frequencies[0].Frequency != shape.hertz) {
		t.Errorf("%s: the call carriers are %#v, want the one carrier %d Hz", shape.token, call.Frequencies, shape.hertz)
	}

	if !shape.timestamp.IsZero() && !call.Timestamp.Equal(shape.timestamp) {
		t.Errorf("%s: the call timestamp is %v, want %v", shape.token, call.Timestamp, shape.timestamp)
	}
}

// TestExtractDefaultReadsEveryTokenTheMaskTableNames is the whole token table read: every token
// the mask carries is named by one shape below, its own piece lands on the value that token
// names, and a token whose pattern or substitution changes is a token this test stops reading.
func TestExtractDefaultReadsEveryTokenTheMaskTableNames(t *testing.T) {
	checkEveryMaskTokenHasAShape(t, wholeTableShapes)

	readMaskShapes(t, wholeTableShapes)
}

// TestExtractDefaultReadsAMaskThatWrapsEveryTokenInLiteralBrackets is the same read through the
// brackets themselves: a mask that means the brackets a recording tool wrote into the file name
// escapes them, and the escaped token keeps the capture group and the piece it names. The read
// has always done this, and it is the shape a mask carrying brackets has to keep working for.
func TestExtractDefaultReadsAMaskThatWrapsEveryTokenInLiteralBrackets(t *testing.T) {
	checkEveryMaskTokenHasAShape(t, bracketedTableShapes)

	readMaskShapes(t, bracketedTableShapes)
}

// unescapedBracketMasks are the whole token table with each token wrapped in the brackets an
// operator types: brackets that are regular-expression syntax in themselves, so the plain check
// a mask is given when the watch is saved accepts them, and the substitution then folds them into
// the pattern the token stands for.
func unescapedBracketMasks(t *testing.T) []string {
	t.Helper()

	masks := []string{}

	for _, token := range productionMaskTokens() {
		masks = append(masks, "["+token.text+"]")
	}

	return masks
}

// TestExtractDefaultRefusesEveryMaskThatWrapsATokenInUnescapedBrackets is the read half of the same
// shape: a mask whose substituted string will not compile, or compiles while capturing nothing the
// mask names, is refused with a reason naming the mask, and the watcher is not stopped. A panic
// here lands in the goroutine reading the directory notification, and the watch ends there, deaf to
// that directory until the server restarts; a call built from a degenerate character class files a
// recording with the wrong things on it.
func TestExtractDefaultRefusesEveryMaskThatWrapsATokenInUnescapedBrackets(t *testing.T) {
	name := "10001-1514000-20261006-120000.wav"

	for _, mask := range unescapedBracketMasks(t) {
		watch := domain.NewDirwatch()
		watch.Mask = mask
		watch.Extension = "wav"

		path := watchedFile(t, name)

		var (
			extracted  Extracted
			recognized bool
			err        error
			crashed    any
		)

		func() {
			defer func() {
				crashed = recover()
			}()

			extracted, recognized, err = newParsers(configuredSystems()).Extract(context.Background(), watch, path)
		}()

		if crashed != nil {
			t.Errorf("reading %q through the mask %q stopped the watcher with a panic: %v, want the mask refused", name, mask, crashed)

			continue
		}

		if recognized {
			t.Errorf("the mask %q handed %q to the ingest path, want it refused: the string the watcher compiles from it captures nothing the mask names", mask, name)
		}

		if extracted.Call.AudioFilename != "" {
			t.Errorf("the mask %q returned the call %q, want no call at all", mask, extracted.Call.AudioFilename)
		}

		if err == nil {
			t.Errorf("Extract through the mask %q = nil, want the reason the mask cannot be read", mask)

			continue
		}

		if !strings.Contains(err.Error(), "mask") {
			t.Errorf("Extract through the mask %q = %v, want the error to name the mask as the reason", mask, err)
		}

		if !strings.Contains(err.Error(), mask) {
			t.Errorf("Extract through the mask %q = %v, want the error to name the mask it refused", mask, err)
		}
	}
}

// TestExtractDefaultRefusesAMaskThatTokensMakeImpossibleToCompile: a mask the plain
// regex check accepts, brackets and all, becomes impossible to compile once the tokens
// are swapped for the patterns they name. The watcher still has to be told, not
// crashed: a panic here lands in the timer goroutine reading the notification, and the
// watch ends there, deaf to that directory until the server restarts.
func TestExtractDefaultRefusesAMaskThatTokensMakeImpossibleToCompile(t *testing.T) {
	for _, mask := range impossibleAfterTokens(t) {
		watch := domain.NewDirwatch()
		watch.Mask = mask
		watch.Extension = "wav"

		path := watchedFile(t, "20261028.wav")

		var (
			recognized bool
			err        error
			crashed    any
		)

		func() {
			defer func() {
				crashed = recover()
			}()

			_, recognized, err = newParsers(configuredSystems()).Extract(context.Background(), watch, path)
		}()

		if crashed != nil {
			t.Errorf("reading %q through the mask %q stopped the watcher with a panic: %v, want the watcher told the mask cannot be read",
				"20261028.wav", mask, crashed)
			continue
		}

		if recognized {
			t.Errorf("the mask %q handed a call to the ingest path, want it refused: the mask cannot be matched at all", mask)
		}

		if err == nil {
			t.Errorf("Extract through the mask %q = nil, want the reason the mask cannot be read", mask)
			continue
		}

		if !strings.Contains(err.Error(), "mask") {
			t.Errorf("Extract through the mask %q = %v, want the error to name the mask as the reason", mask, err)
		}
	}
}

// impossibleAfterTokens are the masks that compile as written, so the apply-time check
// lets them through, and are no longer something the watch can read once every token is
// replaced by the pattern the token table gives it. The mask the watcher would compile is
// asked of the domain, which is where that string is built.
func impossibleAfterTokens(t *testing.T) []string {
	impossible := []string{}

	t.Helper()

	for _, mask := range []string{"[#DATE]", "[#TIME]", "[#SYSLBL]", "#DATE[#TG]", "#SITE-#TG"} {
		if _, err := regexp.Compile(mask); err != nil {
			t.Errorf("the mask %q does not compile as written, want a mask the apply-time check accepts", mask)
		}

		watch := domain.NewDirwatch()
		watch.Mask = mask

		if _, err := watch.MaskRegexp(); err != nil {
			impossible = append(impossible, mask)
		}
	}

	return impossible
}

// TestAMasksPiecesLandOnTheTokensThatNamedThem is how a mask is read: each filename piece
// belongs to the token that piece sits under, whatever order the mask writes its tokens in and
// whatever parentheses the mask itself carries. A token's piece used to be found by counting the
// capture groups from the start and matching that count with where in the mask the token's text
// happened to sit, and that count is wrong whenever the table visits a token that comes later in
// the mask before one that comes earlier: each replacement shifts the rest of the mask along, so
// the position recorded for an earlier token is a place in a string that no longer exists. What
// the parser then files is one piece too far along, which for the shapes below puts a talkgroup
// number on the site and a site name on the talkgroup.
func TestAMasksPiecesLandOnTheTokensThatNamedThem(t *testing.T) {
	cases := []struct {
		name      string
		mask      string
		filename  string
		watchSys  uint64
		watchTG   uint64
		systemID  uint64
		systemRef uint
		siteRef   uint
		siteLabel string
		tgRef     uint
		tgLabel   string
		group     string
		unitRef   uint
		unitLabel string
		hertz     uint
	}{
		{
			name:      "a site label written after a system label and a talkgroup",
			mask:      "#SYSLBL-#TG-#SITELBL",
			filename:  "Grand Metro-1514000-North.wav",
			systemID:  1,
			siteLabel: "North",
			tgRef:     1514000,
		},
		{
			name:      "a talkgroup label written between a system label and a site label",
			mask:      "#SYSLBL-#TGLBL-#SITELBL",
			filename:  "Grand Metro-Dispatch-North.wav",
			watchTG:   2,
			systemID:  1,
			siteLabel: "North",
			tgLabel:   "Dispatch",
		},
		{
			name:      "a site written after a date and a time",
			mask:      "#DATE-#TIME-#SYS-#TG-#SITE",
			filename:  "20261006-120000-10001-1514000-7.wav",
			systemRef: 10001,
			siteRef:   7,
			tgRef:     1514000,
		},
		{
			name:      "a site label written after a date, a time, and a system label",
			mask:      "#DATE-#TIME-#SYSLBL-#TG-#SITELBL",
			filename:  "20261006-120000-Grand Metro-1514000-North.wav",
			systemID:  1,
			siteLabel: "North",
			tgRef:     1514000,
		},
		{
			name:     "a parenthesis the mask carries itself",
			mask:     "call(1)-#TG-#SITE",
			filename: "call1-1514000-7.wav",
			watchSys: 1,
			siteRef:  7,
			tgRef:    1514000,
		},
		{
			name:     "a group written after a system label and a talkgroup",
			mask:     "#SYSLBL-#TG-#GROUP",
			filename: "Grand Metro-1514000-Dispatch.wav",
			systemID: 1,
			group:    "Dispatch",
			tgRef:    1514000,
		},
		{
			name:     "a talkgroup label written after a system label and a talkgroup",
			mask:     "#SYSLBL-#TG-#TGLBL",
			filename: "Grand Metro-1514000-Dispatch.wav",
			systemID: 1,
			tgLabel:  "Dispatch",
			tgRef:    1514000,
		},
		{
			name:      "a site label written before a system label",
			mask:      "#SITELBL-#SYSLBL-#TG",
			filename:  "North-Grand Metro-1514000.wav",
			systemID:  1,
			siteLabel: "North",
			tgRef:     1514000,
		},
		{
			name:      "a site written before a system and a talkgroup",
			mask:      "#SITE-#SYS-#TG",
			filename:  "7-10001-1514000.wav",
			systemRef: 10001,
			siteRef:   7,
			tgRef:     1514000,
		},
		{
			name:     "a site written between a system label and a talkgroup",
			mask:     "#SYSLBL-#SITE-#TG",
			filename: "Grand Metro-7-1514000.wav",
			systemID: 1,
			siteRef:  7,
			tgRef:    1514000,
		},
		{
			name:      "a unit label written after a unit",
			mask:      "#SYS-#TG-#UNIT-#UNITLBL",
			filename:  "10001-1514000-6789-Dispatch.wav",
			systemRef: 10001,
			tgRef:     1514000,
			unitRef:   6789,
			unitLabel: "Dispatch",
		},
	}

	for _, testCase := range cases {
		watch := domain.NewDirwatch()
		watch.Mask = testCase.mask
		watch.SystemID = testCase.watchSys
		watch.TalkgroupID = testCase.watchTG

		extracted, _, err := extract(t, watch, testCase.filename)
		if err != nil {
			t.Errorf("%s: Extract: %v, want the mask read as it is written, each filename piece at the token it sits "+
				"under", testCase.name, err)

			continue
		}

		call := extracted.Call
		meta := call.Meta

		if testCase.systemID > 0 && call.System.ID != testCase.systemID {
			t.Errorf("%s: the call belongs to system %#v, want the configured system the mask named by label",
				testCase.name, call.System)
		}

		if testCase.systemRef > 0 && meta.SystemRef != testCase.systemRef {
			t.Errorf("%s: the call claims system reference %d, want %d: the reference the mask names at its own place",
				testCase.name, meta.SystemRef, testCase.systemRef)
		}

		if meta.SiteRef != testCase.siteRef || meta.SiteLabel != testCase.siteLabel {
			t.Errorf("%s: the call claims site %d/%q, want %d/%q: the site the mask names at its own place",
				testCase.name, meta.SiteRef, meta.SiteLabel, testCase.siteRef, testCase.siteLabel)
		}

		if meta.TalkgroupRef != testCase.tgRef {
			t.Errorf("%s: the call claims talkgroup reference %d, want %d: the number the mask names at its own place",
				testCase.name, meta.TalkgroupRef, testCase.tgRef)
		}

		if meta.TalkgroupLabel != testCase.tgLabel {
			t.Errorf("%s: the call claims the talkgroup name %q, want %q: the name the mask names at its own place",
				testCase.name, meta.TalkgroupLabel, testCase.tgLabel)
		}

		wantGroup := []string{}
		if testCase.group != "" {
			wantGroup = []string{testCase.group}
		}

		if len(meta.TalkgroupGroup) != len(wantGroup) || (len(wantGroup) > 0 && meta.TalkgroupGroup[0] != wantGroup[0]) {
			t.Errorf("%s: the call claims the group %v, want %v: the group the mask names at its own place",
				testCase.name, meta.TalkgroupGroup, wantGroup)
		}

		if testCase.unitRef > 0 && (len(call.Units) != 1 || call.Units[0].UnitRef != testCase.unitRef) {
			t.Errorf("%s: the call units are %#v, want the one unit %d the mask names at its own place",
				testCase.name, call.Units, testCase.unitRef)
		}

		if testCase.unitLabel != "" && (len(meta.UnitLabels) != 1 || meta.UnitLabels[0] != testCase.unitLabel) {
			t.Errorf("%s: the call claims the unit names %v, want the one name %q the mask names beside its unit",
				testCase.name, meta.UnitLabels, testCase.unitLabel)
		}

		if testCase.hertz > 0 && (len(call.Frequencies) != 1 || call.Frequencies[0].Frequency != testCase.hertz) {
			t.Errorf("%s: the call carriers are %#v, want the one carrier %d Hz the mask names at its own place",
				testCase.name, call.Frequencies, testCase.hertz)
		}
	}
}
