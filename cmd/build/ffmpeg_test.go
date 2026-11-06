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
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// Component tests of what the ffmpeg phase decides: the build says what
// it will run, and refuses what it cannot honour, before running any of
// it. Nothing here compiles anything.

// aManifestWith is the manifest the repository carries, with the parts a test cares about changed.
func aManifestWith(ffmpeg string, recipe []string) bundle.Manifest {
	manifest := bundle.ReadManifest()

	manifest.FFmpeg = ffmpeg
	manifest.Recipe = recipe

	return manifest
}

// anUncompiledTree is a tree that has nothing cached, on a machine that has podman to compile with.
func anUncompiledTree() ffmpegState {
	return ffmpegState{program: podmanProgram}
}

// aManifestWithOtherTarget is the repository's manifest with one target's
// record replaced, which is how a test asks what would move if a record
// moved. The struct copies by value but shares its targets map.
func aManifestWithOtherTarget(target string, record bundle.Target) bundle.Manifest {
	manifest := bundle.ReadManifest()

	targets := map[string]bundle.Target{}
	for name, rec := range manifest.Targets {
		targets[name] = rec
	}

	targets[target] = record

	manifest.Targets = targets

	return manifest
}

func stepNamed(plan ffmpegPlan, label string) (step, bool) {
	for _, s := range plan.Steps {
		if s.label == label {
			return s, true
		}
	}

	return step{}, false
}

func TestTheBuildCompilesAnFfmpegForTheTargetItIsAskedFor(t *testing.T) {
	o, err := parseArgs([]string{"ffmpeg"})
	if err != nil {
		t.Fatalf("the build refused its own ffmpeg command: %v", err)
	}

	if o.sub != subFFmpeg {
		t.Errorf("the command is %q, want %s", o.sub, subFFmpeg)
	}

	if o.target != "" {
		t.Errorf("with no target named the tool recorded %q, want the target left to be decided", o.target)
	}

	o, err = parseArgs([]string{"ffmpeg", "linux-arm64"})
	if err != nil {
		t.Fatalf("the build refused a target it knows: %v", err)
	}

	if o.target != "linux-arm64" {
		t.Errorf("the build builds for %s, want linux-arm64", o.target)
	}

	// The target an operator did not name is the one this tool is running on, which is what makes a
	// developer's plain build carry an ffmpeg too.
	plan, err := planFFmpeg(options{}, "/repo", bundle.ReadManifest(), anUncompiledTree(), theReleaseHost)
	if err != nil {
		t.Fatalf("the build refused the target it is running on: %v", err)
	}

	if plan.Target != runtimeTarget() {
		t.Errorf("with no target named the build compiles for %s, want %s", plan.Target, runtimeTarget())
	}
}

func TestTheBuildRefusesACommandLineThatNamesTwoThings(t *testing.T) {
	if _, err := parseArgs([]string{"ffmpeg", "server"}); err == nil {
		t.Error("the build accepted ffmpeg and server as one command")
	}
}

func TestTheBuildRefusesATargetItCannotName(t *testing.T) {
	if _, err := parseArgs([]string{"ffmpeg", "arm64"}); err == nil {
		t.Error("the build accepted arm64, which is not a target this tool can name")
	}

	if _, err := parseArgs([]string{"ffmpeg", "--pre", "beta1", "linux-arm64"}); err == nil {
		t.Error("the build accepted a target that was not the thing it was asked to build")
	}
}

// A target the manifest has no record for is not a release target this
// project ships: asking for it says there is nothing to compile.
func TestATargetTheManifestHasNoRecordForProducesNothing(t *testing.T) {
	for _, target := range []string{"netbsd-amd64", "openbsd-amd64"} {
		plan, err := planFFmpeg(options{target: target}, t.TempDir(), bundle.ReadManifest(), anUncompiledTree(), theReleaseHost)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}

		if len(plan.Steps) != 0 {
			t.Errorf("%s has no manifest record and the build still has %d steps for it", target, len(plan.Steps))
		}

		if plan.Says == "" {
			t.Errorf("%s carries no ffmpeg and the build says nothing about it", target)
		}
	}
}

// The container, the toolchain packages and the cross-compile arguments come from the manifest, so
// adding a target is writing a record and not writing Go.
func TestTheFfmpegBuildUsesTheContainerAndToolchainTheManifestRecords(t *testing.T) {
	manifest := bundle.ReadManifest()
	record := manifest.Targets["linux-arm64"]

	plan, err := planFFmpeg(options{target: "linux-arm64"}, "/repo", manifest, anUncompiledTree(), theReleaseHost)
	if err != nil {
		t.Fatalf("the build refused linux-arm64: %v", err)
	}

	image, ok := stepNamed(plan, "image")
	if !ok {
		t.Fatal("the build has no step that prepares the build container")
	}

	if !carries(image.args, record.Container) {
		t.Errorf("the build container is not %s", record.Container)
	}

	for _, pkg := range record.Packages {
		if !carries(image.args, pkg) {
			t.Errorf("the build container does not install %s", pkg)
		}
	}

	configure, ok := stepNamed(plan, "configure")
	if !ok {
		t.Fatal("the build has no step that configures the source")
	}

	for _, flag := range record.Cross {
		if !hasArg(configure.args, flag) {
			t.Errorf("configure does not pass %s", flag)
		}
	}

	for _, flag := range manifest.Recipe {
		if !hasArg(configure.args, flag) {
			t.Errorf("configure does not pass %s", flag)
		}
	}

	if _, ok := stepNamed(plan, "make"); !ok {
		t.Error("the build has no step that compiles the source")
	}
}

// What each container target is compiled with, and where its compiled
// programs are checked, is pinned one target at a time here, written
// out rather than read back off the manifest, containers at digests.
// see the ffmpeg-check-component-tests maintainer note.
func TestEveryContainerTargetProvesWhatItCompiledInWhatItsRecordNames(t *testing.T) {
	manifest := bundle.ReadManifest()

	cases := []struct {
		target      string
		container   string
		from        string // the Containerfile the toolchain image is built from: empty is the shared one
		installs    []string
		sysrootFrom string // the image a sysroot is copied out of: empty names none
		compiler    string // what configure is told to compile with: empty names no compiler at all
		cross       []string
		cflags      string
		ldflags     string
		archiver    string   // what configure writes its libraries with: empty is configure's own choice
		indexer     string   // what writes the symbol table of those archives: empty is configure's own
		unstripped  bool     // the programs keep what the link wrote: false is configure's own strip step
		checked     string   // checks in this container: empty is the container that compiled them
		checkedWith []string // what that container installs
		reads       []string // what the plan reads out of the pair instead of running it: empty is a run
	}{
		{
			target:      "freebsd-amd64",
			container:   "docker.io/debian:stable@sha256:defb09e17b8c7326f9a3b63ecf0797f34674691f5c270d63987b955ff0168bd8",
			from:        "Containerfile.ffmpeg.freebsd",
			installs:    []string{"build-essential", "clang", "lld", "curl", "ca-certificates", "tar", "xz-utils", "python3"},
			sysrootFrom: "docker.io/freebsd/freebsd-toolchain:15.1@sha256:6b28def54592452cdcd4161b62e529a5964c038631efba5e2902a40614c65c45",
			compiler:    "clang --target=x86_64-unknown-freebsd --sysroot=/sysroot",
			cross:       []string{"--enable-cross-compile", "--target-os=freebsd", "--arch=x86_64"},
			cflags:      "--sysroot=/sysroot",
			ldflags:     "-static -fuse-ld=lld --sysroot=/sysroot",
			reads:       []string{"elf64", "exec", "x86_64", "osabi_freebsd", "static", "comment"},
		},
		{
			target:    "linux-386",
			container: "docker.io/debian:stable@sha256:defb09e17b8c7326f9a3b63ecf0797f34674691f5c270d63987b955ff0168bd8",
			installs:  []string{"build-essential", "gcc-multilib", "libc6-dev-i386", "qemu-user-static"},
			cflags:    "-m32",
			ldflags:   "-static -m32",
		},
		{
			target:    "linux-amd64",
			container: "docker.io/debian:stable@sha256:defb09e17b8c7326f9a3b63ecf0797f34674691f5c270d63987b955ff0168bd8",
			installs:  []string{"build-essential"},
			ldflags:   "-static",
		},
		{
			target:    "linux-arm",
			container: "docker.io/debian:stable@sha256:defb09e17b8c7326f9a3b63ecf0797f34674691f5c270d63987b955ff0168bd8",
			installs:  []string{"build-essential", "gcc-arm-linux-gnueabihf", "libc6-dev-armhf-cross", "qemu-user-static"},
			cross:     []string{"--enable-cross-compile", "--target-os=linux", "--arch=arm", "--cross-prefix=arm-linux-gnueabihf-"},
			ldflags:   "-static",
		},
		{
			target:    "linux-arm64",
			container: "docker.io/debian:stable@sha256:defb09e17b8c7326f9a3b63ecf0797f34674691f5c270d63987b955ff0168bd8",
			installs:  []string{"build-essential", "gcc-aarch64-linux-gnu", "libc6-dev-arm64-cross", "qemu-user-static"},
			cross:     []string{"--enable-cross-compile", "--target-os=linux", "--arch=aarch64", "--cross-prefix=aarch64-linux-gnu-"},
			ldflags:   "-static",
		},
		{
			target:      "windows-amd64",
			container:   "docker.io/alpine:latest@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6",
			installs:    []string{"build-base", "mingw-w64-gcc"},
			cross:       []string{"--enable-cross-compile", "--target-os=win64", "--arch=x86_64", "--cross-prefix=x86_64-w64-mingw32-"},
			ldflags:     "-static -static-libgcc",
			checked:     "docker.io/debian:stable@sha256:defb09e17b8c7326f9a3b63ecf0797f34674691f5c270d63987b955ff0168bd8",
			checkedWith: []string{"wine"},
		},
		{
			target:     "macos-amd64",
			container:  "docker.io/debian:stable@sha256:defb09e17b8c7326f9a3b63ecf0797f34674691f5c270d63987b955ff0168bd8",
			installs:   []string{"build-essential", "clang", "lld", "llvm", "curl", "ca-certificates", "tar", "xz-utils", "python3"},
			compiler:   "clang --target=x86_64-apple-darwin20 --sysroot=/sysroot",
			cross:      []string{"--enable-cross-compile", "--target-os=darwin", "--arch=x86_64"},
			cflags:     "-mmacosx-version-min=11.0 --sysroot=/sysroot",
			ldflags:    "-mmacosx-version-min=11.0 -fuse-ld=lld --sysroot=/sysroot",
			archiver:   "llvm-ar",
			indexer:    "llvm-ranlib",
			unstripped: true,
			reads:      []string{"mh_execute", "lc_main", "lc_load_dylinker", "lc_load_libsystem"},
		},
		{
			target:     "macos-arm64",
			container:  "docker.io/debian:stable@sha256:defb09e17b8c7326f9a3b63ecf0797f34674691f5c270d63987b955ff0168bd8",
			installs:   []string{"build-essential", "clang", "lld", "llvm", "curl", "ca-certificates", "tar", "xz-utils", "python3"},
			compiler:   "clang --target=arm64-apple-darwin20 --sysroot=/sysroot",
			cross:      []string{"--enable-cross-compile", "--target-os=darwin", "--arch=aarch64"},
			cflags:     "-mmacosx-version-min=11.0 --sysroot=/sysroot",
			ldflags:    "-mmacosx-version-min=11.0 -fuse-ld=lld --sysroot=/sysroot",
			archiver:   "llvm-ar",
			indexer:    "llvm-ranlib",
			unstripped: true,
			reads:      []string{"mh_execute", "lc_main", "lc_load_dylinker", "lc_load_libsystem", "lc_code_signature"},
		},
	}

	for _, c := range cases {
		plan, err := planFFmpeg(options{target: c.target}, "/repo", manifest, aTreeReadyFor(t, manifest, c.target), theReleaseHost)
		if err != nil {
			t.Errorf("the build refused %s: %v", c.target, err)
			continue
		}

		image, ok := stepNamed(plan, "image")
		if !ok {
			t.Errorf("%s has no step that prepares the container it compiles in", c.target)
			continue
		}

		if plan.Native {
			t.Errorf("%s says it is compiled on the machine running this build, and %s is compiled in a container",
				c.target, c.target)
		}
		if plan.Host {
			t.Errorf("%s takes the host route — its own compile, an adoption, or a cache entry this machine made — and its record names a container that compiles it",
				c.target)
		}

		wantFile := c.from
		if wantFile == "" {
			wantFile = "Containerfile.ffmpeg"
		}
		if file := theFlagValue(image.args, "-f"); file != wantFile {
			t.Errorf("%s builds its toolchain image from %q, want %s", c.target, file, wantFile)
		}

		built := containerBuildArgs(image.args)
		if built["base"] != c.container {
			t.Errorf("%s compiles in %q, want %s", c.target, built["base"], c.container)
		}
		if built["packages"] != strings.Join(c.installs, " ") {
			t.Errorf("%s installs %q, want %s", c.target, built["packages"], strings.Join(c.installs, " "))
		}

		if c.sysrootFrom == "" {
			if named, has := built["sysroot_image"]; has {
				t.Errorf("%s copies a sysroot out of %q, which its record does not name: %v",
					c.target, named, image.args)
			}
		} else if built["sysroot_image"] != c.sysrootFrom {
			t.Errorf("%s copies its sysroot out of %q, want %s: the compile is not the one its record prepares",
				c.target, built["sysroot_image"], c.sysrootFrom)
		}

		runner, hasRunner := stepNamed(plan, "runner")
		if c.checked == "" {
			if hasRunner {
				t.Errorf("%s prepares a second container, %s, which its record never asked for: %v",
					c.target, containerBuildArgs(runner.args)["base"], runner.args)
			}
			if plan.Runner != plan.Image {
				t.Errorf("%s checks the programs it compiled in %s, want the build container %s",
					c.target, plan.Runner, plan.Image)
			}
		} else if !hasRunner {
			t.Errorf("%s has no step that prepares %s, the container its programs are checked in", c.target, c.checked)
		} else {
			checked := containerBuildArgs(runner.args)
			if checked["base"] != c.checked {
				t.Errorf("%s checks its programs in %q, want %s", c.target, checked["base"], c.checked)
			}
			if checked["packages"] != strings.Join(c.checkedWith, " ") {
				t.Errorf("%s checks its programs in a container installing %q, want %s",
					c.target, checked["packages"], strings.Join(c.checkedWith, " "))
			}
			if plan.Runner == plan.Image {
				t.Errorf("%s would check its programs in the container that compiled them", c.target)
			}
		}

		// What proves the compiled pair is what the record names, and the plan carries that answer: a target
		// that is read is never also run, and its compile step says which of the two the release ships.
		if named := manifest.Targets[c.target].Conformance.Reads; !sameArgs(named, c.reads) {
			t.Errorf("%s's record names %v as what to read out of its compiled pair, want %v", c.target, named, c.reads)
		}

		if !sameArgs(plan.Reads, c.reads) {
			t.Errorf("%s plans to read %v out of the pair it compiles, want %v", c.target, plan.Reads, c.reads)
		}

		make, ok := stepNamed(plan, "make")
		if !ok {
			t.Errorf("%s has no step that compiles the pair", c.target)
			continue
		}

		notRun := strings.Contains(make.result, "compiled and not run")
		if len(c.reads) > 0 && !notRun {
			t.Errorf("%s compiles a pair nothing on this machine can start, and its compile step says %q", c.target, make.result)
		}

		if len(c.reads) == 0 && notRun {
			t.Errorf("%s says %q, and what follows in that run does start that pair", c.target, make.result)
		}

		configure, ok := stepNamed(plan, "configure")
		if !ok {
			t.Errorf("%s has no step that configures the source", c.target)
			continue
		}

		// A target's own flags come after the recipe: what makes the compile cross, then the flags it links
		// with. Cross flags where none were chosen, or a link flag that is not the one recorded, is a
		// different compile than the one this release answers for.
		want := []string{}
		want = append(want, c.cross...)
		if c.compiler != "" {
			want = append(want, "--cc="+c.compiler)
		}
		if c.cflags != "" {
			want = append(want, "--extra-cflags="+c.cflags)
		}
		if c.ldflags != "" {
			want = append(want, "--extra-ldflags="+c.ldflags)
		}
		if c.archiver != "" {
			want = append(want, "--ar="+c.archiver)
		}
		if c.indexer != "" {
			want = append(want, "--ranlib="+c.indexer)
		}
		if c.unstripped {
			want = append(want, "--disable-stripping")
		}

		got := configure.args
		if len(got) > len(want) {
			got = got[len(got)-len(want):]
		}
		if !sameArgs(got, want) {
			t.Errorf("%s ends configure with %v, want %v", c.target, got, want)
		}

		// A target whose record names no compiler gets ffmpeg's own default, and a --cc= it was not told to
		// have is a compiler nobody chose.
		if c.compiler == "" {
			for _, arg := range configure.args {
				if strings.HasPrefix(arg, "--cc=") {
					t.Errorf("%s configures with %q, which its record does not name", c.target, arg)
				}
			}
		}
	}
}

