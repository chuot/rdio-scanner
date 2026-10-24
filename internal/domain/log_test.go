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

package domain

import (
	"testing"
	"time"
)

func TestLogLevelsMatchTheLegacyServer(t *testing.T) {
	checks := map[string]struct{ got, want string }{
		"LogLevelInfo":  {LogLevelInfo, "info"},
		"LogLevelWarn":  {LogLevelWarn, "warn"},
		"LogLevelError": {LogLevelError, "error"},
	}

	for name, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", name, c.got, c.want)
		}
	}
}

func TestLogSearchDefaults(t *testing.T) {
	search := ParseLogSearch(map[string]any{})

	if search.HasLevel() {
		t.Error("an empty criteria must not filter by level")
	}

	if search.HasDate() {
		t.Error("an empty criteria must not filter by date")
	}

	if search.Limit != DefaultLogSearchLimit {
		t.Errorf("Limit = %d, want the default %d", search.Limit, DefaultLogSearchLimit)
	}

	if !search.Ascending {
		t.Error("the default sort order is ascending")
	}
}

func TestLogSearchClampsTheLimitButHonoursAnExplicitZero(t *testing.T) {
	if got := ParseLogSearch(map[string]any{"limit": float64(5000)}).Limit; got != MaxLogSearchLimit {
		t.Errorf("Limit = %d, want it clamped to %d", got, MaxLogSearchLimit)
	}

	if got := ParseLogSearch(map[string]any{"limit": float64(50)}).Limit; got != 50 {
		t.Errorf("Limit = %d, want the submitted 50", got)
	}

	// The legacy server clamps a submitted limit but never replaces a submitted zero
	// with the default, so an explicit zero asks for no rows at all.
	if got := ParseLogSearch(map[string]any{"limit": float64(0)}).Limit; got != 0 {
		t.Errorf("Limit = %d, want the submitted 0 preserved", got)
	}
}

func TestLogSearchSortDirection(t *testing.T) {
	if !ParseLogSearch(map[string]any{"sort": float64(1)}).Ascending {
		t.Error("a positive sort asks for ascending order")
	}

	if ParseLogSearch(map[string]any{"sort": float64(-1)}).Ascending {
		t.Error("a negative sort asks for descending order")
	}

	if !ParseLogSearch(map[string]any{"sort": float64(0)}).Ascending {
		t.Error("a zero sort asks for ascending order")
	}

	if !ParseLogSearch(map[string]any{}).Ascending {
		t.Error("an absent sort asks for ascending order")
	}
}

func TestLogSearchDateMustParse(t *testing.T) {
	search := ParseLogSearch(map[string]any{"date": "2026-03-14T09:30:00Z"})

	if !search.HasDate() {
		t.Fatal("a valid RFC3339 date must set the date filter")
	}

	if want := time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC); !search.Date.Equal(want) {
		t.Errorf("Date = %v, want %v", search.Date, want)
	}

	if ParseLogSearch(map[string]any{"date": "not a date"}).HasDate() {
		t.Error("an unparsable date must leave the criteria unfiltered, as the legacy server does")
	}
}

func TestLogSearchLevelFilter(t *testing.T) {
	search := ParseLogSearch(map[string]any{"level": LogLevelError})

	if !search.HasLevel() || search.Level != LogLevelError {
		t.Errorf("ParseLogSearch = %+v, want an error-level filter", search)
	}
}

