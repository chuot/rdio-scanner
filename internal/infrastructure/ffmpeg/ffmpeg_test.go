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

package ffmpeg

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// recordedRecording is one second of a 440 Hz tone in the wav the recording
// tools write: a tone rather than silence, because the loudness filters
// measure whatever they are handed.
func recordedRecording(t *testing.T) []byte {
	t.Helper()

	const (
		samples   = 8000
		rate      = 8000
		amplitude = 12000
	)

	body := make([]byte, samples*2)

	for i := 0; i < samples; i++ {
		sample := int16(amplitude * math.Sin(2*math.Pi*440*float64(i)/float64(rate)))
		binary.LittleEndian.PutUint16(body[i*2:], uint16(sample))
	}

	wav := bytes.NewBuffer([]byte(nil))

	wav.WriteString("RIFF")
	writeUint32(wav, uint32(36+len(body)))
	wav.WriteString("WAVE")

	wav.WriteString("fmt ")
	writeUint32(wav, 16)
	writeUint16(wav, 1) // PCM
	writeUint16(wav, 1) // one channel
	writeUint32(wav, rate)
	writeUint32(wav, rate*2)
	writeUint16(wav, 2) // one sample frame
	writeUint16(wav, 16)

	wav.WriteString("data")
	writeUint32(wav, uint32(len(body)))
	wav.Write(body)

	return wav.Bytes()
}

func writeUint32(out *bytes.Buffer, v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	out.Write(b[:])
}

func writeUint16(out *bytes.Buffer, v uint16) {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)
	out.Write(b[:])
}

// installedConverter is the converter a deployment with ffmpeg gets, built
// over the real binary.

// TestAMissingConverterIsReportedOncePerServerRun is what a deployment
// without ffmpeg sees: one warning line, and then every recording stored as
// it arrived.
func TestAMissingConverterIsReportedOncePerServerRun(t *testing.T) {
	converter := &Converter{}

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	converted, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)

	if err == nil || !strings.Contains(err.Error(), "ffmpeg is not available") {
		t.Fatalf("Convert = %v, want the reason no conversion happened", err)
	}

	if !bytes.Equal(converted.Data, in.Data) || converted.Filename != "call.wav" {
		t.Errorf("the recording is presented as %q, want the original kept when nothing converted it", converted.Filename)
	}

	converted, err = converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)

	if err != nil {
		t.Errorf("the second Convert = %v, want the silence the legacy keeps after the one warning it writes", err)
	}

	if !bytes.Equal(converted.Data, in.Data) {
		t.Errorf("the recording was changed by a converter that is not installed")
	}
}

// TestTheDisabledLevelIsSilentEvenWithoutAConverter pins the order the legacy checks in: a
// deployment that has turned conversion off never hears about the missing converter at all.

// TestTheDisabledLevelIsSilentEvenWithoutAConverter pins the order the legacy checks in: a
// deployment that has turned conversion off never hears about the missing converter at all.
func TestTheDisabledLevelIsSilentEvenWithoutAConverter(t *testing.T) {
	converter := &Converter{}

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	if _, err := converter.Convert(context.Background(), in, domain.AudioConversionDisabled, nil); err != nil {
		t.Errorf("Convert = %v, want nothing said about a converter this deployment does not need", err)
	}
}

