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
	"fmt"
	"html"
	"strings"
)

// renderMarkdown turns the markdown the documentation is written in into HTML.
//
// The constructs it knows are the ones the documentation uses, measured in
// docs/specs/FEAT-013-go-build-retires-the-makefile/research.md: headings, paragraphs with hard breaks,
// bold, emphasis, inline code, links, images, tables, ordered and unordered lists with nesting,
// blockquotes, indented code blocks, and the raw page break the pandoc build passed through. A construct
// the documentation does not use is not worth a parser, and a markdown library buys parsing for
// constructs these documents never use.
//
// The renderer is not a markdown implementation. It is the subset this repository's documentation is
// written in, and a document written outside it renders with the markup left in it, which is what the
// test that renders the real sources checks for.
func renderMarkdown(source string) string {
	var out strings.Builder

	lines := strings.Split(source, "\n")

	i := 0
	for i < len(lines) {
		line := lines[i]

		switch {
		case strings.TrimSpace(line) == "":
			i++

		case isHeading(line):
			level := strings.IndexByte(line, ' ')
			out.WriteString(fmt.Sprintf("<h%d>%s</h%d>", level, renderInline(strings.TrimSpace(line[level+1:])), level))
			i++

		case isPageBreak(line):
			out.WriteString(`<div class="page-break"></div>`)
			i++

		case isTableStart(lines, i):
			i += renderTable(lines, i, &out)

		case isQuote(line):
			i += renderQuote(lines, i, &out)

		case isCode(lines[i]):
			i += renderCode(lines, i, &out)

		case isListItem(line):
			i += renderList(lines, i, &out)

		default:
			i += renderParagraph(lines, i, &out)
		}
	}

	return out.String()
}

// renderInline turns the spans inside one block into HTML. The spans nest: the documentation puts an
// image inside a link, and bold inside emphasis, so the content of a span is rendered the same way.
func renderInline(s string) string {
	var out strings.Builder

	i := 0
	for i < len(s) {
		c := s[i]

		if c == '`' {
			if j := strings.IndexByte(s[i+1:], '`'); j >= 0 {
				out.WriteString("<code>" + html.EscapeString(s[i+1:i+1+j]) + "</code>")
				i = i + 1 + j + 1

				continue
			}
		}

		if c == '!' && i+1 < len(s) && s[i+1] == '[' {
			if alt, url, next, ok := linkSpan(s, i+1); ok {
				out.WriteString(`<img src="` + html.EscapeString(url) + `" alt="` + html.EscapeString(alt) + `">`)
				i = next

				continue
			}
		}

		if c == '[' {
			if text, url, next, ok := linkSpan(s, i); ok {
				out.WriteString(`<a href="` + html.EscapeString(url) + `">` + renderInline(text) + `</a>`)
				i = next

				continue
			}
		}

		if c == '*' && i+1 < len(s) && s[i+1] == '*' {
			if j := strings.Index(s[i+2:], "**"); j >= 0 {
				out.WriteString("<strong>" + renderInline(s[i+2:i+2+j]) + "</strong>")
				i = i + 2 + j + 2

				continue
			}
		}

		if c == '*' {
			if j := strings.IndexByte(s[i+1:], '*'); j >= 0 {
				out.WriteString("<em>" + renderInline(s[i+1:i+1+j]) + "</em>")
				i = i + 1 + j + 1

				continue
			}
		}

		if c == '_' {
			if j := strings.IndexByte(s[i+1:], '_'); j >= 0 {
				out.WriteString("<em>" + renderInline(s[i+1:i+1+j]) + "</em>")
				i = i + 1 + j + 1

				continue
			}
		}

		out.WriteString(html.EscapeString(string(c)))
		i++
	}

	return out.String()
}

// linkSpan reads `[text](url)` starting at i, and says where the closing parenthesis ended.
//
// The closing bracket is found by counting depth, not by taking the first one: the documentation puts
// an image inside a link, and the image's own bracket closes before the link's does.
func linkSpan(s string, i int) (text string, url string, next int, ok bool) {
	depth := 0
	end := -1

	for k := i; k < len(s); k++ {
		if s[k] == '[' {
			depth++
		}

		if s[k] == ']' {
			depth--

			if depth == 0 {
				end = k

				break
			}
		}
	}

	if end < 0 {
		return "", "", i, false
	}

	rest := s[end+1:]
	if !strings.HasPrefix(rest, "(") {
		return "", "", i, false
	}

	close := strings.IndexByte(rest, ')')
	if close < 0 {
		return "", "", i, false
	}

	return s[i+1 : end], rest[1:close], end + 1 + close + 1, true
}

func isHeading(line string) bool {
	level := 0

	for level < len(line) && line[level] == '#' {
		level++
	}

	return level > 0 && level <= 6 && level < len(line) && line[level] == ' '
}

func isPageBreak(line string) bool {
	return strings.TrimSpace(line) == `\pagebreak{}`
}

func isTableStart(lines []string, i int) bool {
	if i+1 >= len(lines) {
		return false
	}

	return isTableRow(lines[i]) && isTableSeparator(lines[i+1])
}

func isTableRow(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "|")
}

func isTableSeparator(line string) bool {
	if !isTableRow(line) {
		return false
	}

	for _, r := range line {
		if r != '|' && r != '-' && r != ' ' && r != ':' {
			return false
		}
	}

	return strings.Contains(line, "-")
}

