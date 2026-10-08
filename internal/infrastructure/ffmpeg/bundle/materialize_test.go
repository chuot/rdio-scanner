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
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// These are integration tests of the seam between the embedded bundle and the data directory the
// server writes into: they run against a real directory, because writing a file a process then has
// to execute is the whole point of the seam.

// aBundle is a bundle for one target, with the two programs it carries held in memory.
func aBundle(t *testing.T, dir string, ffmpeg string, ffprobe string) Bundle {
	t.Helper()

	files := fstest.MapFS{
		dir + "/" + programNamesForTest(dir)[0] + ".gz": {Data: gzipped(t, ffmpeg)},
		dir + "/" + programNamesForTest(dir)[1] + ".gz": {Data: gzipped(t, ffprobe)},
	}

	bundle, ok := lookup(files, dir)
	if !ok {
		t.Fatalf("%s carries both programs and the bundle lookup says it does not", dir)
	}

	return bundle
}

func programNamesForTest(dir string) []string {
	ffmpeg, ffprobe := programNames(dir)

	return []string{ffmpeg, ffprobe}
}

func gzipped(t *testing.T, text string) []byte {
	t.Helper()

	out := bytes.NewBuffer([]byte(nil))

	writer := gzip.NewWriter(out)
	if _, err := writer.Write([]byte(text)); err != nil {
		t.Fatalf("the test cannot gzip its program: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("the test cannot finish gzipping its program: %v", err)
	}

	return out.Bytes()
}

func notGzipped(t *testing.T, dir string, name string) fstest.MapFS {
	t.Helper()

	ffmpeg, ffprobe := programNames(dir)

	if name == ffprobe {
		return fstest.MapFS{
			dir + "/" + ffmpeg + ".gz":  {Data: gzipped(t, "a program")},
			dir + "/" + ffprobe + ".gz": {Data: []byte("this is not a gzipped program")},
		}
	}

	return fstest.MapFS{
		dir + "/" + ffmpeg + ".gz":  {Data: []byte("this is not a gzipped program")},
		dir + "/" + ffprobe + ".gz": {Data: gzipped(t, "a program")},
	}
}

func TestMaterializingABundleWritesBothProgramsAsExecutables(t *testing.T) {
	bundle := aBundle(t, "linux-arm64", "the ffmpeg", "the ffprobe")

	dir := t.TempDir()

	ffmpegPath, ffprobePath, err := bundle.Materialize(dir)
	if err != nil {
		t.Fatalf("materialising the bundle failed: %v", err)
	}

	for _, path := range []string{ffmpegPath, ffprobePath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s was not written: %v", path, err)
		}

		if info.Mode().Perm() != 0755 {
			t.Errorf("%s has mode %v, want 0755 so the server can run it", path, info.Mode().Perm())
		}
	}

	if body, err := os.ReadFile(ffmpegPath); err != nil || string(body) != "the ffmpeg" {
		t.Errorf("the ffmpeg written into %s is %q", ffmpegPath, string(body))
	}

	if body, err := os.ReadFile(ffprobePath); err != nil || string(body) != "the ffprobe" {
		t.Errorf("the ffprobe written into %s is %q", ffprobePath, string(body))
	}
}

// The version is in the name so that upgrading ffmpeg writes a new file instead of replacing one
// another process may be running.
func TestMaterializingABundleNamesItsProgramsAfterTheFfmpegItCarries(t *testing.T) {
	bundle := aBundle(t, "linux-arm64", "the ffmpeg", "the ffprobe")

	version := ReadManifest().FFmpeg

	ffmpegPath, ffprobePath, err := bundle.Materialize(t.TempDir())
	if err != nil {
		t.Fatalf("materialising the bundle failed: %v", err)
	}

	if filepath.Base(ffmpegPath) != "ffmpeg-"+version {
		t.Errorf("the ffmpeg is named %s, want ffmpeg-%s", filepath.Base(ffmpegPath), version)
	}

	if filepath.Base(ffprobePath) != "ffprobe-"+version {
		t.Errorf("the ffprobe is named %s, want ffprobe-%s", filepath.Base(ffprobePath), version)
	}
}

