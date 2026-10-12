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

package bundle

import (
	"strings"
	"testing"
	"testing/fstest"
)

// These are unit tests of the rule that decides whether a target carries a bundle. They are given a
// directory they can see completely, so they say nothing about which target this machine happens to be.

// A directory holding both programs is a bundle, whatever the way those programs came to be: the pair this
// build compiles, and the pair the maintainer drops for a macOS or FreeBSD target, end up in the same
// shape, and the server materializes both the same way.
func TestARecordedTargetCarriesABundle(t *testing.T) {
	for _, dir := range []string{"linux-arm64", "macos-amd64", "macos-arm64", "freebsd-amd64"} {
		files := fstest.MapFS{
			dir + "/ffmpeg.gz":  {Data: []byte("ffmpeg")},
			dir + "/ffprobe.gz": {Data: []byte("ffprobe")},
		}

		found, ok := lookup(files, dir)
		if !ok {
			t.Errorf("%s holds both programs and has no bundle", dir)
			continue
		}

		if found.Dir != dir {
			t.Errorf("the bundle is for %s, want %s", found.Dir, dir)
		}
	}
}

// The three targets whose pair is compiled on the platform each one runs on are the ordinary state of a
// fresh clone: their directories hold only the placeholder that makes the embed compile, so a binary built
// from that clone carries nothing and converts with the ffmpeg its host installs.
func TestASuppliedTargetWhosePairWasNeverDroppedCarriesNoBundle(t *testing.T) {
	for _, dir := range []string{"macos-amd64", "macos-arm64", "freebsd-amd64"} {
		files := fstest.MapFS{dir + "/.gitkeep": {Data: []byte{}}}

		if _, ok := lookup(files, dir); ok {
			t.Errorf("%s holds only its placeholder and still reported a bundle", dir)
		}
	}
}

func TestATargetWithNoRecordCarriesNoBundle(t *testing.T) {
	files := fstest.MapFS{
		"linux-amd64/ffmpeg.gz":  {Data: []byte("ffmpeg")},
		"linux-amd64/ffprobe.gz": {Data: []byte("ffprobe")},
	}

	if _, ok := lookup(files, "netbsd-amd64"); ok {
		t.Fatal("netbsd-amd64 has no directory in the bundle and still reported one")
	}
}

// The placeholder is what makes a plain `go build` compile on a fresh clone: the target directory is
// committed with nothing in it but `.gitkeep`, and the bundle lookup has to read that as no bundle
// rather than as an empty one.
func TestAPlaceholderOnlyTargetDirectoryCarriesNoBundle(t *testing.T) {
	files := fstest.MapFS{
		"linux-arm64/.gitkeep": {Data: []byte("")},
	}

	if _, ok := lookup(files, "linux-arm64"); ok {
		t.Fatal("a target directory holding only its placeholder reported a bundle")
	}
}

// A half-built bundle is a build that failed, not a deployment that falls back quietly.
func TestABundleMissingOneOfItsTwoProgramsCarriesNoBundle(t *testing.T) {
	files := fstest.MapFS{
		"linux-arm64/ffmpeg.gz": {Data: []byte("ffmpeg")},
	}

	if _, ok := lookup(files, "linux-arm64"); ok {
		t.Fatal("a target carrying ffmpeg but not ffprobe reported a bundle")
	}
}

func TestTheManifestNamesThePinnedFfmpegAndTheCompiledTargets(t *testing.T) {
	manifest := ReadManifest()

	if manifest.FFmpeg != "9.0.2" {
		t.Errorf("the manifest pins ffmpeg %s, want 9.0.2", manifest.FFmpeg)
	}

	if manifest.Source.URL != "https://ffmpeg.org/releases/ffmpeg-9.0.2.tar.xz" {
		t.Errorf("the source is %s", manifest.Source.URL)
	}

	if len(manifest.Source.SHA256) != 64 {
		t.Errorf("the pinned source digest is %q, want a sha256", manifest.Source.SHA256)
	}

	want := []string{"linux-amd64", "linux-arm64", "linux-arm", "linux-386", "windows-amd64"}

	for _, target := range want {
		record, ok := manifest.Targets[target]
		if !ok {
			t.Errorf("the manifest has no record for %s", target)
			continue
		}

		if record.Container == "" {
			t.Errorf("%s has no build container", target)
		}

		if len(record.Packages) == 0 {
			t.Errorf("%s has no toolchain packages", target)
		}

		if record.Produced != "" {
			t.Errorf("%s is compiled by this build and says it is produced by %q", target, record.Produced)
		}

		if len(record.Programs) != 0 {
			t.Errorf("%s is compiled from source and pins the digests of programs it did not produce: %v", target, record.Programs)
		}
	}
}

