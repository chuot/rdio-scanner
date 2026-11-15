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
	"regexp"
	"strings"
	"testing"
)

// these checks hold the Version 8.0 entry to the shape an operator reads: the
// changes gathered under what they touch, every group carrying an entry, and no
// line speaking for the release's state instead of for what it contains.

// versionEightGroups are the entry's group headings, in the order it states them.
var versionEightGroups = []string{
	"A rebuild, not an update",
	"Updating from version 6.6.3",
	"The listening page",
	"The log and the administration page",
	"Getting audio in",
	"One licence for the whole server",
}

// assemblyStatuses are the wordings that state a release is still being put
// together. The entry names what landed and stops there, so an earlier feature's
// note about the entry itself does not come back with the next one.
var assemblyStatuses = []string{
	"entry in progress",
	"in progress",
	"being assembled",
	"being put together",
	"not yet complete",
	"not yet finished",
	"names what has landed",
}

// versionEightSection returns the Version 8.0 entry: from its own heading to the
// next section heading, so every check holds over the entry and no other version's.
func versionEightSection(t *testing.T) string {
	t.Helper()

	body := repoFile(t, "CHANGELOG.md")

	const heading = "\n## Version 8.0\n"

	start := strings.Index(body, heading)
	if start < 0 {
		t.Fatalf("the change log does not carry a Version 8.0 section")
	}

	rest := body[start+len(heading):]

	if end := strings.Index(rest, "\n## "); end >= 0 {
		return rest[:end]
	}

	return rest
}

func TestTheVersionEightEntryGroupsItsChanges(t *testing.T) {
	section := versionEightSection(t)

	bulletAtLineStart := regexp.MustCompile(`(?m)^- `)
	groupHeading := regexp.MustCompile(`\n### `)

	// The groups name what each change touches, and they follow one another in
	// the order a reader works through the update.
	after := 0

	for _, name := range versionEightGroups {
		title := "### " + name + "\n"

		at := strings.Index(section[after:], title)
		if at < 0 {
			t.Fatalf("the Version 8.0 entry does not carry the group %q after the groups before it", name)
		}

		at += after

		next := len(section)
		if later := groupHeading.FindStringIndex(section[at+len(title):]); later != nil {
			next = at + len(title) + later[0]
		}

		if entries := bulletAtLineStart.FindAllString(section[at+len(title):next], -1); len(entries) == 0 {
			t.Errorf("the group %q carries no entry under its heading", name)
		}

		after = at + len(title)
	}

	// The summary that closes the entry names the three parts of the product the
	// release touches, so it reads as the entry's own close and not one more group.
	bullets := bulletAtLineStart.FindAllString(section, -1)

	if len(bullets) == 0 {
		t.Fatalf("the Version 8.0 entry carries no entry to read")
	}

	summary := strings.ToLower(section[strings.LastIndex(section, "\n- "):])

	for _, part := range []string{"server", "administration", "listener"} {
		if !strings.Contains(summary, part) {
			t.Errorf("the entry that closes the Version 8.0 section does not name the %s it touches: %q", part, strings.TrimSpace(summary))
		}
	}

	// The warning about backing up comes before any group, where a reader scanning
	// the group headings still meets it.
	warning := "**Make sure to backup your config and your database before updating to Version 8.0.**"

	warningAt := strings.Index(section, warning)
	firstGroup := strings.Index(section, "### ")

	if warningAt < 0 {
		t.Errorf("the Version 8.0 section does not carry the warning to back up first")
	} else if firstGroup >= 0 && warningAt > firstGroup {
		t.Errorf("the warning to back up first sits after the group headings, where a reader scanning them misses it")
	}

	// The entry states what landed. It says nothing about the entry itself.
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			continue
		}

		for _, status := range assemblyStatuses {
			if strings.Contains(strings.ToLower(trimmed), status) {
				t.Errorf("the Version 8.0 section states the release's state instead of what it contains: %q", trimmed)

				break
			}
		}
	}
}
