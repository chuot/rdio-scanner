package main

import (
	"strings"
	"testing"
)

func TestCallIngestDiagnosticsExcludeAudio(t *testing.T) {
	call := NewCall()
	call.ingestRoute = "api/call-upload"
	call.Audio = []byte("synthetic audio that must not be logged")
	call.AudioMime = "video/mp2t"

	diagnostics := call.ingestDiagnostics()
	for _, want := range []string{"ingest_id=", "route=api/call-upload", "input_bytes=39", "audio_type=video/mp2t"} {
		if !strings.Contains(diagnostics, want) {
			t.Errorf("diagnostics %q does not contain %q", diagnostics, want)
		}
	}
	if strings.Contains(diagnostics, "synthetic audio") {
		t.Fatalf("diagnostics included audio content")
	}
}
