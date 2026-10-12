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

// The archive is the program and the one document that operates it, at the names the repository writes
// them at: a reader who extracts the archive finds the guide at the path the guide itself states, and
// finds the ffmpeg licence under the name a reader looks for.
func TestTheArchiveCarriesTheProgramTheGuideAndTheLicenceOfTheFfmpegItCarries(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeArchiveLicence(t, repo, target, "the licence")

	files, err := archiveFiles(repo, releaseTargets[4])
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

	files, err := archiveFiles(repo, releaseTargets[4])
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	for _, f := range files {
		if strings.HasSuffix(f.name, ".md") {
			t.Errorf("the archive carries %s, a markdown file", f.name)
		}
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

	files, err := archiveFiles(repo, releaseTargets[4])
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

	files, err := archiveFiles(repo, releaseTargets[4])
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

	_, err := archiveFiles(repo, releaseTargets[4])
	if err == nil {
		t.Fatal("the release accepted an archive with no guide")
	}

	if !strings.Contains(err.Error(), "docs/admin-guide.html") {
		t.Errorf("the failure %q does not name the guide the release cannot carry", err.Error())
	}
}

// A target this project ships with the ffmpeg its host installs has no licence to carry, because there
// is no ffmpeg inside its binary.
func TestAnUncoveredTargetCarriesNoFfmpegLicence(t *testing.T) {
	repo := t.TempDir()

	writeArchiveSource(t, repo, "macos-arm64", "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeArchiveLicence(t, repo, "macos-arm64", "a licence that should not be in the archive")

	files, err := archiveFiles(repo, releaseTargets[6])
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	for _, f := range files {
		if strings.HasPrefix(f.name, "FFMPEG") {
			t.Errorf("the archive for macos-arm64 carries %s, and no ffmpeg is compiled for it", f.name)
		}
	}

	if len(files) != 2 {
		t.Errorf("the archive carries %d files, want the program and the guide: %v", len(files), fileNames(files))
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

	_, err := archiveFiles(repo, releaseTargets[4])
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

	files, err := archiveFiles(repo, releaseTargets[7])
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

	files, err := archiveFiles(repo, releaseTargets[4])
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

	files, err := archiveFiles(repo, releaseTargets[4])
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