// TestLogSearchDateWindowDependsOnSortDirection pins a legacy asymmetry: the day the
// date filter selects is a full 24 hours from the truncated timestamp when sorting
// ascending, but is shifted back by the submitted time of day when sorting descending.
func TestLogSearchDateWindowDependsOnSortDirection(t *testing.T) {
	date := time.Date(2026, 3, 14, 9, 30, 15, 500, time.UTC)

	ascending := LogSearch{Date: date, Ascending: true}

	from, to, _ := ascending.DateWindow()

	wantFrom := time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)
	wantTo := wantFrom.Add(24*time.Hour - time.Millisecond)

	if !from.Equal(wantFrom) {
		t.Errorf("ascending window start = %v, want %v (seconds and sub-second dropped)", from, wantFrom)
	}

	if !to.Equal(wantTo) {
		t.Errorf("ascending window stop = %v, want %v", to, wantTo)
	}

	descending := LogSearch{Date: date, Ascending: false}

	from, to, _ = descending.DateWindow()

	// 24 hours back, then back again by the submitted hour and minute.
	wantFrom = time.Date(2026, 3, 13, 9, 30, 0, 0, time.UTC).Add(-9*time.Hour - 30*time.Minute)
	wantTo = wantFrom.Add(24*time.Hour - time.Millisecond - 9*time.Hour - 30*time.Minute)

	if !from.Equal(wantFrom) {
		t.Errorf("descending window start = %v, want %v", from, wantFrom)
	}

	if !to.Equal(wantTo) {
		t.Errorf("descending window stop = %v, want %v", to, wantTo)
	}
}

func TestLogSearchWithoutADateHasNoWindow(t *testing.T) {
	from, to, ok := LogSearch{}.DateWindow()

	if ok {
		t.Errorf("DateWindow with no date = %v..%v, want no window", from, to)
	}
}

func TestLogSearchOffset(t *testing.T) {
	if got := ParseLogSearch(map[string]any{"offset": float64(400)}).Offset; got != 400 {
		t.Errorf("Offset = %d, want the submitted 400", got)
	}
}

func TestLogEntryCarriesItsOwnValues(t *testing.T) {
	at := time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)

	entry := LogEntry{ID: 12, DateTime: at, Level: LogLevelWarn, Message: "duplicate call rejected"}

	if entry.ID != 12 || entry.Level != LogLevelWarn || entry.Message != "duplicate call rejected" || !entry.DateTime.Equal(at) {
		t.Errorf("LogEntry = %+v, want the submitted values", entry)
	}
}

func TestLogSearchLimitConstants(t *testing.T) {
	if DefaultLogSearchLimit != 200 {
		t.Errorf("DefaultLogSearchLimit = %d, want 200", DefaultLogSearchLimit)
	}

	if MaxLogSearchLimit != 500 {
		t.Errorf("MaxLogSearchLimit = %d, want 500", MaxLogSearchLimit)
	}
}

