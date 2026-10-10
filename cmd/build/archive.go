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
	"regexp"
	"sort"
	"strings"

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

// documentSources is the documentation a release archive carries, in the order a reader gets it: the
// change log, the target's own platform page, the web app guide, and the FAQ. These are the files the
// pandoc build was fed, carried now as the files they are written in.
func documentSources(tr releaseTarget) []string {
	return []string{
		"CHANGELOG.md",
		"docs/platforms/" + tr.name + ".md",
		"docs/webapp.md",
		"docs/faq.md",
	}
}

// documentImageReferences is the picture path a markdown image reference names, with the suffix a
// GitHub raw link stripped. A reference that names an address is not a file in the archive.
func documentImageReferences(text string) []string {
	var refs []string

	for _, m := range imagePattern.FindAllStringSubmatch(text, -1) {
		if strings.Contains(m[1], "://") {
			continue
		}

		refs = append(refs, strings.TrimSuffix(m[1], "?raw=true"))
	}

	return refs
}

var imagePattern = regexp.MustCompile(`!\[[^]]*\]\(([^)]+)\)`)

// archiveText is the text a source is carried as in the archive: every image reference written without
// the query GitHub uses to serve a raw file. `?raw=true` is part of a URL, and part of a file name on
// disk, so a reader who extracts the archive and opens a document in a local editor gets a page whose
// every picture is broken. The repository's own files are not touched: the change is in the bytes
// written into the archive, and nothing else in the text is rewritten.
func archiveText(text string) string {
	return imagePattern.ReplaceAllStringFunc(text, func(ref string) string {
		return strings.ReplaceAll(ref, "?raw=true", "")
	})
}

// archiveFiles reads what a target's archive carries: the binary, the documentation as the files it is
// written in, the pictures those files point at, and the ffmpeg licence when the target carries an
// ffmpeg.
//
// The licence is the text the ffmpeg phase wrote from the manifest, and the archive carries it under the
// name a reader looks for, not the name the bundle directory uses. A target this project ships with the
// ffmpeg its host installs has no ffmpeg inside its binary, so it has no licence to carry.
//
// The archive's layout is the repository's own layout, so every relative path a source wrote resolves
// inside the archive: a platform page says `../images` and the web app guide says `./images`, and both
// reach `docs/images`. The tool renders nothing, converts nothing, inlines nothing, and rewrites
// nothing.
func archiveFiles(repo string, tr releaseTarget) ([]archiveFile, error) {
	dir := filepath.Join(repo, "dist", tr.targetWord())

	name := "rdio-scanner" + programSuffix(tr.targetWord())

	body, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, fmt.Errorf("the release has no %s for %s: %w", name, tr.targetWord(), err)
	}

	// The binary is a program the reader runs, and the documentation is a document they read. Info-Zip
	// restores the mode it finds in the archive, so the mode has to be in the archive.
	files := []archiveFile{{name: name, mode: 0o755, body: body}}

	var images []archiveFile

	seen := map[string]bool{}

	for _, path := range documentSources(tr) {
		text, err := os.ReadFile(filepath.Join(repo, path))
		if err != nil {
			return nil, fmt.Errorf("the release has no %s for %s: %w", path, tr.targetWord(), err)
		}

		files = append(files, archiveFile{name: path, mode: 0o644, body: []byte(archiveText(string(text)))})

		for _, ref := range documentImageReferences(string(text)) {
			image := filepath.Clean(filepath.Join(filepath.Dir(path), ref))

			// A picture two documents point at is one file in the archive, not two copies of it.
			if seen[image] {
				continue
			}

			seen[image] = true

			picture, err := os.ReadFile(filepath.Join(repo, image))
			if err != nil {
				return nil, fmt.Errorf("the release has no %s for %s: %w", image, tr.targetWord(), err)
			}

			images = append(images, archiveFile{name: image, mode: 0o644, body: picture})
		}
	}

	sort.Slice(images, func(i, j int) bool { return images[i].name < images[j].name })

	files = append(files, images...)

	if _, ok := bundle.ReadManifest().Targets[tr.targetWord()]; ok {
		path := filepath.Join(repo, "internal", "infrastructure", "ffmpeg", "bundle", tr.targetWord(), "LICENCE.md")

		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("the release has no ffmpeg licence for %s: %w", tr.targetWord(), err)
		}

		files = append(files, archiveFile{name: "FFMPEG.md", mode: 0o644, body: body})
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
