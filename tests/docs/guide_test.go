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
	"encoding/json"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"rdio-scanner/internal/domain"
)

// these checks hold the administrator guide to one self-contained file: it carries
// everything it renders and every chapter it promises.

// guideFile is the guide the tests read: the file the reader opens, in the documentation folder.
func guideFile(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs(filepath.Join("..", "..", "docs", "admin-guide.html"))
	if err != nil {
		t.Fatalf("the guide path does not resolve: %v", err)
	}

	return path
}

func guidePath(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(guideFile(t))
	if err != nil {
		t.Fatalf("the guide is not readable at %s: %v", guideFile(t), err)
	}

	return string(body)
}

// repoFile reads a file of the repository the guide documents, the source its tables
// are transcribed from.
func repoFile(t *testing.T, parts ...string) string {
	t.Helper()

	path, err := filepath.Abs(filepath.Join(append([]string{"..", ".."}, parts...)...))
	if err != nil {
		t.Fatalf("%s does not resolve: %v", filepath.Join(parts...), err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s is not readable: %v", filepath.Join(parts...), err)
	}

	return string(body)
}

// stripTags leaves the words a table cell carries, which is what a reader reads.
func stripTags(cell string) string {
	return strings.TrimSpace(regexp.MustCompile(`(?is)<[^>]*>`).ReplaceAllString(cell, " "))
}

// tableWithCaption returns the table the reader sees under a caption.
func tableWithCaption(t *testing.T, body, caption string) string {
	t.Helper()

	for _, table := range regexp.MustCompile(`(?is)<table\b.*?</table>`).FindAllString(body, -1) {
		if strings.Contains(table, "<caption>"+caption+"</caption>") {
			return table
		}
	}

	t.Fatalf("the guide has no table captioned %q", caption)

	return ""
}

// guideChapters lists the guide's chapters in the order a reader works through them.
var guideChapters = []string{
	"ch-01-overview",
	"ch-02-how-audio-flows",
	"ch-03-requirements",
	"ch-04-installation",
	"ch-05-first-run",
	"ch-06-flags-and-ini",
	"ch-07-database",
	"ch-08-administration-interface",
	"ch-09-access-and-api-keys",
	"ch-10-getting-audio-in",
	"ch-11-systems-sites-talkgroups-units",
	"ch-12-groups-and-tags",
	"ch-13-options",
	"ch-14-logs",
	"ch-15-tools",
	"ch-16-rest-api",
	"ch-17-mcp-server",
	"ch-18-command-line-client",
	"ch-19-tls",
	"ch-20-reverse-proxies",
	"ch-21-running-as-a-service",
	"ch-22-security-hardening",
	"ch-23-backup-and-restore",
	"ch-24-upgrading-and-migrating",
	"ch-25-troubleshooting",
	"ch-26-faq",
	"ch-27-reference",
}

// subresourcePattern matches the tags that pull a resource into the page to render
// it; navigation links are not matched.
var subresourcePattern = regexp.MustCompile(`(?i)<(img|link|script|style|source|video|audio|iframe|object|embed|input|track|picture|use|image)\b[^>]*>`)

var attributePattern = regexp.MustCompile(`(?i)\b(src|href|srcset|data|poster|action|xlink:href)\s*=\s*("([^"]*)"|'([^']*)')`)

// isSelfContained reports whether an address renders from inside the document: a
// fragment or a data URI, not a fetch or a file beside the document.
func isSelfContained(value string) bool {
	v := strings.TrimSpace(value)

	switch {
	case strings.HasPrefix(v, "#"):
		return true
	case strings.HasPrefix(v, "data:"):
		return true
	case strings.HasPrefix(v, "http://"), strings.HasPrefix(v, "https://"), strings.HasPrefix(v, "//"):
		return false
	}

	// Anything else names a file beside the document.
	return false
}

// the guide carries every image, stylesheet, and script inside itself, so a reader
// with no network gets the whole document.
func TestTheGuideCarriesEverythingItRendersInsideItself(t *testing.T) {
	body := guidePath(t)

	missing := []string{}

	for _, tag := range subresourcePattern.FindAllString(body, -1) {
		for _, match := range attributePattern.FindAllStringSubmatch(tag, -1) {
			value := match[3]

			if value == "" {
				value = match[4]
			}

			if value == "" {
				continue
			}

			if !isSelfContained(value) {
				missing = append(missing, tag)

				break
			}
		}
	}

	if len(missing) > 0 {
		t.Errorf("the guide reaches out for %d resource(s) it does not carry, first: %s", len(missing), missing[0])
	}

	if strings.Contains(body, "@import") {
		t.Errorf("the guide imports a stylesheet from outside itself")
	}

	for _, m := range regexp.MustCompile(`url\(([^)]*)\)`).FindAllStringSubmatch(body, -1) {
		value := strings.Trim(strings.TrimSpace(m[1]), `"'`)

		if !isSelfContained(value) {
			t.Errorf("the guide's stylesheet reaches out for %q", value)

			break
		}
	}
}

// the guide carries every chapter it promises, in the order the reader works through
// them, and the contents aid names every one.
func TestTheGuideCarriesEveryChapterInTheOrderTheReaderWorksThrough(t *testing.T) {
	body := guidePath(t)

	headings := regexp.MustCompile(`(?i)<h2[^>]*\bid="([^"]+)"`).FindAllStringSubmatch(body, -1)

	var got []string

	for _, m := range headings {
		got = append(got, m[1])
	}

	if len(got) != len(guideChapters) {
		t.Fatalf("the guide carries %d chapters, want %d: %v", len(got), len(guideChapters), got)
	}

	for i, want := range guideChapters {
		if got[i] != want {
			t.Errorf("chapter %d is %q, want %q", i+1, got[i], want)
		}
	}
}

// the contents aid names every chapter, and every anchor it names exists.
func TestTheContentsAidNamesEveryChapter(t *testing.T) {
	body := guidePath(t)

	nav := regexp.MustCompile(`(?is)<nav\b[^>]*aria-label="Contents"[^>]*>(.*?)</nav>`).FindStringSubmatch(body)
	if len(nav) < 2 {
		t.Fatalf("the guide has no contents aid labelled Contents")
	}

	links := regexp.MustCompile(`(?i)href="([^"]+)"`).FindAllStringSubmatch(nav[1], -1)

	want := map[string]bool{}

	for _, id := range guideChapters {
		want["#"+id] = true
	}

	found := map[string]bool{}

	for _, m := range links {
		found[m[1]] = true
		delete(want, m[1])
	}

	if len(want) > 0 {
		t.Errorf("the contents aid does not name %d chapter(s), starting with %q", len(want), firstKey(want))
	}

	for href := range found {
		if !strings.HasPrefix(href, "#") {
			continue
		}

		if !strings.Contains(body, `id="`+strings.TrimPrefix(href, "#")+`"`) {
			t.Errorf("the contents aid points at %q, which the document does not carry", href)
		}
	}
}

func firstKey(m map[string]bool) string {
	for k := range m {
		return k
	}

	return ""
}

// chapterOf returns one chapter's body: from its heading to the end of the section
// that carries it.
func chapterOf(t *testing.T, body string, id string) string {
	t.Helper()

	start := strings.Index(body, `id="`+id+`"`)
	if start < 0 {
		t.Fatalf("the guide does not carry the chapter %q", id)
	}

	end := strings.Index(body[start:], "</section>")
	if end < 0 {
		t.Fatalf("the chapter %q does not end", id)
	}

	return body[start : start+end]
}

// subsectionOf returns one subsection's body: from its heading to the next heading of any
// level. chapterOf answers with a whole chapter, and a chapter carries several subsections,
// so a claim the guide makes in one of them is read where the reader looks for it.
func subsectionOf(t *testing.T, body string, heading string) string {
	t.Helper()

	open := regexp.MustCompile(`(?is)<h3\b[^>]*>` + regexp.QuoteMeta(heading) + `</h3>`)

	at := open.FindStringIndex(body)
	if at == nil {
		t.Fatalf("the guide has no %q subsection, which is where a reader looks for it", heading)
	}

	rest := body[at[1]:]

	end := len(rest)

	if next := regexp.MustCompile(`(?is)<h[23]\b[^>]*>`).FindStringIndex(rest); next != nil {
		end = next[0]
	}

	return rest[:end]
}

// the guide's header carries the project mark inline, so it shows whole on a machine
// with no network.
func TestTheGuideShowsTheMark(t *testing.T) {
	body := guidePath(t)

	header := regexp.MustCompile(`(?is)<header\b[^>]*class="site"[^>]*>(.*?)</header>`).FindStringSubmatch(body)
	if len(header) < 2 {
		t.Fatalf("the guide has no header to carry the mark")
	}

	mark := regexp.MustCompile(`(?is)<img\b[^>]*>`).FindStringSubmatch(header[1])
	if len(mark) < 1 {
		t.Fatalf("the guide's header carries no picture beside the application's name")
	}

	tag := mark[0]

	if !strings.Contains(tag, "data-logo") {
		t.Errorf("the header's picture is not the mark slot: %s", tag)
	}

	if !strings.Contains(tag, `src="data:image/png;base64,`) {
		t.Errorf("the header's mark is not carried inside the document: %s", strings.TrimSuffix(tag[:60], ">"))
	}

	if !regexp.MustCompile(`(?is)\balt="[^"]+"`).MatchString(tag) {
		t.Errorf("the header's mark carries no description of what it shows")
	}

	title := regexp.MustCompile(`(?is)<h1\b[^>]*>(.*?)</h1>`).FindStringSubmatch(header[1])
	if len(title) < 2 || !strings.Contains(regexp.MustCompile(`(?is)<[^>]*>`).ReplaceAllString(title[1], ""), "Rdio Scanner") {
		t.Errorf("the header's name is not the application's name")
	}
}

// the guide marks the superseded upload endpoint in the chapter a reader is in, not
// in one they skipped.
func TestTheGuideMarksTheSupersededUploadEndpoint(t *testing.T) {
	body := guidePath(t)

	// checks run against the text with line breaks collapsed.
	text := strings.Join(strings.Fields(body), " ")

	for _, fact := range []string{
		"Deprecated",
		"/api/call-upload",
		"a future release will remove it",
		"is not read as a sidecar",
	} {
		if !strings.Contains(text, fact) {
			t.Errorf("the guide does not state %q", fact)
		}
	}

	chapters := regexp.MustCompile(`(?is)<h2 id="(ch-[0-9]{2}-[a-z0-9-]+)">.*?</section>`).FindAllString(body, -1)

	for _, chapter := range chapters {
		if !strings.Contains(chapter, "trunk-recorder-call-upload") {
			continue
		}

		id := strings.Trim(regexp.MustCompile(`id="(ch-[0-9]{2}-[a-z0-9-]+)"`).FindString(chapter), `"`)

		if !strings.Contains(strings.ToLower(chapter), "deprecat") {
			t.Errorf("the chapter %s names the trunk-recorder upload endpoint without marking it deprecated", id)
		}

		if !strings.Contains(chapter, "/api/call-upload") {
			t.Errorf("the chapter %s marks the endpoint deprecated without naming the one that replaces it", id)
		}
	}
}

// the guide closes with the project's sign-off inside the file, so any copy carries it.
func TestTheGuideClosesWithTheSignOff(t *testing.T) {
	body := guidePath(t)

	footer := regexp.MustCompile(`(?is)<footer\b[^>]*>(.*?)</footer>`).FindStringSubmatch(body)
	if len(footer) < 2 {
		t.Fatalf("the guide has no footer to close with")
	}

	text := regexp.MustCompile(`(?is)<[^>]*>`).ReplaceAllString(footer[1], "\n")

	last := ""

	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			last = trimmed
		}
	}

	if last != "Happy Rdio Scanning !" {
		t.Errorf("the guide closes with %q, want the project's sign-off", last)
	}
}

