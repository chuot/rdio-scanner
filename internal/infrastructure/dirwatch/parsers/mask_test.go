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
	"fmt"
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
}

// TestExtractDefaultReadsTheSiteTokensTheSameWayWhicheverWayTheMaskNamesItsSystem is the half
// of the site tokens that has never worked: a mask naming its system by reference dropped both
// tokens, because the system it named resolves only later, in the ingest path. Every way a mask
// names its system settles that system in the ingest path, so the parser reads the site tokens the
// same way for all of them: it files no site on the call, and what each shape claims for the
// ingest path to resolve is pinned at the resolve rule and the ingest path, beside the system and
// talkgroup resolves. Those claim readings are T120-005's, because they name claim fields the
// domain does not have yet.
func TestExtractDefaultReadsTheSiteTokensTheSameWayWhicheverWayTheMaskNamesItsSystem(t *testing.T) {
	cases := []struct {
		name     string
		mask     string
		filename string
	}{
		{name: "a system named by reference", mask: "#SYS-#SITE", filename: "10001-7.wav"},
		{name: "a system named by label", mask: "#SYSLBL-#SITE", filename: "Grand Metro-7.wav"},
		{name: "a site named by label under a system named by label", mask: "#SYSLBL-#SITELBL", filename: "Grand Metro-North.wav"},
		{name: "a site named by label under a system the configuration does not have", mask: "#SYSLBL-#SITELBL", filename: "Airport-North.wav"},
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
// lets them through, and stop compiling once every token is replaced by the pattern the
// token table gives it.
func impossibleAfterTokens(t *testing.T) []string {
	impossible := []string{}

	t.Helper()

	for _, mask := range []string{"[#DATE]", "[#TIME]", "[#SYSLBL]", "#DATE[#TG]", "#SITE-#TG"} {
		substituted := mask

		for _, token := range maskTokens {
			if strings.Contains(substituted, token.Text) {
				substituted = strings.Replace(substituted, token.Text, fmt.Sprintf("(%v)", token.Pattern), 1)
			}
		}

		if _, err := regexp.Compile(mask); err != nil {
			t.Errorf("the mask %q does not compile as written, want a mask the apply-time check accepts", mask)
		}

		if _, err := regexp.Compile(substituted); err != nil {
			impossible = append(impossible, mask)
		}
	}

	return impossible
}
