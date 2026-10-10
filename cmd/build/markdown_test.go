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

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The renderer covers the constructs the documentation actually uses, measured in the feature inventory
// in docs/specs/FEAT-013-go-build-retires-the-makefile/research.md: headings, paragraphs, bold, inline
// code, links, images, tables, ordered and unordered lists with nesting, blockquotes, indented code,
// hard breaks, and the raw page-break the pandoc build passed through. Nothing outside that set is
// supported, and a construct the documentation does not use is not worth a parser.

func TestTheHeadings(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"# What is it ?", "<h1>What is it ?</h1>"},
		{"## Version 8.0", "<h2>Version 8.0</h2>"},
	} {
		if got := renderMarkdown(c.in, spans{}); got != c.want {
			t.Errorf("%q renders %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAParagraph(t *testing.T) {
	in := "Rdio Scanner ingests audio files."
	want := "<p>Rdio Scanner ingests audio files.</p>"

	if got := renderMarkdown(in, spans{}); got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

// A trailing backslash is a hard break: the line ends where the author said it ends, and the next line
// is not a new paragraph. The documentation uses it under every image.
func TestATrailingBackslashIsABreakNotAParagraph(t *testing.T) {
	in := "first line\\\nsecond line"
	want := "<p>first line<br>second line</p>"

	if got := renderMarkdown(in, spans{}); got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

// The inline spans the documentation uses, and the two that are not used at all.
func TestTheInlineSpans(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"**rdio-scanner**", "<p><strong>rdio-scanner</strong></p>"},
		{"`-base_dir`", "<p><code>-base_dir</code></p>"},
		{"_rdio-scanner.db_", "<p><em>rdio-scanner.db</em></p>"},
		{"*Entry in progress*", "<p><em>Entry in progress</em></p>"},
		{"[Rdio Scanner](https://github.com/chuot/rdio-scanner)",
			`<p><a href="https://github.com/chuot/rdio-scanner">Rdio Scanner</a></p>`},
		{"![web app main screen](./images/webapp-main.png?raw=true)",
			`<p><img src="./images/webapp-main.png?raw=true" alt="web app main screen"></p>`},
	} {
		if got := renderMarkdown(c.in, spans{}); got != c.want {
			t.Errorf("%q renders %q, want %q", c.in, got, c.want)
		}
	}
}

// A span inside a span: the documentation puts an image inside a link, and bold inside a table cell.
func TestASpanInsideASpan(t *testing.T) {
	in := `[![Available on the App Store](../images/app-store-badge.png?raw=true)](https://apps.apple.com/us/app/rdio-scanner/id1563065667)`
	want := `<p><a href="https://apps.apple.com/us/app/rdio-scanner/id1563065667"><img src="../images/app-store-badge.png?raw=true" alt="Available on the App Store"></a></p>`

	if got := renderMarkdown(in, spans{}); got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

// Text that is markup in HTML is escaped, so a document cannot inject a script through a documentation
// file.
func TestTextThatIsMarkupInHtmlIsEscaped(t *testing.T) {
	in := "a < b && c > d & e"
	want := "<p>a &lt; b &amp;&amp; c &gt; d &amp; e</p>"

	if got := renderMarkdown(in, spans{}); got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

// The tables the documentation uses are GFM tables: a header row, a separator row, and body rows.
func TestATable(t *testing.T) {
	in := "| Recorder | API |\n| --- | --- |\n| Trunk Recorder | X |\n| ProScan | |"
	want := "<table>" +
		"<thead><tr><th>Recorder</th><th>API</th></tr></thead>" +
		"<tbody><tr><td>Trunk Recorder</td><td>X</td></tr>" +
		"<tr><td>ProScan</td><td></td></tr></tbody>" +
		"</table>"

	if got := renderMarkdown(in, spans{}); got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

// A cell holds spans, because the documentation's tables hold images and bold text.
func TestATableCellHoldsSpans(t *testing.T) {
	in := "| ![ON](./images/on.png) | **All** talkgroups |\n| --- | --- |\n| a | b |"
	want := "<table>" +
		`<thead><tr><th><img src="./images/on.png" alt="ON"></th><th><strong>All</strong> talkgroups</th></tr></thead>` +
		"<tbody><tr><td>a</td><td>b</td></tr></tbody>" +
		"</table>"

	if got := renderMarkdown(in, spans{}); got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

func TestTheLists(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"- one\n- two", "<ul><li>one</li><li>two</li></ul>"},
		{"1. one\n2. two", "<ol><li>one</li><li>two</li></ol>"},
	} {
		if got := renderMarkdown(c.in, spans{}); got != c.want {
			t.Errorf("%q renders %q, want %q", c.in, got, c.want)
		}
	}
}

// The documentation nests list items with a two-space indent, twelve times, all inside unordered lists.
func TestANestedList(t *testing.T) {
	in := "- one\n  - a\n  - b\n- two"
	want := "<ul><li>one<ul><li>a</li><li>b</li></ul></li><li>two</li></ul>"

	if got := renderMarkdown(in, spans{}); got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

// A list item holds spans, because the change log's items name the subsystem in inline code.
func TestAListItemHoldsSpans(t *testing.T) {
	in := "- the **log** names the part"
	want := "<ul><li>the <strong>log</strong> names the part</li></ul>"

	if got := renderMarkdown(in, spans{}); got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

func TestABlockquote(t *testing.T) {
	in := "> _Note that the default password is **rdio-scanner**_"
	want := "<blockquote><p><em>Note that the default password is <strong>rdio-scanner</strong></em></p></blockquote>"

	if got := renderMarkdown(in, spans{}); got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

// The documentation's command transcripts are indented, not fenced: 61 blocks of them, 415 lines, and
// no fence anywhere in the five sources.
func TestAnIndentedCodeBlock(t *testing.T) {
	in := "    [rdio@pc-linux ~]$ mkdir rdio-scanner\n    [rdio@pc-linux ~]$ cd rdio-scanner"
	want := "<pre><code>[rdio@pc-linux ~]$ mkdir rdio-scanner\n[rdio@pc-linux ~]$ cd rdio-scanner</code></pre>"

	if got := renderMarkdown(in, spans{}); got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

// A code block keeps what it is given: markup inside it is not markup, and a span inside it is not a
// span.
func TestACodeBlockKeepsWhatItIsGiven(t *testing.T) {
	in := "    **not bold** and `not code`"
	want := "<pre><code>**not bold** and `not code`</code></pre>"

	if got := renderMarkdown(in, spans{}); got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

// The page break is the one pandoc escape hatch the documentation uses, 27 times. In HTML it is a
// printing hint, not a guarantee: the browser paginates where it paginates.
func TestAPageBreakBecomesAPrintingHint(t *testing.T) {
	in := "before\n\n\\pagebreak{}\n\nafter"
	want := "<p>before</p><div class=\"page-break\"></div><p>after</p>"

	if got := renderMarkdown(in, spans{}); got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

// Blocks are separated by blank lines, and the order they are written in is the order they render in.
func TestTheBlocksKeepTheirOrder(t *testing.T) {
	in := "# Title\n\na paragraph\n\n- one\n- two\n\n> a quote"
	want := "<h1>Title</h1><p>a paragraph</p><ul><li>one</li><li>two</li></ul><blockquote><p>a quote</p></blockquote>"

	if got := renderMarkdown(in, spans{}); got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

// A document that is empty renders to nothing, so an empty source is not a document that says
// something.
func TestAnEmptySourceRendersNothing(t *testing.T) {
	if got := renderMarkdown("   \n\n  ", spans{}); got != "" {
		t.Errorf("an empty source renders %q, want nothing", got)
	}
}

// readDoc reads one of the repository's own documentation sources, because the renderer's job is the
// documentation and a test that renders a made-up document proves the parser and not the point.
func readDoc(t *testing.T, name string) string {
	t.Helper()

	repo, err := repositoryRoot()
	if err != nil {
		t.Fatalf("repositoryRoot: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(repo, name))
	if err != nil {
		t.Fatalf("the renderer cannot read %s: %v", name, err)
	}

	return string(body)
}

// The renderer is given the whole documentation, so the counts the feature inventory measured have to
// survive the render. A construct the parser misses shows up here as a count that is short.
func TestTheRealDocumentationRendersEveryConstructItUses(t *testing.T) {
	sources := []string{
		"docs/platforms/freebsd.md",
		"docs/platforms/linux.md",
		"docs/platforms/macos.md",
		"docs/platforms/windows.md",
		"docs/webapp.md",
		"docs/faq.md",
		"CHANGELOG.md",
	}

	counts := map[string]int{}

	for _, name := range sources {
		html := renderMarkdown(readDoc(t, name), spans{})

		counts["h1"] += strings.Count(html, "<h1>")
		counts["h2"] += strings.Count(html, "<h2>")
		counts["h3"] += strings.Count(html, "<h3>")
		counts["table"] += strings.Count(html, "<table>")
		counts["th"] += strings.Count(html, "<th>")
		counts["td"] += strings.Count(html, "<td>")
		counts["img"] += strings.Count(html, "<img ")
		counts["a"] += strings.Count(html, "<a href=")
		counts["strong"] += strings.Count(html, "<strong>")
		counts["code"] += strings.Count(html, "<code>")
		counts["pre"] += strings.Count(html, "<pre>")
		counts["li"] += strings.Count(html, "<li>")
		counts["blockquote"] += strings.Count(html, "<blockquote>")
		counts["page-break"] += strings.Count(html, `class="page-break"`)

		// A construct the parser did not recognise survives as the text it was written as.
		for _, leftover := range []string{"**", "| ---", "\\pagebreak", "![", "]("} {
			if strings.Contains(html, leftover) {
				t.Errorf("%s renders with %q left in it, which is a construct the parser did not take", name, leftover)
			}
		}
	}

	// Measured across the five sources: 35 level-one, 51 level-two, and 9 level-three headings, 8 tables
	// of 60 rows, 55 images, 61 indented code blocks, 11 blockquotes, and 27 page breaks.
	for _, c := range []struct{ key string; want int }{
		{"h1", 35},
		{"h2", 51},
		{"h3", 9},
		{"table", 8},
		{"img", 55},
		{"pre", 61},
		{"blockquote", 11},
		{"page-break", 27},
	} {
		if counts[c.key] != c.want {
			t.Errorf("the render has %d %s, want %d", counts[c.key], c.key, c.want)
		}
	}
}
