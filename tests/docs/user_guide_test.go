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

package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The scanner webapp guide is one file a listener carries with them: an extracted release archive, a mail
// attachment, a phone opened where nothing else is available. These tests pin the properties that make that
// true — the document renders from nothing but itself, it carries every chapter and every part the reader was
// promised, it states the version it describes, and it is small enough to carry — because a guide that reaches
// out to the network, or loses a chapter on the way, fails the reader rather than the suite.
//
// The chapter content these tests read is a transcription of what the shipped listening application puts on
// the screen, so the tests that name controls, filters, display rows, flags, and announced sentences compare
// the guide's tables with the application's own sources, the way the administrator guide's tables are compared
// with the server's flags.

// userGuideChapters are the chapters the guide is written to, in document order. The contents aid and the
// document itself are both held to this list.
var userGuideChapters = []string{
	"ch-01-overview",
	"ch-02-the-main-screen",
	"ch-03-listening-to-live-calls",
	"ch-04-choosing-what-to-hear",
	"ch-05-searching-archived-calls",
	"ch-06-what-the-display-says",
	"ch-07-access-codes",
	"ch-08-on-your-phone-or-tablet",
	"ch-09-help-and-support",
	"ch-10-keyboard-and-screen-readers",
	"ch-11-when-something-does-not-work",
	"ch-12-reference",
}

// userGuideFile is the file a reader opens: the guide, in the documentation folder.
func userGuideFile(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs(filepath.Join("..", "..", "docs", "user-guide.html"))
	if err != nil {
		t.Fatalf("the guide path does not resolve: %v", err)
	}

	return path
}

func userGuidePath(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(userGuideFile(t))
	if err != nil {
		t.Fatalf("the guide is not readable at %s: %v", userGuideFile(t), err)
	}

	return string(body)
}

// userGuideChapter returns one chapter's own text, from its heading to the next chapter heading, which is
// where a chapter's tables, figures, and cautions are read.
func userGuideChapter(t *testing.T, body, id string) string {
	t.Helper()

	start := strings.Index(body, `id="`+id+`"`)
	if start < 0 {
		t.Fatalf("the guide does not carry the chapter %q", id)
	}

	end := len(body)

	for _, other := range userGuideChapters {
		if other == id {
			continue
		}

		if at := strings.Index(body[start:], `id="`+other+`"`); at > 0 {
			if start+at < end {
				end = start + at
			}
		}
	}

	return body[start:end]
}

// scannerPrintedText is the corpus of words the listening application can put on its own screen: every source
// under the presentation of the server, Go and browser alike. The only rewriting is the one a reader needs:
// a line break inside a label the components write — LIVE and FEED, SEARCH and CALL — becomes a space, and runs
// of whitespace become one space, so a label spread over two lines is still the one label a reader sees. The
// markup itself is left where it is. Taking tags out instead would let any comparison written with a less-than
// sign start a match that runs to the next greater-than sign somewhere else in the file, and that would swallow
// words the application plainly prints.
func scannerPrintedText(t *testing.T) string {
	t.Helper()

	var text strings.Builder

	err := filepath.Walk(filepath.Join("..", "..", "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			if info.Name() == "bundle" {
				return filepath.SkipDir
			}

			return nil
		}

		if strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "_test.js") {
			return nil
		}

		switch filepath.Ext(path) {
		case ".go", ".js", ".html", ".css", ".webmanifest":
		default:
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		text.WriteString(strings.ReplaceAll(string(body), "<br>", " "))

		text.WriteString("\n")

		return nil
	})

	if err != nil {
		t.Fatalf("the sources the guide is checked against cannot be read: %v", err)
	}

	text.WriteString("\n")

	return regexp.MustCompile(`\s+`).ReplaceAllString(text.String(), " ")
}

// isAddressShape reports whether something the guide sets in code is an address the reader types or the server
// serves — a path, a query — rather than a word the application prints. Those are held by the chapters that
// describe them and not checked against the words on the screen, because no screen prints a path.
func isAddressShape(name string) bool {
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "?") {
		return true
	}

	return strings.Contains(name, "://")
}

