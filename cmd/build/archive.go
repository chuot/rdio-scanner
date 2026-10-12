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

	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// archivePath is where a target's release archive is written. The name is the name the platform
// document's transcript tells the reader to download, so a release that writes a file the document does
// not name is a release a reader cannot follow.
func archivePath(target string, version string) string {
	return filepath.Join("dist", fmt.Sprintf("rdio-scanner-%s-v%s.zip", target, version))
}

// archiveFile is one file in a release archive.
type archiveFile struct {
	name string
	mode os.FileMode
	body []byte
}

// guideSource is the document a release archive carries: the administrator guide, at the path the guide
// itself states. It is the whole of the documentation a deployment ships with — the platform pages, the
// container guide, the log guide, the questions, and the licence and access position are chapters of it —
// and it carries its own pictures, its stylesheet, and the project's mark, so the archive needs no
// picture folder and the reader needs no network.
const guideSource = "docs/admin-guide.html"

// archiveFiles reads what a target's archive carries: the program, the guide that operates it, and the
// licence of the ffmpeg the build embeds.
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
func archiveFiles(repo string, tr releaseTarget) ([]archiveFile, error) {
	dir := filepath.Join(repo, "dist", tr.targetWord())

	name := "rdio-scanner" + programSuffix(tr.targetWord())

	body, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, fmt.Errorf("the release has no %s for %s: %w", name, tr.targetWord(), err)
	}

	// The binary is a program the reader runs, and the guide is a document they read. Info-Zip
	// restores the mode it finds in the archive, so the mode has to be in the archive.
	files := []archiveFile{{name: name, mode: 0o755, body: body}}

	text, err := os.ReadFile(filepath.Join(repo, guideSource))
	if err != nil {
		return nil, fmt.Errorf("the release has no %s for %s: %w", guideSource, tr.targetWord(), err)
	}

	files = append(files, archiveFile{name: guideSource, mode: 0o644, body: text})

	if _, ok := bundle.ReadManifest().Targets[tr.targetWord()]; ok {
		path := filepath.Join(repo, "internal", "infrastructure", "ffmpeg", "bundle", tr.targetWord(), "LICENCE.md")

		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("the release has no ffmpeg licence for %s: %w", tr.targetWord(), err)
		}

		files = append(files, archiveFile{name: "FFMPEG.txt", mode: 0o644, body: body})
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
			Name:   f.name,
			Method: zip.Deflate,
		}

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
