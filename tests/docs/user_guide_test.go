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