// the opening chapter tells an administrator coming from the last release what
// changed, in groups named after what each change touches, and each group names the
// chapter that operates it.
var changesGroups = []struct {
	group    string
	chapters []string
}{
	{"The log", []string{"ch-14-logs"}},
	{"The database, and updating from version 6.6.3", []string{"ch-07-database", "ch-24-upgrading-and-migrating"}},
	{"The administration interface", []string{"ch-08-administration-interface", "ch-15-tools"}},
	{"Talkgroups, groups, and tags", []string{"ch-11-systems-sites-talkgroups-units", "ch-12-groups-and-tags"}},
	{"Getting audio in, and how calls are filed", []string{"ch-10-getting-audio-in", "ch-13-options"}},
	{"Interfaces other than the web pages", []string{"ch-16-rest-api", "ch-17-mcp-server"}},
	{"The release", []string{"ch-04-installation"}},
}

// changesSection returns chapter 1's changes since version 6.6.3: from that heading to
// the next heading of the same rank, so the checks hold over the groups and the caution
// that closes them and over nothing else in the chapter.
func changesSection(t *testing.T, body string) string {
	t.Helper()

	chapter := chapterOf(t, body, "ch-01-overview")

	start := strings.Index(chapter, "What&#39;s new since version 6.6.3")
	if start < 0 {
		start = strings.Index(chapter, "What's new since version 6.6.3")
	}

	if start < 0 {
		t.Fatalf("the opening chapter does not carry the changes since version 6.6.3")
	}

	rest := chapter[start:]

	if end := strings.Index(rest, "<h3>"); end >= 0 {
		return rest[:end]
	}

	return rest
}

// changesGroupBlocks returns the heading of each group of the changes section and the
// text that sits under it, up to the next group heading.
func changesGroupBlocks(section string) (headings, bodies []string) {
	groups := regexp.MustCompile(`(?is)<h4\b[^>]*>(.*?)</h4>`).FindAllStringSubmatchIndex(section, -1)

	for i, m := range groups {
		end := len(section)
		if i+1 < len(groups) {
			end = groups[i+1][0]
		}

		headings = append(headings, strings.TrimSpace(stripTags(section[m[2]:m[3]])))
		bodies = append(bodies, section[m[1]:end])
	}

	return headings, bodies
}

// chapterNumber reads the number an in-document chapter anchor carries, so a link can
// be read against the way the guide names that chapter in its sentences.
func chapterNumber(id string) string {
	m := regexp.MustCompile(`^ch-0*(\d+)`).FindStringSubmatch(id)
	if m == nil {
		return ""
	}

	return m[1]
}

func TestTheGuideTellsAVersion6AdministratorWhatChanged(t *testing.T) {
	body := guidePath(t)

	section := changesSection(t, body)

	if strings.Contains(strings.ToLower(section), "<table") {
		t.Errorf("the changes since version 6.6.3 are tabulated, where a reader has to scan rows and cells to find one change")
	}

	headings, blocks := changesGroupBlocks(section)

	want := []string{}
	for _, group := range changesGroups {
		want = append(want, group.group)
	}

	if !slices.Equal(headings, want) {
		t.Fatalf("the changes since version 6.6.3 carry the groups %q, want %q", headings, want)
	}

	linksAt := regexp.MustCompile(`(?i)href="([^"]+)"`)
	linkTexts := regexp.MustCompile(`(?is)<a\b[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)

	for i, group := range changesGroups {
		block := blocks[i]

		links := linksAt.FindAllStringSubmatch(block, -1)
		if len(links) == 0 {
			t.Errorf("the group %q names no chapter to operate what it states", group.group)
		}

		pointed := map[string]bool{}

		for _, m := range links {
			href := m[1]

			if !strings.HasPrefix(href, "#") {
				t.Errorf("the group %q points at %q, outside the document", group.group, href)

				continue
			}

			id := strings.TrimPrefix(href, "#")

			if !strings.Contains(body, `id="`+id+`"`) {
				t.Errorf("the group %q points at %q, which the document does not carry", group.group, href)

				continue
			}

			pointed[id] = true
		}

		for _, id := range group.chapters {
			if !pointed[id] {
				t.Errorf("the group %q does not point at chapter %s, which carries what it states", group.group, chapterNumber(id))
			}
		}

		// A link read out of context has to say where it lands, so the chapter number
		// sits in the link's own words or in the sentence the link rides in.
		sentence := strings.ToLower(stripTags(block))

		for _, m := range linkTexts.FindAllStringSubmatch(block, -1) {
			number := chapterNumber(strings.TrimPrefix(m[1], "#"))
			if number == "" {
				continue
			}

			text := strings.TrimSpace(stripTags(m[2]))

			if text == "" || text == number {
				t.Errorf("the group %q links chapter %s with %q, which names no destination", group.group, number, text)

				continue
			}

			if !strings.Contains(strings.ToLower(text), "chapter "+number) && !strings.Contains(sentence, "chapter "+number) {
				t.Errorf("the group %q links %q without naming chapter %s beside it", group.group, text, number)
			}
		}
	}

	for _, cost := range []string{"Back up", "rewrites your database", "session", "log in again"} {
		if !strings.Contains(section, cost) {
			t.Errorf("the changes since version 6.6.3 do not name the update's cost: %q is missing", cost)
		}
	}
}

// colourFields are the four administrator fields that offer a colour, each with the sentence
// that says where its level sits in the order a call's colour is read.
type colourField struct {
	caption string
	order   string
}

var colourFields = []colourField{
	{
		caption: "The system row",
		order:   "It wins over the talkgroup's own colour and loses to a group or a tag the talkgroup carries.",
	},
	{
		caption: "The talkgroup row",
		order:   "It is the last level read, so it colours the call only when no group, tag, or system colours it.",
	},
	{
		caption: "The group row",
		order:   "It is the first level read, so it wins over its tag's, its system's, and each talkgroup's own colour.",
	},
	{
		caption: "The tag row",
		order:   "It wins over the system's and the talkgroup's colour and loses to a group's.",
	},
}

// colourFieldRow returns the colour row of one of the guide's field tables as plain text,
// with the whitespace the document wraps its cells with collapsed away.
func colourFieldRow(t *testing.T, body, caption string) string {
	t.Helper()

	for _, row := range regexp.MustCompile(`(?is)<tr\b.*?</tr>`).FindAllString(tableWithCaption(t, body, caption), -1) {
		if strings.Contains(row, "<td>Led Color</td>") {
			return strings.Join(strings.Fields(stripTags(row)), " ")
		}
	}

	t.Fatalf("the table captioned %q carries no Led Color row", caption)

	return ""
}

// where an administrator sets colours, the guide names the order those levels are read in,
// what a level left at its default leaves the listener with, and that the configuration a
// listener is sent already carries the resolved colour instead of an empty field.
func TestTheGuideSaysWhichColourWinsAtEachLevelThatOffersOne(t *testing.T) {
	body := guidePath(t)

	const leftAlone = "Left alone, it colours nothing, and a call no level colours shows green."

	for _, field := range colourFields {
		row := colourFieldRow(t, body, field.caption)

		if !strings.Contains(row, field.order) {
			t.Errorf("the colour row of %q does not state where its level sits in the order: %q is missing", field.caption, field.order)
		}

		if !strings.Contains(row, leftAlone) {
			t.Errorf("the colour row of %q does not name the colour a call ends with: %q is missing", field.caption, leftAlone)
		}
	}

	chapter := strings.Join(strings.Fields(stripTags(chapterOf(t, body, "ch-11-systems-sites-talkgroups-units"))), " ")

	for _, stated := range []string{
		"the group a talkgroup belongs to, then its tag, then its system, then the talkgroup itself",
		"a call that none of the four colours shows in green",
		"carrying a colour rather than an empty one for the listener to resolve",
		"a system's entry carries the colour you chose for it, or green when you chose none",
		"a talkgroup's entry carries the colour that order resolves to for that talkgroup",
	} {
		if !strings.Contains(chapter, stated) {
			t.Errorf("chapter 11 does not say %q", stated)
		}
	}

	if !strings.Contains(words(tableWithCaption(t, body, "Where groups and tags are used")), "The assignment order is group, then tag, then system, then talkgroup.") {
		t.Errorf("the chapter 12 table that names where groups and tags are used no longer states the assignment order")
	}
}

// serverFlag is one flag the composition root registers, with the default expression the source writes.
type serverFlag struct {
	name string
	expr string
}

// serverFlags reads the flag set the server builds at its composition root, the source
// the guide's flag tables are checked against.
func serverFlags(t *testing.T) []serverFlag {
	source := repoFile(t, "cmd", "rdio-scanner", "config.go")

	var flags []serverFlag

	for _, m := range regexp.MustCompile(`fs\.(?:String|Bool|Uint|Int|StringVar|BoolVar|UintVar|IntVar)\(\s*(?:&[A-Za-z0-9_.]+,\s*)?"([a-z_]+)",\s*([^,]+),`).FindAllStringSubmatch(source, -1) {
		flags = append(flags, serverFlag{name: m[1], expr: strings.TrimSpace(m[2])})
	}

	if len(flags) < 20 {
		t.Fatalf("the composition root registers %d flags: the flag set is not readable from it", len(flags))
	}

	return flags
}

// packageConstants collects the constants of the composition root's own packages, so a
// flag default named by a constant resolves to its value.
func packageConstants(t *testing.T) map[string]string {
	consts := map[string]string{}

	for _, source := range []string{
		repoFile(t, "cmd", "rdio-scanner", "config.go"),
		repoFile(t, "internal", "app", "app.go"),
	} {
		for _, block := range regexp.MustCompile(`(?s)const\s*\((.*?)\n\)`).FindAllStringSubmatch(source, -1) {
			for _, line := range strings.Split(block[1], "\n") {
				m := regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*(?:string\s+)?=\s*(.+)$`).FindStringSubmatch(strings.TrimSpace(line))
				if len(m) == 3 {
					consts[m[1]] = strings.TrimSpace(m[2])
				}
			}
		}
	}

	return consts
}

