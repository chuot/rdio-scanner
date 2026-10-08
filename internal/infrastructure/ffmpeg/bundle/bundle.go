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

// Package bundle is the ffmpeg this server carries inside its own binary.
//
// A server built for a covered target carries the ffmpeg compiled for that target, so a deployment
// needs nothing installed on the host it runs on. A server built for a target this project does not
// compile an ffmpeg for — macOS, FreeBSD, and every plain `go build` a developer runs — carries none,
// and the server falls back to the ffmpeg its host provides.
//
// Which target a binary carries is decided by its build tags, in the bundle_<target>.go files: each
// one embeds exactly one target's directory, so a linux-arm64 binary carries the linux-arm64 pair and
// nothing else. This file holds the rules that do not depend on the target.
package bundle

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
)

// Manifest is everything the build and the running server agree about: which ffmpeg release is
// shipped, where its source is and how it is recognised, the configure recipe it was compiled with,
// and the targets that carry one.
//
// It is the only place any of that is written down. The build reads it to decide what to compile, the
// release archive quotes it to say what it ships, and a running server reads the version out of it to
// name the ffmpeg it carries.
type Manifest struct {
	FFmpeg  string            `json:"ffmpeg"`
	Source  Source            `json:"source"`
	Recipe  []string          `json:"recipe"`
	Targets map[string]Target `json:"targets"`
}

// Source is the ffmpeg release the bundle was compiled from, and how the build recognises the
// tarball it downloads as that release.
//
// ffmpeg.org signs its releases and publishes no checksum, so the digest here is pinned by this
// project rather than published by ffmpeg's own.
type Source struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// Target is how one architecture's ffmpeg is produced: the container that holds its toolchain, the
// packages to install in it, and the configure arguments that make the compile cross to it.
//
// A target with no record in the manifest is an uncovered target: the build compiles no ffmpeg for it
// and the server that runs on it uses the host's.
type Target struct {
	Container string   `json:"container"`
	Packages  []string `json:"packages"`
	Cross     []string `json:"cross"`
	CFlags    []string `json:"cflags"`
	LDFlags   []string `json:"ldflags"`
}

//go:embed manifest.json
var manifestJSON []byte

// ReadManifest returns the manifest this binary was compiled with.
//
// It is embedded rather than read from disk because the running server has to be able to name the
// ffmpeg it carries without knowing where it was started from.
func ReadManifest() Manifest {
	var manifest Manifest

	if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
		panic(fmt.Sprintf("the ffmpeg bundle manifest is not readable: %v", err))
	}

	return manifest
}

// Bundle is the pair of programs one target carries.
type Bundle struct {
	// Dir is the target this bundle was compiled for, which is also the directory its programs are
	// embedded under.
	Dir string

	files fs.FS
}

// Lookup returns the bundle this binary carries, or reports that its target carries none.
func Lookup() (Bundle, bool) {
	files, dir := target()

	return lookup(files, dir)
}

// lookup is the rule, separated from the build tags that decide which directory this binary has, so
// that it can be exercised against a directory the test can see completely.
//
// A target carries a bundle when its directory holds both programs. A directory holding only its
// placeholder — which is what a fresh clone has, and what makes a plain `go build` compile — carries
// none, and so does a directory holding only one of the two, because that is a build that failed
// rather than a deployment that should quietly convert with half a converter.
func lookup(files fs.FS, dir string) (Bundle, bool) {
	if files == nil || dir == "" {
		return Bundle{}, false
	}

	ffmpeg, ffprobe := programNames(dir)

	for _, name := range []string{ffmpeg, ffprobe} {
		if _, err := fs.Stat(files, dir+"/"+name+".gz"); err != nil {
			return Bundle{}, false
		}
	}

	return Bundle{Dir: dir, files: files}, true
}

// programNames returns the two programs a bundle carries, named the way that target's operating
// system names a program it runs.
func programNames(dir string) (ffmpeg string, ffprobe string) {
	suffix := executableSuffix(dir)

	return "ffmpeg" + suffix, "ffprobe" + suffix
}

// executableSuffix is what a target's operating system puts on the end of a program it runs.
func executableSuffix(dir string) string {
	if strings.HasPrefix(dir, "windows-") {
		return ".exe"
	}

	return ""
}
