package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestAudioConversionArgsUse64kAAC(t *testing.T) {
	args := audioConversionArgs(false, AUDIO_CONVERSION_DISABLED, nil)

	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-b:a" && args[i+1] == "64k" {
			return
		}
	}

	t.Fatalf("conversion arguments do not select 64k AAC: %v", args)
}

func TestFFMpegErrorClass(t *testing.T) {
	tests := map[string]string{
		"Output file does not contain any stream": "no_stream",
		"Invalid data found when processing input":  "invalid_input",
		"Error opening input file":                 "input_open",
		"Error opening output file -: Invalid argument": "output_open",
		"unexpected encoder failure":               "other",
	}

	for stderr, want := range tests {
		if got := ffmpegErrorClass(stderr); got != want {
			t.Errorf("ffmpegErrorClass(%q) = %q, want %q", stderr, got, want)
		}
	}
}

func TestFFMpegConvertRejectsMalformedInput(t *testing.T) {
	originalCommand := ffmpegCommand
	defer func() { ffmpegCommand = originalCommand }()
	ffmpegCommand = fakeFFMpegCommand("error")

	input := bytes.Repeat([]byte{0}, 128)
	call := NewCall()
	call.Audio = input
	call.AudioFilename = "malformed.ts"
	call.AudioMime = "video/mp2t"
	call.Talkgroup = &Talkgroup{}

	ffmpeg := &FFMpeg{available: true}
	err := ffmpeg.Convert(call, nil, &Tags{}, AUDIO_CONVERSION_ENABLED)

	if err == nil || !strings.Contains(err.Error(), "class=no_stream") {
		t.Fatalf("Convert error = %v, want no_stream classification", err)
	}
	if !strings.Contains(err.Error(), "exit_status=1") || !strings.Contains(err.Error(), "input_bytes=128") {
		t.Fatalf("Convert error = %v, want exit status and input size", err)
	}
	if !bytes.Equal(call.Audio, input) || call.AudioFilename != "malformed.ts" || call.AudioMime != "video/mp2t" {
		t.Fatalf("failed conversion mutated call audio metadata")
	}
}

func TestFFMpegConvertPreservesSuccessfulM4AHandling(t *testing.T) {
	originalCommand := ffmpegCommand
	defer func() { ffmpegCommand = originalCommand }()
	ffmpegCommand = fakeFFMpegCommand("success")

	call := NewCall()
	call.Audio = []byte("valid synthetic input")
	call.AudioFilename = "neighbor.mp3"
	call.AudioMime = "audio/mpeg"
	call.Talkgroup = &Talkgroup{}

	ffmpeg := &FFMpeg{available: true}
	if err := ffmpeg.Convert(call, nil, &Tags{}, AUDIO_CONVERSION_ENABLED); err != nil {
		t.Fatalf("Convert returned error for successful ffmpeg: %v", err)
	}
	if string(call.Audio) != "synthetic m4a" || call.AudioFilename != "neighbor.m4a" || call.AudioMime != "audio/mp4" {
		t.Fatalf("successful conversion produced unexpected call: audio=%q filename=%q mime=%q", call.Audio, call.AudioFilename, call.AudioMime)
	}
}

func fakeFFMpegCommand(mode string) func(string, ...string) *exec.Cmd {
	return func(_ string, _ ...string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=TestFFMpegHelperProcess", "--")
		cmd.Env = append(os.Environ(), "GO_WANT_FFMPEG_HELPER=1", "GO_FFMPEG_HELPER_MODE="+mode)
		return cmd
	}
}

func TestFFMpegHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_FFMPEG_HELPER") != "1" {
		return
	}

	if os.Getenv("GO_FFMPEG_HELPER_MODE") == "error" {
		fmt.Fprintln(os.Stderr, "Output file does not contain any stream")
		os.Exit(1)
	}

	fmt.Fprint(os.Stdout, "synthetic m4a")
}
