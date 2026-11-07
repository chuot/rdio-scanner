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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// archivePath is where a target's release archive is written.
// The name is the one the platform document tells the reader to download.
func archivePath(target string, version string) string {
	return filepath.Join("dist", fmt.Sprintf("rdio-scanner-%s-v%s.zip", target, version))
}

// archiveVersionOf reads the version out of a release archive's name,
// found by the target's known shape rather than by the last hyphen.
// A name the tool did not write is not an archive of this release.
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

// pruneReleaseFolder removes the archives of versions this run did not release,
// so nobody uploads an older file sitting beside the new ones,
// and names each removal it makes.
// A file that is not an archive name is left to the operator who put it there.
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

// archiveFile is one file a release archive carries,
// with the time that file itself was written.
type archiveFile struct {
	name     string
	mode     os.FileMode
	modified time.Time
	body     []byte
}

// readArchiveSource reads one file the archive carries from a path in the tree,
// keeping the time the source file itself carries.
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

// guideSource is the administrator guide the archive carries for whoever
// installs and runs the server, at the path the guide itself states,
// with its own pictures and stylesheet inside, so the reader needs no network.
const guideSource = "docs/admin-guide.html"

// listenerGuideSource is the guide the archive carries for whoever hears
// calls on the web application, at the path it states, carrying its own
// pictures and stylesheet, so the listener is not handed the administrator's document.
const listenerGuideSource = "docs/user-guide.html"

// archiveFiles reads what a target's archive carries: the program the run wrote,
// the two guides, and the licence of the ffmpeg the binary embeds. The program
// comes from the path this run wrote it to, never a path of its own.
func archiveFiles(repo string, tr releaseTarget, output string) ([]archiveFile, error) {
	name := "rdio-scanner" + programSuffix(tr.targetWord())

	// The program must arrive executable: the archiver restores
	// the mode it finds in the archive, so the mode goes in.
	program, err := readArchiveSource(repo, output, name, 0o755)
	if err != nil {
		return nil, fmt.Errorf("the release has no %s for %s: %w", name, tr.targetWord(), err)
	}

	guide, err := readArchiveSource(repo, guideSource, guideSource, 0o644)
	if err != nil {
		return nil, fmt.Errorf("the release has no %s for %s: %w", guideSource, tr.targetWord(), err)
	}

	listenerGuide, err := readArchiveSource(repo, listenerGuideSource, listenerGuideSource, 0o644)
	if err != nil {
		return nil, fmt.Errorf("the release has no %s for %s: %w", listenerGuideSource, tr.targetWord(), err)
	}

	files := []archiveFile{program, guide, listenerGuide}

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

		// The entry carries the time of the file it carries,
		// not the moment the archive was written.

		// SetMode puts the permissions where the archiver
		// looks for them when it restores the file.
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
