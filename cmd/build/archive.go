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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// archivePath is where a target's release archive is written. The name is the name the platform
// document's transcript tells the reader to download, so a release that writes a file the document does
// not name is a release a reader cannot follow.
func archivePath(target string, version string) string {
	return filepath.Join("dist", fmt.Sprintf("rdio-scanner-%s-v%s.zip", target, version))
}

// archiveVersionOf reads the version out of a release archive's name: the name is
// `rdio-scanner-<target>-v<version>.zip`, and the target is the part whose shape the tool knows, so the
// separator between the two is found by the target's shape and not by the last hyphen a prerelease name
// happens to carry. A name the tool did not write is not an archive of this release, and a file the
// release folder happens to hold is left to the operator who put it there.
func archiveVersionOf(name string) (string, bool) {
	rest, found := strings.CutSuffix(strings.TrimPrefix(name, "rdio-scanner-"), ".zip")
	if !found {
		return "", false
	}

	for i := 0; i < len(rest); i++ {
		if !strings.HasPrefix(rest[i:], "-v") {
			continue
		}

		if !isTarget(rest[:i]) {
			continue
		}

		return rest[i+2:], true
	}

	return "", false
}

// pruneReleaseFolder removes the release archives of the versions this run did not release. The release
// folder is where the archives sit side by side until they are uploaded by hand, and the archives of an
// older version sitting next to the new ones is how the wrong file gets uploaded. The version a run
// resolved is derived from the declared core and git, so the run knows exactly which archive names are
// current, and names each removal it makes.
//
// A file in the release folder whose name is not an archive name is left to the operator who put it
// there.
func pruneReleaseFolder(repo string, version string) ([]string, error) {
	dir := filepath.Join(repo, "dist")

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("the build cannot read %s: %w", dir, err)
	}

	var removed []string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name, found := archiveVersionOf(entry.Name())
		if !found || name == version {
			continue
		}

		path := filepath.Join(dir, entry.Name())

		if err := os.RemoveAll(path); err != nil {
			return nil, fmt.Errorf("the release run cannot remove %s: %w", path, err)
		}

		removed = append(removed, filepath.Join("dist", entry.Name()))
	}

	return removed, nil
}

// archiveFile is one file in a release archive, with the time the file it carries was written. The
// archive is required to state when the files it carries were written, and stamping every entry with the
// moment the archive was written states nothing: the binary the run compiled, the guide the repository
// has, and the licence the ffmpeg phase wrote are three files with three histories.
type archiveFile struct {
	name     string
	mode     os.FileMode
	modified time.Time
	body     []byte
}

// readArchiveSource reads one file the archive carries, at the path in the tree it reads it from, with
// the time that file was written: the time the source file carries is the only time that says when the
// file was written.
func readArchiveSource(repo string, path string, name string, mode os.FileMode) (archiveFile, error) {
	full := filepath.Join(repo, path)

	body, err := os.ReadFile(full)
	if err != nil {
		return archiveFile{}, err
	}

	info, err := os.Stat(full)
	if err != nil {
		return archiveFile{}, err
	}

	return archiveFile{name: name, mode: mode, modified: info.ModTime(), body: body}, nil
}

// guideSource is the document a release archive carries: the administrator guide, at the path the guide
// itself states. It is the whole of the documentation a deployment ships with — the platform pages, the
// container guide, the log guide, the questions, and the licence and access position are chapters of it —
// and it carries its own pictures, its stylesheet, and the project's mark, so the archive needs no
// picture folder and the reader needs no network.
const guideSource = "docs/admin-guide.html"

// archiveFiles reads what a target's archive carries: the program the run wrote to `output`, the guide
// that operates it, and the licence of the ffmpeg the build embeds.
//
// The program is read from the path the run wrote it to, and not from a path the archive chose for
// itself: a release that reads a path it picked ships whatever was standing there, which is a binary the
// release did not build.
//
// Every target the release matrix ships carries an ffmpeg inside its binary, so every one of these
// archives carries a licence: the five this build compiles carry the notice naming the recipe, the source
// digest and the command that produced the binaries, and the three whose ffmpeg binaries were compiled on
// the platform the target runs on carry the notice naming the host that compiled them, the command that
// built them there, and the codecs the run that adopted them recorded. The licence is the
// text the ffmpeg phase wrote from the manifest, and the archive carries it under the name a reader looks
// for, not the name the bundle directory uses.
//
// A target the manifest has no record for carries no licence, because it carries no ffmpeg: that is a
// hand-named target outside the matrix, which is what a developer builds to try something this project
// does not release.
//
// The archive's layout is the repository's own layout, so the path the guide states for itself resolves
// inside the archive. The tool renders nothing, converts nothing, inlines nothing, and rewrites nothing:
// the guide the archive carries is the guide the repository has, byte for byte.
func archiveFiles(repo string, tr releaseTarget, output string) ([]archiveFile, error) {
	name := "rdio-scanner" + programSuffix(tr.targetWord())

	// The binary is a program the reader runs, and the guide is a document they read. Info-Zip
	// restores the mode it finds in the archive, so the mode has to be in the archive.
	program, err := readArchiveSource(repo, output, name, 0o755)
	if err != nil {
		return nil, fmt.Errorf("the release has no %s for %s: %w", name, tr.targetWord(), err)
	}

	guide, err := readArchiveSource(repo, guideSource, guideSource, 0o644)
	if err != nil {
		return nil, fmt.Errorf("the release has no %s for %s: %w", guideSource, tr.targetWord(), err)
	}

	files := []archiveFile{program, guide}

	if _, ok := bundle.ReadManifest().Targets[tr.targetWord()]; ok {
		licence, err := readArchiveSource(repo, filepath.Join("internal", "infrastructure", "ffmpeg", "bundle", tr.targetWord(), "LICENCE.md"), "FFMPEG.txt", 0o644)
		if err != nil {
			return nil, fmt.Errorf("the release has no ffmpeg licence for %s: %w", tr.targetWord(), err)
		}

		files = append(files, licence)
	}

	return files, nil
}

// writeArchive writes the files as a zip a standard tool opens.
func writeArchive(path string, files []archiveFile) error {
	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("the release cannot write %s: %w", path, err)
	}

	defer out.Close()

	writer := zip.NewWriter(out)

	for _, f := range files {
		header := &zip.FileHeader{
			Name:     f.name,
			Method:   zip.Deflate,
			Modified: f.modified,
		}

		// The entry carries the time of the file it carries. An archive whose every entry is stamped
		// with the moment it was written tells a reader who extracts it nothing about when the files
		// inside it were written, and the three files here have three histories.

		// The mode has to be in the archive, because Info-Zip restores the mode it finds there and a
		// binary that arrives not executable is a release a reader cannot run. SetMode is the supported
		// way: it puts the permissions in the high half of the external attributes and the Unix creator
		// byte that tells a reader to look there.
		header.SetMode(f.mode)

		w, err := writer.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("the archive cannot carry %s: %w", f.name, err)
		}

		if _, err := w.Write(f.body); err != nil {
			return fmt.Errorf("the archive cannot carry %s: %w", f.name, err)
		}
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("the archive cannot be closed: %w", err)
	}

	return nil
}