// The machine a build runs on is not part of what a container target is
// compiled with: the container carries the target's own compiler and
// headers, and a macOS sysroot is mounted inside the container.
// see the targets-and-platforms maintainer note.
func TestEveryContainerTargetIsCompiledInAContainerWhateverHostRunsTheBuild(t *testing.T) {
	manifest := bundle.ReadManifest()

	macHosts := []host{{goos: "darwin", goarch: "arm64"}, {goos: "darwin", goarch: "amd64"}}

	cases := []struct {
		target   string
		machines []host
	}{
		// A FreeBSD toolchain belongs to no host this build ever stands on, and this is the target whose plan
		// used to ask the host for one.
		{target: "freebsd-amd64", machines: append(append([]host{theReleaseHost}, macHosts...), host{goos: "linux", goarch: "arm64"})},

		// A Mac is the one host a macOS target's ffmpeg is compiled on with its own toolchain, and every other
		// host — including a Mac of the other architecture — compiles it in a container. Those hosts are here.
		{target: "macos-amd64", machines: []host{theReleaseHost, {goos: "linux", goarch: "arm64"}}},
		{target: "macos-arm64", machines: []host{theReleaseHost}},

		// A Mac producing a release does not compile a Linux or a Windows ffmpeg with the tools it happens to
		// have, and this is the state the host route used to be keyed on a manifest field instead of on.
		{target: "linux-amd64", machines: macHosts},
		{target: "linux-arm64", machines: macHosts},
		{target: "windows-amd64", machines: macHosts},
	}

	for _, c := range cases {
		record := manifest.Targets[c.target]

		wantFile := record.Containerfile
		if wantFile == "" {
			wantFile = "Containerfile.ffmpeg"
		}

		for _, machine := range c.machines {
			plan, err := planFFmpeg(options{target: c.target}, "/repo", manifest, aTreeReadyFor(t, manifest, c.target), machine)
			if err != nil {
				t.Errorf("the build refused %s on a %s %s host: %v", c.target, machine.goos, machine.goarch, err)
				continue
			}

			if plan.Native {
				t.Errorf("%s on a %s %s host says it is compiled with that host's own toolchain, and %s is compiled in a container",
					c.target, machine.goos, machine.goarch, c.target)
			}

			if plan.Host {
				t.Errorf("%s on a %s %s host takes the host route, and the host standing here is not the platform a %s ffmpeg is compiled on",
					c.target, machine.goos, machine.goarch, c.target)
			}

			image, ok := stepNamed(plan, "image")
			if !ok {
				t.Errorf("%s on a %s %s host has no step that prepares the container it compiles in",
					c.target, machine.goos, machine.goarch)
				continue
			}

			built := containerBuildArgs(image.args)

			if file := theFlagValue(image.args, "-f"); file != wantFile {
				t.Errorf("%s on a %s %s host builds its toolchain image from %q, want %s",
					c.target, machine.goos, machine.goarch, file, wantFile)
			}

			if built["base"] != record.Container+"@"+record.ContainerDigest {
				t.Errorf("%s on a %s %s host compiles in %q, want the container its record names at the digest it pins, %s@%s",
					c.target, machine.goos, machine.goarch, built["base"], record.Container, record.ContainerDigest)
			}

			// The image installs what its record names. A target whose pair is started by an interpreter, and
			// started in the container that compiled it because no other container is named, carries that
			// interpreter in the same image: an arm64 pair checked on a build host that is not arm64 needs the
			// translator in the place the pair is run.
			wantInstalled := append([]string{}, record.Packages...)

			if record.Conformance.Container == "" {
				wantInstalled = append(wantInstalled, record.Conformance.Packages...)
			}

			if built["packages"] != strings.Join(wantInstalled, " ") {
				t.Errorf("%s on a %s %s host installs %q, want %s",
					c.target, machine.goos, machine.goarch, built["packages"], strings.Join(wantInstalled, " "))
			}

			sysrootImage, hasSysroot := built["sysroot_image"]

			if record.SysrootImage == "" && hasSysroot {
				t.Errorf("%s on a %s %s host copies a sysroot out of %q, which its record does not name: %v",
					c.target, machine.goos, machine.goarch, sysrootImage, image.args)
			}

			if record.SysrootImage != "" && sysrootImage != record.SysrootImage+"@"+record.SysrootDigest {
				t.Errorf("%s on a %s %s host copies its sysroot out of %q, want %s at the digest its record pins, %s",
					c.target, machine.goos, machine.goarch, sysrootImage, record.SysrootImage, record.SysrootDigest)
			}

			// What proves the pair travels with the record and not with the machine: a pair no host in this list
			// can start is read out of the binaries the compile produced.
			if !sameArgs(plan.Reads, record.Conformance.Reads) {
				t.Errorf("%s on a %s %s host proves the pair by reading %v, want %v",
					c.target, machine.goos, machine.goarch, plan.Reads, record.Conformance.Reads)
			}

			make, ok := stepNamed(plan, "make")
			if !ok {
				t.Errorf("%s on a %s %s host has no step that compiles the pair", c.target, machine.goos, machine.goarch)
				continue
			}

			notRun := strings.Contains(make.result, "compiled and not run")

			if len(record.Conformance.Reads) > 0 && !notRun {
				t.Errorf("%s on a %s %s host compiles a pair nothing on that host can start, and its compile step says %q",
					c.target, machine.goos, machine.goarch, make.result)
			}

			if len(record.Conformance.Reads) == 0 && notRun {
				t.Errorf("%s on a %s %s host says %q, and what follows in that run does start that pair",
					c.target, machine.goos, machine.goarch, make.result)
			}
		}
	}
}

// A record names one proof. A record naming two is refused rather than planned: the cheaper proof is the one a
// build under pressure ends up making, and the release would carry a pair wearing a claim nobody chose.
func TestARecordNamingBothARunAndAReadIsRefused(t *testing.T) {
	manifest := bundle.ReadManifest()

	record := manifest.Targets["freebsd-amd64"]
	record.Conformance.Run = []string{"/work/ffmpeg-9.0.2/ffmpeg", "-version"}

	manifest.Targets["freebsd-amd64"] = record

	_, err := planFFmpeg(options{target: "freebsd-amd64"}, "/repo", manifest, anUncompiledTree(), theReleaseHost)
	if err == nil {
		t.Fatal("the build planned a target whose record names both a run and a read")
	}

	if !strings.Contains(err.Error(), "will not guess") {
		t.Errorf("the refusal %q does not say it refuses to guess which proof the record means", err.Error())
	}
}

// A record's packages are the container distribution's own builds, so a record that names no machine for them
// is compiled with whatever that distribution happens to carry on the container host: no gcc-multilib, no
// libc6-dev-i386, and a binutils that cannot read what the cross compiler wrote. The build finds that out at
// the end of a compile that has already spent its time, so it asks the question before planning anything.
func TestARecordNamingNoToolchainPlatformIsRefused(t *testing.T) {
	record := bundle.ReadManifest().Targets["freebsd-amd64"]
	record.CompilePlatform = ""

	manifest := aManifestWithOtherTarget("freebsd-amd64", record)

	_, err := planFFmpeg(options{target: "freebsd-amd64"}, "/repo", manifest, anUncompiledTree(), theReleaseHost)
	if err == nil {
		t.Fatal("the build planned a target whose record names no platform for its toolchain")
	}

	if !strings.Contains(err.Error(), "freebsd-amd64") || !strings.Contains(err.Error(), "compile_platform") {
		t.Errorf("the refusal %q does not name the target and the field that is missing", err.Error())
	}
}

// A record's toolchain is the distribution's builds for one machine, so the image that carries them has to be
// built on that machine and not on whatever machine the container host's daemon happens to be. The build hands
// the record's platform to the image build; a build that does not is at the mercy of the host's default.
func TestTheToolchainImageIsBuiltOnThePlatformTheRecordNames(t *testing.T) {
	manifest := bundle.ReadManifest()

	for _, target := range theContainerCompiledTargets {
		record := manifest.Targets[target]

		plan, err := planFFmpeg(options{target: target}, "/repo", manifest, aTreeReadyFor(t, manifest, target), theReleaseHost)
		if err != nil {
			t.Fatalf("%s: the build refused its container target: %v", target, err)
		}

		image, ok := stepNamed(plan, "image")
		if !ok {
			t.Fatalf("%s: the build has no step that prepares the build container", target)
		}

		if got := containerBuildArgs(image.args)["compile_platform"]; got != record.CompilePlatform {
			t.Errorf("%s: the toolchain image is built with compile_platform %q, want the %s record's %q: %v",
				target, got, target, record.CompilePlatform, image.args)
		}

		if runner, ok := stepNamed(plan, "runner"); ok {
			if got := containerBuildArgs(runner.args)["compile_platform"]; got != record.CompilePlatform {
				t.Errorf("%s: the container that checks the compiled pair is built with compile_platform %q, want the %s record's %q: %v",
					target, got, target, record.CompilePlatform, runner.args)
			}
		}
	}
}

// the container-compiled targets of the shipped manifest: every one of them compiles in a container on the
// release host, which is the host the release runs on.
var theContainerCompiledTargets = []string{
	"freebsd-amd64",
	"linux-386",
	"linux-amd64",
	"linux-arm",
	"linux-arm64",
	"macos-amd64",
	"macos-arm64",
	"windows-amd64",
}

// Two containerfiles compile the shipped targets today. A stage that names no platform is built for the machine
// the container host is, which is a different toolchain wearing the same target's name, so each of these files
// has to take the platform from the build the same way it takes the base image and the packages.
func TestEveryFfmpegContainerfileNamesThePlatformItsCompileRunsOn(t *testing.T) {
	for _, file := range []string{"Containerfile.ffmpeg", "Containerfile.ffmpeg.freebsd"} {
		text, err := os.ReadFile(filepath.Join("..", "..", file))
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}

		contents := string(text)

		if !strings.Contains(contents, "ARG compile_platform=") {
			t.Errorf("%s declares no compile_platform build argument, so the build cannot tell it which machine to compile on", file)
		}

		for _, line := range strings.Split(contents, "\n") {
			stage := strings.TrimSpace(line)
			if !strings.HasPrefix(stage, "FROM ") {
				continue
			}

			if strings.Contains(stage, "${base}") && !strings.Contains(stage, "--platform=${compile_platform}") {
				t.Errorf("%s starts a stage on the base image with no platform: %q. The base is a multi-architecture "+
					"image, so that stage is built for the container host's own machine, and its distribution's packages "+
					"are that machine's builds", file, stage)
			}
		}

		// The FreeBSD sysroot stage is the one stage whose platform is not the compile's: it is the operating
		// system the compile produces, and it reaches the compile as files, never as a machine that runs.
		if file == "Containerfile.ffmpeg.freebsd" && !strings.Contains(contents, "FROM --platform=freebsd/amd64 ${sysroot_image}") {
			t.Errorf("%s no longer takes its sysroot from the operating system the compile is for", file)
		}
	}
}

// containerBuildArgs is the --build-arg of a step that prepares a container, keyed by what it sets.
func containerBuildArgs(args []string) map[string]string {
	out := map[string]string{}

	for i, arg := range args {
		if arg != "--build-arg" || i+1 >= len(args) {
			continue
		}

		if key, value, ok := strings.Cut(args[i+1], "="); ok {
			out[key] = value
		}
	}

	return out
}