// resolveDefault reads a default expression as the value the server starts with, and
// reports false for defaults computed at start-up.
func resolveDefault(expr string, consts map[string]string, depth int) (string, bool) {
	if depth > 6 {
		return "", false
	}

	expr = strings.TrimSpace(expr)

	for _, wrapper := range []string{"uint(", "int("} {
		if strings.HasPrefix(expr, wrapper) && strings.HasSuffix(expr, ")") {
			return resolveDefault(expr[len(wrapper):len(expr)-1], consts, depth+1)
		}
	}

	if expr == "false" || expr == "true" {
		return expr, true
	}

	if strings.HasPrefix(expr, `"`) {
		value, err := strconv.Unquote(expr)
		if err != nil {
			return "", false
		}

		return value, true
	}

	if expr != "" && strings.Trim(expr, "0123456789") == "" {
		return expr, true
	}

	if i := strings.LastIndex(expr, "."); i >= 0 && !strings.ContainsAny(expr[:i], " \t()") {
		return resolveDefault(expr[i+1:], consts, depth+1)
	}

	if next, ok := consts[expr]; ok {
		return resolveDefault(next, consts, depth+1)
	}

	return "", false
}

// computedDefaults are the flags whose default the composition root computes at
// start-up; the guide's cell must state that outcome.
var computedDefaults = map[string]string{
	"base_dir": "~/Rdio Scanner",
}

// defaultIsStated reports whether the guide's cell states the default the server starts
// with, including none for an unset default and off for a switch left off.
func defaultIsStated(name, cell, expr string, consts map[string]string) bool {
	value, literal := resolveDefault(expr, consts, 0)

	if !literal {
		outcome, ok := computedDefaults[name]

		return ok && strings.Contains(cell, outcome)
	}

	switch value {
	case "":
		return strings.Contains(cell, "none") || strings.Contains(cell, "—") || strings.Contains(cell, "the driver's")
	case "false":
		return strings.Contains(cell, "false") || strings.Contains(cell, "off")
	case "true":
		return strings.Contains(cell, "true")
	}

	return strings.Contains(cell, value)
}

// guideFlagChapters are the chapters a reader looks flags up in, the operations chapter
// and the reference appendix; every table in them carrying a default is read.
var guideFlagChapters = []string{
	"ch-06-flags-and-ini",
	"ch-27-reference",
}

// tabulatedFlags collects the flags a chapter's tables state a default for, keyed by the flag name
// without its dash.
func tabulatedFlags(t *testing.T, body, chapter string) map[string]string {
	t.Helper()

	rows := map[string]string{}

	for _, table := range regexp.MustCompile(`(?is)<table\b.*?</table>`).FindAllString(chapterOf(t, body, chapter), -1) {
		for _, m := range regexp.MustCompile(`(?is)<tr><td><code>(-([a-z_]+))[^<]*</code></td><td class="default">(.*?)</td>`).FindAllStringSubmatch(table, -1) {
			rows[m[2]] = stripTags(m[3])
		}
	}

	if len(rows) == 0 {
		t.Fatalf("the guide's chapter %q tabulates no flag default", chapter)
	}

	return rows
}

// the guide's section table repeats the administration navigation's order and chapter
// for every section, read from the frontend's own declaration.
func TestTheGuideTabulatesTheSectionsInTheOrderTheNavigationListsThem(t *testing.T) {
	body := guidePath(t)

	want := navigationSections(t)

	table := tableWithCaption(t, body, "The sections, and what each one configures")

	var got []string

	chapters := map[string]string{}

	for _, m := range regexp.MustCompile(`(?is)<tr><td>([^<]+)</td><td>[^<]*</td><td><a href="#([a-z0-9-]+)">\d+</a></td></tr>`).FindAllStringSubmatch(table, -1) {
		label := stripTags(m[1])

		got = append(got, label)
		chapters[label] = m[2]
	}

	if len(got) != len(want) {
		t.Fatalf("the guide's section table names %d sections, the administration navigation lists %d: %v", len(got), len(want), got)
	}

	for i, label := range want {
		if got[i] != label {
			t.Errorf("the navigation lists %s as its item %d, the guide's table names %s there: %v", label, i+1, got[i], got)
		}
	}

	for label, wantChapter := range sectionGuideChapters {
		gotChapter, ok := chapters[label]
		if !ok {
			t.Errorf("the guide's section table does not name %s, which the navigation lists", label)

			continue
		}

		if gotChapter != wantChapter {
			t.Errorf("the guide's section table sends %s to %q, not to the chapter that describes it, %q", label, gotChapter, wantChapter)
		}
	}
}

// navigationSections reads the administration navigation's declared sections in
// declaration order, and fails loudly when the navigation stops declaring itself
// this way.
func navigationSections(t *testing.T) []string {
	t.Helper()

	source := repoFile(t, "internal", "presentation", "admin", "frontend", "presentation", "admin", "rdio-admin.js")

	var labels []string

	for _, m := range regexp.MustCompile(`\{\s*name:\s*'([a-z]+)',\s*label:\s*'([^']+)'`).FindAllStringSubmatch(source, -1) {
		labels = append(labels, m[2])
	}

	if len(labels) < 14 {
		t.Fatalf("the administration navigation declares %d sections in its own source: the declaration is not readable from it", len(labels))
	}

	return labels
}

// The chapter that describes each section the navigation lists.
var sectionGuideChapters = map[string]string{
	"Dashboard":   "ch-08-administration-interface",
	"Systems":     "ch-11-systems-sites-talkgroups-units",
	"Sites":       "ch-11-systems-sites-talkgroups-units",
	"Talkgroups":  "ch-11-systems-sites-talkgroups-units",
	"Units":       "ch-11-systems-sites-talkgroups-units",
	"Groups":      "ch-12-groups-and-tags",
	"Tags":        "ch-12-groups-and-tags",
	"Dirwatch":    "ch-10-getting-audio-in",
	"Downstreams": "ch-10-getting-audio-in",
	"API Keys":    "ch-09-access-and-api-keys",
	"Access":      "ch-09-access-and-api-keys",
	"Options":     "ch-13-options",
	"Logs":        "ch-14-logs",
	"Tools":       "ch-15-tools",
	"OpenAPI":     "ch-16-rest-api",
}

