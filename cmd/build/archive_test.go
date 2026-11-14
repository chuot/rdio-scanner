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
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// One archive per release target, carrying the release-archive contract:
// the program, the two guides that operate it, and the ffmpeg licence.

// the archive name matches the name the guide's installation chapter names
func TestTheArchiveIsNamedForTheTargetAndTheVersionTheReleaseResolved(t *testing.T) {
	for _, c := range []struct {
		version string
		want    string
	}{
		{
			version: "8.0.0+g19042cd",
			want:    filepath.Join("dist", "rdio-scanner-linux-amd64-v8.0.0+g19042cd.zip"),
		},
		{
			version: "8.0.0-beta1",
			want:    filepath.Join("dist", "rdio-scanner-linux-amd64-v8.0.0-beta1.zip"),
		},
	} {
		got := archivePath("linux-amd64", c.version)

		if got != c.want {
			t.Errorf("the archive of %s is written to %q, want %q", c.version, got, c.want)
		}
	}
}

// every archive entry carries the time of the file it carries,
// never the moment the archive was written
func TestEveryArchiveEntryCarriesTheTimeOfTheFileItCarries(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	program := defaultOutputPath(target)
	licence := filepath.Join("internal", "infrastructure", "ffmpeg", "bundle", target, "FFMPEG.txt")

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary the run compiled")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeRepoFile(t, repo, "docs/user-guide.html", "the guide for the listener")
	writeArchiveLicence(t, repo, target, "the licence")

	// four files, four distinct times, on even seconds,
	// the resolution the zip timestamp field carries.
	// Each is read from the path the repository holds it at and enters the
	// archive under its own name, the name the extracted folder holds.
	want := []struct {
		name     string
		path     string
		modified time.Time
	}{
		{"rdio-scanner", program, time.Date(2026, 3, 4, 5, 6, 8, 0, time.Local)},
		{"admin-guide.html", "docs/admin-guide.html", time.Date(2025, 1, 2, 3, 4, 6, 0, time.Local)},
		{"user-guide.html", "docs/user-guide.html", time.Date(2025, 2, 3, 4, 5, 7, 0, time.Local)},
		{"FFMPEG.txt", licence, time.Date(2024, 11, 22, 9, 10, 12, 0, time.Local)},
	}

	for _, w := range want {
		if err := os.Chtimes(filepath.Join(repo, w.path), w.modified, w.modified); err != nil {
			t.Fatalf("chtimes %s: %v", w.path, err)
		}
	}

	files, err := archiveFiles(repo, releaseTargets[4], program)
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	if len(files) != len(want) {
		t.Fatalf("the archive carries %d files, want %d: %v", len(files), len(want), fileNames(files))
	}

	for i, w := range want {
		if files[i].name != w.name {
			t.Fatalf("the archive carries %q at %d, want %q", files[i].name, i, w.name)
		}

		if !files[i].modified.Equal(w.modified) {
			t.Errorf("%s carries the time %v, want the time of the file it carries (%v)", w.name, files[i].modified, w.modified)
		}
	}

	// the times survive the archive into the header where a reader's unzip reads them
	path := filepath.Join(t.TempDir(), "release.zip")

	if err := writeArchive(path, files); err != nil {
		t.Fatalf("writeArchive: %v", err)
	}

	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("opening the archive: %v", err)
	}

	defer reader.Close()

	if len(reader.File) != len(want) {
		t.Fatalf("the archive holds %d entries, want %d", len(reader.File), len(want))
	}

	for i, w := range want {
		entry := reader.File[i]

		if entry.Name != w.name {
			t.Fatalf("the archive holds %q at %d, want %q", entry.Name, i, w.name)
		}

		if !entry.Modified.Truncate(time.Second).Equal(w.modified) {
			t.Errorf("the archive's entry %q carries %v, want %v", entry.Name, entry.Modified, w.modified)
		}
	}

	// the archive carries the executable mode a standard tool restores
	if mode := reader.File[0].Mode().Perm(); mode != 0o755 {
		t.Errorf("the archive's program entry has mode %o, want 0755", mode)
	}
}

