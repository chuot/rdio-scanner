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
	"strconv"
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

// The light beside the branding line is the LED. That is the noun for it in everything that ships it: the
// component file describes itself as the branding line and the server LED and gives the element the class led,
// the client domain that resolves its colour is named for LED colour, and the operator's screens print a caption
// about the default LED colour being green. This guide's own rule is that the names it states are the names the
// shipped application writes, and "the server light" is a paraphrase no shipped string carries anywhere. The name
// the page gives that element to a screen reader is a different string, printed as Server status: followed by the
// state, and it is quoted exactly as printed. The word light stays for the page's other lights, because they are
// not this one.
func TestTheGuideNamesTheHeaderLightAsTheProductDoes(t *testing.T) {
	body := userGuidePath(t)

	hits := regexp.MustCompile(`(?i)server light`).FindAllString(body, -1)

	if len(hits) > 0 {
		t.Errorf("the guide calls the light beside the branding line %q %d times, and no shipped string uses that name: the component, the client domain that paints it, and the operator's own screens all call it the LED", "server light", len(hits))
	}

	for _, chapter := range []string{"ch-02-the-main-screen", "ch-03-listening-to-live-calls", "ch-10-keyboard-and-screen-readers", "ch-12-reference"} {
		// A picture carried inside the guide is a long run of base64, and base64 spells the same letters by
		// chance. The claim being made here is about the words the guide says, so the bytes of its own pictures
		// are taken out before the chapter is read.
		prose := regexp.MustCompile(`src="data:[^"]*"`).ReplaceAllString(userGuideChapter(t, body, chapter), `src=""`)

		if !strings.Contains(prose, "LED") {
			t.Errorf("the chapter %q operates the light beside the branding line and never names it the LED", chapter)
		}
	}

	// The guide's new noun is not the accessible name, and the two are not interchangeable: the name the page
	// gives the element is what a reader who cannot see the colour is told, and the guide quotes it as printed.
	if !strings.Contains(body, "<code>Server status:</code>") {
		t.Errorf("the guide no longer quotes %q followed by the state, which is the name the shipped page gives the light to a screen reader", "Server status:")
	}

	// Renaming the header light is not renaming every light on the page. The corner lights are the controls' own
	// marks, and a guide that calls them LEDs describes controls that do not have one.
	if !strings.Contains(body, "corner light") {
		t.Errorf("the guide no longer names the corner lights on the controls, which are lights on the screen and not the header light")
	}

	// The reference chapter's own row for this light has to carry the name, because a reader looks a word up in
	// that table after having already seen it on the screen. The row is found by the name the page gives the
	// element, which is printed in the row and nowhere else on the page.
	lights := regexp.MustCompile(`(?is)<caption>\s*The lights on the screen[^<]*</caption>[\s\S]*?<tbody>(.*?)</tbody>`).FindStringSubmatch(userGuideChapter(t, body, "ch-12-reference"))

	if lights == nil {
		t.Fatalf("the reference chapter carries no table of the lights on the screen, so its row for the header light cannot be read")
	}

	header := ""

	for _, row := range regexp.MustCompile(`(?is)<tr>\s*<th scope="row">(.*?)</th>.*?</tr>`).FindAllStringSubmatch(lights[1], -1) {
		if strings.Contains(row[0], "<code>Server status:</code>") {
			header = strings.Join(strings.Fields(stripTags(row[1])), " ")

			break
		}
	}

	if header == "" {
		t.Errorf("the reference chapter's table of the lights on the screen carries no row quoting %q, which is how a reader finds the header light there", "Server status:")
	} else if !strings.Contains(header, "LED") {
		t.Errorf("the reference chapter names the row for the header light %q, and the name the shipped application uses for that element is the LED", header)
	}
}