// the log chapter tabulates every subsystem the server files entries under, and the reference
// chapter indexes the same list: a subsystem an operator can filter on and cannot find in the guide
// is a subsystem nobody filters on. Both tables are pinned to the vocabulary the server declares,
// the chapter's in the server's own order and the index in the order a reader scans it.
func TestTheGuideTabulatesEverySubsystemTheServerFilesEntriesUnder(t *testing.T) {
	body := guidePath(t)

	want := domain.KnownLogSources()

	chapter := regexp.MustCompile(`(?is)<tr><td><code>([a-z]+)</code></td><td>([^<]+)</td><td>(.+?)</td></tr>`).
		FindAllStringSubmatch(tableWithCaption(t, body, "Every subsystem the server files entries under, and what it reports"), -1)

	index := regexp.MustCompile(`(?is)<tr><td><em>([^<]+)</em></td><td><code>([a-z]+)</code></td><td>(.+?)</td></tr>`).
		FindAllStringSubmatch(tableWithCaption(t, body, "The subsystems, alphabetically, with the name the log view shows and the name the filter uses"), -1)

	if len(chapter) != len(want) {
		t.Fatalf("the log chapter tabulates %d subsystems and the server files entries under %d: %v",
			len(chapter), len(want), guideTableNames(chapter, 1))
	}

	shown := map[string]string{}

	for i, source := range want {
		name := chapter[i][1]

		if name != string(source) {
			t.Errorf("the log chapter's row %d names %q, want %q: the table is read in the order the server "+
				"lists the subsystems, so a mismatch is one missing and another moved to take its place",
				i+1, name, source)

			continue
		}

		label := strings.TrimSpace(chapter[i][2])

		if label == "" {
			t.Errorf("the log chapter shows %s with no label, and a filter choice nobody can read is one nobody uses", source)
		}

		if strings.TrimSpace(stripTags(chapter[i][3])) == "" {
			t.Errorf("the log chapter says nothing about what %s reports, so an operator cannot tell what to filter for", source)
		}

		shown[string(source)] = label
	}

	if len(index) != len(want) {
		t.Fatalf("the reference index lists %d subsystems and the server files entries under %d: %v",
			len(index), len(want), guideTableNames(index, 2))
	}

	order := []string{}

	for _, row := range index {
		name := row[2]

		if _, ok := shown[name]; !ok {
			t.Errorf("the reference index lists %q, which the log chapter does not tabulate under the server's vocabulary", name)

			continue
		}

		label := strings.TrimSpace(row[1])

		if label != shown[name] {
			t.Errorf("the reference index shows %s as %q and the log chapter shows it as %q: the two tables the "+
				"same operator reads name one subsystem two ways", name, label, shown[name])
		}

		if strings.TrimSpace(stripTags(row[3])) == "" {
			t.Errorf("the reference index names %s with no line about what it covers", name)
		}

		order = append(order, label)
	}

	if scanned := append([]string{}, order...); !sort.SliceIsSorted(scanned, func(i, j int) bool {
		return strings.ToLower(scanned[i]) < strings.ToLower(scanned[j])
	}) {
		t.Errorf("the reference index is captioned as the subsystems alphabetically and lists them %v: a reader "+
			"scanning for one name misses it", order)
	}
}

// guideTableNames collects one column of a parsed table, so a short table can name what it holds
// where a whole row would only add noise.
func guideTableNames(rows [][]string, column int) []string {
	out := []string{}

	for _, row := range rows {
		out = append(out, row[column])
	}

	return out
}

// every flag and default the guide tabulates is one the server accepts and starts with,
// checked in the operations chapter and the appendix alike.
func TestTheFlagsTheGuideTabulatesAreTheFlagsTheServerAccepts(t *testing.T) {
	body := guidePath(t)

	flags := serverFlags(t)
	consts := packageConstants(t)

	accepted := map[string]bool{}

	for _, flag := range flags {
		accepted[flag.name] = true
	}

	for _, chapter := range guideFlagChapters {
		rows := tabulatedFlags(t, body, chapter)

		for _, flag := range flags {
			cell, ok := rows[flag.name]
			if !ok {
				t.Errorf("the guide's chapter %q does not tabulate the -%s flag the server accepts", chapter, flag.name)

				continue
			}

			value, literal := resolveDefault(flag.expr, consts, 0)
			if !literal {
				value = "worked out at start-up"
			}

			if !defaultIsStated(flag.name, cell, flag.expr, consts) {
				t.Errorf("the guide's chapter %q states the -%s default as %q, the server starts with %s", chapter, flag.name, cell, value)
			}
		}

		for name := range rows {
			if !accepted[name] {
				t.Errorf("the guide's chapter %q tabulates -%s, which the server does not accept", chapter, name)
			}
		}
	}
}

// proseFlagPattern matches a code element whose whole text is one flag: the guide's prose
// names a flag this way when it describes what the flag does, as distinct from a command
// line where the flag sits among its arguments, and a trailing star is the guide's glob
// for a family of flags rather than one flag's name.
var proseFlagPattern = regexp.MustCompile(`(?is)<code>\s*-([a-z_][a-z0-9_]*)\s*(\*?)\s*</code>`)

// proseFlags collects the flags the guide names in a code element of its own, outside the
// flag tables the tabulated check reads.
func proseFlags(t *testing.T, body string) []string {
	t.Helper()

	var named []string

	for _, m := range proseFlagPattern.FindAllStringSubmatch(body, -1) {
		if m[2] == "*" {
			continue
		}

		if !slices.Contains(named, m[1]) {
			named = append(named, m[1])
		}
	}

	if len(named) < 20 {
		t.Fatalf("the guide names %d flags outside its flag tables: the mentions are not readable from it", len(named))
	}

	sort.Strings(named)

	return named
}

// the guide describes flags in its prose as well as in its tables, and a reader follows the
// prose: every flag it names in a code element has to be one the composition root registers.
func TestTheFlagsTheGuideNamesOutsideItsTablesAreTheFlagsTheServerAccepts(t *testing.T) {
	body := guidePath(t)

	accepted := map[string]bool{}

	for _, flag := range serverFlags(t) {
		accepted[flag.name] = true
	}

	for _, name := range proseFlags(t, body) {
		if !accepted[name] {
			t.Errorf("the guide describes the -%s flag in its prose, which the server does not accept", name)
		}
	}
}

// maintenanceFlagRows are the two one-shot switches, each chapter that tabulates them, and
// the sentences each chapter's row has to carry: the clearing comes before the compaction it
// follows, and the report-only half says plainly that it removes nothing.
var maintenanceFlagRows = map[string]map[string][]string{
	"ch-06-flags-and-ini": {
		"vacuum": {
			"Clears the records a legacy migration left in problem",
			"then compacts the sqlite database file",
			"refuses to run while another server holds the deployment",
		},
		"vacuum_dry_run": {
			"Prints what -vacuum would clear and drop",
			"removes nothing, drops no table, and compacts nothing",
		},
	},
	"ch-27-reference": {
		"vacuum": {
			"Clears the records a legacy migration left in problem",
			"then compacts the sqlite database file",
			"refusing a deployment another server holds",
		},
		"vacuum_dry_run": {
			"Prints what -vacuum would clear and drop",
			"removes nothing, drops no table, and compacts nothing",
		},
	},
}

// guideFlagRow returns one flag's row in a chapter's tables as the plain sentences a reader
// sees, with the document's wrapping and markup collapsed away.
func guideFlagRow(t *testing.T, body, chapter, name string) string {
	t.Helper()

	for _, table := range regexp.MustCompile(`(?is)<table\b.*?</table>`).FindAllString(chapterOf(t, body, chapter), -1) {
		for _, row := range regexp.MustCompile(`(?is)<tr>.*?</tr>`).FindAllString(table, -1) {
			if strings.Contains(row, "<code>-"+name+"</code>") {
				return strings.Join(strings.Fields(stripTags(row)), " ")
			}
		}
	}

	t.Fatalf("the guide's chapter %q tabulates no row for the -%s flag", chapter, name)

	return ""
}

// the one-shot maintenance switches are tabulated in both chapters a reader looks flags up
// in, and each row says what that half of the run answers: a run clears what a migration left
// in problem before it compacts, and the dry run lists the same records without touching them.
func TestTheGuideTabulatesTheMaintenanceSwitchesAndWhatEachAnswers(t *testing.T) {
	body := guidePath(t)

	for _, chapter := range guideFlagChapters {
		wants := maintenanceFlagRows[chapter]

		for _, name := range []string{"vacuum", "vacuum_dry_run"} {
			row := guideFlagRow(t, body, chapter, name)

			for _, stated := range wants[name] {
				if !strings.Contains(row, stated) {
					t.Errorf("the guide's chapter %q does not say what the -%s switch answers: %q is missing from its row:\n%s",
						chapter, name, stated, row)
				}
			}
		}

		// the order the row states is the order the run performs: the records go first, the
		// file is rewritten afterwards
		clears := strings.Index(guideFlagRow(t, body, chapter, "vacuum"), "Clears the records a legacy migration left in problem")
		compacts := strings.Index(guideFlagRow(t, body, chapter, "vacuum"), "then compacts the sqlite database file")

		if clears < 0 || compacts < 0 || clears > compacts {
			t.Errorf("the guide's chapter %q states the -vacuum run's compaction before its clearing: a reader is told "+
				"the file is rewritten before the records are removed:\n%s", chapter, guideFlagRow(t, body, chapter, "vacuum"))
		}
	}
}

// the migration chapter is where an operator decides whether to run the one-shot over an older
// database, so it names what the run removes, what it leaves alone, and the copies a run takes
// away with the rows that held them.
var maintenanceRunWording = []string{
	"removes three things a migration leaves behind",
	"the migrationArchive table with every row in it",
	"any legacy table the migration left standing",
	"clears the records first and compacts the file afterwards",
	"a unit whose label is one you wrote, a unit with a number range, a unit with no radio number of its own, and every recorded call",
	"The archive is the only copy of the configuration a migration could not carry.",
	"The archive table exists only where a migration had something to put in it",
	"the same SELECT answers an error on a deployment that never archived anything",
	"An option value this release has no option of is gone with the row that held it",
	"The row that made a legacy table stay standing has no copy anywhere",
	"on MariaDB, MySQL, or PostgreSQL the run names the compaction it skipped",
}

// the guide's migration chapter carries the same account the run prints at the terminal,
// including the warning that some archived rows are the only copy there is.
func TestTheGuideWarnsWhatAMaintenanceRunRemovesFromAnOlderDatabase(t *testing.T) {
	body := guidePath(t)

	chapter := strings.Join(strings.Fields(stripTags(chapterOf(t, body, "ch-24-upgrading-and-migrating"))), " ")

	for _, stated := range maintenanceRunWording {
		if !strings.Contains(chapter, stated) {
			t.Errorf("the guide's migration chapter does not say what a maintenance run does: %q is missing", stated)
		}
	}
}

// httpMethods are the methods the contract publishes as operations; its websocket
// entries are the listener's channel, not endpoints a client calls.
var httpMethods = map[string]bool{
	"get": true, "post": true, "put": true, "delete": true, "patch": true, "head": true, "options": true,
}

// The word the guide uses for each credential the contract names.
var credentialWords = map[string]string{
	"adminJwt":   "session",
	"accessCode": "access code",
}

