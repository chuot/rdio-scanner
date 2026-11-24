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

	bulletLines := regexp.MustCompile(`(?m)^- .*`)
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

		if entries := bulletLines.FindAllString(section[at+len(title):next], -1); len(entries) == 0 {
			t.Errorf("the group %q carries no entry under its heading", name)
		}

		after = at + len(title)
	}

	// The summary that closes the entry names the three parts of the product the
	// release touches, so it reads as the entry's own close and not one more group.
	bullets := bulletLines.FindAllString(section, -1)

	if len(bullets) == 0 {
		t.Fatalf("the Version 8.0 entry carries no entry to read")
	}

	summary := strings.ToLower(section[strings.LastIndex(section, "\n- "):])

	for _, part := range []string{"server", "administration", "listener"} {
		if !strings.Contains(summary, part) {
			t.Errorf("the entry that closes the Version 8.0 section does not name the %s it touches: %q", part, strings.TrimSpace(summary))
		}
	}

	// The entry tells an operator about the type the release adds and the talkgroup
	// numbering that type brings, in one line under the group that change belongs to.
	// A claim spread over two bullets, or one written outside a named group, is not an
	// entry a reader scanning the headings will find.
	afsClaims := []string{"edacs", "agency-fleet-subfleet", "agency", "fleet", "subfleet"}

	found := ""

	for _, group := range versionEightGroups {
		for _, entry := range bulletLines.FindAllString(versionEightGroup(t, section, group), -1) {
			lowered := strings.ToLower(entry)

			if !strings.Contains(lowered, "edacs") {
				continue
			}

			for _, want := range afsClaims {
				if !strings.Contains(lowered, want) {
					t.Errorf("the Version 8.0 entry's %q line does not say %q: %q", group, want, strings.TrimSpace(entry))
				}
			}

			if found != "" {
				t.Errorf("the Version 8.0 entry carries the EDACS line in more than one group: %q and %q",
					strings.TrimSpace(found), strings.TrimSpace(entry))
			}

			found = entry
		}
	}

	if found == "" {
		t.Errorf("no group of the Version 8.0 entry names the %s type and the talkgroup numbering it writes", "EDACS")
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

// versionEightGroup returns the entries one group of the Version 8.0 entry carries: from that
// group's heading to the next heading of the same rank, so a claim is read against the group it
// belongs to and not against whatever the entry says elsewhere.
func versionEightGroup(t *testing.T, section, name string) string {
	t.Helper()

	title := "### " + name + "\n"

	at := strings.Index(section, title)
	if at < 0 {
		t.Fatalf("the Version 8.0 entry does not carry the group %q", name)
	}

	rest := section[at+len(title):]

	if next := strings.Index(rest, "\n### "); next >= 0 {
		return rest[:next]
	}

	return rest
}

// the published container image is what an operator who runs the server in a container pulls, so
// the entry names it under its own group and says in one line what pulling the published name now
// delivers on a machine that is not amd64.
func TestTheVersionEightEntrySaysThePublishedImageCarriesTheThreeArchitectures(t *testing.T) {
	section := versionEightSection(t)

	const group = "The published container image"

	entries := regexp.MustCompile(`(?m)^- .*`).FindAllString(versionEightGroup(t, section, group), -1)

	if len(entries) == 0 {
		t.Fatalf("the group %q carries no entry to read", group)
	}

	// One line says the whole claim, so a reader scanning the group meets the pull and what it
	// delivers together.
	want := []string{
		"chuot/rdio-scanner",
		"image index",
		"linux/amd64",
		"linux/arm/v7",
		"linux/arm64",
		"its own architecture",
	}

	found := ""

	for _, entry := range entries {
		lowered := strings.ToLower(entry)

		if !strings.Contains(lowered, "image index") {
			continue
		}

		for _, phrase := range want {
			if !strings.Contains(lowered, strings.ToLower(phrase)) {
				t.Errorf("the group %q does not say %q on the line that names the image index: %q", group, phrase, strings.TrimSpace(entry))
			}
		}

		if found != "" {
			t.Errorf("the group %q states the published index in more than one entry: %q and %q",
				group, strings.TrimSpace(found), strings.TrimSpace(entry))
		}

		found = entry
	}

	if found == "" {
		t.Errorf("the Version 8.0 entry does not say under %q what pulling %s now delivers, naming the index and the three architectures it carries",
			group, "chuot/rdio-scanner")
	}

	// What the published tag was before, and the loose pull it forced on an arm host. The entry
	// names the index, so it does not also say the published name was one architecture's image.
	for _, gone := range []string{
		"amd64 only",
		"only the amd64",
		"only available for amd64",
	} {
		if strings.Contains(strings.ToLower(versionEightGroup(t, section, group)), gone) {
			t.Errorf("the group %q says %q, and the published name now carries every architecture the image is built for", group, gone)
		}
	}

	// The group is one of the entry's groups, so the line that closes the entry still closes it.
	if strings.Index(section, "### "+group+"\n") > strings.LastIndex(section, "\n- ") {
		t.Errorf("the group %q sits after the line that closes the Version 8.0 entry", group)
	}
}

// the entry tells an upgrading operator what the directory watcher now makes of a file name, in
// the words the operator would search the entry for.
func TestTheVersionEightEntrySaysWhatTheWatcherReadsOutOfAFileName(t *testing.T) {
	section := versionEightSection(t)

	claims := []struct {
		group  string
		states string
		need   []string
	}{
		{
			group:  "Getting audio in",
			states: "that a mask's frequency read is exact now",
			need:   []string{"mask", "exactly"},
		},
		{
			group:  "Getting audio in",
			states: "that the frequency a mask reads replaces the one the directory names",
			need:   []string{"replaces"},
		},
		{
			group:  "Getting audio in",
			states: "that whole-hertz talkgroup names read as they did in version 6.6.3",
			need:   []string{"#TGHZ"},
		},
		{
			group:  "Getting audio in",
			states: "that a file name's talkgroup label renames the channel it lands on",
			need:   []string{"renames", "auto-populate"},
		},
		{
			group:  "Getting audio in",
			states: "that a recording naming only a channel label stays where it is",
			need:   []string{"watched directory"},
		},
		{
			group:  "Getting audio in",
			states: "that a watched directory's site reaches its calls",
			need:   []string{"#SITE", "#SITELBL"},
		},
		{
			group:  "Updating from version 6.6.3",
			states: "that frequencies read out of file names come out exact from the first call the upgraded server files",
			need:   []string{"hertz"},
		},
	}

	for _, claim := range claims {
		group := strings.ToLower(versionEightGroup(t, section, claim.group))

		for _, want := range claim.need {
			if !strings.Contains(group, strings.ToLower(want)) {
				t.Errorf("the group %q does not state %s: it carries no %q", claim.group, claim.states, want)
			}
		}
	}
}

// the entry also has to reach the operator whose mask holds a bracket, or whose watched directory already
// holds a mask the watcher cannot read. The four words are what that person searches the entry for.
func TestTheVersionEightEntrySaysAMaskWithABracketAroundATokenIsRefused(t *testing.T) {
	section := versionEightSection(t)

	claims := []struct {
		states string
		need   string
	}{
		{states: "that the mask a watched directory carries is checked as the watcher reads it", need: "mask"},
		{states: "that a bracket written around a mask token is what the check refuses", need: "brackets"},
		{states: "that such a mask is refused when the watch is saved", need: "refused"},
		{states: "that a mask already stored and unreadable is reported, not left silent", need: "reported"},
	}

	group := strings.ToLower(versionEightGroup(t, section, "Getting audio in"))

	for _, claim := range claims {
		if !strings.Contains(group, strings.ToLower(claim.need)) {
			t.Errorf("the group %q does not state %s: it carries no %q", "Getting audio in", claim.states, claim.need)
		}
	}
}

// TestTheVersionEightEntrySaysTheUpgradeSaysEachMigrationFindingOnce holds the release's word about the
// migration announcements to one bullet of the group an operator updating from 6.6.3 reads first: the finding
// record by name, the two sentences that moved to the notice level, the shape of what a start names on a damaged
// deployment, and the one-shot run that clears the record. Someone whose log got quieter after the update has to
// find out why from this entry, and only from this one bullet.
func TestTheVersionEightEntrySaysTheUpgradeSaysEachMigrationFindingOnce(t *testing.T) {
	section := versionEightSection(t)

	claims := []struct {
		states string
		need   string
	}{
		{states: "that each reason the migration has is said once across starts", need: "said once"},
		{states: "that the record holding those findings is named", need: "migrationfindings"},
		{states: "that an option with no equivalent in this version is a notice", need: "notice"},
		{states: "that the migration bookkeeping being left behind is a notice", need: "bookkeeping"},
		{states: "that a start names the units the migration damaged", need: "units"},
		{states: "that the one-shot run clears that record", need: "-vacuum"},
		{states: "that the guide chapter holding the whole reading is pointed at", need: "chapter 24"},
	}

	bullets := regexp.MustCompile(`(?m)^- .*`).FindAllString(versionEightGroup(t, section, "Updating from version 6.6.3"), -1)

	found := 0

	for _, entry := range bullets {
		lowered := strings.ToLower(entry)

		if !strings.Contains(lowered, "migrationfindings") {
			continue
		}

		found++

		for _, claim := range claims {
			if !strings.Contains(lowered, strings.ToLower(claim.need)) {
				t.Errorf("the entry that names the findings record does not state %s: it carries no %q in %q",
					claim.states, claim.need, strings.TrimSpace(entry))
			}
		}
	}

	if found == 0 {
		t.Errorf("the group %q names no findings record, so an operator whose log changed after the update has "+
			"nothing to search for", "Updating from version 6.6.3")
	}

	if found > 1 {
		t.Errorf("%d entries of the group %q name the findings record: the reading of one record belongs in one "+
			"bullet, not spread over the group", found, "Updating from version 6.6.3")
	}
}
