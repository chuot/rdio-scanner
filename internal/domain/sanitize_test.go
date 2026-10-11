package domain

import (
	"strings"
	"testing"
	"time"
)

// TestSanitizeTextReplacesControlCharacters: text an upload supplies
// becomes a log line, a label, and a broadcast. A newline in it is a second forged log line,
// a tab is a forged column, and an escape is a command to the terminal reading the log. Every
// one of them becomes a space; the visible text survives.
func TestSanitizeTextReplacesControlCharacters(t *testing.T) {
	got := SanitizeText("Grand\rMetro\nDispatch\tOne\x1b[31mred\x1b[0m", 256)

	if strings.ContainsAny(got, "\r\n\t\x1b") {
		t.Errorf("SanitizeText = %q, want no control characters left", got)
	}

	if !strings.Contains(got, "Grand") || !strings.Contains(got, "Metro") || !strings.Contains(got, "Dispatch") {
		t.Errorf("SanitizeText = %q, want the visible text kept", got)
	}
}

// The cap is what a label can carry: a posted string of a megabyte is not a label.
func TestSanitizeTextCapsAtTheGivenLength(t *testing.T) {
	got := SanitizeText(strings.Repeat("x", 1000), 256)

	if len(got) != 256 {
		t.Errorf("SanitizeText = %d characters, want the cap at 256", len(got))
	}
}

// A limit of zero says "no cap" — the ffmpeg report is bounded by its own rule, not by the
// label cap.
func TestSanitizeTextWithoutALimitKeepsTheWholeText(t *testing.T) {
	long := strings.Repeat("x", 1000)

	if got := SanitizeText(long, 0); got != long {
		t.Errorf("SanitizeText with no limit = %d characters, want all 1000 kept", len(got))
	}
}

// TestCallSanitizedMakesEverySuppliedTextSafe: the call carries every text an outside source
// supplied — the recording's name, the labels the call claims, the unit labels. Sanitizing
// the call is the one door all of it passes through on its way to logs, labels and
// broadcasts, and it leaves the original call alone.
func TestCallSanitizedMakesEverySuppliedTextSafe(t *testing.T) {
	call := NewCall()
	call.AudioFilename = "a.wav\nforged line"
	call.Meta.SystemLabel = "Metro\tOne"
	call.Meta.TalkgroupLabel = "Fire\x1b[1munit"
	call.Meta.TalkgroupName = "name\r\n"
	call.Meta.TalkgroupTag = "tag\n"
	call.Meta.TalkgroupGroup = []string{"g1\ng2"}
	call.Meta.UnitLabels = []string{"unit\x1b[31m"}

	sanitized := call.Sanitized()

	for name, text := range map[string]string{
		"audio filename":  sanitized.AudioFilename,
		"system label":    sanitized.Meta.SystemLabel,
		"talkgroup label": sanitized.Meta.TalkgroupLabel,
		"talkgroup name":  sanitized.Meta.TalkgroupName,
		"talkgroup tag":   sanitized.Meta.TalkgroupTag,
	} {
		if strings.ContainsAny(text, "\r\n\t\x1b") {
			t.Errorf("%s = %q, want no control characters", name, text)
		}
	}

	for _, group := range sanitized.Meta.TalkgroupGroup {
		if strings.ContainsAny(group, "\r\n\t\x1b") {
			t.Errorf("group label = %q, want no control characters", group)
		}
	}

	for _, label := range sanitized.Meta.UnitLabels {
		if strings.ContainsAny(label, "\r\n\t\x1b") {
			t.Errorf("unit label = %q, want no control characters", label)
		}
	}

	if call.AudioFilename != "a.wav\nforged line" {
		t.Error("Sanitized must leave the call it was given alone")
	}
}

// The length a source supplied is a number from outside the server: a negative duration is not
// a length, and the one door the outside values pass through clamps it to the unknown the
// server uses for "never measured".
func TestCallSanitizedClampsASuppliedNegativeAudioLength(t *testing.T) {
	call := NewCall()
	call.AudioLength = -2500 * time.Millisecond

	if got := call.Sanitized().AudioLength; got != 0 {
		t.Errorf("AudioLength = %v, want a negative length from outside clamped to no length", got)
	}
}

// The clamp takes the negative and leaves the length the server itself measured alone.
func TestCallSanitizedKeepsAKnownAudioLength(t *testing.T) {
	call := NewCall()
	call.AudioLength = 3500 * time.Millisecond

	if got := call.Sanitized().AudioLength; got != 3500*time.Millisecond {
		t.Errorf("AudioLength = %v, want the measured length kept", got)
	}
}
