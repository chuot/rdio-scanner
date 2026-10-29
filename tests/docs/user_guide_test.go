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

// The listener guide is held to its shape, never to its sentences: these tests
// pin the file's properties, the chapter inventory, the contents aid, the
// figures, and the page furniture, and no test here compares the document
// against a sentence the document itself writes.

// userGuideChapters are the chapters the guide is written to, in document
// order. The contents aid and the document itself are both held to this list.
var userGuideChapters = []string{
	"ch-01-start-here",
	"ch-02-first-session",
	"ch-03-choosing",
	"ch-04-live-calls",
	"ch-05-recorded-calls",
	"ch-06-the-screen",
	"ch-07-access-codes",
	"ch-08-phone-tablet",
	"ch-09-problems",
	"ch-10-getting-help",
	"ch-11-quick-reference",
}

// userGuideFigure names one screenshot the guide carries and the chapter that
// explains it.
type userGuideFigure struct {
	shot    string
	chapter string
}

// userGuideFigures are the screenshots the guide carries, each beside the
// chapter that explains it.
var userGuideFigures = []userGuideFigure{
	{"main-idle", "ch-02-first-session"},
	{"light-states", "ch-02-first-session"},
	{"main-live", "ch-02-first-session"},
	{"select-panel", "ch-03-choosing"},
	{"main-paused", "ch-04-live-calls"},
	{"search-list", "ch-05-recorded-calls"},
	{"search-filters", "ch-05-recorded-calls"},
	{"search-playback", "ch-05-recorded-calls"},
	{"search-empty", "ch-05-recorded-calls"},
	{"main-flags", "ch-06-the-screen"},
	{"main-history", "ch-06-the-screen"},
	{"unlock-code", "ch-07-access-codes"},
	{"unlock-expired", "ch-07-access-codes"},
	{"main-narrow", "ch-08-phone-tablet"},
	{"update-dialog", "ch-08-phone-tablet"},
	{"promo-snackbar", "ch-08-phone-tablet"},
	{"help-snackbar", "ch-10-getting-help"},
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

// The chapter inventory is the whole inventory: exactly the named chapters, in
// the named order, each carried by a numbered heading inside a section. A
// chapter that drifts out of the reader's order, or one that quietly returns
// after it was dropped, fails here.
func TestTheGuideCarriesTheChaptersItPromises(t *testing.T) {
	body := userGuidePath(t)

	var found []string

	for _, m := range regexp.MustCompile(`(?is)<h2\b[^>]*\bid="([^"]+)"`).FindAllStringSubmatch(body, -1) {
		found = append(found, m[1])
	}

	if len(found) != len(userGuideChapters) {
		t.Errorf("the guide carries %d chapters (%s), want %d", len(found), strings.Join(found, ", "), len(userGuideChapters))
	}

	for i, want := range userGuideChapters {
		if i >= len(found) {
			t.Errorf("the guide is missing chapter %q at position %d", want, i+1)
			continue
		}

		if found[i] != want {
			t.Errorf("chapter %d is %q, want %q", i+1, found[i], want)
		}
	}

	for i, id := range userGuideChapters {
		heading := regexp.MustCompile(`(?is)<h2\b[^>]*\bid="`+id+`"[^>]*>(.*?)</h2>`).FindStringSubmatch(body)
		if len(heading) < 2 {
			t.Errorf("chapter %q has no heading of its own", id)
			continue
		}

		text := strings.TrimSpace(stripTags(heading[1]))
		if !regexp.MustCompile(`^`+strconv.Itoa(i+1)+`\D`).MatchString(text) {
			t.Errorf("chapter %q is headed %q, which does not carry its number %d", id, text, i+1)
		}

		chapter := userGuideChapter(t, body, id)
		if !strings.Contains(chapter, "</section>") {
			t.Errorf("chapter %q is not a section of the document", id)
		}
	}
}

// The contents aid is one flat list: every chapter, in reading order, each
// named by an anchor aimed at the chapter's own id. There are no parts.
func TestTheContentsAidListsEveryChapterInOrder(t *testing.T) {
	body := userGuidePath(t)

	nav := regexp.MustCompile(`(?is)<nav\b[^>]*class="toc"[^>]*>(.*?)</nav>`).FindStringSubmatch(body)
	if len(nav) < 2 {
		t.Fatalf("the guide carries no contents aid built as a nav with class toc")
	}

	open := regexp.MustCompile(`(?is)<nav\b[^>]*>`).FindString(nav[0])

	for _, attribute := range []string{`aria-label="Contents"`, `id="contents"`} {
		if !strings.Contains(open, attribute) {
			t.Errorf("the contents aid's opening tag does not carry %s: %s", attribute, open)
		}
	}

	var anchors []string

	for _, m := range regexp.MustCompile(`(?is)<a\b[^>]*\bhref="#([^"]+)"[^>]*>(.*?)</a>`).FindAllStringSubmatch(nav[1], -1) {
		anchors = append(anchors, m[1])

		if strings.TrimSpace(stripTags(m[2])) == "" {
			t.Errorf("the contents aid aims at %q with an anchor that says nothing", m[1])
		}
	}

	if len(anchors) != len(userGuideChapters) {
		t.Errorf("the contents aid lists %d chapters (%s), want %d", len(anchors), strings.Join(anchors, ", "), len(userGuideChapters))
	}

	for i, want := range userGuideChapters {
		if i < len(anchors) && anchors[i] != want {
			t.Errorf("the contents aid lists %q at position %d, want %q", anchors[i], i+1, want)
		}
	}

	if regexp.MustCompile(`(?is)<p\b[^>]*class="part"`).MatchString(nav[1]) {
		t.Errorf("the contents aid groups its chapters under part headings; the guide is one flat list")
	}
}

// A keyboard reader meets the skip link before anything else on the page, and
// the link goes somewhere the page actually carries.
func TestTheSkipLinkComesFirstAndAimsAtTheContents(t *testing.T) {
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

	if !strings.Contains(body, `id="contents"`) {
		t.Errorf("the guide's skip link aims at #contents, and the document carries no contents aid with that anchor")
	}
}

// The page announces itself the way the contract names it: an english
// document titled for the listener, with the project's own mark inlined
// beside the application's name in the banner.
func TestThePageAnnouncesItself(t *testing.T) {
	body := userGuidePath(t)

	if !regexp.MustCompile(`(?is)<html\b[^>]*\blang="en"`).MatchString(body) {
		t.Errorf("the guide does not declare itself english")
	}

	title := regexp.MustCompile(`(?is)<title>(.*?)</title>`).FindStringSubmatch(body)
	if len(title) < 2 || strings.TrimSpace(title[1]) != "Rdio Scanner — Listener Guide" {
		t.Errorf("the guide is titled %q, want the listener guide's own title", strings.TrimSpace(strings.Join(title[1:], " ")))
	}

	banner := regexp.MustCompile(`(?is)<header\b[^>]*class="site"[^>]*role="banner"[^>]*>.*?</header>`).FindString(body)
	if banner == "" {
		t.Fatalf("the guide has no banner header built as the contract names it")
	}

	if !strings.Contains(banner, "<main") && !strings.Contains(body, "<main") {
		t.Errorf("the guide has no main element holding the reading")
	}

	head := regexp.MustCompile(`(?is)<h1>(.*?)</h1>`).FindStringSubmatch(banner)
	if len(head) < 2 {
		t.Fatalf("the banner carries no heading of its own")
	}

	mark := regexp.MustCompile(`(?is)<img\b[^>]*class="mark"[^>]*>`).FindString(head[1])
	if mark == "" {
		t.Fatalf("the banner carries no mark image beside the application's name")
	}

	alt := regexp.MustCompile(`(?i)alt="([^"]*)"`).FindStringSubmatch(mark)
	if len(alt) < 2 || strings.TrimSpace(alt[1]) == "" {
		t.Errorf("the banner's mark carries no alternative text: %s", mark)
	}

	if !regexp.MustCompile(`(?i)\bdata-logo\b`).MatchString(mark) {
		t.Errorf("the banner's mark is not the slot the picture step fills: %s", mark)
	}

	src := regexp.MustCompile(`(?i)src="([^"]*)"`).FindStringSubmatch(mark)
	if len(src) < 2 || !strings.HasPrefix(src[1], "data:image/png;base64,") {
		t.Errorf("the banner's mark is not the project's own drawing inlined as a data URI, so a reader with no network gets an empty frame beside the application's name: %s", mark)
	}
}

// The version line at the head of the page names the release the reader has,
// read from the repository's own version, not a number the guide carries.
func TestTheGuideStatesTheVersionItDocuments(t *testing.T) {
	body := userGuidePath(t)

	version := regexp.MustCompile(`(?is)<p class="meta"[^>]*>\s*Version (\d+\.\d+(?:\.\d+)?)`).FindStringSubmatch(body)
	if len(version) < 2 {
		t.Fatalf("the guide does not state a version at the head of the page")
	}

	want := strings.TrimSpace(repoFile(t, "VERSION.txt"))

	if version[1] != want {
		t.Errorf("the guide says it covers version %q, the repository is %q", version[1], want)
	}
}

// The document's last line is the project's sign-off, the same farewell the
// administrator's reader is given.
func TestTheFooterClosesWithTheSignOff(t *testing.T) {
	body := userGuidePath(t)

	footer := regexp.MustCompile(`(?is)<footer\b[^>]*class="site"[^>]*role="contentinfo"[^>]*>.*?</footer>`).FindString(body)
	if footer == "" {
		t.Fatalf("the guide has no footer built as the contract names it")
	}

	paragraphs := regexp.MustCompile(`(?is)<p\b[^>]*>(.*?)</p>`).FindAllStringSubmatch(footer, -1)
	if len(paragraphs) == 0 {
		t.Fatalf("the guide's footer says nothing")
	}

	last := strings.TrimSpace(stripTags(paragraphs[len(paragraphs)-1][1]))

	if last != "Happy Rdio Scanning !" {
		t.Errorf("the guide closes with %q, want the project's sign-off", last)
	}
}

// Every screenshot of the manifest sits in the chapter that explains it, and
// only those screenshots are in the guide. Each figure is a picture with its
// own words: an alternative for the picture and a caption under it, and the
// two are never the same sentence.
func TestTheGuideCarriesEveryFigureOfTheManifest(t *testing.T) {
	body := userGuidePath(t)

	wanted := map[string]string{}
	for _, figure := range userGuideFigures {
		wanted[figure.shot] = figure.chapter
	}

	found := map[string]bool{}

	for _, m := range regexp.MustCompile(`(?is)<figure\b[^>]*\bdata-shot="([^"]*)"[^>]*>.*?</figure>`).FindAllStringSubmatch(body, -1) {
		shot := m[1]
		found[shot] = true

		owner, wantedHere := wanted[shot]
		if !wantedHere {
			continue
		}

		chapter := userGuideChapter(t, body, owner)
		if !strings.Contains(chapter, m[0]) {
			t.Errorf("the figure %q is not inside the chapter that explains it (%q)", shot, owner)
		}

		img := regexp.MustCompile(`(?is)<img\b[^>]*>`).FindString(m[0])
		if img == "" {
			t.Errorf("the figure %q carries no picture", shot)
			continue
		}

		alt := regexp.MustCompile(`(?i)alt="([^"]*)"`).FindStringSubmatch(img)
		if len(alt) < 2 || strings.TrimSpace(alt[1]) == "" {
			t.Errorf("the figure %q carries a picture with no alternative text", shot)
		}

		src := regexp.MustCompile(`(?i)src="([^"]*)"`).FindStringSubmatch(img)
		if len(src) < 2 || !strings.HasPrefix(src[1], "data:image/png;base64,") {
			t.Errorf("the figure %q is not inlined as a picture, so a reader with no network gets an empty frame", shot)
		}

		caption := regexp.MustCompile(`(?is)<figcaption\b[^>]*>(.*?)</figcaption>`).FindStringSubmatch(m[0])
		if len(caption) < 2 || strings.TrimSpace(stripTags(caption[1])) == "" {
			t.Errorf("the figure %q carries no caption", shot)
			continue
		}

		if len(alt) >= 2 && strings.EqualFold(strings.TrimSpace(stripTags(alt[1])), strings.TrimSpace(stripTags(caption[1]))) {
			t.Errorf("the figure %q says the same sentence as the picture and under it", shot)
		}
	}

	for shot := range wanted {
		if !found[shot] {
			t.Errorf("the guide carries no figure %q", shot)
		}
	}

	if len(found) > len(wanted) {
		for shot := range found {
			if _, wantedHere := wanted[shot]; !wantedHere {
				t.Errorf("the guide carries a figure %q that no chapter explains", shot)
			}
		}
	}
}

// The guide renders from itself: every address it reaches through is a
// fragment or a data URI, its one stylesheet is inlined, and it carries no
// script at all, so the reading works from an extracted archive, a mail
// attachment, or a machine with no network.
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

	styles := regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style>`).FindAllStringSubmatch(body, -1)
	if len(styles) != 1 {
		t.Fatalf("the guide carries %d stylesheets, want one authored stylesheet inlined", len(styles))
	}

	for _, m := range regexp.MustCompile(`(?i)url\(([^)]*)\)`).FindAllStringSubmatch(styles[0][1], -1) {
		if !isSelfContained(strings.Trim(m[1], `"'`)) {
			t.Errorf("the guide's stylesheet reaches out to %q", m[1])
		}
	}

	if at := strings.Index(strings.ToLower(body), "<script"); at >= 0 {
		t.Errorf("the guide carries a script; the reading needs none")
	}
}