// A Windows build has no ffmpeg make target: the rule is %$(PROGSSUF)$(EXESUF), and make ffmpeg
// fails with "No rule to make target". Plain make builds both programs.
func TestTheFfmpegBuildRunsPlainMakeForAWindowsTarget(t *testing.T) {
	plan, err := planFFmpeg(options{target: "windows-amd64"}, "/repo", bundle.ReadManifest(), anUncompiledTree(), theReleaseHost)
	if err != nil {
		t.Fatalf("the build refused windows-amd64: %v", err)
	}

	make, ok := stepNamed(plan, "make")
	if !ok {
		t.Fatal("the build has no step that compiles the source")
	}

	if hasArg(make.args, "ffmpeg") {
		t.Errorf("the Windows build asks make for the ffmpeg target it does not have: %v", make.args)
	}
}

// A covered target is a promise: the build does not quietly produce a server that will have to do
// without an ffmpeg.
func TestTheBuildRefusesToCompileAnFfmpegItHasNoToolchainFor(t *testing.T) {
	_, err := planFFmpeg(options{target: "linux-arm64"}, "/repo", bundle.ReadManifest(), ffmpegState{}, theReleaseHost)
	if err == nil {
		t.Fatal("the build reported no problem compiling linux-arm64 without a container engine")
	}

	if !strings.Contains(err.Error(), "podman") {
		t.Errorf("the build says %q, which does not name what is missing", err)
	}
}

// ffmpeg.org publishes no checksum, only the recorded digest can verify the source.
// see the ffmpeg-source-digest-anchor maintainer note.
func TestTheBuildRefusesASourceTarballThatIsNotTheReleaseItPinned(t *testing.T) {
	dir := t.TempDir()

	path := filepath.Join(dir, "ffmpeg-9.0.2.tar.xz")

	if err := os.WriteFile(path, []byte("something else entirely"), 0644); err != nil {
		t.Fatalf("the test cannot write the source it was given: %v", err)
	}

	err := checkSource(path, bundle.ReadManifest())
	if err == nil {
		t.Fatal("the build accepted a source tarball that is not the release it pinned")
	}

	if !strings.Contains(err.Error(), "8c3850283eb25fa026482078a04051e0be17347b09ef81a0849bec15a96e002e") {
		t.Errorf("the build says %q, which does not name the digest it expected", err)
	}
}

func TestTheBuildAcceptsTheSourceTarballItPinned(t *testing.T) {
	manifest := bundle.ReadManifest()

	body := []byte("the release this project pinned")

	manifest.Source.SHA256 = sha256Of(body)

	path := filepath.Join(t.TempDir(), "ffmpeg-9.0.2.tar.xz")

	if err := os.WriteFile(path, body, 0644); err != nil {
		t.Fatalf("the test cannot write the source it pinned: %v", err)
	}

	if err := checkSource(path, manifest); err != nil {
		t.Errorf("the build refused the source it pinned: %v", err)
	}
}

// Everything the ffmpeg compile reads is part of the name of the cache
// entry its binaries land in: the release, source, recipe, target, and
// what its record names. What proves a pair is not among them: a cache
// hit re-runs no proof, and a proof name would drop warm entries.
func TestTheFfmpegCacheKeyChangesWithEverythingThatChangesTheCompile(t *testing.T) {
	manifest := bundle.ReadManifest()

	same := cacheKey(manifest, "linux-arm64")

	if changed := cacheKey(manifest, "linux-arm64"); changed != same {
		t.Errorf("the same manifest and target gave two cache keys, %s and %s", same, changed)
	}

	byTarget := cacheKey(manifest, "linux-arm")
	if byTarget == same {
		t.Error("linux-arm and linux-arm64 share a cache key")
	}

	byVersion := cacheKey(aManifestWith("9.0.3", manifest.Recipe), "linux-arm64")
	if byVersion == same {
		t.Error("a new ffmpeg release reuses the previous release's cache")
	}

	byRecipe := cacheKey(aManifestWith(manifest.FFmpeg, append(append([]string{}, manifest.Recipe...), "--enable-filter=volume")), "linux-arm64")
	if byRecipe == same {
		t.Error("a changed configure recipe reuses the previous recipe's cache")
	}

	// The source is part of what the compiled binaries are. A cache that survives the digest being changed
	// is a cache that can be handed a tarball this project never pinned.
	withSource := manifest
	withSource.Source = bundle.Source{URL: manifest.Source.URL, SHA256: strings.Repeat("0", 64)}

	if cacheKey(withSource, "linux-arm64") == same {
		t.Error("a different pinned source digest reuses the previous source's cache")
	}

	record := manifest.Targets["linux-arm64"]
	record.Cross = []string{"--enable-cross-compile", "--arch=aarch64"}

	if cacheKey(aManifestWithOtherTarget("linux-arm64", record), "linux-arm64") == same {
		t.Error("a changed toolchain reuses the previous toolchain's cache")
	}

	// The digest a record pins is one of the things its compile reads, exactly as the image's name is: an image
	// pinned at another digest is another toolchain, and a warm entry carried over from the old one is a pair
	// built with compilers nobody named.
	record = manifest.Targets["linux-arm64"]
	record.ContainerDigest = "sha256:" + strings.Repeat("a", 64)

	if cacheKey(aManifestWithOtherTarget("linux-arm64", record), "linux-arm64") == same {
		t.Error("a record naming another digest for the container it compiles in reuses the cache the image named by the old digest built")
	}

	// A FreeBSD compile's own three fields: the file its toolchain image is built from, the image its sysroot is
	// copied out of, and the compiler invocation that reaches configure. Reusing the warm entry the old one
	// produced would ship a binary built against a sysroot or a toolchain nobody named.
	freebsd := cacheKey(manifest, "freebsd-amd64")

	record = manifest.Targets["freebsd-amd64"]
	record.Containerfile = "Containerfile.ffmpeg"

	if cacheKey(aManifestWithOtherTarget("freebsd-amd64", record), "freebsd-amd64") == freebsd {
		t.Error("a FreeBSD compile asked of the shared Containerfile reuses the cache of the one asked of the file that carries a FreeBSD sysroot")
	}

	// The machine the toolchain runs on is one of the things the compile reads, because a distribution's cross
	// compilers are packages of one machine: a record naming another machine describes compilers that install,
	// or do not install, and an entry warmed under the old one holds binaries built with toolchains nobody named.
	record = manifest.Targets["freebsd-amd64"]
	record.CompilePlatform = "linux/arm64"

	if cacheKey(aManifestWithOtherTarget("freebsd-amd64", record), "freebsd-amd64") == freebsd {
		t.Error("a record naming another machine for its toolchain reuses the cache of the pair the old machine's toolchain built")
	}

	record = manifest.Targets["freebsd-amd64"]
	record.SysrootImage = "docker.io/freebsd/freebsd-toolchain:15.0"

	if cacheKey(aManifestWithOtherTarget("freebsd-amd64", record), "freebsd-amd64") == freebsd {
		t.Error("a FreeBSD record naming another sysroot image reuses the cache of the binaries the old sysroot built")
	}

	record = manifest.Targets["freebsd-amd64"]
	record.SysrootDigest = "sha256:" + strings.Repeat("b", 64)

	if cacheKey(aManifestWithOtherTarget("freebsd-amd64", record), "freebsd-amd64") == freebsd {
		t.Error("a FreeBSD record naming another digest for the image its sysroot is copied out of reuses the cache the sysroot named by the old digest built")
	}

	// The container a record names when its pair is proved by running it travels in the key with the container,
	// the packages, and the program this build runs that proof with. For most targets that container is the one
	// that compiled the pair, and its proof tools are installed inside it; where a record names a separate one,
	// moving its digest moves the entry too, which is the conservative side of the rule: the environment a pair
	// was proved in is named by the entry that holds that pair.
	record = manifest.Targets["windows-amd64"]
	record.Conformance.Digest = "sha256:" + strings.Repeat("c", 64)

	if cacheKey(aManifestWithOtherTarget("windows-amd64", record), "windows-amd64") == cacheKey(manifest, "windows-amd64") {
		t.Error("a record naming another digest for the container its pair is proved in reuses the cache of the proof ran in the container named by the old digest")
	}

	record = manifest.Targets["freebsd-amd64"]
	record.CC = "clang"

	if cacheKey(aManifestWithOtherTarget("freebsd-amd64", record), "freebsd-amd64") == freebsd {
		t.Error("a FreeBSD compile whose compiler names no target triple reuses the cache of the one that named one")
	}

	// What proves a pair is not a compile input: a cache hit re-runs no proof, and a proof name in the key
	// would throw away the entry a compile that changed nothing produced.
	record = manifest.Targets["linux-arm64"]
	record.Conformance.Reads = []string{"elf64", "exec"}

	if cacheKey(aManifestWithOtherTarget("linux-arm64", record), "linux-arm64") != same {
		t.Error("naming what to read out of a pair moves the cache entry its unchanged compile produced")
	}

	// A macOS compile reads a sysroot somebody prepared outside both repositories, so that tree is one of the
	// inputs its entry is named by: an SDK version moved, or a tree re-prepared from another source, is a
	// different pair of binaries wearing the same target's name.
	darwin := cacheKey(manifest, "macos-arm64")

	record = manifest.Targets["macos-arm64"]
	record.Sysroot.SDK = "15.4"

	if cacheKey(aManifestWithOtherTarget("macos-arm64", record), "macos-arm64") == darwin {
		t.Error("a macOS compile asked of another SDK reuses the cache of the binaries the pinned SDK built")
	}

	record = manifest.Targets["macos-arm64"]
	record.Sysroot.SHA256 = strings.Repeat("0", 64)

	if cacheKey(aManifestWithOtherTarget("macos-arm64", record), "macos-arm64") == darwin {
		t.Error("a macOS compile asked of a re-prepared tree reuses the cache of the binaries the tree this record pins built")
	}

	record = manifest.Targets["macos-arm64"]
	record.Sysroot.Generator = "macos-cross deadbeef"

	if cacheKey(aManifestWithOtherTarget("macos-arm64", record), "macos-arm64") == darwin {
		t.Error("a macOS compile asked of a tree another revision prepared reuses the cache of the binaries the pinned preparation built")
	}

	// The host block is the compile a Mac runs with its own toolchain, and it is part of the name too: the two
	// compiles of one macOS target land in one entry, and each one's inputs move it.
	record = manifest.Targets["macos-arm64"]
	record.Host.LDFlags = append(append([]string{}, record.Host.LDFlags...), "-mmacosx-version-min=12.0")

	if cacheKey(aManifestWithOtherTarget("macos-arm64", record), "macos-arm64") == darwin {
		t.Error("a macOS host compile whose flags changed reuses the cache of the binaries the old flags built")
	}

	record = manifest.Targets["macos-arm64"]
	record.Host.Make = "another-make"

	if cacheKey(aManifestWithOtherTarget("macos-arm64", record), "macos-arm64") == darwin {
		t.Error("a macOS host compile running another make program reuses the cache of the binaries the old one built")
	}

	record = manifest.Targets["macos-arm64"]
	record.LDFlags = append(append([]string{}, record.LDFlags...), "-Wl,-z,now")

	if cacheKey(aManifestWithOtherTarget("macos-arm64", record), "macos-arm64") == darwin {
		t.Error("a macOS container compile whose link flags changed reuses the cache of the binaries the old flags linked")
	}

	// The archiver and the indexer are compile inputs the way the compiler is: a pair whose libraries were
	// archived by another tool is another pair of binaries, whatever the target's name says.
	record = manifest.Targets["macos-arm64"]
	record.AR = "ar"

	if cacheKey(aManifestWithOtherTarget("macos-arm64", record), "macos-arm64") == darwin {
		t.Error("a macOS compile asked of another archiver reuses the cache of the pair its record's archiver wrote")
	}

	record = manifest.Targets["macos-arm64"]
	record.Ranlib = "ranlib"

	if cacheKey(aManifestWithOtherTarget("macos-arm64", record), "macos-arm64") == darwin {
		t.Error("a macOS compile asked of another indexer reuses the cache of the pair whose archives its record's indexer indexed")
	}

	// Keeping the programs exactly as its link wrote them is an input the way the tools are: a pair that went
	// through a strip step is another pair of binaries than a pair that did not, whatever the target's name says.
	record = manifest.Targets["macos-arm64"]
	record.NoStripping = false

	if cacheKey(aManifestWithOtherTarget("macos-arm64", record), "macos-arm64") == darwin {
		t.Error("a macOS compile told to strip its programs reuses the cache of the pair its record says ships as its link wrote them")
	}

	// The mount path is not its own input: the record already spells it into the compiler invocation and the
	// flags, and those are in the key.
	record = manifest.Targets["macos-arm64"]
	record.Sysroot.Mount = "/opt/sysroot"

	if cacheKey(aManifestWithOtherTarget("macos-arm64", record), "macos-arm64") != darwin {
		t.Error("a record naming only another mount path for the same sysroot and the same flags moves the cache it warmed")
	}

	// The two macOS targets differ only by the architecture their records tell the compile to produce, so their
	// keys have to differ.
	if cacheKey(manifest, "macos-amd64") == cacheKey(manifest, "macos-arm64") {
		t.Error("macos-amd64 and macos-arm64 share a cache key, and one machine compiling both writes one entry")
	}

	// The eight cache keys are pinned by value: a warm cache is why a
	// release run takes minutes, and a manifest gaining fields must not
	// drop the entries an unchanged compile produced.
	// see the ffmpeg-cache-key-inputs maintainer note.
	want := map[string]string{
		"freebsd-amd64": "0c3fe1cc043cf2e23badfc2e2cd27fb2ff45799a41e83585e625d9d88ab2cfdb",
		"linux-386":     "4fa360ed32fc35aac0bdf1c71af8c0c91b5bacef74f21fc63621860ba2cb26f2",
		"linux-amd64":   "ee9978d311d5295b68bf9f127e46f57d4bd09718ea825d9e23af501fe7e618f4",
		"linux-arm":     "b930c19847ec5113a62b6c586dd4ba6fcf024991bb49eb1a5e0041a957635a0e",
		"linux-arm64":   "54502ed7df6cb70c8e62b0846194e9a3a22228b37cb1008bde0feb8cc7bb2c4f",
		"macos-amd64":   "4589b1ae72c2119c0d5b22ecac97db6a534442b5b1c8af0dd481becd2748372e",
		"macos-arm64":   "24dce1ba6cec79937d8e8943ef1706b869086351c127e387c54789faa3823a4a",
		"windows-amd64": "3b080ebdd130d75caf4a3414213b2cddc12a0bf248ae7069983dd866a3a6d105",
	}

	for _, target := range []string{"freebsd-amd64", "linux-386", "linux-amd64", "linux-arm", "linux-arm64", "macos-amd64", "macos-arm64", "windows-amd64"} {
		if got := cacheKey(manifest, target); got != want[target] {
			t.Errorf("%s's cache key is %s, want %s: something the compile reads has moved, and the warm entries move with it — when the move is the recipe or a record, record the key it computes now", target, got, want[target])
		}
	}
}