// publishedEndpoints reads the contract's endpoints with the credential each takes and
// the header that credential is presented in.
func publishedEndpoints(t *testing.T) (map[string][]string, map[string]string) {
	var contract struct {
		Security   []map[string][]string                 `json:"security"`
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			SecuritySchemes map[string]struct {
				In   string `json:"in"`
				Name string `json:"name"`
			} `json:"securitySchemes"`
		} `json:"components"`
	}

	if err := json.Unmarshal([]byte(repoFile(t, "docs", "openapi.json")), &contract); err != nil {
		t.Fatalf("the published contract does not read: %v", err)
	}

	endpoints := map[string][]string{}

	for path, operations := range contract.Paths {
		for method, raw := range operations {
			if !httpMethods[strings.ToLower(method)] {
				continue
			}

			var operation struct {
				Security *[]map[string][]string `json:"security"`
			}

			if err := json.Unmarshal(raw, &operation); err != nil {
				t.Fatalf("the contract's %s %s does not read: %v", strings.ToUpper(method), path, err)
			}

			requirements := operation.Security
			if requirements == nil {
				requirements = &contract.Security
			}

			names := []string{}

			for _, requirement := range *requirements {
				for name := range requirement {
					names = append(names, name)
				}
			}

			sort.Strings(names)

			endpoints[strings.ToUpper(method)+" "+path] = names
		}
	}

	headers := map[string]string{}

	for scheme, schemeDetails := range contract.Components.SecuritySchemes {
		if schemeDetails.In == "header" {
			headers[scheme] = schemeDetails.Name
		}
	}

	return endpoints, headers
}

// the guide's endpoint table names exactly the endpoints the contract publishes, each
// with the credential it takes, so an administrator's scripts authenticate instead of
// being refused.
func TestTheEndpointsTheGuideTabulatesAreTheEndpointsTheContractPublishes(t *testing.T) {
	body := guidePath(t)

	endpoints, headers := publishedEndpoints(t)

	chapter := chapterOf(t, body, "ch-16-rest-api")

	rows := map[string]string{}

	for _, m := range regexp.MustCompile(`(?is)<tr><td><code>([A-Z]+) ([^<]+)</code></td><td>(.*?)</td>`).FindAllStringSubmatch(tableWithCaption(t, chapter, "The endpoints the server answers"), -1) {
		rows[m[1]+" "+m[2]] = stripTags(m[3])
	}

	for key, schemes := range endpoints {
		cell, ok := rows[key]
		if !ok {
			t.Errorf("the guide's endpoint table does not carry %s, which the contract publishes", key)

			continue
		}

		if len(schemes) == 0 {
			if !strings.Contains(cell, "nothing") && !strings.Contains(cell, "key") {
				t.Errorf("the guide's endpoint table says %s takes %q, the contract publishes it with no credential", key, cell)
			}

			continue
		}

		for _, scheme := range schemes {
			word, ok := credentialWords[scheme]
			if !ok {
				t.Fatalf("the contract names the %s credential, which the guide's vocabulary does not know", scheme)
			}

			if !strings.Contains(strings.ToLower(cell), word) {
				t.Errorf("the guide's endpoint table says %s takes %q, the contract publishes it with the %s credential", key, cell, scheme)
			}
		}
	}

	for key := range rows {
		if _, ok := endpoints[key]; !ok {
			t.Errorf("the guide's endpoint table carries %s, which the contract does not publish", key)
		}
	}

	credential := tableWithCaption(t, chapter, "How each credential is presented")

	for scheme, header := range headers {
		if !strings.Contains(credential, header) {
			t.Errorf("the guide does not say the %s credential is presented in the %s header", scheme, header)
		}
	}
}

// no link, reference, or resource in the guide leaves the file, except the named
// out-of-scope pointer carried beside it.
func TestNothingInTheGuidePointsOutsideItself(t *testing.T) {
	body := guidePath(t)

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

	if links < 100 {
		t.Errorf("the guide carries %d links: the contents aid and the cross-references are not all there", links)
	}

	prose := regexp.MustCompile(`(?is)<[^>]*>`).ReplaceAllString(body, " ")

	for _, name := range regexp.MustCompile(`[A-Za-z0-9_\-./]+\.(?:md|html)\b`).FindAllString(prose, -1) {
		switch name {
		case "user-guide.html", "docs/user-guide.html", "admin-guide.html", "docs/admin-guide.html":
			continue
		}

		t.Errorf("the guide names %q, a document the reader does not have beside the guide", name)
	}
}

// the guide stays inside its size budget for one carried file, and every run records
// the measured size.
func TestTheGuideIsSmallEnoughToCarry(t *testing.T) {
	const budget = 6 << 20

	info, err := os.Stat(guideFile(t))
	if err != nil {
		t.Fatalf("the guide is not readable: %v", err)
	}

	t.Logf("the guide measures %d bytes of %d allowed", info.Size(), budget)

	if info.Size() > budget {
		t.Errorf("the guide measures %d bytes, over the %d byte budget", info.Size(), budget)
	}
}

// every row of the release matrix says its archive carries its own ffmpeg, compiled by
// this project's build in a container, including the targets built for an operating
// system the build host does not run.
func TestTheReleaseMatrixSaysEveryArchiveCarriesItsOwnFfmpeg(t *testing.T) {
	const caption = "The release matrix, as the build tool defines it. Every release carries its own ffmpeg inside the program, compiled by this project's build in a container on the machine that produces the releases."

	body := guidePath(t)

	rows := regexp.MustCompile(`(?is)<tr><td>([^<]+)</td><td>([^<]+)</td><td><code>rdio-scanner-([a-z0-9-]+)-v[^<]*</code></td><td>([^<]+)</td></tr>`).
		FindAllStringSubmatch(tableWithCaption(t, chapterOf(t, body, "ch-03-requirements"), caption), -1)

	targets := releasedTargets(t)

	if len(rows) != len(targets) {
		t.Fatalf("the release matrix table carries %d rows, want the %d platforms this project releases", len(rows), len(targets))
	}

	for _, m := range rows {
		word := m[3]

		if _, recorded := targets[word]; !recorded {
			t.Errorf("the release matrix table carries %s, which the manifest has no record for", word)
		}

		if strings.TrimSpace(m[4]) != "Yes" {
			t.Errorf("the release matrix table says the %s archive carries %q where its own ffmpeg is asked about, want Yes", word, strings.TrimSpace(m[4]))
		}
	}
}

// releasedTargets is the target set the manifest records, which is the list the guide's table transcribes.
func releasedTargets(t *testing.T) map[string]json.RawMessage {
	t.Helper()

	var manifest struct {
		Targets map[string]json.RawMessage `json:"targets"`
	}

	body := repoFile(t, "internal", "infrastructure", "ffmpeg", "bundle", "manifest.json")

	if err := json.Unmarshal([]byte(body), &manifest); err != nil {
		t.Fatalf("the manifest is not readable as json: %v", err)
	}

	return manifest.Targets
}

// words strips tags and line wrapping, leaving the phrases a reader reads.
func words(body string) string {
	return strings.Join(strings.Fields(stripTags(body)), " ")
}

// what a reader finds beside the program after unpacking one release archive is named by each
// file's own name, because no entry of the archive carries a directory.
func TestTheGuideNamesTheFilesAReleaseArchiveUnpacksBesideTheProgram(t *testing.T) {
	body := guidePath(t)

	section := chapterOf(t, body, "ch-04-installation")

	named := []string{}

	for _, m := range regexp.MustCompile(`(?is)<code>([^<]*)</code>`).FindAllStringSubmatch(section, -1) {
		name := strings.TrimSpace(m[1])

		// The names that stand for a file the reader finds after unpacking, and not for a command,
		// an image, or a container path.
		if name == "rdio-scanner" || strings.HasSuffix(name, ".html") || strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".txt") {
			named = append(named, name)
		}
	}

	for _, want := range []string{"rdio-scanner", "admin-guide.html", "user-guide.html", "FFMPEG.txt"} {
		if !slices.Contains(named, want) {
			t.Errorf("the installation section never names %q as a whole file beside the program: it names %v", want, named)
		}
	}

	for _, name := range named {
		if strings.Contains(name, "/") {
			t.Errorf("the installation section names %q with a directory in it, and no archive entry carries one", name)
		}
	}
}

// the guide states the ffmpeg is compiled for audio only wherever operators read the
// ritual, and keeps the hand route as drop two files and run one command.
func TestTheGuideSaysTheFfmpegIsCompiledForAudioOnly(t *testing.T) {
	body := guidePath(t)

	places := []struct {
		chapter string
		need    []string
	}{
		{
			chapter: "ch-03-requirements",
			need: []string{
				"compiled for audio",
				"no video codec, decoder, or encoder, and no network protocol",
			},
		},
		{
			chapter: "ch-04-installation",
			need: []string{
				"compiled for audio",
				"refuses binaries that register a video codec",
			},
		},
		{
			chapter: "ch-21-running-as-a-service",
			need: []string{
				"compiled for audio",
			},
		},
		{
			chapter: "ch-25-troubleshooting",
			need: []string{
				"compiled for audio",
			},
		},
		{
			chapter: "ch-26-faq",
			need: []string{
				"compiled for audio",
			},
		},
		{
			chapter: "ch-27-reference",
			need: []string{
				"no video codec, decoder, or encoder, and no network protocol",
				"the codecs the run recorded",
			},
		},
	}

	for _, p := range places {
		text := words(chapterOf(t, body, p.chapter))

		for _, want := range p.need {
			if !strings.Contains(text, want) {
				t.Errorf("the guide's chapter %q never says %q", p.chapter, want)
			}
		}
	}
}

