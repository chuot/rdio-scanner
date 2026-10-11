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

package backend

import (
	"testing"

	"rdio-scanner/internal/domain"
)

// The administrator's reading capabilities: what this deployment is, what its log view says, and
// what was said around a line the operator is looking at. Each one answers a question an operator
// asks, in one call, with the paging the server accepts echoed back as it was applied.

func TestInspectSystemSummarizesWhatIsBeingAdministered(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	answered := call(t, s, "inspect_system", map[string]any{})

	var summary deploymentSummaryShape
	decode(t, answered, &summary)

	if summary.Version != testVersion {
		t.Fatalf("the summary names version %q, want %q", summary.Version, testVersion)
	}

	// The counts are the inventory an operator reads before changing anything: how many of each
	// thing is under this server.
	counts := map[string]uint{
		"systems":     summary.Counts.Systems,
		"talkgroups":  summary.Counts.Talkgroups,
		"sites":       summary.Counts.Sites,
		"units":       summary.Counts.Units,
		"groups":      summary.Counts.Groups,
		"tags":        summary.Counts.Tags,
		"accesses":    summary.Counts.Accesses,
		"apikeys":     summary.Counts.Apikeys,
		"dirwatches":  summary.Counts.Dirwatches,
		"downstreams": summary.Counts.Downstreams,
	}
	for _, name := range []string{"systems", "groups", "tags", "accesses", "apikeys", "dirwatches", "downstreams"} {
		if counts[name] == 0 {
			t.Fatalf("the summary counts %s as none, and the fixture holds some: %+v", name, counts)
		}
	}

	if summary.Counts.Systems != 2 {
		t.Fatalf("the summary counts %d systems, want the fixture's two", summary.Counts.Systems)
	}

	// A disabled watcher and a disabled forwarder are named as disabled, because "the watched
	// directory is not being read" is the answer the operator is looking for.
	if summary.DisabledDirwatches != 1 {
		t.Fatalf("the summary names %d disabled watched directories, want one", summary.DisabledDirwatches)
	}
	if summary.DisabledDownstreams != 1 {
		t.Fatalf("the summary names %d disabled downstreams, want one", summary.DisabledDownstreams)
	}

	// The settings that decide what the server keeps and what it does with a recording.
	if summary.PruneDays == 0 {
		t.Fatalf("the summary names no prune horizon: %+v", summary)
	}
	if summary.MaxClients == 0 {
		t.Fatalf("the summary names no listener ceiling: %+v", summary)
	}
	if !summary.AutoPopulate {
		t.Fatalf("the fixture's options auto-populate, the summary says they do not: %+v", summary)
	}

	// The server's own notice that the default credential is still in place.
	if !summary.PasswordNeedsChange {
		t.Fatalf("the summary does not report that the administrator password still needs changing")
	}

	// The alert sequences the operator can name in a capture payload: the nine the server carries,
	// each with the tones it is made of.
	if len(summary.Alerts) != 9 {
		t.Fatalf("the summary carries %d alert sequences, want the nine the server carries", len(summary.Alerts))
	}
	for _, alert := range summary.Alerts {
		if alert.Name == "" || len(alert.Tones) == 0 {
			t.Fatalf("the summary carries an alert sequence with no name or no tones: %+v", alert)
		}
	}
}

// The administrator's reading capabilities are behind the administrator's credential. An access code
// opens the read-only tier, and the refusal says what is required without naming what the caller is
// missing.
func TestTheAdministratorsReadingCapabilitiesRefuseAnAccessCodeCaller(t *testing.T) {
	s := newCallerSession(t, readScoped())

	for _, tool := range []string{"inspect_system", "search_logs", "explain_error"} {
		refused := refusedCall(t, s, tool, map[string]any{"source": "ffmpeg", "text": "conversion", "date": fixtureTime.Format("2006-01-02")})

		if refused == "" {
			t.Fatalf("%s answered an access-code caller", tool)
		}
	}
}

// A deployment that lists no access codes takes an anonymous caller into the read-only tier, not
// into the administrator's: reaching the whole deployment is done by signing in, and the surface
// that is open to the network does not decide on its own that an anonymous caller is an
// administrator.
func TestTheAdministratorsReadingCapabilitiesRefuseAnAnonymousCaller(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierReadOnly})

	for _, tool := range []string{"inspect_system", "search_logs", "explain_error"} {
		refused := refusedCall(t, s, tool, map[string]any{"source": "ffmpeg", "text": "conversion", "date": fixtureTime.Format("2006-01-02")})

		if refused == "" {
			t.Fatalf("%s answered a caller carrying no credential", tool)
		}
	}
}

func TestSearchLogsAnswersTheFiltersAndEchoesThePageItUsed(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	day := fixtureTime.Format("2006-01-02")

	// Every filter the log view accepts is an argument of this one tool.
	for _, probe := range []struct {
		what  string
		args  map[string]any
		count uint64
	}{
		{"the whole day", map[string]any{"date": day}, 5},
		{"by severity", map[string]any{"date": day, "level": "error"}, 1},
		{"by subsystem", map[string]any{"date": day, "source": "ffmpeg"}, 2},
		{"by message", map[string]any{"date": day, "text": "call 12"}, 3},
		{"by subsystem and severity", map[string]any{"date": day, "source": "ffmpeg", "level": "error"}, 1},
	} {
		answered := call(t, s, "search_logs", probe.args)

		var page logPageShape
		decode(t, answered, &page)

		if page.Count != probe.count {
			t.Fatalf("%s answered %d entries, want %d: %+v", probe.what, page.Count, probe.count, page.Entries)
		}
	}

	// The page is echoed as the server applied it, not as it was asked for: a request past the
	// ceiling gets the ceiling, and a request naming no page gets the default.
	answered := call(t, s, "search_logs", map[string]any{"date": day, "limit": 9000})

	var clamped logPageShape
	decode(t, answered, &clamped)

	if clamped.Options.Limit != domain.MaxLogSearchLimit {
		t.Fatalf("a page asking for 9000 rows echoed %d, want the ceiling the server applies",
			clamped.Options.Limit)
	}

	answered = call(t, s, "search_logs", map[string]any{"date": day})

	var defaulted logPageShape
	decode(t, answered, &defaulted)

	if defaulted.Options.Limit != domain.DefaultLogSearchLimit {
		t.Fatalf("a page naming no size echoed %d, want the default the server applies",
			defaulted.Options.Limit)
	}
}