// the archive carries the program and the documents that operate it,
// each entered under its own name beside the program
func TestTheArchiveCarriesTheProgramTheGuidesAndTheLicenceOfTheFfmpegItCarries(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeRepoFile(t, repo, "docs/user-guide.html", "the guide for the listener")
	writeArchiveLicence(t, repo, target, "the licence")

	files, err := archiveFiles(repo, releaseTargets[4], defaultOutputPath(target))
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	// the names a reader finds in the extracted folder, in the order the
	// release writes them, with no directory in any of them
	want := []string{
		"rdio-scanner",
		"admin-guide.html",
		"user-guide.html",
		"FFMPEG.txt",
	}

	if len(files) != len(want) {
		t.Fatalf("the archive carries %d files, want %d: %v", len(files), len(want), fileNames(files))
	}

	for i, name := range want {
		if files[i].name != name {
			t.Errorf("the archive carries %q at %d, want %q", files[i].name, i, name)
		}
	}

	// documents are found by name, not by position
	licence := ""

	for _, f := range files {
		if f.name == "FFMPEG.txt" {
			licence = string(f.body)
		}
	}

	if licence != "the licence" {
		t.Errorf("the archive carries %q as its licence, want the text the ffmpeg phase wrote", licence)
	}

	// The notice travels byte for byte out of the directory the server embeds,
	// and the archive's entry carries the name that directory holds it at: no
	// second copy of it, under a second name, is what a release carries.
	onDisk, err := os.ReadFile(filepath.Join(repo, "internal", "infrastructure", "ffmpeg", "bundle", target, "FFMPEG.txt"))
	if err != nil {
		t.Fatalf("reading the notice the ffmpeg phase left in %s: %v", filepath.Join("internal", "infrastructure", "ffmpeg", "bundle", target), err)
	}

	if licence != string(onDisk) {
		t.Errorf("the notice in the archive is not the bytes the bundle directory holds: %q against %q", licence, string(onDisk))
	}

	for _, name := range fileNames(files) {
		if name == "LICENCE.md" {
			t.Errorf("the archive carries a second notice named %s, next to the one named %s", name, "FFMPEG.txt")
		}
	}
}

// the archive carries no markdown file;
// the repository's own documents stay in the repository
func TestTheArchiveCarriesNoMarkdownFile(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeRepoFile(t, repo, "docs/user-guide.html", "the guide for the listener")
	writeRepoFile(t, repo, "CHANGELOG.md", "the change log")
	writeRepoFile(t, repo, "README.md", "the repository's front page")
	writeRepoFile(t, repo, "COMPILING.md", "the compiling notes")
	writeRepoFile(t, repo, "SECURITY.md", "the security notes")
	writeArchiveLicence(t, repo, target, "the licence")

	files, err := archiveFiles(repo, releaseTargets[4], defaultOutputPath(target))
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	for _, f := range files {
		if strings.HasSuffix(f.name, ".md") {
			t.Errorf("the archive carries %s, a markdown file", f.name)
		}
	}
}

// the archive carries the binary this run compiled, read from the path it wrote,
// never a file left standing at some other path
func TestTheArchiveCarriesTheBinaryTheRunWroteAndNotTheFileStandingAtThePathItReadsFrom(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	// a decoy file standing at the read path before the run wrote anywhere
	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary the run never compiled")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeRepoFile(t, repo, "docs/user-guide.html", "the guide for the listener")
	writeArchiveLicence(t, repo, target, "the licence")

	// The binary the run wrote, at the path the run wrote to.
	output := filepath.Join("build", "scratch", "rdio-scanner")

	writeRepoFile(t, repo, output, "the binary the run compiled")

	files, err := archiveFiles(repo, releaseTargets[4], output)
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	if files[0].name != "rdio-scanner" {
		t.Errorf("the archive carries its program under %q, want rdio-scanner", files[0].name)
	}

	if string(files[0].body) != "the binary the run compiled" {
		t.Errorf("the archive carries %q, the file that was standing at the path it reads from, and not the binary the run wrote to %q", string(files[0].body), output)
	}
}

// each guide in the archive shows every picture offline,
// on an extracted archive with no network
func TestTheGuidesInTheArchiveCarryEveryPictureInsideThemselves(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	guide := `<figure data-shot="section-systems"><img src="data:image/png;base64,aW1hZ2U=" alt="The Systems section."><figcaption>The Systems section.</figcaption></figure>`
	listenerGuide := `<figure data-shot="main-live"><img src="data:image/png;base64,bGl2ZQ==" alt="The listening screen with a call playing."><figcaption>The listening screen with a call playing.</figcaption></figure>`

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", guide)
	writeRepoFile(t, repo, "docs/user-guide.html", listenerGuide)
	writeArchiveLicence(t, repo, target, "the licence")

	files, err := archiveFiles(repo, releaseTargets[4], defaultOutputPath(target))
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	carried := 0

	for _, f := range files {
		if f.name != "admin-guide.html" && f.name != "user-guide.html" {
			continue
		}

		carried++

		if !strings.Contains(string(f.body), `src="data:image/png;base64,`) {
			t.Errorf("%s in the archive carries no picture inside itself: %q", f.name, f.body)
		}

		if strings.Contains(string(f.body), "?raw=true") {
			t.Errorf("%s in the archive asks for %q, which no folder on disk is named", f.name, "?raw=true")
		}
	}

	if carried != 2 {
		t.Errorf("the archive carries %d of the two guides, so a reader with no network is handed a document with an empty frame where a picture was to be shown", carried)
	}
}

