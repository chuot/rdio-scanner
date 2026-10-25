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
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// these checks hold the administrator guide to one self-contained file: it carries
// everything it renders and every chapter it promises.
// see the docs-guide-checks maintainer note.

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

// the opening chapter tells an administrator coming from the last release what changed,
// every change links to a chapter of this guide, and the summary says the database is
// rewritten in place.
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
// see the docs-guide-checks maintainer note.
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
// see the docs-guide-checks maintainer note.
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

// the guide states the ffmpeg is compiled for audio only wherever operators read the
// ritual, and keeps the hand route as drop two files and run one command.
// see the docs-guide-checks maintainer note.
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

// the guide names which machine compiles each archive's ffmpeg, the container route for
// every target, and what the build reads instead of running.
// see the docs-guide-checks maintainer note.
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
// see the docs-guide-checks maintainer note.
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

// the guide says the compile is the check, one command, and no longer names a check
// command, digests to copy, or a manifest to edit.
// see the docs-guide-checks maintainer note.
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
// see the docs-guide-checks maintainer note.
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
	// see the docs-guide-checks maintainer note.
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
// see the docs-guide-checks maintainer note.
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
	// see the docs-licence-checks maintainer note.
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
