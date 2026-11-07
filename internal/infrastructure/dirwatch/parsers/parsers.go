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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dhowden/tag"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Extracted is what a file that appeared in a watched directory is worth to the ingest
// path.
type Extracted struct {
	// Call is the call the file describes, with its system and talkgroup	// still named as claims: resolving them is the ingest use case's job,	// the same job for a watch, an upload, and a forwarded call.
	Call domain.Call

	// Files are every file the call was built from: the file the watcher	// noticed plus any companion a parser read, so deleting after ingesting	// deletes them all.
	Files []string
}

// Parsers read what an ingest source produced into the call the server// records: the four shapes a watched directory holds, and the form// fields an upload endpoint is handed.
type Parsers struct {
	systems ports.SystemRepository
	clock   ports.Clock
}

// New builds the parsers for a server that ingests calls from watched directories and from
// uploads.
func New(systems ports.SystemRepository, clock ports.Clock) *Parsers {
	return &Parsers{
		systems: systems,
		clock:   clock,
	}
}

// Extract reads one file that appeared in a watched directory. The// second result says whether the file was this watch's parser's file// at all: a file it does not recognize is not a failure and nothing is// said about it, as in the legacy. A file it recognizes but cannot// turn into a call is a failure, and the watcher reports it in its// log, which is how an administrator finds a watch set to the wrong// shape.
func (parsers *Parsers) Extract(ctx context.Context, watch domain.Dirwatch, filePath string) (Extracted, bool, error) {
	var (
		extracted Extracted
		err       error
	)

	var recognized bool

	switch watch.Kind {
	case domain.DirwatchTypeDSDPlus:
		extracted, recognized, err = parsers.extractDSDPlus(watch, filePath)

	case domain.DirwatchTypeTrunkRecorder:
		extracted, recognized, err = parsers.extractTrunkRecorder(ctx, watch, filePath)

	case domain.DirwatchTypeSdrTrunk:
		extracted, recognized, err = parsers.extractSdrTrunk(ctx, watch, filePath)

	default:
		extracted, recognized, err = parsers.extractDefault(ctx, watch, filePath)
	}

	if err != nil {
		return Extracted{}, false, err
	}

	return extracted, recognized, nil
}

// extractDefault reads a recording whose metadata is in its filename, which is what the
// watch's mask says where in the name each piece of it is.
func (parsers *Parsers) extractDefault(ctx context.Context, watch domain.Dirwatch, filePath string) (Extracted, bool, error) {
	if !hasExtension(filePath, watch.Extension, ".wav") {
		return Extracted{}, false, nil
	}

	call := domain.NewCall()

	call.AudioFilename = filepath.Base(filePath)
	call.AudioMime = mime.TypeByExtension(path.Ext(filePath))

	// The recording is stamped with the moment it was noticed, and the	// mask can replace that with the moment the filename names. A mask	// with no date tokens therefore records a call at the time the file	// appeared, not at the time it was recorded.
	call.Timestamp = parsers.clock.Now().UTC()

	if watch.Frequency > 0 {
		call.Frequencies = append(call.Frequencies, domain.CallFrequency{
			Frequency: watch.Frequency,
			Offset:    0,
		})
	}

	audio, err := os.ReadFile(filePath)
	if err != nil {
		return Extracted{}, false, err
	}

	call.Audio = audio

	if err := parsers.applyMask(ctx, watch, &call); err != nil {
		return Extracted{}, false, err
	}

	// The watch's own system and talkgroup win over anything the mask claimed, which is
	// what the legacy applies last.
	if watch.SystemID > 0 {
		call.Meta.SystemID = watch.SystemID
	}

	if watch.TalkgroupID > 0 {
		call.Meta.TalkgroupID = watch.TalkgroupID
	}

	if err := call.IsValid(); err != nil {
		return Extracted{}, false, err
	}

	return Extracted{Call: call, Files: []string{filePath}}, true, nil
}