// The source vocabulary is the operator's names for the parts of the server, and it is closed.
// A name outside it is not a filter that matches nothing — it is a caller asking for a subsystem this
// server does not have, and it has to be refused rather than answered with an empty log view.
func TestKnownLogSourcesAreTheDeclaredVocabulary(t *testing.T) {
	want := []LogSource{
		LogSourceServer, LogSourceConfig, LogSourceDirwatch, LogSourceDownstream, LogSourceDelayer,
		LogSourceIngest, LogSourceLogin, LogSourceAccess, LogSourceLivefeed, LogSourceFFmpeg,
		LogSourceUpload, LogSourcePrune, LogSourceLogs, LogSourceDatabase, LogSourceAdmin, LogSourceMcp,
		LogSourceCalls, LogSourceLegacy,
	}

	got := KnownLogSources()

	if len(got) != len(want) {
		t.Fatalf("KnownLogSources() = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("KnownLogSources()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestEveryLogSourceSaysWhetherAWriterWritesIt is the classification the vocabulary owes
// whoever reads it: a name is either a part of this server that writes entries, or a name
// that exists only so rows written before the source column existed can still be found.
// A name in neither group means a filter nothing can match reads as "nothing happened".
func TestEveryLogSourceSaysWhetherAWriterWritesIt(t *testing.T) {
	written := []LogSource{
		LogSourceServer, LogSourceConfig, LogSourceDirwatch, LogSourceDownstream, LogSourceDelayer,
		LogSourceIngest, LogSourceLogin, LogSourceAccess, LogSourceLivefeed, LogSourceFFmpeg,
		LogSourceUpload, LogSourcePrune, LogSourceLogs, LogSourceDatabase, LogSourceAdmin, LogSourceMcp,
	}

	// calls is the name this feature found rather than the one it designed: the legacy server's call
	// repository stamped its lines calls., and this server writes nothing about a call under that name.
	// legacy is the other: no writer files under it, and it is what the backfill gives a message that maps
	// to nothing.
	backfilled := []LogSource{LogSourceCalls, LogSourceLegacy}

	for _, source := range KnownLogSources() {
		want := containsLogSource(written, source)

		if !want && !containsLogSource(backfilled, source) {
			t.Errorf("%q is in the vocabulary and classified as neither a name a writer writes nor a name only "+
				"the backfill reaches: an unclassified name is a filter the log view offers and nothing can "+
				"answer", source)

			continue
		}

		if got := LogSourceIsWritten(source); got != want {
			t.Errorf("LogSourceIsWritten(%q) = %v, want %v", source, got, want)
		}
	}

	if got, want := len(written)+len(backfilled), len(KnownLogSources()); got != want {
		t.Errorf("the vocabulary classifies %d names and holds %d: a name classified twice, or a name in one "+
			"list and not the other", got, want)
	}

	// A name the server does not have is not one it writes either.
	if LogSourceIsWritten(LogSource("nonsense")) {
		t.Errorf("LogSourceIsWritten(nonsense) = true, want false: a name outside the vocabulary is not a part " +
			"of this server")
	}
}

func containsLogSource(sources []LogSource, want LogSource) bool {
	for _, source := range sources {
		if source == want {
			return true
		}
	}

	return false
}

func TestIsKnownLogSourceAcceptsEveryDeclaredName(t *testing.T) {
	for _, source := range KnownLogSources() {
		if !IsKnownLogSource(source) {
			t.Errorf("IsKnownLogSource(%q) = false, want true for a declared name", source)
		}
	}
}

func TestIsKnownLogSourceRefusesANameThatIsNotOne(t *testing.T) {
	// `dirwatcher` is the name a reader reaches for first, and `apikey` is a table the server has
	// but a subsystem it does not report on: an apikey is only ever presented outbound.
	for _, name := range []string{"", "dirwatcher", "apikey", "Dirwatch", "downstreams", "live feed"} {
		if IsKnownLogSource(LogSource(name)) {
			t.Errorf("IsKnownLogSource(%q) = true, want false: the vocabulary is closed", name)
		}
	}
}

func TestParseLogSourceReadsANameInTheVocabulary(t *testing.T) {
	source, ok := ParseLogSource("dirwatch")

	if !ok || source != LogSourceDirwatch {
		t.Errorf("ParseLogSource(\"dirwatch\") = %q, %v, want %q, true", source, ok, LogSourceDirwatch)
	}
}

func TestParseLogSourceRefusesWhatIsNotAName(t *testing.T) {
	for _, submitted := range []any{"dirwatcher", "", "DIRWATCH", 7, nil, map[string]any{}} {
		source, ok := ParseLogSource(submitted)

		if ok {
			t.Errorf("ParseLogSource(%v) = %q, true, want a refusal", submitted, source)
		}

		if source != "" {
			t.Errorf("ParseLogSource(%v) returned %q with a refusal, want no source", submitted, source)
		}
	}
}

func TestLogEntryCarriesItsSource(t *testing.T) {
	at := time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)

	entry := LogEntry{
		ID:       12,
		DateTime: at,
		Level:    LogLevelError,
		Source:   LogSourceDirwatch,
		Message:  "the watcher for /data/incoming stopped: no such file or directory",
	}

	if entry.Source != LogSourceDirwatch {
		t.Errorf("LogEntry.Source = %q, want %q", entry.Source, LogSourceDirwatch)
	}
}

// The two new search criteria. They follow the rule the existing ones follow — a criterion that is not
// submitted is not applied — with one exception the log view depends on: a source that is not a name in
// the vocabulary is reported, so the endpoint can refuse it instead of showing an empty log.
func TestLogSearchHasNoSourceAndNoTextWhenNoneIsSubmitted(t *testing.T) {
	search := ParseLogSearch(map[string]any{})

	if search.HasSource() {
		t.Error("an empty criteria must not filter by source")
	}

	if search.HasText() {
		t.Error("an empty criteria must not filter by text")
	}

	if search.SourceWasRejected() {
		t.Error("a criteria that names no source has nothing to refuse")
	}
}

func TestLogSearchReadsASourceCriterion(t *testing.T) {
	search := ParseLogSearch(map[string]any{"source": "dirwatch"})

	if !search.HasSource() || search.Source != LogSourceDirwatch {
		t.Errorf("Source = %q after submitting \"dirwatch\", want %q", search.Source, LogSourceDirwatch)
	}

	if search.SourceWasRejected() {
		t.Error("a submitted name in the vocabulary is not a refusal")
	}
}

func TestLogSearchReportsASourceThatIsNotAName(t *testing.T) {
	search := ParseLogSearch(map[string]any{"source": "dirwatcher"})

	if search.HasSource() {
		t.Errorf("Source = %q, want no source applied for a name outside the vocabulary", search.Source)
	}

	if !search.SourceWasRejected() {
		t.Error("a submitted source that is not a name must be reported, not ignored")
	}
}

func TestLogSearchReadsATextCriterion(t *testing.T) {
	search := ParseLogSearch(map[string]any{"text": "/data/incoming"})

	if !search.HasText() || search.Text != "/data/incoming" {
		t.Errorf("Text = %q, want %q", search.Text, "/data/incoming")
	}

	// An empty text is the log view clearing the box, not a search for the empty string.
	if cleared := ParseLogSearch(map[string]any{"text": ""}); cleared.HasText() {
		t.Error("an empty text must not filter anything")
	}

	// A text that is not a string is not a criterion the caller meant to apply, and is not applied.
	if notText := ParseLogSearch(map[string]any{"text": 7}); notText.HasText() {
		t.Errorf("Text = %q after submitting a number, want no text filter", notText.Text)
	}
}

func TestLogSearchKeepsReadingTheCriteriaItAlreadyRead(t *testing.T) {
	at, _ := time.Parse(time.RFC3339, "2026-03-14T09:30:00Z")

	search := ParseLogSearch(map[string]any{
		"date": "2026-03-14T09:30:00Z", "level": LogLevelWarn, "limit": float64(50),
		"offset": float64(10), "sort": float64(-1), "source": "ingest", "text": "1514000",
	})

	if !search.HasDate() || !search.Date.Equal(at) {
		t.Errorf("Date = %v, want the submitted instant", search.Date)
	}

	if !search.HasLevel() || search.Level != LogLevelWarn {
		t.Errorf("Level = %q, want warn", search.Level)
	}

	if search.Limit != 50 || search.Offset != 10 || search.Ascending {
		t.Errorf("limit/offset/sort = %d/%d/%v, want 50/10/descending", search.Limit, search.Offset, search.Ascending)
	}

	if search.Source != LogSourceIngest || search.Text != "1514000" {
		t.Errorf("source/text = %q/%q, want ingest/1514000", search.Source, search.Text)
	}
}

// TestParseLogSearchClampsThePageParameters: the log view's page parameters
// clamp the same way the call search's do — a negative limit is not "no limit", and a huge
// offset is not a scan past the end of the log.
func TestParseLogSearchClampsThePageParameters(t *testing.T) {
	tests := []struct {
		name       string
		limit      float64
		offset     float64
		wantLimit  uint
		wantOffset uint
	}{
		{"negative limit", -5, 0, 1, 0},
		{"zero limit", 0, 0, 0, 0},
		{"huge limit", 1e18, 0, MaxLogSearchLimit, 0},
		{"negative offset", 100, -1, 100, 0},
		{"huge offset", 100, 1e18, 100, MaxSearchOffset},
		{"ordinary page", 50, 200, 50, 200},
	}

	for _, tc := range tests {
		search := ParseLogSearch(map[string]any{"limit": tc.limit, "offset": tc.offset})

		if search.Limit != tc.wantLimit || search.Offset != tc.wantOffset {
			t.Errorf("%s: limit = %d offset = %d, want limit = %d offset = %d",
				tc.name, search.Limit, search.Offset, tc.wantLimit, tc.wantOffset)
		}
	}
}