// The guide names no file the reader might not have. It may speak of the
// operator's guide in words, but a filename is an instruction a reader
// holding one file cannot follow.
func TestTheGuideNamesNoDocumentTheReaderLacks(t *testing.T) {
	body := userGuidePath(t)

	spoken := stripTags(body)

	for _, m := range regexp.MustCompile(`(?i)\b[a-z0-9][a-z0-9._-]*\.(?:html?|md|txt|json|css|js|png|jpe?g|svg|pdf)\b`).FindAllString(spoken, -1) {
		t.Errorf("the guide names the document %q, which a reader holding one file does not have", m)
	}
}

// The guide stays a file a reader carries: an attachment, a stick, a folder
// beside the archive.
func TestTheGuideStaysInsideItsSizeBudget(t *testing.T) {
	body := userGuidePath(t)

	if len(body) > 4<<20 {
		t.Errorf("the guide is %d bytes, over the %d byte budget", len(body), 4<<20)
	}
}

// The one authored stylesheet is inlined, and it carries what the contract
// promises: an @media print block, so a printed reading of the guide works.
func TestTheStylesheetCarriesItsPromisedBlocks(t *testing.T) {
	body := userGuidePath(t)

	style := regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style>`).FindStringSubmatch(body)
	if len(style) < 2 {
		t.Fatalf("the guide carries no inlined stylesheet")
	}

	if !regexp.MustCompile(`(?i)@media\s+print`).MatchString(style[1]) {
		t.Errorf("the guide carries no reading on paper")
	}
}

// Every list of screen facts is a table the reader can follow: captioned, with
// a header row, and with the first column naming each row.
func TestEveryTableIsACaptionedTranscription(t *testing.T) {
	body := userGuidePath(t)

	tables := regexp.MustCompile(`(?is)<table\b.*?</table>`).FindAllString(body, -1)
	if len(tables) == 0 {
		t.Fatalf("the guide carries no table at all")
	}

	for i, table := range tables {
		caption := regexp.MustCompile(`(?is)<caption\b[^>]*>(.*?)</caption>`).FindStringSubmatch(table)
		if len(caption) < 2 || strings.TrimSpace(stripTags(caption[1])) == "" {
			t.Errorf("table %d carries no caption naming what it transcribes", i+1)
		}

		if !regexp.MustCompile(`(?is)<th\b[^>]*scope="col"`).MatchString(table) {
			t.Errorf("table %d has no header row of column names", i+1)
		}

		if !regexp.MustCompile(`(?is)<th\b[^>]*scope="row"`).MatchString(table) {
			t.Errorf("table %d does not name its rows in the first column", i+1)
		}
	}
}

// A warning the reader can act on is set apart and labelled with its weight,
// and detail folded out of the reading opens with a word that says what is
// folded.
func TestTheCalloutsNameTheirWeightAndTheFoldoutsTheirSummary(t *testing.T) {
	body := userGuidePath(t)

	for _, m := range regexp.MustCompile(`(?is)<aside\b([^>]*)>(.*?)</aside>`).FindAllStringSubmatch(body, -1) {
		weight := ""

		if w := regexp.MustCompile(`(?i)class="[^"]*\b(note|caution)\b`).FindStringSubmatch(m[1]); len(w) > 1 {
			weight = strings.ToLower(w[1])
		} else {
			t.Errorf("a call-out carries no weight: <aside%s>", m[1])
			continue
		}

		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(stripTags(m[2]))), weight) {
			t.Errorf("a %s call-out does not open by naming its weight in words", weight)
		}
	}

	for _, block := range regexp.MustCompile(`(?is)<details\b.*?</details>`).FindAllString(body, -1) {
		summary := regexp.MustCompile(`(?is)<summary\b[^>]*>(.*?)</summary>`).FindStringSubmatch(block)
		if len(summary) < 2 || strings.TrimSpace(stripTags(summary[1])) == "" {
			t.Errorf("folded detail opens with nothing to open it by")
		}
	}
}

// The guide is written for a reader who has never seen an earlier version and
// for a reader who reads it as printed: no accessibility material, no version
// history, nothing that argues with a past the reader never lived through.
func TestTheGuideCarriesNoAccessibilityOrVersionHistoryMaterial(t *testing.T) {
	body := userGuidePath(t)

	spoken := strings.ToLower(stripTags(body))

	for _, gone := range []string{"accessib", "screen reader", "what's new", "what\u2019s new", "6.6.3"} {
		if strings.Contains(spoken, gone) {
			t.Errorf("the guide carries %q, which the reader was never promised", gone)
		}
	}
}
