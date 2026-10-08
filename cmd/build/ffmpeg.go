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
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// runtimeTarget is the target this build is running on, which is the target an operator who named none
// is building for.
func runtimeTarget() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// ffmpegState is what the tree already has, read before anything is decided.
type ffmpegState struct {
	podmanIsThere  bool
	sourceIsCached bool
	bundleIsCached bool
}

// ffmpegPaths are the places the ffmpeg phase keeps its pieces. Everything under the build directory
// is disposable: deleting it costs a rebuild and nothing else.
type ffmpegPaths struct {
	root    string
	sources string
	source  string
	context string
	scratch string
	cached  string
	bundle  string
}

func (p ffmpegPaths) cachedProgram(name string) string {
	return filepath.Join(p.cached, name)
}

// ffmpegSource is the release the build compiles from, and whether it still has to be fetched.
type ffmpegSource struct {
	Path  string
	URL   string
	Fetch bool
}

// ffmpegPlan is what the ffmpeg phase decided to do, decided completely before any of it runs.
type ffmpegPlan struct {
	Target string

	// Says is what the operator is told about this phase, and is empty when the ordinary thing is
	// happening.
	Says string

	// Steps are the external commands that produce the bundle, and are empty when nothing has to be
	// compiled: an uncovered target, or a bundle that is already there.
	Steps []step

	Source ffmpegSource
	Paths  ffmpegPaths

	// Image is the container the target is compiled in, and Runner is the container its compiled
	// program is checked in, which is the same one unless the target needs an operating system the
	// build container does not have.
	Image  string
	Runner string
}

// ffmpegPathsFor is where one target's pieces are kept. The cache key is in the path, so a recipe that
// changed cannot be mistaken for the one it replaced.
func ffmpegPathsFor(repo string, manifest bundle.Manifest, target string) ffmpegPaths {
	root := filepath.Join(repo, ".build", "ffmpeg")
	source := sourceName(manifest)

	return ffmpegPaths{
		root:    root,
		sources: filepath.Join(root, "src"),
		source:  filepath.Join(root, "src", source),
		context: filepath.Join(root, "context"),
		scratch: filepath.Join(root, "scratch", target),
		cached:  filepath.Join(root, manifest.FFmpeg, cacheKey(manifest, target), target),
		bundle:  filepath.Join(repo, "internal", "infrastructure", "ffmpeg", "bundle", target),
	}
}

// sourceName is the tarball the release is published as, and sourceDir is the directory it extracts
// to. They are the same name with the suffix taken off, which is what ffmpeg's own releases are.
func sourceName(manifest bundle.Manifest) string {
	return "ffmpeg-" + manifest.FFmpeg + ".tar.xz"
}

func sourceDir(manifest bundle.Manifest) string {
	return strings.TrimSuffix(sourceName(manifest), ".tar.xz")
}

// cacheKey is what has to be the same for a compiled ffmpeg to be reused: the release, the target, the
// configure recipe, and that target's toolchain and check environment.
func cacheKey(manifest bundle.Manifest, target string) string {
	record := manifest.Targets[target]

	parts := []string{
		manifest.FFmpeg,
		target,
		strings.Join(manifest.Recipe, "\n"),
		record.Container,
		strings.Join(record.Packages, " "),
		strings.Join(record.Cross, " "),
		strings.Join(record.CFlags, " "),
		strings.Join(record.LDFlags, " "),
		record.Conformance.Container,
		strings.Join(record.Conformance.Packages, " "),
		strings.Join(record.Conformance.Run, " "),
	}

	return sha256Of([]byte(strings.Join(parts, "\n")))
}

func sha256Of(body []byte) string {
	sum := sha256.Sum256(body)

	return hex.EncodeToString(sum[:])
}

// podmanRun is how every step and every conformance command enters a container.
//
// --userns=host is what makes the artifacts the container writes belong to the operator who ran the
// build. Without it, rootless podman maps the container's root to a subuid: the tree it compiled in
// comes back owned by somebody the build cannot read, overwrite, or throw away, and the next build
// cannot start.
func podmanRun() []string {
	return []string{"run", "--rm", "--userns=host"}
}

// ffmpegTarget is the target the ffmpeg phase compiles for: the one the operator named, or the one this
// machine is.
func ffmpegTarget(o options) string {
	if o.target != "" {
		return o.target
	}

	return runtimeTarget()
}

