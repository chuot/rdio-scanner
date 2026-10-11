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
	"regexp"
	"strings"
	"testing"
)

// One archive per release target. What it carries is the release-archive contract this command carries: the
// binary, the documentation as the files it is written in, the images those files reference, and the
// ffmpeg licence when the target carries an ffmpeg.

// The archive's name is the name the platform document's transcript tells the reader to download, so a
// release that writes a file the document does not name is a release a reader cannot follow.
func TestTheArchiveIsNamedForTheTargetAndTheVersionTheReleaseResolved(t *testing.T) {
	got := archivePath("linux-amd64", "8.0.0+g19042cd")

	want := filepath.Join("dist", "rdio-scanner-linux-amd64-v8.0.0+g19042cd.zip")

	if got != want {
		t.Errorf("the archive is written to %q, want %q", got, want)
	}
}

// The archive carries the documentation as the files it is written in, at the names the repository
// writes them at. A reader who extracts the archive gets the same folder layout the repository has, so
// the relative paths the sources wrote resolve inside the archive.
func TestTheArchiveCarriesTheDocumentationAsTheFilesItIsWrittenIn(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "CHANGELOG.md", "the change log")
	writeRepoFile(t, repo, "docs/platforms/linux.md", "![one](../images/one.png?raw=true)")
	writeRepoFile(t, repo, "docs/webapp.md", "![two](./images/two.png?raw=true)")
	writeRepoFile(t, repo, "docs/faq.md", "the FAQ")
	writeRepoFile(t, repo, "docs/images/one.png", "the first picture")
	writeRepoFile(t, repo, "docs/images/two.png", "the second picture")
	writeArchiveLicence(t, repo, target, "the licence")

	files, err := archiveFiles(repo, releaseTargets[4])
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	want := []string{
		"rdio-scanner",
		"CHANGELOG.md",
		"docs/platforms/linux.md",
		"docs/webapp.md",
		"docs/faq.md",
		"docs/images/one.png",
		"docs/images/two.png",
		"FFMPEG.md",
	}

	if len(files) != len(want) {
		t.Fatalf("the archive carries %d files, want %d", len(files), len(want))
	}

	for i, name := range want {
		if files[i].name != name {
			t.Errorf("the archive carries %q at %d, want %q", files[i].name, i, name)
		}
	}

	if string(files[7].body) != "the licence" {
		t.Errorf("the archive carries %q as its licence, want the text the ffmpeg phase wrote", files[7].body)
	}
}

// The platform documents say `../images` and the web app guide says `./images`, and both reach the same
// folder. An image is resolved against the directory of the document it is written in, so an archive
// that resolved every path against the repository root would carry the web app guide's pictures twice
// and none of the platform page's.
func TestTheImagesAreResolvedAgainstTheDocumentEachIsWrittenIn(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "CHANGELOG.md", "the change log")
	writeRepoFile(t, repo, "docs/platforms/linux.md", "![one](../images/one.png?raw=true)")
	writeRepoFile(t, repo, "docs/webapp.md", "![two](./images/two.png?raw=true)")
	writeRepoFile(t, repo, "docs/faq.md", "the FAQ")
	writeRepoFile(t, repo, "docs/images/one.png", "the first picture")
	writeRepoFile(t, repo, "docs/images/two.png", "the second picture")
	writeArchiveLicence(t, repo, target, "the licence")

	files, err := archiveFiles(repo, releaseTargets[4])
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	for _, want := range []struct {
		name string
		body string
	}{
		{"docs/images/one.png", "the first picture"},
		{"docs/images/two.png", "the second picture"},
	} {
		found := false

		for _, f := range files {
			if f.name == want.name {
				found = true

				if string(f.body) != want.body {
					t.Errorf("the archive carries %q as %q, want the picture the document points at", f.name, f.body)
				}
			}
		}

		if !found {
			t.Errorf("the archive does not carry %s, which the documentation points at", want.name)
		}
	}
}

// The archive carries the images the documentation references, and no image it does not. A folder of
// pictures copied wholesale puts files in every archive that no document asks for.
func TestTheArchiveCarriesNoImageTheDocumentationDoesNotReference(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "CHANGELOG.md", "the change log")
	writeRepoFile(t, repo, "docs/platforms/linux.md", "![one](../images/one.png?raw=true)")
	writeRepoFile(t, repo, "docs/webapp.md", "![two](./images/two.png?raw=true)")
	writeRepoFile(t, repo, "docs/faq.md", "the FAQ")
	writeRepoFile(t, repo, "docs/images/one.png", "the first picture")
	writeRepoFile(t, repo, "docs/images/two.png", "the second picture")
	writeRepoFile(t, repo, "docs/images/unused.png", "a picture no document points at")
	writeArchiveLicence(t, repo, target, "the licence")

	files, err := archiveFiles(repo, releaseTargets[4])
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	for _, f := range files {
		if f.name == "docs/images/unused.png" {
			t.Errorf("the archive carries %s, which no document references", f.name)
		}
	}
}