// the guide states the question the administration page asks when it finds a new version, in the
// chapter an operator is already reading when the question appears, and states in the
// troubleshooting chapter why a page opened over an address the browser will not give a worker
// never asks.
func TestTheGuideSaysTheAdministrationPageAsksBeforeItUsesANewVersion(t *testing.T) {
	body := guidePath(t)

	places := []struct {
		chapter string
		need    []string
	}{
		{
			chapter: "ch-08-administration-interface",
			need: []string{
				"New version available",
				"A new version of the administration interface is available. Would you like to reload the page?",
				"No keeps what you have open for now, and the page asks again next time.",
				"Yes reloads the page, and the new version starts.",
			},
		},
		{
			chapter: "ch-25-troubleshooting",
			need: []string{
				"The administration page did not ask about a new version",
				"The question comes from the page's own worker",
				"registers no worker, so it never finds a new version and never asks",
			},
		},
	}

	for _, p := range places {
		text := words(chapterOf(t, body, p.chapter))

		for _, want := range p.need {
			if !strings.Contains(text, want) {
				t.Errorf("the guide's chapter %q never says %q", p.chapter, want)
			}
		}
	}
}

// the guide names which machine compiles each archive's ffmpeg, the container route for
// every target, and what the build reads instead of running.
func TestTheGuideSaysWhereTheFfmpegIsCompiledAndWhatTheBuildReadsInsteadOfRunning(t *testing.T) {
	body := guidePath(t)

	places := []struct {
		chapter string
		need    []string
	}{
		{
			chapter: "ch-03-requirements",
			need: []string{
				"Every one of the eight archives carries the ffmpeg binaries this project's build compiled, in a container",
				"the build host runs no FreeBSD program and no macOS program",
			},
		},
		{
			chapter: "ch-04-installation",
			need: []string{
				"go run ./cmd/build ffmpeg",
				"compiles them, runs them, and adopts what converts",
				"drop ffmpeg and ffprobe",
			},
		},
		{
			chapter: "ch-21-running-as-a-service",
			need: []string{
				"the release build compiles the ffmpeg of every release target in a container",
			},
		},
		{
			chapter: "ch-27-reference",
			need: []string{
				"names the container image and digest that compiled them",
			},
		},
	}

	for _, p := range places {
		text := words(chapterOf(t, body, p.chapter))

		for _, want := range p.need {
			if !strings.Contains(text, want) {
				t.Errorf("the guide's chapter %q never says %q", p.chapter, want)
			}
		}
	}

	// the one build command is the whole ritual, on its own command line.
	var lines []string

	for _, block := range regexp.MustCompile(`(?is)<pre><code>(.*?)</code></pre>`).FindAllStringSubmatch(chapterOf(t, body, "ch-04-installation"), -1) {
		for _, line := range strings.Split(block[1], "\n") {
			if i := strings.Index(line, "#"); i >= 0 {
				line = line[:i]
			}

			lines = append(lines, strings.TrimSpace(line))
		}
	}

	if !slices.Contains(lines, "go run ./cmd/build ffmpeg") {
		t.Errorf("the guide's commands are %v, and none of them is the one command that produces the ffmpeg of the platform you are on", lines)
	}

	// The compile's own steps belong to the build.
	for _, step := range []string{"./configure", "make -j", "codesign -s -", "tar -xJf", "strip ffmpeg"} {
		if strings.Contains(words(chapterOf(t, body, "ch-04-installation")), step) {
			t.Errorf("the guide's chapter 4 tells the operator to run %s, which is the build's step to run", step)
		}
	}
}

// the guide teaches which Mac compiles both macOS architectures when a Mac uses its own
// toolchain, and that the release build compiles both in a container.
func TestTheGuideSaysOneMacCompilesBothMacOSArchitectures(t *testing.T) {
	body := guidePath(t)

	places := []struct {
		chapter string
		need    []string
	}{
		{
			chapter: "ch-03-requirements",
			need: []string{
				"the two macOS architectures compile each other's ffmpeg binaries",
				"one Apple Silicon Mac compiles both macOS archives",
				"Rosetta 2",
			},
		},
		{
			chapter: "ch-04-installation",
			need: []string{
				"binaries it compiles for the other architecture are run on the Mac that runs them",
				"host that cannot run the binaries it compiled leaves them in the target's bundle directory",
			},
		},
		{
			chapter: "ch-21-running-as-a-service",
			need: []string{
				"arm64 binaries compiled on an Intel Mac are run on an Apple Silicon Mac",
				"Rosetta 2",
			},
		},
		{
			chapter: "ch-26-faq",
			need: []string{
				"darwin-amd64",
				"darwin-arm64",
				"one Apple Silicon Mac compiles both macOS archives",
			},
		},
		{
			chapter: "ch-27-reference",
			need: []string{
				"the host that ran them",
			},
		},
		{
			chapter: "ch-25-troubleshooting",
			need: []string{
				"the compile leaves the binaries in the target's bundle directory and names the Mac that runs them",
			},
		},
	}

	for _, p := range places {
		text := words(chapterOf(t, body, p.chapter))

		for _, want := range p.need {
			if !strings.Contains(text, want) {
				t.Errorf("the guide's chapter %q never says %q", p.chapter, want)
			}
		}
	}

	// the cross-architecture compile is an operator command, on its own line.
	var lines []string

	for _, block := range regexp.MustCompile(`(?is)<pre><code>(.*?)</code></pre>`).FindAllStringSubmatch(chapterOf(t, body, "ch-04-installation"), -1) {
		for _, line := range strings.Split(block[1], "\n") {
			if i := strings.Index(line, "#"); i >= 0 {
				line = line[:i]
			}

			lines = append(lines, strings.TrimSpace(line))
		}
	}

	if !slices.Contains(lines, "go run ./cmd/build ffmpeg macos-amd64") {
		t.Errorf("the guide's commands are %v, and none of them is the command an Apple Silicon Mac runs to compile the Intel binaries", lines)
	}

	// the guide teaches the go names of macOS machines as accepted release words.
	for _, word := range []string{"darwin-amd64", "darwin-arm64"} {
		if !strings.Contains(words(chapterOf(t, body, "ch-26-faq")), word) {
			t.Errorf("the guide never says the build accepts %s", word)
		}
	}
}

// the guide says which architectures the container image is built for, in the same product
// names the release matrix uses, names the platform the container program is asked for each
// one by, states for an image the host rule it already states for an archive, and names the
// architecture in the tag an operator types when starting an image built on their own machine.
func TestTheGuideSaysWhichArchitecturesTheContainerImageIsBuiltFor(t *testing.T) {
	body := guidePath(t)

	section := strings.ToLower(words(subsectionOf(t, body, "Run the container image")))

	// `linux-arm` is read where no digit follows it, because `linux-arm64` carries the shorter
	// word inside itself.
	for _, tc := range []struct {
		target string
		named  *regexp.Regexp
	}{
		{target: "linux-amd64", named: regexp.MustCompile(`linux-amd64`)},
		{target: "linux-arm64", named: regexp.MustCompile(`linux-arm64`)},
		{target: "linux-arm", named: regexp.MustCompile(`linux-arm[^0-9]`)},
	} {
		if !tc.named.MatchString(section) {
			t.Errorf("the guide's container image section does not name %s, one of the architectures the image is built for", tc.target)
		}
	}

	for _, want := range []string{
		"linux/amd64",
		"linux/arm64",
		"linux/arm/v7",
		"machine that imports audio",
		"Raspberry Pi 3 or 4",
		"64-bit Raspberry Pi OS",
		"hard-float",
		"latest-linux-arm64",
		"no manifest list",
	} {
		if !strings.Contains(section, strings.ToLower(want)) {
			t.Errorf("the guide's container image section never says %q, which is what an operator choosing an image for their own machine reads off it", want)
		}
	}

	// building an image for another architecture is an operator command, on its own line,
	// beside the other build lines the chapter types.
	var lines []string

	for _, block := range regexp.MustCompile(`(?is)<pre><code>(.*?)</code></pre>`).FindAllStringSubmatch(chapterOf(t, body, "ch-04-installation"), -1) {
		for _, line := range strings.Split(block[1], "\n") {
			if i := strings.Index(line, "#"); i >= 0 {
				line = line[:i]
			}

			lines = append(lines, strings.TrimSpace(line))
		}
	}

	if !slices.Contains(lines, "go run ./cmd/build container linux-arm64") {
		t.Errorf("the guide's commands are %v, and none of them builds the container image for a 64-bit ARM machine", lines)
	}
}

// the guide says the compile is the check, one command, and no longer names a check
// command, digests to copy, or a manifest to edit.
func TestTheGuideSaysTheCompileIsTheCheck(t *testing.T) {
	body := strings.ToLower(words(guidePath(t)))

	for _, gone := range []string{
		"ffmpeg-check",
		"digests it prints",
		"pin the two digests",
		"pinned with the digests",
		"the digests the release pins",
		"digests the manifest pins",
		"proof file",
		".build/ffmpeg/checks",
		"the host that recorded the proof",
		"the proof its ffmpeg check left",
		"gmake",
		"freebsd is compiled on freebsd",
		"compiled only on freebsd",
		"ships the five archives",
		"the five linux and windows",
		"the macos and freebsd targets",
		"on the platform each target runs on",
	} {
		if strings.Contains(body, gone) {
			t.Errorf("the guide says %q, which is the ritual of a build that audited the binaries it adopted", gone)
		}
	}

	for _, want := range []string{
		"the compile is the check",
		"runs them",
		"adopts what converts",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the guide never says %q, which is the whole of what the build does with the binaries it compiles", want)
		}
	}
}