// TestTheLoudnormProbeReadsTheFilterListTheCapturedReleasesPrint: the
// loudness filters are decided by the filter list the program itself prints,
// and the fixtures below are the real filter lists of the releases named,
// in the flag-column shape each release prints.
func TestTheLoudnormProbeReadsTheFilterListTheCapturedReleasesPrint(t *testing.T) {
	cases := []struct {
		name    string
		filters string
		want    bool
	}{
		{name: "the 4.3 release that introduced the filter", filters: filters43, want: true},
		{name: "the 4.10 release the version string misread", filters: filters410, want: true},
		{name: "the 10.0 release the version string misread", filters: filters100, want: true},
		{name: "a tagged N-build", filters: filtersN7, want: true},
		{name: "a git build", filters: filtersGit, want: true},
		{name: "the release before the filter existed", filters: filters32, want: false},
		{name: "a probe that printed nothing", filters: "", want: false},
		{name: "a probe that printed noise", filters: "Abnormal exit at startup\nsee /usr/share/doc/ffmpeg\n", want: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasLoudnorm(c.filters); got != c.want {
				t.Errorf("hasLoudnorm of the %s filter list = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

// The captured `ffmpeg -hide_banner -filters` outputs, one per release
// named.

const filters32 = `Filters:
  T.. = Timeline support
  .S. = Slice threading
  ..C = Command support
  A = Audio input/output
  V = Video input/output
  N = Dynamic number and/or type of input/output
  | = Source or sink filter
 T.. abuffer            0->1-...     Buffer audio source
 .. anexamplifier      1->1         Amplify audio with a certain factor.
 .. volume             1->1         Gain control for audio.
`

const filters43 = `Filters:
  T.. = Timeline support
  .S. = Slice threading
  ..C = Command support
  A = Audio input/output
  V = Video input/output
  N = Dynamic number and/or type of input/output
  | = Source or sink filter
 T.. abuffer            0->1-...     Buffer audio source
 .. anexamplifier      1->1         Amplify audio with a certain factor.
 .. loudnorm           1->1         EBU R128 loudness normalization
 .. volume             1->1         Gain control for audio.
`

const filters410 = `Filters:
  T.. = Timeline support
  .S. = Slice threading
  ..C = Command support
  A = Audio input/output
  V = Video input/output
  N = Dynamic number and/or type of input/output
  | = Source or sink filter
 T.. abuffer            0->1-...     Buffer audio source
 .. loudnorm           1->1         EBU R128 loudness normalization
 .. volume             1->1         Gain control for audio.
`

const filters100 = `Filters:
  T.. = Timeline support
  .S. = Slice threading
  A = Audio input/output
  V = Video input/output
  N = Dynamic number and/or type of input/output
  | = Source or sink filter
 TS aap               AA->A      Apply Affine Projection algorithm to first audio stream.
 .. loudnorm          A->A       EBU R128 loudness normalization
 .. volume            1->1       Gain control for audio.
`

const filtersN7 = `Filters:
  T.. = Timeline support
  .S. = Slice threading
  A = Audio input/output
  V = Video input/output
  N = Dynamic number and/or type of input/output
  | = Source or sink filter
 .. loudnorm          A->A       EBU R128 loudness normalization
 .. volume            1->1       Gain control for audio.
`

const filtersGit = `Filters:
  T.. = Timeline support
  .S. = Slice threading
  A = Audio input/output
  V = Video input/output
  N = Dynamic number and/or type of input/output
  | = Source or sink filter
 .. loudnorm          A->A       EBU R128 loudness normalization
 .. volume            1->1       Gain control for audio.
`

// The filter list a program the tests are given answers its probe with: the
// loudnorm line in the form the releases from 4.3 on print it.
const filtersWithLoudnorm = filters43

// The filter list without it, in the 3.x form.
const filtersWithoutLoudnorm = filters32

// aProgramIsGiven writes a program the test controls and returns its path:
// it answers the filter probe with the filter list it was given and converts
// by saying who it is.
func aProgramIsGiven(t *testing.T, filters string) string {
	t.Helper()

	dir := t.TempDir()

	path := filepath.Join(dir, "ffmpeg")
	filtersFile := filepath.Join(dir, "filters")

	if err := os.WriteFile(filtersFile, []byte(filters), 0644); err != nil {
		t.Fatalf("the test cannot write the filter list it was given: %v", err)
	}

	script := "#!/bin/sh\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$arg\" = \"-filters\" ]; then\n" +
		"    cat \"" + filtersFile + "\"\n" +
		"    exit 0\n" +
		"  fi\n" +
		"done\n" +
		"cat > /dev/null\n" +
		"printf 'converted by the program the server was given'\n"

	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("the test cannot write the program it was given: %v", err)
	}

	return path
}

func TestTheConverterRunsTheProgramItWasGiven(t *testing.T) {
	converter := NewAt(aProgramIsGiven(t, filtersWithLoudnorm))

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	converted, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)
	if err != nil {
		t.Fatalf("the converter refused a recording: %v", err)
	}

	if string(converted.Data) != "converted by the program the server was given" {
		t.Errorf("the recording was converted by something other than the program the converter was given: %q", string(converted.Data))
	}
}

// A server whose bundled program could not be written out, or whose data directory will not hold an
// executable, is a server with no converter: the recording is kept and the reason is said once.
func TestTheConverterReportsAProgramItWasGivenThatIsNotThere(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "ffmpeg-9.0.2")

	converter := NewAt(missing)

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	converted, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)
	if err == nil {
		t.Fatal("a converter given a program that is not there reported no failure")
	}

	if string(converted.Data) != string(in.Data) {
		t.Error("the recording was changed even though there was no program to convert it")
	}

	if _, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil); err != nil {
		t.Errorf("the missing converter was reported a second time: %v", err)
	}
}