// An image the documentation points at that is not in the tree is a broken document, and a release that
// quietly left it out is a release that shipped a broken document.
func TestAnImageTheDocumentationPointsAtThatIsNotInTheTreeStopsTheRelease(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "CHANGELOG.md", "the change log")
	writeRepoFile(t, repo, "docs/platforms/linux.md", "![gone](../images/gone.png?raw=true)")
	writeRepoFile(t, repo, "docs/webapp.md", "the web app guide")
	writeRepoFile(t, repo, "docs/faq.md", "the FAQ")

	_, err := archiveFiles(repo, releaseTargets[4])
	if err == nil {
		t.Fatal("the release accepted a document pointing at a picture that is not in the tree")
	}

	if !strings.Contains(err.Error(), "docs/images/gone.png") {
		t.Errorf("the failure %q does not name the picture the document points at", err.Error())
	}
}

// A target this project ships with the ffmpeg its host installs has no licence to carry, because there
// is no ffmpeg inside its binary.
func TestAnUncoveredTargetCarriesNoFfmpegLicence(t *testing.T) {
	repo := t.TempDir()

	writeArchiveSource(t, repo, "macos-arm64", "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "CHANGELOG.md", "the change log")
	writeRepoFile(t, repo, "docs/platforms/macos.md", "the macos page")
	writeRepoFile(t, repo, "docs/webapp.md", "the web app guide")
	writeRepoFile(t, repo, "docs/faq.md", "the FAQ")
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

	if len(files) != 5 {
		t.Errorf("the archive carries %d files, want the binary and the four documentation sources", len(files))
	}
}

// A windows target's program has the suffix its operating system puts on a program it runs, which is the
// rule the ffmpeg phase already follows.
func TestTheWindowsArchiveCarriesTheProgramItsOperatingSystemRuns(t *testing.T) {
	repo := t.TempDir()

	writeArchiveSource(t, repo, "windows-amd64", "rdio-scanner.exe", "the binary")
	writeRepoFile(t, repo, "CHANGELOG.md", "the change log")
	writeRepoFile(t, repo, "docs/platforms/windows.md", "the windows page")
	writeRepoFile(t, repo, "docs/webapp.md", "the web app guide")
	writeRepoFile(t, repo, "docs/faq.md", "the FAQ")
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
	writeRepoFile(t, repo, "CHANGELOG.md", "the change log")
	writeRepoFile(t, repo, "docs/platforms/linux.md", "![one](../images/one.png?raw=true)")
	writeRepoFile(t, repo, "docs/webapp.md", "the web app guide")
	writeRepoFile(t, repo, "docs/faq.md", "the FAQ")
	writeRepoFile(t, repo, "docs/images/one.png", "the first picture")
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
	writeRepoFile(t, repo, "CHANGELOG.md", "the change log")
	writeRepoFile(t, repo, "docs/platforms/linux.md", "![one](../images/one.png?raw=true)")
	writeRepoFile(t, repo, "docs/webapp.md", "![two](./images/two.png?raw=true)")
	writeRepoFile(t, repo, "docs/faq.md", "the FAQ")
	writeRepoFile(t, repo, "docs/images/one.png", "the first picture")
	writeRepoFile(t, repo, "docs/images/two.png", "the second picture")
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

	want := []string{
		"rdio-scanner",
		"CHANGELOG.md",
		"docs/platforms/linux.md",
		"docs/webapp.md",
		"docs/faq.md",
		"docs/images/one.png",
		"docs/images/two.png",
		"FFMPEG.md",
	}

	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Errorf("the archive holds %v, want %v", names, want)
	}
}