// The guide is read against the screen a listener is looking at. Every name it sets as the application's own word
// has to be a word the application writes. A caption is left out of this: a caption is an account of a picture,
// and a picture carries whatever the sample deployment was showing when it was photographed — a version, a count,
// the name of a talkgroup that only the sample deployment has.
func TestTheNamesTheGuidePrintsAreTheNamesTheApplicationPrints(t *testing.T) {
	body := userGuidePath(t)

	corpus := scannerPrintedText(t)

	body = regexp.MustCompile(`(?is)<figcaption>.*?</figcaption>`).ReplaceAllString(body, " ")

	names := map[string]bool{}

	for _, m := range regexp.MustCompile(`(?is)<code\b[^>]*>(.*?)</code>`).FindAllStringSubmatch(body, -1) {
		name := strings.TrimSpace(regexp.MustCompile(`(?is)<[^>]*>`).ReplaceAllString(m[1], " "))

		name = strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(name, " "))

		if name == "" || isAddressShape(name) {
			continue
		}

		names[name] = true

		if !strings.Contains(corpus, name) {
			t.Errorf("the guide writes %q as a word the application itself uses, and the application never writes it", name)
		}
	}

	if len(names) < 8 {
		t.Errorf("the guide names %d of the application's own words as printed words, want the controls, flags, and display rows it describes named the way the screen shows them", len(names))
	}
}

// The second chapter describes the screen the way the application assembles it: the branding line the operator
// sets, the server light and the three states it takes, the display area with what each row holds before a first
// call has arrived, the control area, and the version line. The names are the ones the components write, and the
// two figures belong to this chapter.
func TestTheUserGuideDescribesTheScreenAsTheApplicationBuildsIt(t *testing.T) {
	body := userGuidePath(t)

	chapter := userGuideChapter(t, body, "ch-02-the-main-screen")

	spoken := guideSpoken(chapter)

	for _, want := range []string{
		"Server status:",
		"on paused",
		"System",
		"Tag",
		"Talkgroup",
		"TGID: 0",
		"UID: 0",
		"AVOID",
		"PAUSE",
	} {
		if !strings.Contains(spoken, want) {
			t.Errorf("the chapter on the main screen never names %q, which is what the screen itself shows", want)
		}
	}

	for _, id := range []string{"main-idle", "light-states"} {
		slot := regexp.MustCompile(`(?is)<figure data-shot="` + id + `">(.*?)</figure>`).FindStringSubmatch(chapter)
		if len(slot) < 2 {
			t.Errorf("the chapter on the main screen does not carry the figure %q", id)

			continue
		}

		alt := regexp.MustCompile(`(?is)<img\b[^>]*\balt="([^"]*)"`).FindStringSubmatch(slot[1])
		if len(alt) < 2 || strings.TrimSpace(alt[1]) == "" {
			t.Errorf("the figure %q carries an image with nothing to say about it", id)
		}

		if !strings.Contains(slot[1], "<figcaption>") {
			t.Errorf("the figure %q carries no caption", id)
		}
	}
}

// The third chapter operates the listening controls. What it says about them has to match what the browser
// components and the domain code actually do: the words the page puts up when you try to leave while something is
// playing, the order the four levels are consulted in for a tone and for a colour, the built-in colour names, the
// timed windows an avoid walks through, the delay the display waits by before it dims, and the two figures that
// belong to the chapter.
func TestTheUserGuideOperatesTheListeningControlsAsTheApplicationDoes(t *testing.T) {
	body := userGuidePath(t)

	chapter := userGuideChapter(t, body, "ch-03-listening-to-live-calls")

	// The sentence the application hands to the browser, verbatim. It is shown by the browser's own leave
	// prompt, and a guide that tidies it up would disagree with what the reader is shown.
	spoken := guideSpoken(chapter)

	// The sentence the application hands to the browser, verbatim. It is shown by the browser's own leave
	// prompt, and a guide that tidies it up would disagree with what the reader is shown.
	if !strings.Contains(spoken, "Live Feed is ON, do you really want to leave?") {
		t.Errorf("the chapter on live calls does not give the words the application supplies to the browser when you try to leave")
	}

	at := strings.Index(spoken, "Alert tones")

	if at < 0 {
		t.Fatalf("the chapter on live calls has no part about alert tones, which is where the application's order of consultation belongs")
	}

	tones := spoken[at+len("Alert tones"):]

	for _, name := range []string{"group", "tag", "system", "talkgroup"} {
		level := strings.Index(tones, name)

		if level < 0 {
			t.Fatalf("the part of the chapter about alert tones never names %q, which is one of the four levels the application consults", name)
		}

		tones = tones[level+len(name):]
	}

	for _, color := range []string{"blue", "cyan", "green", "magenta", "orange", "red", "white", "yellow"} {
		if !strings.Contains(spoken, color) {
			t.Errorf("the chapter on live calls never names the colour %q, which is one of the colours the application can paint the server light in", color)
		}
	}

	for _, step := range []string{"30 minutes", "60 minutes", "120 minutes", "indefinitely"} {
		if !strings.Contains(spoken, step) {
			t.Errorf("the chapter on live calls does not give the avoid cycle as the application runs it, which holds %s among its steps", step)
		}
	}

	if !strings.Contains(spoken, "five seconds") {
		t.Errorf("the chapter on live calls never says how long the display waits before it dims, which is a delay the application fixes at five seconds")
	}

	for _, name := range []string{"LIVE FEED", "REPLAY LAST", "SKIP NEXT", "AVOID", "PAUSE", "Q:", "L:"} {
		if !strings.Contains(spoken, name) {
			t.Errorf("the chapter on live calls never names %q, which is a control or a reading the screen carries", name)
		}
	}

	for _, id := range []string{"main-live", "main-paused"} {
		slot := regexp.MustCompile(`(?is)<figure data-shot="` + id + `">(.*?)</figure>`).FindStringSubmatch(chapter)
		if len(slot) < 2 {
			t.Errorf("the chapter on live calls does not carry the figure %q", id)

			continue
		}

		alt := regexp.MustCompile(`(?is)<img\b[^>]*\balt="([^"]*)"`).FindStringSubmatch(slot[1])
		if len(alt) < 2 || strings.TrimSpace(alt[1]) == "" {
			t.Errorf("the figure %q carries an image with nothing to say about it", id)
		}

		if !strings.Contains(slot[1], "<figcaption>") {
			t.Errorf("the figure %q carries no caption", id)
		}
	}
}

