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
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
)

// recordedAt is the moment the parsers run at, in a time zone that is not UTC so that a
// parser reading a local time and a parser reading a UTC time cannot pass each other by
// accident.
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

// TestExtractDefaultReadsTheCallTheMaskNames is the whole point of the default watch: the
// filename says the system, the talkgroup, the radio, and the moment, and the mask says where
// in the name each of them sits.
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

// TestExtractDefaultWithoutAMaskNamesNoSystemAndIsRefused is the configuration an
// administrator gets wrong most often: a default watch with no mask, no system, and no
// talkgroup. Every recording in the directory is refused, and the log says why.
func TestExtractDefaultWithoutAMaskNamesNoSystemAndIsRefused(t *testing.T) {
	_, recognized, err := extract(t, domain.NewDirwatch(), "recording.wav")

	if recognized {
		t.Error("a call naming no system was handed to the ingest path, want it refused")
	}

	if err == nil || !strings.Contains(err.Error(), "no system") {
		t.Errorf("Extract = %v, want the reason the call was refused", err)
	}
}

// TestExtractDefaultReadsTheWatchsOwnSystemAndTalkgroup is the other way to configure a
// default watch: every recording in the directory belongs to one system and one talkgroup, and
// no mask is needed.
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

// TestExtractDefaultAppliesTheWatchSystemAndTalkgroupToWhatTheMaskClaimed pins what a watch
// that configures both produces: both claims, and the identifier claim is the one the ingest
// path resolves first.
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

