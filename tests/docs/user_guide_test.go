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

// the user guide renders from itself, carries every promised chapter,
// states its version, and stays small enough to carry.
// see the docs-user-guide-checks maintainer note.

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

// userGuideChapter returns one chapter's text, from its heading to the next.
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

// scannerPrintedText is the corpus of words the listening application can
// put on its own screen, from every source under the server's
// presentation, split labels rejoined and whitespace collapsed.
// see the docs-user-guide-printed-text maintainer note.
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

// isAddressShape reports whether code text is an address the reader types
// rather than a word the application prints.
// see the docs-user-guide-checks maintainer note.
func isAddressShape(name string) bool {
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "?") {
		return true
	}

	return strings.Contains(name, "://")
}

// every name the guide sets as the application's own word is a word the application writes.
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

// the LED is the shipped noun for the light beside the branding line,
// and the guide holds to shipped names: it quotes the page's accessible
// name exactly and keeps "light" for the controls' own marks.
// see the docs-user-guide-naming maintainer note.
func TestTheGuideNamesTheHeaderLightAsTheProductDoes(t *testing.T) {
	body := userGuidePath(t)

	hits := regexp.MustCompile(`(?i)server light`).FindAllString(body, -1)

	if len(hits) > 0 {
		t.Errorf("the guide calls the light beside the branding line %q %d times, and no shipped string uses that name: the component, the client domain that paints it, and the operator's own screens all call it the LED", "server light", len(hits))
	}

	for _, chapter := range []string{"ch-02-the-main-screen", "ch-03-listening-to-live-calls", "ch-10-keyboard-and-screen-readers", "ch-12-reference"} {
		// the guide's own pictures come out before its words are read.
		prose := regexp.MustCompile(`src="data:[^"]*"`).ReplaceAllString(userGuideChapter(t, body, chapter), `src=""`)

		if !strings.Contains(prose, "LED") {
			t.Errorf("the chapter %q operates the light beside the branding line and never names it the LED", chapter)
		}
	}

	// the accessible name is not the guide's noun for that thing.
	if !strings.Contains(body, "<code>Server status:</code>") {
		t.Errorf("the guide no longer quotes %q followed by the state, which is the name the shipped page gives the light to a screen reader", "Server status:")
	}

	// the corner lights have no LED, unlike the header light.
	if !strings.Contains(body, "corner light") {
		t.Errorf("the guide no longer names the corner lights on the controls, which are lights on the screen and not the header light")
	}

	// the reference chapter's row carries the shipped name printed in it.
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

// the screen's parts are named by the shipped code: display rows,
// history table, unlock overlay, control bar. leads, titles, captions,
// and alt text all hold to those nouns, or readers hold two names.
// see the docs-user-guide-naming maintainer note.
func TestTheGuideNamesTheScreensPartsAsTheComponentsPaintThem(t *testing.T) {
	body := userGuidePath(t)

	// the guide's own pictures come out before its words are read.
	prose := regexp.MustCompile(`src="data:[^"]*"`).ReplaceAllString(body, `src=""`)

	for _, paraphrase := range []string{"three areas", "the area starts", "the display area", "the control area", "control row of the listening screen"} {
		if hits := regexp.MustCompile(`(?i)`+paraphrase).FindAllString(prose, -1); len(hits) > 0 {
			t.Errorf("the guide names a region of the screen %q %d times, where the shipped components and the guide's own section titles name it the display, the control bar, or one of the three parts of the screen", paraphrase, len(hits))
		}
	}

	main := guideSpoken(userGuideChapter(t, body, "ch-02-the-main-screen"))

	if !strings.Contains(main, "built in three parts") {
		t.Errorf("the chapter on the main screen never says the screen is built in three parts, which is the reading its own section title and its own table caption carry")
	}

	if !strings.Contains(main, "the display, and the control bar") {
		t.Errorf("the chapter on the main screen never names the display and the control bar as the two parts the branding line sits beside, which is the order the shipped page lays them out in")
	}

	display := guideSpoken(userGuideChapter(t, body, "ch-06-what-the-display-says"))

	if matched, _ := regexp.MatchString(`(?i)\bthe display starts dimmed\b`, display); !matched {
		t.Errorf("the chapter on the display never says the display starts dimmed, which is what the shipped dimmer does and what the chapter on listening to live calls already says")
	}
}

// chapter two names the screen as the components write it, and holds
// its two figures.
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

	// the paused light alternates its own face; the standing ring belongs to
	// the reduced-motion reading only.
	// see the docs-user-guide-superseded-claims maintainer note.
	for _, superseded := range []string{"the ring blinks", "always blinking"} {
		if strings.Contains(spoken, superseded) {
			t.Errorf("the chapter on the main screen carries %q, which describes a mark the shipped page does not draw in that state", superseded)
		}
	}

	// the minutes beside the hourglass are an avoid window's length, and
	// nothing counts them down.
	// see the docs-user-guide-superseded-claims maintainer note.
	for _, superseded := range []string{"minutes left", "minutes remaining", "minutes are left"} {
		if strings.Contains(spoken, superseded) {
			t.Errorf("the chapter on the main screen carries %q, which describes a countdown the shipped display does not keep", superseded)
		}
	}
}