// extractDSDPlus reads a recording whose metadata is in the name dsd+ gives it, and the date
// dsd+ writes into the directory name it recorded into.
func (parsers *Parsers) extractDSDPlus(watch domain.Dirwatch, filePath string) (Extracted, bool, error) {
	if !hasExtension(filePath, watch.Extension, ".mp3") {
		return Extracted{}, false, nil
	}

	call := domain.NewCall()

	call.AudioFilename = filepath.Base(filePath)
	call.AudioMime = mime.TypeByExtension(path.Ext(filePath))

	if watch.Frequency > 0 {
		call.Frequencies = append(call.Frequencies, domain.CallFrequency{
			Frequency: watch.Frequency,
			Offset:    0,
		})
	}

	// The watch's own system and talkgroup are claimed for every	// recording in the directory, as they are for a default watch. Its	// site is not: the legacy writes it into a metadata field nothing ever	// reads, so a dsd+ watch's site setting has never had an effect.
	if watch.SystemID > 0 {
		call.Meta.SystemID = watch.SystemID
	}

	if watch.TalkgroupID > 0 {
		call.Meta.TalkgroupID = watch.TalkgroupID
	}

	audio, err := os.ReadFile(filePath)
	if err != nil {
		return Extracted{}, false, err
	}

	call.Audio = audio

	if err := parsers.applyDSDPlus(&call, filePath); err != nil {
		return Extracted{}, false, err
	}

	if err := call.IsValid(); err != nil {
		return Extracted{}, false, err
	}

	return Extracted{Call: call, Files: []string{filePath}}, true, nil
}

// extractSdrTrunk reads a recording whose metadata sdr-trunk wrote into its audio tags.
func (parsers *Parsers) extractSdrTrunk(ctx context.Context, watch domain.Dirwatch, filePath string) (Extracted, bool, error) {
	if !strings.EqualFold(path.Ext(filePath), ".mp3") {
		return Extracted{}, false, nil
	}

	call := domain.NewCall()

	call.AudioFilename = filepath.Base(filePath)
	call.AudioMime = mime.TypeByExtension(path.Ext(filePath))

	if watch.Frequency > 0 {
		call.Frequencies = append(call.Frequencies, domain.CallFrequency{
			Frequency: watch.Frequency,
			Offset:    0,
		})
	}

	audio, err := os.ReadFile(filePath)
	if err != nil {
		return Extracted{}, false, err
	}

	call.Audio = audio

	if err := parsers.applySdrTrunk(ctx, &call); err != nil {
		return Extracted{}, false, err
	}

	if err := call.IsValid(); err != nil {
		return Extracted{}, false, err
	}

	return Extracted{Call: call, Files: []string{filePath}}, true, nil
}

// extractTrunkRecorder reads a recording from the pair trunk-recorder// writes: the sidecar that describes the call, and the recording beside// it. The recording is read first, and a sidecar whose recording is not// there is dropped without a word, which is what the legacy does by// returning the read failure as a nil error; a sidecar that cannot be// read at all is reported.
func (parsers *Parsers) extractTrunkRecorder(ctx context.Context, watch domain.Dirwatch, filePath string) (Extracted, bool, error) {
	if !strings.EqualFold(path.Ext(filePath), ".json") {
		return Extracted{}, false, nil
	}

	audioPath := strings.TrimSuffix(filePath, ".json") + audioExtension(watch.Extension)

	call := domain.NewCall()

	call.AudioFilename = filepath.Base(audioPath)
	call.AudioMime = mime.TypeByExtension(path.Ext(audioPath))

	if watch.Frequency > 0 {
		call.Frequencies = append(call.Frequencies, domain.CallFrequency{
			Frequency: watch.Frequency,
			Offset:    0,
		})
	}

	audio, err := os.ReadFile(audioPath)
	if err != nil {
		return Extracted{}, false, nil
	}

	call.Audio = audio

	sidecar, err := os.ReadFile(filePath)
	if err != nil {
		return Extracted{}, false, err
	}

	if err := parsers.applyTrunkRecorder(&call, sidecar); err != nil {
		return Extracted{}, false, err
	}

	if err := call.IsValid(); err != nil {
		return Extracted{}, false, err
	}

	return Extracted{Call: call, Files: []string{filePath, audioPath}}, true, nil
}