// TestExtractDefaultIgnoresAFileThatIsNotTheWatchsExtension is what a watch pointed at a
// directory holding other applications' output does: it reads its own files and says nothing
// about the rest.
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
// in the directory was heard on one carrier, and the mask may name more.
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
// recording tool writes down which talkgroup a call was on.
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
		{name: "a carrier in kilohertz", mask: "#SYS-#TGKHZ", filename: "10001-1514.000.wav", wantRef: 1514, wantFreq: 1514000},
		// A carrier read in megahertz is read as a 32-bit number and then scaled, which is
		// what the legacy does, so 151.4000 megahertz is 151399993 hertz and the talkgroup
		// reference derived from it is 151399.
		{name: "a carrier in megahertz", mask: "#SYS-#TGMHZ", filename: "10001-151.4000.wav", wantRef: 151399, wantFreq: 151399993},
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
// tool writes a carrier in.
func TestExtractDefaultReadsTheCarrierFromEachUnitTheMaskNames(t *testing.T) {
	cases := []struct {
		name     string
		mask     string
		filename string
		want     uint
	}{
		{name: "hertz", mask: "#SYS-#TG-#HZ", filename: "10001-1514000-1514000.wav", want: 1514000},
		{name: "kilohertz", mask: "#SYS-#TG-#KHZ", filename: "10001-1514000-1514.000.wav", want: 1514000},
		{name: "megahertz", mask: "#SYS-#TG-#MHZ", filename: "10001-1514000-151.4000.wav", want: 151399993},
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

// TestExtractDefaultReadsSeparatorsInTheDateAndTimeTheMaskNames covers the shapes recording
// tools actually write: dashes and colons in place of the runs of digits the tokens are named
// for.
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

// TestExtractDefaultReadsABareDateAsAUnixSecondCount is what a mask that names `#DATE` and
// neither time token does: the digits of the date are read as a moment in 1970. It is the
// legacy behavior, and it is the one mask configuration that produces calls dated four decades
// before the recording.
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

// TestExtractDefaultResolvesASystemNamedByLabelAndTheSiteUnderIt is the half of the site tokens
// that works: a mask that names the system by label resolves it, and the site is looked up
// among that system's sites.
func TestExtractDefaultResolvesASystemNamedByLabelAndTheSiteUnderIt(t *testing.T) {
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

	if extracted.Call.SiteRef != 7 {
		t.Errorf("the call site reference is %d, want the site the mask named under that system", extracted.Call.SiteRef)
	}
}

// TestExtractDefaultClaimsTheNextFreeReferenceForASystemTheConfigurationDoesNotHave is the
// claim auto-populate creates a system from: the name the mask read, and a reference nothing
// else is using.
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

	// The reference is the lowest one nothing is using, computed from the configuration the
	// same way the legacy computes it: for a deployment whose systems are numbered 10001 and
	// 10002, that is 1.
	if extracted.Call.Meta.SystemRef != 1 {
		t.Errorf("the call claims system reference %d, want the lowest reference the configuration is not using", extracted.Call.Meta.SystemRef)
	}
}

// TestExtractDefaultDropsTheSiteTokensForASystemNamedByReference is the half of the site tokens
// that has never worked, and the reason a deployment that configured them sees no site: a site
// is looked up under a resolved system, and a reference claim resolves only later, in the
// ingest path.
func TestExtractDefaultDropsTheSiteTokensForASystemNamedByReference(t *testing.T) {
	byRef := domain.NewDirwatch()
	byRef.Mask = "#SYS-#SITE"
	byRef.TalkgroupID = 2

	extracted, _, err := extract(t, byRef, "10001-7.wav")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if extracted.Call.SiteRef != 0 {
		t.Errorf("the call site reference is %d, want the token to be dropped: the system was named by reference, not resolved", extracted.Call.SiteRef)
	}

	byLabel := domain.NewDirwatch()
	byLabel.Mask = "#SYSLBL-#SITELBL"
	byLabel.TalkgroupID = 2

	extracted, _, err = extract(t, byLabel, "Grand Metro-North.wav")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if extracted.Call.SiteRef != 7 {
		t.Errorf("the call site reference is %d, want the site named by label under the resolved system", extracted.Call.SiteRef)
	}

	unresolved := domain.NewDirwatch()
	unresolved.Mask = "#SYSLBL-#SITELBL"
	unresolved.TalkgroupID = 2

	extracted, _, err = extract(t, unresolved, "Airport-North.wav")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if extracted.Call.SiteRef != 0 {
		t.Errorf("the call site reference is %d, want the label token dropped for a system that is not configured", extracted.Call.SiteRef)
	}
}

// TestExtractDefaultReadsTheUnitLabelTokenWhole is a preserved quirk, repaired by the decision:
// the token table applies longest-first, so a mask naming only `#UNITLBL` — the obvious
// way to write it — has the label token replaced whole. It used to have `#UNIT` replaced
// inside the longer token, leaving a literal LBL the filename then had to end with, and
// the mask silently matched nothing.
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

	// The pairing rule is the legacy's and unchanged: a name lands on the unit the
	// #UNIT token added. This mask names no unit, so the label is captured and has
	// nothing to pair with.
	if len(extracted.Call.Units) != 0 {
		t.Errorf("the call units are %#v, want none: the mask names no unit token", extracted.Call.Units)
	}

	if len(extracted.Call.Meta.UnitLabels) != 0 {
		t.Errorf("the unit labels are %#v, want none: there is no unit for the name to pair with", extracted.Call.Meta.UnitLabels)
	}
}

// TestExtractDefaultPairsTheUnitLabelWithTheUnitTheMaskNamed pins the pairing the mask
// offers, which is one unit and one name however many the filename holds — and that the
// pairing works when the mask names the unit itself before the label, which is the only way
// the label token survives the replacement order, as it now does however the mask names
// the two tokens.
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

// TestExtractDefaultReadsNothingFromAFilenameTheMaskDoesNotMatch is the misconfigured watch
// that looks correct: the mask is there, the filenames are not what it describes, and every
// recording in the directory is refused.
func TestExtractDefaultReadsNothingFromAFilenameTheMaskDoesNotMatch(t *testing.T) {
	watch := domain.NewDirwatch()
	watch.Mask = "#SYS-#TG"

	_, _, err := extract(t, watch, "recording.wav")

	if err == nil || !strings.Contains(err.Error(), "no system") {
		t.Errorf("Extract = %v, want a filename the mask does not match to name no system", err)
	}
}

// TestExtractDefaultReportsARecordingItCannotRead covers the recording that went away between
// the notification and the read, which the watcher checks for and a second application sharing
// the directory does not.
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
