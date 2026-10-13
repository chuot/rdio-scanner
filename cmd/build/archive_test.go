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

// One archive per release target. What it carries is the release-archive contract this command carries:
// the program, the administrator guide that operates it, and the licence of the ffmpeg the build embeds.
//
// The guide carries its own pictures, its own stylesheet, and its own mark, so the archive carries no
// pictures and needs no network for the reader to read it. It carries no markdown: the guide absorbed
// the pages the archive used to carry, and a markdown file beside the guide is a document the guide
// already says.

// The archive's name is the name the guide's installation chapter tells the reader to download, so a
// release that writes a file the guide does not name is a release a reader cannot follow.
func TestTheArchiveIsNamedForTheTargetAndTheVersionTheReleaseResolved(t *testing.T) {
	got := archivePath("linux-amd64", "8.0.0+g19042cd")

	want := filepath.Join("dist", "rdio-scanner-linux-amd64-v8.0.0+g19042cd.zip")

	if got != want {
		t.Errorf("the archive is written to %q, want %q", got, want)
	}
}

// A release archive that stamps every entry with the moment the archive was written says nothing about
// when the files inside it were written: the binary the run compiled, the guide the repository has, and
// the licence the ffmpeg phase wrote are three files with three histories, and a reader who extracts
// them and finds three identical timestamps has learned nothing from the timestamps. The archive is
// required to state when the files it carries were written.
func TestEveryArchiveEntryCarriesTheTimeOfTheFileItCarries(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	program := defaultOutputPath(target)
	licence := filepath.Join("internal", "infrastructure", "ffmpeg", "bundle", target, "LICENCE.md")

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary the run compiled")
	writeRepoFile(t, repo, guideSource, "the guide")
	writeArchiveLicence(t, repo, target, "the licence")

	// Three files, three histories. The times are distinct, in the range a zip archive records, and on
	// even seconds, which is the resolution the archive's own timestamp field carries.
	want := []struct {
		name     string
		path     string
		modified time.Time
	}{
		{"rdio-scanner", program, time.Date(2026, 3, 4, 5, 6, 8, 0, time.Local)},
		{guideSource, guideSource, time.Date(2025, 1, 2, 3, 4, 6, 0, time.Local)},
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

	// The times have to survive the archive: a reader's `unzip` is the consumer this rule is about, and
	// a time the writer does not put in the header is a time the reader never sees.
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

	// The program is a program the reader runs: the mode is the fact the archive has to carry for a
	// standard tool to restore an executable binary.
	if mode := reader.File[0].Mode().Perm(); mode != 0o755 {
		t.Errorf("the archive's program entry has mode %o, want 0755", mode)
	}
}

// The archive is the program and the one document that operates it, at the names the repository writes
// them at: a reader who extracts the archive finds the guide at the path the guide itself states, and
// finds the ffmpeg licence under the name a reader looks for.
func TestTheArchiveCarriesTheProgramTheGuideAndTheLicenceOfTheFfmpegItCarries(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeArchiveLicence(t, repo, target, "the licence")

	files, err := archiveFiles(repo, releaseTargets[4], defaultOutputPath(target))
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	want := []string{
		"rdio-scanner",
		"docs/admin-guide.html",
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

	if string(files[2].body) != "the licence" {
		t.Errorf("the archive carries %q as its licence, want the text the ffmpeg phase wrote", files[2].body)
	}
}

// A release archive carries no markdown file. The change log and the scanner webapp guide are the
// project's own documents and stay in the repository; the guide the archive carries absorbed what an
// administrator needs from the rest of them, so a markdown file in the archive is a document the
// reader is told to open and does not have.
func TestTheArchiveCarriesNoMarkdownFile(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeRepoFile(t, repo, "CHANGELOG.md", "the change log")
	writeRepoFile(t, repo, "docs/faq.md", "the FAQ")
	writeRepoFile(t, repo, "docs/platforms/linux.md", "the platform page")
	writeRepoFile(t, repo, "docs/webapp.md", "the scanner webapp guide")
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

// The archive carries the binary the run compiled. Reading the program from a path the tool chose for
// itself, instead of from the path the run wrote to, is how a release ships a binary the release never
// built: what stands at that path is whatever the last build, or the last hand that typed there, left
// behind.
func TestTheArchiveCarriesTheBinaryTheRunWroteAndNotTheFileStandingAtThePathItReadsFrom(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	// The file standing at the path the archive used to read from, before the run wrote anywhere.
	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary the run never compiled")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
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

// The guide is read where nothing else is: an extracted archive on a machine with no network. The copy
// in the archive is the file the repository has, pictures and all, so the reader who opens it offline
// gets every picture it shows.
func TestTheGuideInTheArchiveCarriesEveryPictureInsideItself(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	guide := `<figure data-shot="section-systems"><img src="data:image/png;base64,aW1hZ2U=" alt="The Systems section."><figcaption>The Systems section.</figcaption></figure>`

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", guide)
	writeArchiveLicence(t, repo, target, "the licence")

	files, err := archiveFiles(repo, releaseTargets[4], defaultOutputPath(target))
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	for _, f := range files {
		if f.name != "docs/admin-guide.html" {
			continue
		}

		if !strings.Contains(string(f.body), `src="data:image/png;base64,`) {
			t.Errorf("the guide in the archive carries no picture inside itself: %q", f.body)
		}

		if strings.Contains(string(f.body), "?raw=true") {
			t.Errorf("the guide in the archive asks for %q, which no folder on disk is named", "?raw=true")
		}
	}
}

// The guide is the repository's own document, written by hand. The release carries it as it is written:
// a tool that rewrote the guide on the way into the archive would be a tool that published a document
// nobody read.
func TestTheGuideInTheArchiveIsTheGuideTheRepositoryHas(t *testing.T) {
	real, err := repositoryRoot()
	if err != nil {
		t.Fatalf("the tool cannot find the repository it is releasing: %v", err)
	}

	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	body, err := os.ReadFile(filepath.Join(real, "docs", "admin-guide.html"))
	if err != nil {
		t.Fatalf("the release has no guide to carry: %v", err)
	}

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", string(body))
	writeArchiveLicence(t, repo, target, "the ffmpeg licence")

	files, err := archiveFiles(repo, releaseTargets[4], defaultOutputPath(target))
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	carried := false

	for _, f := range files {
		if f.name != "docs/admin-guide.html" {
			continue
		}

		carried = true

		if string(f.body) != string(body) {
			t.Errorf("the guide in the archive is not the guide the repository has: %d bytes carried of %d written", len(f.body), len(body))
		}
	}

	if !carried {
		t.Errorf("the archive does not carry the guide")
	}
}

// A release without the guide is a release whose reader has the program and nothing that operates it.
func TestAGuideThatIsNotInTheTreeStopsTheRelease(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeArchiveLicence(t, repo, target, "the licence")

	_, err := archiveFiles(repo, releaseTargets[4], defaultOutputPath(target))
	if err == nil {
		t.Fatal("the release accepted an archive with no guide")
	}

	if !strings.Contains(err.Error(), "docs/admin-guide.html") {
		t.Errorf("the failure %q does not name the guide the release cannot carry", err.Error())
	}
}

// Every release target carries an ffmpeg inside its binary, so every release archive carries the licence
// of the one it carries: the five this build compiles, and the three whose ffmpeg binaries the maintainer
// compiled
// on the platform the target runs on. An archive that quietly left the licence out shipped the terms of a
// component it embedded with none of the terms.
func TestEveryReleaseTargetCarriesTheLicenceOfTheFfmpegItCarries(t *testing.T) {
	for _, tr := range releaseTargets {
		target := tr.targetWord()

		repo := t.TempDir()

		writeArchiveSource(t, repo, target, "rdio-scanner"+programSuffix(target), "the binary")
		writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
		writeArchiveLicence(t, repo, target, "the licence of the ffmpeg "+target+" carries")

		files, err := archiveFiles(repo, tr, defaultOutputPath(tr.targetWord()))
		if err != nil {
			t.Fatalf("%s: archiveFiles: %v", target, err)
		}

		names := fileNames(files)

		if len(names) != 3 {
			t.Fatalf("%s: the archive carries %d files, want the program, the guide and the licence: %v", target, len(names), names)
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

// A supplied target's archive says what the ffmpeg inside its binary is, in the text the ffmpeg phase
// writes for binaries it adopted and did not compile in a container: the licence terms, the source release
// the manifest pins, the configure recipe, the audio-only posture, the host that compiled them, the command
// that built them, and the codecs the run recorded. The archive carries that text under the name a reader
// looks for, and carries no markdown file beside it.
func TestASuppliedTargetsArchiveCarriesWhatItsFfmpegIs(t *testing.T) {
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
	writeRepoFile(t, repo, "docs/faq.md", "the FAQ")
	writeArchiveLicence(t, repo, "macos-arm64", licence)

	files, err := archiveFiles(repo, tr, defaultOutputPath(tr.targetWord()))
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	names := fileNames(files)

	if strings.Join(names, " ") != strings.Join([]string{"rdio-scanner", "docs/admin-guide.html", "FFMPEG.txt"}, " ") {
		t.Fatalf("the archive carries %v, want the program, the guide and the licence", names)
	}

	for _, f := range files {
		if strings.HasSuffix(f.name, ".md") {
			t.Errorf("the archive carries %s, a markdown file", f.name)
		}
	}

	carried := string(files[2].body)

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

// A target whose ffmpeg the build compiles has the licence the ffmpeg phase wrote. A release that
// quietly left it out is a release that shipped the terms of a component it embedded with none of the
// terms.
func TestAMissingFfmpegLicenceStopsTheRelease(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")

	_, err := archiveFiles(repo, releaseTargets[4], defaultOutputPath(target))
	if err == nil {
		t.Fatal("the release accepted an ffmpeg with no licence to carry")
	}

	if !strings.Contains(err.Error(), "ffmpeg licence") {
		t.Errorf("the failure %q does not name the ffmpeg licence the release cannot carry", err.Error())
	}
}

// A windows target's program has the suffix its operating system puts on a program it runs, which is the
// rule the ffmpeg phase already follows.
func TestTheWindowsArchiveCarriesTheProgramItsOperatingSystemRuns(t *testing.T) {
	repo := t.TempDir()

	writeArchiveSource(t, repo, "windows-amd64", "rdio-scanner.exe", "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeArchiveLicence(t, repo, "windows-amd64", "the licence")

	files, err := archiveFiles(repo, releaseTargets[7], defaultOutputPath("windows-amd64"))
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	if files[0].name != "rdio-scanner.exe" {
		t.Errorf("the archive carries %q, want rdio-scanner.exe", files[0].name)
	}
}

// Info-Zip restores the mode it stored, so a release archive has to carry the mode a program needs. A
// binary that arrives not executable is a release a reader cannot run.
func TestTheBinaryInTheArchiveIsExecutable(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
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

		if f.Mode()&0o111 != 0 {
			t.Errorf("%s in the archive has mode %v, which is executable", f.Name, f.Mode())
		}
	}
}

// The archive is a zip a standard tool opens, and the bytes inside are the bytes it was given.
func TestTheArchiveIsAStandardZipFile(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
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

	want := []string{
		"rdio-scanner",
		"docs/admin-guide.html",
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

// writeArchiveSource puts one file a release archive carries into the tree, in the directory the release
// writes the target into.
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

// writeArchiveLicence puts the licence the ffmpeg phase writes into the bundle directory the server
// embeds. The phase writes it from the manifest the ffmpeg build records; the archive carries it under
// the name a reader looks for, which is not the name the bundle directory uses.
func writeArchiveLicence(t *testing.T, repo string, target string, contents string) {
	t.Helper()

	dir := filepath.Join(repo, "internal", "infrastructure", "ffmpeg", "bundle", target)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making %s: %v", dir, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "LICENCE.md"), []byte(contents), 0o644); err != nil {
		t.Fatalf("writing the licence: %v", err)
	}
}
