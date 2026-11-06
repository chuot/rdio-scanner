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

package bundle

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// These are unit tests of the rule that decides whether a target carries a bundle. They are given a
// directory they can see completely, so they say nothing about which target this machine happens to be.

// A directory holding both programs is a bundle, whatever the way those programs came to be: the pair a build
// run compiles and drops, and the pair somebody drops in themselves, end up in the same shape, and the server
// materializes both the same way.
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

// A target directory holding only the placeholder that makes the embed compile carries no bundle: a server
// built from such a tree converts with the ffmpeg its host installs, and carries binaries of its own only
// after a build run has dropped a pair into the target's directory.
func TestATargetWhoseBinariesWereNeverDroppedCarriesNoBundle(t *testing.T) {
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

	want := []string{"linux-amd64", "linux-arm64", "linux-arm", "linux-386", "windows-amd64", "freebsd-amd64", "macos-amd64", "macos-arm64"}

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

		// Every compiled record pins the image it compiles in, and pins the image a sysroot is copied out of
		// and the image a proof runs in whenever it names one. A tag is a name the registry can point somewhere
		// else tomorrow: a compile that resolves whatever the tag points at then is not the compile this record
		// describes, and the pair it produced is filed under this record's name anyway.
		if !anImagePinnedAtADigest(record.ContainerDigest) {
			t.Errorf("%s compiles in %s named at %q: a record that names no digest names a moving tag", target, record.Container, record.ContainerDigest)
		}

		if record.SysrootImage != "" && !anImagePinnedAtADigest(record.SysrootDigest) {
			t.Errorf("%s copies its sysroot out of %s named at %q: the image a sysroot is copied out of is pinned like the image doing the copying", target, record.SysrootImage, record.SysrootDigest)
		}

		if record.Conformance.Container != "" && !anImagePinnedAtADigest(record.Conformance.Digest) {
			t.Errorf("%s checks its programs in %s named at %q: a container a record names to prove its pair is pinned like the container that compiled it", target, record.Conformance.Container, record.Conformance.Digest)
		}

		// What proves the pair each record names is gated where the plan is built, in the table that writes out
		// what every compiled target is compiled with: a record naming both a run and a read is refused there.
	}
}

// A record's toolchain is a list of packages the container's distribution installs, and every one of those
// packages was built by the distribution for one machine. Debian carries gcc-multilib and libc6-dev-i386 in its
// amd64 archive and not in its arm64 one, and the cross compilers the other records install are amd64-hosted
// builds of GCC. So a record that names no platform for its toolchain is compiled on whatever machine the
// container host happens to be, and installs a different toolchain there: Debian's binutils is built for the
// machine it runs on, and the arm64 build answers that it cannot recognise the format of the x86-64 FreeBSD
// object an amd64 cross compiler wrote. A record therefore names the platform its toolchain runs on in the words
// the container runtime uses, and the build asks that runtime for the platform when it builds the toolchain image
// and when it starts the compile inside it.
func TestEveryCompiledTargetNamesThePlatformItsToolchainRunsOn(t *testing.T) {
	manifest := ReadManifest()

	for _, target := range []string{"linux-amd64", "linux-arm64", "linux-arm", "linux-386", "windows-amd64", "freebsd-amd64", "macos-amd64", "macos-arm64"} {
		record, ok := manifest.Targets[target]
		if !ok {
			t.Errorf("the manifest has no record for %s", target)
			continue
		}

		if record.CompilePlatform == "" {
			t.Errorf("%s names no platform for its toolchain: the packages its record installs were built by the distribution for one machine, and the compile that runs on another machine installs whatever toolchain that one carries", target)
			continue
		}

		runtimeOS, arch, found := strings.Cut(record.CompilePlatform, "/")
		if !found || runtimeOS == "" || arch == "" {
			t.Errorf("%s names toolchain platform %q, want os/arch as the container runtime names it", target, record.CompilePlatform)
		}

		// The eight records all name the same platform today because all eight install an amd64-hosted
		// cross toolchain; a record that ever moves to another one has to say why its packages exist there.
		if record.CompilePlatform != "linux/amd64" {
			t.Errorf("%s compiles its toolchain on %s, want linux/amd64: the cross compilers and multilib packages the records name are the amd64 archive's", target, record.CompilePlatform)
		}
	}
}

