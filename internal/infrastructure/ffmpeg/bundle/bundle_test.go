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
	"testing"
	"testing/fstest"
)

// These are unit tests of the rule that decides whether a target carries a bundle. They are given a
// directory they can see completely, so they say nothing about which target this machine happens to be.

func TestARecordedTargetCarriesABundle(t *testing.T) {
	files := fstest.MapFS{
		"linux-arm64/ffmpeg.gz":  {Data: []byte("ffmpeg")},
		"linux-arm64/ffprobe.gz": {Data: []byte("ffprobe")},
	}

	found, ok := lookup(files, "linux-arm64")
	if !ok {
		t.Fatal("a target that carries both programs has no bundle")
	}

	if found.Dir != "linux-arm64" {
		t.Errorf("the bundle is for %s, want linux-arm64", found.Dir)
	}
}

func TestATargetWithNoRecordCarriesNoBundle(t *testing.T) {
	files := fstest.MapFS{
		"linux-amd64/ffmpeg.gz":  {Data: []byte("ffmpeg")},
		"linux-amd64/ffprobe.gz": {Data: []byte("ffprobe")},
	}

	if _, ok := lookup(files, "freebsd-amd64"); ok {
		t.Fatal("freebsd-amd64 has no directory in the bundle and still reported one")
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

func TestTheManifestNamesThePinnedFfmpegAndTheCoveredTargets(t *testing.T) {
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
	}

	if _, ok := manifest.Targets["macos-arm64"]; ok {
		t.Error("macos-arm64 is an uncovered target and the manifest has a record for it")
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
