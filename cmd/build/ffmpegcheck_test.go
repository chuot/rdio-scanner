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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These are component tests of the ffmpeg check: the compiled ffmpeg is run through the argument
// list the converter actually uses, and what comes back is read the way the server's audio endpoints
// read it. The commands are answered by a stand-in so that the check itself is what is being tested;
// the real compiled binaries are run by the build, and recorded in validation.md.

// anM4A is a converted recording whose first boxes say it is what the server stores.
func anM4A() []byte {
	return []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'M', '4', 'A', ' ', 0, 0, 0, 0}
}

// aStandIn answers the ffmpeg check's commands the way a working ffmpeg answers them, and keeps
// what it was asked to run. A duration or a set of tags it does not override is the one a working
// build reports.
type aStandIn struct {
	runs     []string
	duration map[string]string
	tags     map[string]map[string]string
}

func (s *aStandIn) run(argv []string, stdin []byte) ([]byte, error) {
	s.runs = append(s.runs, strings.Join(argv, " "))

	if isProbe(argv) {
		body, err := json.Marshal(probedRecording{Format: probedFormat{
			Duration: s.durationFor(argv[len(argv)-1]),
			Tags:     s.tagsFor(argv[len(argv)-1]),
		}})

		return body, err
	}

	return anM4A(), nil
}

func (s *aStandIn) durationFor(path string) string {
	if duration, ok := s.duration[filepath.Base(path)]; ok {
		return duration
	}

	// A working build pads the two levels that ask for it, and leaves the plain one the length of the
	// recording it was given.
	if strings.Contains(path, "-enabled.") {
		return "1.058667"
	}

	return "3.010667"
}

func (s *aStandIn) tagsFor(path string) map[string]string {
	if tags, ok := s.tags[filepath.Base(path)]; ok {
		return tags
	}

	return theRecordingTags
}

// asked returns the runs that named one of the programs the check was given.
func (s *aStandIn) asked(name string) []string {
	asked := []string{}

	for _, run := range s.runs {
		if strings.Contains(run, name) {
			asked = append(asked, run)
		}
	}

	return asked
}

func isProbe(argv []string) bool {
	for _, arg := range argv {
		if strings.Contains(arg, "ffprobe") {
			return true
		}
	}

	return false
}

// aCheckThatRefuses answers the conversions with one failure and the probes with nothing, which is
// what a trimmed build that left something out looks like.
func aCheckThatRefuses(reason string) command {
	return func(argv []string, stdin []byte) ([]byte, error) {
		if isProbe(argv) {
			return nil, nil
		}

		return nil, errors.New(reason)
	}
}

// aCheckThatWrites answers the conversions with what it is given and the probes with nothing.
func aCheckThatWrites(body []byte) command {
	return func(argv []string, stdin []byte) ([]byte, error) {
		if isProbe(argv) {
			return nil, nil
		}

		return body, nil
	}
}

func anFFMPEGCheck(t *testing.T, execute command) (checkReport, error) {
	t.Helper()

	return ffmpegCheck(execute, checkPrograms{
		FFmpeg:  []string{"podman", "run", "--rm", "-i", "rdio-ffmpeg", "/work/ffmpeg-9.0.2/ffmpeg"},
		FFProbe: []string{"podman", "run", "--rm", "rdio-ffmpeg", "/work/ffmpeg-9.0.2/ffprobe"},
	}, t.TempDir())
}

// The check is only worth having if it runs the argument list the server actually runs, so that list
// comes from the converter itself and the two cannot drift apart.
func TestTheFFMPEGCheckRunsTheCompiledFfmpegThroughTheConvertersOwnArgumentList(t *testing.T) {
	stand := &aStandIn{}

	if _, err := anFFMPEGCheck(t, stand.run); err != nil {
		t.Fatalf("the ffmpeg check refused a working ffmpeg: %v", err)
	}

	ran := stand.asked("/ffmpeg-9.0.2/ffmpeg")

	if len(ran) != 6 {
		t.Fatalf("the check ran the compiled ffmpeg %d times, want the three conversion levels from a WAV and from an MP3", len(ran))
	}

	for _, argv := range ran {
		for _, want := range []string{"-i -", "-c:a aac", "-b:a 32k", "-movflags frag_keyframe+empty_moov", "-f ipod -"} {
			if !strings.Contains(argv, want) {
				t.Errorf("the check ran %s, which does not carry %s", argv, want)
			}
		}

		if !strings.Contains(argv, "-metadata title=Call") {
			t.Errorf("the check ran %s without the tags a tagged talkgroup writes", argv)
		}
	}

	if !wasRun(ran, "apad=whole_dur=3s,loudnorm") {
		t.Error("the check never ran the recording the normalising level asks for")
	}

	if !wasRun(ran, "apad=whole_dur=3s,loudnorm=I=-16:TP=-1.5:LRA=11") {
		t.Error("the check never ran the recording the loudness-target level asks for")
	}
}