// The fourth chapter operates the select panel. Its checks carry the two halves of one shipped fact: the small
// pair at the end of every row is named OFF and ON on the page this guide documents, and ALL ON and ALL OFF on the
// page it replaced, so the chapter is held both to the names in use and away from the names that are not. The rest
// are the conditions the panel itself applies — when its first row appears, when a row's pair appears, the sentence
// it answers with when the server has nothing configured — the colour the mixed state takes, the words the row is
// built from, and the three figures that belong to the chapter.
func TestTheUserGuideOperatesTheSelectPanelAsTheApplicationBuildsIt(t *testing.T) {
	chapter := userGuideChapter(t, userGuidePath(t), "ch-04-choosing-what-to-hear")

	spoken := guideSpoken(chapter)

	// The pair is checked in the form this guide uses for a word the screen prints, which is the markup claim
	// being made rather than a claim about the words alone.
	for _, name := range []string{"OFF", "ON"} {
		if !strings.Contains(chapter, "<code>"+name+"</code>") {
			t.Errorf("the chapter on choosing what to hear does not give the small button %q in the form this guide uses for a word the screen prints", name)
		}
	}

	for _, superseded := range []string{"ALL OFF", "ALL ON"} {
		if strings.Contains(spoken, superseded) {
			t.Errorf("the chapter on choosing what to hear names that pair %q, which is the name the superseded page printed and this one does not", superseded)
		}
	}

	// The sentence the panel answers with when no system has any talkgroups, verbatim from the component.
	if !strings.Contains(spoken, "No talkgroup configured!") {
		t.Errorf("the chapter on choosing what to hear does not give the words the panel says when the server has nothing configured")
	}

	for _, phrase := range []string{"two or more groups", "more than one talkgroup", "yellow", "timed"} {
		if !strings.Contains(spoken, phrase) {
			t.Errorf("the chapter on choosing what to hear never gives %q, which is a reading or a condition the panel applies", phrase)
		}
	}

	// The panel's first row is built from the deployment's groups. It was described as a row of tags while this
	// guide was being planned, and the panel's own component says otherwise.
	for _, mistaken := range []string{"tag row", "tag buttons", "tags row"} {
		if strings.Contains(spoken, mistaken) {
			t.Errorf("the chapter on choosing what to hear calls the panel's first row %q, and the panel builds that row from the deployment's groups", mistaken)
		}
	}

	for _, name := range []string{"SELECT TG", "AVOID"} {
		if !strings.Contains(spoken, name) {
			t.Errorf("the chapter on choosing what to hear never names %q, which is a control the chapter operates", name)
		}
	}

	for _, id := range []string{"select-panel", "select-panel-timed", "select-panel-empty"} {
		slot := regexp.MustCompile(`(?is)<figure data-shot="` + id + `">(.*?)</figure>`).FindStringSubmatch(chapter)
		if len(slot) < 2 {
			t.Errorf("the chapter on choosing what to hear does not carry the figure %q", id)

			continue
		}

		alt := regexp.MustCompile(`(?is)<img\b[^>]*\balt="([^"]*)"`).FindStringSubmatch(slot[1])
		if len(alt) < 2 || strings.TrimSpace(alt[1]) == "" {
			t.Errorf("the figure %q carries an image with nothing to say about it", id)
		}

		if !strings.Contains(slot[1], "<figcaption>") {
			t.Errorf("the figure %q carries no caption", id)
		}
	}
}