// The archive carries every picture the real documentation points at. The references are read from the
// sources themselves, so the test asks the question a reader asks after extracting the archive: does
// every image in the documents I opened resolve to a file I have.
func TestTheArchiveCarriesEveryImageTheRealDocumentationReferences(t *testing.T) {
	real, err := repositoryRoot()
	if err != nil {
		t.Fatalf("the tool cannot find the repository it is releasing: %v", err)
	}

	tr := releaseTargets[4]

	sources := []string{
		"CHANGELOG.md",
		"docs/platforms/" + tr.name + ".md",
		"docs/webapp.md",
		"docs/faq.md",
	}

	// The references the documents wrote, resolved against the directory of the document each is in.
	// This is the reader's arithmetic, not the tool's, so a renderer that resolved paths the other way
	// fails here.
	referenced := map[string]bool{}

	for _, name := range sources {
		text, err := os.ReadFile(filepath.Join(real, name))
		if err != nil {
			t.Fatalf("the release has no %s: %v", name, err)
		}

		for _, ref := range imageReferences(string(text)) {
			referenced[filepath.Clean(filepath.Join(filepath.Dir(name), ref))] = true
		}
	}

	// The test builds the repository it asserts against, as every other test in this file does: the
	// documentation as it is written, the pictures that documentation points at, and a binary. The
	// subject here is the documentation, so the binary is bytes and no build has to have run. Copying
	// each picture is the check that the documentation points at a file the repository has; a reference
	// to a file that is not in the repository fails here.
	repo := t.TempDir()

	for _, name := range sources {
		copyArchiveSource(t, real, repo, name)
	}

	for path := range referenced {
		copyArchiveSource(t, real, repo, path)
	}

	// The target carries an ffmpeg, so the archive carries the licence the ffmpeg phase wrote from the
	// manifest. The licence is a build artifact, not a file the repository keeps, so the test writes it
	// as it writes the binary: the test is about the documentation, and the licence is bytes the archive
	// carries.
	writeRepoFile(t, repo, filepath.Join("internal", "infrastructure", "ffmpeg", "bundle", tr.targetWord(), "LICENCE.md"), "the ffmpeg licence")

	writeArchiveSource(t, repo, tr.targetWord(), "rdio-scanner", "the binary")

	files, err := archiveFiles(repo, tr)
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	carried := map[string]bool{}

	for _, f := range files {
		carried[f.name] = true
	}

	for _, name := range sources {
		if !carried[name] {
			t.Errorf("the archive does not carry %s, which is the documentation as it is written", name)
		}
	}

	for path := range referenced {
		if !carried[path] {
			t.Errorf("the archive does not carry %s, which the documentation points at", path)
		}
	}

	// Every picture the real documentation points at is written with GitHub's raw suffix, so the
	// archive's copy of each source has to be written without it. A reader who extracts the archive and
	// opens a document in a local editor gets a page whose every picture is broken if the suffix
	// survives.
	for _, f := range files {
		if strings.HasSuffix(f.name, ".md") && strings.Contains(string(f.body), "?raw=true") {
			t.Errorf("the archive carries %s with %q, which no folder on disk is named", f.name, "?raw=true")
		}
	}

	if len(referenced) != 33 {
		t.Errorf("the documentation points at %d pictures, want the 33 the sources reference", len(referenced))
	}
}

// `?raw=true` is the query GitHub uses to serve a raw file, and on disk it is part of the file name. A
// reader who extracts the archive and opens a document in a local editor gets a page whose every picture
// is broken, because the path the document asks for is a path no folder has. The archive carries the
// sources with the suffix removed, so the path the reader's editor resolves is the path the archive
// filled. The repository's own files are not touched: the change is in the bytes written into the
// archive.
func TestTheArchiveCarriesTheImagesWhereTheReadersEditorCanFindThem(t *testing.T) {
	repo := t.TempDir()

	target := releaseTargets[4].targetWord()

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary")
	writeRepoFile(t, repo, "CHANGELOG.md", "the change log")
	writeRepoFile(t, repo, "docs/platforms/linux.md", "![one](../images/one.png?raw=true)")
	writeRepoFile(t, repo, "docs/webapp.md", "![two](./images/two.png?raw=true)")
	writeRepoFile(t, repo, "docs/faq.md", "the FAQ")
	writeRepoFile(t, repo, "docs/images/one.png", "the first picture")
	writeRepoFile(t, repo, "docs/images/two.png", "the second picture")
	writeArchiveLicence(t, repo, target, "the licence")

	files, err := archiveFiles(repo, releaseTargets[4])
	if err != nil {
		t.Fatalf("archiveFiles: %v", err)
	}

	for _, name := range []string{"docs/platforms/linux.md", "docs/webapp.md"} {
		for _, f := range files {
			if f.name != name {
				continue
			}

			if strings.Contains(string(f.body), "?raw=true") {
				t.Errorf("the archive carries %s with %q, which no folder on disk is named", name, "?raw=true")
			}

			if !strings.Contains(string(f.body), "](../images/one.png)") && !strings.Contains(string(f.body), "](./images/two.png)") {
				t.Errorf("the archive carries %s without the path the picture it filled is at: %q", name, f.body)
			}
		}
	}

	// The repository's own file is not rewritten: the change is in the archive, not in the tree.
	body, err := os.ReadFile(filepath.Join(repo, "docs/platforms/linux.md"))
	if err != nil {
		t.Fatalf("reading the source: %v", err)
	}

	if !strings.Contains(string(body), "?raw=true") {
		t.Errorf("the tool rewrote %s in the tree, and the tree is not the archive", "docs/platforms/linux.md")
	}
}

// imageReferences is the picture path a markdown image reference names, with the suffix a GitHub raw
// link carries stripped. A reference that names an address is not a file.
func imageReferences(text string) []string {
	var refs []string

	for _, m := range regexp.MustCompile(`!\[[^]]*\]\(([^)]+)\)`).FindAllStringSubmatch(text, -1) {
		if strings.Contains(m[1], "://") {
			continue
		}

		refs = append(refs, strings.TrimSuffix(m[1], "?raw=true"))
	}

	return refs
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

// copyArchiveSource copies one file the real repository has into the tree the test asserts against, at
// the name the repository writes it at. A file the documentation points at that the repository does not
// have fails here, which is the reader's question: does every image in the documents resolve to a file
// in the archive.
func copyArchiveSource(t *testing.T, real string, repo string, path string) {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(real, path))
	if err != nil {
		t.Fatalf("the real repository has no %s, which the documentation points at: %v", path, err)
	}

	writeRepoFile(t, repo, path, string(body))
}

// writeRepoFile puts a documentation file into a tree at the name the repository writes it at.
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
