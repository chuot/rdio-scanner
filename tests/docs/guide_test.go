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

package docs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The administrator guide is one file a reader opens where nothing else is available: an extracted
// release archive, a mail attachment, a USB stick, a machine with no network. These tests pin the two
// properties that make that true — the document carries everything it renders inside itself, and it
// carries every chapter the reader is promised — so a guide that quietly reaches out to the network, or
// that loses a chapter on the way, fails the suite instead of failing the reader.

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

// repoFile reads a file of the repository the guide documents. The guide's tables are transcriptions of
// the server's own flag set and of the contract the server publishes, so the tests read what those
// transcriptions are made from and compare it with the tables the reader sees.
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

// The chapters the guide is written to, in the order a reader works through them: install, run,
// configure, ingest, expose, operate, recover, reference. The contents aid names every one of them.
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

// The attributes that pull a resource into the page to render it. A navigation link is not one of them:
// a guide that points a reader at the scanner webapp guide is doing its job, a guide that points the browser at
// a stylesheet it does not carry is not.
var subresourcePattern = regexp.MustCompile(`(?i)<(img|link|script|style|source|video|audio|iframe|object|embed|input|track|picture|use|image)\b[^>]*>`)

var attributePattern = regexp.MustCompile(`(?i)\b(src|href|srcset|data|poster|action|xlink:href)\s*=\s*("([^"]*)"|'([^']*)')`)

// An address is anything the browser has to go out to, or anything that names a file beside the
// document. A fragment is the reader moving inside the page, and a data URI is the page carrying the
// thing itself.
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

	// Anything else names a file: a relative path, a protocol-relative path, a scheme we do not know.
	return false
}

// The guide renders everything it shows from bytes inside itself: every picture, every stylesheet, every
// script. A reader with no network gets the whole document, not a document with holes in it.
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

// The guide is read by jumping to the chapter a reader needs, so every chapter has to be in the document,
// in the order the reader works through it, and named by the contents aid.
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

// The contents aid is how a reader finds a chapter in a document this long, so it names every chapter and
// every anchor it names exists.
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

// chapterOf returns one chapter's body: the text from its heading to the end of the section that carries
// it, which is the part of the document a reader lands on when they jump to that chapter.
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

// The guide is the project's own document, and the reader meets the project's mark in its header before
// they meet a word of prose. A header whose mark is a file the reader does not have is a header that shows
// a broken frame offline, which is the one condition the guide is written to survive.
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