// The filter list the given program prints decides whether the loudness
// filters are used.
func TestTheConverterAsksTheProgramItWasGivenWhichFiltersItHas(t *testing.T) {
	without := NewAt(aProgramIsGiven(t, filtersWithoutLoudnorm))

	if without.loudnorm {
		t.Error("a program whose filter list names no loudnorm was taken for one that has the loudness filters")
	}

	with := NewAt(aProgramIsGiven(t, filtersWithLoudnorm))

	if !with.loudnorm {
		t.Error("a program whose filter list names loudnorm was taken for one that does not have the loudness filters")
	}
}

// aProgramThatFailsWith writes a program that answers the filter probe with
// a filter list and fails with the given transcript on stderr.
func aProgramThatFailsWith(t *testing.T, transcript string) string {
	t.Helper()

	dir := t.TempDir()

	path := filepath.Join(dir, "ffmpeg")
	filtersFile := filepath.Join(dir, "filters")

	if err := os.WriteFile(filtersFile, []byte(filtersWithLoudnorm), 0644); err != nil {
		t.Fatalf("the test cannot write the filter list it was given: %v", err)
	}

	script := "#!/bin/sh\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$arg\" = \"-filters\" ]; then\n" +
		"    cat \"" + filtersFile + "\"\n" +
		"    exit 0\n" +
		"  fi\n" +
		"done\n" +
		"cat > /dev/null\n" +
		"printf '" + transcript + "' >&2\n" +
		"exit 1\n"

	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("the test cannot write the program it was given: %v", err)
	}

	return path
}

// TestAProgramWhoseFilterProbeFailsIsReportedAsNoConverterOnce: the filter
// probe is the converter's availability probe, and a program that cannot
// answer it is a converter this server does not have.
func TestAProgramWhoseFilterProbeFailsIsReportedAsNoConverterOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ffmpeg")

	script := "#!/bin/sh\n" +
		"printf 'ffmpeg: the filter probe exploded\n' >&2\n" +
		"exit 1\n"

	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("the test cannot write the program it was given: %v", err)
	}

	converter := NewAt(path)

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	converted, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)
	if err == nil {
		t.Fatal("a program whose filter probe failed reported no failure")
	}

	if !strings.Contains(err.Error(), "ffmpeg is not available") {
		t.Errorf("the failure = %q, want the missing-converter report: a converter that cannot say what filters it has is a converter this server does not have", err)
	}

	if string(converted.Data) != string(in.Data) {
		t.Error("the recording was changed even though the probe said there is no converter")
	}

	if _, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil); err != nil {
		t.Errorf("the failed probe was reported a second time: %v", err)
	}
}

// TestTheConverterReportsAFailingProgramShortAndQuiet: ffmpeg's stderr on a
// failure is written into the server's log line, and the report keeps the
// first 512 bytes and nothing that can forge a log entry.
func TestTheConverterReportsAFailingProgramShortAndQuiet(t *testing.T) {
	flood := strings.Repeat("x", 2000) + "\\nsecond\\nline\\t\\033[31mred\\033[0m\\n"

	converter := NewAt(aProgramThatFailsWith(t, flood))

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	_, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)
	if err == nil {
		t.Fatal("a failing conversion program reported no failure")
	}

	message := err.Error()

	if strings.ContainsAny(message, "\n\r\t\x1b") {
		t.Errorf("the failure report = %q, want no newlines, tabs, or escapes: it is one log line", message)
	}

	if len(message) > len("ffmpeg: ")+512 {
		t.Errorf("the failure report = %d bytes, want at most 512 after the prefix: %q",
			len(message), message[:min(len(message), 80)])
	}
}

