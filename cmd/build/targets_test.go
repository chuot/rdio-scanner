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

	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// The release matrix is one list, and the two facts it carries are kept apart: the platforms this
// project ships, and which of them carry an ffmpeg. The first is a product decision and lives here;
// the second is a build fact and lives in the manifest. These tests read the list the tool composes
// from, and refuse a target the matrix has no place for.

// The eight platforms are the release, and the names are the names the archives carry. macos is the
// name this project ships, and darwin is the operating system go builds it for: the two are not the
// same word, and a matrix that used one for both builds a macos release for the wrong binary.
func TestTheReleaseMatrixIsTheEightPlatformsThisProjectShips(t *testing.T) {
	want := []string{
		"freebsd-amd64",
		"linux-386",
		"linux-amd64",
		"linux-arm",
		"linux-arm64",
		"macos-amd64",
		"macos-arm64",
		"windows-amd64",
	}

	got := []string{}
	for _, tr := range releaseTargets {
		got = append(got, tr.targetWord())
	}

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the release matrix is %v, want %v", got, want)
	}

	// The operating instructions an administrator reads are in the guide the archive carries: the service
	// chapter carries a sub-section named for each system the release matrix ships. A matrix that named a
	// system the guide has no sub-section for ships an archive whose only document says nothing about the
	// system the reader is installing on.
	repo, err := repositoryRoot()
	if err != nil {
		t.Fatalf("the tool cannot find the repository it is releasing: %v", err)
	}

	guide, err := os.ReadFile(filepath.Join(repo, "docs/admin-guide.html"))
	if err != nil {
		t.Fatalf("docs/admin-guide.html is missing: %v\nthe archive carries it, and its service chapter is where each system's instructions live", err)
	}

	headings := serviceChapterHeadings(string(guide))

	for _, tr := range releaseTargets {
		found := false

		for _, heading := range headings {
			if strings.EqualFold(heading, tr.name) {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("the guide's service chapter has no sub-section named %q for %s, and the archive for that system carries the guide as its only instructions", tr.name, tr.targetWord())
		}
	}

	// The one platform whose release name is not a go operating system.
	tr, found := targetFor("macos-arm64")
	if !found {
		t.Fatal("macos-arm64 is not in the release matrix")
	}

	if tr.goos != "darwin" {
		t.Errorf("macos-arm64 is built for %q, want darwin", tr.goos)
	}

	// The server binary for a macos target is built for darwin, and the archive is named macos.
	o, err := parseArgs([]string{"dist", "macos-arm64"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	s := serverStep(o, "/repo", theVersion(t))

	if !contains(s.env, "GOOS=darwin") {
		t.Errorf("the server for macos-arm64 is built with env %v, which does not name darwin", s.env)
	}

	if !strings.Contains(s.result, "macos-arm64") {
		t.Errorf("the server for macos-arm64 is written to %q, which is not the macos archive directory", s.result)
	}
}

// serviceChapterHeadings lists the subsection titles of the guide's service chapter: the h3 headings
// between the chapter's own heading and the next chapter. The release matrix names the systems the
// guide has to carry instructions for, and these headings are where the guide says so.
func serviceChapterHeadings(guide string) []string {
	const chapter = `id="ch-21-running-as-a-service"`

	start := strings.Index(guide, chapter)
	if start < 0 {
		return nil
	}

	rest := guide[start:]
	if end := strings.Index(rest, "<h2 "); end >= 0 {
		rest = rest[:end]
	}

	headings := []string{}
	for {
		i := strings.Index(rest, "<h3>")
		if i < 0 {
			break
		}

		rest = rest[i+len("<h3>"):]

		j := strings.Index(rest, "</h3>")
		if j < 0 {
			break
		}

		headings = append(headings, strings.TrimSpace(rest[:j]))
		rest = rest[j+len("</h3>"):]
	}

	return headings
}

// Which targets carry an ffmpeg is answered by the manifest, not by a second list. The three this
// project ships with the ffmpeg their host installs have no record, and the build says so.
func TestTheTargetsThatCarryAnFfmpegAreTheOnesTheManifestRecords(t *testing.T) {
	manifest := bundle.ReadManifest()

	covered := []string{}
	for _, tr := range releaseTargets {
		if _, ok := manifest.Targets[tr.targetWord()]; ok {
			covered = append(covered, tr.targetWord())
		}
	}

	want := []string{"linux-386", "linux-amd64", "linux-arm", "linux-arm64", "windows-amd64"}

	if strings.Join(covered, " ") != strings.Join(want, " ") {
		t.Errorf("the targets with a manifest record are %v, want %v", covered, want)
	}

	for _, word := range []string{"freebsd-amd64", "macos-amd64", "macos-arm64"} {
		if _, ok := manifest.Targets[word]; ok {
			t.Errorf("%s has a manifest record, and this project ships it with the ffmpeg its host installs", word)
		}
	}
}

// A dist run asked for one target builds that target; asked for none, it builds the whole matrix.
func TestADistRunBuildsTheTargetItIsAskedFor(t *testing.T) {
	o, err := parseArgs([]string{"dist"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if o.sub != "dist" {
		t.Errorf("the subcommand is %q, want dist", o.sub)
	}

	if o.target != "" {
		t.Errorf("a dist run that named no target builds for %q, want the whole matrix", o.target)
	}

	o, err = parseArgs([]string{"dist", "linux-arm64"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if o.target != "linux-arm64" {
		t.Errorf("the dist run builds for %q, want linux-arm64", o.target)
	}
}

// A target outside the matrix is a mistake in the command line, and refusing it here names the target
// the operator typed rather than building something they did not ask for.
func TestADistRunAskedForATargetOutsideTheMatrixIsRefused(t *testing.T) {
	_, err := parseArgs([]string{"dist", "netbsd-amd64"})
	if err == nil {
		t.Fatal("a dist run for netbsd-amd64 was accepted, want a refusal")
	}

	if !strings.Contains(err.Error(), "netbsd-amd64") {
		t.Errorf("the refusal %q does not name the target it rejected", err.Error())
	}

	_, err = parseArgs([]string{"dist", "linux"})
	if err == nil {
		t.Fatal("a dist run for the word linux was accepted, want a refusal")
	}
}