// A recorder configured once posts to the endpoint it was told about, and keeps posting there for years.
// A guide that names the superseded endpoint without saying it is superseded is the guide that keeps the
// deployment on it. The marking has to be in the chapter the reader is in, not in a chapter they skipped.
func TestTheGuideMarksTheSupersededUploadEndpoint(t *testing.T) {
	body := guidePath(t)

	// The facts are asserted on the document's text, with the line breaks the source is written with
	// collapsed: a sentence the reader reads as one sentence is one sentence, wherever the author broke it.
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

// The guide is a document the project hands to an administrator, and it closes the way the project hands
// it over: the sign-off is the last thing the reader meets, inside the file, so a copy of the guide carries
// it too.
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

// The opening chapter tells an administrator coming from the last published release what they are walking
// into: the changes, each one operable from a chapter of this guide, and the cost of the update. A list of
// changes whose links land nowhere is a list the reader cannot act on, and a change summary that hides that
// the database is rewritten in place is the summary that causes the lost deployment.
func TestTheGuideTellsAVersion6AdministratorWhatChanged(t *testing.T) {
	body := guidePath(t)

	chapter := chapterOf(t, body, "ch-01-overview")

	start := strings.Index(chapter, "What&#39;s new since version 6.6.3")
	if start < 0 {
		start = strings.Index(chapter, "What's new since version 6.6.3")
	}

	if start < 0 {
		t.Fatalf("the opening chapter does not carry the changes since version 6.6.3")
	}

	section := chapter[start:]

	links := regexp.MustCompile(`(?i)href="([^"]+)"`).FindAllStringSubmatch(section, -1)

	if len(links) < 5 {
		t.Fatalf("the changes since version 6.6.3 name %d chapters, want the changes to be operable", len(links))
	}

	for _, m := range links {
		href := m[1]

		if !strings.HasPrefix(href, "#") {
			t.Errorf("the changes since version 6.6.3 point at %q, outside the document", href)

			continue
		}

		if !strings.Contains(body, `id="`+strings.TrimPrefix(href, "#")+`"`) {
			t.Errorf("the changes since version 6.6.3 point at %q, which the document does not carry", href)
		}
	}

	for _, cost := range []string{"Back up", "migrates it in place", "session"} {
		if !strings.Contains(section, cost) {
			t.Errorf("the changes since version 6.6.3 do not name the update's cost: %q is missing", cost)
		}
	}
}

// serverFlag is one flag the composition root registers, with the default expression the source writes.
type serverFlag struct {
	name string
	expr string
}

// serverFlags reads the flag set the server builds at its composition root. The guide's flag tables are
// transcriptions of that set, so the set is what the tables are checked against.
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

// packageConstants collects the constants the composition root's own packages write, so a flag whose
// default is named by a constant is read as the value the server starts with.
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

// resolveDefault reads a default expression as the value the server starts with. A default the
// composition root works out at start-up has no literal to read, and says so.
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

// computedDefaults are the flags whose default the composition root works out at start-up instead of
// writing as a literal. The guide states the outcome, and the outcome is what its cell has to name.
var computedDefaults = map[string]string{
	"base_dir": "~/Rdio Scanner",
}

// defaultIsStated reports whether the guide's cell states the default the server starts with. An unset
// default is stated as none, as the em dash the appendix uses, or as the driver's own answer; a switch
// left off is stated as off or false.
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

// The chapters a reader looks the server's flags up in: the chapter that operates them, and the appendix
// that lists them. A chapter may split the set across tables — the modes the same command line selects
// are not the settings the server runs with — so every table in the chapter carrying a default is read.
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

// The navigation is the map an administrator works from, and the guide's table of sections is that map with a
// chapter beside every item. A table sorted one way while the column is ordered another leaves the reader to
// match the two up by hand, and a row pointing at the wrong chapter describes a section as if it were
// documented somewhere it is not. The order is read out of the administration frontend's own declaration, the
// way the flag defaults are read out of the composition root.
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

// navigationSections reads the sections the administration navigation declares, in the order it declares them.
// The declaration is one array of name and label entries; a navigation that stops declaring itself this way is
// a navigation the guide can no longer be checked against, and that is named rather than guessed at.
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

// A flag the guide tabulates that the server does not accept is a flag an administrator types and the
// server refuses. A default the guide states wrongly is a setting the administrator believes they have.
// Both tables are checked, because a reader looks the flags up in the appendix as often as they read
// them in the chapter.
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

// The HTTP methods the contract publishes as operations. The contract's websocket entries are the
// listener's channel, not endpoints a client calls.
var httpMethods = map[string]bool{
	"get": true, "post": true, "put": true, "delete": true, "patch": true, "head": true, "options": true,
}

// The word the guide uses for each credential the contract names.
var credentialWords = map[string]string{
	"adminJwt":   "session",
	"accessCode": "access code",
}

// publishedEndpoints reads the endpoints the contract publishes with the credential each takes, and the
// header each credential is presented in.
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

// The endpoint table is the list an administrator drives a deployment from. A path the guide carries that
// the contract does not publish is a call that fails; a published endpoint the guide leaves out is the
// endpoint nobody uses. The credential each endpoint takes is the difference between a script that
// authenticates and a script that is refused.
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

// The guide is read where nothing else is: an extracted archive, a mail attachment, a machine with no
// network. A link that leaves the document is a page that renders empty there, and a document the guide
// names that the reader does not have is the instruction the reader cannot follow. The scanner webapp's
// guide is the one document the guide names, and it names it as the out-of-scope pointer.
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
		case "webapp.md", "docs/webapp.md", "admin-guide.html", "docs/admin-guide.html":
			continue
		}

		t.Errorf("the guide names %q, a document the reader does not have beside the guide", name)
	}
}

// The size budget the guide is written to: one file a reader carries. The measurement is recorded on
// every run, so the number the guide promises is the number the file has.
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