// A second build that changes nothing does not compile anything again.
func TestAFfmpegAlreadyCompiledForTheSameRecipeIsNotCompiledAgain(t *testing.T) {
	plan, err := planFFmpeg(
		options{target: "linux-arm64"},
		"/repo",
		bundle.ReadManifest(),
		ffmpegState{program: podmanProgram, bundleIsCached: true},
		theReleaseHost,
	)
	if err != nil {
		t.Fatalf("the build refused linux-arm64: %v", err)
	}

	if len(plan.Steps) != 0 {
		t.Errorf("a cached ffmpeg is compiled again: %d steps", len(plan.Steps))
	}

	if plan.Says == "" {
		t.Error("the build reuses a compiled ffmpeg and says nothing about it")
	}

	if !plan.Reuses {
		t.Error("the plan that compiles nothing from a cache entry does not say that it reuses one")
	}
}

// A cache entry is only as good as the record of how its binaries were proved. The pair and a
// licence written by a run that never looked at them are not a proof: the entry is left alone and
// the pair is compiled again.
func TestACachedPairWithNoLicenceWrittenByItsOwnRunIsNotReused(t *testing.T) {
	repo := t.TempDir()

	aCacheEntryFor(t, repo, "freebsd-amd64", "")

	compiled := false

	tl, out := aBuildCompilingInAContainer(t, repo, "freebsd-amd64", func(t *testing.T, dir string, name string) {
		compiled = true
		anELFProgramWrittenTo(t, dir, name, theMeasuredFreeBSDPair)
	})

	if _, err := tl.runFFmpeg(options{target: "freebsd-amd64"}, out); err != nil {
		t.Fatalf("the build refused the target: %v\n%s", err, out.String())
	}

	if !compiled {
		t.Error("the build reused a cache entry that names no proof of the pair it holds")
	}

	said := out.String()

	if strings.Contains(said, "reusing") {
		t.Errorf("the build reused a cache entry that names no proof of the pair it holds:\n%s", said)
	}

	if !strings.Contains(said, "no proof") {
		t.Errorf("the build did not say the cache entry names no proof of the pair it holds:\n%s", said)
	}
}

// When an entry does carry the licence the compiling run wrote, that text is what the release
// publishes: the run that only reused the pair composes nothing of its own.
func TestTheLicencePublishedForAReusedPairIsTheTextStoredWithThatPair(t *testing.T) {
	repo := t.TempDir()

	const proved = "proved on the machine that compiled this pair, and nobody else"

	aCacheEntryFor(t, repo, "freebsd-amd64", proved)

	tl, out := aBuildCompilingInAContainer(t, repo, "freebsd-amd64", func(t *testing.T, dir string, name string) {
		anELFProgramWrittenTo(t, dir, name, theMeasuredFreeBSDPair)
	})

	if _, err := tl.runFFmpeg(options{target: "freebsd-amd64"}, out); err != nil {
		t.Fatalf("the build refused the target: %v\n%s", err, out.String())
	}

	body, err := os.ReadFile(filepath.Join(ffmpegPathsFor(repo, bundle.ReadManifest(), "freebsd-amd64").bundle, "LICENCE.md"))
	if err != nil {
		t.Fatalf("reading the licence the build published: %v\n%s", err, out.String())
	}

	if string(body) != proved {
		t.Errorf("the published licence is not the text stored beside the pair:\n%s", body)
	}
}

// A run that reaches an entry whose licence text has gone cannot compose words about a compile it
// did not see, so the entry is named and the run stops.
func TestACacheEntryWithNoLicenceStoredCannotSayHowItsPairWasProved(t *testing.T) {
	repo := t.TempDir()

	aCacheEntryFor(t, repo, "freebsd-amd64", "")

	paths := ffmpegPathsFor(repo, bundle.ReadManifest(), "freebsd-amd64")

	_, err := theLicenceInTheCache(paths, "freebsd-amd64")
	if err == nil {
		t.Fatal("a run read a cache entry that stores no proof of its pair and said nothing")
	}

	if !strings.Contains(err.Error(), paths.cached) {
		t.Errorf("the refusal %q does not name the entry it found", err.Error())
	}

	if !strings.Contains(err.Error(), "no licence") {
		t.Errorf("the refusal %q does not say the entry stores no licence text", err.Error())
	}
}

// aCacheEntryFor seeds the cache entry one target's recipe keys: the gzipped pair, and the licence
// text the run that compiled them wrote beside those binaries. An empty licence leaves the entry
// holding the pair with nothing that says how those binaries were proved.
func aCacheEntryFor(t *testing.T, repo string, target string, licence string) {
	t.Helper()

	manifest := bundle.ReadManifest()

	paths := ffmpegPathsFor(repo, manifest, target)

	if err := os.MkdirAll(paths.cached, 0o755); err != nil {
		t.Fatalf("making the cache entry for %s: %v", target, err)
	}

	fixture := thePairThisBuildCompilesForLinux
	if manifest.Targets[target].Sysroot != (bundle.Sysroot{}) {
		fixture = theMeasuredFreeBSDPair
	}

	raw := theELFProgramBytes(fixture)

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		gzipped := &bytes.Buffer{}

		writer := gzip.NewWriter(gzipped)
		if _, err := writer.Write(raw); err != nil {
			t.Fatalf("gzipping the cached %s: %v", name, err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("closing the gzip of the cached %s: %v", name, err)
		}

		if err := os.WriteFile(paths.cachedProgram(name+".gz"), gzipped.Bytes(), 0o644); err != nil {
			t.Fatalf("writing the cached %s: %v", name, err)
		}
	}

	if licence != "" {
		if err := os.WriteFile(filepath.Join(paths.cached, "LICENCE.md"), []byte(licence), 0o644); err != nil {
			t.Fatalf("writing the licence beside the cached pair: %v", err)
		}
	}
}

// The source is fetched once and kept: a release tarball already there
// does not have to be downloaded again.
func TestTheBuildFetchesTheSourceOnlyWhenItDoesNotHaveIt(t *testing.T) {
	plan, err := planFFmpeg(options{target: "linux-arm64"}, "/repo", bundle.ReadManifest(), anUncompiledTree(), theReleaseHost)
	if err != nil {
		t.Fatalf("the build refused linux-arm64: %v", err)
	}

	if plan.Source.Path == "" {
		t.Fatal("the build does not know where the ffmpeg source is kept")
	}

	if !plan.Source.Fetch {
		t.Error("a tree with no cached source does not plan to fetch one")
	}

	plan, err = planFFmpeg(
		options{target: "linux-arm64"},
		"/repo",
		bundle.ReadManifest(),
		ffmpegState{program: podmanProgram, sourceIsCached: true, bundleIsCached: true},
		theReleaseHost,
	)
	if err != nil {
		t.Fatalf("the build refused linux-arm64: %v", err)
	}

	if plan.Source.Fetch {
		t.Error("the build plans to fetch a source it already has")
	}
}

// The ffmpeg check reaches the compiled ffmpeg through the container it
// was built in, and the container has to be named before the program:
// otherwise podman reads the program as the image to run.
func TestTheFFMPEGCheckNamesTheContainerItRunsTheCompiledFfmpegIn(t *testing.T) {
	argv := containerRun(
		theContainerProgram{name: podmanProgram},
		"rdio-ffmpeg:linux-arm64-0123456789ab",
		"/repo/.build/ffmpeg/scratch/linux-arm64",
		[]string{"qemu-aarch64", "/work/ffmpeg-9.0.2/ffmpeg", "-i", "-"},
	)

	if argv[0] != "podman" || argv[1] != "run" {
		t.Fatalf("the check does not run the compiled ffmpeg in a container: %v", argv)
	}

	for i, arg := range argv {
		if arg != "qemu-aarch64" {
			continue
		}

		if argv[i-1] != "rdio-ffmpeg:linux-arm64-0123456789ab" {
			t.Errorf("the container is not named before the program it runs: %v", argv)
		}

		break
	}

	if !carries(argv, "/repo/.build/ffmpeg/scratch/linux-arm64:/work") {
		t.Error("the check does not mount the directory the compiled ffmpeg was built in")
	}
}

// A release archive has to say what ffmpeg is inside it, under what
// licence, and compiled from what, all written from the manifest itself.
func TestTheBuildSaysWhatTheFfmpegItCompiledIsAndUnderWhatLicence(t *testing.T) {
	manifest := bundle.ReadManifest()

	licence := ffmpegLicence(manifest, "linux-arm64")

	for _, want := range []string{
		"Lesser General Public License",
		manifest.FFmpeg,
		manifest.Source.URL,
		manifest.Source.SHA256,
		"--enable-filter=loudnorm",
		"go run ./cmd/build ffmpeg linux-arm64",
	} {
		if !strings.Contains(licence, want) {
			t.Errorf("the licence text does not name %s", want)
		}
	}

	// The licence posture is a promise this feature makes, and the licence text is only true while the
	// recipe keeps it.
	for _, flag := range manifest.Recipe {
		if strings.HasPrefix(flag, "--enable-gpl") || strings.HasPrefix(flag, "--enable-nonfree") {
			t.Errorf("the recipe carries %s, which is not the LGPL-only posture the licence text states", flag)
		}
	}

	if !strings.Contains(licence, "without --enable-gpl") {
		t.Error("the licence text does not say what was left out")
	}

	// The targets this build proves by reading and not by running are the ones whose archive is the only place
	// a reader learns it: the step result says the pair was compiled and not run, and the text inside the
	// archive has to say the same about the pair it carries. The claim is made of the proof the record names,
	// so a target whose proof was a run of its own binaries in a container never carries it.
	for _, one := range releaseTargets {
		target := one.targetWord()
		record, pinned := manifest.Targets[target]

		if !pinned {
			continue
		}

		text := ffmpegLicence(manifest, target)
		provenByReading := len(record.Conformance.Reads) > 0
		saysItWasNotRun := strings.Contains(text, "compiled and not run")

		if provenByReading && !saysItWasNotRun {
			t.Errorf("%s is proven by reading, and its licence text does not say its pair was compiled and not run:\n%s", target, text)
		}

		if saysItWasNotRun && !provenByReading {
			t.Errorf("%s is proven by running its own binaries in a container, and its licence text says its pair was compiled and not run:\n%s", target, text)
		}

		// The archive is the only place a reader of a release finds out what compiled the pair inside it, and
		// the record now pins that toolchain at a digest. The text names the reference the build handed the
		// container program: the name the record writes, at the digest it pins, and the file the image was
		// built from.
		if !strings.Contains(text, record.Container+"@"+record.ContainerDigest) {
			t.Errorf("%s's licence text does not name the container it was compiled in at the digest its record pins, %s@%s:\n%s",
				target, record.Container, record.ContainerDigest, text)
		}

		containerfile := record.Containerfile
		if containerfile == "" {
			containerfile = "Containerfile.ffmpeg"
		}

		if !strings.Contains(text, containerfile) {
			t.Errorf("%s's licence text does not name the %s its toolchain image was built from:\n%s", target, containerfile, text)
		}

		// A pair linked against another operating system's headers, libraries and startup files has to say which
		// tree those came from, and the record names it: the FreeBSD project's toolchain image at its digest for
		// one target, the SDK version with the digest its preparation recorded for the two macOS targets.
		if record.SysrootImage != "" && !strings.Contains(text, record.SysrootImage+"@"+record.SysrootDigest) {
			t.Errorf("%s's licence text does not name the image its sysroot was copied out of at its digest, %s@%s:\n%s",
				target, record.SysrootImage, record.SysrootDigest, text)
		}

		if record.Sysroot != (bundle.Sysroot{}) {
			for _, want := range []string{record.Sysroot.SDK, record.Sysroot.SHA256, record.Sysroot.Generator} {
				if !strings.Contains(text, want) {
					t.Errorf("%s's licence text does not name %s about the SDK tree it was cross-compiled against:\n%s", target, want, text)
				}
			}
		}

		// How the pair was proven is what its record describes, and the text says the same thing the build does:
		// started in the container that compiled it, started under the runner the record names, started under a
		// runner in the separate container the record names at its digest, or never started, with what was read
		// out of it named in the order the record names it.
		if provenByReading {
			at := 0

			for _, read := range record.Conformance.Reads {
				found := strings.Index(text[at:], read)

				if found < 0 {
					t.Errorf("%s's licence text does not name the %s it reads out of the pair in the order its record names them:\n%s",
						target, read, text)
					break
				}

				at += found + len(read)
			}

			if !strings.Contains(text, "No run recorded") {
				t.Errorf("%s's pair is never started, and its licence text does not say no run recorded what it registers:\n%s", target, text)
			}
		} else {
			if strings.Contains(text, "No run recorded") {
				t.Errorf("%s's pair is started by this build, and its licence text says no run recorded what it registers:\n%s", target, text)
			}

			if len(record.Conformance.Run) == 0 && record.Conformance.Container == "" && !strings.Contains(text, "the container that compiled them") {
				t.Errorf("%s's pair is started in the container that compiled it, and its licence text does not say so:\n%s", target, text)
			}

			for _, runner := range record.Conformance.Run {
				if !strings.Contains(text, runner) {
					t.Errorf("%s's pair is started under %s, and its licence text does not name it:\n%s", target, runner, text)
				}
			}

			if record.Conformance.Container != "" {
				pinned := record.Conformance.Container + "@" + record.Conformance.Digest

				if !strings.Contains(text, pinned) {
					t.Errorf("%s's pair is proved in %s, and its licence text does not name that container at its digest:\n%s", target, pinned, text)
				}
			}
		}

		// The codecs the notice names are the ones the recipe selects, because a reused compile starts the pair
		// nothing and so cannot report what a program registered: the recipe is the one place the codecs a
		// release promises are written down.
		for _, flag := range manifest.Recipe {
			codec := strings.TrimPrefix(flag, "--enable-decoder=")

			if codec == flag {
				codec = strings.TrimPrefix(flag, "--enable-encoder=")
			}

			if codec == flag {
				continue
			}

			if !strings.Contains(text, codec) {
				t.Errorf("%s's licence text does not name the codec %s its recipe selects:\n%s", target, codec, text)
			}
		}
	}

	// Two mutations show the new sentences are composed from the record and not typed out for the targets this
	// release happens to ship today: a runner the record changes is a runner the text follows, and a read the
	// record drops is a read the text stops promising.
	changed := bundle.ReadManifest()
	record := changed.Targets["linux-amd64"]
	record.Conformance.Run = []string{"qemu-x86_64"}
	changed.Targets["linux-amd64"] = record

	if text := ffmpegLicence(changed, "linux-amd64"); !strings.Contains(text, "qemu-x86_64") {
		t.Error("the licence text for linux-amd64 does not follow the runner its record names, so it is written for today's records and not from them")
	}

	changed = bundle.ReadManifest()
	record = changed.Targets["freebsd-amd64"]
	record.Conformance.Reads = []string{"elf64", "exec", "x86_64", "static", "comment"}
	changed.Targets["freebsd-amd64"] = record

	if text := ffmpegLicence(changed, "freebsd-amd64"); strings.Contains(text, "osabi_freebsd") {
		t.Error("the licence text names osabi_freebsd for a record that stopped naming it as something the build reads")
	}

	// And the codecs: the notice names what the recipe selects, so a recipe that stops selecting an encoder
	// stops the notice promising it.
	changed.Recipe = aRecipeWithout(changed.Recipe, "--enable-encoder=aac")

	if text := ffmpegLicence(changed, "freebsd-amd64"); strings.Contains(text, "aac") {
		t.Error("the licence text promises an aac encoder to a recipe that stopped enabling one")
	}
}