// applyDSDPlus reads the call out of the name dsd+ gives a recording,// and out of the directory dsd+ recorded it into. The name is split on// underscores that are not inside square brackets, because dsd+ writes// labels in brackets and a label may itself contain underscores.
func (parsers *Parsers) applyDSDPlus(call *domain.Call, filePath string) error {
	dir := filepath.Dir(filePath)
	base := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))

	meta := splitDSDPlusName(base)

	// The talkgroup and the source are read from the last two pieces of the	// name, so a name with fewer than two pieces has nothing to read them	// from. The legacy reads them unguarded and takes the server down with	// an index-out-of-range panic; here the name is refused and reported,	// the outcome the legacy's startup walk already produces for the same	// file.
	if len(meta) < 2 {
		return fmt.Errorf("dsdplus: %s does not name a talkgroup and a source", base)
	}

	// The date is only in the name dsd+ writes when it recorded into a directory named for
	// the day, and the time is only in a name that starts with one.
	if day := regexp.MustCompile(`([0-9]+)$`).FindStringSubmatch(dir); len(day) == 2 && len(day[1]) == 8 {
		if clock := regexp.MustCompile(`^([0-9]+)`).FindStringSubmatch(base); len(clock) == 2 && len(clock[1]) == 6 {
			year, errYear := strconv.Atoi(day[1][0:4])
			month, errMonth := strconv.Atoi(day[1][4:6])
			dayOfMonth, errDay := strconv.Atoi(day[1][6:8])
			hour, errHour := strconv.Atoi(clock[1][0:2])
			minute, errMinute := strconv.Atoi(clock[1][2:4])
			second, errSecond := strconv.Atoi(clock[1][4:6])

			if errYear == nil && errMonth == nil && errDay == nil && errHour == nil && errMinute == nil && errSecond == nil {
				call.Timestamp = time.Date(
					year,
					time.Month(month),
					dayOfMonth,
					hour,
					minute,
					second,
					0,
					parsers.clock.Now().Location(),
				).UTC()
			}
		}
	}

	// The system is named by the third piece of the name, and only for the system types dsd+
	// reports it for. Each type writes the reference in its own shape.
	if len(meta) > 3 {
		switch meta[2] {
		case "ConP(BS)", "DMR(BS)", "P25(BS)":
			if name := regexp.MustCompile(`^([0-9]+)-.+$`).FindStringSubmatch(meta[3]); len(name) > 1 {
				if ref, err := strconv.Atoi(name[1]); err == nil {
					call.Meta.SystemRef = uint(ref)
				}
			}

		case "NEXEDGE48(CB)", "NEXEDGE48(CS)", "NEXEDGE48(TB)", "NEXEDGE96(CB)", "NEXEDGE96(CS)", "NEXEDGE96(TB)":
			if name := regexp.MustCompile(`^.([0-9]+)-[0-9]+$`).FindStringSubmatch(meta[3]); len(name) > 1 {
				if ref, err := strconv.Atoi(name[1]); err == nil && ref > 0 {
					call.Meta.SystemRef = uint(ref)
				}
			} else if len(meta) > 4 {
				if name := regexp.MustCompile(`RAN([0-9]+)`).FindStringSubmatch(meta[4]); len(name) > 1 {
					if ref, err := strconv.Atoi(name[1]); err == nil && ref > 0 {
						call.Meta.SystemRef = uint(ref)
					}
				}
			}

		case "P25":
			if name := regexp.MustCompile(`^[^\.]+\.([^-]+)`).FindStringSubmatch(meta[3]); len(name) > 1 {
				if ref, err := strconv.ParseInt(name[1], 16, 64); err == nil && ref > 0 {
					call.Meta.SystemRef = uint(ref)
				}
			}
		}
	}

	// The talkgroup is the first number of the second-to-last piece, and	// its label is whatever follows it, unless that is only punctuation,	// which is what dsd+ writes when it has no name for the talkgroup.
	if parts := regexp.MustCompile(`[^\[\]]+`).FindAllString(meta[len(meta)-2], -1); len(parts) > 0 {
		if ref, err := strconv.Atoi(parts[0]); err == nil && ref > 0 {
			call.Meta.TalkgroupRef = uint(ref)
		}

		if len(parts) > 1 && len(parts[1]) > 0 {
			if !regexp.MustCompile(`^([\.\-\ ,_]+)$`).MatchString(parts[1]) {
				call.Meta.TalkgroupLabel = parts[1]
			}
		}
	}

	// The source is the first number of the last piece, and its label is	// whatever follows it - unless that is the number again, which is what	// dsd+ writes when it has no name for the radio, or only punctuation.
	if parts := regexp.MustCompile(`[^\[\]]+`).FindAllString(meta[len(meta)-1], -1); len(parts) > 0 {
		if source, err := strconv.Atoi(parts[0]); err == nil && source > 0 {
			call.Units = append(call.Units, domain.CallUnit{
				UnitRef: uint(source),
				Offset:  0,
			})

			if len(parts) > 1 && len(parts[1]) > 0 {
				label := strings.TrimSpace(parts[1])

				if label != fmt.Sprintf("%v", source) {
					if !regexp.MustCompile(`^([\.\-\ ,_]+)$`).MatchString(parts[1]) {
						call.Meta.UnitLabels = append(call.Meta.UnitLabels, label)
						call.Meta.UnitRefs = append(call.Meta.UnitRefs, uint(source))
					}
				}
			}
		}
	}

	return nil
}