// the guides in the archive are the repository's bytes,
// rewritten by no tool on the way in
func TestTheGuidesInTheArchiveAreTheGuidesTheRepositoryHas(t *testing.T) {
	real, err := repositoryRoot()
	if err != nil {
		t.Fatalf("the tool cannot find the repository it is releasing: %v", err)
	}

	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	// the guides are read from the repository being built,
	// so the archive is compared with the published bytes.
	// The map is keyed by the name the archive entry carries.
	committed := map[string]string{}

	for _, name := range []string{"admin-guide.html", "user-guide.html"} {
		body, err := os.ReadFile(filepath.Join(real, "docs", name))
		if err != nil {
			t.Fatalf("the release has no docs/%s to carry: %v", name, err)
		}

		committed[name] = string(body)

		writeRepoFile(t, repo, "docs/"+name, committed[name])
	}

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")

	writeArchiveLicence(t, repo, target, "the ffmpeg licence")

	files, err := archiveFiles(repo, releaseTargets[4], defaultOutputPath(target))
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	carried := 0

	for _, f := range files {
		want, ok := committed[f.name]
		if !ok {
			continue
		}

		carried++

		if string(f.body) != want {
			t.Errorf("%s in the archive is not the guide the repository has: %d bytes carried of %d written", f.name, len(f.body), len(want))
		}
	}

	if carried != len(committed) {
		t.Errorf("the archive carries %d of the %d guides the repository has, so a release ships a document it did not read from the tree it is releasing", carried, len(committed))
	}
}

// a release missing either guide stops, naming the repository path it expected the guide at
func TestAGuideThatIsNotInTheTreeStopsTheRelease(t *testing.T) {
	guides := []string{"docs/admin-guide.html", "docs/user-guide.html"}

	for _, missing := range guides {
		repo := t.TempDir()

		target := releaseTargets[4].targetWord()

		writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")

		for _, name := range guides {
			if name == missing {
				continue
			}

			writeRepoFile(t, repo, name, "the guide the tree does hold")
		}

		writeArchiveLicence(t, repo, target, "the licence")

		_, err := archiveFiles(repo, releaseTargets[4], defaultOutputPath(target))
		if err == nil {
			t.Fatalf("the release accepted an archive with no %s", missing)
		}

		if !strings.Contains(err.Error(), missing) {
			t.Errorf("the failure %q does not name the guide %s the release cannot carry", err.Error(), missing)
		}
	}
}

// every release target embeds an ffmpeg, so every archive carries its licence,
// whatever route produced that pair
func TestEveryReleaseTargetCarriesTheLicenceOfTheFfmpegItCarries(t *testing.T) {
	for _, tr := range releaseTargets {
		target := tr.targetWord()

		repo := t.TempDir()

		writeArchiveSource(t, repo, target, "rdio-scanner"+programSuffix(target), "the binary")
		writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
		writeRepoFile(t, repo, "docs/user-guide.html", "the guide for the listener")
		writeArchiveLicence(t, repo, target, "the licence of the ffmpeg "+target+" carries")

		files, err := archiveFiles(repo, tr, defaultOutputPath(tr.targetWord()))
		if err != nil {
			t.Fatalf("%s: archiveFiles: %v", target, err)
		}

		names := fileNames(files)

		if len(names) != 4 {
			t.Fatalf("%s: the archive carries %d files, want the program, the two guides and the licence: %v", target, len(names), names)
		}

		found := false

		for _, f := range files {
			if f.name == "FFMPEG.txt" {
				found = true

				if !strings.Contains(string(f.body), target) {
					t.Errorf("%s: the licence in the archive does not name the target it is the licence of: %s", target, f.body)
				}
			}
		}

		if !found {
			t.Errorf("%s: the archive carries no FFMPEG.txt: %v", target, names)
		}
	}
}

