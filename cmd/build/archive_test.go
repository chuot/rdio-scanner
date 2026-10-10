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

// One archive per release target. What it carries is the contract in
// docs/specs/FEAT-013-go-build-retires-the-makefile/contract-release-archive.md: the binary, the
// document, and the ffmpeg licence when the target carries an ffmpeg.

// The archive's name is the name the platform document's transcript tells the reader to download, so a
// release that writes a file the document does not name is a release a reader cannot follow.
func TestTheArchiveIsNamedForTheTargetAndTheVersionTheReleaseResolved(t *testing.T) {
	got := archivePath("linux-amd64", "8.0.0+g19042cd")

	want := filepath.Join("dist", "rdio-scanner-linux-amd64-v8.0.0+g19042cd.zip")

	if got != want {
		t.Errorf("the archive is written to %q, want %q", got, want)
	}
}

// The archive carries the binary, the document, and the licence the ffmpeg phase wrote from the
// manifest. The licence is in the archive under the name a reader looks for, not the name the bundle
// directory uses.
func TestTheArchiveCarriesTheBinaryTheDocumentAndTheFfmpegLicence(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeArchiveSource(t, repo, target, "rdio-scanner.html", "the document")
	writeArchiveLicence(t, repo, target, "the licence")

	files, err := archiveFiles(repo, releaseTargets[4])
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	want := []string{"rdio-scanner", "rdio-scanner.html", "FFMPEG.md"}

	if len(files) != len(want) {
		t.Fatalf("the archive carries %d files, want %d", len(files), len(want))
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

// A target this project ships with the ffmpeg its host installs has no licence to carry, because there
// is no ffmpeg inside its binary.
func TestAnUncoveredTargetCarriesNoFfmpegLicence(t *testing.T) {
	repo := t.TempDir()

	writeArchiveSource(t, repo, "macos-arm64", "rdio-scanner", "the binary")
	writeArchiveSource(t, repo, "macos-arm64", "rdio-scanner.html", "the document")
	writeArchiveLicence(t, repo, "macos-arm64", "a licence that should not be in the archive")

	files, err := archiveFiles(repo, releaseTargets[6])
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	for _, f := range files {
		if f.name == "FFMPEG.md" {
			t.Errorf("the archive for macos-arm64 carries the ffmpeg licence, and no ffmpeg is compiled for it")
		}
	}

	if len(files) != 2 {
		t.Errorf("the archive carries %d files, want the binary and the document", len(files))
	}
}

// A windows target's program has the suffix its operating system puts on a program it runs, which is the
// rule the ffmpeg phase already follows.
func TestTheWindowsArchiveCarriesTheProgramItsOperatingSystemRuns(t *testing.T) {
	repo := t.TempDir()

	writeArchiveSource(t, repo, "windows-amd64", "rdio-scanner.exe", "the binary")
	writeArchiveSource(t, repo, "windows-amd64", "rdio-scanner.html", "the document")
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
	writeArchiveSource(t, repo, target, "rdio-scanner.html", "the document")
	writeArchiveLicence(t, repo, target, "the licence")

	files, err := archiveFiles(repo, releaseTargets[4])
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	path := filepath.Join(repo, "rdio-scanner-linux-amd64-v8.0.0.zip")

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
		switch f.Name {
		case "rdio-scanner":
			if f.Mode()&0o111 == 0 {
				t.Errorf("the binary in the archive has mode %v, which is not executable", f.Mode())
			}

		case "rdio-scanner.html", "FFMPEG.md":
			if f.Mode()&0o111 != 0 {
				t.Errorf("%s in the archive has mode %v, which is executable", f.Name, f.Mode())
			}
		}
	}
}

// The archive is a zip a standard tool opens, and the bytes inside are the bytes it was given.
func TestTheArchiveIsAStandardZipFile(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeArchiveSource(t, repo, target, "rdio-scanner.html", "the document")
	writeArchiveLicence(t, repo, target, "the licence")

	files, err := archiveFiles(repo, releaseTargets[4])
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	path := filepath.Join(repo, "rdio-scanner-linux-amd64-v8.0.0.zip")

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

	if strings.Join(names, " ") != "rdio-scanner rdio-scanner.html FFMPEG.md" {
		t.Errorf("the archive holds %v, want the binary, the document, and the licence", names)
	}
}

// writeArchiveSource puts one file a release archive carries into a tree, in the directory the release
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

// writeArchiveLicence puts the licence the ffmpeg phase writes into the bundle directory the server
// embeds.
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