// `produced` is how a record used to say that this build compiles nothing for its target and takes the binaries
// from wherever they come from. No target is produced that way any more, and the manifest is where that is
// settled: the field is out of both the record and the struct that reads it, so a manifest that grew one again
// would be a manifest carrying a field nothing in the build reads. The same holds of the top-level `make`
// program: a container installs the make its compile step runs, and the one compile that asks a host for its
// make program reads it out of the record's host block.
func TestNoRecordNamesAProductionKindOrATopLevelMakeProgram(t *testing.T) {
	body, err := os.ReadFile("manifest.json")
	if err != nil {
		t.Fatalf("the manifest this package embeds cannot be read: %v", err)
	}

	var raw struct {
		Targets map[string]map[string]json.RawMessage `json:"targets"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("the manifest this package embeds does not read as JSON: %v", err)
	}

	for target, fields := range raw.Targets {
		for _, field := range []string{"produced", "make"} {
			if _, found := fields[field]; found {
				t.Errorf("%s names %q at the top of its record, and nothing in the build reads it", target, field)
			}
		}
	}
}

// A macOS target is compiled two ways, and one record names both: the Linux build host compiles it in a
// container against Apple's SDK mounted inside it, and a Mac compiles it with its own compiler, its own link,
// and its own ad-hoc signature. The record's cflags and ldflags are the container's, and they name the mount;
// the host block is the Mac's, and it names no mount, because /sysroot is a path that exists only inside the
// container, and no linker switch, because a Mac links with its own ld. The two lists repeat the same
// deployment floor on purpose, and pinning both by value is what catches one being moved and the other left
// behind: two archives for one target promising two different minimum systems.
//
// Neither list says anything about the binaries themselves. The container compile is proven by reading what it
// produced, because nothing on the build host can run a Mach-O, and a manifest carrying a digest of a program
// the build never ran would be a promise the release has no way to keep.
//
// The record names one more thing about the container compile: the archiver it writes its libraries with, and the
// tool that writes the index of those archives. The archiver the container's own distribution ships is built for
// the machine it runs on, and a Darwin link refuses an archive carrying nothing it can look a library up in.
//
// And it says something about the programs that compile produces: they are not stripped. The Darwin link signs
// the binary it writes, and a strip step rewrites that file afterwards in a container that has nothing which can
// sign it again.
func TestTheManifestRecordsBothWaysAMacOSTargetIsCompiled(t *testing.T) {
	manifest := ReadManifest()

	type macOSRecord struct {
		cross     string
		cc        string
		cflags    string
		ldflags   string
		hostFlags string
		archiver  string
		indexer   string
		reads     string
	}

	cases := map[string]macOSRecord{
		"macos-amd64": {
			cross:     "--enable-cross-compile --target-os=darwin --arch=x86_64",
			cc:        "clang --target=x86_64-apple-darwin20 --sysroot=/sysroot",
			cflags:    "-mmacosx-version-min=11.0 --sysroot=/sysroot",
			ldflags:   "-mmacosx-version-min=11.0 -fuse-ld=lld --sysroot=/sysroot",
			hostFlags: "-mmacosx-version-min=11.0",
			archiver:  "llvm-ar",
			indexer:   "llvm-ranlib",
			reads:     "mh_execute lc_main lc_load_dylinker lc_load_libsystem",
		},
		"macos-arm64": {
			cross:     "--enable-cross-compile --target-os=darwin --arch=aarch64",
			cc:        "clang --target=arm64-apple-darwin20 --sysroot=/sysroot",
			cflags:    "-mmacosx-version-min=11.0 --sysroot=/sysroot",
			ldflags:   "-mmacosx-version-min=11.0 -fuse-ld=lld --sysroot=/sysroot",
			hostFlags: "-mmacosx-version-min=11.0",
			archiver:  "llvm-ar",
			indexer:   "llvm-ranlib",
			reads:     "mh_execute lc_main lc_load_dylinker lc_load_libsystem lc_code_signature",
		},
	}

	for target, c := range cases {
		record, ok := manifest.Targets[target]
		if !ok {
			t.Errorf("the manifest has no record for %s, so its release archive carries no ffmpeg of its own", target)
			continue
		}

		if record.Container != "docker.io/debian:stable" {
			t.Errorf("%s compiles in %q, want docker.io/debian:stable", target, record.Container)
		}

		if got := strings.Join(record.Packages, " "); got != "build-essential clang lld llvm curl ca-certificates tar xz-utils python3" {
			t.Errorf("%s installs %q", target, got)
		}

		if got := strings.Join(record.Cross, " "); got != c.cross {
			t.Errorf("%s configures with %q, want %q", target, got, c.cross)
		}

		if record.CC != c.cc {
			t.Errorf("%s compiles with %q, which is the answer configure passes to the --cc= it was given", target, record.CC)
		}

		if got := strings.Join(record.CFlags, " "); got != c.cflags {
			t.Errorf("%s compiles in its container with the cflags %q, want %q", target, got, c.cflags)
		}

		// Debian's ld is not what writes a Mach-O: for arm64 it is ld64.lld that writes the code signature the
		// read rule asks for, and the container's linker has to be named the way FreeBSD's is.
		if got := strings.Join(record.LDFlags, " "); got != c.ldflags {
			t.Errorf("%s links in its container with the ldflags %q, want %q", target, got, c.ldflags)
		}

		if got := strings.Join(record.Host.CFlags, " "); got != c.hostFlags {
			t.Errorf("%s is compiled on its own host with the cflags %q, want %q", target, got, c.hostFlags)
		}

		if got := strings.Join(record.Host.LDFlags, " "); got != c.hostFlags {
			t.Errorf("%s is linked on its own host with the ldflags %q, want %q", target, got, c.hostFlags)
		}

		// The Command Line Tools carry a make, so the host compile asks the host's PATH for it.
		if record.Host.Make != "" {
			t.Errorf("%s names %s as its host make program, and the Command Line Tools carry make", target, record.Host.Make)
		}

		// A Darwin link looks a library up in its archive's symbol table before it reads any member of that
		// archive, and the archiver Debian ships in this container cannot write one: it is built for the machine
		// it runs on and answers "file format not recognized" for a Mach-O object, so its ranlib reports success
		// having written nothing. The record names the archiver and the indexer the compile writes its libraries
		// with, which is what the platform whose binary it produces archives with.
		if record.AR != c.archiver {
			t.Errorf("%s archives its libraries with %q, want %s: the link refuses an archive that carries no symbol table", target, record.AR, c.archiver)
		}

		if record.Ranlib != c.indexer {
			t.Errorf("%s indexes its archives with %q, want %s: the archiver is asked to write an index it cannot read out of the objects", target, record.Ranlib, c.indexer)
		}

		// The linker signs the binary while it writes it, and nothing in this build can write a signature over
		// bytes a strip step changed afterwards: the pair ships exactly what the link produced. Naming that in the
		// record is what keeps configure's own default strip out of the compile, instead of leaving the produced
		// programs to whatever the container happens to answer for strip.
		if !record.NoStripping {
			t.Errorf("%s leaves its programs to configure's default strip, and the signature its link wrote over them cannot be written again over stripped bytes", target)
		}

		if record.Sysroot.SDK != "15.5" {
			t.Errorf("%s compiles against SDK %q, want the pinned 15.5", target, record.Sysroot.SDK)
		}

		if record.Sysroot.SHA256 != "51a55f4aa4b036b44a5eebb00a36abd16c8d44b4625170f4774e5968fc387987" {
			t.Errorf("%s compiles against the tree whose digest is %s, want the digest the harness measured", target, record.Sysroot.SHA256)
		}

		if record.Sysroot.Generator != "macos-cross 8dbfdc08e33c32aca17958461e878efdce337003" {
			t.Errorf("%s compiles against the tree prepared by %q, which is not the pinned macos-cross revision", target, record.Sysroot.Generator)
		}

		if record.Sysroot.Mount != "/sysroot" {
			t.Errorf("%s mounts its sysroot at %q, want /sysroot", target, record.Sysroot.Mount)
		}

		if got := strings.Join(record.Conformance.Reads, " "); got != c.reads {
			t.Errorf("%s is proven by reading %q, want %q", target, got, c.reads)
		}

		// Nothing here, or on the container's own distribution, runs a Mach-O, so no macOS record names a machine
		// it has to be run on.
		if record.Conformance.Container != "" || len(record.Conformance.Packages) > 0 || len(record.Conformance.Run) > 0 {
			t.Errorf("%s names a way of running the binaries it compiles: container %q, run %v", target, record.Conformance.Container, record.Conformance.Run)
		}
	}
}

// Two things belong to the Darwin records alone. The archiver and the indexer are what their link demands: the
// failure that measures this names one compile, the macOS run that reported all seven of the archives it wrote as
// `archive has no index`. The choice not to strip is what their signature needs, and a Mac's own compile is not
// covered by it: a host strips its own binaries and signs them again with its own codesign. A target whose compile
// writes archives its own container's archiver understands, and whose binaries carry no signature to keep, is not
// asked to name either — and a record naming either for such a target would move the cache entry its binaries are
// named by for nothing.
func TestOnlyTheDarwinRecordsNameTheirToolchain(t *testing.T) {
	manifest := ReadManifest()

	for target, record := range manifest.Targets {
		darwin := strings.HasPrefix(target, "macos-")
		names := record.AR != "" || record.Ranlib != ""

		if darwin && !names {
			t.Errorf("%s names no archiver and no indexer, and the link of the pair it compiles refuses an archive that carries no symbol table", target)
		}

		if darwin && !record.NoStripping {
			t.Errorf("%s leaves its programs to configure's default strip, and the signature its link wrote over them cannot be written again by anything this build runs", target)
		}

		if darwin {
			continue
		}

		if names {
			t.Errorf("%s names ar=%q and ranlib=%q, and nothing measured says the compile that target describes needs a tool beside the one its container already carries", target, record.AR, record.Ranlib)
		}

		if record.NoStripping {
			t.Errorf("%s says its programs are not stripped, and nothing measured says stripping breaks the pair that target produces", target)
		}
	}
}

// The FreeBSD toolchain container is a Debian container with a FreeBSD sysroot copied out of an image of
// FreeBSD's own, and the build never runs that first image: a FreeBSD binary does not execute on this host,
// so the sysroot arrives as data. The record names the image the sysroot comes from and the compiler command
// that names the target, and the build reads both out of the record, so a record naming them has to read as
// naming them.
func TestARecordNamingASysrootImageParses(t *testing.T) {
	manifest := ReadManifest()

	record, ok := manifest.Targets["freebsd-amd64"]
	if !ok {
		t.Fatal("the manifest has no record for freebsd-amd64, which this build compiles in a container")
	}

	if record.Containerfile != "Containerfile.ffmpeg.freebsd" {
		t.Errorf("freebsd-amd64 builds its toolchain from %q, want Containerfile.ffmpeg.freebsd", record.Containerfile)
	}

	if record.SysrootImage != "docker.io/freebsd/freebsd-toolchain:15.1" {
		t.Errorf("freebsd-amd64 takes its sysroot from the image %q, want docker.io/freebsd/freebsd-toolchain:15.1", record.SysrootImage)
	}

	if record.CC != "clang --target=x86_64-unknown-freebsd --sysroot=/sysroot" {
		t.Errorf("freebsd-amd64 compiles with %q, which is the answer configure passes to the --cc= it was given", record.CC)
	}

	// The sysroot is inside the image the compile runs in, so the build mounts nothing and the operator
	// prepares nothing. A record naming a sysroot the operator prepared names one held outside the
	// repositories, which is what a macOS SDK is.
	if record.Sysroot != (Sysroot{}) {
		t.Errorf("freebsd-amd64 names a sysroot somebody prepared: %+v", record.Sysroot)
	}

	if got := strings.Join(record.Cross, " "); got != "--enable-cross-compile --target-os=freebsd --arch=x86_64" {
		t.Errorf("freebsd-amd64 configures with %q", got)
	}

	if got := strings.Join(record.CFlags, " "); got != "--sysroot=/sysroot" {
		t.Errorf("freebsd-amd64 compiles with the cflags %q", got)
	}

	// Debian's own ld stamps the binary it links as a GNU program, and a FreeBSD kernel is not going to
	// load that; naming lld is what makes the produced ELF say FreeBSD.
	if got := strings.Join(record.LDFlags, " "); got != "-static -fuse-ld=lld --sysroot=/sysroot" {
		t.Errorf("freebsd-amd64 links with the ldflags %q", got)
	}

	// A FreeBSD target's host is not a host this build ever stands on, so its record names no host compile.
	if len(record.Host.CFlags) > 0 || len(record.Host.LDFlags) > 0 || record.Host.Make != "" {
		t.Errorf("freebsd-amd64 names a host compile: cflags %v, ldflags %v, make %q", record.Host.CFlags, record.Host.LDFlags, record.Host.Make)
	}
}

// Nothing on this build host runs a FreeBSD program: the container that compiled the pair runs Debian, Debian
// ships no FreeBSD emulator, and qemu builds the FreeBSD emulator it has only on a host that already is a BSD.
// The record therefore names what the build reads out of the two files the compile produced, in the order it
// reads them, and names no machine the program has to be run on before it is accepted.
func TestARecordThatCannotRunItsProgramNamesWhatItReads(t *testing.T) {
	manifest := ReadManifest()

	record, ok := manifest.Targets["freebsd-amd64"]
	if !ok {
		t.Fatal("the manifest has no record for freebsd-amd64, which this build proves by reading what it compiled")
	}

	want := strings.Join([]string{"elf64", "exec", "x86_64", "osabi_freebsd", "static", "comment"}, " ")

	if got := strings.Join(record.Conformance.Reads, " "); got != want {
		t.Errorf("freebsd-amd64 is proven by reading %q, want %q", got, want)
	}

	if record.Conformance.Container != "" || record.Conformance.Digest != "" || len(record.Conformance.Packages) > 0 || len(record.Conformance.Run) > 0 {
		t.Errorf("freebsd-amd64 names both a way to run its program and a way to read it: container %q, digest %q, packages %v, run %v",
			record.Conformance.Container, record.Conformance.Digest, record.Conformance.Packages, record.Conformance.Run)
	}

	// No record names an operating system the build boots, because the build boots nothing to prove a program:
	// neither the field that named one, nor the image it pinned, nor the site it would have been fetched from
	// is in the file the operator reads.
	body, err := os.ReadFile("manifest.json")
	if err != nil {
		t.Fatalf("the manifest this package embeds is not there to read: %v", err)
	}

	for _, gone := range []string{"freebsd_vm", "qcow2", "download.freebsd.org"} {
		if strings.Contains(string(body), gone) {
			t.Errorf("the manifest names %q, and nothing in this build boots a system image to prove anything", gone)
		}
	}

	// A record names one way of proving the program it compiled: run it in the container that compiled it, run
	// it under an interpreter or an emulation layer, or read what the binary itself says because nothing on the
	// build host can run it. Naming more than one would let a build pick whichever is cheapest and report the
	// other as the proof.
	for target, rec := range manifest.Targets {
		names := 0

		if rec.Conformance.Container != "" || len(rec.Conformance.Run) > 0 {
			names++
		}

		if len(rec.Conformance.Reads) > 0 {
			names++
		}

		if names > 1 {
			t.Errorf("%s names %d ways of proving the program it compiled, and a build that has to choose one will choose the cheapest", target, names)
		}
	}
}

// The manifest is the file the operator reads and commits, and the build writes nothing into it. A field
// naming a program's digest is a field a build would have to fill in, and the build fills in nothing: the
// binaries it adopts are binaries it ran.
func TestTheManifestCarriesNoDigestOfAnyProgram(t *testing.T) {
	body, err := os.ReadFile("manifest.json")
	if err != nil {
		t.Fatalf("the manifest this package embeds is not there to read: %v", err)
	}

	var file map[string]json.RawMessage

	if err := json.Unmarshal(body, &file); err != nil {
		t.Fatalf("the manifest is not readable: %v", err)
	}

	var targets map[string]json.RawMessage

	if err := json.Unmarshal(file["targets"], &targets); err != nil {
		t.Fatalf("the manifest's targets are not readable: %v", err)
	}

	if len(targets) == 0 {
		t.Fatal("the manifest names no targets at all")
	}

	for _, target := range []string{"macos-amd64", "macos-arm64", "freebsd-amd64", "linux-amd64", "linux-arm64", "linux-arm", "linux-386", "windows-amd64"} {
		record, ok := targets[target]
		if !ok {
			t.Errorf("the manifest names no record for %s", target)
			continue
		}

		var fields map[string]json.RawMessage

		if err := json.Unmarshal(record, &fields); err != nil {
			t.Fatalf("%s: the record is not readable: %v", target, err)
		}

		if _, found := fields["programs"]; found {
			t.Errorf("%s carries a programs field: the build has no digest to write into it, and the operator has no digest to commit", target)
		}
	}
}

// What the converter asks of ffmpeg is what the recipe has to keep enabling: leaving one of these out
// is the kind of edit configure accepts and the conversion then fails. The two buffer filters are not on
// this list and the recipe does not name them, because ffmpeg compiles abuffer and abuffersink with no
// configure guard of its own and every build carries them — which is why the pinned release answers
// --enable-filter=abuffer and --enable-filter=abuffersink with "did not match anything".
func TestTheRecipeCarriesWhatTheConverterAsksOfFfmpeg(t *testing.T) {
	recipe := ReadManifest().Recipe

	want := []string{
		"--enable-ffmpeg",
		"--enable-ffprobe",
		"--enable-protocol=fd",
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

// The pinned release reports these three switches as matching no component at all: configure prints
// "Option --enable-... did not match anything" for each one, and the trimmed build converts audio with the
// same codec list and the same recording whether they are carried or not. A switch that selects nothing is
// indistinguishable, to the next reader, from a switch a conversion depends on, so it is not carried.
func TestTheRecipeCarriesNoSwitchThatSelectsNothing(t *testing.T) {
	recipe := ReadManifest().Recipe

	for _, selectsNothing := range []string{
		"--enable-muxer=json",
		"--enable-filter=abuffer",
		"--enable-filter=abuffersink",
	} {
		if hasFlag(recipe, selectsNothing) {
			t.Errorf("the recipe carries %s, which the pinned release reports as matching nothing", selectsNothing)
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

// The recipe is the whole of the audio-only promise this project makes to ffmpeg's build. The pinned
// release has no --disable-video to carry, so the promise is `--disable-everything` plus a list of
// components that are all audio, and `--disable-network`, the switch that keeps libavformat's socket
// layer out of the compiled program. The recipe is the only place the build is told anything, so the
// switches are named here.
func TestTheRecipeSaysAudioOnlyToTheBuild(t *testing.T) {
	recipe := ReadManifest().Recipe

	for _, want := range []string{
		"--disable-everything",
		"--disable-network",
	} {
		if !hasFlag(recipe, want) {
			t.Errorf("the recipe does not carry %s, and the build is not told what this project ships", want)
		}
	}
}

// Nothing in the recipe may enable a video component. The ffmpeg this project ships converts audio, and
// a decoder, encoder, filter, demuxer, muxer, protocol, parser, bitstream filter, hardware accelerator,
// or device that carries video is not the program its licence text describes. The list below is the
// components the converter's own argument list asks of ffmpeg, and it is closed: a component is added
// here with the conversion that needs it, or it is not added.
func TestTheRecipeEnablesNoVideoComponent(t *testing.T) {
	// The rule is shown refusing a video component on a recipe of the test's own, so it is a gate and
	// not a description of the manifest as it happens to read today.
	for _, fixture := range []string{
		"--enable-decoder=h264",
		"--enable-encoder=mpeg4",
		"--enable-filter=crop",
		"--enable-demuxer=mpegts",
		"--enable-muxer=mp4",
		"--enable-protocol=http",
	} {
		if got := theComponentsOutside([]string{fixture}); len(got) == 0 {
			t.Errorf("the rule accepts %s, a component this server does not convert with", fixture)
		}
	}

	for _, name := range theComponentsOutside(ReadManifest().Recipe) {
		t.Errorf("the recipe enables %s, which is not a component this server converts with", name)
	}
}

// theAudioComponents are the components the recipe is allowed to enable: the audio demuxers, decoders,
// encoder, muxers and filters the converter's argument list runs, and the pipe, fd and file protocols
// the server hands the recording in and out on. ffprobe answers with its own json writer and not with a
// muxer the recipe enables, and ffmpeg compiles its buffer filters without a switch, so neither belongs
// on this list.
var theAudioComponents = map[string]bool{
	"protocol=pipe": true,
	"protocol=fd":   true,
	"protocol=file": true,

	"demuxer=wav": true,
	"demuxer=mp3": true,
	"demuxer=mov": true,

	"decoder=pcm_s16le": true,
	"decoder=pcm_s16be": true,
	"decoder=pcm_s24le": true,
	"decoder=pcm_f32le": true,
	"decoder=pcm_mulaw": true,
	"decoder=pcm_alaw":  true,
	"decoder=mp3float":  true,
	"decoder=mp3":       true,
	"encoder=aac":       true,
	"muxer=ipod":        true,
	"filter=aformat":    true,
	"filter=aresample":  true,
	"filter=apad":       true,
	"filter=loudnorm":   true,
}

// theComponentKinds are the component names configure is given with `--enable-<kind>=<name>`. A flag
// that enables something with no name — the programs, the static link — is not a component, and is not
// what this rule is about.
var theComponentKinds = map[string]bool{
	"decoder":  true,
	"encoder":  true,
	"demuxer":  true,
	"muxer":    true,
	"filter":   true,
	"protocol": true,
	"parser":   true,
	"bsf":      true,
	"hwaccel":  true,
	"indev":    true,
	"outdev":   true,
}

// theComponentsOutside names the components a recipe enables that are not on the audio list.
func theComponentsOutside(recipe []string) []string {
	var outside []string

	for _, flag := range recipe {
		kind, name, named := strings.Cut(strings.TrimPrefix(flag, "--enable-"), "=")

		if !named || !theComponentKinds[kind] {
			continue
		}

		if !theAudioComponents[kind+"="+name] {
			outside = append(outside, kind+"="+name)
		}
	}

	return outside
}

func hasFlag(recipe []string, flag string) bool {
	for _, one := range recipe {
		if one == flag {
			return true
		}
	}

	return false
}

// anImagePinnedAtADigest says whether a record names an image at the sum the registry hands back for it, rather
// than at a tag alone: the algorithm, a colon, and the sixty-four lowercase hex digits an image content is
// summed to. A record naming `latest`, or naming nothing, is a record naming wherever that tag points tomorrow.
func anImagePinnedAtADigest(digest string) bool {
	value, named := strings.CutPrefix(digest, "sha256:")

	return named && len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}