// A windows build runs a program named with .exe, and that is decided by the target the bundle was
// compiled for rather than by the machine doing the materialising.
func TestMaterializingAWindowsBundleNamesItsProgramsAsWindowsPrograms(t *testing.T) {
	bundle := aBundle(t, "windows-amd64", "the ffmpeg", "the ffprobe")

	version := ReadManifest().FFmpeg

	dir := t.TempDir()

	ffmpegPath, ffprobePath, err := bundle.Materialize(dir)
	if err != nil {
		t.Fatalf("materialising the bundle failed: %v", err)
	}

	if filepath.Base(ffmpegPath) != "ffmpeg-"+version+".exe" {
		t.Errorf("the ffmpeg is named %s, want ffmpeg-%s.exe", filepath.Base(ffmpegPath), version)
	}

	if filepath.Base(ffprobePath) != "ffprobe-"+version+".exe" {
		t.Errorf("the ffprobe is named %s, want ffprobe-%s.exe", filepath.Base(ffprobePath), version)
	}
}

// A server that starts again must not rewrite a program another server may be running out from under
// it, so a program that is already the right one is left exactly as it is.
func TestMaterializingABundleAgainLeavesTheProgramsItAlreadyWrote(t *testing.T) {
	bundle := aBundle(t, "linux-amd64", "the ffmpeg", "the ffprobe")

	dir := t.TempDir()

	ffmpegPath, _, err := bundle.Materialize(dir)
	if err != nil {
		t.Fatalf("materialising the bundle failed: %v", err)
	}

	// The same length as what was written, so a rewrite cannot be hiding behind a size check.
	if err := os.WriteFile(ffmpegPath, []byte(strings.Repeat("x", len("the ffmpeg"))), 0755); err != nil {
		t.Fatalf("the test cannot mark the program it wrote: %v", err)
	}

	if _, _, err := bundle.Materialize(dir); err != nil {
		t.Fatalf("materialising the bundle a second time failed: %v", err)
	}

	body, err := os.ReadFile(ffmpegPath)
	if err != nil {
		t.Fatalf("the program the server already wrote is gone: %v", err)
	}

	if string(body) != strings.Repeat("x", len("the ffmpeg")) {
		t.Errorf("the second materialising rewrote a program that was already there: got %q", string(body))
	}
}

// A program that is there but is not the one this binary carries is replaced, because a bundle that
// was built from another ffmpeg version has to win over whatever an older server left behind.
func TestMaterializingABundleReplacesAProgramThatIsNotTheOneItCarries(t *testing.T) {
	bundle := aBundle(t, "linux-amd64", "the ffmpeg", "the ffprobe")

	dir := t.TempDir()

	ffmpegPath, _, err := bundle.Materialize(dir)
	if err != nil {
		t.Fatalf("materialising the bundle failed: %v", err)
	}

	if err := os.WriteFile(ffmpegPath, []byte("left behind by an older server"), 0755); err != nil {
		t.Fatalf("the test cannot replace the program with an older one: %v", err)
	}

	if _, _, err := bundle.Materialize(dir); err != nil {
		t.Fatalf("materialising the bundle a second time failed: %v", err)
	}

	body, err := os.ReadFile(ffmpegPath)
	if err != nil {
		t.Fatalf("the program the server already wrote is gone: %v", err)
	}

	if string(body) != "the ffmpeg" {
		t.Errorf("the bundle left %q where it should have written the ffmpeg it carries", string(body))
	}
}

// A bundle whose program cannot be read back is a build that produced something broken, and the
// server must not end up with one program written and the other missing.
func TestMaterializingABundleWritesNothingWhenOneOfItsProgramsIsNotReadable(t *testing.T) {
	dir := t.TempDir()

	ffmpeg, ffprobe := programNames("linux-amd64")

	bundle, ok := lookup(notGzipped(t, "linux-amd64", ffprobe), "linux-amd64")
	if !ok {
		t.Fatal("the bundle lookup should report the pair is present even when one of them is broken")
	}

	if _, _, err := bundle.Materialize(dir); err == nil {
		t.Fatal("materialising a bundle with a program it cannot read back reported no failure")
	}

	for _, name := range []string{ffmpeg, ffprobe} {
		path := filepath.Join(dir, name+"-"+ReadManifest().FFmpeg)

		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s was written even though the bundle could not be materialised", path)
		}
	}
}

func TestMaterializingABundleReportsTheDirectoryItCannotWriteTo(t *testing.T) {
	bundle := aBundle(t, "linux-amd64", "the ffmpeg", "the ffprobe")

	base := t.TempDir()

	// A file where the directory should be: the server cannot put its programs inside it.
	blocker := filepath.Join(base, "ffmpeg")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("the test cannot block the bundle directory: %v", err)
	}

	if _, _, err := bundle.Materialize(blocker); err == nil {
		t.Fatal("materialising into a path that is not a directory reported no failure")
	}
}