// The release matrix table is what an administrator reads to decide what a deployment runs, and the column
// that says whether the archive carries its own ffmpeg is the reason a deployment installs nothing. Every
// target this project releases carries one inside the program, and the guide has to say so for every row:
// the ffmpeg binaries this project's build compiles for the Linux and Windows targets, and the binaries
// compiled on the target's own platform and adopted by the release for the macOS and FreeBSD ones.
func TestTheReleaseMatrixSaysEveryArchiveCarriesItsOwnFfmpeg(t *testing.T) {
	const caption = "The release matrix, as the build tool defines it. Every release carries its own ffmpeg inside the program; the macOS and FreeBSD ffmpeg binaries are compiled on the platform each target runs on, and the build adopts them into the release."

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

// words leaves the words a chapter carries, with the tags and the line wrapping taken out: the phrases
// the tests pin are the phrases a reader reads, and the guide's source wraps them across lines.
func words(body string) string {
	return strings.Join(strings.Fields(stripTags(body)), " ")
}

// The macOS and FreeBSD ffmpeg binaries are compiled by the build on the machine that is the target, with
// the one command the operator already knows, and the guide is where an operator reads that. The statement
// is pinned in the places the ritual is written — the chapter that says what an archive carries, the
// chapter that lists the commands, the per-platform sections of the service chapter, and the licence
// appendix — because a reader who finds the old ritual in one of them follows the old ritual.
//
// The hand route stays: binaries built by hand are dropped where the build looks for them, and the build
// runs them and adopts what converts. What the guide has to stop doing is listing the compile's own steps
// as the operator's ritual, because the build runs them, and a guide that tells an operator to run them
// invites binaries the release adopts on nothing.
// The ffmpeg binaries every release carries are compiled for audio: the recipe disables every component
// and enables the audio components the converter asks of ffmpeg, and the run that adopts binaries refuses
// ones that register a video codec. An operator decides what to install, and what to do when a conversion
// fails, from chapters that say what the program they are installing carries, so the statement is pinned
// where they read it: the requirements chapter, the installation chapter, the service sections, the
// troubleshooting entry, the FAQ answer, and the licence appendix.
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

func TestTheGuideSaysTheFfmpegBinariesAreCompiledByTheBuildOnTheirOwnHost(t *testing.T) {
	body := guidePath(t)

	places := []struct {
		chapter string
		need    []string
	}{
		{
			chapter: "ch-03-requirements",
			need: []string{
				"compiled by this build on a machine that runs the target's operating system",
				"this build compiles them on a machine that runs the target's operating system",
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
				"this build compiles them on the machine that is the target",
			},
		},
		{
			chapter: "ch-27-reference",
			need: []string{
				"names the host that compiled them",
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

	// The command is the whole of the ritual, so it stands on its own line in the list of commands,
	// where an operator reads the commands from.
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

// The two macOS architectures compile each other's ffmpeg binaries, and the guide is where an operator
// learns which Mac to stand on. The statements here are the ones that answer the question the release host
// cannot: there is no machine that is both macOS architectures, so the release of the Intel archive comes
// from an Apple Silicon Mac, and the release of the Apple Silicon archive can come from an Intel Mac.
// Rosetta 2 is what lets the Apple Silicon Mac run the Intel binaries it compiled, and the run an Intel Mac
// performs on the arm64 binaries is the run that proves the program the Apple Silicon machine ships.
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
				"FreeBSD is compiled on FreeBSD",
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

	// The compile across the two architectures is a command an operator runs, so it stands on its own
	// line in the list of commands, beside the one command for the platform you are on.
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

	// The words the operator types are the words go names a macOS machine by, and the guide is where
	// they are taught: darwin-amd64 and darwin-arm64 are accepted as the release words.
	for _, word := range []string{"darwin-amd64", "darwin-arm64"} {
		if !strings.Contains(words(chapterOf(t, body, "ch-26-faq")), word) {
			t.Errorf("the guide never says the build accepts %s", word)
		}
	}
}

// The compile is the check, and it is one command: the guide is where an operator learns that nothing
// else is asked of them. The check command, the digests to copy, and the manifest to edit are gone, and a
// guide that still named them would send an operator to a command the tool refuses and a file the build
// never reads.
//
// The compile's own steps are the build's, and the hand route asks the operator to drop two files and run
// one command: the guide says that, and says nothing about what the build records, because the build
// records nothing.
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

// COMPILING.md is the developer's note beside the repository, and it states the same ritual the guide
// states: one command on a Mac, one on a FreeBSD box, one Apple Silicon Mac for both macOS archives, the
// compile running the binaries it compiled, no check command, and no digests for anybody to copy.
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
	} {
		if !strings.Contains(body, want) {
			t.Errorf("COMPILING.md never says %q", want)
		}
	}

	for _, gone := range []string{
		"ffmpeg-check",
		"digests it prints",
		"proof file",
		".build/ffmpeg/checks",
		"no code signature",
		"program interpreter",
	} {
		if strings.Contains(body, gone) {
			t.Errorf("COMPILING.md says %q, which describes the audit this build no longer performs", gone)
		}
	}
}

// The hand-built drop is adopted by the three targets whose ffmpeg binaries are compiled where they run.
// The five this build compiles in a container take the binaries the container produced: a program an
// operator drops in one of those directories is embedded by the server binary, is used by nothing, and
// no message says it is there. The guide is where the operator reads the drop route, so the guide names
// the targets the route belongs to and says what a drop in the other directories costs.
func TestTheGuideScopesTheHandBuiltDropToTheTargetsThatAdoptIt(t *testing.T) {
	body := guidePath(t)

	chapter := strings.ToLower(words(chapterOf(t, body, "ch-04-installation")))

	for _, want := range []string{
		"drop ffmpeg and ffprobe in the target's bundle directory",
		"the macOS and FreeBSD targets",
		"embedded in the binary and used by nothing",
	} {
		if !strings.Contains(chapter, strings.ToLower(want)) {
			t.Errorf("the guide's installation chapter never says %q", want)
		}
	}

	notes := strings.ToLower(words(repoFile(t, "COMPILING.md")))

	for _, want := range []string{
		"drop `ffmpeg` and `ffprobe`",
		"the three targets compiled on their own platform",
		"embedded in the binary and used by nothing",
	} {
		if !strings.Contains(notes, strings.ToLower(want)) {
			t.Errorf("COMPILING.md never says %q", want)
		}
	}
}