// applySdrTrunk reads the call out of the tags sdr-trunk writes into the recording.
func (parsers *Parsers) applySdrTrunk(ctx context.Context, call *domain.Call) error {
	metadata, err := tag.ReadFrom(bytes.NewReader(call.Audio))
	if err != nil {
		return err
	}

	// The artist is the radio that spoke, and its label after the reference.
	if parts := regexp.MustCompile(`^([0-9]+) ?(.*)$`).FindStringSubmatch(metadata.Artist()); len(parts) >= 2 {
		ref, err := strconv.Atoi(parts[1])
		if err != nil {
			return err
		}

		if ref > 0 {
			call.Units = append(call.Units, domain.CallUnit{
				UnitRef: uint(ref),
				Offset:  0,
			})

			if len(parts) >= 3 && len(parts[2]) > 0 {
				call.Meta.UnitLabels = append(call.Meta.UnitLabels, strings.TrimSpace(parts[2]))
				call.Meta.UnitRefs = append(call.Meta.UnitRefs, uint(ref))
			}
		}
	}

	// The comment is one run of labeled fields separated by semicolons.
	comment := metadata.Comment()

	if parts := regexp.MustCompile(`Date:([^;]+);`).FindStringSubmatch(comment); len(parts) == 2 {
		timestamp, err := time.ParseInLocation("2006-01-02 15:04:05.999", parts[1], parsers.clock.Now().Location())
		if err != nil {
			return err
		}

		call.Timestamp = timestamp
	}

	if parts := regexp.MustCompile(`Frequency:([0-9]+);`).FindStringSubmatch(comment); len(parts) == 2 && len(parts[1]) > 0 {
		frequency, err := strconv.Atoi(parts[1])
		if err != nil {
			return err
		}

		if frequency > 0 {
			call.Frequencies = []domain.CallFrequency{
				{
					Frequency: uint(frequency),
					Offset:    0,
				},
			}
		}
	}

	if parts := regexp.MustCompile(`System:([^;]+);`).FindStringSubmatch(comment); len(parts) == 2 {
		systems, err := parsers.systems.AllSystems(ctx)
		if err != nil {
			return err
		}

		if system, ok := systems.ByLabel(parts[1]); ok {
			call.System = system
		} else {
			call.Meta.SystemRef = systems.NextSystemRef()
			call.Meta.SystemLabel = parts[1]
		}
	}

	if parts := regexp.MustCompile(`([0-9]+)`).FindStringSubmatch(metadata.Title()); len(parts) > 1 && len(parts[1]) > 0 {
		ref, err := strconv.Atoi(parts[1])
		if err != nil {
			return err
		}

		if ref > 0 {
			call.Meta.TalkgroupRef = uint(ref)
		}
	}

	// The talkgroup the call was patched to is written into the title as a JSON array.
	if parts := regexp.MustCompile(`(\[.+\])`).FindStringSubmatch(metadata.Title()); len(parts) > 1 && len(parts[1]) > 0 {
		var patches any

		if err := json.Unmarshal([]byte(parts[1]), &patches); err == nil {
			if list, ok := patches.([]any); ok {
				for _, patch := range list {
					if ref, ok := patch.(float64); ok && ref > 0 {
						call.PatchTalkgroupRefs = append(call.PatchTalkgroupRefs, uint(ref))
					}
				}
			}
		}
	}

	if parts := regexp.MustCompile(`"([^"]+)"`).FindStringSubmatch(metadata.Title()); len(parts) > 1 {
		call.Meta.TalkgroupLabel = parts[1]
		call.Meta.TalkgroupName = parts[1]
	}

	return nil
}