// aProgramThatConvertsWithProgress writes a program that converts by
// echoing a fixed body and says the given progress lines on its standard
// error.
func aProgramThatConvertsWithProgress(t *testing.T, progress string) string {
	t.Helper()

	dir := t.TempDir()

	path := filepath.Join(dir, "ffmpeg")
	filtersFile := filepath.Join(dir, "filters")
	progressFile := filepath.Join(dir, "progress")

	if err := os.WriteFile(filtersFile, []byte(filtersWithLoudnorm), 0644); err != nil {
		t.Fatalf("the test cannot write the filter list: %v", err)
	}

	if err := os.WriteFile(progressFile, []byte(progress), 0644); err != nil {
		t.Fatalf("the test cannot write the progress lines: %v", err)
	}

	script := "#!/bin/sh\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$arg\" = \"-filters\" ]; then\n" +
		"    cat \"" + filtersFile + "\"\n" +
		"    exit 0\n" +
		"  fi\n" +
		"done\n" +
		"cat > /dev/null\n" +
		"cat \"" + progressFile + "\" >&2\n" +
		"printf 'the converted recording'\n"

	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("the test cannot write the program: %v", err)
	}

	return path
}

// The progress lines ffmpeg writes while it converts name the running time
// of the recording it is writing; the last one is the length of the whole
// file, read off the output the converter already captures.
func TestTheConverterReportsTheLengthTheProgramStatedLast(t *testing.T) {
	converter := NewAt(aProgramThatConvertsWithProgress(t,
		"size=       1kB time=00:00:01.02 bitrate=   6.7kbits/s speed=0.31x\n"+
			"size=       4kB time=00:00:03.01 bitrate=  10.8kbits/s speed=0.42x\n"+
			"video:0kB audio:3kB subtitle:0kB other streams:0kB global headers:0kB muxing overhead: 33.021059%\n",
	))

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	converted, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)
	if err != nil {
		t.Fatalf("the converter refused a recording: %v", err)
	}

	if converted.Duration != 3010*time.Millisecond {
		t.Errorf("the converted recording is %v long, want the 3.01 seconds the program stated last", converted.Duration)
	}
}

// A program that says no running time is a conversion that names no
// length, and the converter returns no length rather than guessing one.
func TestTheConverterReportsNoLengthWhenTheProgramStatedNone(t *testing.T) {
	converter := NewAt(aProgramThatConvertsWithProgress(t,
		"video:0kB audio:3kB subtitle:0kB other streams:0kB muxing overhead: 33.0%\n",
	))

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	converted, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)
	if err != nil {
		t.Fatalf("the converter refused a recording: %v", err)
	}

	if converted.Duration != 0 {
		t.Errorf("the converted recording is %v long, want no length from output naming none", converted.Duration)
	}
}

// The running time is read from the progress of a conversion that ran: a
// failed conversion's transcript must not become a length for audio the
// converter did not produce.
func TestAFailedConversionReportsNoLength(t *testing.T) {
	converter := NewAt(aProgramThatFailsWith(t,
		"size=       4kB time=00:00:03.01 bitrate=  10.8kbits/s speed=0.42x\\nError opening output files: Invalid argument\\n",
	))

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	converted, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)
	if err == nil {
		t.Fatal("a failing conversion program reported no failure")
	}

	if converted.Duration != 0 {
		t.Errorf("the kept recording is %v long, want no length: the converter produced nothing here", converted.Duration)
	}
}

// The status view reports whether this deployment converts audio at all.
func TestTheConverterReportsWhetherItsProgramRanItsProbe(t *testing.T) {
	if available := NewAt(aProgramIsGiven(t, filtersWithLoudnorm)).Available(); !available {
		t.Error("Available() = false for a program that answered the filter probe, want true: this deployment converts")
	}

	missing := NewAt(filepath.Join(t.TempDir(), "ffmpeg-9.0.2"))

	if available := missing.Available(); available {
		t.Error("Available() = true for a program that is not there, want false: no audio conversion will be performed")
	}
}