// `COMPILING.md` states the same simplified compile the guide states: one container
// command, one Mac command, no check command, no digests.
func TestCompilingNotesStateTheSimplifiedCompile(t *testing.T) {
	body := strings.ToLower(words(repoFile(t, "COMPILING.md")))

	for _, want := range []string{
		"go run ./cmd/build ffmpeg",
		"the compile is the check",
		"runs them",
		"adopts what converts",
		"the two macos architectures compile each other's ffmpeg binaries",
		"rosetta 2",
		"drop `ffmpeg` and `ffprobe`",
		"the ffmpeg of every release target is compiled in a container",
		"compiled and not run",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("COMPILING.md never says %q", want)
		}
	}

	// `COMPILING.md` names both cached-pair lines: container says compiled,
	// the Mac's own compile says built.
	for _, printed := range []string{"reusing the ffmpeg compiled for", "reusing the ffmpeg built for"} {
		if !strings.Contains(body, printed) {
			t.Errorf("COMPILING.md never names %q, which is a line the build prints when its cache already holds the pair", printed)
		}
	}

	for _, gone := range []string{
		"ffmpeg-check",
		"digests it prints",
		"proof file",
		".build/ffmpeg/checks",
		"no code signature",
		"program interpreter",
		"gmake",
		"freebsd box",
		"the other three",
		"compiled on the platform each one runs on",
		"the three targets compiled on their own platform",
	} {
		if strings.Contains(body, gone) {
			t.Errorf("COMPILING.md says %q, which describes the audit this build no longer performs", gone)
		}
	}
}

// the guide scopes the hand-built ffmpeg drop to the two macOS targets on a macOS host,
// and says a drop elsewhere is embedded in the binary and used by nothing.
func TestTheGuideScopesTheHandBuiltDropToTheTargetsThatAdoptIt(t *testing.T) {
	body := guidePath(t)

	chapter := strings.ToLower(words(chapterOf(t, body, "ch-04-installation")))

	for _, want := range []string{
		"drop ffmpeg and ffprobe in the target's bundle directory",
		"the two macOS targets",
		"embedded in the binary and used by nothing",
	} {
		if !strings.Contains(chapter, strings.ToLower(want)) {
			t.Errorf("the guide's installation chapter never says %q", want)
		}
	}

	notes := strings.ToLower(words(repoFile(t, "COMPILING.md")))

	for _, want := range []string{
		"drop `ffmpeg` and `ffprobe`",
		"the two macOS targets on a host that runs macOS",
		"embedded in the binary and used by nothing",
	} {
		if !strings.Contains(notes, strings.ToLower(want)) {
			t.Errorf("COMPILING.md never says %q", want)
		}
	}
}

// guideFooter returns the closing statement, where the guide says in one sentence what
// the project is under.
func guideFooter(t *testing.T, body string) string {
	t.Helper()

	footer := regexp.MustCompile(`(?is)<footer\b[^>]*>(.*?)</footer>`).FindStringSubmatch(body)
	if len(footer) < 2 {
		t.Fatalf("the guide has no closing statement to carry its licence position")
	}

	return footer[1]
}

// the guide states one licence position in every place an operator reads it: every
// interface the server answers is under GPL version 3.
func TestTheGuideStatesOneLicencePositionForEveryInterface(t *testing.T) {
	body := guidePath(t)

	places := []struct {
		chapter string
		need    []string
	}{
		{
			chapter: "ch-01-overview",
			need: []string{
				"the licence, and what it means for both interfaces the server answers, is in chapter 27",
			},
		},
		{
			chapter: "ch-02-how-audio-flows",
			need: []string{
				"both are free to use for any purpose, under the same terms as the rest of the server",
			},
		},
		{
			chapter: "ch-16-rest-api",
			need: []string{
				"the server asks a restricted server's listeners for an access code",
				"the connection carries no licence restriction of its own",
				"as every other interface the server answers",
				"what gates it is the deployment's own access code",
			},
		},
		{
			chapter: "ch-27-reference",
			need: []string{
				"gnu general public license, version 3",
				"both of the interfaces it answers",
				"is under the same terms",
				"what limits a listener is the deployment's own choice, not the licence",
				"nothing here needs an agreement with anyone",
			},
		},
	}

	for _, p := range places {
		text := strings.ToLower(words(chapterOf(t, body, p.chapter)))

		for _, want := range p.need {
			if !strings.Contains(text, strings.ToLower(want)) {
				t.Errorf("the guide's chapter %q never says %q", p.chapter, want)
			}
		}
	}

	footer := strings.ToLower(words(guideFooter(t, body)))

	for _, want := range []string{
		"free software under the gnu general public license, version 3",
		"every interface the server answers",
		"available for all uses under those terms",
	} {
		if !strings.Contains(footer, strings.ToLower(want)) {
			t.Errorf("the guide's closing statement never says %q", want)
		}
	}

	// these refusals catch wordings of the retired second licence position.
	all := strings.ToLower(words(body))

	for _, withdrawn := range []string{
		"proprietary",
		"reserved for",
		"licence agreement",
		"saubeo solutions and its native applications",
	} {
		if strings.Contains(all, withdrawn) {
			t.Errorf("the guide still states a withdrawn position (%q)", withdrawn)
		}
	}
}

// guideCredentialCell returns the description one field's row carries in one of chapter 9's credential
// tables, with the markup intact: the code spans and the emphasised control names a row carries are part of
// what the row says.
func guideCredentialCell(t *testing.T, body, caption, field string) string {
	t.Helper()

	for _, row := range regexp.MustCompile(`(?is)<tr>.*?</tr>`).FindAllString(tableWithCaption(t, body, caption), -1) {
		cells := regexp.MustCompile(`(?is)<td\b[^>]*>(.*?)</td>`).FindAllStringSubmatch(row, -1)

		if len(cells) < 2 {
			continue
		}

		if strings.Join(strings.Fields(stripTags(cells[0][1])), " ") == field {
			return strings.TrimSpace(cells[1][1])
		}
	}

	t.Fatalf("the guide's table %q carries no row for the %q field", caption, field)

	return ""
}

// jsStringConstant and jsNumberConstant read one constant the administration webapp's generator module
// declares. The guide states what that generator produces, so a constant moved there is a sentence the guide
// has to follow.
func jsStringConstant(t *testing.T, parts []string, source, name string) string {
	t.Helper()

	m := regexp.MustCompile(`(?m)^const ` + regexp.QuoteMeta(name) + `\s*=\s*'([^']*)';`).FindStringSubmatch(source)

	if m == nil {
		t.Fatalf("%s no longer declares %s as a single-quoted string constant this guard can read", strings.Join(parts, "/"), name)
	}

	return m[1]
}

func jsNumberConstant(t *testing.T, parts []string, source, name string) int {
	t.Helper()

	m := regexp.MustCompile(`(?m)^const ` + regexp.QuoteMeta(name) + `\s*=\s*([0-9]+);`).FindStringSubmatch(source)

	if m == nil {
		t.Fatalf("%s no longer declares %s as a whole-number constant this guard can read", strings.Join(parts, "/"), name)
	}

	value, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("%s declares %s as %q, which is not a number this guard can read", strings.Join(parts, "/"), name, m[1])
	}

	return value
}

// generateControlName reads the accessible name a section's row gives its generate control. The guide has to
// call the button what the button calls itself.
func generateControlName(t *testing.T, parts []string, source string) string {
	t.Helper()

	names := []string{}

	for _, m := range regexp.MustCompile(`setAttribute\('aria-label',\s*'([^']*)'`).FindAllStringSubmatch(source, -1) {
		if strings.Contains(strings.ToLower(m[1]), "generate") {
			names = append(names, m[1])
		}
	}

	if len(names) != 1 {
		t.Fatalf("%s gives %d of its controls an accessible name naming generate, and the guide documents one generate control per row",
			strings.Join(parts, "/"), len(names))
	}

	return names[0]
}

// guideRowCarriesItsGenerateControl reads the claim both credential rows have to make: the row names the
// button by the accessible name the webapp gives it, and the sentence naming it says that saving the section
// is what makes the value the button wrote the credential the server accepts.
func guideRowCarriesItsGenerateControl(t *testing.T, caption, field, cell, control string) {
	t.Helper()

	if !strings.Contains(cell, "<em>"+control+"</em>") {
		t.Errorf("the guide's %q row in the %q table does not name the %q control inside <em>, which is how the guide names a control a reader presses:\n%s",
			field, caption, control, cell)
	}

	plain := strings.Join(strings.Fields(stripTags(cell)), " ")

	named := []string{}

	for _, sentence := range regexp.MustCompile(`\.\s+`).Split(plain, -1) {
		sentence = strings.TrimSpace(sentence)

		if sentence == "" {
			continue
		}

		if strings.Contains(strings.ToLower(sentence), strings.ToLower(control)) {
			named = append(named, sentence)
		}
	}

	if len(named) == 0 {
		t.Errorf("the guide's %q row in the %q table never names the %q control in a sentence of its own:\n%s", field, caption, control, cell)

		return
	}

	applies := false

	for _, sentence := range named {
		lower := strings.ToLower(sentence)

		if strings.Contains(lower, "sav") && strings.Contains(lower, "server") {
			applies = true
		}
	}

	if !applies {
		t.Errorf("the guide's %q row names %q without saying, in the same sentence, that saving the section is what makes "+
			"the new value the credential the server accepts: a reader who reads only that the button writes a value "+
			"would take the value as applied the moment it appears:\n%s", field, control, strings.Join(named, "\n"))
	}
}

// isMarkRune reports whether a character is one of the marks an access code may carry: neither a letter, a
// digit, nor space.
func isMarkRune(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsSpace(r)
}

// marksOnly reports whether a code span names marks and nothing else, which is how the guide's credential
// row lists the characters that are neither letters nor digits.
func marksOnly(text string) bool {
	if text == "" {
		return false
	}

	for _, r := range text {
		if !isMarkRune(r) {
			return false
		}
	}

	return true
}