// an adopted ffmpeg's archive carries the ffmpeg phase's own notice:
// terms, source release, recipe, posture, host, command, and codecs.
func TestTheArchiveOfATargetWhoseFfmpegWasAdoptedCarriesWhatItsFfmpegIs(t *testing.T) {
	repo := t.TempDir()

	tr, found := targetFor("macos-arm64")
	if !found {
		t.Fatal("the release matrix has no macos-arm64, the target this test is about")
	}

	manifest := bundle.ReadManifest()

	run := aRunOfTheBinaries()

	licence := suppliedFFMpegLicence(manifest, "macos-arm64", run)

	writeArchiveSource(t, repo, "macos-arm64", "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeRepoFile(t, repo, "docs/user-guide.html", "the guide for the listener")
	writeRepoFile(t, repo, "docs/faq.md", "the FAQ")
	writeArchiveLicence(t, repo, "macos-arm64", licence)

	files, err := archiveFiles(repo, tr, defaultOutputPath(tr.targetWord()))
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	names := fileNames(files)

	if strings.Join(names, " ") != strings.Join([]string{"rdio-scanner", "admin-guide.html", "user-guide.html", "FFMPEG.txt"}, " ") {
		t.Fatalf("the archive carries %v, want the program, the two guides and the licence", names)
	}

	for _, f := range files {
		if strings.HasSuffix(f.name, ".md") {
			t.Errorf("the archive carries %s, a markdown file", f.name)
		}
	}

	carried := ""

	for _, f := range files {
		if f.name == "FFMPEG.txt" {
			carried = string(f.body)
		}
	}

	if carried != licence {
		t.Errorf("the archive carries %q as its licence, want the text the ffmpeg phase writes for supplied binaries", carried)
	}

	for _, want := range []string{
		"ffmpeg is free software",
		"Lesser General Public License version 2.1",
		manifest.Source.URL,
		"compiled for macos-arm64\non the machine that runs that machine code",
		"compiled for audio",
		"no network protocol is compiled in",
		"The run that adopted them asked them what they register: " + strings.Join(theAudioCodecNames, ", "),
		"These binaries were compiled on " + run.Host,
		"It converted 6 recordings",
		"go run ./cmd/build ffmpeg macos-arm64",
	} {
		if !strings.Contains(carried, want) {
			t.Errorf("the licence in the archive does not say %q:\n%s", want, carried)
		}
	}
}

// a missing ffmpeg licence stops the release, not a quiet shipping without terms
func TestAMissingFfmpegLicenceStopsTheRelease(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeRepoFile(t, repo, "docs/user-guide.html", "the guide for the listener")

	_, err := archiveFiles(repo, releaseTargets[4], defaultOutputPath(target))
	if err == nil {
		t.Fatal("the release accepted an ffmpeg with no licence to carry")
	}

	if !strings.Contains(err.Error(), "ffmpeg licence") {
		t.Errorf("the failure %q does not name the ffmpeg licence the release cannot carry", err.Error())
	}
}

// the notice is carried for the targets the ffmpeg manifest has a record for:
// a target it has no record for carries its program and the two guides, and nothing else
func TestTheArchiveOfATargetTheFfmpegManifestHasNoRecordForCarriesNoNotice(t *testing.T) {
	repo := t.TempDir()

	// A target outside the release matrix, so the manifest this repository carries
	// cannot hold a record for it.
	target := releaseTarget{name: "plan9", goos: "plan9", goarch: "riscv"}

	word := target.targetWord()

	writeArchiveSource(t, repo, word, "rdio-scanner"+programSuffix(word), "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeRepoFile(t, repo, "docs/user-guide.html", "the guide for the listener")
	writeArchiveLicence(t, repo, releaseTargets[4].targetWord(), "the licence of another target")

	files, err := archiveFiles(repo, target, defaultOutputPath(word))
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	want := []string{
		"rdio-scanner",
		"admin-guide.html",
		"user-guide.html",
	}

	if names := fileNames(files); strings.Join(names, " ") != strings.Join(want, " ") {
		t.Fatalf("the archive of %s carries %v, want %v: the manifest has no ffmpeg for this target, so its archive carries no notice", word, names, want)
	}
}

// a windows target's program carries the suffix its system runs, the way the ffmpeg phase does,
// and it sits beside the same three names every other archive carries
func TestTheWindowsArchiveCarriesTheProgramItsOperatingSystemRuns(t *testing.T) {
	repo := t.TempDir()

	writeArchiveSource(t, repo, "windows-amd64", "rdio-scanner.exe", "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeRepoFile(t, repo, "docs/user-guide.html", "the guide for the listener")
	writeArchiveLicence(t, repo, "windows-amd64", "the licence")

	files, err := archiveFiles(repo, releaseTargets[7], defaultOutputPath("windows-amd64"))
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	want := []string{
		"rdio-scanner.exe",
		"admin-guide.html",
		"user-guide.html",
		"FFMPEG.txt",
	}

	if names := fileNames(files); strings.Join(names, " ") != strings.Join(want, " ") {
		t.Fatalf("the windows archive carries %v, want %v", names, want)
	}
}

// the archive carries the executable mode the archiver restores.
func TestTheBinaryInTheArchiveIsExecutable(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeRepoFile(t, repo, "docs/user-guide.html", "the guide for the listener")
	writeArchiveLicence(t, repo, target, "the licence")

	files, err := archiveFiles(repo, releaseTargets[4], defaultOutputPath(target))
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	path := filepath.Join(repo, "rdio-scanner-linux-arm64-v8.0.0.zip")

	if err := writeArchive(path, files); err != nil {
		t.Fatalf("writeArchive: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the archive was not written: %v", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("a standard zip reader cannot open the archive: %v", err)
	}

	for _, f := range reader.File {
		if f.Name == "rdio-scanner" {
			if f.Mode()&0o111 == 0 {
				t.Errorf("the binary in the archive has mode %v, which is not executable", f.Mode())
			}

			continue
		}

		if perm := f.Mode().Perm(); perm != 0o644 {
			t.Errorf("%s in the archive has mode %o, want the 0644 a document carries", f.Name, perm)
		}
	}
}

// The archive is a zip a standard tool opens, and the bytes inside are the bytes it was given.
func TestTheArchiveIsAStandardZipFile(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeRepoFile(t, repo, "docs/user-guide.html", "the guide for the listener")
	writeArchiveLicence(t, repo, target, "the licence")

	files, err := archiveFiles(repo, releaseTargets[4], defaultOutputPath(target))
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	path := filepath.Join(repo, "rdio-scanner-linux-arm64-v8.0.0.zip")

	if err := writeArchive(path, files); err != nil {
		t.Fatalf("writeArchive: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the archive was not written: %v", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("a standard zip reader cannot open the archive: %v", err)
	}

	names := []string{}

	for _, f := range reader.File {
		names = append(names, f.Name)

		if f.Name == "rdio-scanner" {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("cannot open %s in the archive: %v", f.Name, err)
			}

			got, err := io.ReadAll(rc)
			rc.Close()

			if err != nil {
				t.Fatalf("cannot read %s in the archive: %v", f.Name, err)
			}

			if string(got) != "the binary" {
				t.Errorf("the archive holds %q where the binary goes, want the bytes it was given", got)
			}
		}
	}

	// the four names an extracted folder holds, in the order the release wrote them
	want := []string{
		"rdio-scanner",
		"admin-guide.html",
		"user-guide.html",
		"FFMPEG.txt",
	}

	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Errorf("the archive holds %v, want %v", names, want)
	}
}

// fileNames is what an archive carries, in the order it carries it, for a failure that has to say so.
func fileNames(files []archiveFile) []string {
	names := []string{}

	for _, f := range files {
		names = append(names, f.name)
	}

	return names
}

// writeArchiveSource puts one file the archive carries into the tree,
// in the directory the release writes the target into.
func writeArchiveSource(t *testing.T, repo string, target string, name string, contents string) {
	t.Helper()

	dir := filepath.Join(repo, "dist", target)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making %s: %v", dir, err)
	}

	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

// writeRepoFile puts a file into a tree at the name the repository writes it at.
func writeRepoFile(t *testing.T, repo string, path string, contents string) {
	t.Helper()

	dir := filepath.Join(repo, filepath.Dir(path))

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making %s: %v", dir, err)
	}

	if err := os.WriteFile(filepath.Join(repo, path), []byte(contents), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// writeArchiveLicence puts the notice the ffmpeg phase writes into the bundle
// directory the server embeds, under the one name that notice is kept under
// everywhere it is stored.
func writeArchiveLicence(t *testing.T, repo string, target string, contents string) {
	t.Helper()

	dir := filepath.Join(repo, "internal", "infrastructure", "ffmpeg", "bundle", target)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making %s: %v", dir, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "FFMPEG.txt"), []byte(contents), 0o644); err != nil {
		t.Fatalf("writing the licence: %v", err)
	}
}
