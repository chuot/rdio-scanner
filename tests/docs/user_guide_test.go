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
	"io/fs"
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
		heading := regexp.MustCompile(`(?is)<h2\b[^>]*\bid="` + id + `"[^>]*>(.*?)</h2>`).FindStringSubmatch(body)
		if len(heading) < 2 {
			t.Errorf("chapter %q has no heading of its own", id)
			continue
		}

		text := strings.TrimSpace(stripTags(heading[1]))
		if !regexp.MustCompile(`^` + strconv.Itoa(i+1) + `\D`).MatchString(text) {
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

// The contents aid and the chapters share one two-column layout container: the
// stylesheet's sticky-sidebar rules select the nav inside it, and without the
// container the contents fall out of the sidebar and stack above the chapters.
func TestTheContentsAndTheChaptersShareTheLayoutContainer(t *testing.T) {
	body := userGuidePath(t)

	if !regexp.MustCompile(`(?is)<div\b[^>]*\bclass="layout"[^>]*>`).MatchString(body) {
		t.Fatalf("the guide carries no div with class layout to hold the contents beside the chapters")
	}

	nav := regexp.MustCompile(`(?is)<nav\b[^>]*class="toc"[\s\S]*?</nav>`).FindString(body)
	main := regexp.MustCompile(`(?is)<main\b[\s\S]*?</main>`).FindString(body)

	if nav == "" || main == "" {
		t.Fatalf("the guide is missing its contents nav or its main content")
	}

	open := regexp.MustCompile(`(?is)<div\b[^>]*\bclass="layout"[^>]*>`).FindStringIndex(body)
	footer := strings.Index(body, "<footer")
	if footer < 0 {
		t.Fatalf("the guide carries no footer to close the content")
	}

	if open[1] >= footer || !strings.Contains(body[open[1]:footer], "</div>") {
		t.Errorf("the layout container does not close after the content it wraps")
		return
	}

	inside := body[open[1]:footer]

	if !strings.Contains(inside, nav) {
		t.Errorf("the contents nav sits outside the layout container")
	}

	if !strings.Contains(inside, main) {
		t.Errorf("the main content sits outside the layout container")
	}
}

// The contents aid carries the markup the stylesheet styles: a heading naming
// it and one ordered list with one item per chapter. Bare anchors match none
// of the list or heading rules, and the narrow-screen column rules for the
// list do nothing.
func TestTheContentsAidCarriesAHeadingAndOneOrderedList(t *testing.T) {
	body := userGuidePath(t)

	nav := regexp.MustCompile(`(?is)<nav\b[^>]*class="toc"[^>]*>(.*?)</nav>`).FindStringSubmatch(body)
	if len(nav) < 2 {
		t.Fatalf("the guide carries no contents aid built as a nav with class toc")
	}

	heading := regexp.MustCompile(`(?is)<h2\b[^>]*>(.*?)</h2>`).FindStringSubmatch(nav[1])
	if len(heading) < 2 || strings.TrimSpace(stripTags(heading[1])) != "Contents" {
		t.Errorf("the contents aid carries no h2 reading \"Contents\"")
	}

	lists := regexp.MustCompile(`(?is)<ol\b`).FindAllStringIndex(nav[1], -1)
	if len(lists) != 1 {
		t.Errorf("the contents aid carries %d ordered lists, want exactly 1", len(lists))
		return
	}

	list := regexp.MustCompile(`(?is)<ol\b[^>]*>([\s\S]*?)</ol>`).FindStringSubmatch(nav[1])
	if len(list) < 2 {
		t.Fatalf("the contents aid's ordered list does not close")
	}

	items := regexp.MustCompile(`(?is)<li\b[^>]*>\s*<a\b[^>]*\bhref="#([^"]+)"[^>]*>\s*(.*?)\s*</a>\s*</li>`).FindAllStringSubmatch(list[1], -1)
	if len(items) != len(userGuideChapters) {
		t.Errorf("the contents list carries %d items, want %d", len(items), len(userGuideChapters))
	}

	for i, want := range userGuideChapters {
		if i < len(items) && items[i][1] != want {
			t.Errorf("the contents list aims at %q at position %d, want %q", items[i][1], i+1, want)
		}
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
	t.Logf("measured size: %d bytes, budget %d bytes", len(body), 4<<20)
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

// The guide quotes whole messages the application prints, and one of them runs
// longer than a narrow page is wide. Held to a single line, such a quote pushes the
// page sideways, and the words past the edge of a sideways scroll are out of reach
// for anyone reading with a magnifier or moving through the document with a
// keyboard. A quoted message in a paragraph or a list item is therefore free to
// wrap where its paragraph ends, at every width the page is read at.
func TestAQuotedScreenMessageWrapsInsideItsParagraph(t *testing.T) {
	body := userGuidePath(t)

	longest := ""
	prose := regexp.MustCompile(`(?is)<p\b[^>]*>.*?</p>|<li\b[^>]*>.*?</li>`).FindAllString(body, -1)
	for _, passage := range prose {
		for _, quote := range regexp.MustCompile(`(?is)<code\b[^>]*>(.*?)</code>`).FindAllStringSubmatch(passage, -1) {
			if text := strings.TrimSpace(stripTags(quote[1])); len(text) > len(longest) {
				longest = text
			}
		}
	}

	if len(longest) < 60 {
		t.Fatalf("the guide quotes no message from the application inside a paragraph or a list item; "+
			"the longest quote it carries there is %d characters", len(longest))
	}

	last := ""
	for _, rule := range regexp.MustCompile(`(?is)([^{}]+)\{([^{}]*)\}`).FindAllStringSubmatch(userGuideBaseStylesheet(t), -1) {
		if !ruleReachesCodeInProse(rule[1]) {
			continue
		}
		for _, decl := range strings.Split(rule[2], ";") {
			name, value, found := strings.Cut(decl, ":")
			if !found || !strings.EqualFold(strings.TrimSpace(name), "white-space") {
				continue
			}
			last = strings.ToLower(strings.TrimSpace(value))
		}
	}

	// With nothing said about it, a code span wraps the way the words around it do.
	if last == "nowrap" || last == "pre" {
		t.Errorf("the guide holds a quoted message to one line: white-space is %q for a code span in a "+
			"paragraph, and the %d character quote %q pushes the page sideways at narrow widths",
			last, len(longest), longest)
	}
}

// userGuideBaseStylesheet is the guide's one inlined stylesheet with every @media
// block taken out: that is what the page is styled with before any narrow-width
// exemption has a chance to speak, and it is what a wide reading of the page uses.
func userGuideBaseStylesheet(t *testing.T) string {
	t.Helper()

	style := regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style>`).FindStringSubmatch(userGuidePath(t))
	if len(style) < 2 {
		t.Fatalf("the guide carries no inlined stylesheet")
	}

	out := style[1]
	for {
		at := regexp.MustCompile(`(?is)@media[^{]*\{`).FindStringIndex(out)
		if at == nil {
			return out
		}
		end := stylesheetBlockEnd(out, at[1]-1)
		if end < 0 {
			t.Fatalf("the guide's stylesheet carries an @media block that never closes")
		}
		out = out[:at[0]] + out[end:]
	}
}

// stylesheetBlockEnd returns the index just past the brace that closes the block
// opened at open, or -1 when the block never closes.
func stylesheetBlockEnd(stylesheet string, open int) int {
	depth := 0
	for i := open; i < len(stylesheet); i++ {
		switch stylesheet[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

// ruleReachesCodeInProse reports whether a rule's selector list reaches a code span
// sitting in a paragraph or a list item, which is where the guide quotes what the
// application prints.
func ruleReachesCodeInProse(selector string) bool {
	for _, one := range strings.Split(selector, ",") {
		one = strings.ReplaceAll(one, ">", " ")
		one = strings.Join(strings.Fields(one), " ")
		if one == "p code" || one == "li code" ||
			strings.HasSuffix(one, " p code") || strings.HasSuffix(one, " li code") {
			return true
		}
	}
	return false
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

// ---------------------------------------------------------------------------
// Fact tests
//
// A fact is stated by the application's own printed name: the guide has to
// carry each name the shipped application prints where the reader meets it,
// and every name the guide prints has to be one the shipped application
// prints. Neither check compares the guide to a sentence the guide writes.
// ---------------------------------------------------------------------------

// userGuideSpokenText is the guide with its markup removed and its runs of
// whitespace collapsed, so a name the guide carries across a line break or a
// tag still reads as one name.
func userGuideSpokenText(t *testing.T, body string) string {
	t.Helper()

	return strings.Join(strings.Fields(stripTags(body)), " ")
}

// requireUserGuideFacts fails over every fact name the guide does not carry,
// each name matched as a whole unit. A pin states the fact; how the guide
// words it around the name is free. A name passed with a trailing space is
// matched as a prefix: the application prints it followed by a live value.
func requireUserGuideFacts(t *testing.T, text string, facts ...string) {
	t.Helper()

	for _, name := range facts {
		units := `(^|[^A-Za-z0-9])` + regexp.QuoteMeta(strings.TrimSpace(name))

		if !strings.HasSuffix(name, " ") {
			units += `([^A-Za-z0-9]|$)`
		}

		if !regexp.MustCompile(`(?s)` + units).MatchString(text) {
			t.Errorf("the guide never carries the name %q, which the shipped application prints", name)
		}
	}
}

// scannerFrontendRoot is the listener application's own folder: every name the
// guide quotes is transcribed from the sources kept there.
func scannerFrontendRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", "..", "internal", "presentation", "scanner", "frontend"))
	if err != nil {
		t.Fatalf("the listener source path does not resolve: %v", err)
	}

	return root
}

// listenerPrintedText is the text the shipped listener application shows: the
// words inside the markup its templates assemble, the values of the
// attributes that name its controls, and the strings it assigns to the
// display. The values the application computes at run time — the ${...} spans
// in its sources — read as spaces, so a name the guide carries with a live
// value still matches the source that prints it.
func listenerPrintedText(t *testing.T) string {
	t.Helper()

	root := scannerFrontendRoot(t)

	var texts []string

	for _, name := range []string{"index.html", "main.js"} {
		texts = append(texts, listenerSourceText(t, filepath.Join(root, name)))
	}

	err := fs.WalkDir(os.DirFS(root), "presentation", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() && strings.HasSuffix(p, ".js") {
			texts = append(texts, listenerSourceText(t, filepath.Join(root, p)))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("the listener sources cannot be read: %v", err)
	}

	common := filepath.Join("..", "..", "internal", "presentation", "common", "frontend")

	err = fs.WalkDir(os.DirFS(common), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() && strings.HasSuffix(p, ".js") {
			texts = append(texts, listenerSourceText(t, filepath.Join(common, p)))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("the shared listener sources cannot be read: %v", err)
	}

	return strings.Join(texts, " ")
}

func listenerSourceText(t *testing.T, file string) string {
	t.Helper()

	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("the listener source %s cannot be read: %v", file, err)
	}

	text := string(raw)

	text = regexp.MustCompile(`\$\{[^{}]*\}`).ReplaceAllString(text, " ")

	var named []string

	for _, m := range regexp.MustCompile(`(?i)\b(?:aria-label|title|placeholder)="([^"]*)"`).FindAllStringSubmatch(text, -1) {
		named = append(named, m[1])
	}

	// markup tags: a tag name letter sits right after the opening bracket,
	// so the angle brackets of comparisons like i < len stay untouched.
	text = regexp.MustCompile(`(?i)<[a-zA-Z][a-zA-Z0-9]*(\s[^>]*)?>`).ReplaceAllString(text, " ")

	// any angle bracket left over is a glued edge of markup or a comparison:
	// either way it is not part of a word the reader sees.
	text = strings.NewReplacer("<", " ", ">", " ", "&nbsp;", " ").Replace(text)

	// the application assembles its text inside quoted strings: the quotes
	// are not part of what the reader sees, so they read as spaces here.
	text = strings.NewReplacer("`", " ", "'", " ", "\"", " ").Replace(text)

	return strings.Join(strings.Fields(strings.Join(named, " ")+" "+text), " ")
}

// Every name the guide prints in code type is a name the shipped application
// prints: a caption, heading, label, message, or table header the reader can
// find on the screen, transcribed from the application's own sources.
func TestEveryQuotedNameIsPrintedByTheApplication(t *testing.T) {
	body := userGuidePath(t)

	printed := " " + listenerPrintedText(t) + " "

	for _, m := range regexp.MustCompile(`(?is)<code\b[^>]*>(.*?)</code>`).FindAllStringSubmatch(body, -1) {
		name := strings.Join(strings.Fields(stripTags(m[1])), " ")

		if name == "" {
			t.Errorf("the guide prints an empty code span, which names nothing")
			continue
		}

		if !strings.Contains(printed, " "+name+" ") {
			t.Errorf("the guide prints the name %q, which the shipped application never prints", name)
		}
	}
}

// The reader is told about every control the control bar carries, by the name
// the bar prints, and about the panels, the fullscreen control, the light's
// own name, and the prompt the page puts up when the reader leaves with the
// feed on.
func TestTheGuideCarriesThePageAndItsChrome(t *testing.T) {
	body := userGuidePath(t)
	text := userGuideSpokenText(t, body)

	requireUserGuideFacts(t, text,
		"LIVE FEED", "HOLD SYS", "HOLD TG", "REPLAY LAST", "SKIP NEXT", "AVOID", "SEARCH CALL", "PAUSE", "SELECT TG",
		"Search", "Close search panel", "Select", "Close select panel",
		"Fullscreen", "Exit fullscreen",
		"Server status",
		"Live Feed is ON, do you really want to leave?",
	)
}

// The display at rest is described by the placeholder rows it actually shows,
// and by the version line it carries until the first call arrives.
func TestTheGuideCarriesTheDisplayAtRest(t *testing.T) {
	body := userGuidePath(t)
	text := userGuideSpokenText(t, body)

	requireUserGuideFacts(t, text,
		"System", "Tag", "Talkgroup", "TGID: 0", "E: 0 S: 0", "UID: 0", "00:00", "Rdio Scanner v",
		"F: ", "Hz",
	)
}

// The display during a call is described by the rows and readings it prints,
// the flags it can raise, and the history table's own headers.
func TestTheGuideCarriesTheDisplayDuringACall(t *testing.T) {
	body := userGuidePath(t)
	text := userGuideSpokenText(t, body)

	requireUserGuideFacts(t, text,
		"NO LINK", "L:", "Q:", "F:", "TGID:", "E:", "S:", "UID:",
		"AVOID", "PATCH", "DELAYED", "⧖",
		"Time", "System", "Talkgroup", "Name",
	)
}

// The display prints a short type name, and the reader who has never held a
// radio has to be told what it is. The chapter that walks the screen says what
// the system type is and names every type the server can write; the quick-
// reference word list carries the same term for the reader who goes straight
// to the back of the guide.
//
// userGuideSystemTypes are the type names that chapter prints, in the order it
// prints them, and the same names the administration offers beside a system and
// beside a talkgroup.
var userGuideSystemTypes = []string{"AM", "DMR", "EDACS", "FM", "NFM", "NXDN", "P25", "Provoice", "SmartNet", "Tetra"}

// numberWords are the written counts the guide's sentences use, so a count stated in
// words can be read against the number of names the sentence beside it prints.
var numberWords = map[string]int{
	"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7,
	"eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12,
}

func TestTheGuideExplainsTheSystemType(t *testing.T) {
	body := userGuidePath(t)

	chapter := userGuideChapter(t, body, "ch-06-the-screen")

	display := userGuideSpokenText(t, chapter)
	requireUserGuideFacts(t, display,
		"system type", "modulation",
		"AM", "DMR", "EDACS", "FM", "NFM", "NXDN", "P25", "Provoice", "SmartNet", "Tetra",
	)

	// The chapter counts the names it prints, and the count is the number of names
	// the reader can actually read in the list beside it.
	counted := regexp.MustCompile(`(?is)([A-Za-z]+|\d+)\s+names are possible:\s*(.*?)\.\s`).FindStringSubmatch(chapter)

	if counted == nil {
		t.Fatalf("the chapter that names the system types no longer states the count beside the list of names")
	}

	want := numberWords[strings.ToLower(counted[1])]

	if want == 0 {
		if digits, err := strconv.Atoi(counted[1]); err == nil {
			want = digits
		}
	}

	printed := []string{}

	for _, piece := range regexp.MustCompile(`(?is)<em>\s*([^<]+?)\s*</em>`).FindAllStringSubmatch(counted[2], -1) {
		printed = append(printed, strings.TrimSpace(piece[1]))
	}

	if len(printed) != want {
		t.Errorf("the chapter says %s names are possible and then prints %d: %v",
			counted[1], len(printed), printed)
	}

	if strings.Join(printed, ", ") != strings.Join(userGuideSystemTypes, ", ") {
		t.Errorf("the chapter prints %v as the type names, want %v", printed, userGuideSystemTypes)
	}

	glossary := userGuideChapter(t, body, "ch-11-quick-reference")
	if !regexp.MustCompile(`(?is)<th scope="row">\s*<em>\s*system type\b`).MatchString(glossary) {
		t.Errorf("the quick-reference word list carries no row for the system type")
	}
}

// TestTheGuideExplainsAgencyFleetSubfleetNumbering covers the reader who sees a
// talkgroup written as three numbers joined by hyphens and has nothing in the guide to
// say what the three numbers are. The chapter that walks the screen names the form, its
// three parts, the two types that produce it, the pages that show it, and the one page
// that keeps the plain decimal number.
func TestTheGuideExplainsAgencyFleetSubfleetNumbering(t *testing.T) {
	body := userGuidePath(t)

	chapter := userGuideSpokenText(t, userGuideChapter(t, body, "ch-06-the-screen"))

	// Each fact has to sit in the one sentence that says what it is: the form beside the
	// two types that number talkgroups that way, the form beside the pages that show it,
	// and the decimal id beside the pages that keep it. A name the chapter carries
	// somewhere else does not tell the reader what they are looking at.
	for _, want := range [][]string{
		{"EDACS", "Provoice", "agency", "fleet", "subfleet"},
		{"agency-fleet-subfleet", "listening"},
		{"decimal", "administration"},
	} {
		if !guideChapterSays(chapter, want) {
			t.Errorf("no sentence of the chapter that walks the screen says all of %q at once", strings.Join(want, ", "))
		}
	}
}

// guideChapterSays reports whether one sentence of the chapter carries every name the
// claim needs, whatever case the guide prints them in, so a fact stated somewhere in the
// chapter but never beside its own subject does not pass.
func guideChapterSays(text string, wants []string) bool {
	for _, sentence := range regexp.MustCompile(`\.\s+|\.\n`).Split(text, -1) {
		lowered := strings.ToLower(sentence)
		found := true

		for _, want := range wants {
			if !strings.Contains(lowered, strings.ToLower(want)) {
				found = false

				break
			}
		}

		if found {
			return true
		}
	}

	return false
}

// The name's spoken form is the one the operator's guide teaches: the written
// name, its pronunciation spelling, and its syllable reading. The guide teaches
// that form and never teaches another.
func TestTheGuideCarriesTheSpokenFormOfTheName(t *testing.T) {
	body := userGuidePath(t)
	text := userGuideSpokenText(t, body)

	requireUserGuideFacts(t, text, "/ˈreɪ.di.oʊ ˈskæn.ər/", "RAY-dee-oh SCAN-er")

	if at := strings.Index(strings.ToLower(text), "raydio"); at >= 0 {
		t.Errorf("the guide teaches a spoken form the project does not use: %s", text[max(0, at-60):at+60])
	}
}

// The select panel is described by the small buttons it prints and by the
// answer it gives when the server has configured nothing.
func TestTheGuideCarriesTheSelectPanelFacts(t *testing.T) {
	body := userGuidePath(t)
	text := userGuideSpokenText(t, body)

	requireUserGuideFacts(t, text,
		"OFF", "ON", "No talkgroup configured!",
	)
}

// The search panel is described by the filter form's own labels and starting
// choices, the results table's own headers, and the names its row actions
// carry.
func TestTheGuideCarriesTheSearchPanelFacts(t *testing.T) {
	body := userGuidePath(t)
	text := userGuideSpokenText(t, body)

	requireUserGuideFacts(t, text,
		"Sort order", "Ascending", "Descending",
		"Date", "System", "Talkgroup", "Group", "Tag",
		"All Systems", "All Talkgroups", "All Groups", "All Tags",
		"Reset", "Dur",
		"Play", "Stop", "Download", "Loading",
	)
}

// The access-code chapter carries the field's own label and the refusals the
// application prints, word for word.
func TestTheGuideCarriesTheAccessCodeFacts(t *testing.T) {
	body := userGuidePath(t)
	text := userGuideSpokenText(t, body)

	requireUserGuideFacts(t, text,
		"Unlock code",
		"This unlock code has expired",
		"Invalid unlock code",
		"Too many connections",
	)
}

// The phone chapter carries the update dialog's own title, question, and
// actions, and the help chapter carries the help notice's own opening words.
func TestTheGuideCarriesThePhoneAndUpdateFacts(t *testing.T) {
	body := userGuidePath(t)
	text := userGuideSpokenText(t, body)

	requireUserGuideFacts(t, text,
		"New version available",
		"A new version of the scanner webapp is available. Would you like to reload the page?",
		"No", "Yes",
		"For further assistance please contact",
	)
}
