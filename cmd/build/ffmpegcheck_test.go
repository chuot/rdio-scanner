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

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Component tests of the ffmpeg check: the commands are answered by a stand-in, so
// the check itself is what these tests exercise.

// anM4A is a converted recording whose first boxes say it is what the server stores.
func anM4A() []byte {
	return []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'M', '4', 'A', ' ', 0, 0, 0, 0}
}

// aStandIn answers the ffmpeg check's commands the way a working ffmpeg does, and
// keeps what it was asked to run. Any duration or tags it does not override are
// the ones a working build reports.
type aStandIn struct {
	runs     []string
	duration map[string]string
	tags     map[string]map[string]string

	// codecs are the registry lines the program answers its codec probe with. The
	// default is a working build: the converter's audio codecs, no video codec.
	codecs []string
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

	if namesTheCodecProbe(argv) {
		lines := s.codecs
		if lines == nil {
			lines = theAudioCodecLines
		}

		return aCodecRegistry(lines...), nil
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

// aCheckThatRefuses answers the conversions with one failure and the probes with
// nothing, which is what a trimmed build that left something out looks like.
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

// the check runs the converter's own argument list, taken from the converter itself
// so the two cannot drift apart.
func TestTheFFMPEGCheckRunsTheCompiledFfmpegThroughTheConvertersOwnArgumentList(t *testing.T) {
	stand := &aStandIn{}

	if _, err := anFFMPEGCheck(t, stand.run); err != nil {
		t.Fatalf("the ffmpeg check refused a working ffmpeg: %v", err)
	}

	ran := stand.asked("/ffmpeg-9.0.2/ffmpeg")

	// The check runs the compiled program once for each recording at each level, and
	// one more time to ask what the program registers.
	conversions, registry := splitTheCodecProbe(ran)

	if want := len(theCheckFixtures) * len(theConversionLevels); len(conversions) != want {
		t.Fatalf(
			"the check ran the compiled ffmpeg %d times, want the %d conversion levels from each of the %d recordings it names",
			len(conversions), len(theConversionLevels), len(theCheckFixtures),
		)
	}

	if len(registry) != 1 {
		t.Fatalf("the check asked the compiled ffmpeg what it registers %d times, want once", len(registry))
	}

	for _, argv := range conversions {
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

	// the codec probe asks only for the program's own list, quietly, read the way the
	// operator reads it.
	if !wasRun(registry, "-hide_banner -codecs") {
		t.Errorf("the check asked what the program registers with %s", strings.Join(registry, " and "))
	}
}

// splitTheCodecProbe takes one program's runs apart into the conversions and the
// codec probe.
func splitTheCodecProbe(runs []string) (conversions []string, registry []string) {
	for _, run := range runs {
		if strings.Contains(run, "-codecs") {
			registry = append(registry, run)
			continue
		}

		conversions = append(conversions, run)
	}

	return conversions, registry
}

// a trimmed build missing something the converter needs stops the build instead of
// warning about it.
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

// the padded conversion comes back the three seconds the filters ask for, the length
// the converter was written for.
func TestTheFFMPEGCheckConfirmsThePaddedRecordingIsTheLengthTheFiltersAskFor(t *testing.T) {
	stand := &aStandIn{duration: map[string]string{
		"wav-norm.m4a": "1.500000",
	}}

	if _, err := anFFMPEGCheck(t, stand.run); err == nil {
		t.Error("the ffmpeg check accepted a normalised recording that was not padded to three seconds")
	}
}

// the plain level does not pad: a plain recording back at three seconds means
// filters nobody asked for ran.
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

// the check probes the converted recording as a file, because ffprobe gets no
// duration from an unseekable stream.
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

		if namesTheCodecProbe(argv) {
			return aCodecRegistry(theAudioCodecLines...), nil
		}

		return anM4A(), nil
	}

	if _, err := anFFMPEGCheck(t, execute); err != nil {
		t.Fatalf("the ffmpeg check refused a working ffmpeg: %v", err)
	}

	if want := len(theCheckFixtures) * len(theConversionLevels); len(probed) != want {
		t.Fatalf("the check probed %d recordings, want one for each of the %d conversions it runs", len(probed), want)
	}

	for _, line := range probed {
		argv := strings.Fields(line)
		path := argv[len(argv)-1]

		if !strings.HasSuffix(path, ".m4a") {
			t.Errorf("the check probed %s, which is not a recording it wrote out", line)
		}
	}
}

// aCheckFixture is one recording the gate feeds the compiled ffmpeg. `Mime` is the label
// the dirwatch gives a file of that extension — `mime.TypeByExtension`, the same call the
// watch makes, so an extension the standard library does not name labels the recording
// with nothing. `At` is where in the file its marker sits, because a box-based container
// names its type after the size of its first box, and each Marker is what this machine
// read off the file it authored. A fixture with no file is written by the check itself.
type aCheckFixture struct {
	Name   string
	File   string
	Mime   string
	At     int
	Marker []byte
}

// theCheckFixtures are the recordings the gate feeds the compiled ffmpeg: one for each
// container the recipe's decoders and demuxers open, each one second of the tone the gate
// already generates, so the only thing a fixture varies is the container and the codec it
// carries.
var theCheckFixtures = []aCheckFixture{
	{Name: "wav", Mime: "audio/wav", Marker: []byte("RIFF")},
	{Name: "mp3", File: "tone.mp3", Mime: "audio/mpeg", Marker: []byte("ID3")},
	{Name: "m4a", File: "tone.m4a", Mime: "audio/mp4", At: 4, Marker: []byte("ftyp")},
	{Name: "m4a-fragmented", File: "tone-m4a-frag.m4a", Mime: "audio/mp4", At: 4, Marker: []byte("ftyp")},
	{Name: "adts", File: "tone.aac", Mime: "audio/aac", Marker: []byte{0xff, 0xf1}},
	{Name: "mp2", File: "tone.mp2", Mime: "audio/mp2", Marker: []byte{0xff, 0xf5}},
	{Name: "wav-adpcm", File: "tone-ima.wav", Mime: "audio/wav", Marker: []byte("RIFF")},
	{Name: "flac", File: "tone.flac", Mime: "audio/flac", Marker: []byte("fLaC")},
	{Name: "ogg-vorbis", File: "tone-vorbis.ogg", Mime: "audio/ogg", Marker: []byte("OggS")},
	{Name: "webm-opus", File: "tone-opus.webm", Mime: "audio/webm", Marker: []byte{0x1a, 0x45, 0xdf, 0xa3}},
	{Name: "wma", File: "tone.wma", Mime: "audio/x-ms-wma", Marker: []byte{0x30, 0x26, 0xb2, 0x75}},
	{Name: "aiff", File: "tone.aiff", Mime: "audio/x-aiff", Marker: []byte("FORM")},
	{Name: "voc", File: "tone.voc", Mime: "audio/x-voc", Marker: []byte("Creative Voice")},
	{Name: "w64", File: "tone.w64", Mime: "", Marker: theW64Marker},
	{Name: "mov", File: "tone.mov", Mime: "video/quicktime", At: 4, Marker: []byte("ftyp")},
	{Name: "mp4-with-video", File: "tone-video.mp4", Mime: "video/mp4", At: 4, Marker: []byte("ftyp")},
}

// theW64Marker is the whole of a W64 file's first box header: the four bytes a RIFF wav
// fills with "RIFF", followed by the GUID that says this one is sixty-four bit.
var theW64Marker = []byte{
	'r', 'i', 'f', 'f',
	0x2e, 0x91, 0xcf, 0x11, 0xa5, 0xd6, 0x28, 0xdb, 0x04, 0xc1, 0x00, 0x00,
}

// aCheckFixtureNamed finds the named fixture in the table above.
func aCheckFixtureNamed(name string) (aCheckFixture, bool) {
	for _, fixture := range theCheckFixtures {
		if fixture.Name == name {
			return fixture, true
		}
	}

	return aCheckFixture{}, false
}

// the check feeds ffmpeg one recording for each container the recipe opens: the same
// tone in a wav, an mp3, the m4a pair, raw ADTS, mp2, an ADPCM wav, Flac, Ogg Vorbis,
// WebM Opus, an ASF WMA, an AIFF, a Creative Voice file, a W64, a QuickTime mov, and an
// mp4 that carries a video stream beside its audio.
func TestTheFFMPEGCheckFeedsItTheRecordingsTheServerAccepts(t *testing.T) {
	inputs := checkInputs()

	if len(inputs) != len(theCheckFixtures) {
		t.Fatalf(
			"the check feeds ffmpeg %d recordings, want one for each container the recipe opens, which is %d",
			len(inputs), len(theCheckFixtures),
		)
	}

	fed := map[string]bool{}

	for _, in := range inputs {
		fixture, ok := aCheckFixtureNamed(in.Name)
		if !ok {
			t.Errorf("the check feeds ffmpeg a %s recording, which is not a container the gate names", in.Name)

			continue
		}

		fed[in.Name] = true

		if len(in.Body) <= fixture.At {
			t.Errorf(
				"the check feeds ffmpeg a %s recording that is %d bytes, too short to carry the %s marker",
				in.Name, len(in.Body), fixture.Name,
			)

			continue
		}

		head := in.Body[fixture.At:]

		if !bytes.HasPrefix(head, fixture.Marker) {
			t.Errorf(
				"the check feeds ffmpeg a %s recording carrying % x at byte %d, want the marker % x",
				in.Name, head[:min(len(head), len(fixture.Marker))], fixture.At, fixture.Marker,
			)
		}

		if in.Mime != fixture.Mime {
			t.Errorf("%s is labelled %q, want the label the watch gives that extension, %q", in.Name, in.Mime, fixture.Mime)
		}
	}

	for _, fixture := range theCheckFixtures {
		if !fed[fixture.Name] {
			t.Errorf("the check does not feed ffmpeg a %s recording", fixture.Name)

			continue
		}

		if fixture.File == "" {
			continue
		}

		body, err := os.ReadFile(filepath.Join("assets", fixture.File))
		if err != nil {
			t.Errorf("the gate has no %s to feed ffmpeg: %v", fixture.File, err)

			continue
		}

		if len(body) <= fixture.At {
			t.Errorf("%s is %d bytes, which is not far enough into it to carry its marker", fixture.File, len(body))

			continue
		}

		head := body[fixture.At:]

		if !bytes.HasPrefix(head, fixture.Marker) {
			t.Errorf(
				"%s carries % x at byte %d, want the %s marker % x: the recording this machine authored is the file the gate must keep",
				fixture.File, head[:min(len(head), len(fixture.Marker))], fixture.At, fixture.Name, fixture.Marker,
			)
		}
	}
}

// theAudioCodecLines are a working build's answer to its codec probe: the PCM and
// MP3 decoders the converter demuxes and decodes, the AAC encoder it writes with,
// and a codec descriptor. Every ffmpeg carries descriptors, and no video decoder or
// encoder in the list still means an audio-only program.
var theAudioCodecLines = []string{
	"D.A..S pcm_s16le            PCM signed 16-bit little-endian",
	"D.A..S pcm_s16be            PCM signed 16-bit big-endian",
	"D.A..S pcm_s24le            PCM signed 24-bit little-endian",
	"D.A..S pcm_f32le            PCM 32-bit floating point little-endian",
	"D.A..S pcm_mulaw            PCM mu-law",
	"D.A..S pcm_alaw             PCM A-law",
	"D.A.L. mp3float             MP3 (MPEG audio layer 3)",
	"D.A.L. mp3                  MP3 (MPEG audio layer 3)",
	".EA.L. aac                  AAC (Advanced Audio Coding)",
	" ..VI.S 012v                Uncompressed 4:2:2 10-bit",
}

// theAudioCodecNames are the codecs the lines above register, in the order the program names them.
var theAudioCodecNames = []string{
	"pcm_s16le", "pcm_s16be", "pcm_s24le", "pcm_f32le", "pcm_mulaw", "pcm_alaw",
	"mp3float", "mp3", "aac",
}

// aCodecRegistry is ffmpeg's answer to `-codecs`: the legend, the separator, and the
// lines it is given, pinned to what the release the build uses prints.
func aCodecRegistry(lines ...string) []byte {
	body := "Codecs:\n" +
		" D..... = Decoding supported\n" +
		" .E.... = Encoding supported\n" +
		" ..V... = Video codec\n" +
		" ..A... = Audio codec\n" +
		" ..S... = Subtitle codec\n" +
		" ..D... = Data codec\n" +
		" ..T... = Attachment codec\n" +
		" ...I.. = Intra frame-only codec\n" +
		" ....L. = Lossy compression\n" +
		" .....S = Lossless compression\n" +
		" -------\n"

	for _, line := range lines {
		body += line + "\n"
	}

	return []byte(body)
}

// namesTheCodecProbe reports whether a command asks the program what it registers.
func namesTheCodecProbe(argv []string) bool {
	for _, arg := range argv {
		if arg == "-codecs" {
			return true
		}
	}

	return false
}

// the shipped ffmpeg is compiled for audio: the recipe disables everything and
// enables audio components only, so a program registering a video codec is refused.
func TestTheFFMPEGCheckRefusesAFfmpegThatRegistersVideoCodecs(t *testing.T) {
	stand := &aStandIn{codecs: []string{
		"D.A.L. mp3                  MP3 (MPEG audio layer 3)",
		"DEVIL. h264                 H.264 / AVC / MPEG-4 AVC / MPEG-4 part 10",
		"DEVIL. mpeg4                MPEG-4 part 2 (MPEG4) / ISO 14496-2",
	}}

	_, err := anFFMPEGCheck(t, stand.run)
	if err == nil {
		t.Fatal("the ffmpeg check accepted a program that registers video codecs")
	}

	for _, want := range []string{"h264", "mpeg4"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure %q does not name the video codec %s it found", err.Error(), want)
		}
	}

	// the codec probe is asked of the compiled program inside the container, so the
	// refusal is about the binaries the build ships, not the build host.
	if !wasRun(stand.asked("/work/ffmpeg-9.0.2/ffmpeg"), "-codecs") {
		t.Error("the check asked the machine it stands on what it registers, and not the compiled binaries")
	}
}

// the check records the codecs the program registers, so the report says what the
// binaries are, not only what they converted; descriptors that convert nothing do
// not count as registered codecs.
func TestTheFFMPEGCheckRecordsTheCodecsTheProgramRegisters(t *testing.T) {
	report, err := anFFMPEGCheck(t, (&aStandIn{}).run)
	if err != nil {
		t.Fatalf("the ffmpeg check refused a working ffmpeg: %v", err)
	}

	if len(report.Codecs.Audio) != len(theAudioCodecNames) {
		t.Errorf("the check recorded %v as the codecs the program registers, want %v", report.Codecs.Audio, theAudioCodecNames)
	}

	for _, want := range theAudioCodecNames {
		if !aCodecNamed(report.Codecs.Audio, want) {
			t.Errorf("the check does not record the %s the converter decodes and encodes with", want)
		}
	}

	if len(report.Codecs.Video) != 0 {
		t.Errorf("the check recorded video codecs %v for a program that registers none", report.Codecs.Video)
	}
}

func aCodecNamed(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}

	return false
}

func wasRun(runs []string, want string) bool {
	for _, run := range runs {
		if strings.Contains(run, want) {
			return true
		}
	}

	return false
}