// planFFmpeg decides what the ffmpeg phase does for one target.
//
// A target the manifest has no record for is not a failure: macOS and FreeBSD are release targets this
// project ships with the ffmpeg their host installs, and the release matrix asks for them the same way
// it asks for the covered ones. A target the manifest does have a record for is a promise, and a build
// that cannot keep it says so instead of quietly producing a server that will do without.
func planFFmpeg(o options, repo string, manifest bundle.Manifest, state ffmpegState) (ffmpegPlan, error) {
	target := ffmpegTarget(o)

	record, covered := manifest.Targets[target]
	if !covered {
		return ffmpegPlan{Target: target, Says: "no ffmpeg is compiled for " + target}, nil
	}

	paths := ffmpegPathsFor(repo, manifest, target)
	source := ffmpegSource{Path: paths.source, URL: manifest.Source.URL}

	if state.bundleIsCached {
		source.Fetch = false

		return ffmpegPlan{
			Target: target,
			Says:   "reusing the ffmpeg compiled for " + target,
			Source: source,
			Paths:  paths,
		}, nil
	}

	if !state.podmanIsThere {
		return ffmpegPlan{}, fmt.Errorf("compiling the ffmpeg for %s needs podman, which this machine does not have", target)
	}

	key := cacheKey(manifest, target)
	image := "rdio-ffmpeg:" + target + "-" + key[:12]
	runner := image

	if record.Conformance.Container != "" {
		runner = "rdio-ffmpeg:" + target + "-check-" + key[:12]
	}

	source.Fetch = !state.sourceIsCached

	plan := ffmpegPlan{
		Target: target,
		Source: source,
		Paths:  paths,
		Image:  image,
		Runner: runner,
	}

	packages := record.Packages
	if record.Conformance.Container == "" {
		packages = append(append([]string{}, record.Packages...), record.Conformance.Packages...)
	}

	plan.Steps = append(plan.Steps, step{
		label:  "image",
		result: image,
		dir:    repo,
		name:   "podman",
		args:   buildImageArgs(image, record.Container, packages),
	})

	if runner != image {
		plan.Steps = append(plan.Steps, step{
			label:  "runner",
			result: runner,
			dir:    repo,
			name:   "podman",
			args:   buildImageArgs(runner, record.Conformance.Container, record.Conformance.Packages),
		})
	}

	// Each target starts from a pristine copy of the tarball: a tree that has already been configured
	// for another target fails later with ffbuild/config.mak is missing, which reads like a broken
	// recipe and is not one. The container does the throwing away because it is the container that owns
	// what is left in there.
	plan.Steps = append(plan.Steps, step{
		label:  "clean",
		result: "/work",
		dir:    repo,
		name:   "podman",
		args: append(podmanRun(),
			"-v", paths.scratch+":/work",
			image,
			"find", "/work", "-mindepth", "1", "-delete",
		),
	})

	plan.Steps = append(plan.Steps, step{
		label:  "unpack",
		result: "/work/" + sourceDir(manifest),
		dir:    repo,
		name:   "podman",
		args: append(podmanRun(),
			"-v", paths.sources+":/src:ro",
			"-v", paths.scratch+":/work",
			image,
			"tar", "-xJf", "/src/"+sourceName(manifest), "-C", "/work",
		),
	})

	plan.Steps = append(plan.Steps, configureStep(repo, paths, image, manifest, record))

	plan.Steps = append(plan.Steps, step{
		label:  "make",
		result: "ffmpeg and ffprobe for " + target,
		dir:    repo,
		name:   "podman",
		args: append(podmanRun(),
			"-v", paths.scratch+":/work",
			"-w", "/work/"+sourceDir(manifest),
			image,
			"make", "-j", strconv.Itoa(runtime.NumCPU()),
		),
	})

	// The tree the container compiled in belongs to whatever uid podman gave the container, which is
	// not a uid this build can name: rootless podman maps it into a subuid range, and the ownership
	// ffmpeg's own tarball carries lands somewhere else again. Chasing the uid is a dead end — the
	// numbers differ between rootless and rootful podman, and a wrong one leaves a tree the next build
	// cannot even open. Making the tree readable, writable and traversable by everybody in it is what
	// the build actually needs, and it does not depend on which uid turned out to be which.
	plan.Steps = append(plan.Steps, step{
		label:  "chmod",
		result: paths.scratch,
		dir:    repo,
		name:   "podman",
		args: append(podmanRun(),
			"-v", paths.scratch+":/work",
			image,
			"chmod", "-R", "a+rwX", "/work",
		),
	})

	return plan, nil
}

// buildImageArgs prepares the container one target is compiled or checked in. The build context is an
// empty directory on purpose: the image is a toolchain, and nothing of this repository belongs in it.
func buildImageArgs(image string, base string, packages []string) []string {
	args := []string{
		"build",
		"-t", image,
		"-f", "Containerfile.ffmpeg",
		"--build-arg", "base=" + base,
		"--build-arg", "packages=" + strings.Join(packages, " "),
		filepath.Join(".build", "ffmpeg", "context"),
	}

	return args
}