// A trimmed build that left out something the converter needs is exactly what this check exists to
// catch, and it has to stop the build rather than warn about it.
func TestTheFFMPEGCheckRefusesACompiledFfmpegThatCannotConvert(t *testing.T) {
	_, err := anFFMPEGCheck(t, aCheckThatRefuses("Error opening output files: Invalid argument"))
	if err == nil {
		t.Fatal("the ffmpeg check accepted an ffmpeg that refused to convert")
	}
}

func TestTheFFMPEGCheckRefusesACompiledFfmpegThatWritesNothing(t *testing.T) {
	if _, err := anFFMPEGCheck(t, aCheckThatWrites([]byte{})); err == nil {
		t.Fatal("the ffmpeg check accepted an ffmpeg that wrote an empty recording")
	}
}

func TestTheFFMPEGCheckRefusesAnOutputThatIsNotTheRecordingTheServerStores(t *testing.T) {
	if _, err := anFFMPEGCheck(t, aCheckThatWrites([]byte("RIFF....WAVEfmt "))); err == nil {
		t.Fatal("the ffmpeg check accepted output that is not an m4a")
	}
}

// The filters pad the recording to three seconds. A build whose normalised output is not that long is
// not the build the converter was written for.
func TestTheFFMPEGCheckConfirmsThePaddedRecordingIsTheLengthTheFiltersAskFor(t *testing.T) {
	stand := &aStandIn{duration: map[string]string{
		"wav-norm.m4a": "1.500000",
	}}

	if _, err := anFFMPEGCheck(t, stand.run); err == nil {
		t.Error("the ffmpeg check accepted a normalised recording that was not padded to three seconds")
	}
}

// The plain level does not pad, and a plain recording that came back three seconds long is a build
// that applied filters nobody asked for.
func TestTheFFMPEGCheckConfirmsThePlainRecordingIsNotPadded(t *testing.T) {
	stand := &aStandIn{duration: map[string]string{
		"wav-enabled.m4a": "3.010667",
	}}

	if _, err := anFFMPEGCheck(t, stand.run); err == nil {
		t.Error("the ffmpeg check accepted a plain recording that was padded when nothing asked for it")
	}
}

func TestTheFFMPEGCheckConfirmsTheTagsReachTheConvertedRecording(t *testing.T) {
	lost := map[string]string{"title": "Call", "artist": "Scanner"}

	stand := &aStandIn{tags: map[string]map[string]string{"wav-loudnorm.m4a": lost}}

	if _, err := anFFMPEGCheck(t, stand.run); err == nil {
		t.Error("the ffmpeg check accepted a converted recording that lost the tags the server writes")
	}
}

// The duration and the tags are read out of the converted recording with the bundled ffprobe, given a
// file rather than a pipe: ffprobe cannot get a duration from a stream it cannot seek.
func TestTheFFMPEGCheckProbesTheConvertedRecordingAsAFile(t *testing.T) {
	probed := []string{}

	execute := func(argv []string, stdin []byte) ([]byte, error) {
		if isProbe(argv) {
			probed = append(probed, strings.Join(argv, " "))

			duration := "3.010667"
			if strings.Contains(argv[len(argv)-1], "-enabled.") {
				duration = "1.058667"
			}

			body, err := json.Marshal(probedRecording{Format: probedFormat{
				Duration: duration,
				Tags:     theRecordingTags,
			}})

			return body, err
		}

		return anM4A(), nil
	}

	if _, err := anFFMPEGCheck(t, execute); err != nil {
		t.Fatalf("the ffmpeg check refused a working ffmpeg: %v", err)
	}

	if len(probed) != 6 {
		t.Fatalf("the check probed %d recordings, want one for each it converted", len(probed))
	}

	for _, line := range probed {
		argv := strings.Fields(line)
		path := argv[len(argv)-1]

		if !strings.HasSuffix(path, ".m4a") {
			t.Errorf("the check probed %s, which is not a recording it wrote out", line)
		}
	}
}

// The recordings the check feeds ffmpeg are the ones the dirwatch hands the converter: the same tone,
// as a WAV and as an MP3.
func TestTheFFMPEGCheckFeedsItTheRecordingsTheServerAccepts(t *testing.T) {
	inputs := checkInputs()

	if len(inputs) != 2 {
		t.Fatalf("the check feeds ffmpeg %d recordings, want a WAV and an MP3", len(inputs))
	}

	want := map[string]string{"wav": "audio/wav", "mp3": "audio/mpeg"}

	for _, in := range inputs {
		if len(in.Body) == 0 {
			t.Errorf("%s is empty", in.Name)
		}

		if want[in.Name] != in.Mime {
			t.Errorf("%s is %s, want %s", in.Name, in.Mime, want[in.Name])
		}
	}

	if _, err := os.Stat(filepath.Join("assets", "tone.mp3")); err != nil {
		t.Error("the check has no MP3 recording to feed ffmpeg")
	}
}

func wasRun(runs []string, want string) bool {
	for _, run := range runs {
		if strings.Contains(run, want) {
			return true
		}
	}

	return false
}