// guideSpoken reads a chapter the way it comes out of the page: the markup taken away and the runs of whitespace
// joined, so a sentence the document's source wraps across three lines is still the sentence a reader reads. A
// check about what the guide says works against this reading; a check about how the guide is built — a code span, a
// figure slot — works against the markup itself.
func guideSpoken(chapter string) string {
	return regexp.MustCompile(`\s+`).ReplaceAllString(stripTags(chapter), " ")
}

// The fifth chapter operates the search panel and the playback a searched call starts. Two things are checked
// here. The first is the words the panel puts on its own controls, which the guide has to carry because a reader
// reads this chapter while looking at that form. The second is the absence of four promises the superseded page
// made and the shipped panel does not keep: a paginator said to be disabled in playback mode, a stop said to cancel
// playback, a searched row said to be unavailable while the feed is paused, and a paginator said to follow the
// playback walk across pages. Each forbidden phrase is taken from the superseded page's own wording, so a claim
// that returns is a failing check rather than a finding someone has to notice in review.
func TestTheUserGuideOperatesSearchAndPlaybackAsTheShippedPanelDoes(t *testing.T) {
	chapter := userGuideChapter(t, userGuidePath(t), "ch-05-searching-archived-calls")

	spoken := guideSpoken(chapter)

	for _, name := range []string{
		"Sort order", "Date", "Time", "Dur", "System", "Talkgroup", "Group", "Tag",
		"All Systems", "All Talkgroups", "All Groups", "All Tags",
		"Download mode", "Reset", "Searching calls",
		"Play", "Stop", "Download", "Loading",
		"First page", "Previous page", "Next page", "Last page",
		"paginator", "6.6.3",
	} {
		if !strings.Contains(spoken, name) {
			t.Errorf("the chapter on searching recorded calls never names %q, which is a word the search panel or its paginator puts on the screen", name)
		}
	}

	// The shipped reading, stated as the decision about shipped behaviour says the chapter must state it: the
	// paginator is not taken away from the reader during a search or during a playback.
	if !strings.Contains(spoken, "stays available") {
		t.Errorf("the chapter on searching recorded calls does not say what the paginator does during a search, which is that it stays available")
	}

	// The panel bounds the date field only when the answer holds calls. The chapter's first draft claimed the
	// opposite - that an empty answer bounds the field to a date the archive has nothing in - so the check holds
	// both halves here: the shipped fact, and the refusal of the bound the shipped code refuses to set.
	if !strings.Contains(spoken, "unbounded") {
		t.Errorf("the chapter on searching recorded calls does not say what happens to the date field's bounds when the answer is empty, which is that the field is left unbounded")
	}

	if strings.Contains(spoken, "1970") {
		t.Errorf("the chapter on searching recorded calls bounds the date field to a date in 1970, which is a bound the panel refuses to set when an answer holds no calls")
	}

	// The actions column has no heading, which the chapter has to say because it is the one column a reader
	// cannot read a name for.
	if !strings.Contains(spoken, "no heading") {
		t.Errorf("the chapter on searching recorded calls does not say that the column of row actions carries no heading, which is what the panel's own table header holds")
	}

	for _, promised := range []string{
		"is disabled while in playback",
		"paginator is disabled",
		"paginator is locked",
		"paginator follows",
		"follows the walk",
		"cancel the playback",
		"Stop ends the playback",
		"ends playback mode",
		"cannot be played while",
		"will not play while",
		"unavailable while",
		"is not offered while",
	} {
		if strings.Contains(spoken, promised) {
			t.Errorf("the chapter on searching recorded calls says %q, which is a promise the superseded page made and the shipped panel does not keep", promised)
		}
	}

	for _, id := range []string{"search-list", "search-filters", "search-download", "search-playback", "search-empty"} {
		slot := regexp.MustCompile(`(?is)<figure data-shot="` + id + `">(.*?)</figure>`).FindStringSubmatch(chapter)
		if len(slot) < 2 {
			t.Errorf("the chapter on searching recorded calls does not carry the figure %q", id)

			continue
		}

		alt := regexp.MustCompile(`(?is)<img\b[^>]*\balt="([^"]*)"`).FindStringSubmatch(slot[1])
		if len(alt) < 2 || strings.TrimSpace(alt[1]) == "" {
			t.Errorf("the figure %q carries an image with nothing to say about it", id)
		}

		if !strings.Contains(slot[1], "<figcaption>") {
			t.Errorf("the figure %q carries no caption", id)
		}
	}
}