// The licence text says the program is compiled for audio, and the
// switches that make that true are the recipe's. The text is composed
// from the recipe, so a recipe that drops a switch drops it from the
// notice too: the archive never promises a build it no longer produces.
func TestTheLicenceSaysTheFfmpegIsCompiledForAudioOnly(t *testing.T) {
	manifest := bundle.ReadManifest()

	licence := ffmpegLicence(manifest, "linux-arm64")

	for _, want := range []string{
		"compiled for audio",
		"no video codec, video decoder, or video encoder is compiled in",
		"no network protocol is compiled in",
	} {
		if !strings.Contains(licence, want) {
			t.Errorf("the licence text does not say %q:\n%s", want, licence)
		}
	}

	dropped := bundle.ReadManifest()
	dropped.Recipe = aRecipeWithout(dropped.Recipe, "--disable-network")

	if strings.Contains(ffmpegLicence(dropped, "linux-arm64"), "no network protocol is compiled in") {
		t.Error("the licence text promises a build with no network protocol to a recipe that dropped --disable-network")
	}

	dropped = bundle.ReadManifest()
	dropped.Recipe = aRecipeWithout(dropped.Recipe, "--disable-everything")

	if strings.Contains(ffmpegLicence(dropped, "linux-arm64"), "no video codec") {
		t.Error("the licence text promises a build with no video codec to a recipe that dropped --disable-everything")
	}
}

// aRecipeWithout is the recipe with one switch taken out, which shows a
// licence text promising a build its own recipe does not produce.
func aRecipeWithout(recipe []string, flag string) []string {
	kept := []string{}

	for _, one := range recipe {
		if one == flag {
			continue
		}

		kept = append(kept, one)
	}

	return kept
}

// A reused compile leaves the same tree a fresh one does, licence text
// included: what a release archive carries cannot depend on a warm cache.
func TestAReusedFfmpegIsPublishedTheWayAFreshOneIs(t *testing.T) {
	repo := t.TempDir()
	manifest := bundle.ReadManifest()

	paths := ffmpegPathsFor(repo, manifest, "linux-arm64")

	if err := os.MkdirAll(paths.cached, 0755); err != nil {
		t.Fatalf("the test cannot prepare the cache: %v", err)
	}

	for _, name := range []string{"ffmpeg.gz", "ffprobe.gz"} {
		if err := os.WriteFile(filepath.Join(paths.cached, name), []byte("compiled"), 0644); err != nil {
			t.Fatalf("the test cannot fill the cache: %v", err)
		}
	}

	if err := publishFFmpeg(paths, manifest, "linux-arm64", ffmpegLicence(manifest, "linux-arm64")); err != nil {
		t.Fatalf("the build refused to publish a compiled ffmpeg: %v", err)
	}

	for _, name := range []string{"ffmpeg.gz", "ffprobe.gz", "LICENCE.md"} {
		if !aFileIsThere(filepath.Join(paths.bundle, name)) {
			t.Errorf("%s is not in the bundle directory the server embeds", name)
		}
	}
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}

	return false
}

// carries reports whether a command line names something anywhere in it,
// how a build argument is checked: the container rides inside --build-arg.
func carries(args []string, want string) bool {
	for _, arg := range args {
		if strings.Contains(arg, want) {
			return true
		}
	}

	return false
}

// aDropOfSuppliedBinaries drops the binaries a maintainer builds for one
// target and returns the repository's manifest. Nothing is recorded about
// them: the run that adopts them is the run that proves them.
func aDropOfSuppliedBinaries(t *testing.T, repo string, target string) bundle.Manifest {
	t.Helper()

	body := aSuppliedProgramBytes(target)

	return aDropOfSuppliedBinariesWith(t, repo, target, body, append(append([]byte{}, body...), byte(len(target))))
}

// aDropOfSuppliedBinariesWith drops binaries whose programs are the bytes a test asks for. The plan reads which
// machine code they hold, so a test that wants the plan to reach the run hands it the target's own shape.
func aDropOfSuppliedBinariesWith(t *testing.T, repo string, target string, ffmpegBytes []byte, ffprobeBytes []byte) bundle.Manifest {
	t.Helper()

	dir := filepath.Join(repo, "internal", "infrastructure", "ffmpeg", "bundle", target)

	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("making the bundle directory: %v", err)
	}

	// The directory a clone carries: the placeholder that makes the embed compile, and nothing else.
	if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), nil, 0644); err != nil {
		t.Fatalf("writing the placeholder: %v", err)
	}

	for name, body := range map[string][]byte{"ffmpeg": ffmpegBytes, "ffprobe": ffprobeBytes} {
		if err := os.WriteFile(filepath.Join(dir, name+programSuffix(target)), body, 0755); err != nil {
			t.Fatalf("dropping the supplied %s: %v", name, err)
		}
	}

	return bundle.ReadManifest()
}

// A drop is read by the same rule that reads a compiled pair, and an Apple
// Silicon Mac runs arm64 and x86-64 alike, so only the header says the
// Intel archive carries Intel code. The refusal names the architecture.
func TestTheBuildRefusesSuppliedBinariesOfTheOtherArchitecture(t *testing.T) {
	repo := t.TempDir()

	intel := machOBytes(aSystemProgram(theX8664CPU))

	manifest := aDropOfSuppliedBinariesWith(t, repo, "macos-arm64", intel, intel)

	_, err := planFFmpeg(options{target: "macos-arm64"}, repo, manifest, aNativeTree("macos-arm64"), aMachineForTarget("macos-arm64"))
	if err == nil {
		t.Fatal("the build adopted x86-64 binaries for macos-arm64, which runs arm64")
	}

	if !strings.Contains(err.Error(), "arm64") || !strings.Contains(err.Error(), "x86-64") {
		t.Errorf("the failure %q does not name both architectures", err.Error())
	}
}

// A pair dropped into a target's bundle directory is adopted by the
// targets this build adopts on: the two macOS targets on a Mac. On the
// release host a macOS target compiles in a container, its drop is not
// read, and every server built there embeds it beside the gzipped pair.
func TestADroppedPairIsAdoptedOnlyForAMacOSTargetOnAMacHost(t *testing.T) {
	for _, target := range []string{"macos-amd64", "macos-arm64"} {
		repo := t.TempDir()

		manifest := aDropOfSuppliedBinaries(t, repo, target)

		onTheBuildHost, err := planFFmpeg(options{target: target}, repo, manifest, aTreeReadyFor(t, manifest, target), theReleaseHost)
		if err != nil {
			t.Fatalf("%s: the build refused the macOS target its record says it compiles: %v", target, err)
		}

		if onTheBuildHost.Adopts {
			t.Errorf("%s: the machine that produces the releases adopted binaries it did not compile, when its own record names the container that compiles them", target)
		}

		if _, ok := stepNamed(onTheBuildHost, "make"); !ok {
			t.Errorf("%s: the build compiled no pair on the machine that compiles it: %d steps", target, len(onTheBuildHost.Steps))
		}

		onItsOwnHost, err := planFFmpeg(options{target: target}, repo, manifest, aNativeTree(target), aMachineForTarget(target))
		if err != nil {
			t.Fatalf("%s: the build refused the binaries dropped for it on the host that is the target: %v", target, err)
		}

		if !onItsOwnHost.Adopts {
			t.Errorf("%s: the host standing on the target adopted nothing, and the binaries are sitting in the directory it embeds", target)
		}
	}
}

// Binaries already in the cache are not adopted again: the cache is keyed
// by the manifest's record and the target, so a warm entry is what this
// machine's own run produced.
func TestSuppliedBinariesAlreadyInCacheAreNotAdoptedAgain(t *testing.T) {
	repo := t.TempDir()

	manifest := aDropOfSuppliedBinaries(t, repo, "macos-arm64")

	paths := ffmpegPathsFor(repo, manifest, "macos-arm64")

	if err := adoptSuppliedFFmpeg(paths, "macos-arm64", suppliedFFMpegLicence(manifest, "macos-arm64", aRunOfTheBinaries())); err != nil {
		t.Fatalf("adopting the binaries: %v", err)
	}

	state := aNativeTree("macos-arm64")
	state.bundleIsCached = true

	plan, err := planFFmpeg(options{target: "macos-arm64"}, repo, manifest, state, aMachineForTarget("macos-arm64"))
	if err != nil {
		t.Fatalf("the build refused cached supplied binaries: %v", err)
	}

	if len(plan.Steps) != 0 {
		t.Errorf("cached supplied binaries are adopted again: %d steps", len(plan.Steps))
	}

	if plan.Native {
		t.Error("the build compiles binaries its own cache already holds")
	}

	if plan.Adopts {
		t.Error("the plan adopts binaries the cache already holds, so the run looks for a drop it does not need")
	}

	if plan.Says == "" {
		t.Error("the build says nothing about reusing supplied binaries")
	}
}

// The drop is consumed: the binaries move out of the directory a server
// embeds, leaving the gzipped ones and their licence. Raw binaries left
// there are embedded as well, ten megabytes per built server.
func TestSuppliedBinariesAreAdoptedIntoTheDirectoryTheServerEmbeds(t *testing.T) {
	repo := t.TempDir()

	manifest := aDropOfSuppliedBinaries(t, repo, "macos-arm64")

	paths := ffmpegPathsFor(repo, manifest, "macos-arm64")

	licence := suppliedFFMpegLicence(manifest, "macos-arm64", aRunOfTheBinaries())

	digests := map[string]string{}

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		body, err := os.ReadFile(suppliedDropFor(paths, "macos-arm64")[name])
		if err != nil {
			t.Fatalf("reading the dropped %s: %v", name, err)
		}

		digests[name] = sha256Of(body)
	}

	if err := adoptSuppliedFFmpeg(paths, "macos-arm64", licence); err != nil {
		t.Fatalf("adopting the binaries: %v", err)
	}

	if err := publishFFmpeg(paths, manifest, "macos-arm64", licence); err != nil {
		t.Fatalf("publishing the binaries: %v", err)
	}

	names := []string{}

	entries, err := os.ReadDir(paths.bundle)
	if err != nil {
		t.Fatalf("reading the bundle directory: %v", err)
	}

	for _, entry := range entries {
		names = append(names, entry.Name())

		if entry.Name() == "ffmpeg" || entry.Name() == "ffprobe" {
			t.Errorf("%s is left in the directory the server embeds: //go:embed all: embeds every file in it", entry.Name())
		}
	}

	for _, want := range []string{".gitkeep", "ffmpeg.gz", "ffprobe.gz", "LICENCE.md"} {
		if !theNamesCarry(names, want) {
			t.Errorf("the bundle directory does not carry %s: %v", want, names)
		}
	}

	// The licence text travels with the cache entry: a run reusing the entry publishes the words the run
	// that compiled the binaries wrote, and not words it composes about a compile it did not take part in.
	if !aFileIsThere(filepath.Join(paths.cached, "LICENCE.md")) {
		t.Errorf("the cache entry %s carries no licence text, so a reusing run has nothing to put in the archive", paths.cached)
	}

	// The binaries the server writes out of its own binary are the binaries
	// that were dropped: gzip is lossless, and the ad-hoc code signature is a
	// hash over those exact bytes.
	for _, program := range []struct {
		name   string
		digest string
	}{
		{"ffmpeg", digests["ffmpeg"]},
		{"ffprobe", digests["ffprobe"]},
	} {
		for _, dir := range []string{paths.cached, paths.bundle} {
			body, err := readGzip(filepath.Join(dir, program.name+".gz"))
			if err != nil {
				t.Fatalf("reading %s in %s: %v", program.name+".gz", dir, err)
			}

			if sha256Of(body) != program.digest {
				t.Errorf("%s in %s is %s, and the binaries dropped were %s", program.name, dir, sha256Of(body), program.digest)
			}
		}
	}
}

// aRunOfTheBinaries is the run of a working build: the target host, the
// codecs the audio-only recipe registers, and the server's own recordings.
func aRunOfTheBinaries() theRunOfTheBinaries {
	return theRunOfTheBinaries{
		Host:        "darwin-arm64",
		Version:     "ffmpeg version 9.0.2 Copyright (c) 2000-2025 the FFmpeg developers",
		Codecs:      codecRegistry{Audio: theAudioCodecNames},
		Conversions: 6,
		Productions: []production{
			{Input: "wav", Mode: "plain", Bytes: 7675, Duration: "2.128000"},
			{Input: "wav", Mode: "norm", Bytes: 12251, Duration: "3.010667"},
			{Input: "wav", Mode: "loudnorm", Bytes: 12131, Duration: "3.010667"},
			{Input: "mp3", Mode: "plain", Bytes: 7756, Duration: "2.128000"},
			{Input: "mp3", Mode: "norm", Bytes: 12251, Duration: "3.010667"},
			{Input: "mp3", Mode: "loudnorm", Bytes: 12131, Duration: "3.010667"},
		},
	}
}