// chapter 9 documents both credentials an operator can have the administration webapp write for them: the
// access code handed out over the phone, and the API key pasted into a recorder's configuration. Each row has
// to name the button that writes a fresh value by the name the webapp gives that button, state the shape the
// value takes, and say in the same sentence that the value applies only once the section is saved. The control
// names, the code length and alphabet, and the key's byte count are read here from the source that produces
// them, so the guide cannot promise a credential the webapp would not write or the server would not accept.
//
// The controls are documented inside the row of the field each one writes, and the chapter's two figures stay
// the screenshots they are: neither shows a generate button, so neither may claim one.
func TestTheGuideNamesTheTwoGenerateControlsAndWhatTheyWrite(t *testing.T) {
	body := guidePath(t)

	generatorParts := []string{"internal", "presentation", "admin", "frontend", "domain", "random-generator.js"}
	apikeysParts := []string{"internal", "presentation", "admin", "frontend", "presentation", "admin", "rdio-admin-apikeys.js"}
	accessParts := []string{"internal", "presentation", "admin", "frontend", "presentation", "admin", "rdio-admin-access.js"}

	generator := repoFile(t, generatorParts...)
	alphabet := jsStringConstant(t, generatorParts, generator, "ACCESS_CODE_ALPHABET")
	length := jsNumberConstant(t, generatorParts, generator, "ACCESS_CODE_LENGTH")
	keyBytes := jsNumberConstant(t, generatorParts, generator, "API_KEY_BYTES")

	keyControl := generateControlName(t, apikeysParts, repoFile(t, apikeysParts...))
	codeControl := generateControlName(t, accessParts, repoFile(t, accessParts...))

	// The marks an access code is drawn over, in the order the generator lists them. "Special characters"
	// alone would leave a reader guessing, and a mark the guide names that the generator does not draw is a
	// mark the server refuses.
	specials := []string{}

	for _, r := range alphabet {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}

		specials = append(specials, string(r))
	}

	if len(specials) == 0 {
		t.Fatalf("%s draws an access code over letters and digits alone, which is not the alphabet the guide's Code row names", strings.Join(generatorParts, "/"))
	}

	codeCell := guideCredentialCell(t, body, "The access code row", "Code")
	keyCell := guideCredentialCell(t, body, "The API key row", "Key")

	guideRowCarriesItsGenerateControl(t, "The access code row", "Code", codeCell, codeControl)
	guideRowCarriesItsGenerateControl(t, "The API key row", "Key", keyCell, keyControl)

	// The shape of a generated access code: as long as the generator says, over letters and digits named as
	// such, and over exactly the marks it draws.
	plainCode := strings.Join(strings.Fields(stripTags(codeCell)), " ")

	if !strings.Contains(plainCode, strconv.Itoa(length)) {
		t.Errorf("the guide's Code row never states how long a generated access code is: the webapp writes %d characters, "+
			"and the length is what someone handing the code out has to be able to count:\n%s", length, codeCell)
	}

	for _, part := range []string{"letters", "digits"} {
		if !strings.Contains(strings.ToLower(plainCode), part) {
			t.Errorf("the guide's Code row never says a generated access code is drawn over %s: naming only the marks leaves "+
				"the rest of the %d-character alphabet a guess", part, len([]rune(alphabet)))
		}
	}

	offered := []string{}

	for _, span := range regexp.MustCompile(`(?is)<code\b[^>]*>(.*?)</code>`).FindAllString(codeCell, -1) {
		text := strings.TrimSpace(html.UnescapeString(stripTags(span)))

		if !marksOnly(text) {
			continue
		}

		for _, r := range text {
			offered = append(offered, string(r))
		}
	}

	if strings.Join(offered, "") != strings.Join(specials, "") {
		t.Errorf("the guide's Code row names %q as the marks a generated access code is drawn over, and the webapp's generator "+
			"draws %q: the guide cannot promise a mark the server refuses, and it cannot leave a mark the operator may "+
			"write unnamed. The alphabet is %q.", strings.Join(offered, ""), strings.Join(specials, ""), alphabet)
	}

	if strings.Contains(plainCode, "@") {
		t.Errorf("the guide's Code row carries an @, and the alphabet the server accepts has none: an operator who reads "+
			"that a code may carry one writes a code the server refuses")
	}

	// The form a generated API key takes: the sixteen bytes a UUID is, with the version the generator fixes.
	// Naming the form is how a reader tells the key the button wrote from a secret they invented.
	if keyBytes != 16 {
		t.Fatalf("%s draws an API key from %d bytes, and only 16 are the sixteen bytes a UUID is, which is the form the guide's Key row names",
			strings.Join(generatorParts, "/"), keyBytes)
	}

	plainKey := strings.Join(strings.Fields(stripTags(keyCell)), " ")

	if !regexp.MustCompile(`(?i)version[ -]?4`).MatchString(plainKey) || !strings.Contains(strings.ToUpper(plainKey), "UUID") {
		t.Errorf("the guide's Key row never names the form a generated API key takes: the webapp writes a version-4 UUID, and "+
			"the form is what a reader recognises when they check a recorder's configuration:\n%s", keyCell)
	}

	// A generate control is a button beside a field, not a field an operator fills in: the guide says what it
	// does inside the row of the credential field it writes.
	for _, caption := range []string{"The access code row", "The API key row"} {
		for _, cell := range regexp.MustCompile(`(?is)<tr><td>(.*?)</td>`).FindAllStringSubmatch(tableWithCaption(t, body, caption), -1) {
			name := strings.ToLower(strings.Join(strings.Fields(stripTags(cell[1])), " "))

			if strings.Contains(name, "generate") {
				t.Errorf("the guide's %q table tabulates the generate control as a field named %q: it writes the credential field beside it, and its row is the credential's",
					caption, name)
			}
		}
	}

	// The chapter carries two screenshots, and each says what the picture it carries shows. A generate button
	// has no picture in the page, so no figure may claim one and the copy button's caption stays its own.
	figures := regexp.MustCompile(`(?is)<figure\b.*?</figure>`).FindAllString(chapterOf(t, body, "ch-09-access-and-api-keys"), -1)

	if len(figures) != 2 {
		t.Fatalf("chapter 9 carries %d figures where it carried 2: a control this pair of rows gained has no screenshot of its own to carry", len(figures))
	}

	captions := []string{}

	for _, figure := range figures {
		m := regexp.MustCompile(`(?is)<figcaption\b[^>]*>(.*?)</figcaption>`).FindStringSubmatch(figure)

		if m == nil {
			t.Fatalf("chapter 9 carries a figure with no caption, and a screenshot nobody can tell what it shows is one nobody reads")
		}

		captions = append(captions, strings.Join(strings.Fields(stripTags(m[1])), " "))
	}

	for _, want := range []string{
		"Access codes as the section shows them. The code is masked until you press the eye; the summary line under Choose systems says whether the scope is all systems or the ones you ticked.",
		"API keys as the section shows them. The copy button is how a key gets into a recorder's configuration without being retyped off a screen.",
	} {
		if !slices.Contains(captions, want) {
			t.Errorf("chapter 9 no longer captions one of its figures as %q: the caption is what the baked-in screenshot actually shows, and it may not be rewritten to describe a control the picture does not carry", want)
		}
	}
}

// the dirwatch table's Site and Frequency rows say what those two fields do to the calls a
// directory files: a system and a talkgroup select override whatever a file name claims, a site
// select stands only against a name that claims no site of its own, and the frequency the row
// names is the carrier a mask's own read replaces.
func TestTheGuideSaysWhatADirwatchsSiteSelectAndFrequencyDo(t *testing.T) {
	body := guidePath(t)

	table := tableWithCaption(t, body, "The dirwatch row")

	rows := []struct {
		field string
		need  []string
		ban   []string
	}{
		{
			field: "System, Site, Talkgroup",
			need:  []string{"override", "claims no site"},
			ban:   []string{"whatever the file says"},
		},
		{
			field: "Frequency",
			need:  []string{"replaces", "hertz"},
			ban:   []string{"fake"},
		},
	}

	for _, row := range rows {
		cellAt := regexp.MustCompile(`(?is)<tr><td>` + regexp.QuoteMeta(row.field) + `</td><td>(.*?)</td>`)

		m := cellAt.FindStringSubmatch(table)
		if m == nil {
			t.Fatalf("the dirwatch table carries no %q row to read", row.field)
		}

		cell := strings.Join(strings.Fields(stripTags(m[1])), " ")
		lowered := strings.ToLower(cell)

		for _, want := range row.need {
			if !strings.Contains(lowered, strings.ToLower(want)) {
				t.Errorf("the dirwatch table's %q row says nothing about %q: it says %q", row.field, want, cell)
			}
		}

		for _, unwanted := range row.ban {
			if strings.Contains(lowered, strings.ToLower(unwanted)) {
				t.Errorf("the dirwatch table's %q row still says %q, which is not what the field does: %q", row.field, unwanted, cell)
			}
		}
	}

	// The caption under the chapter's screenshot states the same rule in one sentence,
	// so it has to carry the same shape as the two rows it sits beside.
	caption := ""

	for _, found := range regexp.MustCompile(`(?is)<figcaption\b[^>]*>(.*?)</figcaption>`).FindAllStringSubmatch(chapterOf(t, body, "ch-10-getting-audio-in"), -1) {
		plain := strings.Join(strings.Fields(stripTags(found[1])), " ")
		if strings.HasPrefix(plain, "A watched directory as the section shows it") {
			caption = plain
			break
		}
	}

	if caption == "" {
		t.Fatalf("chapter 10 carries no figure caption describing a watched directory, which is where the row table's override rule is stated beside the screenshot")
	}

	loweredCaption := strings.ToLower(caption)

	for _, unwanted := range []string{"system, site, and talkgroup"} {
		if strings.Contains(loweredCaption, unwanted) {
			t.Errorf("the chapter's dirwatch caption still names %q as a set that overrides what a file name claims, which is no longer true of a site: %q", unwanted, caption)
		}
	}

	for _, want := range []string{"claims no site"} {
		if !strings.Contains(loweredCaption, want) {
			t.Errorf("the chapter's dirwatch caption says nothing about %q: %q", want, caption)
		}
	}
}
