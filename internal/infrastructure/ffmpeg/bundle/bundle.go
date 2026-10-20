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

// Package bundle is the ffmpeg this server carries inside its own binary.
//
// A server built for a target whose ffmpeg this build compiles carries the ffmpeg binaries compiled for
// that target, so a deployment needs nothing installed on the host it runs on. A server built for a
// target whose ffmpeg binaries are compiled where the target runs carries the binaries that compile
// produced — the ones this build compiled on the machine that is the target, or the ones dropped for it
// — for the same effect. A server built for a target whose binaries never arrived, and for every plain
// `go build` a developer runs, carries none, and the server falls back to the ffmpeg its host provides.
//
// Which target a binary carries is decided by its build tags, in the bundle_<target>.go files: each
// one embeds exactly one target's directory, so a linux-arm64 binary carries the linux-arm64 ffmpeg
// binaries and nothing else. This file holds the rules that do not depend on the target.
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

// Target is how one architecture's ffmpeg comes to be.
//
// A record with no `produced` names a toolchain: the container that holds it, the Containerfile that builds
// it, the packages to install in it, and the configure arguments that make the compile cross to it. This
// build compiles that target's ffmpeg and proves the pair it produced the way its `conformance` says to: by
// running it, or, when no host this build runs on can start it, by reading out of the files what the record
// names.
//
// A record with `produced` saying `supplied` names no container, no packages, no cross arguments, and no
// check environment: the machine that produces the releases has none of those for it, and asking for one
// would be a build that fails for wanting a toolchain it was never going to have. What it does name is the
// compile that runs on the platform the target runs on — `cflags` and `ldflags` are the extra configure
// flags that compile adds, and `make` is the make program it runs.
//
// A target with no record in the manifest is an uncovered target: the build compiles no ffmpeg for it,
// adopts nothing for it, and the server that runs on it uses the host's.
type Target struct {
	// Container is the image the toolchain is built in. Containerfile names the Containerfile at the root of
	// this repository that builds it, which is empty for Containerfile.ffmpeg: the toolchain of a target
	// compiled with the compiler its container's own distribution ships needs nothing beside that.
	// ContainerDigest is the image the record names as the build that compiled these binaries pulled it,
	// which is what makes the compile reproducible; empty is the tag the record names, and a tag moves.
	Container       string   `json:"container"`
	Containerfile   string   `json:"containerfile"`
	ContainerDigest string   `json:"container_digest"`
	Packages        []string `json:"packages"`
	Cross           []string `json:"cross"`

	// CC is the answer configure passes on to the `--cc=` it was given, which is where a compile that
	// crosses to another operating system names the invocation that produces binaries for it: the compiler,
	// the target triple, and the sysroot it reads the other system's headers and libraries from. Empty is
	// configure's own choice, which is the right answer when the container's compiler already targets the
	// container's own operating system.
	CC string `json:"cc"`

	CFlags  []string `json:"cflags"`
	LDFlags []string `json:"ldflags"`

	// SysrootImage names an image whose contents become the sysroot the compile reads: the headers and
	// libraries of the operating system the compile crosses to, published by that operating system's own
	// project. The build never runs such an image — every program in it is written for a kernel this host
	// does not have — and copies it out of the image whole. SysrootDigest pins it.
	SysrootImage  string  `json:"sysroot_image"`
	SysrootDigest string  `json:"sysroot_digest"`
	Sysroot       Sysroot `json:"sysroot"`

	// Make is the make program a compile of this target runs. Empty is `make`.
	Make string `json:"make"`

	// Conformance is how the compiled program is run to be checked, which is not always the container
	// that compiled it: a cross-compiled binary needs an interpreter, and a Windows program needs an
	// operating system.
	Conformance Conformance `json:"conformance"`

	// Produced says how this target's ffmpeg comes to be. Empty is the ordinary case: this build compiles
	// it, in a container. "supplied" is the ffmpeg binaries compiled on the target's own platform — by
	// this build standing on that machine, or by the maintainer and dropped where this build finds it —
	// and adopted from there.
	Produced string `json:"produced"`
}

// Sysroot is a sysroot somebody outside both repositories prepared, which is what an operating system's
// software development kit is: its publisher ships no container of it and licenses it in a way that keeps it
// out of this repository's build, so nothing this repository builds can carry it. The build finds the tree
// named by SDK under the directory its version names in the cache the operator keeps, or under the directory
// the operator points at, mounts it read-only at Mount, and refuses when it is not there.
type Sysroot struct {
	SDK       string `json:"sdk"`
	SHA256    string `json:"sha256"`
	Generator string `json:"generator"`
	Mount     string `json:"mount"`
}

// Conformance is the environment the build runs the compiled ffmpeg in to prove it can do the job: the
// container that holds it, which is empty when the build container is the one, the packages that container
// needs on top, and the program that starts a binary the container cannot run by itself.
//
// A record names one way of proving the program it compiled, and at most one: `run` runs it under an
// interpreter or an emulation layer, and `reads` says nothing on the build host can run it at all, which
// leaves the build reading out of the binaries what they say about how they were compiled. Which one a record
// names is what the licence text has to say about the binaries it describes.
type Conformance struct {
	Container string   `json:"container"`
	Digest    string   `json:"conformance_digest"`
	Packages  []string `json:"packages"`
	Run       []string `json:"run"`

	// Reads names what the build reads out of the binaries when it cannot run them anywhere: a target that
	// is proven by shape is proven differently from one that was run, and says so. The names are the ones
	// the check that reads them knows, in the order it checks them.
	Reads []string `json:"reads"`
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

// Bundle is the two programs one target carries.
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