// applyTrunkRecorder reads the call out of the sidecar trunk-recorder writes beside the
// recording.
func (parsers *Parsers) applyTrunkRecorder(call *domain.Call, sidecar []byte) error {
	m := map[string]any{}

	if err := json.Unmarshal(sidecar, &m); err != nil {
		return err
	}

	if list, ok := m["freqList"].([]any); ok {
		for _, item := range list {
			frequency := domain.CallFrequency{}

			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}

			if errors, ok := entry["error_count"].(float64); ok && errors >= 0 {
				frequency.Errors = uint(errors)
			}

			if value, ok := entry["freq"].(float64); ok && value > 0 {
				frequency.Frequency = uint(value)
			}

			if offset, ok := entry["pos"].(float64); ok && offset >= 0 {
				frequency.Offset = float32(offset)
			}

			if spikes, ok := entry["spike_count"].(float64); ok && spikes >= 0 {
				frequency.Spikes = uint(spikes)
			}

			call.Frequencies = append(call.Frequencies, frequency)
		}
	}

	if list, ok := m["patched_talkgroups"].([]any); ok {
		for _, item := range list {
			if ref, ok := item.(float64); ok && ref > 0 {
				call.PatchTalkgroupRefs = append(call.PatchTalkgroupRefs, uint(ref))
			}
		}
	}

	if list, ok := m["srcList"].([]any); ok {
		for _, item := range list {
			unit := domain.CallUnit{}

			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}

			if offset, ok := entry["pos"].(float64); ok && offset >= 0 {
				unit.Offset = float32(offset)
			}

			if source, ok := entry["src"].(float64); ok && source > 0 {
				unit.UnitRef = uint(source)
			}

			call.Units = append(call.Units, unit)
		}
	}

	// The recorded instant is read from `start_time` and then replaced with	// the moment the file was read, a debug line the legacy server has	// carried since the field was written. A sidecar that also names	// `timestamp` - read below, later in the same pass - keeps its	// instant; one that does not is recorded as a call that happened now.
	if start, ok := m["start_time"].(float64); ok {
		_ = time.Unix(int64(start), 0)

		call.Timestamp = parsers.clock.Now()
	}

	if label, ok := m["short_name"].(string); ok && len(label) > 0 && label != "-" {
		call.Meta.SystemLabel = label
	}

	if ref, ok := m["talkgroup"].(float64); ok && ref > 0 {
		call.Meta.TalkgroupRef = uint(ref)
	}

	if name, ok := m["talkgroup_description"].(string); ok && len(name) > 0 && name != "-" {
		call.Meta.TalkgroupName = name
	}

	if group, ok := m["talkgroup_group"].(string); ok && len(group) > 0 && group != "-" {
		call.Meta.TalkgroupGroup = append(call.Meta.TalkgroupGroup, group)
	}

	if tag, ok := m["talkgroup_group_tag"].(string); ok && len(tag) > 0 && tag != "-" {
		call.Meta.TalkgroupTag = tag
	}

	if label, ok := m["talkgroup_tag"].(string); ok && len(label) > 0 && label != "-" {
		call.Meta.TalkgroupLabel = label
	}

	if timestamp, ok := m["timestamp"].(float64); ok {
		call.Timestamp = time.UnixMilli(int64(timestamp))
	}

	return nil
}

// ApplyTrunkRecorderSidecar reads the JSON document trunk-recorder// writes beside a recording, reached from the call-upload endpoint// instead of from a watched directory. It is the same reading and it// keeps the same quirk: a sidecar naming a start time and no timestamp// is recorded at the moment the server read it.
func (parsers *Parsers) ApplyTrunkRecorderSidecar(call *domain.Call, sidecar []byte) error {
	return parsers.applyTrunkRecorder(call, sidecar)
}

// splitDSDPlusName splits a dsd+ recording name on the underscores that are not inside
// square brackets.
func splitDSDPlusName(base string) []string {
	meta := []string{""}

	insideLabel := false
	current := 0

	for i := 0; i < len(base); i++ {
		switch base[i] {
		case '[':
			insideLabel = true

		case ']':
			insideLabel = false
		}

		if !insideLabel && base[i] == '_' {
			current++

			meta = append(meta, "")

			continue
		}

		meta[current] += string(base[i])
	}

	return meta
}

// hasExtension reports whether a watched file is the kind this parser// reads. The watch's own extension wins over the parser's default, and// the comparison ignores case. The configured extension is written// without its dot, because that is what the administrator types: an// extension typed with a leading dot asks for a file whose extension// is a dot followed by the extension, which matches nothing.
func hasExtension(filePath, configured, fallback string) bool {
	extension := fallback

	if len(configured) > 0 {
		extension = fmt.Sprintf(".%s", configured)
	}

	return strings.EqualFold(path.Ext(filePath), extension)
}

// audioExtension is the recording extension a trunk-recorder watch// looks for beside the sidecar it read. The name composes a filesystem// path, so a stored name that is not a plain name - the kind an old// server left behind before the apply-time rule - asks for a file// outside the watched directory, and asks for nothing here: the pair// is not read.
func audioExtension(configured string) string {
	if len(configured) > 0 {
		if !plainExtensionPattern.MatchString(configured) {
			return ""
		}

		return fmt.Sprintf(".%s", configured)
	}

	return ".wav"
}

// plainExtensionPattern is what a recording extension may be: letters and digits, one to
// ten of them — the same shape the domain refuses at apply.
var plainExtensionPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,10}$`)