// The licence a supplied archive carries names the host that compiled the
// binaries, the source and recipe, the command that built them, and the
// codecs the run recorded. It names no program digest: the build checks
// nothing beyond that they run and which machine code they hold.
func TestTheBuildSaysWhatSuppliedBinariesAreAndUnderWhatLicence(t *testing.T) {
	manifest := bundle.ReadManifest()

	run := aRunOfTheBinaries()

	text := suppliedFFMpegLicence(manifest, "macos-arm64", run)

	for _, want := range []string{
		"Lesser General Public License",
		manifest.FFmpeg,
		manifest.Source.URL,
		manifest.Source.SHA256,
		manifest.Recipe[0],
		"compiled for audio",
		"go run ./cmd/build ffmpeg macos-arm64",
		"compiled on",
		"macos-arm64",
		run.Host,
		theAudioCodecNames[0],
		"no video codec",
		"6 recordings",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the licence text for supplied binaries names no %q:\n%s", want, text)
		}
	}

	for _, not := range []string{
		"programs",
		"ffmpeg  sha256",
		"compiled from source for macos-arm64. It converts",
	} {
		if strings.Contains(text, not) {
			t.Errorf("the licence text says %q, which is not true of binaries this build did not compile in a container:\n%s", not, text)
		}
	}
}

// A cache entry older than the licence text travelling with it says only
// what the manifest says, and names this machine's cache as the source.
func TestTheLicenceOfBinariesReusedFromCacheNamesNoRunItDidNotSee(t *testing.T) {
	manifest := bundle.ReadManifest()

	text := suppliedFFMpegLicence(manifest, "macos-arm64", theRunOfTheBinaries{})

	for _, want := range []string{
		"the machine that compiled them",
		manifest.Source.SHA256,
		"macos-arm64",
		"reuses what that run published in its own cache",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the licence text for cached binaries names no %q:\n%s", want, text)
		}
	}

	for _, not := range []string{
		"darwin-arm64",
		"recordings",
		"The run that adopted them asked",
	} {
		if strings.Contains(text, not) {
			t.Errorf("the licence text says %q, which is a run this build did not witness:\n%s", not, text)
		}
	}
}

func theNamesCarry(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}

	return false
}

func readGzip(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

// The target an operator who named none is building for is the release
// target this machine belongs to, named the way the release names it:
// macos-arm64 on a Mac, the target the manifest records, not the
// darwin-arm64 name go builds for.
func TestTheFFmpegTargetOfARunThatNamedNoTargetIsTheMachinesReleaseTarget(t *testing.T) {
	want := releaseTargetWordFor(runtime.GOOS, runtime.GOARCH)

	if got := ffmpegTarget(options{}); got != want {
		t.Errorf("with no target named the ffmpeg phase builds for %q, want the release target %q of this %s-%s machine", got, want, runtime.GOOS, runtime.GOARCH)
	}
}

// The host macOS targets compile with the host toolchain: the manifest names
// source, digest, recipe, host flags, make program, and the codesign step,
// and the flags passed are the record's host flags only: a Mac has no
// /sysroot and links with the linker Apple ships.
func TestTheMacOSTargetOnItsOwnHostCompilesItsOwnBinaries(t *testing.T) {
	for _, target := range []string{"macos-amd64", "macos-arm64"} {
		manifest := bundle.ReadManifest()

		plan, err := planFFmpeg(options{target: target}, t.TempDir(), manifest, aNativeTree(target), aMachineForTarget(target))
		if err != nil {
			t.Errorf("%s: the build refused to compile the binaries on the host that is the target: %v", target, err)
			continue
		}

		if !plan.Native {
			t.Errorf("%s: the plan does not compile the binaries on the host that is the target", target)
			continue
		}

		if !plan.Host {
			t.Errorf("%s: the plan does not say this machine runs the compile, and the run that carries a plan out is chosen by that", target)
		}

		// Stripping rewrites the binary, so the signature goes on after it: a signature over the bytes
		// strip removed is a signature macOS refuses to run.
		want := []string{"unpack", "configure", "make", "strip", "sign"}

		if got := stepLabels(plan.Steps); !sameWords(got, want) {
			t.Errorf("%s: the compile runs %v, want %v", target, got, want)
		}

		for _, s := range plan.Steps {
			if s.name == "podman" {
				t.Errorf("%s: the compile of binaries built on this host runs a container: %v", target, s.args)
			}
		}

		// The container route names the image it compiles in and the runner it proves under, and this plan is
		// not that route: a host plan naming either is a plan a run could carry into a container it has no
		// reason to be in, and the host route's whole point is that it is not a container compile.
		if plan.Image != "" || plan.Runner != "" {
			t.Errorf("%s: the host compile names image %q and runner %q, and the host route has no container in it", target, plan.Image, plan.Runner)
		}

		if !plan.Source.Fetch {
			t.Errorf("%s: the compile plans no source to fetch", target)
		}

		if plan.Source.URL != manifest.Source.URL {
			t.Errorf("%s: the compile fetches %s, want the source the manifest pins, %s", target, plan.Source.URL, manifest.Source.URL)
		}

		configure := stepFor(plan.Steps, "configure")

		if configure.name == "" {
			t.Fatalf("%s: the compile plans no configure", target)
		}

		for _, flag := range manifest.Recipe {
			if !carries(configure.args, flag) {
				t.Errorf("%s: the configure does not carry the manifest's %s", target, flag)
			}
		}

		record := manifest.Targets[target]

		if len(record.Host.CFlags) > 0 && !carries(configure.args, "--extra-cflags="+strings.Join(record.Host.CFlags, " ")) {
			t.Errorf("%s: the configure does not add the record's host cflags %v to the source: %v", target, record.Host.CFlags, configure.args)
		}

		if len(record.Host.LDFlags) > 0 && !carries(configure.args, "--extra-ldflags="+strings.Join(record.Host.LDFlags, " ")) {
			t.Errorf("%s: the configure does not add the record's host ldflags %v to the source: %v", target, record.Host.LDFlags, configure.args)
		}

		// What the container compile is told is not what this one is told. A Mac has no /sysroot, and its
		// linker is the one Apple ships: passing either to a host compile is asking it for a path and a program
		// that are not there.
		for _, prefix := range []string{"--extra-cflags=", "--extra-ldflags="} {
			if flags := theFlagNamed(configure.args, prefix); strings.Contains(flags, "sysroot") || strings.Contains(flags, "-fuse-ld") {
				t.Errorf("%s: the host compile passes %q, and a Mac reads the SDK its Command Line Tools installed and links with its own ld", target, flags)
			}
		}

		if carries(configure.args, "--cc=") {
			t.Errorf("%s: the host compile is told which compiler to run, when the compiler its own machine has is the answer: %v", target, configure.args)
		}

		if carries(configure.args, "--enable-cross-compile") {
			t.Errorf("%s: the compile cross-compiles on the machine the binaries run on", target)
		}

		make := stepFor(plan.Steps, "make")

		if want := aMakeProgramFor(target); make.name != want {
			t.Errorf("%s: the compile runs %s, want %s: the record names the make program its host needs", target, make.name, want)
		}

		strip := stepFor(plan.Steps, "strip")

		if !carries(strip.args, "ffmpeg") || !carries(strip.args, "ffprobe") {
			t.Errorf("%s: the compile strips %v", target, strip.args)
		}

		// The identity is the ad-hoc one: no developer identity, and no keychain, because the binaries have to
		// start on a machine nobody prepared, and both of them are signed by the one command the host has.
		sign := stepFor(plan.Steps, "sign")

		if sign.name != "codesign" {
			t.Errorf("%s: the host compile signs with %s %v, want the host's own codesign", target, sign.name, sign.args)
		}

		if !sameWords(sign.args, []string{"-s", "-", "ffmpeg", "ffprobe"}) {
			t.Errorf("%s: codesign is given %v, want -s - ffmpeg ffprobe: the ad-hoc identity signs both programs, and an identity the host does not hold fails the compile instead of producing binaries that start", target, sign.args)
		}

		if stepIndex(plan.Steps, "strip") > stepIndex(plan.Steps, "sign") {
			t.Errorf("%s: the binaries are signed before they are stripped, and the signature is over bytes strip rewrote", target)
		}
	}
}

// The compile's steps run inside the unpacked ffmpeg tree, and the tar
// command alone runs from the repository: configure writes its build
// files into the directory it is run from.
// see the ffmpeg-cross-build maintainer note.
func TestTheNativeCompileRunsItsStepsInTheTreeItUnpacked(t *testing.T) {
	for _, target := range []string{"macos-amd64", "macos-arm64"} {
		manifest := bundle.ReadManifest()

		plan, err := planFFmpeg(options{target: target}, t.TempDir(), manifest, aNativeTree(target), aMachineForTarget(target))
		if err != nil {
			t.Errorf("%s: the build refused to compile the binaries on the host that is the target: %v", target, err)
			continue
		}

		work := filepath.Join(plan.Paths.scratch, sourceDir(manifest))

		unpack := stepFor(plan.Steps, "unpack")

		if got := argAfter(unpack.args, "-C"); got != plan.Paths.scratch {
			t.Errorf("%s: the unpack extracts into %s, want %s: the tree the rest of the compile reads is the one the unpack writes", target, got, plan.Paths.scratch)
		}

		for _, label := range []string{"configure", "make", "strip", "sign"} {
			s := stepFor(plan.Steps, label)

			if s.name == "" {
				continue
			}

			if s.dir != work {
				t.Errorf("%s: the %s step runs in %s, want the tree the unpack wrote (%s)", target, label, s.dir, work)
			}
		}

		if name := stepFor(plan.Steps, "configure").name; name != filepath.Join(work, "configure") {
			t.Errorf("%s: the configure is %s, want the configure of the tree the unpack wrote (%s)", target, name, work)
		}
	}
}

// The two macOS architectures ship one toolchain: the Command Line Tools'
// clang produces either architecture on either Mac, the SDK carries the
// libraries of both, and codesign signs either on either.
func TestAMacOSHostCompilesTheOtherArchitecturesBinaries(t *testing.T) {
	for _, tc := range []struct {
		target  string
		machine host
	}{
		{target: "macos-amd64", machine: host{goos: "darwin", goarch: "arm64"}},
		{target: "macos-arm64", machine: host{goos: "darwin", goarch: "amd64"}},
	} {
		plan, err := planFFmpeg(options{target: tc.target}, t.TempDir(), bundle.ReadManifest(), aNativeTree(tc.target), tc.machine)
		if err != nil {
			t.Errorf("%s on a %s host: the build refused to compile the binaries: %v", tc.target, tc.machine.goarch, err)
			continue
		}

		if !plan.Native {
			t.Errorf("%s on a %s host: the build compiled nothing, and one macOS toolchain produces both architectures", tc.target, tc.machine.goarch)
		}
	}
}

// The cache entry a macOS target's binaries are looked for in is named by
// the record and the target, not by the machine computing it: the Mac
// that compiled them and a Mac reusing them look for the same entry, and
// each machine's cache is its own tree, holding what that machine compiled.
func TestTheCacheEntryOfAMacOSTargetIsTheSameEntryOnEveryMac(t *testing.T) {
	manifest := bundle.ReadManifest()

	repo := t.TempDir()

	warm := ffmpegState{bundleIsCached: true}

	onIntel, err := planFFmpeg(options{target: "macos-amd64"}, repo, manifest, warm, aMachineForTarget("macos-amd64"))
	if err != nil {
		t.Fatalf("the Intel host refused to reuse the cached binaries: %v", err)
	}

	onAppleSilicon, err := planFFmpeg(options{target: "macos-amd64"}, repo, manifest, warm, host{goos: "darwin", goarch: "arm64"})
	if err != nil {
		t.Fatalf("the Apple Silicon host refused to reuse the cached binaries: %v", err)
	}

	if onIntel.Paths.cached != onAppleSilicon.Paths.cached {
		t.Errorf("the two hosts look for the binaries in different entries: %s and %s", onIntel.Paths.cached, onAppleSilicon.Paths.cached)
	}
}

// A cross macOS compile names its architecture: ffmpeg's architecture
// switch, the switch keeping configure from running foreign test
// programs, and clang's -arch in compiler and linker flags. A native
// macOS compile names none of them: the compiler's default is correct.
func TestACompileAcrossTheTwoArchitecturesNamesTheTargetArchitecture(t *testing.T) {
	for _, tc := range []struct {
		target     string
		machine    host
		ffmpegArch string
		clangArch  string
	}{
		{target: "macos-amd64", machine: host{goos: "darwin", goarch: "arm64"}, ffmpegArch: "x86_64", clangArch: "x86_64"},
		{target: "macos-arm64", machine: host{goos: "darwin", goarch: "amd64"}, ffmpegArch: "aarch64", clangArch: "arm64"},
	} {
		plan, err := planFFmpeg(options{target: tc.target}, t.TempDir(), bundle.ReadManifest(), aNativeTree(tc.target), tc.machine)
		if err != nil {
			t.Errorf("%s on a %s host: %v", tc.target, tc.machine.goarch, err)
			continue
		}

		configure := stepFor(plan.Steps, "configure")

		for _, want := range []string{"--arch=" + tc.ffmpegArch, "--enable-cross-compile"} {
			if !carries(configure.args, want) {
				t.Errorf("%s compiled on a %s host: configure does not name %s", tc.target, tc.machine.goarch, want)
			}
		}

		for _, prefix := range []string{"--extra-cflags=", "--extra-ldflags="} {
			flags := theFlagNamed(configure.args, prefix)

			if !strings.Contains(flags, "-arch "+tc.clangArch) {
				t.Errorf("%s compiled on a %s host: %s does not name -arch %s: the compiler's default is the host's architecture", tc.target, tc.machine.goarch, prefix, tc.clangArch)
			}

			if !strings.Contains(flags, "-mmacosx-version-min=11.0") {
				t.Errorf("%s compiled on a %s host: %s drops the flag the manifest record names: %q", tc.target, tc.machine.goarch, prefix, flags)
			}
		}
	}

	plan, err := planFFmpeg(options{target: "macos-arm64"}, t.TempDir(), bundle.ReadManifest(), aNativeTree("macos-arm64"), aMachineForTarget("macos-arm64"))
	if err != nil {
		t.Fatalf("the build refused to compile the ffmpeg binaries of the machine it is standing on: %v", err)
	}

	own := stepFor(plan.Steps, "configure")

	if carries(own.args, "--enable-cross-compile") || carries(own.args, "--arch=") {
		t.Errorf("a compile on the target's own host names a cross-compile: %v", own.args)
	}

	if carries(own.args, "-arch ") {
		t.Errorf("a compile on the target's own host names -arch, asking the compiler for the architecture it is standing on: %v", own.args)
	}
}

// A record pinning an SDK names a tree this machine must have, and the
// run is refused before any container or fetch. The refusal names target,
// SDK version, the directory looked in, and the variable naming another,
// so one line says whether the tree is unprepared or prepared elsewhere.
func TestTheBuildRefusesAMacOSTargetWhosePreparedSysrootIsAbsent(t *testing.T) {
	manifest := bundle.ReadManifest()

	for _, target := range []string{"macos-amd64", "macos-arm64"} {
		record := manifest.Targets[target]

		home := t.TempDir()

		tree, lookedIn := thePreparedSysrootFor(record, noEnvironment, home)

		if tree != "" {
			t.Fatalf("%s: the build found a prepared sysroot in a home directory that holds none: %s", target, tree)
		}

		state := ffmpegState{program: podmanProgram, sysrootLookedIn: lookedIn}

		plan, err := planFFmpeg(options{target: target}, "/repo", manifest, state, theReleaseHost)
		if err == nil {
			t.Errorf("%s: the build planned a compile with no sysroot for it to read: %d steps", target, len(plan.Steps))
			continue
		}

		if len(plan.Steps) > 0 {
			t.Errorf("%s: the refused plan still carries %d steps", target, len(plan.Steps))
		}

		for _, want := range []string{target, record.Sysroot.SDK, lookedIn, "RDIO_FFMPEG_DARWIN_SDK"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: the refusal %q does not name %q", target, err.Error(), want)
			}
		}

		plan, err = planFFmpeg(options{sub: subDist, target: target}, "/repo", manifest, state, theReleaseHost)
		if err != nil {
			t.Errorf("%s: a release run was refused outright, and the release it was asked for has other archives in it: %v", target, err)
			continue
		}

		if !plan.WritesNone {
			t.Errorf("%s: the release run reports this target as writing an archive it cannot compile", target)
			continue
		}

		if len(plan.Steps) > 0 {
			t.Errorf("%s: the release run still has %d steps for a compile it cannot start", target, len(plan.Steps))
		}

		for _, want := range []string{target, record.Sysroot.SDK, lookedIn, "RDIO_FFMPEG_DARWIN_SDK"} {
			if !strings.Contains(plan.Says, want) {
				t.Errorf("%s: what the release run says about this target, %q, does not name %q", target, plan.Says, want)
			}
		}
	}
}

