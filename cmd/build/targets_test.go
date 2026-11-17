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

// the release matrix is one list, and the two facts it carries are
// kept apart: the platforms this project ships, a product decision
// that lives here, and which of them carry an ffmpeg, a build fact
// that lives in the manifest.

// the eight platforms are the release, named as the archives are
// named: macos is the shipped name and darwin the operating system
// word go builds it for, and they are not interchangeable.
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

	// the guide the archive carries has a service sub-section named for each
	// system the matrix ships, and a matrix naming a system the guide has
	// none for ships an archive whose document says nothing about the
	// system the reader installs on.
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

// serviceChapterHeadings lists the subsection titles of the guide's
// service chapter: the h3 headings before the next chapter, where
// the guide says which systems it carries instructions for.
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

// the targets that carry an ffmpeg are the ones the manifest records,
// not a second list: every release target has a record, and every
// record names a container this build compiles the pair in.
func TestTheTargetsThatCarryAnFfmpegAreTheOnesTheManifestRecords(t *testing.T) {
	manifest := bundle.ReadManifest()

	compiled := []string{}

	for _, tr := range releaseTargets {
		record, ok := manifest.Targets[tr.targetWord()]
		if !ok {
			t.Errorf("the release matrix ships %s and the manifest has no record for it", tr.targetWord())
			continue
		}

		if record.Container == "" {
			t.Errorf("%s has a manifest record that names no container to compile its ffmpeg in", tr.targetWord())
			continue
		}

		compiled = append(compiled, tr.targetWord())
	}

	wantCompiled := []string{"freebsd-amd64", "linux-386", "linux-amd64", "linux-arm", "linux-arm64", "macos-amd64", "macos-arm64", "windows-amd64"}

	if strings.Join(compiled, " ") != strings.Join(wantCompiled, " ") {
		t.Errorf("the targets this build compiles an ffmpeg for are %v, want %v", compiled, wantCompiled)
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

// the dist target list is the named target, the whole matrix, or the
// machine's release target, decided once before the run walks it; a
// machine nothing is released for is refused naming the matrix flag,
// because building nothing at all is not what a release run is for.
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

	// a machine running darwin is a macos release target, and the archive
	// it builds is named for macos.
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

// the container target list is the named architecture, the three architectures a container image is
// built for, or the architecture of the machine the run stands on, decided once before the run walks
// it. A named architecture is accepted whatever machine the run stands on; a machine outside the
// three is refused naming that machine, the three, and both remedies a run naming no target has.
func TestTheContainerTargetListIsTheNamedTargetTheThreeArchitecturesOrTheMachine(t *testing.T) {
	named, err := containerTargetsFor(options{target: "linux-arm64"}, "darwin", "arm64")
	if err != nil {
		t.Fatalf("containerTargetsFor: %v", err)
	}

	if len(named) != 1 || named[0].targetWord() != "linux-arm64" {
		t.Errorf("container linux-arm64 walks %v, want linux-arm64 alone, whatever machine the run stands on", targetWords(named))
	}

	matrix, err := containerTargetsFor(options{all: true}, "linux", "amd64")
	if err != nil {
		t.Fatalf("containerTargetsFor: %v", err)
	}

	got := []string{}
	want := []string{}

	for _, tr := range matrix {
		got = append(got, tr.targetWord())
	}

	for _, word := range containerTargets {
		want = append(want, word)
	}

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("container --all walks %v, want the three architectures a container image is built for in the order the tool lists them: %v", got, want)
	}

	for _, machine := range []struct{ goos, goarch, word string }{
		{goos: "linux", goarch: "amd64", word: "linux-amd64"},
		{goos: "linux", goarch: "arm64", word: "linux-arm64"},
		{goos: "linux", goarch: "arm", word: "linux-arm"},
	} {
		list, err := containerTargetsFor(options{}, machine.goos, machine.goarch)
		if err != nil {
			t.Fatalf("containerTargetsFor on %s-%s: %v", machine.goos, machine.goarch, err)
		}

		if len(list) != 1 || list[0].targetWord() != machine.word {
			t.Errorf("on %s-%s, container walks %v, want %s, the release target of the machine running it",
				machine.goos, machine.goarch, targetWords(list), machine.word)
		}
	}

	// A machine the image is not built for is refused rather than handed linux-amd64 it never asked
	// for: the refusal names the machine, the three architectures, and the two ways out of it.
	for _, machine := range []struct{ goos, goarch, word string }{
		{goos: "darwin", goarch: "arm64", word: "darwin-arm64"},
		{goos: "windows", goarch: "amd64", word: "windows-amd64"},
		{goos: "freebsd", goarch: "amd64", word: "freebsd-amd64"},
		{goos: "linux", goarch: "386", word: "linux-386"},
	} {
		_, err := containerTargetsFor(options{}, machine.goos, machine.goarch)
		if err == nil {
			t.Errorf("container on %s-%s was accepted, and no container image is built for that machine", machine.goos, machine.goarch)
			continue
		}

		for _, want := range append([]string{machine.word}, containerTargets...) {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal %q for a container run on %s does not name %s", err.Error(), machine.word, want)
			}
		}

		// Both remedies: name one of the three architectures, or ask for all three.
		for _, says := range []string{
			"this machine is " + machine.word,
			"no architecture a container image is built for",
			"name one of " + strings.Join(containerTargets, ", "),
			"build all three with go run ./cmd/build container --all",
		} {
			if !strings.Contains(err.Error(), says) {
				t.Errorf("the refusal %q for a container run on %s does not say %q", err.Error(), machine.word, says)
			}
		}
	}

	// linux-386 is a platform this project releases, and a container run is refused for it all the
	// same: no ffmpeg pair and no image rootfs is built for it.
	if release, err := distTargets(options{}, "linux", "386"); err != nil || len(release) != 1 || release[0].targetWord() != "linux-386" {
		t.Errorf("dist on linux-386 walks %v with %v, want the release this project builds for that machine", targetWords(release), err)
	}

	// A named architecture outside the three is refused by the words the parse refusal uses, from any
	// machine.
	for _, machine := range []struct{ goos, goarch string }{
		{goos: "linux", goarch: "amd64"},
		{goos: "darwin", goarch: "arm64"},
	} {
		_, err := containerTargetsFor(options{target: "linux-386"}, machine.goos, machine.goarch)
		if err == nil {
			t.Errorf("container linux-386 on %s-%s was accepted, and no container image is built for it", machine.goos, machine.goarch)
			continue
		}

		for _, want := range []string{
			"a container image is built for " + strings.Join(containerTargets, ", "),
			"is not a target a container run builds for",
			"linux-386",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal %q does not say %q", err.Error(), want)
			}
		}
	}

	if _, err := containerTargetsFor(options{target: "macos-arm64"}, "darwin", "arm64"); err == nil {
		t.Error("container macos-arm64 was accepted, and no container image is built for it")
	}
}

func targetWords(targets []releaseTarget) []string {
	words := []string{}

	for _, tr := range targets {
		words = append(words, tr.targetWord())
	}

	return words
}

// the dist run refuses a target outside the matrix at once, naming
// the word the operator typed rather than building something else.
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

// the word go names a machine by resolves to the release word: the
// manifest, the output path, and every operator message name the
// target the way the release names it.
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