// configureStep is the trimmed build asked of the source: the recipe, then what makes the compile cross
// to this target, then the flags that link it statically for the target's word size.
//
// --enable-cross-compile and ffmpeg's own win32 and win64 operating-system names are what the manifest
// records, because both mistakes surface as a Makefile error rather than as a cross-compile error.
func configureStep(repo string, paths ffmpegPaths, image string, manifest bundle.Manifest, record bundle.Target) step {
	args := append(podmanRun(),
		"-v", paths.scratch+":/work",
		"-w", "/work/"+sourceDir(manifest),
		image,
		"./configure",
	)

	args = append(args, manifest.Recipe...)
	args = append(args, record.Cross...)

	if len(record.CFlags) > 0 {
		args = append(args, "--extra-cflags="+strings.Join(record.CFlags, " "))
	}

	if len(record.LDFlags) > 0 {
		args = append(args, "--extra-ldflags="+strings.Join(record.LDFlags, " "))
	}

	return step{
		label:  "configure",
		result: "the trimmed build for " + record.Container,
		dir:    repo,
		name:   "podman",
		args:   args,
	}
}

// programSuffix is what a target's operating system puts on the end of a program it runs.
func programSuffix(target string) string {
	if strings.HasPrefix(target, "windows-") {
		return ".exe"
	}

	return ""
}

// checkSource is the integrity anchor of this feature. ffmpeg.org signs its releases with GPG and
// publishes no checksum, so the digest recorded in the manifest when the release was chosen is what
// every fetch is checked against, and a mismatch stops the build.
func checkSource(path string, manifest bundle.Manifest) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("the ffmpeg source is not there to be checked: %w", err)
	}

	defer file.Close()

	hash := sha256.New()

	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("the ffmpeg source cannot be read to be checked: %w", err)
	}

	got := hex.EncodeToString(hash.Sum(nil))

	if got != manifest.Source.SHA256 {
		return fmt.Errorf(
			"%s is %s, and this build only compiles the ffmpeg release whose sha256 is %s",
			filepath.Base(path), got, manifest.Source.SHA256,
		)
	}

	return nil
}

// fetchSource puts the release tarball in the build cache. It is fetched by the tool rather than by a
// program the operator has to install, so that podman is the only new prerequisite this build adds.
func fetchSource(src ffmpegSource) error {
	client := &http.Client{Timeout: 10 * time.Minute}

	resp, err := client.Get(src.URL)
	if err != nil {
		return fmt.Errorf("the ffmpeg source cannot be fetched from %s: %w", src.URL, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s is not the ffmpeg release this build wants: %s", src.URL, resp.Status)
	}

	temporary, err := os.CreateTemp(filepath.Dir(src.Path), filepath.Base(src.Path)+".*")
	if err != nil {
		return fmt.Errorf("the build cannot prepare a place for the ffmpeg source: %w", err)
	}

	defer os.Remove(temporary.Name())

	if _, err := io.Copy(temporary, resp.Body); err != nil {
		temporary.Close()

		return fmt.Errorf("the ffmpeg source did not arrive whole: %w", err)
	}

	if err := temporary.Close(); err != nil {
		return fmt.Errorf("the ffmpeg source did not arrive whole: %w", err)
	}

	if err := os.Rename(temporary.Name(), src.Path); err != nil {
		return fmt.Errorf("the build cannot put the ffmpeg source where it looks for it: %w", err)
	}

	return nil
}

// runFFmpeg produces the ffmpeg a server binary built on this machine is going to carry.
func (t tool) runFFmpeg(o options, stdout io.Writer) error {
	manifest := bundle.ReadManifest()

	podman, err := t.resolve("podman")

	target := ffmpegTarget(o)

	paths := ffmpegPathsFor(t.repo, manifest, target)

	record, covered := manifest.Targets[target]

	state := ffmpegState{
		podmanIsThere:  err == nil && podman != "",
		sourceIsCached: aFileIsThere(paths.source),
		bundleIsCached: covered && aFileIsThere(paths.cachedProgram("ffmpeg.gz")) && aFileIsThere(paths.cachedProgram("ffprobe.gz")),
	}

	plan, err := planFFmpeg(o, t.repo, manifest, state)
	if err != nil {
		return err
	}

	if plan.Says != "" {
		fmt.Fprintf(stdout, "%-7s %s\n", subFFmpeg, plan.Says)
	}

	if len(plan.Steps) == 0 {
		return nil
	}

	if err := prepareFFmpeg(plan.Paths); err != nil {
		return err
	}

	if plan.Source.Fetch {
		fmt.Fprintf(stdout, "%-7s %s\n", "fetch", plan.Source.URL)

		if err := fetchSource(plan.Source); err != nil {
			return err
		}
	}

	if err := checkSource(plan.Source.Path, manifest); err != nil {
		return err
	}

	for _, s := range plan.Steps {
		if err := t.execute(s); err != nil {
			return fmt.Errorf("%s: %w", s.label, err)
		}

		fmt.Fprintf(stdout, "%-7s %s\n", s.label, s.result)
	}

	report, err := t.runConformance(podman, plan, manifest, record, target)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "%-7s %d conversions, %s s to %s s\n", "check", report.Conversions,
		strconv.FormatFloat(report.Shortest, 'f', 6, 64),
		strconv.FormatFloat(report.Longest, 'f', 6, 64),
	)

	if err := installFFmpeg(plan.Paths, manifest, target); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "%-7s %s\n", subFFmpeg, plan.Paths.bundle)

	return nil
}