// A directory standing where the build looks is not a sysroot: the tree a
// compile reads carries the library a Darwin link reads, and an empty,
// half prepared, or one-level-too-high directory is refused like absent.
func TestTheBuildRefusesADirectoryThatCarriesNoSysroot(t *testing.T) {
	manifest := bundle.ReadManifest()

	record := manifest.Targets["macos-amd64"]

	home := t.TempDir()

	keptAt := filepath.Join(home, ".cache", "rdio-scanner", "ffmpeg-darwin-sdk", record.Sysroot.SDK, "MacOSX"+record.Sysroot.SDK+".sdk")

	if err := os.MkdirAll(keptAt, 0755); err != nil {
		t.Fatalf("the test cannot write the directory it was given: %v", err)
	}

	tree, lookedIn := thePreparedSysrootFor(record, noEnvironment, home)

	if tree != "" {
		t.Errorf("the build took an empty directory for the macOS %s SDK its record pins: %s", record.Sysroot.SDK, tree)
	}

	if lookedIn != keptAt {
		t.Errorf("the build looked in %s, and the harness that prepares the tree writes it at %s", lookedIn, keptAt)
	}

	plan, err := planFFmpeg(options{target: "macos-amd64"}, "/repo", manifest,
		ffmpegState{program: podmanProgram, sysrootLookedIn: lookedIn}, theReleaseHost)
	if err == nil {
		t.Fatalf("the build planned a compile against a directory that carries no libraries: %d steps", len(plan.Steps))
	}

	if !strings.Contains(err.Error(), lookedIn) {
		t.Errorf("the refusal %q does not name the directory it found nothing prepared in", err.Error())
	}
}

// The tree a record pins is looked for where the record says this machine
// keeps it: the directory under the operator's home the preparing harness
// writes, or the directory named with RDIO_FFMPEG_DARWIN_SDK. The variable
// names the tree itself, because the tree is what the container mounts.
func TestTheBuildLooksForItsSysrootWhereItsRecordSaysTheMachineKeepsIt(t *testing.T) {
	manifest := bundle.ReadManifest()

	record := manifest.Targets["macos-amd64"]

	home := t.TempDir()

	inTheCacheOfItsOwn := filepath.Join(home, ".cache", "rdio-scanner", "ffmpeg-darwin-sdk", record.Sysroot.SDK, "MacOSX"+record.Sysroot.SDK+".sdk")

	if err := writeASysrootTree(inTheCacheOfItsOwn); err != nil {
		t.Fatalf("the test cannot write the sysroot it was given: %v", err)
	}

	tree, lookedIn := thePreparedSysrootFor(record, noEnvironment, home)

	if lookedIn != inTheCacheOfItsOwn || tree != inTheCacheOfItsOwn {
		t.Errorf("the build resolved %s and %s for a tree written at %s", tree, lookedIn, inTheCacheOfItsOwn)
	}

	elsewhere := t.TempDir()

	if err := writeASysrootTree(elsewhere); err != nil {
		t.Fatalf("the test cannot write the sysroot the operator named: %v", err)
	}

	tree, lookedIn = thePreparedSysrootFor(record, anEnvironment(map[string]string{
		"RDIO_FFMPEG_DARWIN_SDK": elsewhere,
	}), home)

	if lookedIn != elsewhere || tree != elsewhere {
		t.Errorf("the operator named %s and the build resolved %s and %s", elsewhere, tree, lookedIn)
	}
}

// A candidate sysroot ties to its record only through the sha256 digest
// file its preparation left beside it: the recorded digest must be the
// digest the record names. A swapped or hand-assembled tree records
// nothing, and the build compiles against neither.
func TestTheBuildRefusesASysrootWhoseRecordedDigestIsNotTheOneItsRecordPins(t *testing.T) {
	manifest := bundle.ReadManifest()

	record := manifest.Targets["macos-amd64"]

	home := t.TempDir()

	tree := filepath.Join(home, ".cache", "rdio-scanner", "ffmpeg-darwin-sdk", record.Sysroot.SDK, "MacOSX"+record.Sysroot.SDK+".sdk")

	if err := writeASysrootTree(tree); err != nil {
		t.Fatalf("the test cannot write the sysroot it was given: %v", err)
	}

	const anotherTree = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	cases := []struct {
		// recorded is the digest written beside the tree, and "" is nothing written there at all.
		recorded string
		want     []string
	}{
		{recorded: anotherTree, want: []string{"macos-amd64", record.Sysroot.SDK, tree, anotherTree, record.Sysroot.SHA256, darwinSysrootVariable}},
		{recorded: "", want: []string{"macos-amd64", record.Sysroot.SDK, tree, record.Sysroot.SHA256, darwinSysrootVariable}},
	}

	for _, c := range cases {
		label := filepath.Join(filepath.Dir(tree), "sha256")

		if c.recorded == "" {
			if err := os.Remove(label); err != nil && !os.IsNotExist(err) {
				t.Fatalf("the test cannot remove the digest it wrote beside the tree: %v", err)
			}
		} else if err := os.WriteFile(label, []byte(c.recorded+"\n"), 0644); err != nil {
			t.Fatalf("the test cannot write the digest beside the tree: %v", err)
		}

		if found := theSysrootDigestRecordedBeside(tree); found != c.recorded {
			t.Fatalf("the digest recorded beside %s reads %q, and the preparation records it at %s", tree, found, label)
		}

		state := anUncompiledTree()

		state.sysrootTree, state.sysrootLookedIn = thePreparedSysrootFor(record, noEnvironment, home)
		state.sysrootDigest = theSysrootDigestRecordedBeside(tree)

		if state.sysrootTree != tree {
			t.Fatalf("the build found no prepared sysroot at %s", tree)
		}

		plan, err := planFFmpeg(options{target: "macos-amd64"}, "/repo", manifest, state, theReleaseHost)
		if err == nil {
			t.Errorf("a tree recording %q was planned a compile with %d steps, and the record pins %s",
				c.recorded, len(plan.Steps), record.Sysroot.SHA256)
			continue
		}

		for _, want := range c.want {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal of a tree recording %q does not name %q: %q", c.recorded, want, err.Error())
			}
		}

		if strings.Contains(err.Error(), "no prepared sysroot") {
			t.Errorf("a tree that is there is refused as a tree that is not: %q", err.Error())
		}

		// The same refusal answers a release run as the target it wrote no archive for, whichever of the two
		// sysroot refusals it is.
		plan, err = planFFmpeg(options{sub: subDist, target: "macos-amd64"}, "/repo", manifest, state, theReleaseHost)
		if err != nil {
			t.Errorf("a release run was refused outright over a tree recording %q, and the release it was asked for has other archives in it: %v", c.recorded, err)
			continue
		}

		if !plan.WritesNone {
			t.Errorf("a release run reports a target compiled against a tree recording %q as one it wrote an archive for", c.recorded)
			continue
		}

		for _, want := range c.want {
			if !strings.Contains(plan.Says, want) {
				t.Errorf("what a release run says about a tree recording %q does not name %q: %q", c.recorded, want, plan.Says)
			}
		}
	}
}

// The prepared sysroot reaches the compile as a read-only mount and not a
// layer: the compile image is built with nothing of Apple's in it, and
// the tree goes where the record says the compiler looks for it.
func TestTheMacOSCompileMountsItsPreparedSysrootReadOnly(t *testing.T) {
	manifest := bundle.ReadManifest()

	for _, target := range []string{"macos-amd64", "macos-arm64"} {
		record := manifest.Targets[target]

		state, tree := aTreeWithTheSysrootPreparedFor(t, record)

		plan, err := planFFmpeg(options{target: target}, "/repo", manifest, state, theReleaseHost)
		if err != nil {
			t.Errorf("%s: the build refused a compile whose sysroot is prepared: %v", target, err)
			continue
		}

		want := tree + ":" + record.Sysroot.Mount + ":ro"

		for _, label := range []string{"configure", "make"} {
			s, ok := stepNamed(plan, label)
			if !ok {
				t.Errorf("%s: the build has no %s step", target, label)
				continue
			}

			if !carries(s.args, want) {
				t.Errorf("%s: the %s step does not mount the prepared sysroot read-only as %s: %v", target, label, want, s.args)
			}
		}

		image, ok := stepNamed(plan, "image")
		if !ok {
			t.Errorf("%s: the build has no step that prepares the container it compiles in", target)
			continue
		}

		if carries(image.args, tree) {
			t.Errorf("%s: the image step carries the sysroot tree %s, and the image is built with nothing of Apple's in it: %v", target, tree, image.args)
		}

		if _, pinned := containerBuildArgs(image.args)["sysroot_image"]; pinned {
			t.Errorf("%s: the image step copies a sysroot out of an image into the image, and this sysroot arrives at run time: %v", target, image.args)
		}

		for _, label := range []string{"clean", "unpack", "chmod"} {
			s, ok := stepNamed(plan, label)
			if !ok {
				continue
			}

			if carries(s.args, tree) {
				t.Errorf("%s: the %s step mounts the sysroot, and it reads no headers, libraries or startup files: %v", target, label, s.args)
			}
		}
	}
}

// The mount names the path the record says the compile reads, the same
// path the record spells into its own compiler and linker flags.
func TestTheSysrootIsMountedWhereItsRecordSaysTheCompileReadsIt(t *testing.T) {
	manifest := bundle.ReadManifest()

	record := manifest.Targets["macos-arm64"]
	record.Sysroot.Mount = "/SDKroot"

	manifest = aManifestWithOtherTarget("macos-arm64", record)

	state, tree := aTreeWithTheSysrootPreparedFor(t, record)

	plan, err := planFFmpeg(options{target: "macos-arm64"}, "/repo", manifest, state, theReleaseHost)
	if err != nil {
		t.Fatalf("the build refused a compile whose sysroot is prepared: %v", err)
	}

	for _, label := range []string{"configure", "make"} {
		s, ok := stepNamed(plan, label)
		if !ok {
			t.Errorf("the build has no %s step", label)
			continue
		}

		if !carries(s.args, tree+":/SDKroot:ro") {
			t.Errorf("the %s step mounts the sysroot somewhere other than where its record says the compile reads it: %v", label, s.args)
		}
	}
}

// A Mac compiling its own ffmpeg needs no prepared tree: the SDK that
// compile reads is the one its own toolchain carries, so the host route
// refuses nothing and mounts nothing.
func TestTheMacOSCompileOnItsOwnHostNeedsNoPreparedSysroot(t *testing.T) {
	manifest := bundle.ReadManifest()

	for _, tc := range []struct {
		target  string
		machine host
	}{
		{target: "macos-amd64", machine: host{goos: "darwin", goarch: "arm64"}},
		{target: "macos-arm64", machine: host{goos: "darwin", goarch: "amd64"}},
	} {
		plan, err := planFFmpeg(options{target: tc.target}, t.TempDir(), manifest, aNativeTree(tc.target), tc.machine)
		if err != nil {
			t.Errorf("%s on a %s Mac: the build refused a compile that needs no prepared sysroot: %v", tc.target, tc.machine.goarch, err)
			continue
		}

		for _, s := range plan.Steps {
			if carries(s.args, ":/sysroot") || carries(s.args, "--sysroot") {
				t.Errorf("%s on a %s Mac: the %s step names a sysroot that host does not prepare: %v", tc.target, tc.machine.goarch, s.label, s.args)
			}
		}
	}
}