func TestSearchLogsNamesTheSpanTheCriteriaMatch(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	answered := call(t, s, "search_logs", map[string]any{"date": fixtureTime.Format("2006-01-02")})

	var page logPageShape
	decode(t, answered, &page)

	if page.DateStart == "" || page.DateStop == "" {
		t.Fatalf("the page names no span: %+v", page)
	}

	// The span belongs to everything the criteria match, so it stays where it is while the page
	// moves: the same criteria further along answer the same span.
	next := call(t, s, "search_logs", map[string]any{"date": fixtureTime.Format("2006-01-02"), "offset": 2})

	var second logPageShape
	decode(t, next, &second)

	if second.DateStart != page.DateStart || second.DateStop != page.DateStop {
		t.Fatalf("the span moved with the page: %s..%s then %s..%s",
			page.DateStart, page.DateStop, second.DateStart, second.DateStop)
	}
}

func TestExplainErrorReadsTheNeighbourhoodOfALine(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	answered := call(t, s, "explain_error", map[string]any{
		"source": "ffmpeg",
		"text":   "conversion failed",
		"date":   fixtureTime.Format("2006-01-02"),
	})

	var explanation errorExplanationShape
	decode(t, answered, &explanation)

	if explanation.Named.ID != 2 {
		t.Fatalf("the explanation names entry %d, want the line the text matched: %+v",
			explanation.Named.ID, explanation.Named)
	}

	// The same subsystem, in order, on both sides of the named line.
	if len(explanation.Same) != 2 {
		t.Fatalf("the subsystem's own neighbourhood answered %d lines, want the two it wrote that day: %+v",
			len(explanation.Same), explanation.Same)
	}
	if explanation.Same[0].ID != 1 || explanation.Same[1].ID != 2 {
		t.Fatalf("the subsystem's neighbourhood is out of order: %+v", explanation.Same)
	}

	// The neighbouring subsystems in the same window, grouped, so the answer says what else was
	// going on: the ingest line that arrived next to the failure, and not the prune line an hour
	// later filed under its own subsystem.
	neighbours := map[string][]logEntryShape{}
	for _, group := range explanation.Neighbours {
		neighbours[group.Source] = group.Entries
	}

	if _, ok := neighbours["ffmpeg"]; ok {
		t.Fatalf("the explanation lists the named subsystem among its neighbours: %+v", explanation.Neighbours)
	}

	found := false
	for _, row := range neighbours["ingest"] {
		if row.ID == 3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("the explanation does not carry the neighbouring subsystem's line: %+v", explanation.Neighbours)
	}
}

// deploymentSummaryShape is the shape inspect_system answers.
type deploymentSummaryShape struct {
	Version string `json:"version"`
	Docker  bool   `json:"docker"`

	PasswordNeedsChange bool `json:"passwordNeedsChange"`

	Counts struct {
		Systems     uint `json:"systems"`
		Talkgroups  uint `json:"talkgroups"`
		Sites       uint `json:"sites"`
		Units       uint `json:"units"`
		Groups      uint `json:"groups"`
		Tags        uint `json:"tags"`
		Accesses    uint `json:"accesses"`
		Apikeys     uint `json:"apikeys"`
		Dirwatches  uint `json:"dirwatches"`
		Downstreams uint `json:"downstreams"`
	} `json:"counts"`

	DisabledDirwatches  uint `json:"disabledDirwatches"`
	DisabledDownstreams uint `json:"disabledDownstreams"`

	PruneDays                  uint `json:"pruneDays"`
	MaxClients                 uint `json:"maxClients"`
	AutoPopulate               bool `json:"autoPopulate"`
	DuplicateDetectionDisabled bool `json:"duplicateDetectionDisabled"`
	AudioConversion            uint `json:"audioConversion"`

	Alerts []struct {
		Name  string `json:"name"`
		Tones []struct {
			Begin     float32 `json:"begin"`
			End       float32 `json:"end"`
			Frequency uint    `json:"frequency"`
			Kind      string  `json:"type"`
		} `json:"tones"`
	} `json:"alerts"`
}

// logPageShape is the shape search_logs answers.
type logPageShape struct {
	Count     uint64 `json:"count"`
	DateStart string `json:"dateStart"`
	DateStop  string `json:"dateStop"`

	Options struct {
		Limit  uint   `json:"limit"`
		Offset uint   `json:"offset"`
		Level  string `json:"level"`
		Source string `json:"source"`
		Text   string `json:"text"`
		Sort   int    `json:"sort"`
	} `json:"options"`

	Entries []logEntryShape `json:"entries"`
}

// logEntryShape is one entry of the log view, as the test reads it.
type logEntryShape struct {
	ID       uint64 `json:"id"`
	DateTime string `json:"dateTime"`
	Level    string `json:"level"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

// errorExplanationShape is the shape explain_error answers.
type errorExplanationShape struct {
	Named      logEntryShape   `json:"named"`
	Same       []logEntryShape `json:"same"`
	Neighbours []struct {
		Source  string          `json:"source"`
		Entries []logEntryShape `json:"entries"`
	} `json:"neighbours"`
}