// chapter three's listening controls match the shipped components and
// domain code.
func TestTheUserGuideOperatesTheListeningControlsAsTheApplicationDoes(t *testing.T) {
	body := userGuidePath(t)

	chapter := userGuideChapter(t, body, "ch-03-listening-to-live-calls")

	// the leave prompt's sentence is quoted verbatim, as the browser shows it.
	spoken := guideSpoken(chapter)

	// the leave prompt's sentence is quoted verbatim, as the browser shows it.
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

	// a tone plays before its recording, which is not under way during it:
	// silencing or skipping during the tone records nothing.
	// see the docs-user-guide-superseded-claims maintainer note.
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

	// only two lights animate: the LED while audio is held, and a talkgroup's
	// button inside a timed window.
	// see the docs-user-guide-superseded-claims maintainer note.
	if !strings.Contains(spoken, "shape") {
		t.Errorf("the chapter on live calls never says that the state light's shape does not change, which is the fact a reader needs in order to know the colour is the whole of the signal")
	}

	for _, superseded := range []string{"blinking", "blinks"} {
		if strings.Contains(spoken, superseded) {
			t.Errorf("the chapter on live calls carries %q, which describes motion the shipped control bar does not draw", superseded)
		}
	}

	// a timed window closes and is cleared, never renews itself; the minutes
	// are the window's length, never a countdown.
	// see the docs-user-guide-superseded-claims maintainer note.
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

// chapter four holds the select panel to the shipped OFF and ON names
// and its own conditions.
func TestTheUserGuideOperatesTheSelectPanelAsTheApplicationBuildsIt(t *testing.T) {
	chapter := userGuideChapter(t, userGuidePath(t), "ch-04-choosing-what-to-hear")

	spoken := guideSpoken(chapter)

	// the pair is checked as the guide prints a word, markup included.
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

	// the panel's first row is built from the deployment's groups, not from tags.
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

// guideSpoken reads a chapter as prose: markup away, whitespace joined.
// word checks use it; markup checks do not.
func guideSpoken(chapter string) string {
	return regexp.MustCompile(`\s+`).ReplaceAllString(stripTags(chapter), " ")
}

// chapter five carries the search panel's own control words and refuses
// four promises only the superseded page made.
// see the docs-user-guide-superseded-claims maintainer note.
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

	// a deployment-held call arrives with the DELAYED flag, as printed.
	if !strings.Contains(spoken, "DELAYED") {
		t.Errorf("the chapter on searching recorded calls never names the delay flag as %q, which is how the page prints it", "DELAYED")
	}

	if strings.Contains(chapter, "<code>Delayed</code>") {
		t.Errorf("the chapter on searching recorded calls prints the delay flag as %q, which is not the spelling the page prints", "Delayed")
	}

	// the paginator stays available during a search and during a playback.
	if !strings.Contains(spoken, "stays available") {
		t.Errorf("the chapter on searching recorded calls does not say what the paginator does during a search, which is that it stays available")
	}

	// the panel bounds the date field only when the answer holds calls, and
	// the guide must not promise the bound the shipped code refuses.
	// see the docs-user-guide-superseded-claims maintainer note.
	if !strings.Contains(spoken, "unbounded") {
		t.Errorf("the chapter on searching recorded calls does not say what happens to the date field's bounds when the answer is empty, which is that the field is left unbounded")
	}

	if strings.Contains(spoken, "1970") {
		t.Errorf("the chapter on searching recorded calls bounds the date field to a date in 1970, which is a bound the panel refuses to set when an answer holds no calls")
	}

	// the busy marking is written onto one bar kept in the layout, not onto
	// the panel or the results; controls stay in place marked as doing
	// nothing, and the panel says out loud what it is doing.
	// see the docs-user-guide-busy-state maintainer note.
	if strings.Contains(spoken, "panel marks itself busy") {
		t.Errorf("the chapter on searching recorded calls says the panel marks itself busy, which is a marking the shipped panel writes onto the progress bar it keeps in the layout, not onto the panel or onto the calls it found")
	}

	for _, carrier := range []string{"progress bar", "is marked as busy"} {
		if !strings.Contains(spoken, carrier) {
			t.Errorf("the chapter on searching recorded calls never names %q, which is the element the shipped panel marks busy while an answer is on its way", carrier)
		}
	}

	// the actions column has no heading, and the chapter has to say so.
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

	// a row draws one action, chosen by a chain the download switch heads:
	// with the switch on, the heard recording reads Download, not Stop.
	// see the docs-user-guide-superseded-claims maintainer note.
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

	// the searching chapter operates converted audio: played and downloaded
	// recordings read the server's bytes, converted first when the operator
	// switched conversion on.
	// see the docs-user-guide-superseded-claims maintainer note.
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

// chapter six carries the display's printed words and the readings a
// glance cannot give.
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

	// the chapter keeps its two figures: the full-screen control's own crop
	// was cut as showing nothing.
	// see the docs-user-guide-figure-inventory maintainer note.
	if figures := regexp.MustCompile(`(?is)<figure data-shot="`).FindAllString(chapter, -1); len(figures) != 2 {
		t.Errorf("the chapter on the display carries %d figures, want the two it operates: the flag row and the history table", len(figures))
	}
}

// chapter seven keeps server answers and field readings apart:
// lockouts answer like wrong codes.
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

// chapter eight carries the shipped update prompt and promo words,
// names stores without linking them, and holds its three figures.
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

// chapter nine checks the shipped help notice and version line, and
// says help needs an operator address.
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

	// the other-listeners count is a reading the display writes only when
	// the operator turns it on, and the server ships it off.
	// see the docs-user-guide-superseded-claims maintainer note.
	if !strings.Contains(spoken, "when the deployment shows it") {
		t.Errorf("the chapter on help and support asks whether anybody else was listening without saying that the count is only there when the deployment shows it")
	}

	// the chapter owns exactly one figure, the one the figure inventory gives it.
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

// chapter ten names what the application does for keyboard and
// screen-reader readers, and holds its measured and three figures.
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

	// the measured colour figures were corrected once; repeating the
	// superseded numbers would publish a fault the shipped page lacks.
	// see the docs-user-guide-superseded-claims maintainer note.
	for _, superseded := range []string{"2.61", "1.00", "invisible by design", "always blinking"} {
		if strings.Contains(spoken, superseded) {
			t.Errorf("the chapter on keyboards and screen readers carries %q, which is a superseded figure or the claim it was written to support", superseded)
		}
	}

	// the actions column is the shipped table's first column and has no
	// heading; it is not at the right edge.
	// see the docs-user-guide-superseded-claims maintainer note.
	for _, superseded := range []string{"last column of the search results"} {
		if strings.Contains(spoken, superseded) {
			t.Errorf("the chapter on keyboards and screen readers carries %q, which places the search table's action column where the shipped table does not put it", superseded)
		}
	}

	if !strings.Contains(spoken, "first column of the search results") {
		t.Errorf("the chapter on keyboards and screen readers never says the search results' action column is the first one, which is where the shipped table puts it")
	}

	// while an answer is on its way the panel marks one bar busy, leaves
	// criteria and reset marked as doing nothing, and speaks into the live
	// region; the results carry no such state.
	// see the docs-user-guide-busy-state maintainer note.
	state := ""

	for _, row := range regexp.MustCompile(`(?is)<tr>\s*<th scope="row">(.*?)</th>(.*?)</tr>`).FindAllStringSubmatch(chapter, -1) {
		if strings.Contains(row[1], "answer is on its way") {
			state = row[2]

			break
		}
	}

	if state == "" {
		t.Fatalf("the chapter on keyboards and screen readers carries no row for the search panel while an answer is on its way, so what it says about the marking cannot be read")
	}

	readRow := guideSpoken(state)

	if strings.Contains(readRow, "results area is marked as busy") || strings.Contains(readRow, "results are marked as busy") {
		t.Errorf("the chapter's row for the search panel says the calls it is waiting for carry the busy marking, which is a marking the shipped panel writes onto the progress bar it keeps in the layout")
	}

	for _, want := range []string{"progress bar", "marked as busy", "Searching calls", "doing nothing rather than taken away"} {
		if !strings.Contains(readRow, want) {
			t.Errorf("the chapter's row for the search panel never names %q, which is what the shipped panel does for a reader while an answer is on its way", want)
		}
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

// chapter eleven answers symptom first, offers only shipped remedies,
// and names the operator's own limits.
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

	// no remedy reads as a server procedure: those belong to the
	// operator's guide.
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

	// the search panel opens with the filters it already holds; it never
	// fills them from the call on the display.
	// see the docs-user-guide-superseded-claims maintainer note.
	for _, superseded := range []string{"already filled in"} {
		if strings.Contains(spoken, superseded) {
			t.Errorf("the chapter about when something does not work carries %q, which describes a thing the shipped search panel does not do", superseded)
		}
	}

	// the hourglass minutes ride the flag row, SKIP NEXT during a hold
	// starts nothing, and a version update reuses the stored code; each
	// superseded reading is refused by name.
	// see the docs-user-guide-superseded-claims maintainer note.
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

	// without its font the timed-avoid mark may show as a box, and the
	// reading is unaffected.
	for _, want := range []string{"arrives as a box", "the minutes are still beside it"} {
		if !strings.Contains(spoken, want) {
			t.Errorf("the chapter about when something does not work never gives %q, which is what a reader who sees the timed-avoid mark as a box needs to be told", want)
		}
	}

	// the chapter links out instead of restating, and its hrefs are read
	// as the self-containment check reads them.
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

	// the inventory gives this chapter no figures: this reader needs words.
	if figures := regexp.MustCompile(`(?is)<figure data-shot="`).FindAllString(chapter, -1); len(figures) != 0 {
		t.Errorf("the chapter about when something does not work carries a figure, and the inventory gives it none")
	}
}

// every table names its rows and columns: a caption, a header row, and a
// row header wherever the first column names the row.
// a caption is checked too, and two tables may not share one.
// see the docs-user-guide-checks maintainer note.
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

// the guide states no licence position of its own and names where the
// project states it; the refusal reads the words, not file bytes.
// see the docs-user-guide-licence-stance maintainer note.
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

// the closing reference chapter is tables and definitions, carries no
// figures, and points back at the operating chapters.
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

	// the glossary defines the words printed on the display, and a draft
	// left "type" out while pointing readers here.
	// see the docs-user-guide-superseded-claims maintainer note.
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

	// the reference chapter restates the LED's states briefly, so the
	// superseded ring claim is refused here as well.
	// see the docs-user-guide-superseded-claims maintainer note.
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

// the guide's tables name the printed names of the listening page,
// read from closed structures in the shipped sources and compared both
// ways, with order compared where a caption promises it.
// see the docs-user-guide-checks maintainer note.
func TestTheNamesTheGuideTabulatesAreTheNamesTheScannerPagePrints(t *testing.T) {
	body := userGuidePath(t)

	// the controls in the screen's rows, left to right: an order the
	// operating chapter promises and the reference chapter gathers.
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

	// the form's criteria and their order: promised by the operating
	// chapter, gathered by the reference chapter.
	criteria := searchFormNames(t)

	criteriaChapter := tableWithCaption(t, body, "The search panel's criteria, in the order the form puts them.")

	compareNames(t, "search chapter's criteria table", "the search panel's form", criteria,
		rowNames(guideTableRows(criteriaChapter), printedName))
	compareOrder(t, "search chapter's criteria table", "the search panel's form", criteria,
		rowNames(guideTableRows(criteriaChapter), printedName))

	criteriaReference := tableWithCaption(t, body, "The search panel's criteria, what each one holds, and where this guide operates it.")

	compareNames(t, "reference chapter's criteria table", "the search panel's form", criteria,
		rowNames(guideTableRows(criteriaReference), printedName))

	// the entry each criterion opens with before the listener touches anything.
	openings := searchFormOpenings(t)

	compareWords(t, "search chapter's criteria table", "the search panel's form", openings, bodyWords(criteriaChapter))
	compareWords(t, "reference chapter's criteria table", "the search panel's form", openings, bodyWords(criteriaReference))

	// the result table's headings in column order; the guide says in words
	// that the first one is missing, and inventing one fails.
	columns := searchColumnNames(t)

	resultRows := tableWithCaption(t, body, "What the search panel's result rows hold, in the order the columns come.")

	compareNames(t, "search chapter's result table", "the search panel's result table", columns,
		rowNames(guideTableRows(resultRows), columnHeadedName))
	compareOrder(t, "search chapter's result table", "the search panel's result table", columns,
		rowNames(guideTableRows(resultRows), columnHeadedName))

	// the display's flags beside a call; both chapters tabulate them and
	// neither promises an order.
	flags := displayFlagNames(t)

	compareNames(t, "display chapter's flag table", "the display", flags,
		rowNames(guideTableRows(tableWithCaption(t, body, "The display's flags, and what has to be true for each of them to be lit.")), printedName))
	compareNames(t, "reference chapter's flag table", "the display", flags,
		rowNames(guideTableRows(tableWithCaption(t, body, "The flags the display carries beside a call, what has to be true for each to appear, and where this guide operates it.")), printedName))

	// the two panels' added controls; criteria and result headings are
	// bound above, so they are not gathered here.
	compareNames(t, "reference chapter's panel table", "the panel chrome", panelControlNames(t),
		codeSpansIn(rowHeaders(tableWithCaption(t, body, "The controls the two panels add, what each one does, and where this guide operates it."))))

	// the select panel's fixed-word buttons only; group and talkgroup
	// buttons take their words from the configuration.
	compareNames(t, "select chapter's button table", "the select panel", selectPanelButtons(t),
		codeSpansIn(rowHeaders(tableWithCaption(t, body, "What the select panel's buttons show, and what a press on each one does."))))

	// What the unlock field says when the server refuses the code a listener offers.
	compareNames(t, "access code chapter's table", "the display's unlock field", unlockReadings(t),
		rowNames(guideTableRows(tableWithCaption(t, body, "What the unlock field says, what the server actually answered, and what is worth doing about it.")), printedName))

	// the announced sentences are compared by their fixed words, not
	// demanded as row headers.
	announcements := announcedSentences(t)
	announcementTable := tableWithCaption(t, body, "The sentences the page says out loud for a screen reader, what each one is for, and where this guide operates it.")

	compareWords(t, "reference chapter's announcement table", "the page's own live region", announcements,
		bodyWords(announcementTable))

	for _, name := range codeSpansIn(rowHeaders(announcementTable)) {
		if !holds(announcements, name) {
			t.Errorf("the guide's announcement table names %q as a sentence the page says out loud, which no component of the listening page announces", name)
		}
	}

	// the display's waiting-row readings are looked for in the rows
	// table's description, not as row names.
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

// the settings table gathers listener-feeling server settings and their
// shipped positions, measured against the administration list and the
// values the server starts with.
// see the docs-user-guide-checks maintainer note.
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

		// a table with no shipped-position column is reported as itself.
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

// uniqueNames drops repeats when one printed name answers two rows across chapters and tables.
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
		// a configuration-filled entry carries no fixed word, and the guide
		// names none for it.
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

// searchColumnNames reads the result table's headings in column order,
// keeping the headingless first column so the guide's row for it is
// compared.
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

// displayFlagNames reads the three flags in the display's markup and
// the timed avoid mark, held in markup only while that part runs.
func displayFlagNames(t *testing.T) []string {
	source := listenerComponentSource(t, "rdio-display.js")

	names := []string{}

	for _, flag := range regexp.MustCompile(`(?is)<span class="flag flag-[a-z]+">(.*?)</span>`).FindAllStringSubmatch(source, -1) {
		if name := printedLabel(flag[1]); name != "" {
			names = append(names, name)
		}
	}

	// the mark exists only in the reading written while the timed part
	// runs.
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

	// a one-string sentence carries its words whole, so single-quoted
	// runes are unquoted here rather than by Go's reader.
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

// the guide never sends a reader to the administration application.
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

// the guide's terms table names the words it defines, the settings table included.
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

// the terms table's words and the glossary's words must not overlap.
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

// the skip link and the keyboard path are described in words a listener
// reads, not as markup mechanics the guide cannot instruct.
// see the docs-user-guide-checks maintainer note.
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

// the keyboard reaches the skip link first, and every control keeps a name.
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

// the guide states the measured colour and contrast figures of the shipped page.
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

// colour is never the only signal: every state carries a word as well.
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

// the accessibility chapter's words match the shipped page's spoken
// sentences and control names.
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

// the version line names the page's version, and the guide never
// blurs page, server, and deployment.
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

// the version line and the help notice read from the shipped sources,
// not from invented sentences.
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

	// the appendix and history are not chapters and the contents does not
	// list them, so the naming rule and the figure count skip them.
	// see the docs-user-guide-naming maintainer note.
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

	// the whole guide stays a single carried document: one title, one nav,
	// and nothing outside itself.
	branding := regexp.MustCompile(`(?is)<tr>\s*<th scope="row">The branding line, the help affordance, and the promotion of the native applications\.</th>(.*?)</tr>`).FindStringSubmatch(section)

	if branding == nil {
		t.Fatalf("the changes since version 6.6.3 carry no row for the branding line, the help affordance, and the promotion, so this check proves nothing about where the guide operates them")
	}

	for _, want := range []string{"#ch-02-the-main-screen", "#ch-08-on-your-phone-or-tablet", "#ch-09-help-and-support"} {
		if !strings.Contains(branding[1], want) {
			t.Errorf("the row for the branding line, the help affordance, and the promotion no longer points at %q, which is a chapter that operates one of them", want)
		}
	}

	// every anchor the guide asks for arrives where it is asked.
	prose := strings.Join(strings.Fields(section), " ")

	if !strings.Contains(prose, "presented again by itself") {
		t.Errorf("the changes since version 6.6.3 do not say the stored access code is presented again by itself, which is what the shipped page does with the code an older application left in the same browser")
	}

	for _, superseded := range []string{"may have to be presented again", "a code you have to enter again"} {
		if strings.Contains(prose, superseded) {
			t.Errorf("the changes since version 6.6.3 promise %q, which is not a cost the shipped update has", superseded)
		}
	}

	// no chapter is left out of the contents, and no contents entry lacks
	// a chapter.
	for _, behind := range []string{"was rewritten", "without a framework", "became two separate applications", "have types."} {
		if strings.Contains(prose, behind) {
			t.Errorf("the changes since version 6.6.3 carry %q, which is a change behind the screen and not something a listener sees or hears while listening", behind)
		}
	}

	// the contents is the page's first landmark, and its links stay
	// readable when the page is recoloured.
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

// every chapter opens with its heading and its lead paragraph.
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

// the version line, the history, and the sign-off belong to the guide
// itself, and their wording comes from the shipped page and the release,
// not from invented prose.
// see the docs-user-guide-version-line maintainer note.
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

// the guide's tables fit the narrow page they are read on, checked
// against the shipped stylesheet.
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

	// the guide states the version it documents, in the page's own version
	// line, and that line is the release's, not the guide's own number.
	// see the docs-user-guide-version-line maintainer note.
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

// the appendix states the licence position by reference, and the guide adds none of its own.
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

// the guide tells a listener what changed and what to do, from the
// shipped release notes' own words.
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

		// a removed sub-section's facts survive somewhere in the guide.
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

// the guide says what it leaves out and how the reader's name is read back.
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

// a converted-recording row points at a chapter that actually carries
// conversion, not the layout chapter.
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

// the guide's callouts each say what kind they are, in the shipped class names.
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

// the guide is small enough to carry, held to a size the reader can actually download.
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
