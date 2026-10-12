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
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The administrator guide is one file a reader opens where nothing else is available: an extracted
// release archive, a mail attachment, a USB stick, a machine with no network. These tests pin the two
// properties that make that true — the document carries everything it renders inside itself, and it
// carries every chapter the reader is promised — so a guide that quietly reaches out to the network, or
// that loses a chapter on the way, fails the suite instead of failing the reader.

// guidePath is the guide the tests read: the file the reader opens, in the documentation folder.
func guidePath(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs(filepath.Join("..", "..", "docs", "admin-guide.html"))
	if err != nil {
		t.Fatalf("the guide path does not resolve: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the guide is not readable at %s: %v", path, err)
	}

	return string(body)
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
// a guide that points a reader at the web app guide is doing its job, a guide that points the browser at
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