// A record's container toolchain names reach the compile through
// configure's own options, which write them into the generated Makefile:
// archiver, indexer, and the choice not to strip. Naming none leaves
// configure's defaults, and naming one unmeasured moves its cache entry.
func TestTheCompileIsToldTheToolchainItsRecordNames(t *testing.T) {
	manifest := bundle.ReadManifest()

	for _, target := range []string{"macos-amd64", "macos-arm64"} {
		record := manifest.Targets[target]

		plan, err := planFFmpeg(options{target: target}, "/repo", manifest, aTreeReadyFor(t, manifest, target), theReleaseHost)
		if err != nil {
			t.Errorf("%s: the build refused a compile whose sysroot is prepared: %v", target, err)
			continue
		}

		configure := stepFor(plan.Steps, "configure")

		for _, asked := range []struct{ arg, why string }{
			{arg: "--ar=" + record.AR, why: "the archives written with the archiver the container ships are refused by the link that reads them"},
			{arg: "--ranlib=" + record.Ranlib, why: "the archives it writes carry no symbol table for that link to look a library up in"},
			{arg: "--disable-stripping", why: "the container rewrites a program its own link signed, and nothing it has can write that signature again"},
		} {
			if !hasArg(configure.args, asked.arg) {
				t.Errorf("%s: configure was not told %s, and %s: %v", target, asked.arg, asked.why, configure.args)
			}
		}
	}

	for _, target := range []string{"linux-386", "linux-amd64", "linux-arm", "linux-arm64", "windows-amd64", "freebsd-amd64"} {
		plan, err := planFFmpeg(options{target: target}, "/repo", manifest, aTreeReadyFor(t, manifest, target), theReleaseHost)
		if err != nil {
			t.Errorf("%s: the build refused a target whose record names no prepared sysroot: %v", target, err)
			continue
		}

		if configure := stepFor(plan.Steps, "configure"); carries(configure.args, "--ar=") || carries(configure.args, "--ranlib=") || carries(configure.args, "--disable-stripping") {
			t.Errorf("%s: configure was told which archiver writes its libraries, or told to keep its programs as its link wrote them, and nothing measured about that compile names a tool it cannot read out of its own container or a signature it has to keep: %v", target, configure.args)
		}
	}

	for _, tc := range []struct {
		target  string
		machine host
	}{
		{target: "macos-amd64", machine: host{goos: "darwin", goarch: "arm64"}},
		{target: "macos-arm64", machine: host{goos: "darwin", goarch: "amd64"}},
	} {
		plan, err := planFFmpeg(options{target: tc.target}, t.TempDir(), manifest, aNativeTree(tc.target), tc.machine)
		if err != nil {
			t.Errorf("%s on a %s Mac: the build refused a compile that asks nothing of the machine: %v", tc.target, tc.machine.goarch, err)
			continue
		}

		for _, s := range plan.Steps {
			if carries(s.args, "--ar=") || carries(s.args, "--ranlib=") || carries(s.args, "--disable-stripping") {
				t.Errorf("%s on a %s Mac: the %s step names a choice made for a container that compile does not run in: %v", tc.target, tc.machine.goarch, s.label, s.args)
			}
		}
	}
}

// aTreeReadyFor is the state of a machine a plan is asked on: the
// container program it has and, when the record pins an SDK tree its
// compile reads, that tree prepared in the machine's own cache directory.
func aTreeReadyFor(t *testing.T, manifest bundle.Manifest, target string) ffmpegState {
	t.Helper()

	record, covered := manifest.Targets[target]
	if !covered || record.Sysroot == (bundle.Sysroot{}) {
		return anUncompiledTree()
	}

	state, _ := aTreeWithTheSysrootPreparedFor(t, record)

	return state
}

// aTreeWithTheSysrootPreparedFor is a machine holding the SDK tree a
// record pins: the tree and its preparation's recorded digest, in a home
// of the test's own in the harness's layout, plus the tree's directory so
// a test can name the mount a container should get.
func aTreeWithTheSysrootPreparedFor(t *testing.T, record bundle.Target) (ffmpegState, string) {
	t.Helper()

	home := t.TempDir()

	tree := filepath.Join(home, ".cache", "rdio-scanner", "ffmpeg-darwin-sdk", record.Sysroot.SDK, "MacOSX"+record.Sysroot.SDK+".sdk")

	if err := writeASysrootTree(tree); err != nil {
		t.Fatalf("the test cannot write the sysroot it was given: %v", err)
	}

	if err := writeTheSysrootDigestRecordedFor(record, tree); err != nil {
		t.Fatalf("the test cannot write the digest the preparation records beside its tree: %v", err)
	}

	state := anUncompiledTree()

	state.sysrootTree, state.sysrootLookedIn = thePreparedSysrootFor(record, noEnvironment, home)
	state.sysrootDigest = theSysrootDigestRecordedBeside(tree)

	if state.sysrootTree != tree {
		t.Fatalf("the build resolved %s beside the tree it was shown at %s", state.sysrootTree, tree)
	}

	if state.sysrootDigest != record.Sysroot.SHA256 {
		t.Fatalf("the digest recorded beside the tree it was shown reads %q, and the record pins %s", state.sysrootDigest, record.Sysroot.SHA256)
	}

	return state, tree
}

// writeASysrootTree writes the one file a macOS link reads, which is what
// a directory has to carry before this build takes it for a pinned SDK.
func writeASysrootTree(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "usr", "lib"), 0755); err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, "usr", "lib", "libSystem.tbd"), []byte("text-based stub library\n"), 0644)
}

// writeTheSysrootDigestRecordedFor writes the record a prepared sysroot
// leaves beside its tree: the tree's sha256, in a file named after the
// digest it holds, because carrying the library a link reads makes a
// directory a sysroot and the digest beside it makes it the pinned one.
func writeTheSysrootDigestRecordedFor(record bundle.Target, tree string) error {
	return os.WriteFile(filepath.Join(filepath.Dir(tree), theSysrootDigestFile), []byte(record.Sysroot.SHA256+"\n"), 0644)
}

// aHomeWithTheSysrootPreparedInIt is the home a whole run finds a prepared
// SDK tree in: the tree and its recorded digest where this build looks,
// and the run moves into it for the test. The record's tree variable is
// emptied, so the test cannot answer to the operator's own environment.
func aHomeWithTheSysrootPreparedInIt(t *testing.T, record bundle.Target) string {
	t.Helper()

	home := t.TempDir()

	tree := filepath.Join(home, ".cache", "rdio-scanner", "ffmpeg-darwin-sdk", record.Sysroot.SDK, "MacOSX"+record.Sysroot.SDK+".sdk")

	t.Setenv("HOME", home)
	t.Setenv(darwinSysrootVariable, "")

	if err := writeASysrootTree(tree); err != nil {
		t.Fatalf("the test cannot write the sysroot it was given: %v", err)
	}

	if err := writeTheSysrootDigestRecordedFor(record, tree); err != nil {
		t.Fatalf("the test cannot write the digest the preparation records beside its tree: %v", err)
	}

	if got, lookedIn := thePreparedSysrootFor(record, os.LookupEnv, home); got != tree || lookedIn != tree {
		t.Fatalf("the test prepared the sysroot at %s and the run looks for it at %s", tree, lookedIn)
	}

	return home
}

// theFlagNamed is the value of one flag-bearing argument, which is how a test reads what a step was told
// to pass.
func theFlagNamed(args []string, prefix string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return arg
		}
	}

	return ""
}

// A run asked for the machine it stands on, in the words go names that
// machine by, compiles the ffmpeg binaries of the release target that
// machine is: the alias is a spelling, and the phase sees the target.
func TestTheFFmpegPhaseAskedForTheWordGoNamesTheMachineByPlansTheReleaseTarget(t *testing.T) {
	o, err := parseArgs([]string{"ffmpeg", "darwin-arm64"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	plan, err := planFFmpeg(o, t.TempDir(), bundle.ReadManifest(), aNativeTree("macos-arm64"), aMachineForTarget("macos-arm64"))
	if err != nil {
		t.Fatalf("the build refused to compile the ffmpeg binaries of the machine it is standing on: %v", err)
	}

	if plan.Target != "macos-arm64" {
		t.Errorf("the plan is for %s, want macos-arm64", plan.Target)
	}

	if !plan.Native {
		t.Errorf("the plan for %s compiles nothing on the machine that is that target", plan.Target)
	}
}

// argAfter is the argument a command names after a flag, which is how a test reads where a step is
// pointed at.
func argAfter(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}

	return ""
}

// Binaries the maintainer dropped are adopted on their own host exactly
// as anywhere else: the compile is what the build does when there is
// nothing to adopt, not a replacement for the drop. Both macOS targets
// are pinned: a route adopting only the native target strands the other.
func TestSuppliedBinariesDroppedOnTheirOwnHostAreAdoptedAndNotCompiled(t *testing.T) {
	for _, target := range []string{"macos-amd64", "macos-arm64"} {
		repo := t.TempDir()

		manifest := aDropOfSuppliedBinaries(t, repo, target)

		plan, err := planFFmpeg(options{target: target}, repo, manifest, aNativeTree(target), aMachineForTarget(target))
		if err != nil {
			t.Fatalf("%s: the build refused the binaries dropped for it: %v", target, err)
		}

		if plan.Native {
			t.Errorf("%s: the build compiled binaries when binaries are sitting in the directory it embeds", target)
		}

		if !plan.Adopts {
			t.Errorf("%s: the plan adopts nothing: the host standing on the target is the host that can run them", target)
		}

		if !plan.Host {
			t.Errorf("%s: the plan does not say the binaries come from this machine, and the run that carries a plan out is chosen by that", target)
		}

		if !strings.Contains(plan.Says, "supplied") {
			t.Errorf("%s: the message %q does not say the ffmpeg is supplied", target, plan.Says)
		}
	}
}

// The host's own build tools are what a native compile runs, and the
// operator is told which program is missing and what installs it before a
// minute of work is spent finding out.
func TestTheNativeCompileRefusesAHostWithNoToolchain(t *testing.T) {
	for _, missing := range []string{"tar", "cc", "strip", "make", "codesign"} {
		repo := t.TempDir()

		state := aNativeTree("macos-arm64")
		delete(state.nativeTools, missing)

		_, err := planFFmpeg(options{target: "macos-arm64"}, repo, bundle.ReadManifest(), state, aMachineForTarget("macos-arm64"))
		if err == nil {
			t.Errorf("the build planned a compile on a host with no %s", missing)
			continue
		}

		if !strings.Contains(err.Error(), missing) {
			t.Errorf("the refusal %q does not name the %s that is missing", err.Error(), missing)
		}
	}
}

func aMachineForTarget(target string) host {
	tr, ok := targetFor(target)
	if !ok {
		panic("no release target " + target)
	}

	return host{goos: tr.goos, goarch: tr.goarch}
}

// theReleaseHost is the machine that produces the releases: a Linux host,
// not a host either macOS target is compiled on with its own toolchain.
var theReleaseHost = host{goos: "linux", goarch: "amd64"}

// aNativeTree is a tree with nothing compiled in it, and the host's own build tools in front of it.
func aNativeTree(target string) ffmpegState {
	state := anUncompiledTree()
	state.nativeTools = aToolchainFor(target)

	return state
}

func aToolchainFor(target string) map[string]string {
	programs := []string{"tar", "cc", "strip", aMakeProgramFor(target)}

	if strings.HasPrefix(target, "macos-") {
		programs = append(programs, "codesign")
	}

	tools := map[string]string{}

	for _, name := range programs {
		tools[name] = "/usr/bin/" + name
	}

	return tools
}

// aMakeProgramFor is the make program the host compile of a target runs,
// which is what a test that asks what a host compile runs needs.
func aMakeProgramFor(target string) string {
	record, ok := bundle.ReadManifest().Targets[target]
	if !ok || record.Host.Make == "" {
		return "make"
	}

	return record.Host.Make
}

func stepFor(steps []step, label string) step {
	for _, s := range steps {
		if s.label == label {
			return s
		}
	}

	return step{}
}

func stepIndex(steps []step, label string) int {
	for i, s := range steps {
		if s.label == label {
			return i
		}
	}

	return -1
}

func stepLabels(steps []step) []string {
	labels := []string{}

	for _, s := range steps {
		labels = append(labels, s.label)
	}

	return labels
}

func sameWords(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}

// Every step that speaks to a container names the program the run
// resolved: a step naming a program the machine does not have fails on a
// machine that can build the image, and a plan whose steps disagree about
// who they are talking to is a plan that should not exist.
func TestEveryContainerStepOfTheFfmpegPlanNamesTheProgramTheRunResolved(t *testing.T) {
	manifest := bundle.ReadManifest()

	for _, program := range []string{podmanProgram, dockerProgram} {
		plan, err := planFFmpeg(options{target: "linux-arm64"}, "/repo", manifest, ffmpegState{program: program}, theReleaseHost)
		if err != nil {
			t.Fatalf("planFFmpeg for %s: %v", program, err)
		}

		if len(plan.Steps) == 0 {
			t.Fatalf("the plan for %s composed no steps", program)
		}

		for _, s := range plan.Steps {
			if s.name != program {
				t.Errorf("the %s step speaks to %q, want the program the run resolved: %q", s.label, s.name, program)
			}
		}

		// The steps that enter a container say it in the vocabulary that program
		// understands, and every image build names the file it builds from:
		// docker looks for Dockerfile and does not find Containerfile.
		for _, s := range plan.Steps {
			if len(s.args) == 0 {
				t.Fatalf("the %s step carries no arguments", s.label)
			}

			switch s.args[0] {
			case "run":
				begins := theContainerProgram{name: program}.run()

				if len(s.args) < len(begins) {
					t.Errorf("the %s step runs %v, which is shorter than the arguments %s needs", s.label, s.args, program)

					continue
				}

				if !sameArgs(s.args[:len(begins)], begins) {
					t.Errorf("the %s step runs %v, want the arguments %s understands: %v", s.label, s.args, program, begins)
				}
			case "build":
				if file := theFlagValue(s.args, "-f"); file == "" {
					t.Errorf("the %s image build does not name the file it builds from: docker looks for Dockerfile and does not find Containerfile",
						s.label)
				}
			}
		}
	}
}