func isQuote(line string) bool {
	return strings.HasPrefix(line, ">")
}

func isCode(line string) bool {
	return strings.HasPrefix(line, "    ") && strings.TrimSpace(line) != ""
}

func isListItem(line string) bool {
	trimmed := strings.TrimLeft(line, " ")

	if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
		return true
	}

	digits := 0

	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}

	return digits > 0 && digits+1 < len(trimmed) && trimmed[digits] == '.' && trimmed[digits+1] == ' '
}

func renderParagraph(lines []string, i int, out *strings.Builder) int {
	var body strings.Builder
	n := 0
	broke := false

	for i+n < len(lines) {
		line := lines[i+n]

		if strings.TrimSpace(line) == "" {
			break
		}

		if n > 0 && (isHeading(line) || isPageBreak(line) || isTableStart(lines, i+n) || isQuote(line) || isCode(line) || isListItem(line)) {
			break
		}

		hard := strings.HasSuffix(line, `\`)

		if hard {
			line = strings.TrimSuffix(line, `\`)
		}

		if body.Len() > 0 && !broke {
			body.WriteString(" ")
		}

		body.WriteString(renderInline(line))

		if hard {
			body.WriteString("<br>")
		}

		broke = hard
		n++
	}

	out.WriteString("<p>" + body.String() + "</p>")

	return n
}

func renderTable(lines []string, i int, out *strings.Builder) int {
	n := 0

	for i+n < len(lines) && isTableRow(lines[i+n]) {
		n++
	}

	out.WriteString("<table><thead><tr>")

	for _, cell := range splitRow(lines[i]) {
		out.WriteString("<th>" + renderInline(cell) + "</th>")
	}

	out.WriteString("</tr></thead><tbody>")

	for _, row := range lines[i+2 : i+n] {
		out.WriteString("<tr>")

		for _, cell := range splitRow(row) {
			out.WriteString("<td>" + renderInline(cell) + "</td>")
		}

		out.WriteString("</tr>")
	}

	out.WriteString("</tbody></table>")

	return n
}

func splitRow(row string) []string {
	cells := strings.Split(strings.Trim(strings.TrimSpace(row), "|"), "|")

	for k, cell := range cells {
		cells[k] = strings.TrimSpace(cell)
	}

	return cells
}

func renderQuote(lines []string, i int, out *strings.Builder) int {
	n := 0
	var parts []string

	for i+n < len(lines) && isQuote(lines[i+n]) {
		parts = append(parts, strings.TrimSpace(strings.TrimPrefix(lines[i+n], ">")))
		n++
	}

	out.WriteString("<blockquote><p>" + renderInline(strings.Join(parts, " ")) + "</p></blockquote>")

	return n
}

func renderCode(lines []string, i int, out *strings.Builder) int {
	n := 0
	var parts []string

	for i+n < len(lines) && isCode(lines[i+n]) {
		parts = append(parts, strings.TrimPrefix(lines[i+n], "    "))
		n++
	}

	// A code block keeps what it is given: markup inside it is not markup, and a span inside it is not
	// a span.
	out.WriteString("<pre><code>" + html.EscapeString(strings.Join(parts, "\n")) + "</code></pre>")

	return n
}

func renderList(lines []string, i int, out *strings.Builder) int {
	n := 0
	var block []string

	for i+n < len(lines) {
		line := lines[i+n]

		if strings.TrimSpace(line) == "" || !isListItem(line) {
			break
		}

		block = append(block, line)
		n++
	}

	out.WriteString(renderListItems(parseListItems(block)))

	return n
}

// listitem is one item of a list, and the items nested inside it.
type listitem struct {
	text     string
	ordered  bool
	children []*listitem
}

// parseListItems turns a block of list lines into items, nesting by indent. The documentation nests with
// a two-space indent, twelve times, all inside unordered lists.
func parseListItems(lines []string) []*listitem {
	var roots []*listitem

	// The stack is the path from the root to the item last read, with the indent each was written at.
	// An item written deeper than the last belongs to it; an item written at or above it closes that
	// item's list.
	type frame struct {
		indent int
		item   *listitem
	}

	var stack []frame

	for _, line := range lines {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		item := &listitem{text: itemText(line), ordered: isOrdered(line)}

		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}

		if len(stack) == 0 {
			roots = append(roots, item)
		} else {
			parent := stack[len(stack)-1].item
			parent.children = append(parent.children, item)
		}

		stack = append(stack, frame{indent: indent, item: item})
	}

	return roots
}

func renderListItems(items []*listitem) string {
	tag := "ul"

	if len(items) > 0 && items[0].ordered {
		tag = "ol"
	}

	var out strings.Builder

	out.WriteString("<" + tag + ">")

	for _, item := range items {
		out.WriteString("<li>" + renderInline(item.text))

		if len(item.children) > 0 {
			out.WriteString(renderListItems(item.children))
		}

		out.WriteString("</li>")
	}

	out.WriteString("</" + tag + ">")

	return out.String()
}

func isOrdered(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	digits := 0

	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}

	return digits > 0 && digits < len(trimmed) && trimmed[digits] == '.'
}

func itemText(line string) string {
	trimmed := strings.TrimLeft(line, " ")

	if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
		return strings.TrimSpace(trimmed[2:])
	}

	digits := 0

	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}

	return strings.TrimSpace(trimmed[digits+1:])
}
