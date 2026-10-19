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

// Which targets carry an ffmpeg is answered by the manifest, not by a second list. Every release target
// has a record: five records name the toolchain this build compiles the ffmpeg binaries in, and three
// name the binaries the maintainer compiles on the platform the target runs on.
func TestTheTargetsThatCarryAnFfmpegAreTheOnesTheManifestRecords(t *testing.T) {
	manifest := bundle.ReadManifest()

	compiled := []string{}
	supplied := []string{}

	for _, tr := range releaseTargets {
		record, ok := manifest.Targets[tr.targetWord()]
		if !ok {
			t.Errorf("the release matrix ships %s and the manifest has no record for it", tr.targetWord())
			continue
		}

		switch record.Produced {
		case "":
			compiled = append(compiled, tr.targetWord())
		case "supplied":
			supplied = append(supplied, tr.targetWord())
		default:
			t.Errorf("%s says it is produced by %q, which is not a way this project produces an ffmpeg", tr.targetWord(), record.Produced)
		}
	}

	wantCompiled := []string{"linux-386", "linux-amd64", "linux-arm", "linux-arm64", "windows-amd64"}

	if strings.Join(compiled, " ") != strings.Join(wantCompiled, " ") {
		t.Errorf("the targets this build compiles an ffmpeg for are %v, want %v", compiled, wantCompiled)
	}

	wantSupplied := []string{"freebsd-amd64", "macos-amd64", "macos-arm64"}

	if strings.Join(supplied, " ") != strings.Join(wantSupplied, " ") {
		t.Errorf("the targets whose ffmpeg is compiled on their own platform are %v, want %v", supplied, wantSupplied)
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
		t.Errorf("a dist run that named no target names %s, and the machine running it decides the target", o.target)
	}

	o, err = parseArgs([]string{"dist", "linux-arm64"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if o.target != "linux-arm64" {
		t.Errorf("the dist run builds for %q, want linux-arm64", o.target)
	}
}

// The list a release run walks is decided once, before any of it runs: the target the operator named,
// every platform the matrix releases when the whole matrix was asked for, and the release target of the
// machine running it when neither was. A machine the project releases nothing for is refused naming the
// flag that asks for the matrix, because building nothing at all is not what a release run is for.
func TestTheDistTargetListIsTheNamedTargetTheMatrixOrTheMachine(t *testing.T) {
	named, err := distTargets(options{target: "windows-amd64"}, "linux", "arm64")
	if err != nil {
		t.Fatalf("distTargets: %v", err)
	}

	if len(named) != 1 || named[0].targetWord() != "windows-amd64" {
		t.Errorf("dist windows-amd64 walks %v, want windows-amd64 alone", targetWords(named))
	}

	matrix, err := distTargets(options{all: true}, "linux", "arm64")
	if err != nil {
		t.Fatalf("distTargets: %v", err)
	}

	if len(matrix) != len(releaseTargets) {
		t.Errorf("dist --all walks %d targets, want the whole matrix of %d", len(matrix), len(releaseTargets))
	}

	machine, err := distTargets(options{}, "linux", "arm64")
	if err != nil {
		t.Fatalf("distTargets: %v", err)
	}

	if len(machine) != 1 || machine[0].targetWord() != "linux-arm64" {
		t.Errorf("dist walks %v, want the release target of the machine running it", targetWords(machine))
	}

	// The macOS name is not the operating system go builds it for: a machine running darwin is a
	// macos release target, and the archive it builds is named for macos.
	apple, err := distTargets(options{}, "darwin", "arm64")
	if err != nil {
		t.Fatalf("distTargets: %v", err)
	}

	if len(apple) != 1 || apple[0].targetWord() != "macos-arm64" {
		t.Errorf("on darwin-arm64, dist walks %v, want macos-arm64", targetWords(apple))
	}

	_, err = distTargets(options{}, "plan9", "riscv")
	if err == nil {
		t.Fatal("dist accepted a machine this project releases nothing for, and built nothing at all")
	}

	if !strings.Contains(err.Error(), "--all") {
		t.Errorf("the refusal %q does not name the flag that asks for the matrix", err.Error())
	}
}

func targetWords(targets []releaseTarget) []string {
	words := []string{}

	for _, tr := range targets {
		words = append(words, tr.targetWord())
	}

	return words
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

// The machine running the build is named by the release target it belongs to, because the manifest, the
// output path, and every message an operator reads name the target by the name the release uses. The name
// go builds a macOS binary for is darwin, and the release is named macos: a machine named by go's word
// asks the manifest about a target this project has no record for, and the operator standing on the Mac
// the ffmpeg binaries are built on is told that no ffmpeg is compiled for a platform they are standing on.
//
// A machine the matrix has no entry for keeps the name go gives it: a hand-named target is built for the
// platform its own name names, and the phase asked to build for it says what it can do there.
// go builds a macOS binary for darwin, and the release names the same machine macos. A person naming the
// target their own machine is types the word go gave it, and the release word is what the manifest records,
// what the archive is named for, and what the guide uses. The word typed is resolved to the release word
// where the run reads it, so every phase downstream sees one name; a word the matrix has no target for
// keeps the words it was typed with, and the phase asked to build for it says what it can do there.
func TestTheWordGoNamesAMachineByIsTheReleaseWord(t *testing.T) {
	for _, typed := range []struct{ word, release string }{
		{"darwin-amd64", "macos-amd64"},
		{"darwin-arm64", "macos-arm64"},
		{"macos-amd64", "macos-amd64"},
		{"macos-arm64", "macos-arm64"},
		{"freebsd-amd64", "freebsd-amd64"},
		{"linux-amd64", "linux-amd64"},
		{"windows-amd64", "windows-amd64"},
		{"darwin-riscv64", "darwin-riscv64"},
		{"plan9-riscv", "plan9-riscv"},
	} {
		if got := theReleaseWordTheRunBuildsFor(typed.word); got != typed.release {
			t.Errorf("a run asked for %q builds for %q, want %q", typed.word, got, typed.release)
		}
	}

	for _, word := range []string{"darwin-amd64", "darwin-arm64"} {
		tr, found := targetFor(word)
		if !found {
			t.Errorf("%s names no release target, and the manifest records the macOS target the same machine runs", word)
			continue
		}

		if !strings.HasPrefix(tr.targetWord(), "macos-") {
			t.Errorf("%s resolves to %s: a macOS machine's release target is the macOS word", word, tr.targetWord())
		}
	}
}

func TestTheMachineIsNamedByTheReleaseTargetItBelongsTo(t *testing.T) {
	for _, machine := range []struct{ goos, goarch, word string }{
		{"freebsd", "amd64", "freebsd-amd64"},
		{"linux", "386", "linux-386"},
		{"linux", "amd64", "linux-amd64"},
		{"linux", "arm", "linux-arm"},
		{"linux", "arm64", "linux-arm64"},
		{"darwin", "amd64", "macos-amd64"},
		{"darwin", "arm64", "macos-arm64"},
		{"windows", "amd64", "windows-amd64"},
		{"linux", "riscv64", "linux-riscv64"},
		{"plan9", "riscv", "plan9-riscv"},
	} {
		if got := releaseTargetWordFor(machine.goos, machine.goarch); got != machine.word {
			t.Errorf("a machine running %s on %s is named %q, want %q", machine.goos, machine.goarch, got, machine.word)
		}
	}
}