// prepareFFmpeg makes the directories the phase writes into. The scratch directory is emptied by the
// container that owns what is left in it, in the plan, rather than here.
func prepareFFmpeg(paths ffmpegPaths) error {
	for _, dir := range []string{paths.sources, paths.context, paths.scratch, paths.cached} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("the build cannot prepare %s: %w", dir, err)
		}
	}

	return nil
}

// runConformance runs the compiled programs through the converter's own argument list, inside the
// container that can run them.
func (t tool) runConformance(podman string, plan ffmpegPlan, manifest bundle.Manifest, record bundle.Target, target string) (conformanceReport, error) {
	image := plan.Image
	if record.Conformance.Container != "" {
		image = plan.Runner
	}

	work := "/work/" + sourceDir(manifest)

	ffmpeg := append(append([]string{}, record.Conformance.Run...), work+"/ffmpeg"+programSuffix(target))
	ffprobe := append(append([]string{}, record.Conformance.Run...), work+"/ffprobe"+programSuffix(target))

	// The recordings the check writes out land in the scratch directory itself, which the container
	// sees at /work, and not in the source tree the compiled programs live in.
	return conformanceCheck(
		inContainer(podman, image, plan.Paths.scratch),
		conformancePrograms{FFmpeg: ffmpeg, FFProbe: ffprobe, Work: "/work"},
		plan.Paths.scratch,
	)
}

// containerRun is how the build reaches a binary it cannot run itself: the compiled ffmpeg lives in the
// mounted scratch directory, inside the container that compiled it. The image has to be named before the
// program, or podman reads the program as the image to run.
func containerRun(podman string, image string, scratch string, argv []string) []string {
	args := append([]string{podman}, podmanRun()...)
	args = append(args, "-i", "-v", scratch+":/work", image)

	return append(args, argv...)
}

// inContainer runs one program in that container, hands it a recording on its input, and hands back
// what it wrote.
func inContainer(podman string, image string, scratch string) command {
	return func(argv []string, stdin []byte) ([]byte, error) {
		args := containerRun(podman, image, scratch, argv)

		out := bytes.NewBuffer([]byte(nil))
		errout := bytes.NewBuffer([]byte(nil))

		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdin = bytes.NewReader(stdin)
		cmd.Stdout = out
		cmd.Stderr = errout

		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("%s", strings.TrimSpace(errout.String()))
		}

		return out.Bytes(), nil
	}
}

// installFFmpeg gzips the two compiled programs into the cache and into the bundle directory the server
// embeds. The placeholder that keeps a plain go build compiling is left where it is.
func installFFmpeg(paths ffmpegPaths, manifest bundle.Manifest, target string) error {
	suffix := programSuffix(target)
	source := filepath.Join(paths.scratch, sourceDir(manifest))

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		body, err := os.ReadFile(filepath.Join(source, name+suffix))
		if err != nil {
			return fmt.Errorf("the build did not produce %s for %s: %w", name, target, err)
		}

		for _, dir := range []string{paths.cached, paths.bundle} {
			if err := writeGzip(filepath.Join(dir, name+suffix+".gz"), body); err != nil {
				return err
			}
		}
	}

	return nil
}

func writeGzip(path string, body []byte) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("the build cannot write %s: %w", path, err)
	}

	defer file.Close()

	writer := gzip.NewWriter(file)

	if _, err := writer.Write(body); err != nil {
		writer.Close()

		return fmt.Errorf("the build cannot compress %s: %w", path, err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("the build cannot finish compressing %s: %w", path, err)
	}

	return nil
}

func aFileIsThere(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.Mode().IsRegular()
}