// The sixth chapter reads the display area. Two things are checked. The first is the words the display itself
// writes — the row labels and the flags are printed by the application and a reader matches this chapter against
// them while looking at the screen. The second is the handful of readings a reader cannot get from a glance: that
// the history is five rows, that the timed-avoid mark is a character whose glyph is not guaranteed, that the big row
// falls back to a frequency when the talkgroup carries no name, and that full screen has a route that is not a
// double-click.
func TestTheUserGuideReadsTheDisplayAsTheApplicationPaintsIt(t *testing.T) {
	chapter := userGuideChapter(t, userGuidePath(t), "ch-06-what-the-display-says")

	spoken := guideSpoken(chapter)

	for _, name := range []string{
		"System", "Tag", "Talkgroup", "NO LINK", "L:", "Q:", "F:", "TGID:", "E:", "S:", "UID:",
		"AVOID", "PATCH", "DELAYED", "Rdio Scanner v", "Fullscreen", "Exit fullscreen",
	} {
		if !strings.Contains(spoken, name) {
			t.Errorf("the chapter on the display never names %q, which is a word the display area writes", name)
		}
	}

	for _, fact := range []string{"five", "hourglass", "font", "double-click", "no name", "elapsed"} {
		if !strings.Contains(spoken, fact) {
			t.Errorf("the chapter on the display never gives %q, which is a reading or a condition the display itself applies", fact)
		}
	}

	if !strings.Contains(spoken, "screen reader") {
		t.Errorf("the chapter on the display never mentions a screen reader, which is the reader the truncated rows are written for")
	}

	for _, id := range []string{"main-flags", "main-history", "main-fullscreen"} {
		slot := regexp.MustCompile(`(?is)<figure data-shot="` + id + `">(.*?)</figure>`).FindStringSubmatch(chapter)
		if len(slot) < 2 {
			t.Errorf("the chapter on the display does not carry the figure %q", id)

			continue
		}

		alt := regexp.MustCompile(`(?is)<img\b[^>]*\balt="([^"]*)"`).FindStringSubmatch(slot[1])
		if len(alt) < 2 || strings.TrimSpace(alt[1]) == "" {
			t.Errorf("the figure %q carries an image with nothing to say about it", id)
		}

		if !strings.Contains(slot[1], "<figcaption>") {
			t.Errorf("the figure %q carries no caption", id)
		}
	}
}

// The seventh chapter deals with access. What the server answers and what the listener's field says are two
// different things, and the difference is the chapter's subject: a locked-out listener is answered exactly as a
// wrong code is, the sentence about too many connections belongs to a code's own limit and not to the server's
// cap, and the code the page kept in the browser is presented again without the listener doing anything. Each of
// those is one of the things a reader who runs out of luck needs to know before calling someone.
func TestTheUserGuideHandlesAccessCodesAsTheServerAnswersThem(t *testing.T) {
	chapter := userGuideChapter(t, userGuidePath(t), "ch-07-access-codes")

	spoken := guideSpoken(chapter)

	for _, name := range []string{
		"Unlock code", "Invalid unlock code", "This unlock code has expired", "Too many connections",
		"Max Clients", "NO LINK",
	} {
		if !strings.Contains(spoken, name) {
			t.Errorf("the chapter on access codes never names %q, which is a word the unlock screen or the server's settings put on the screen", name)
		}
	}

	for _, fact := range []string{
		"five", "fifth", "ten minutes", "200", "without asking", "operator", "profile", "?id=", "/reset",
	} {
		if !strings.Contains(spoken, fact) {
			t.Errorf("the chapter on access codes never gives %q, which is a number, a route, or a behaviour the shipped access rules apply", fact)
		}
	}

	for _, id := range []string{"unlock-code", "unlock-expired"} {
		slot := regexp.MustCompile(`(?is)<figure data-shot="` + id + `">.*?</figure>`).FindStringSubmatch(chapter)
		if len(slot) < 1 {
			t.Errorf("the chapter on access codes does not carry the figure %q", id)

			continue
		}

		alt := regexp.MustCompile(`(?is)<img\b[^>]*\balt="([^"]*)"`).FindStringSubmatch(slot[0])
		if len(alt) < 2 || strings.TrimSpace(alt[1]) == "" {
			t.Errorf("the figure %q carries an image with nothing to say about it", id)
		}

		if !strings.Contains(slot[0], "<figcaption>") {
			t.Errorf("the figure %q carries no caption", id)
		}
	}
}

// isUserGuideChapter reports whether an anchor names one of the guide's own chapters, which is what a chapter
// cross-reference has to land on.
func isUserGuideChapter(id string) bool {
	for _, want := range userGuideChapters {
		if want == id {
			return true
		}
	}

	return false
}