// The second chapter describes the screen the way the application assembles it: the branding line the operator
// sets, the LED and the three states it takes, the display with what each row holds before a first call has
// arrived, the control bar, and the version line. The names are the ones the components write, and the
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

	// The paused light's normal reading is an alternation of the light's own face, not a ring around it: the shipped
	// stylesheet switches the lit face to the grey the light wears at rest, and the standing ring is what the reading
	// that asks for less motion answers with. A draft of this chapter described the ring as the normal reading, and a
	// reader who believed it would look for a mark that is not on the page.
	for _, superseded := range []string{"the ring blinks", "always blinking"} {
		if strings.Contains(spoken, superseded) {
			t.Errorf("the chapter on the main screen carries %q, which describes a mark the shipped page does not draw in that state", superseded)
		}
	}

	// The minutes beside the hourglass in the flag row are the length of the window an avoid opened, and nothing on
	// the page counts them down. A draft of this chapter told the reader to read the remaining minutes off the mark,
	// and a listener who believed it would watch a number that never moves.
	for _, superseded := range []string{"minutes left", "minutes remaining", "minutes are left"} {
		if strings.Contains(spoken, superseded) {
			t.Errorf("the chapter on the main screen carries %q, which describes a countdown the shipped display does not keep", superseded)
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
			t.Errorf("the chapter on live calls never names the colour %q, which is one of the colours the application can paint the LED in", color)
		}
	}

	for _, step := range []string{"30 minutes", "60 minutes", "120 minutes", "indefinitely"} {
		if !strings.Contains(spoken, step) {
			t.Errorf("the chapter on live calls does not give the avoid cycle as the application runs it, which holds %s among its steps", step)
		}
	}

	// A tone plays before the recording it belongs to, and the recording is not yet under way during it. The shipped
	// play path defers the recording behind the tone and cancels it when the call is silenced, skipped, or taken out of
	// the selection in the meantime. A draft of this chapter left that window out, and a listener who skipped a call
	// during its tone would have expected the recording to arrive anyway and looked for audio that never starts.
	for _, want := range []string{"never starts its recording", "the tone is the only part of that call you hear"} {
		if !strings.Contains(spoken, want) {
			t.Errorf("the chapter on live calls never gives %q, which is what the shipped application does to a call whose tone is interrupted", want)
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

	// Only two lights on the shipped page animate: the LED while audio is held, and a talkgroup's button in
	// the select panel while that talkgroup sits inside a timed window. The four lights on the control bar are drawn
	// once and only their colour changes. A draft of this chapter told the reader the live feed light is always
	// blinking, and a listener who believed it would be hunting for a signal the screen never draws.
	if !strings.Contains(spoken, "shape") {
		t.Errorf("the chapter on live calls never says that the state light's shape does not change, which is the fact a reader needs in order to know the colour is the whole of the signal")
	}

	for _, superseded := range []string{"blinking", "blinks"} {
		if strings.Contains(spoken, superseded) {
			t.Errorf("the chapter on live calls carries %q, which describes motion the shipped control bar does not draw", superseded)
		}
	}

	// A timed window is what the application closes, not what it renews: when a window runs out the talkgroup is put
	// back to hearing and its window is cleared, so a press after that starts the cycle again from its beginning. A
	// draft of this chapter said the window opens itself again, and a listener who believed it would expect a
	// talkgroup to go on being held off past a window the application had already ended. The minutes written beside
	// the mark are the length of that window, which the application never counts down.
	for _, superseded := range []string{"opens itself again", "minutes left"} {
		if strings.Contains(spoken, superseded) {
			t.Errorf("the chapter on live calls carries %q, which describes an avoid window the shipped application does not keep", superseded)
		}
	}

	for _, want := range []string{"closes itself", "the minutes that window is held for"} {
		if !strings.Contains(spoken, want) {
			t.Errorf("the chapter on live calls never gives %q, which is what the shipped application does with a timed avoid window", want)
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

	// The delay named here is the deployment holding a call back, and the flag it arrives with is printed by the
	// display in capitals, the way the other two flags in that row are. A reader is told the flag in words and then
	// looks for it on the screen, and chapter 6 gives the row: the chapter spells it the way the page prints it.
	if !strings.Contains(spoken, "DELAYED") {
		t.Errorf("the chapter on searching recorded calls never names the delay flag as %q, which is how the page prints it", "DELAYED")
	}

	if strings.Contains(chapter, "<code>Delayed</code>") {
		t.Errorf("the chapter on searching recorded calls prints the delay flag as %q, which is not the spelling the page prints", "Delayed")
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

	// The panel draws one action per row, and decides which one through a chain that the download switch heads: with
	// the switch on, the row whose recording is being heard reads Download rather than Stop. A draft of this chapter
	// listed the four shapes in an order the shipped chain does not follow, and a listener downloading a searched
	// call would have looked for a stop control the panel never draws.
	for _, link := range []string{"otherwise Loading", "otherwise Stop", "otherwise Play"} {
		if !strings.Contains(spoken, link) {
			t.Errorf("the chapter on searching recorded calls does not give the row's action as the chain that draws it, which decides %s", link)
		}
	}

	for _, superseded := range []string{"or Stop while that row's recording is the one being heard, or Download"} {
		if strings.Contains(spoken, superseded) {
			t.Errorf("the chapter on searching recorded calls lists the row's action in the order %q, which is not the order the shipped chain decides in", superseded)
		}
	}

	// The chapter on searching is where this guide promises to operate converted audio, so it has to say what
	// conversion does to a recording a listener plays or saves. The shipped play path and the shipped download path
	// read the same bytes the server sends, converted first when the operator has conversion switched on. A draft of
	// this chapter spoke of the arriving recording without naming conversion at all, and the chapter its own
	// converted-audio row pointed at then carried nothing about it.
	for _, want := range []string{
		"the converted copy when the operator has audio conversion switched on",
		"the audio you hear and the file you keep are the same copy",
	} {
		if !strings.Contains(spoken, want) {
			t.Errorf("the chapter on searching recorded calls never gives %q, which is what its own converted-audio row promises this chapter carries", want)
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

// The sixth chapter reads the display. Two things are checked. The first is the words the display itself
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
			t.Errorf("the chapter on the display never names %q, which is a word the display writes", name)
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

	for _, id := range []string{"main-flags", "main-history"} {
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

	// The chapter carried a third figure, a crop of the full-screen control beside the help control. The owner's
	// review of the finished guide called it out as showing nothing, and the review is right: the picture is a small
	// button in a corner, and full screen is a route through the display, which the two readings above carry. The
	// chapter's account of full screen is the double-click route, the control's own route, and the two names the
	// control prints, and a figure that shows neither the display nor the change to it is furniture.
	if figures := regexp.MustCompile(`(?is)<figure data-shot="`).FindAllString(chapter, -1); len(figures) != 2 {
		t.Errorf("the chapter on the display carries %d figures, want the two it operates: the flag row and the history table", len(figures))
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

// The eighth chapter is about the listening screen on a phone. Two things it is required to get right are the words
// the shipped update prompt and promo notice actually carry, and the rule that the native applications are named
// without the guide reaching for them: the store names appear, no link leaves the guide for a store, and the chapter's
// pictures are the three the figure inventory gives it, because a fourth could only be a native application's screen.
func TestTheUserGuideDescribesThePhoneAsTheBrowserShowsIt(t *testing.T) {
	chapter := userGuideChapter(t, userGuidePath(t), "ch-08-on-your-phone-or-tablet")

	spoken := guideSpoken(chapter)

	for _, name := range []string{
		"Rdio Scanner", "New version available", "No", "Yes", "NO LINK", "Android", "Apple", "standalone",
	} {
		if !strings.Contains(spoken, name) {
			t.Errorf("the chapter on phones and tablets never names %q, which is a word the shipped page, its manifest, or its notices put up", name)
		}
	}

	for _, fact := range []string{"ten seconds", "five minutes", "Google Play", "App Store", "countdown", "320"} {
		if !strings.Contains(spoken, fact) {
			t.Errorf("the chapter on phones and tablets never gives %q, which is a shipped interval, store name, or width the chapter has to state", fact)
		}
	}

	if outward := regexp.MustCompile(`(?is)<a\b[^>]*\bhref="http`).FindAllString(chapter, -1); len(outward) > 0 {
		t.Errorf("the chapter on phones and tablets carries %d links out of the guide, and it names the two stores precisely so that it does not", len(outward))
	}

	if figures := regexp.MustCompile(`(?is)<figure data-shot="`).FindAllString(chapter, -1); len(figures) != 3 {
		t.Errorf("the chapter on phones and tablets carries %d figures, and the only three it may carry are the listening screen at a phone's width, the notice it shows about the native applications, and the new-version prompt", len(figures))
	}

	for _, id := range []string{"main-narrow", "promo-snackbar", "update-dialog"} {
		slot := regexp.MustCompile(`(?is)<figure data-shot="` + id + `">.*?</figure>`).FindStringSubmatch(chapter)
		if len(slot) < 1 {
			t.Errorf("the chapter on phones and tablets does not carry the figure %q", id)

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

// The ninth chapter is about whom a listener can ask and what is worth saying when they ask. The words the shipped
// help notice and the shipped version line are built from are checked against the sources, and so are the two things
// the chapter exists to tell a reader: that the help affordance only exists when the operator configured an address,
// and that the version on the screen belongs to the page rather than to the server.
func TestTheUserGuideSaysWhoToAskAndWhatToSay(t *testing.T) {
	chapter := userGuideChapter(t, userGuidePath(t), "ch-09-help-and-support")

	spoken := guideSpoken(chapter)

	for _, name := range []string{"Rdio Scanner v", "Server status", "dev"} {
		if !strings.Contains(spoken, name) {
			t.Errorf("the chapter on help and support never names %q, which is a word the shipped screen puts up or a version the shipped build stamps in", name)
		}
	}

	// The chapter's own heading carries the word Help, so the check for the control runs on what follows the heading.
	body := guideSpoken(regexp.MustCompile(`(?is)<h2\b.*?</h2>`).ReplaceAllString(chapter, ""))

	if !strings.Contains(body, "Help") {
		t.Errorf("the chapter on help and support never names the Help control, which is the only control the screen offers for finding a support address")
	}

	for _, fact := range []string{
		"For further assistance please contact", "Rdio Scanner discussions", "ten seconds", "8.0.0", "mail", "commit",
		"no Help control", "reaches it",
	} {
		if !strings.Contains(spoken, fact) {
			t.Errorf("the chapter on help and support never gives %q, which is a sentence the shipped notice says or a fact about what the screen can tell the operator", fact)
		}
	}

	// The count of other listeners is a reading the display writes only when the operator has it turned on, and the
	// server ships with it turned off. The chapter's first draft asked a reader to note the count as something the
	// display carries, which is a promise the shipped display does not keep for every deployment.
	if !strings.Contains(spoken, "when the deployment shows it") {
		t.Errorf("the chapter on help and support asks whether anybody else was listening without saying that the count is only there when the deployment shows it")
	}

	// The chapter owns exactly one figure, and anything more in it would be a picture the figure inventory does not
	// give this chapter.
	if figures := regexp.MustCompile(`(?is)<figure data-shot="`).FindAllString(chapter, -1); len(figures) != 1 {
		t.Errorf("the chapter on help and support carries %d figures, and the only one it may carry is the notice the help control shows", len(figures))
	}

	slot := regexp.MustCompile(`(?is)<figure data-shot="help-snackbar">.*?</figure>`).FindStringSubmatch(chapter)
	if len(slot) < 1 {
		t.Fatalf("the chapter on help and support does not carry the figure %q", "help-snackbar")
	}

	alt := regexp.MustCompile(`(?is)<img\b[^>]*\balt="([^"]*)"`).FindStringSubmatch(slot[0])
	if len(alt) < 2 || strings.TrimSpace(alt[1]) == "" {
		t.Errorf("the figure %q carries an image with nothing to say about it", "help-snackbar")
	}

	if !strings.Contains(slot[0], "<figcaption>") {
		t.Errorf("the figure %q carries no caption", "help-snackbar")
	}
}

// The tenth chapter is the one a keyboard user and a screen-reader user are meant to trust: it names what the
// application does for them, what it leaves to colour, and the colour figures that were measured. Its own promises
// are what this check holds it to — the sentences the shipped page speaks, the names the shipped page gives its own
// controls, the measured figures, and exactly the three figures the figure inventory gives this chapter.
func TestTheUserGuideReportsWhatTheScreenCarriesBesidesColour(t *testing.T) {
	chapter := userGuideChapter(t, userGuidePath(t), "ch-10-keyboard-and-screen-readers")

	spoken := guideSpoken(chapter)

	for _, name := range []string{
		"Rdio Scanner", "Skip to contents", "Server status:", "Close search panel", "Close select panel",
		"Fullscreen", "Exit fullscreen", "AVOID", "PATCH", "DELAYED",
	} {
		if !strings.Contains(spoken, name) {
			t.Errorf("the chapter on keyboards and screen readers never names %q, which is a name the shipped page gives itself", name)
		}
	}

	for _, fact := range []string{
		"Queue:", "Playing", "Paused", "Stopped", "Searching calls", "Page",
		"reduced motion", "forced colours", "4.5 to 1", "3 to 1", "2.07", "48", "zoom", "320", "landmark",
	} {
		if !strings.Contains(spoken, fact) {
			t.Errorf("the chapter on keyboards and screen readers never gives %q, which is a sentence the shipped page speaks or a figure or rule the chapter was written to report", fact)
		}
	}

	// The two figures this feature first measured wrongly, and the claim one of them was written to support. The
	// corrected figures are on the record; a chapter that repeated the superseded ones would be publishing a fault
	// the shipped page does not have.
	for _, superseded := range []string{"2.61", "1.00", "invisible by design", "always blinking"} {
		if strings.Contains(spoken, superseded) {
			t.Errorf("the chapter on keyboards and screen readers carries %q, which is a superseded figure or the claim it was written to support", superseded)
		}
	}

	// The column that holds the row actions is the first column of the shipped table, and it is the one its header
	// row leaves without a heading. A draft of this chapter placed it at the other end, and a reader looking for it at
	// the right edge of the table would have found the column carrying talkgroup names instead.
	for _, superseded := range []string{"last column of the search results"} {
		if strings.Contains(spoken, superseded) {
			t.Errorf("the chapter on keyboards and screen readers carries %q, which places the search table's action column where the shipped table does not put it", superseded)
		}
	}

	if !strings.Contains(spoken, "first column of the search results") {
		t.Errorf("the chapter on keyboards and screen readers never says the search results' action column is the first one, which is where the shipped table puts it")
	}

	// Three figures, which is all the figure inventory gives this chapter.
	if figures := regexp.MustCompile(`(?is)<figure data-shot="`).FindAllString(chapter, -1); len(figures) != 3 {
		t.Errorf("the chapter on keyboards and screen readers carries %d figures, and the inventory gives it exactly three", len(figures))
	}

	for _, id := range []string{"main-320-reflow", "keyboard-focus", "forced-colours"} {
		slot := regexp.MustCompile(`(?is)<figure data-shot="` + id + `">.*?</figure>`).FindString(chapter)
		if slot == "" {
			t.Fatalf("the chapter on keyboards and screen readers does not carry the figure %q", id)
		}

		alt := regexp.MustCompile(`(?is)<img\b[^>]*\balt="([^"]*)"`).FindStringSubmatch(slot)
		if len(alt) < 2 || strings.TrimSpace(alt[1]) == "" {
			t.Errorf("the figure %q carries an image with nothing to say about it", id)
		}

		if !strings.Contains(slot, "<figcaption>") {
			t.Errorf("the figure %q carries no caption", id)
		}
	}
}

// The eleventh chapter answers the reader who arrives with something broken. It is written symptom first, because
// that is how a listener arrives: they know the screen went quiet, not which of the application's parts is quiet.
// Every remedy it offers is a thing the shipped application already does — a control, a panel, the page's own reading
// of a state — and the chapter's own limits are named: a lockout and a retention window are the operator's, and the
// screen does not distinguish them. The names are what the unlock field and the controls print, the figures are the
// shipped windows the application counts in, and no administration procedure belongs here.
func TestTheUserGuideAnswersTheReaderWhoFindsNothingWorking(t *testing.T) {
	body := userGuidePath(t)

	chapter := userGuideChapter(t, body, "ch-11-when-something-does-not-work")

	spoken := guideSpoken(chapter)

	for _, want := range []string{
		"LIVE FEED",
		"HOLD SYS",
		"HOLD TG",
		"PAUSE",
		"SKIP NEXT",
		"REPLAY LAST",
		"AVOID",
		"SEARCH CALL",
		"SELECT TG",
		"DELAYED",
		"NO LINK",
		"Server status:",
		"Unlock code",
		"Invalid unlock code",
		"This unlock code has expired",
		"Too many connections",
		"Max Clients",
		"Prune Days",
		"New version available",
	} {
		if !strings.Contains(spoken, want) {
			t.Errorf("the chapter about when something does not work never prints %q, which is a name the shipped application puts in front of the reader", want)
		}
	}

	for _, want := range []string{
		"silent",
		"nothing is arriving",
		"answers nothing",
		"will not pause",
		"will not stop",
		"one press",
		"twice",
		"flagged delayed",
		"not in the results",
		"has expired",
		"lost",
		"old version",
		"0 of 0",
		"continues where it stopped",
		"queue",
		"500 milliseconds",
		"hourly",
		"retention",
		"ask the operator",
	} {
		if !strings.Contains(spoken, want) {
			t.Errorf("the chapter about when something does not work never gives %q, which is a symptom a reader searches for, a remedy the shipped application offers, or a window the application counts in", want)
		}
	}

	// A remedy has to be a thing the shipped application does. Anything that reads as a server procedure, or as
	// advice about the machine behind the deployment, belongs in the operator's guide and not here.
	for _, forbidden := range []string{
		"restart",
		"reboot",
		"flush",
		"delete the database",
		"clear the calls",
		"dashboard",
		"console",
		"code is locked",
	} {
		if strings.Contains(spoken, forbidden) {
			t.Errorf("the chapter about when something does not work offers %q, which is not a thing the shipped application does for a listener", forbidden)
		}
	}

	// The search panel opens searching with whatever its filters are already holding, and it does not fill them in
	// from the call standing on the display. A draft of this chapter said it did, and a reader who believed that
	// would look for a prefilled row that the shipped panel never writes.
	for _, superseded := range []string{"already filled in"} {
		if strings.Contains(spoken, superseded) {
			t.Errorf("the chapter about when something does not work carries %q, which describes a thing the shipped search panel does not do", superseded)
		}
	}

	// Three readings the sweep against the shipped application found this chapter holding wrongly. The minutes the
	// hourglass carries are the length of an avoid window, and they ride the display's flag row rather than the
	// control bar's AVOID button, which has no light of its own and blinks in no state. A press of SKIP NEXT while a
	// hold is standing ends the recording but starts nothing, because the application begins no audio while a hold is
	// standing. And a version update costs a listener no code to type again, because the page reads the code an older
	// application left in the same browser and presents it by itself. Each superseded reading is refused by name.
	for _, superseded := range []string{
		"that button's corner mark blinks",
		"how much of the window is left",
		"takes the next thing the queue holds",
		"a code you have to enter again",
	} {
		if strings.Contains(spoken, superseded) {
			t.Errorf("the chapter about when something does not work carries %q, which describes behaviour the shipped application does not have", superseded)
		}
	}

	for _, want := range []string{
		"the minutes that window is held for",
		"only when you let go of the hold",
		"the flag row carries the hourglass",
	} {
		if !strings.Contains(spoken, want) {
			t.Errorf("the chapter about when something does not work never gives %q, which is what the shipped application does instead", want)
		}
	}

	// The timed-avoid mark is a character the page asks a font for, so a machine with none of the fonts it asks for can
	// show it as a box or as nothing. A reader who sees that looks here first, and the answer has to be that the
	// reading is unaffected rather than a second account of the mark, which the chapter on the display gives and this
	// chapter links to.
	for _, want := range []string{"arrives as a box", "the minutes are still beside it"} {
		if !strings.Contains(spoken, want) {
			t.Errorf("the chapter about when something does not work never gives %q, which is what a reader who sees the timed-avoid mark as a box needs to be told", want)
		}
	}

	// The chapter leans on the chapters that already describe each part, rather than describing them a second
	// time. The hrefs are read the way the guide's own self-containment check reads them, because a link whose
	// opening tag wraps across a line is still one link.
	hrefs := map[string]bool{}

	for _, m := range regexp.MustCompile(`(?is)href="([^"]*)"`).FindAllStringSubmatch(chapter, -1) {
		hrefs[m[1]] = true
	}

	for _, want := range []string{
		"#ch-01-overview",
		"#ch-04-choosing-what-to-hear",
		"#ch-05-searching-archived-calls",
		"#ch-07-access-codes",
		"#ch-08-on-your-phone-or-tablet",
	} {
		if !hrefs[want] {
			t.Errorf("the chapter about when something does not work never points at %s, which is where the detail it leans on is already written", want)
		}
	}

	// No figures: the figure inventory gives this chapter none, and a reader who cannot get anything working is
	// reading words, not a picture of the application doing it.
	if figures := regexp.MustCompile(`(?is)<figure data-shot="`).FindAllString(chapter, -1); len(figures) != 0 {
		t.Errorf("the chapter about when something does not work carries a figure, and the inventory gives it none")
	}
}

// Every list the guide asks a reader to scan is a table, and a table whose rows are named by ordinary cells leaves a
// screen reader nothing to hold a row together with: each value reads as one of a string of unrelated things, and the
// reader has to work out which column it belongs to. The guide's own contract states the shape — a caption, a header
// row of column headers, and a row header wherever the first column names the row — so this reads every table in the
// document, not only the ones the reference chapter adds. A caption is checked too, because a caption is what a
// screen reader announces before the reader is inside the table, and two tables with the same caption are two tables
// a reader cannot tell apart.
func TestEveryTableInTheGuideNamesItsRowsAndColumns(t *testing.T) {
	tables := regexp.MustCompile(`(?is)<table\b.*?</table>`).FindAllString(userGuidePath(t), -1)

	if len(tables) == 0 {
		t.Fatalf("the guide carries no table at all, so this check proves nothing")
	}

	seen := map[string]int{}

	for _, table := range tables {
		caption := regexp.MustCompile(`(?is)<caption\b[^>]*>(.*?)</caption>`).FindStringSubmatch(table)

		if caption == nil {
			label := strings.Join(strings.Fields(stripTags(table)), " ")

			if runes := []rune(label); len(runes) > 70 {
				label = string(runes[:70])
			}

			t.Errorf("the guide has a table a reader reaches with nothing saying what it lists: %s", label)

			continue
		}

		name := strings.Join(strings.Fields(stripTags(caption[1])), " ")

		seen[name]++

		if head := regexp.MustCompile(`(?is)<thead\b[^>]*>(.*?)</thead>`).FindStringSubmatch(table); head == nil {
			t.Errorf("the table %q has no header row, so nothing names its columns", name)
		} else {
			for _, cell := range regexp.MustCompile(`(?is)<(th|td)\b([^>]*)>`).FindAllStringSubmatch(head[1], -1) {
				if cell[1] != "th" {
					t.Errorf("the table %q builds its header row from a %q cell, which names no column", name, cell[1])

					continue
				}

				if !strings.Contains(cell[2], `scope="col"`) {
					t.Errorf("the table %q has a column header with no scope=%q, so a reader cannot pair a cell with its column", name, "col")
				}
			}
		}

		for _, body := range regexp.MustCompile(`(?is)<tbody\b[^>]*>(.*?)</tbody>`).FindAllStringSubmatch(table, -1) {
			for _, row := range regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr>`).FindAllStringSubmatch(body[1], -1) {
				cells := regexp.MustCompile(`(?is)<(th|td)\b([^>]*)>`).FindAllStringSubmatch(row[1], -1)

				if len(cells) == 0 {
					continue
				}

				if cells[0][1] != "th" || !strings.Contains(cells[0][2], `scope="row"`) {
					t.Errorf("the table %q starts a row with a %q cell, so the row has nothing naming it", name, cells[0][1])
				}

				for _, cell := range cells[1:] {
					if cell[1] == "th" && !strings.Contains(cell[2], "scope=") {
						t.Errorf("the table %q has a header cell inside its body with no scope", name)
					}
				}
			}
		}

		opened := len(regexp.MustCompile(`(?i)<th\b`).FindAllString(table, -1))

		if closed := strings.Count(table, "</th>"); opened != closed {
			t.Errorf("the table %q opens %d header cells and closes %d, so its markup is not the markup a reader's browser is given", name, opened, closed)
		}
	}

	for name, count := range seen {
		if count > 1 {
			t.Errorf("%d tables in the guide carry the caption %q, and a reader is told nothing about which one they are in", count, name)
		}
	}
}

// The project holds one position about the licence of what it ships, and it is stated where an operator reads it. The
// listening guide documents the screen a listener uses: it has no standing to restate those terms, and a reader who
// carries an extracted copy of it away from the rest of the archive would be holding terms the guide has no business
// asserting. So the guide names where the position is written and states none of its own. The refusal is on the words
// a reader reads, not on the bytes of the file: an embedded picture's base64 can hold any three letters the guide
// never meant, and the stylesheet is not prose.
func TestTheUserGuideStatesNoLicencePositionOfItsOwn(t *testing.T) {
	body := userGuidePath(t)

	visible := regexp.MustCompile(`(?is)<style\b.*?</style>`).ReplaceAllString(body, " ")
	visible = strings.ToLower(stripTags(visible))

	for _, term := range []string{
		"general public license",
		"gnu",
		"gpl",
		"free software",
		"free software foundation",
		"copyright",
		"warranty",
		"proprietary",
	} {
		if strings.Contains(visible, term) {
			t.Errorf("the guide states %q to a reader, and the guide holds no licence position of its own to state", term)
		}
	}

	spoken := guideSpoken(userGuideChapter(t, body, "ch-12-reference"))

	for _, want := range []string{"licence", "no licence position of its own", "operator's guide"} {
		if !strings.Contains(spoken, want) {
			t.Errorf("the reference chapter never says %q, which is how a reader is told where that position is written", want)
		}
	}
}

// The last chapter is where a reader who already knows what they are looking for goes. It is tables and a definition
// list rather than prose, it carries no figures, and it points back at the chapters that operate each thing it
// names: the point of a reference is that it can be scanned, and everything in it was earned earlier in the guide.
// The names are the words the shipped application puts on its own controls, flags, criteria, and settings, and the
// sentences it says out loud for a screen reader.
func TestTheUserGuideCarriesTheReferenceChapterAReaderScans(t *testing.T) {
	body := userGuidePath(t)

	chapter := userGuideChapter(t, body, "ch-12-reference")

	spoken := guideSpoken(chapter)

	for _, want := range []string{
		"LIVE FEED", "HOLD SYS", "HOLD TG", "REPLAY LAST", "SKIP NEXT", "AVOID", "SEARCH CALL", "PAUSE", "SELECT TG",
		"OFF", "ON", "Reset", "Play", "Stop", "Download mode", "First page", "Previous page", "Next page", "Last page",
		"PATCH", "DELAYED", "NO LINK", "Server status:", "L:", "Q:",
		"Sort order", "Ascending", "Descending", "Date", "System", "Talkgroup", "Group", "Tag",
		"Audio Conversion", "Auto Populate", "Branding Label", "Disable Duplicate Call Detection", "Email Support",
		"Keypad Beep Style", "Max Clients", "Prune Days", "Show Listeners Count", "Sort Talkgroups",
		"Queue:", "Playing", "Paused", "Stopped", "Page", "Searching calls",
	} {
		if !strings.Contains(spoken, want) {
			t.Errorf("the reference chapter never prints %q, which is a name the shipped application puts in front of the reader", want)
		}
	}

	for _, caption := range []string{
		"The nine controls on the control bar, what a press on each one does, and where this guide operates it.",
		"The controls the two panels add, what each one does, and where this guide operates it.",
		"The flags the display carries beside a call, what has to be true for each to appear, and where this guide operates it.",
		"The lights on the screen, what each one says, and where this guide operates it.",
		"The search panel's criteria, what each one holds, and where this guide operates it.",
		"The settings the operator chooses, and what each one does to the screen a listener is looking at.",
		"The sentences the page says out loud for a screen reader, what each one is for, and where this guide operates it.",
	} {
		if !strings.Contains(chapter, "<caption>"+caption+"</caption>") {
			t.Errorf("the reference chapter has no table captioned %q, which is one of the lists a reader comes back to scan", caption)
		}
	}

	defs := regexp.MustCompile(`(?is)<dt\b[^>]*>(.*?)</dt>\s*<dd\b[^>]*>(.*?)</dd>`).FindAllStringSubmatch(chapter, -1)

	if len(defs) < 8 {
		t.Errorf("the reference chapter defines %d of the application's words, and the words a reader meets across the guide need at least 8", len(defs))
	}

	for _, def := range defs {
		word := strings.Join(strings.Fields(stripTags(def[1])), " ")

		if !strings.Contains(def[1], "<dfn") {
			t.Errorf("the reference chapter defines %q without marking the word being defined, so a reader has nothing to hold it by", word)
		}

		if len(strings.Fields(stripTags(def[2]))) < 5 {
			t.Errorf("the definition of %q is shorter than a sentence", word)
		}
	}

	// The type is one of the words a reader sees written on the display, and this is the chapter the guide sends a
	// reader to for the words the application uses. A draft of the glossary defined nineteen words and left this
	// one out, while the opening chapter's account of the change pointed a reader here and the chapter on the
	// display kept its reading to itself.
	if !strings.Contains(chapter, "<dfn>type</dfn>") {
		t.Errorf("the reference chapter does not define the word type, which the display writes and the opening chapter sends a reader here to find")
	}

	hrefs := map[string]bool{}

	for _, m := range regexp.MustCompile(`(?is)href="([^"]*)"`).FindAllStringSubmatch(chapter, -1) {
		if isUserGuideChapter(strings.TrimPrefix(m[1], "#")) {
			hrefs[m[1]] = true
		}
	}

	if len(hrefs) < 5 {
		t.Errorf("the reference chapter points back at %d chapters of this guide, and everything it lists was operated in at least 5 of them", len(hrefs))
	}

	// The paused reading of the LED is a place this guide already superseded once: the shipped state
	// alternates, and the ring is what a reduced-motion operating system asks for. The reference chapter restates the
	// state briefly, so the refusal belongs here too rather than only in the chapter that operates the light.
	for _, superseded := range []string{
		"a lit face inside a ring",
		"the ring blinks",
		"always blinking",
	} {
		if strings.Contains(spoken, superseded) {
			t.Errorf("the reference chapter states %q about the LED, which is the reading this guide superseded: the shipped state alternates, and the ring is what reduced motion asks for", superseded)
		}
	}

	if !strings.Contains(spoken, "less motion") {
		t.Errorf("the reference chapter states the paused reading of the LED without saying that the ring is what an operating system asking for less motion gets")
	}

	if figures := regexp.MustCompile(`(?is)<figure data-shot="`).FindAllString(chapter, -1); len(figures) != 0 {
		t.Errorf("the reference chapter carries a figure, and the inventory gives it none: a reader scanning a reference is reading names and columns, not a picture")
	}
}

// The guide's tables gather the names the listening page puts on its own screen. This check reads the components
// those names come from and compares them with the tables a reader scans, the way the operator guide's tables are
// compared with the server's own flag set. Both directions are held because both go wrong: a name the guide
// carries that the page never renders is a name a listener looks for and does not find, and a name the page
// renders that the guide leaves out is a name the reader has to guess at. Where a table's own caption promises the
// order the screen puts its rows in, the order is compared too. Every failure names the name that differs, because
// a count of disagreeing rows tells a reader nothing they can act on.
//
// Each inventory is read from a closed structure in the shipped sources: the control bar's own template, the search
// panel's form and the heading row of its result table, the display's flag row and the words it writes into its own
// rows, the fixed names the page's components put on their controls, the sentences the components say out loud, and
// the words the display heads the columns of the table under it. The four paginator names come from the shared
// component the search panel builds its paginator from, because the search panel asks that component for the
// four-control shape and those four names are what the paginator the reader sees carries.
func TestTheNamesTheGuideTabulatesAreTheNamesTheScannerPagePrints(t *testing.T) {
	body := userGuidePath(t)

	// The controls, in the rows the screen puts them in, left to right within each row. The chapter about the main
	// screen promises that arrangement in its own caption, so its rows are compared one for one; the reference
	// chapter gathers the same names for a reader who only needs one of them.
	bar := controlBarRows(t)

	barNames := []string{}
	for _, row := range bar {
		barNames = append(barNames, row...)
	}

	arranged := guideTableRows(tableWithCaption(t, body, "The controls as they sit on the screen, top row first, left to right within each row."))

	for index, screen := range bar {
		if index >= len(arranged) {
			t.Errorf("the control bar puts %s in a row the guide's arrangement has no row for: the guide names %d rows and the screen %d",
				strings.Join(screen, ", "), len(arranged), len(bar))

			continue
		}

		got := strings.Join(codeSpansIn(strings.Join(arranged[index], " ")), ", ")

		if got != strings.Join(screen, ", ") {
			t.Errorf("the guide's control arrangement says the row it names %q holds %s; the control bar puts %s in that row",
				words(arranged[index][0]), saidList(codeSpansIn(strings.Join(arranged[index], " "))), strings.Join(screen, ", "))
		}
	}

	compareNames(t, "reference chapter's control table", "the control bar", barNames,
		codeSpansIn(rowHeaders(tableWithCaption(t, body, "The nine controls on the control bar, what a press on each one does, and where this guide operates it."))))

	// The criteria the search panel's form offers, and the order the form puts them in. The chapter operating the
	// search panel promises the order in its caption; the reference chapter only gathers the names.
	criteria := searchFormNames(t)

	criteriaChapter := tableWithCaption(t, body, "The search panel's criteria, in the order the form puts them.")

	compareNames(t, "search chapter's criteria table", "the search panel's form", criteria,
		rowNames(guideTableRows(criteriaChapter), printedName))
	compareOrder(t, "search chapter's criteria table", "the search panel's form", criteria,
		rowNames(guideTableRows(criteriaChapter), printedName))

	criteriaReference := tableWithCaption(t, body, "The search panel's criteria, what each one holds, and where this guide operates it.")

	compareNames(t, "reference chapter's criteria table", "the search panel's form", criteria,
		rowNames(guideTableRows(criteriaReference), printedName))

	// The entries a listener sees in each criterion before touching anything: the entry that searches every one of
	// a thing, and the choice the one criterion that preselects opens with.
	openings := searchFormOpenings(t)

	compareWords(t, "search chapter's criteria table", "the search panel's form", openings, bodyWords(criteriaChapter))
	compareWords(t, "reference chapter's criteria table", "the search panel's form", openings, bodyWords(criteriaReference))

	// The headings of the result table, in the order the columns come. The shipped table leaves its first column
	// without a heading, and the guide's row for that column says so in words, which is all there is to say about
	// it: a guide that invented a heading for it is named as inventing one.
	columns := searchColumnNames(t)

	resultRows := tableWithCaption(t, body, "What the search panel's result rows hold, in the order the columns come.")

	compareNames(t, "search chapter's result table", "the search panel's result table", columns,
		rowNames(guideTableRows(resultRows), columnHeadedName))
	compareOrder(t, "search chapter's result table", "the search panel's result table", columns,
		rowNames(guideTableRows(resultRows), columnHeadedName))

	// The flags the display carries beside a call. The chapter operating the display and the reference chapter both
	// tabulate them, and neither promises an order.
	flags := displayFlagNames(t)

	compareNames(t, "display chapter's flag table", "the display", flags,
		rowNames(guideTableRows(tableWithCaption(t, body, "The display's flags, and what has to be true for each of them to be lit.")), printedName))
	compareNames(t, "reference chapter's flag table", "the display", flags,
		rowNames(guideTableRows(tableWithCaption(t, body, "The flags the display carries beside a call, what has to be true for each to appear, and where this guide operates it.")), printedName))

	// The controls the two panels add. The criteria the form offers and the headings of the result table are bound
	// by the checks above, so they are not gathered here as well.
	compareNames(t, "reference chapter's panel table", "the panel chrome", panelControlNames(t),
		codeSpansIn(rowHeaders(tableWithCaption(t, body, "The controls the two panels add, what each one does, and where this guide operates it."))))

	// The buttons the select panel itself prints words on. The buttons naming a group or a talkgroup take their
	// words from the configuration rather than from the application, so no fixed word is theirs to compare, and the
	// guide describes those two in words of its own.
	compareNames(t, "select chapter's button table", "the select panel", selectPanelButtons(t),
		codeSpansIn(rowHeaders(tableWithCaption(t, body, "What the select panel's buttons show, and what a press on each one does."))))

	// What the unlock field says when the server refuses the code a listener offers.
	compareNames(t, "access code chapter's table", "the display's unlock field", unlockReadings(t),
		rowNames(guideTableRows(tableWithCaption(t, body, "What the unlock field says, what the server actually answered, and what is worth doing about it.")), printedName))

	// The sentences the page says out loud for a screen reader. The reference chapter gathers them as sentences,
	// and one of them is built entirely out of a reading, so the words each sentence holds fixed are looked for in
	// the table's own words rather than demanded as row headers. What the table names as a printed sentence is held
	// to the sentences the components actually announce.
	announcements := announcedSentences(t)
	announcementTable := tableWithCaption(t, body, "The sentences the page says out loud for a screen reader, what each one is for, and where this guide operates it.")

	compareWords(t, "reference chapter's announcement table", "the page's own live region", announcements,
		bodyWords(announcementTable))

	for _, name := range codeSpansIn(rowHeaders(announcementTable)) {
		if !holds(announcements, name) {
			t.Errorf("the guide's announcement table names %q as a sentence the page says out loud, which no component of the listening page announces", name)
		}
	}

	// The words the display writes into its own rows before any call has arrived. Its rows table is a description
	// rather than a list of names, so the readings are looked for in the table's words, and a guide that leaves one
	// out leaves the reader guessing what a row says while it is waiting.
	compareWords(t, "display chapter's rows table", "the display", displayReadings(t),
		bodyWords(tableWithCaption(t, body, "The display's rows, what each holds before any call, and what it holds once a call is on the screen.")))

	// The headings of the table that sits under the display, read where the guide operates that table.
	history := guideSection(t, userGuideChapter(t, body, "ch-06-what-the-display-says"), "The history table")

	for _, name := range historyHeadings(t) {
		if !holds(codeSpansIn(history), name) {
			t.Errorf("the display heads a column of the table under it %q, which the guide's section about that table never names as a printed name", name)
		}
	}
}

// The reference chapter's settings table gathers the settings a listener never chooses but feels on the screen, and
// says what each one is as shipped. These are the server's own settings, so the table is measured against the
// administration interface's own list of them and against the values the server starts with, the way the operator
// guide's flag tables are measured against the server's flags. A setting the guide names that the server does not
// define, a defined setting the guide leaves out, and a shipped position the guide states wrongly are each named by
// the setting they concern. A default that moves and a guide still reading the old one is a sentence that has
// quietly become untrue, which is what this check is for.
func TestTheSettingsTheGuideTabulatesAreTheSettingsTheServerDefines(t *testing.T) {
	body := userGuidePath(t)

	table := tableWithCaption(t, body, "The settings the operator chooses, and what each one does to the screen a listener is looking at.")

	column := shippedPositionColumn(t, table)

	stated := map[string]string{}
	titled := map[string]bool{}

	for _, row := range guideTableRows(table) {
		titles := codeSpansIn(row[0])

		if len(titles) == 0 {
			t.Errorf("the guide's settings table has a row naming no setting: %s", words(row[0]))

			continue
		}

		titled[titles[0]] = true

		if column < 0 {
			continue
		}

		if column >= len(row) {
			t.Errorf("the guide's settings table names %q in a row with no cell saying what the setting is as shipped", titles[0])

			continue
		}

		stated[titles[0]] = words(row[column])
	}

	settings := settingDefaults(t)

	defined := map[string]bool{}

	for _, setting := range settings {
		defined[setting.title] = true

		if !titled[setting.title] {
			t.Errorf("the server defines the %q setting the administration interface offers, which the guide's settings table never names", setting.title)

			continue
		}

		// A table with no column for what a setting is as shipped is already reported as itself, which is the fault
		// a reader needs naming.
		if column < 0 {
			continue
		}

		if !statesShippedPosition(t, setting, stated[setting.title]) {
			t.Errorf("the guide's settings table says the %q setting is %s; the server starts with %s",
				setting.title, said(stated[setting.title]), readsAsShipped(setting))
		}
	}

	for title := range titled {
		if !defined[title] {
			t.Errorf("the guide's settings table names %q, which is no setting the administration interface offers or the server defines", title)
		}
	}
}

// listenerComponentSource reads one of the components the listening page is built from. The names held against
// these sources are the ones the components write into the screen themselves.
func listenerComponentSource(t *testing.T, name string) string {
	t.Helper()

	return repoFile(t, "internal", "presentation", "scanner", "frontend", "presentation", "listener", name)
}

// sharedComponentSource reads the component both webapps build their controls from.
func sharedComponentSource(t *testing.T) string {
	t.Helper()

	return repoFile(t, "internal", "presentation", "common", "frontend", "presentation", "ui", "primitives.js")
}

// printedLabel turns a label a component writes into the words a reader sees. The control bar splits a label over
// two lines, and a listener reads it as the one label it names.
func printedLabel(label string) string {
	return strings.Join(strings.Fields(regexp.MustCompile(`(?is)<[^>]*>`).ReplaceAllString(label, " ")), " ")
}

// codeSpansIn reads the names a piece of the guide sets out as words the application prints.
func codeSpansIn(fragment string) []string {
	names := []string{}

	for _, span := range regexp.MustCompile(`(?is)<code>(.*?)</code>`).FindAllStringSubmatch(fragment, -1) {
		names = append(names, strings.Join(strings.Fields(span[1]), " "))
	}

	return names
}

// holds reports whether a list of names carries one.
func holds(names []string, name string) bool {
	for _, have := range names {
		if have == name {
			return true
		}
	}

	return false
}

// said reads a name, or the words saying nothing was there, so a failure never shows an empty quote where a reader
// needs to know the guide printed nothing.
func said(name string) string {
	if name == "" {
		return "nothing"
	}

	return strconv.Quote(name)
}

// saidList reads the names a guide table carries, or the words saying it carries none.
func saidList(names []string) string {
	if len(names) == 0 {
		return "no printed names at all"
	}

	return strings.Join(names, ", ")
}

// guideTableRows reads a guide table's body rows: each row's cells in the order they come, the row header first.
// The header row is left out because it names the columns rather than a row.
func guideTableRows(table string) [][]string {
	rows := [][]string{}

	for _, row := range regexp.MustCompile(`(?is)<tr\b[^>]*>.*?</tr>`).FindAllString(table, -1) {
		if strings.Contains(row, `scope="col"`) {
			continue
		}

		cells := []string{}

		for _, cell := range regexp.MustCompile(`(?is)<t[hd]\b[^>]*>(.*?)</t[hd]>`).FindAllStringSubmatch(row, -1) {
			cells = append(cells, cell[1])
		}

		if len(cells) > 0 {
			rows = append(rows, cells)
		}
	}

	return rows
}

// rowHeaders joins the cells that name each row of a guide table, which are the cells carrying the names the table
// promises.
func rowHeaders(table string) string {
	headers := []string{}

	for _, row := range guideTableRows(table) {
		headers = append(headers, row[0])
	}

	return strings.Join(headers, " ")
}

// bodyWords leaves the words a table's body rows carry. Its caption and its column headers name the table and its
// columns, and are not what a row promises.
func bodyWords(table string) string {
	parts := []string{}

	for _, row := range guideTableRows(table) {
		parts = append(parts, words(strings.Join(row, " ")))
	}

	return strings.Join(parts, " ")
}

// rowNames reads the name each of a table's body rows puts in front of the reader. nameOf says how a row names the
// thing it is about.
func rowNames(rows [][]string, nameOf func([]string) string) []string {
	names := []string{}

	for _, row := range rows {
		names = append(names, nameOf(row))
	}

	return names
}

// printedName reads the one printed name a row header carries, for the tables whose rows are one name each. A row
// naming nothing returns nothing, and the comparison says so by name.
func printedName(row []string) string {
	names := codeSpansIn(row[0])

	if len(names) == 0 {
		return ""
	}

	return names[0]
}

// columnHeadedName reads the name a row carries for a column of a table. The one column a shipped table leaves
// without a heading is named by the guide in words, because that is all there is to say about it.
func columnHeadedName(row []string) string {
	if strings.Contains(strings.ToLower(words(row[0])), "no heading") {
		return ""
	}

	return printedName(row)
}

// uniqueNames drops the repeats a table leaves behind when one printed name answers two rows, which is what the
// select panel's pair does in a chapter about that panel and in a table gathering the panels' controls.
func uniqueNames(names []string) []string {
	unique := []string{}
	seen := map[string]bool{}

	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}

		seen[name] = true
		unique = append(unique, name)
	}

	return unique
}

// compareNames holds one guide table to the printed names the listening application puts on its screen. It reports
// the differing name in either direction.
func compareNames(t *testing.T, table, screen string, want, got []string) {
	offered := map[string]bool{}
	for _, name := range want {
		offered[name] = true
	}

	for _, name := range uniqueNames(got) {
		if !offered[name] {
			t.Errorf("the guide's %s names %q, which %s never puts on its screen", table, name, screen)
		}
	}

	carried := map[string]bool{}
	for _, name := range got {
		carried[name] = true
	}

	for _, name := range want {
		if !carried[name] {
			t.Errorf("%s puts %q on its screen, which the guide's %s never names", screen, name, table)
		}
	}
}

// compareOrder holds one guide table to the order the screen puts its names in, for the tables whose own caption
// promises that order. It names where the two part rather than counting how far apart they are.
func compareOrder(t *testing.T, table, screen string, want, got []string) {
	if len(want) != len(got) {
		return
	}

	for index, name := range want {
		if got[index] != name {
			t.Errorf("the guide's %s puts %s in the place %s puts it: %q", table, said(got[index]), screen, name)
		}
	}
}

// compareWords holds a guide table to the words an application writes into what it shows, for the readings the
// guide describes in sentences rather than naming row by row.
func compareWords(t *testing.T, table, screen string, want []string, text string) {
	for _, word := range want {
		if !strings.Contains(text, word) {
			t.Errorf("the guide's %s never carries %q, which %s writes into what it shows", table, word, screen)
		}
	}
}

// controlBarRows reads the control bar's buttons from the component that renders them, grouped into the rows the
// screen puts them in and left to right within each row, which is the order a reader sees them in.
func controlBarRows(t *testing.T) [][]string {
	template := regexp.MustCompile("(?s)this.innerHTML = `(.*?)`;").FindStringSubmatch(listenerComponentSource(t, "rdio-controls.js"))

	if template == nil {
		t.Fatalf("the control bar component does not read as the markup it renders")
	}

	rows := [][]string{}

	for _, chunk := range strings.Split(template[1], `<div class="row">`)[1:] {
		names := []string{}

		for _, button := range regexp.MustCompile(`data-action="[a-zA-Z]+">(.*?)</button>`).FindAllStringSubmatch(chunk, -1) {
			names = append(names, printedLabel(button[1]))
		}

		if len(names) > 0 {
			rows = append(rows, names)
		}
	}

	if len(rows) == 0 {
		t.Fatalf("the control bar component renders no rows of controls")
	}

	return rows
}

// searchFormNames reads the criteria the search panel's form offers, in the order the form puts them.
func searchFormNames(t *testing.T) []string {
	names := []string{}

	for _, label := range regexp.MustCompile(`(?is)<label>([^<]*)</label>`).FindAllStringSubmatch(listenerComponentSource(t, "rdio-search-panel.js"), -1) {
		names = append(names, printedLabel(label[1]))
	}

	if len(names) == 0 {
		t.Fatalf("the search panel component writes no criteria into its form")
	}

	return names
}

// searchFormOpenings reads the entries a listener sees in the criteria before touching anything: the entry that
// searches every one of a thing, and the choice the one criterion that preselects opens with.
func searchFormOpenings(t *testing.T) []string {
	source := listenerComponentSource(t, "rdio-search-panel.js")

	names := []string{}

	for _, entry := range regexp.MustCompile(`(?is)<option value="">([^<]+)</option>`).FindAllStringSubmatch(source, -1) {
		// An entry the panel fills in from the configuration carries no fixed word of its own, and the guide names
		// none for it either.
		if strings.Contains(entry[1], "${") {
			continue
		}

		names = append(names, printedLabel(entry[1]))
	}

	for _, entry := range regexp.MustCompile(`(?is)<option value="[^"]+" selected>([^<]+)</option>`).FindAllStringSubmatch(source, -1) {
		if strings.Contains(entry[1], "${") {
			continue
		}

		names = append(names, printedLabel(entry[1]))
	}

	if len(names) == 0 {
		t.Fatalf("the search panel's criteria carry no openings")
	}

	return uniqueNames(names)
}

// searchColumnNames reads the headings of the search panel's result table in the order the columns come, keeping the
// first column the shipped table leaves without a heading so that the guide's row for it is compared in its place
// rather than dropped from the table.
func searchColumnNames(t *testing.T) []string {
	thead := regexp.MustCompile(`(?is)<thead>(.*?)</thead>`).FindStringSubmatch(listenerComponentSource(t, "rdio-search-panel.js"))

	if thead == nil {
		t.Fatalf("the search panel renders no result table")
	}

	names := []string{}

	for _, cell := range regexp.MustCompile(`(?is)<th\b[^>]*>(.*?)</th>`).FindAllStringSubmatch(thead[1], -1) {
		names = append(names, printedLabel(cell[1]))
	}

	return names
}

// displayFlagNames reads the flags the display carries beside a call: the three it prints into its markup, and the
// mark it writes for the timed part of the avoid cycle, which has no place in the markup because it is written only
// while that part is running.
func displayFlagNames(t *testing.T) []string {
	source := listenerComponentSource(t, "rdio-display.js")

	names := []string{}

	for _, flag := range regexp.MustCompile(`(?is)<span class="flag flag-[a-z]+">(.*?)</span>`).FindAllStringSubmatch(source, -1) {
		if name := printedLabel(flag[1]); name != "" {
			names = append(names, name)
		}
	}

	// The mark exists nowhere in the markup, because the component writes it only while the timed part of the
	// cycle is running: it is the fixed head of the reading the component writes.
	const write = "tempAvoid.textContent = minutes ? `"

	at := strings.Index(source, write)

	if at < 0 {
		t.Fatalf("the display writes no mark for the timed part of the avoid cycle")
	}

	head := source[at+len(write):]

	if stop := strings.IndexAny(head, "$`"); stop >= 0 {
		head = head[:stop]
	}

	names = append(names, strings.TrimSpace(head))

	return names
}

// panelControlNames reads the controls the two panels add, from the components that render them and from the page
// shell that renders the bars the panels open on: the select panel's pair, the search panel's switch, its reset, and
// the one control its result rows carry in the shapes it takes, the names the paginator the search panel asks the
// shared component for, and the two controls that shut the panels.
func panelControlNames(t *testing.T) []string {
	names := []string{}

	for _, control := range regexp.MustCompile(`>(OFF|ON)<`).FindAllStringSubmatch(listenerComponentSource(t, "rdio-select-panel.js"), -1) {
		names = append(names, control[1])
	}

	search := listenerComponentSource(t, "rdio-search-panel.js")

	for _, control := range regexp.MustCompile(`aria-label="([^"$]+)"`).FindAllStringSubmatch(search, -1) {
		names = append(names, control[1])
	}

	for _, control := range regexp.MustCompile(`>(Reset)<`).FindAllStringSubmatch(search, -1) {
		names = append(names, control[1])
	}

	paginator := regexp.MustCompile("(?s)const paginatorControls = \\(material\\) =>(.*?\\];)").FindStringSubmatch(sharedComponentSource(t))

	if paginator == nil {
		t.Fatalf("the shared component writes no paginator for the search panel to ask for")
	}

	for _, control := range regexp.MustCompile(`\[('[a-z]+', )?'[^']*', '([A-Z][^']*)'\]`).FindAllStringSubmatch(paginator[1], -1) {
		names = append(names, control[2])
	}

	for _, control := range regexp.MustCompile(`panel-close[^>]*aria-label="([^"$]+)"`).FindAllStringSubmatch(listenerComponentSource(t, "rdio-app.js"), -1) {
		names = append(names, control[1])
	}

	names = uniqueNames(names)

	if len(names) == 0 {
		t.Fatalf("the two panels render no control the check can name")
	}

	return names
}

// selectPanelButtons reads the buttons the select panel prints words on itself.
func selectPanelButtons(t *testing.T) []string {
	names := uniqueNames([]string{})

	for _, control := range regexp.MustCompile(`>(OFF|ON)<`).FindAllStringSubmatch(listenerComponentSource(t, "rdio-select-panel.js"), -1) {
		names = append(names, control[1])
	}

	if len(names) == 0 {
		t.Fatalf("the select panel renders no button it names in words")
	}

	return names
}

// unlockReadings reads what the display writes into the unlock field when the server refuses the code a listener
// offers.
func unlockReadings(t *testing.T) []string {
	names := []string{}

	for _, message := range regexp.MustCompile(`setError\('([^']+)'\)`).FindAllStringSubmatch(listenerComponentSource(t, "rdio-display.js"), -1) {
		names = append(names, message[1])
	}

	if len(names) == 0 {
		t.Fatalf("the display never refuses an unlock code in words")
	}

	return names
}

// announcedSentences reads the words the page says out loud for a screen reader: every sentence the components of
// the listening page announce, and the words each sentence holds fixed while the rest of it is filled in from the
// reading the screen is showing.
func announcedSentences(t *testing.T) []string {
	sentences := []string{}

	for _, component := range []string{"rdio-display.js", "rdio-search-panel.js"} {
		for _, call := range regexp.MustCompile(`announce\((.*?)\);`).FindAllStringSubmatch(listenerComponentSource(t, component), -1) {
			sentences = append(sentences, fixedWords(call[1])...)
		}
	}

	sentences = uniqueNames(sentences)

	if len(sentences) == 0 {
		t.Fatalf("no component of the listening page announces anything")
	}

	return sentences
}

// fixedWords returns the words an announced sentence holds fixed. What is filled in is a reading the guide operates
// where it operates the display and the search results, and a sentence built entirely out of readings holds no
// fixed word to compare.
func fixedWords(expression string) []string {
	expression = strings.TrimSpace(expression)

	// A sentence the components write as one string carries its own words whole. Go's unquoting reads a
	// single-quoted literal as one rune, which is not what the components write, so the quotes are taken off here.
	if len(expression) >= 2 &&
		((strings.HasPrefix(expression, "'") && strings.HasSuffix(expression, "'")) ||
			(strings.HasPrefix(expression, `"`) && strings.HasSuffix(expression, `"`))) {
		return []string{strings.TrimSpace(expression[1 : len(expression)-1])}
	}

	found := []string{}

	for _, part := range regexp.MustCompile(`\$\{[^}]*\}`).Split(strings.Trim(expression, "`"), -1) {
		if word := strings.TrimSpace(part); word != "" {
			found = append(found, word)
		}
	}

	for _, part := range regexp.MustCompile(`'([^']*)'`).FindAllStringSubmatch(expression, -1) {
		if word := strings.TrimSpace(part[1]); word != "" {
			found = append(found, word)
		}
	}

	return found
}

// displayReadings reads the words the display writes into its own rows as plain words: what a row holds before any
// call has arrived. A reading built out of a number or a name is filled in from a call, and describing it is the
// display chapter's work rather than naming it.
func displayReadings(t *testing.T) []string {
	found := []string{}

	for _, write := range regexp.MustCompile(`\.textContent = '([^']*)'`).FindAllStringSubmatch(listenerComponentSource(t, "rdio-display.js"), -1) {
		if word := strings.TrimSpace(write[1]); word != "" {
			found = append(found, word)
		}
	}

	found = uniqueNames(found)

	if len(found) == 0 {
		t.Fatalf("the display writes nothing into its rows as a plain word")
	}

	return found
}

// historyHeadings reads the headings of the table the display renders under its rows.
func historyHeadings(t *testing.T) []string {
	thead := regexp.MustCompile(`(?is)<table class="history">(.*?)</thead>`).FindStringSubmatch(listenerComponentSource(t, "rdio-display.js"))

	if thead == nil {
		t.Fatalf("the display renders no table of recently heard calls")
	}

	names := []string{}

	for _, cell := range regexp.MustCompile(`(?is)<th\b[^>]*>(.*?)</th>`).FindAllStringSubmatch(thead[1], -1) {
		if name := printedLabel(cell[1]); name != "" {
			names = append(names, name)
		}
	}

	return names
}

// guideSection reads the part of a chapter under one of its own headings, up to the next heading or the end of the
// chapter. A check binding a promise to the part of the guide that operates something reads that part, so a name
// earned somewhere else in the chapter does not answer for it here.
func guideSection(t *testing.T, chapter, heading string) string {
	headings := regexp.MustCompile(`(?is)<h3\b[^>]*>(.*?)</h3>`)

	bounds := headings.FindAllStringSubmatchIndex(chapter, -1)

	end := len(chapter)

	if closer := strings.Index(chapter, "</section>"); closer >= 0 {
		end = closer
	}

	for index, bound := range bounds {
		if words(chapter[bound[2]:bound[3]]) != heading {
			continue
		}

		if index+1 < len(bounds) && bounds[index+1][0] < end {
			end = bounds[index+1][0]
		}

		return chapter[bound[1]:end]
	}

	t.Fatalf("the guide has no section headed %q, so this check proves nothing", heading)

	return ""
}

// settingDefault is one setting the administration interface offers, the option the server stores it in, and the
// value the server starts with.
type settingDefault struct {
	title string
	field string
	value string
	label string
}

// settingDefaults reads the settings the administration interface lists, pairs each with the option the domain
// declares for it, and resolves the value the server starts with. The settings a listener never chooses are the
// server's own, so what the guide says about them is measured against these values.
func settingDefaults(t *testing.T) []settingDefault {
	source := repoFile(t, "internal", "presentation", "admin", "frontend", "presentation", "admin", "rdio-admin-options.js")

	controls := regexp.MustCompile("(?s)const CONTROLS = \\[(.*?)\n\\];").FindStringSubmatch(source)

	if controls == nil {
		t.Fatalf("the administration interface's settings do not read as a list")
	}

	keys := []string{}
	for _, key := range regexp.MustCompile(`key: '([a-zA-Z]+)'`).FindAllStringSubmatch(controls[1], -1) {
		keys = append(keys, key[1])
	}

	titles := []string{}
	for _, title := range regexp.MustCompile(`title: '([^']+)'`).FindAllStringSubmatch(controls[1], -1) {
		titles = append(titles, title[1])
	}

	if len(keys) != len(titles) {
		t.Fatalf("the administration interface lists %d settings keys and %d settings titles, so the settings do not read as pairs", len(keys), len(titles))
	}

	fields := optionsFields(t)
	consts := optionConstants(t)
	started := startedOptions(t)

	settings := []settingDefault{}

	for index, key := range keys {
		field := strings.ToUpper(key[:1]) + key[1:]

		kind, declared := fields[field]

		if !declared {
			t.Fatalf("the administration interface offers %q, which no option named %q holds", key, field)
		}

		expression, stated := started[field]

		if !stated {
			expression = zeroOf(kind)
		}

		value, ok := resolveDefault(expression, consts, 0)

		if !ok {
			t.Fatalf("the server's own starting value for %q reads as %q, which resolves to nothing the guide can be compared with", field, expression)
		}

		settings = append(settings, settingDefault{
			title: titles[index],
			field: field,
			value: value,
			label: optionLabel(source, key, value),
		})
	}

	if len(settings) == 0 {
		t.Fatalf("the administration interface offers no settings")
	}

	return settings
}

// optionsFields reads the options the domain declares, with the kind each one holds, so a setting the server
// starts with nothing in is read as that kind's nothing rather than as missing.
func optionsFields(t *testing.T) map[string]string {
	declared := regexp.MustCompile("(?s)type Options struct \\{(.*?)\n\\}").FindStringSubmatch(repoFile(t, "internal", "domain", "options.go"))

	if declared == nil {
		t.Fatalf("the domain declares no options")
	}

	fields := map[string]string{}

	for _, field := range regexp.MustCompile(`(?m)^\s*([A-Z][A-Za-z]*)\s+(bool|string|uint|int)\s*$`).FindAllStringSubmatch(declared[1], -1) {
		fields[field[1]] = field[2]
	}

	return fields
}

// optionConstants reads the constants the domain writes its option values with, so a default named by a constant is
// read as the value the server starts with.
func optionConstants(t *testing.T) map[string]string {
	source := repoFile(t, "internal", "domain", "options.go")

	consts := map[string]string{}

	for _, block := range regexp.MustCompile(`(?s)const \((.*?)\n\)`).FindAllStringSubmatch(source, -1) {
		for _, line := range strings.Split(block[1], "\n") {
			name := regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*(?:uint|string|bool|int)?\s*=\s*(.+)$`).FindStringSubmatch(strings.TrimSpace(line))

			if len(name) == 3 {
				consts[name[1]] = strings.TrimSpace(name[2])
			}
		}
	}

	for _, single := range regexp.MustCompile(`(?m)^const ([A-Za-z_][A-Za-z0-9_]*)\s*(?:uint|string|bool|int)?\s*=\s*(.+)$`).FindAllStringSubmatch(source, -1) {
		consts[single[1]] = strings.TrimSpace(single[2])
	}

	return consts
}

// startedOptions reads the values the server gives its options before an operator changes any of them. An option
// left out of that list is started with nothing in it.
func startedOptions(t *testing.T) map[string]string {
	source := repoFile(t, "internal", "domain", "options.go")

	body := regexp.MustCompile("(?s)func DefaultOptions\\(\\) Options \\{\n\treturn Options\\{(.*?)\n\\}").FindStringSubmatch(source)

	if body == nil {
		t.Fatalf("the domain writes no starting values for its options")
	}

	started := map[string]string{}

	for _, field := range regexp.MustCompile(`([A-Z][A-Za-z]*):\s+(.*),`).FindAllStringSubmatch(body[1], -1) {
		started[field[1]] = strings.TrimSpace(field[2])
	}

	return started
}

// zeroOf names the nothing a kind of option starts with when the server names no value for it.
func zeroOf(kind string) string {
	switch kind {
	case "bool":
		return "false"
	case "uint", "int":
		return "0"
	default:
		return `""`
	}
}

// optionLabel reads the words the administration interface prints for one value of one setting. A listener chooses
// between those words, so the guide names them rather than the value the server stores.
func optionLabel(source, key, value string) string {
	control := regexp.MustCompile("(?s)key: '" + regexp.QuoteMeta(key) + "'.*?options: \\[(.*?)\n\\s*\\]").FindStringSubmatch(source)

	if control == nil {
		return ""
	}

	label := regexp.MustCompile(`\[` + regexp.QuoteMeta("'"+value+"'") + `, '([^']+)'\]`).FindStringSubmatch(control[1])

	if label == nil {
		return ""
	}

	return label[1]
}

// shippedPositionColumn finds the column of the guide's settings table saying what each setting is as shipped. The
// guide's tables name every column in their header row, the column of row headers included, so a header cell and the
// cells beneath it hold the same index. A table without that column says nothing a default can be compared with.
func shippedPositionColumn(t *testing.T, table string) int {
	head := regexp.MustCompile(`(?is)<thead\b[^>]*>(.*?)</thead>`).FindStringSubmatch(table)

	if head == nil {
		t.Fatalf("the guide's settings table has no header row")
	}

	for index, cell := range regexp.MustCompile(`(?is)<th\b[^>]*>(.*?)</th>`).FindAllStringSubmatch(head[1], -1) {
		if words(cell[1]) == "As shipped" {
			return index
		}
	}

	t.Errorf("the guide's settings table carries no column saying what each setting is as shipped, so no value the server starts with can be compared with it")

	return -1
}

// conversionReadings are the words the guide's settings table uses for the recordings a listener hears, for each
// mode the server starts conversion in. The guide writes the listener's words rather than the names the
// administration interface prints, because a listener is choosing what they will hear and not configuring a server.
var conversionReadings = map[string]string{
	"0": "left as they arrived",
	"1": "converted copies, not levelled",
	"2": "converted copies, levelled",
	"3": "converted copies, levelled",
}

// statesShippedPosition reports whether the guide's table says what a setting is as shipped in the words the value
// the server starts with calls for. Each rule is the shape the guide's cell answers to: a switch answers on or off,
// the retention is a number of days or the pass switched off, the client cap is the number the server names, the
// conversion setting is what it does to the recordings, a setting started with nothing in it is named as nothing
// set, and a setting whose values are printed as choices is named by the choice the interface prints. An option the
// guide's table reaches for with no rule here is a decision somebody has to make on purpose.
func statesShippedPosition(t *testing.T, setting settingDefault, cell string) bool {
	t.Helper()

	cell = words(cell)

	switch setting.field {
	case "AutoPopulate", "DisableDuplicateDetection", "ShowListenersCount", "SortTalkgroups":
		if setting.value == "true" {
			return strings.HasPrefix(cell, "On")
		}

		return strings.HasPrefix(cell, "Off")

	case "PruneDays":
		if setting.value == "0" {
			return strings.HasPrefix(cell, "Off")
		}

		return strings.Contains(cell, setting.value+" days")

	case "MaxClients":
		return strings.Contains(cell, setting.value)

	case "AudioConversion":
		reading, known := conversionReadings[setting.value]

		return known && strings.Contains(strings.ToLower(cell), strings.ToLower(reading))

	case "Branding", "Email":
		return setting.value == "" && strings.Contains(strings.ToLower(cell), "nothing set")

	case "KeypadBeeps":
		return setting.label != "" && strings.Contains(cell, setting.label)
	}

	t.Fatalf("the guide's settings table names %q, and this check has no rule for what the server starting with %q looks like in the guide's words", setting.title, setting.value)

	return false
}

// readsAsShipped says what the server starts with in the words the guide's cell has to match, so a failure tells a
// reader which side moved.
func readsAsShipped(setting settingDefault) string {
	switch setting.field {
	case "AutoPopulate", "DisableDuplicateDetection", "ShowListenersCount", "SortTalkgroups":
		if setting.value == "true" {
			return "the setting on"
		}

		return "the setting off"

	case "PruneDays":
		if setting.value == "0" {
			return "the pruning pass switched off"
		}

		return setting.value + " days"

	case "MaxClients":
		return setting.value

	case "AudioConversion":
		return conversionReadings[setting.value]

	case "Branding", "Email":
		return "nothing set"

	case "KeypadBeeps":
		if setting.label == "" {
			return "the value " + setting.value + ", which the administration interface prints no words for"
		}

		return "the set it names " + setting.label
	}

	return setting.value
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

// The skip link is the first thing the keyboard meets, and it goes where its own name says: the contents aid.
// An earlier version aimed it at #main on the reasoning that a keyboard reader had already passed the contents
// list. That is backwards — the link is the first stop of the whole route, so the contents aid is exactly what
// lies ahead of it — and a reader told "Skip to contents" who lands inside a chapter has been given a promise the
// document does not keep.
func TestTheUserGuideSkipsToItsContentsAid(t *testing.T) {
	body := userGuidePath(t)

	skip := regexp.MustCompile(`(?is)<a class="skip"[^>]*href="#contents"[^>]*>(.*?)</a>`).FindStringSubmatch(body)
	if len(skip) < 2 {
		t.Fatalf("the guide carries no skip link aimed at #contents, which is what its own name promises")
	}

	if strings.TrimSpace(skip[1]) == "" {
		t.Errorf("the guide's skip link says nothing, so a keyboard reader is told nothing about where it goes")
	}

	if header := strings.Index(strings.ToLower(body), "<header"); header >= 0 && strings.Index(body, `<a class="skip"`) > header {
		t.Errorf("the skip link comes after the banner, so it is not the first thing the keyboard meets")
	}

	if !regexp.MustCompile(`(?is)<nav class="toc"[^>]*id="contents"`).MatchString(body) {
		t.Errorf("the guide's skip link aims at #contents, and the document carries no contents aid with that anchor")
	}

	if strings.Contains(body, `href="#main"`) {
		t.Errorf("the guide aims a link at #main, which is the main text rather than the contents the link names")
	}
}

// A figure in this guide is only a figure once it carries its own bytes: the pictures travel inside the file, so a
// slot that names no picture shows the reader an empty frame. Every figure names its picture, says what it shows to
// a reader who cannot see it, and captions it for a reader who can, and the document embeds enough of them that it
// is showing the screen rather than describing it.
func TestEveryFigureInTheGuideCarriesItsPictureCaptionAndDescription(t *testing.T) {
	body := userGuidePath(t)

	figures := regexp.MustCompile(`(?is)<figure data-shot="([^"]+)">([\s\S]*?)</figure>`).FindAllStringSubmatch(body, -1)

	if len(figures) != 23 {
		t.Errorf("the guide carries %d figures, and the twelve chapters account for 23 of them between themselves, so a figure sits outside the chapter written to hold it or one is missing", len(figures))
	}

	embedded := 0

	for _, figure := range figures {
		id := figure[1]

		img := regexp.MustCompile(`(?is)<img\b[^>]*>`).FindString(figure[2])

		if img == "" {
			t.Errorf("the figure %q carries no image element, so the reader is shown nothing", id)

			continue
		}

		if alt := regexp.MustCompile(`(?i)alt="([^"]*)"`).FindStringSubmatch(img); len(alt) < 2 || strings.TrimSpace(alt[1]) == "" {
			t.Errorf("the figure %q carries an image with no account of what it shows, so a reader who cannot see it is left with the frame alone", id)
		}

		src := regexp.MustCompile(`(?i)src="([^"]*)"`).FindStringSubmatch(img)

		if len(src) < 2 {
			t.Errorf("the figure %q carries no picture inside the guide: its image names no src, so the frame the reader is shown is empty", id)

			continue
		}

		if !strings.HasPrefix(src[1], "data:image/png;base64,") {
			t.Errorf("the figure %q carries a picture that is not a PNG inlined as a data URI, so the guide reaches outside itself for it: %q", id, src[1])

			continue
		}

		embedded++

		caption := regexp.MustCompile(`(?is)<figcaption\b[^>]*>(.*?)</figcaption>`).FindStringSubmatch(figure[2])

		if len(caption) < 2 || strings.TrimSpace(stripTags(caption[1])) == "" {
			t.Errorf("the figure %q carries no caption, so a reader who can see the picture is not told what it is a picture of", id)
		}
	}

	if embedded < 20 {
		t.Errorf("the guide embeds %d pictures inside itself, and a guide that shows the listening screen at all needs at least 20", embedded)
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

	if src := regexp.MustCompile(`(?i)src="([^"]*)"`).FindStringSubmatch(mark); len(src) < 2 || !strings.HasPrefix(src[1], "data:image/png;base64,") {
		t.Errorf("the header's mark is not the project's own drawing inlined as a data URI, so a listener with no network gets an empty frame beside the application's name: %s", mark)
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

	// The table is the listener's list: the changes a listener sees or hears on the listening screen, and the
	// owner's review of the finished guide is what narrowed it to 22 rows by taking out the three that name work
	// behind the screen. The count is read off the rows rather than off the anchors, because an anchor count passes
	// whether a row is missing or a row has been added, and the rows are what the guide promises a reader.
	rows := regexp.MustCompile(`(?is)<tr>\s*<th scope="row">(.*?)</th>.*?</tr>`).FindAllStringSubmatch(section, -1)

	if len(rows) != 22 {
		t.Errorf("the changes since version 6.6.3 carry %d rows, want the 22 changes a listener sees or hears on the listening screen", len(rows))
	}

	for _, row := range rows {
		if !strings.Contains(row[0], `href="#ch-`) {
			t.Errorf("the changes since version 6.6.3 carry a row nothing can be acted on from, because it names no chapter of this guide: %s", strings.TrimSpace(stripTags(row[1])))
		}
	}

	links := regexp.MustCompile(`(?i)href="([^"]+)"`).FindAllStringSubmatch(section, -1)

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

	// The row that promises the branding line, the help affordance, and the promotion of the native applications
	// names the chapters that operate each of the three. The branding line is operated in the chapter on the main
	// screen, which is where the guide first says what that line is. A draft of the row named only the chapter on
	// phones and tablets and the chapter on help, and a reader who followed the row never reached the account of
	// the line itself.
	branding := regexp.MustCompile(`(?is)<tr>\s*<th scope="row">The branding line, the help affordance, and the promotion of the native applications\.</th>(.*?)</tr>`).FindStringSubmatch(section)

	if branding == nil {
		t.Fatalf("the changes since version 6.6.3 carry no row for the branding line, the help affordance, and the promotion, so this check proves nothing about where the guide operates them")
	}

	for _, want := range []string{"#ch-02-the-main-screen", "#ch-08-on-your-phone-or-tablet", "#ch-09-help-and-support"} {
		if !strings.Contains(branding[1], want) {
			t.Errorf("the row for the branding line, the help affordance, and the promotion no longer points at %q, which is a chapter that operates one of them", want)
		}
	}

	// The stored access code is read from the same place the older application wrote it, and the page presents it by
	// itself on every reconnect, so typing it out again is not among the costs of the update. A draft of this chapter
	// promised a code the reader would have to enter again, and a listener who believed it went looking for a cost the
	// update does not have.
	prose := strings.Join(strings.Fields(section), " ")

	if !strings.Contains(prose, "presented again by itself") {
		t.Errorf("the changes since version 6.6.3 do not say the stored access code is presented again by itself, which is what the shipped page does with the code an older application left in the same browser")
	}

	for _, superseded := range []string{"may have to be presented again", "a code you have to enter again"} {
		if strings.Contains(prose, superseded) {
			t.Errorf("the changes since version 6.6.3 promise %q, which is not a cost the shipped update has", superseded)
		}
	}

	// Three of the changes this update brought are work a listener cannot see or hear from the listening screen:
	// how the listening application was written, that the listening and administration applications were split
	// apart, and the type each system and talkgroup carries in its configuration. A listener meets the first as
	// the same screens they already knew, the second only as an address that is not theirs, and the third only as
	// colour and tone, which have rows of their own. They are recorded in the specifications and not in this
	// guide, and a later edit that slides one back into the table fails here instead of widening a listener's list
	// back into a change log.
	for _, behind := range []string{"was rewritten", "without a framework", "became two separate applications", "have types."} {
		if strings.Contains(prose, behind) {
			t.Errorf("the changes since version 6.6.3 carry %q, which is a change behind the screen and not something a listener sees or hears while listening", behind)
		}
	}

	// The row that promises converted audio names the chapters that operate it. The chapter on phones and tablets is
	// not one of them: what that chapter is written to carry is the narrow layout, adding the application to a home
	// screen, what works with no server, the new-version prompt, the store promotion, and what the browser remembers,
	// and none of it is a converted recording. The row names the chapter on searching, which now says a played
	// recording and a saved one are the same converted copy, and the reference chapter, which carries the setting.
	row := regexp.MustCompile(`(?is)<tr>\s*<th scope="row">Converted audio\.</th>(.*?)</tr>`).FindStringSubmatch(section)

	if row == nil {
		t.Fatalf("the changes since version 6.6.3 carry no row for converted audio, so this check proves nothing about where the guide operates it")
	}

	for _, want := range []string{"#ch-05-searching-archived-calls", "#ch-12-reference"} {
		if !strings.Contains(row[1], want) {
			t.Errorf("the converted-audio row no longer points at %q, which is a chapter that operates converted audio", want)
		}
	}

	for _, superseded := range []string{"#ch-08-on-your-phone-or-tablet"} {
		if strings.Contains(row[1], superseded) {
			t.Errorf("the converted-audio row points at %q, a chapter this guide writes to carry the narrow layout, installing, working offline, the update prompt, and what the browser remembers, and nothing about a converted recording", superseded)
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

// Two sub-sections came out of the guide on the owner's review of the finished document: the one whose whole
// subject was the guide's own limits, and the one that described a select panel on a deployment with nothing
// configured. The headings went and what they said did not. What is proved here is both halves of that: the guide
// no longer carries the two headings, because a section that survived its own removal is a section the review did
// not carry out; and the facts a reader can act on are still stated in the chapters that now hold them, because a
// guide that loses a heading and a fact in the same edit has lost something it was written to say.
func TestTheRemovedSubSectionsLeftTheirFactsBehind(t *testing.T) {
	body := userGuidePath(t)

	for _, gone := range []string{"What this guide does not cover", "The panel with nothing configured"} {
		if regexp.MustCompile(`(?is)<h[34][^>]*>\s*` + regexp.QuoteMeta(gone) + `\s*</h[34]>`).MatchString(body) {
			t.Errorf("the guide still carries the sub-section %q, which the owner's review of the finished guide took out", gone)
		}
	}

	opening := userGuideChapter(t, body, "ch-01-overview")

	if !strings.Contains(opening, "Who this guide is for") {
		t.Errorf("the opening chapter no longer carries the section the exclusions were folded into, so the reader has nowhere left to meet them")
	}

	if !strings.Contains(guideSpoken(opening), "does not cover") {
		t.Errorf("the opening chapter no longer says in its own sentence what it does not cover, which is the sentence the removed heading used to carry")
	}

	panel := userGuideChapter(t, body, "ch-04-choosing-what-to-hear")

	if !strings.Contains(panel, "How the panel is arranged") {
		t.Errorf("the chapter on choosing what to hear no longer carries the section the empty state was folded into")
	}

	if !strings.Contains(guideSpoken(panel), "No talkgroup configured!") {
		t.Errorf("the chapter on choosing what to hear no longer answers a deployment whose systems carry no talkgroups")
	}

	if figures := regexp.MustCompile(`(?is)<figure data-shot="`).FindAllString(panel, -1); len(figures) != 3 {
		t.Errorf("the chapter on choosing what to hear carries %d figures after the empty state moved, want the three it is written to carry", len(figures))
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

	// The line carries the version the page was built with, and there are two readings of it. A build made at a
	// commit a tag names stamps the version and nothing after it; a build made from a commit no tag names stamps
	// the version with a mark naming that commit, which begins with +g. A guide that describes only one of them
	// leaves a listener quoting a mark at an operator who has never seen one on a screen, which is what the guide
	// itself did in its first draft: it said a release build stamps the release and a mark, and the build tool
	// leaves the mark out whenever a tag has already named the release. The chapter is read as running text, so the
	// phrases are proved whatever line the document happens to break them on.
	spoken := guideSpoken(userGuideChapter(t, body, "ch-09-help-and-support"))

	if !strings.Contains(spoken, "no tag names") {
		t.Errorf("the chapter on help and support never says that a build made from a commit no tag names is the reading that carries a mark, so the reader cannot tell which version line they are looking at")
	}

	if !strings.Contains(spoken, "nothing after it") {
		t.Errorf("the chapter on help and support never says that a build made at the release stamps the version with nothing after it, which is the reading a listener at a release sees")
	}

	if !strings.Contains(spoken, "+g") {
		t.Errorf("the chapter on help and support never names the shape the mark takes, so a reader who sees one has nothing to match it against")
	}
}

// The guide is one long read, and a reader who follows a route out of the contents aid has already said where
// they are going. Gliding there through seventy thousand pixels of chapters they are not reading keeps them
// waiting for a place they named, and a reader who moves by key is reading a page that is moving under them.
// So the guide arrives when it is asked to, and the only motion it starts on its own is the highlight a
// contents link takes up â the reading the reader's own request for less motion turns off.
func TestTheGuidesAnchorRoutesArriveWhereTheyAreAsked(t *testing.T) {
	style := regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style>`).FindStringSubmatch(userGuidePath(t))
	if len(style) < 2 {
		t.Fatalf("the guide carries no inlined stylesheet")
	}

	if at := strings.Index(style[1], "scroll-behavior: smooth"); at >= 0 {
		t.Errorf("the guide's stylesheet glides to an anchor at offset %d, so a route the reader has already named arrives seconds late", at)
	}

	if !strings.Contains(style[1], "@media (prefers-reduced-motion: reduce)") {
		t.Errorf("the guide starts motion of its own and carries no rule for a reader who has asked the platform for less")
	}
}

// At phone width a guide has to fit its tables onto the page it was given. A name that cannot break holds the
// column it sits in open, the table paints past the edge of the page, and the words past that edge are out of
// reach for a reader who moves through the document with a keyboard: a sideways scroll has nothing in it to put
// focus into. So the narrow reading of the guide lets what sits in a column break, the names set in their own
// face included.
func TestTheGuidesTablesFitTheNarrowPageTheyAreReadOn(t *testing.T) {
	style := regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style>`).FindStringSubmatch(userGuidePath(t))
	if len(style) < 2 {
		t.Fatalf("the guide carries no inlined stylesheet")
	}

	breaks := regexp.MustCompile(`@media \(max-width: ([0-9.]+)rem\)`).FindAllStringSubmatch(style[1], -1)

	if len(breaks) == 0 {
		t.Fatalf("the guide carries no width at which it rearranges itself for a narrow page")
	}

	narrowest, offset := "", -1

	for _, at := range regexp.MustCompile(`@media \(max-width: ([0-9.]+)rem\)`).FindAllStringSubmatchIndex(style[1], -1) {
		width := style[1][at[2]:at[3]]

		if narrowest == "" || parseRem(width) < parseRem(narrowest) {
			narrowest, offset = width, at[0]
		}
	}

	if offset < 0 {
		t.Fatalf("the guide names no narrow page to rearrange itself for")
	}

	next := strings.Index(style[1][offset+1:], "@media")

	block := style[1][offset:]

	if next >= 0 {
		block = style[1][offset : offset+1+next]
	}

	// A rule the guide explains in a comment is still that rule, so the explanation comes out before the rules
	// are read.
	block = regexp.MustCompile(`/\*[\s\S]*?\*/`).ReplaceAllString(block, " ")

	rules := regexp.MustCompile(`(?s)([^{}]+)\{([^{}]*)\}`).FindAllStringSubmatch(block, -1)

	cells := false
	names := false
	laidOut := false
	displayChanged := ""

	for _, rule := range rules {
		selector := strings.Join(strings.Fields(strings.TrimSpace(rule[1])), " ")
		body := rule[2]

		// A table at phone width is laid out from the page it was given. Left to the browser's own layout the
		// column with the shortest content shrinks to whatever its longest word needs, and every column beside
		// it is squeezed until the words in it break in the middle. And a table given a display that is not a
		// table stops being a table for whatever is reading it, which is a poor price to pay for a sideways
		// scroll that nothing on the page needed.
		if isTableSelector(selector) {
			if strings.Contains(body, "table-layout: fixed") {
				laidOut = true
			}

			if shown := regexp.MustCompile(`display:\s*(block|flex|grid|inline-block|inline-flex)`).FindString(body); shown != "" {
				displayChanged = selector + " { " + strings.TrimSpace(shown) + " }"
			}
		}

		if !strings.Contains(body, "overflow-wrap") && !strings.Contains(body, "white-space: normal") {
			continue
		}

		for _, part := range strings.Split(selector, ",") {
			words := strings.Fields(part)

			for _, word := range words {
				if word == "td" || word == "th" {
					cells = true
				}

				if word == "code" && (strings.Contains(part, "td") || strings.Contains(part, "th")) {
					names = true
				}
			}
		}
	}

	if !cells {
		t.Errorf("at the %s rem page the guide has no rule that lets what sits in a table column break, so a wide table can paint past the page it was read on", narrowest)
	}

	if !names {
		t.Errorf("at the %s rem page the guide keeps a name set in its own face unbreakable inside a column, which is what holds a table open past the page", narrowest)
	}

	if !laidOut {
		t.Errorf("at the %s rem page the guide lets the browser size its columns from the words inside them, which shrinks the shortest column to one word and breaks the words in its neighbours in the middle", narrowest)
	}

	if displayChanged != "" {
		t.Errorf("at the %s rem page the guide sets %s, which stops a table being a table for whatever is reading it", narrowest, displayChanged)
	}
}

// isTableSelector reports that a rule at the narrow breakpoint is about a table itself, rather than about
// something sitting inside one.
func isTableSelector(selector string) bool {
	for _, part := range strings.Split(selector, ",") {
		words := strings.Fields(part)

		if len(words) == 1 && words[0] == "table" {
			return true
		}
	}

	return false
}

// parseRem reads a width the guide says it rearranges itself at, so the narrowest of them can be compared.
func parseRem(reading string) float64 {
	width, err := strconv.ParseFloat(reading, 64)
	if err != nil {
		return -1
	}

	return width
}

// Where the reader has the machine draw everything in their own colours, the guide flattens its own surfaces to
// that palette, and a list of chapters has to survive the flattening as a list of things to follow. An entry
// keeps the line under it or an edge drawn in the reader's own ink; a colour alone would leave the contents aid
// reading as a list of ordinary words with something hidden in it.
func TestTheContentsAidStaysReadableUnderARecolouredReading(t *testing.T) {
	style := regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style>`).FindStringSubmatch(userGuidePath(t))
	if len(style) < 2 {
		t.Fatalf("the guide carries no inlined stylesheet")
	}

	start := strings.Index(style[1], "@media (forced-colors: active)")

	if start < 0 {
		t.Fatalf("the guide carries no reading for a machine drawing everything in the reader's own colours")
	}

	next := strings.Index(style[1][start+1:], "@media")

	block := style[1][start:]

	if next >= 0 {
		block = style[1][start : start+1+next]
	}

	cue := ""

	for _, rule := range regexp.MustCompile(`(?s)([^{}]+)\{([^{}]*)\}`).FindAllStringSubmatch(block, -1) {
		selector := strings.Join(strings.Fields(strings.TrimSpace(rule[1])), " ")

		if !strings.Contains(selector, "nav.toc a") {
			continue
		}

		if strings.Contains(rule[2], "text-decoration: underline") {
			cue = "the line under it"

			break
		}

		if regexp.MustCompile(`border-[a-z-]*color:\s*CanvasText`).MatchString(rule[2]) {
			cue = "an edge drawn in the reader's own ink"

			break
		}
	}

	if cue == "" {
		t.Errorf("under a palette the reader chose the guide leaves a contents-aid entry with nothing but a colour to tell it from the words around it, so the list of chapters reads as a list of words")

		return
	}

	t.Logf("under a recoloured reading a contents-aid entry is carried by %s", cue)
}

// A call-out's weight is the thing that tells a reader how much care to take: a note adds something to what the
// reader already knows, a caution says a belief the reader arrived with is wrong or a thing they are doing has a
// cost. The guide carries that weight in the aside's class, which paints it as a coloured edge and a coloured
// ground, and under the recoloured reading the guide declares support for every one of those surfaces becomes the
// reader's own ink — a caution then reads exactly as a note reads, in print as well as on a screen that has been
// flattened. So the words carry it: each call-out's label names its kind before it names what it is about.
func TestTheGuidesCalloutsSayWhatKindTheyAre(t *testing.T) {
	body := userGuidePath(t)

	callouts := regexp.MustCompile(`(?is)<aside class="(note|caution|danger)">\s*<span class="label">([^<]*)</span>`).FindAllStringSubmatch(body, -1)

	if len(callouts) != 9 {
		t.Errorf("the guide carries %d call-outs, and the twelve chapters account for 9 of them between themselves, so a call-out sits outside the chapter written to hold it or one is missing", len(callouts))
	}

	for _, callout := range callouts {
		weight, label := callout[1], strings.Join(strings.Fields(callout[2]), " ")
		words := strings.Fields(label)
		kind := ""

		if len(words) > 0 {
			kind = strings.ToLower(strings.Trim(words[0], ":—-"))
		}

		if kind != weight {
			t.Errorf("the guide's call-out classed %q carries the label %q, which says what it is about but not what kind of call-out it is; the edge and the ground that carry the kind are flattened to the reader's own ink under the recoloured reading the guide declares support for, and are grey-on-grey on paper", weight, label)

			continue
		}

		if len(words) < 2 {
			t.Errorf("the guide's call-out labelled %q names its kind and nothing else, so a reader is told a caution is there without being told what it cautions about", label)
		}
	}
}

// The guide opens a chapter with a lead: the sentence or two that say what the chapter is for before the reader is
// inside it. Every chapter of the administrator guide opens this way, and every chapter of this guide except two;
// the two that opened in ordinary body text sat beside ten that had been given the larger, softer opening, which
// reads as an opening that was left unfinished rather than a chapter that does not need one.
func TestEveryChapterInTheGuideOpensWithItsLead(t *testing.T) {
	body := userGuidePath(t)

	heads := regexp.MustCompile(`(?is)<h2 id="(ch-[a-z0-9-]+)">[\s\S]*?</h2>`).FindAllStringSubmatchIndex(body, -1)

	if len(heads) != 12 {
		t.Fatalf("the guide carries %d chapters, want the 12 its contents aid lists", len(heads))
	}

	for at, head := range heads {
		id := body[head[2]:head[3]]
		start := head[1]
		end := len(body)

		if at+1 < len(heads) {
			end = heads[at+1][0]
		} else if main := strings.Index(body[start:], "</main>"); main >= 0 {
			end = start + main
		}

		lead := regexp.MustCompile(`(?is)^\s*<p class="lead">([\s\S]*?)</p>`).FindStringSubmatch(body[start:end])

		if len(lead) < 2 {
			t.Errorf("the chapter %q opens in ordinary body text, where every other chapter of this guide and every chapter of the administrator guide opens with a lead", id)

			continue
		}

		if words := len(strings.Fields(stripTags(lead[1]))); words < 12 {
			t.Errorf("the chapter %q opens with a lead of %d words, which says less than the heading above it already said", id, words)
		}
	}
}