// The three targets this build compiles no ffmpeg for are the ones the maintainer compiles on the platform
// each one runs on. The manifest records such a pair by the digest of each program, and records no
// toolchain and no check environment for it, because the machine that produces the releases has neither.
//
// A digest left unpinned is the ordinary state of a target whose pair has not been built yet: the build
// says so and ships nothing for it, and never adopts a pair it cannot name.
func TestTheManifestRecordsTheSuppliedTargetsByDigest(t *testing.T) {
	manifest := ReadManifest()

	for _, target := range []string{"macos-amd64", "macos-arm64", "freebsd-amd64"} {
		record, ok := manifest.Targets[target]
		if !ok {
			t.Errorf("the manifest has no record for %s, so its release archive carries no ffmpeg of its own", target)
			continue
		}

		if record.Produced != "supplied" {
			t.Errorf("%s is produced by %q, want supplied", target, record.Produced)
		}

		for _, name := range []string{"ffmpeg", "ffprobe"} {
			digest, ok := record.Programs[name]
			if !ok {
				t.Errorf("%s records no %s digest, so the build has nothing to adopt the pair against", target, name)
				continue
			}

			if len(digest) != 0 && len(digest) != 64 {
				t.Errorf("%s records the %s digest %q, want a sha256 or nothing pinned", target, name, digest)
			}
		}

		if record.Container != "" || len(record.Packages) > 0 || len(record.Cross) > 0 {
			t.Errorf("%s is supplied and still records a toolchain: container %q, packages %v, cross %v", target, record.Container, record.Packages, record.Cross)
		}

		if record.Conformance.Container != "" || len(record.Conformance.Packages) > 0 || len(record.Conformance.Run) > 0 {
			t.Errorf("%s is supplied and still records a check environment: container %q, run %v", target, record.Conformance.Container, record.Conformance.Run)
		}
	}
}

// A supplied pair is compiled on the platform the target runs on, and the manifest is where that compile is
// written down: the flags that set the floor the binary declares and link it the way the target needs, and
// the make program the build runs. ffmpeg's own documentation refuses BSD make, so the FreeBSD record names
// gmake; the macOS records name none, because the Command Line Tools carry make.
func TestTheSuppliedRecordsNameTheCompileTheirHostRuns(t *testing.T) {
	manifest := ReadManifest()

	type native struct {
		cflags  []string
		ldflags []string
		make    string
	}

	want := map[string]native{
		"freebsd-amd64": {ldflags: []string{"-static"}, make: "gmake"},
		"macos-amd64":   {cflags: []string{"-mmacosx-version-min=11.0"}, ldflags: []string{"-mmacosx-version-min=11.0"}},
		"macos-arm64":   {cflags: []string{"-mmacosx-version-min=11.0"}, ldflags: []string{"-mmacosx-version-min=11.0"}},
	}

	for target, w := range want {
		record, ok := manifest.Targets[target]
		if !ok {
			t.Errorf("the manifest has no record for %s", target)
			continue
		}

		if got := strings.Join(record.CFlags, " "); got != strings.Join(w.cflags, " ") {
			t.Errorf("%s compiles with the cflags %v, want %v", target, record.CFlags, w.cflags)
		}

		if got := strings.Join(record.LDFlags, " "); got != strings.Join(w.ldflags, " ") {
			t.Errorf("%s links with the ldflags %v, want %v", target, record.LDFlags, w.ldflags)
		}

		if record.Make != w.make {
			t.Errorf("%s runs %s as its make program, want %q", target, record.Make, w.make)
		}
	}
}

// The three configure omissions this project found by running the trimmed build are the ones a hand
// edited recipe is most likely to put back wrong, because each one fails at conversion time rather
// than at configure time.
func TestTheRecipeCarriesWhatTheConverterAsksOfFfmpeg(t *testing.T) {
	recipe := ReadManifest().Recipe

	want := []string{
		"--enable-ffmpeg",
		"--enable-ffprobe",
		"--enable-protocol=fd",
		"--enable-filter=abuffer",
		"--enable-filter=abuffersink",
		"--enable-filter=aformat",
		"--enable-filter=aresample",
		"--enable-filter=apad",
		"--enable-filter=loudnorm",
		"--enable-muxer=ipod",
	}

	for _, flag := range want {
		if !hasFlag(recipe, flag) {
			t.Errorf("the recipe does not enable %s, and the conversion fails without it", flag)
		}
	}
}

// A recipe that enables a GPL or nonfree component would change the licence of what is shipped.
func TestTheRecipeEnablesNoGplOrNonfreeComponent(t *testing.T) {
	recipe := ReadManifest().Recipe

	for _, flag := range recipe {
		if flag == "--enable-gpl" || flag == "--enable-nonfree" {
			t.Errorf("the recipe enables %s, which is not what this project ships", flag)
		}
	}
}

func hasFlag(recipe []string, flag string) bool {
	for _, one := range recipe {
		if one == flag {
			return true
		}
	}

	return false
}