// The guide renders everything it shows from bytes inside itself: its stylesheet, and every picture once the
// pictures are in. A listener with no network gets the whole document, not a document with holes in it.
func TestTheUserGuideCarriesEverythingItRendersInsideItself(t *testing.T) {
	body := userGuidePath(t)

	for _, m := range regexp.MustCompile(`(?is)href="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		if !isSelfContained(m[1]) {
			t.Errorf("the guide reaches out to %q through an href", m[1])
		}
	}

	for _, m := range regexp.MustCompile(`(?is)\bsrc="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		if !isSelfContained(m[1]) {
			t.Errorf("the guide reaches out to %q through a src", m[1])
		}
	}

	style := regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style>`).FindStringSubmatch(body)
	if len(style) < 2 {
		t.Fatalf("the guide carries no inlined stylesheet")
	}

	for _, m := range regexp.MustCompile(`(?i)url\(([^)]*)\)`).FindAllStringSubmatch(style[1], -1) {
		if !isSelfContained(strings.Trim(m[1], `"'`)) {
			t.Errorf("the guide's stylesheet reaches out to %q", m[1])
		}
	}

	if at := strings.Index(strings.ToLower(body), "<script"); at >= 0 {
		t.Errorf("the guide carries a script at offset %d, and nothing in it needs one to be read", at)
	}

	if at := strings.Index(strings.ToLower(body), "<link"); at >= 0 {
		t.Errorf("the guide carries a link element at offset %d, and everything it renders is already inside it", at)
	}
}

// The twelve chapters, in the order the reader works through them: listen, then get access and a device, then
// look something up.
func TestTheUserGuideCarriesEveryChapterInTheOrderTheReaderWorksThrough(t *testing.T) {
	body := userGuidePath(t)

	var got []string

	for _, m := range regexp.MustCompile(`(?i)<h2[^>]*\bid="([^"]+)"`).FindAllStringSubmatch(body, -1) {
		got = append(got, m[1])
	}

	if len(got) != len(userGuideChapters) {
		t.Fatalf("the guide carries %d chapters, want %d: %v", len(got), len(userGuideChapters), got)
	}

	for i, want := range userGuideChapters {
		if got[i] != want {
			t.Errorf("chapter %d is %q, want %q", i+1, got[i], want)
		}
	}
}

// The contents aid is how a reader finds a chapter in a document this long. It names every chapter, every
// anchor it names exists, and it groups the chapters into the three parts the guide is written in.
func TestTheContentsAidNamesEveryUserGuideChapter(t *testing.T) {
	body := userGuidePath(t)

	nav := regexp.MustCompile(`(?is)<nav\b[^>]*aria-label="Contents"[^>]*>(.*?)</nav>`).FindStringSubmatch(body)
	if len(nav) < 2 {
		t.Fatalf("the guide has no contents aid labelled Contents")
	}

	want := map[string]bool{}

	for _, id := range userGuideChapters {
		want["#"+id] = true
	}

	found := map[string]bool{}

	for _, m := range regexp.MustCompile(`(?i)href="([^"]+)"`).FindAllStringSubmatch(nav[1], -1) {
		found[m[1]] = true
		delete(want, m[1])
	}

	if len(want) > 0 {
		t.Errorf("the contents aid does not name %d chapter(s), starting with %q", len(want), firstKey(want))
	}

	for href := range found {
		if !strings.HasPrefix(href, "#") {
			t.Errorf("the contents aid points at %q, which leaves the document", href)

			continue
		}

		if !strings.Contains(body, `id="`+strings.TrimPrefix(href, "#")+`"`) {
			t.Errorf("the contents aid points at %q, which the document does not carry", href)
		}
	}

	if parts := len(regexp.MustCompile(`(?i)<p class="part">`).FindAllString(nav[1], -1)); parts != 3 {
		t.Errorf("the contents aid groups the chapters into %d parts, want 3", parts)
	}
}

// The skip link is the first thing in the keyboard route, and it lands on the document's own main text rather
// than on the contents aid the keyboard has already passed.
func TestTheUserGuideSkipsToTheMainText(t *testing.T) {
	body := userGuidePath(t)

	skip := regexp.MustCompile(`(?is)<a class="skip"[^>]*href="#main"[^>]*>(.*?)</a>`).FindStringSubmatch(body)
	if len(skip) < 2 {
		t.Fatalf("the guide carries no skip link aimed at #main")
	}

	if strings.TrimSpace(skip[1]) == "" {
		t.Errorf("the guide's skip link says nothing, so a keyboard reader is told nothing about where it goes")
	}

	if header := strings.Index(strings.ToLower(body), "<header"); header >= 0 && strings.Index(body, `<a class="skip"`) > header {
		t.Errorf("the skip link comes after the banner, so it is not the first thing the keyboard meets")
	}

	if !strings.Contains(body, `id="main"`) {
		t.Errorf("the guide's skip link aims at #main, and the document carries no main text with that anchor")
	}
}

// The header names the mark beside the application's name and says what it is: an image a reader who cannot see
// it still gets an account of.
func TestTheUserGuideShowsTheMarkInTheHeader(t *testing.T) {
	body := userGuidePath(t)

	head := regexp.MustCompile(`(?is)<header\b.*?<h1>(.*?)</h1>`).FindStringSubmatch(body)
	if len(head) < 2 {
		t.Fatalf("the guide has no header with a heading of its own")
	}

	mark := regexp.MustCompile(`(?is)<img\b[^>]*class="mark"[^>]*>`).FindString(head[1])
	if mark == "" {
		t.Fatalf("the header carries no mark image beside the application's name")
	}

	alt := regexp.MustCompile(`(?i)alt="([^"]*)"`).FindStringSubmatch(mark)
	if len(alt) < 2 || strings.TrimSpace(alt[1]) == "" {
		t.Errorf("the header's mark carries no alt text: %s", mark)
	}

	if !regexp.MustCompile(`(?i)\bdata-logo\b`).MatchString(mark) {
		t.Errorf("the header's mark is not the slot the picture step fills: %s", mark)
	}
}

// The document's last line is the project's sign-off, which is how the administrator guide's reader is also
// let go.
func TestTheUserGuideClosesWithTheSignOff(t *testing.T) {
	footer := regexp.MustCompile(`(?is)<footer\b.*?</footer>`).FindString(userGuidePath(t))
	if footer == "" {
		t.Fatalf("the guide has no footer")
	}

	paragraphs := regexp.MustCompile(`(?is)<p\b[^>]*>(.*?)</p>`).FindAllStringSubmatch(footer, -1)
	if len(paragraphs) == 0 {
		t.Fatalf("the guide's footer says nothing")
	}

	last := strings.TrimSpace(regexp.MustCompile(`(?is)<[^>]*>`).ReplaceAllString(paragraphs[len(paragraphs)-1][1], ""))

	if last != "Happy Rdio Scanning !" {
		t.Errorf("the guide closes with %q, want the project's sign-off", last)
	}
}

// Every address in the guide is the reader moving inside the page. The guide names the operator's guide in
// words rather than as a file, because an extracted archive is not a place a filename resolves in.
func TestNothingInTheUserGuidePointsOutsideItself(t *testing.T) {
	body := userGuidePath(t)

	anchors := map[string]bool{}

	for _, m := range regexp.MustCompile(`(?is)\bid="([^"]+)"`).FindAllStringSubmatch(body, -1) {
		anchors[m[1]] = true
	}

	links := 0

	for _, m := range regexp.MustCompile(`(?is)href="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		links++

		href := m[1]

		if !strings.HasPrefix(href, "#") {
			t.Errorf("the guide links to %q, which leaves the document", href)

			continue
		}

		if !anchors[strings.TrimPrefix(href, "#")] {
			t.Errorf("the guide links to %q, which the document does not carry", href)
		}
	}

	if links < 12 {
		t.Errorf("the guide carries %d links: the contents aid alone names every chapter, so there are at least 12", links)
	}

	prose := regexp.MustCompile(`(?is)<[^>]*>`).ReplaceAllString(body, " ")

	for _, name := range regexp.MustCompile(`[A-Za-z0-9_\-./]+\.(?:md|html)\b`).FindAllString(prose, -1) {
		t.Errorf("the guide names %q, a document the reader does not have beside the guide", name)
	}
}

// The size budget the guide is written to: one file a listener carries. The measurement is recorded on every
// run, so the number the guide promises is the number the file has.
func TestTheUserGuideIsSmallEnoughToCarry(t *testing.T) {
	const budget = 4 << 20

	info, err := os.Stat(userGuideFile(t))
	if err != nil {
		t.Fatalf("the guide is not readable: %v", err)
	}

	t.Logf("the guide measures %d bytes of %d allowed", info.Size(), budget)

	if info.Size() > budget {
		t.Errorf("the guide measures %d bytes, over the %d byte budget", info.Size(), budget)
	}
}

// A printed guide, a guide on a machine whose colours the reader chose, and a guide for a reader who has asked
// for less motion all work off the stylesheet inside the document. The recoloured reading is the one the
// application's own stylesheet is already held to, and the guide argues with no colour the reader chose.
func TestTheUserGuideCarriesTheReadingsAPrintedAndARecolouredPageNeeds(t *testing.T) {
	style := regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style>`).FindStringSubmatch(userGuidePath(t))
	if len(style) < 2 {
		t.Fatalf("the guide carries no inlined stylesheet")
	}

	for _, want := range []string{
		"@media print",
		"@media (prefers-reduced-motion: reduce)",
		"@media (forced-colors: active)",
	} {
		if !strings.Contains(style[1], want) {
			t.Errorf("the guide's stylesheet carries no %s rule, so that reading depends on something the guide does not have", want)
		}
	}

	if at := strings.Index(style[1], "forced-color-adjust"); at >= 0 {
		t.Errorf("the guide's stylesheet pins a colour of its own to something at offset %d, which is what a reader's chosen colours are for", at)
	}
}

// A listener who used version 6.6.3 is the reader this guide is written for first: the screens are the ones they
// knew, and everything that changed arrived in the update they have just been given. A change the reader cannot
// act on is not news, so each change names the chapter that operates it, and the update's own cost is said
// rather than left to be discovered on the day the operator upgrades the server.
func TestTheUserGuideTellsAVersion663ListenerWhatChangedAndWhere(t *testing.T) {
	body := userGuidePath(t)
	chapter := userGuideChapter(t, body, "ch-01-overview")

	start := strings.Index(chapter, "What's new since version 6.6.3")
	if start < 0 {
		start = strings.Index(chapter, "What&#39;s new since version 6.6.3")
	}

	if start < 0 {
		t.Fatalf("the opening chapter does not carry the changes since version 6.6.3")
	}

	section := chapter[start:]

	links := regexp.MustCompile(`(?i)href="([^"]+)"`).FindAllStringSubmatch(section, -1)

	if len(links) < 25 {
		t.Errorf("the changes since version 6.6.3 name %d chapters, want a chapter to act on for each of the twenty-five changes the update brought", len(links))
	}

	for _, m := range links {
		href := m[1]

		if !strings.HasPrefix(href, "#") {
			t.Errorf("the changes since version 6.6.3 point at %q, outside the document", href)

			continue
		}

		if !strings.Contains(body, `id="`+strings.TrimPrefix(href, "#")+`"`) {
			t.Errorf("the changes since version 6.6.3 point at %q, which the document does not carry", href)

			continue
		}

		if !isUserGuideChapter(strings.TrimPrefix(href, "#")) {
			t.Errorf("the changes since version 6.6.3 point at %q, which is not one of this guide's chapters", href)
		}
	}

	for _, cost := range []string{"upgraded in place", "access code", "/reset", "selection"} {
		if !strings.Contains(section, cost) {
			t.Errorf("the changes since version 6.6.3 do not name the cost of the update to a listener: %q is missing", cost)
		}
	}
}

// The opening chapter says what the listener will not find here — the server and its administration, and the
// native applications the operator may also have offered — because a guide that reaches for both answers neither
// well. It also says how the product's name is read, which is the first thing a listener who has to ask an
// operator about it needs.
func TestTheUserGuideSaysWhatItLeavesOutAndHowTheNameIsRead(t *testing.T) {
	chapter := userGuideChapter(t, userGuidePath(t), "ch-01-overview")

	for _, want := range []string{"native applications", "administration", "does not cover"} {
		if !strings.Contains(chapter, want) {
			t.Errorf("the opening chapter never names %q, so the reader is left to find out what this guide leaves out", want)
		}
	}

	if !strings.Contains(chapter, `<span class="syllables">`) {
		t.Errorf("the opening chapter never says how the name is read, which is what a listener needs in order to ask about it")
	}

	if !strings.Contains(chapter, "Raydio") {
		t.Errorf("the opening chapter never gives the name as it is spoken, so the written name and the spoken name are left to be guessed apart")
	}
}

// The guide states which version it describes, in its own words, at the top of the page, and that version is
// the one in the repository the guide is published beside. Everything it says about the application is a claim
// about that version behaving as that version behaves.
func TestTheUserGuideStatesTheVersionItDocuments(t *testing.T) {
	body := userGuidePath(t)

	version := regexp.MustCompile(`(?is)<p class="meta"[^>]*>\s*Version (\d+\.\d+(?:\.\d+)?)`).FindStringSubmatch(body)
	if len(version) < 2 {
		t.Fatalf("the guide does not state a version at the head of the page")
	}

	want := strings.TrimSpace(repoFile(t, "VERSION.txt"))

	if version[1] != want {
		t.Errorf("the guide says it covers version %q, the repository is %q", version[1], want)
	}

	if !strings.Contains(body, "The guide links to nothing outside itself") {
		t.Errorf("the guide does not say, at the head of the page, that it reaches out to nothing")
	}
}
