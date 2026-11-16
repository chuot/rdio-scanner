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

// build.go is the orchestrator: the tool, the run calling the phases in order
// - webapps, ffmpeg, then the server that carries both - and the shared
// primitives every phase asks the tree. Webapps are in webapps.go, ffmpeg in ffmpeg.go.

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"rdio-scanner/cmd/build/buildinfo"

	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// versionFile is the file the release declares its core version in.
// The version a binary reports is stamped from it.
const versionFile = "VERSION.txt"

// tool is the build tool: the repository it works in, how it asks that
// repository about its version, and how it runs one step, all decided before anything runs.
type tool struct {
	repo      string
	read      func(string) (source, error)
	resolve   func(string) (string, error)
	installed func(string) bool
	execute   func(step) error

	// errout is the operator's own standard error:
	// a failure has to reach the operator even when the run's progress goes elsewhere.
	errout io.Writer

	// archive writes the release archive. It is not an external command.
	archive func(path string, files []archiveFile) error

	// clean removes what a clean run removes. It is not an external command.
	clean func(repo string) ([]string, error)

	// state is what the tree says about what has already been built;
	// a stand-in answers it, and build_test.go tests the checks.
	state func(repo string, o options) (planState, error)

	// releaseFiles reads what the archive carries, including the program this run wrote;
	// a stand-in answers it, and archive_test.go tests what it does.
	releaseFiles func(repo string, tr releaseTarget, output string) ([]archiveFile, error)

	// ffmpeg produces the ffmpeg the server binary built here embeds: fetch, check,
	// compile in a container, prove what it produced, and cache it for embedding. It
	// reports the target it wrote no ffmpeg for, and the run carries on without it.
	ffmpeg func(options, io.Writer) (bool, error)

	// prove is the gate the ffmpeg binaries pass before anything adopts them: version
	// line, conversions through the converter's own argument list, and codecs.
	prove func(target string, record bundle.Target, paths ffmpegPaths, stdout io.Writer) (theRunOfTheBinaries, error)

	// source is the ffmpeg release the manifest pins arriving in the build's cache
	// and being recognised by its digest: every compile route fetches that source.
	source func(ffmpegSource, bundle.Manifest) error

	// theRun runs one program here on a recording and answers with what it wrote;
	// the build runs its compiled ffmpeg binaries through it, where each can run.
	theRun command

	// machine is the platform the build runs on, and it decides which route a target's
	// ffmpeg takes: native toolchain on that platform, container elsewhere.
	machine func() host

	// now is the moment the run is making the container image: the image's build time is
	// a fact the run knows and the Containerfile cannot, so the run supplies it.
	now func() time.Time
}

// theMachine is the platform the build is standing on; a tool assembled without
// it - a test asking one phase - stands on this one, the answer the field would give.
func (t tool) theMachine() host {
	if t.machine == nil {
		return thisMachine()
	}

	return t.machine()
}

// newTool is the tool the build command runs with: the tree it works in,
// and every part the phases ask of it, wired in a fixed order.
func newTool(repo string) tool {
	t := tool{
		repo:         repo,
		read:         readSource,
		resolve:      resolveExecutable,
		installed:    directoryExists,
		execute:      runStep,
		archive:      writeArchive,
		clean:        cleanTree,
		state:        buildState,
		releaseFiles: archiveFiles,

		// the machine and the way programs run on it are wired with the fields,
		// before any phase is bound, so every bound phase carries them.
		machine: thisMachine,
		theRun:  onThisMachine(),
		now:     time.Now,
		errout:  os.Stderr,
	}

	t.prove = t.proveSuppliedBinaries
	t.source = fetchAndCheckSource

	t.ffmpeg = t.runFFmpeg

	return t
}

// run resolves the version, says what it resolved and why, and then runs the steps that build it.
func (t tool) run(args []string, stdout io.Writer) int {
	o, err := parseArgs(args)
	if err != nil {
		// a refused command line is a failure reaching the operator's standard
		// error with the refused word and the usage line, and it builds nothing.
		fmt.Fprintf(t.errout, "error  %v\n\nusage: go run ./cmd/build [client|scanner|admin|ffmpeg [goos-goarch]|server|dist [name-arch] [--all] [--allow-dirty]|container [linux-amd64|linux-arm64|linux-arm]|test|clean|version] [--pre <name>] [-o <path>, for the runs that write a server binary]\n", err)

		return 2
	}

	src, err := t.read(t.repo)
	if err != nil {
		return fail(t.errout, err)
	}

	version, err := buildinfo.Derive(src.core, src.tag, src.commit, o.pre, src.dirty)
	if err != nil {
		return fail(t.errout, err)
	}

	// The version subcommand answers one question and answers nothing else.
	if o.sub == subVersion {
		fmt.Fprintln(stdout, version)
		return 0
	}

	fmt.Fprintf(stdout, "version %s (%s)\n", version, explain(src, o.pre))

	if src.dirty {
		fmt.Fprintln(stdout, "note   working tree is dirty; the version does not say so")
	}

	// a name the operator asked for keeps its commit only when the tree has work
	// in it, and the log says which name carries that build metadata.
	if o.pre != "" && src.dirty && version.Commit() != "" {
		fmt.Fprintf(stdout, "note   --pre %s carries +g%s: the working tree has uncommitted work\n", o.pre, version.Commit())
	}

	// a release is built from a commit, and its archive is named for it:
	// a dirty tree is refused before anything is built, so an archive is
	// never built from work a reader cannot check out.
	if o.sub == subDist && src.dirty && !o.allowDirty {
		return fail(t.errout, fmt.Errorf("dist releases the tree as it is committed, and this tree has uncommitted work: %s\ncommit it, or say --allow-dirty to release the tree as it stands", strings.Join(src.changed, ", ")))
	}

	// the override is a maintainer releasing work in progress,
	// and the log says so out loud on every run that used it.
	if o.sub == subDist && src.dirty && o.allowDirty {
		fmt.Fprintln(stdout, "note   dist --allow-dirty: this release is built from an uncommitted tree")
	}

	// a clean run removes what a release and a client build leave behind and builds
	// nothing; it needs no npm, no ffmpeg, and no compiler, and ends where it started.
	if o.sub == subClean {
		removed, err := t.clean(t.repo)
		if err != nil {
			return fail(t.errout, fmt.Errorf("%s: %w", subClean, err))
		}

		for _, path := range removed {
			fmt.Fprintf(stdout, "%-7s %s\n", subClean, path)
		}

		return 0
	}

	// the verification run runs the suites and builds nothing: it is the command
	// a release is gated on, and the suites read the sources, so it needs no build
	if o.sub == subTest {
		return t.runTest(o, stdout)
	}

	state, err := t.state(t.repo, o)
	if err != nil {
		return fail(t.errout, err)
	}

	// the web applications come first: the server embeds both bundles,
	// so each bundle has to be on disk before the binary that embeds it is compiled.
	if err := t.runWebapps(o, version, stdout, state); err != nil {
		return fail(t.errout, err)
	}

	// the release matrix builds the applications once:
	// the bundles are the same for every target.
	if o.sub == subDist {
		return t.runDist(o, version, stdout)
	}

	// a container run walks its own list of architectures, each with its own ffmpeg and
	// its own image, so it never reaches the single-target phases below.
	if o.sub == subContainer {
		return t.runContainer(o, version, stdout)
	}

	// the ffmpeg is produced before the server is compiled: the server embeds it,
	// and one built too early quietly ships without one.
	if o.buildsFFmpeg() {
		skipped, err := t.ffmpeg(o, stdout)
		if err != nil {
			return fail(t.errout, fmt.Errorf("%s: %w", subFFmpeg, err))
		}

		// the ffmpeg phase wrote no ffmpeg for this target, so the run ends here:
		// a binary compiled now would convert with whatever the host happens to have.
		if skipped {
			return 0
		}
	}

	if o.buildsServer() {
		if err := t.runServer(o, version, stdout); err != nil {
			return fail(t.errout, fmt.Errorf("%s: %w", subServer, err))
		}
	}

	return 0
}

// runTest runs the two suites this project's verification is made of; a failing
// suite ends the run naming itself, because the release is gated on this command.
func (t tool) runTest(o options, stdout io.Writer) int {
	for _, s := range testSteps(o, t.repo) {
		if _, err := t.resolve(s.name); err != nil {
			return fail(t.errout, fmt.Errorf("%s needs %s, which is not installed here: %w", subTest, s.name, err))
		}

		if err := t.execute(s); err != nil {
			return fail(t.errout, fmt.Errorf("%s (%s): %w", s.label, s.result, err))
		}

		fmt.Fprintf(stdout, "%-7s %s\n", s.label, s.result)
	}

	return 0
}

// runServer compiles the server binary for the target being built. The server
// embeds both bundles and the ffmpeg, so the tree's state is asked again after
// those phases write them, and a binary that is current is not compiled again.
func (t tool) runServer(o options, version buildinfo.Version, stdout io.Writer) error {
	state, err := t.state(t.repo, o)
	if err != nil {
		return err
	}

	if state.serverCurrent {
		return nil
	}

	s := serverStep(o, t.repo, version)

	if err := t.execute(s); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "%-7s %s\n", s.label, s.result)

	return nil
}

// runDist builds the release for the targets asked for: this platform, the whole matrix,
// or the named target, each target's ffmpeg before the server that embeds it. A failed
// ffmpeg ends the run; a skipped target writes no archive and the run carries on.
func (t tool) runDist(o options, version buildinfo.Version, stdout io.Writer) int {
	// the target list is decided once, before any of it runs,
	// so an impossible request is refused before a single application is built.
	targets, err := distTargets(o, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return fail(t.errout, err)
	}

	// a single target's log needs no prefix;
	// every line of a matrix run has to name the target it is about.
	prefix := len(targets) > 1

	var mu sync.Mutex

	log := func(target string) io.Writer {
		if !prefix {
			return stdout
		}

		return &theReleaseLog{mu: &mu, target: target, out: stdout}
	}

	// the run keeps what it wrote and what wrote none,
	// so its last line answers whether the release asked for is the release it has.
	var written []string
	var none []string

	// the ffmpeg phase stays serial and finishes before any server compile starts,
	// so the log records one target's block of lines at a time.
	ready := make([]releaseTarget, 0, len(targets))

	for _, tr := range targets {
		o.target = tr.targetWord()
		o.output = defaultOutputPath(o.target)

		skipped, err := t.ffmpeg(o, log(o.target))
		if err != nil {
			return fail(t.errout, fmt.Errorf("%s for %s: %w", subFFmpeg, o.target, err))
		}

		// this target wrote no ffmpeg: the run carries on to targets it can produce,
		// and says at the end which targets wrote none.
		if skipped {
			none = append(none, o.target)

			continue
		}

		ready = append(ready, tr)
	}

	workers, err := distWorkers(len(ready), t.theMachine(), os.LookupEnv)
	if err != nil {
		return fail(t.errout, err)
	}

	// server compile and archive write run at once for each ready target:
	// each touches only files its own step wrote.
	work := make([]theWorkOfOneTarget, len(ready))

	var wg sync.WaitGroup

	sem := make(chan struct{}, workers)

	for i, tr := range ready {
		wg.Add(1)

		go func(i int, tr releaseTarget) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			// each worker writes the target it is building into its own options,
			// because each target's binary is a different file.
			per := o
			per.target = tr.targetWord()
			per.output = defaultOutputPath(per.target)

			out := log(per.target)

			// the binary is current when newer than every file it was built from,
			// the bundles included.
			if err := t.runServer(per, version, out); err != nil {
				work[i] = theWorkOfOneTarget{target: per.target, err: fmt.Errorf("%s for %s: %w", subServer, per.target, err)}

				return
			}

			// the archive carries this run's binary, the documents as written,
			// and the ffmpeg licence when the target carries an ffmpeg.
			files, err := t.releaseFiles(t.repo, tr, per.output)
			if err != nil {
				work[i] = theWorkOfOneTarget{target: per.target, err: fmt.Errorf("%s for %s: %w", subArchive, per.target, err)}

				return
			}

			path := archivePath(per.target, version.String())

			if err := t.archive(path, files); err != nil {
				work[i] = theWorkOfOneTarget{target: per.target, err: fmt.Errorf("%s for %s: %w", subArchive, per.target, err)}

				return
			}

			fmt.Fprintf(out, "%-7s %s\n", subArchive, path)

			work[i] = theWorkOfOneTarget{target: per.target, path: path}
		}(i, tr)
	}

	wg.Wait()

	// the release reports what it wrote in the matrix's target order,
	// whichever order the workers finished in.
	for _, w := range work {
		if w.err != nil {
			return fail(t.errout, w.err)
		}

		written = append(written, w.path)
	}

	// the release folder is the set to upload, so other versions' archives
	// are removed as soon as this run's are written, and each removal is named.
	pruned, err := pruneReleaseFolder(t.repo, version.String())
	if err != nil {
		return fail(t.errout, err)
	}

	for _, path := range pruned {
		fmt.Fprintf(stdout, "%-7s removed %s\n", subDist, path)
	}

	// the exit status says whether the release asked for is the release written:
	// an incomplete release is a fact a script has to tell from a broken step.
	if len(none) == 0 {
		fmt.Fprintf(stdout, "%-7s %s written\n", subDist, archivesWord(len(written)))

		return 0
	}

	fmt.Fprintf(stdout, "%-7s %s written, %s wrote none: %s\n",
		subDist, archivesWord(len(written)), targetsWord(len(none)), strings.Join(none, ", "))

	return 3
}

// runContainer builds the container images for the architectures the run resolves: the machine it
// stands on, one named target, or the three architectures an image is built for. Each target's
// ffmpeg is compiled before any image is built, because the image copies the ffmpeg from disk, and
// then one image per target, in the order the tool lists the three architectures.
func (t tool) runContainer(o options, version buildinfo.Version, stdout io.Writer) int {
	// the target list is decided once, before any of it runs,
	// so a machine no container image is built for is refused before a single step runs.
	targets, err := containerTargetsFor(o, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return fail(t.errout, err)
	}

	// one run looks for the container program once, whether it builds one image or three.
	program, err := resolveContainerProgram(t.resolve, os.LookupEnv)
	if err != nil {
		return fail(t.errout, err)
	}

	// a single target's log needs no prefix;
	// every line of a three-architecture run has to name the target it is about.
	prefix := len(targets) > 1

	var mu sync.Mutex

	log := func(target string) io.Writer {
		if !prefix {
			return stdout
		}

		return &theReleaseLog{mu: &mu, target: target, out: stdout}
	}

	// the ffmpeg phase stays serial and finishes before any image build starts: an image
	// build copies the ffmpeg from disk, so it can only start once every target's pair is
	// compiled or refused.
	ready := make([]releaseTarget, 0, len(targets))

	var none []string

	for _, tr := range targets {
		o.target = tr.targetWord()

		skipped, err := t.ffmpeg(o, log(o.target))
		if err != nil {
			return fail(t.errout, fmt.Errorf("%s for %s: %w", subFFmpeg, o.target, err))
		}

		// this target wrote no ffmpeg: the image would carry whatever ffmpeg the machine
		// happens to have, so the run builds no image for it and says at the end which
		// targets wrote none.
		if skipped {
			none = append(none, o.target)

			continue
		}

		ready = append(ready, tr)
	}

	// the image builds run one after another and not at once: each one compiles a server
	// inside its own image, and the container program builds one image at a time.
	var written []string

	var failed error

	for _, tr := range ready {
		per := o
		per.target = tr.targetWord()

		out := log(per.target)

		s := containerStep(per, t.repo, version, t.now(), program)

		if err := t.execute(s); err != nil {
			// a target whose image build failed is one target's failure: the targets after
			// it still get their images, and the run ends naming the one it could not build.
			if failed == nil {
				failed = fmt.Errorf("%s for %s: %w", s.label, per.target, err)
			}

			continue
		}

		fmt.Fprintf(out, "%-7s %s\n", s.label, s.result)

		written = append(written, s.result)
	}

	if failed != nil {
		return fail(t.errout, failed)
	}

	// the exit status says whether the images asked for are the images written:
	// an incomplete set of images is a fact a script has to tell from a broken step.
	if len(none) == 0 {
		fmt.Fprintf(stdout, "%-7s %s written\n", subContainer, imagesWord(len(written)))

		return 0
	}

	fmt.Fprintf(stdout, "%-7s %s written, %s wrote none: %s\n",
		subContainer, imagesWord(len(written)), targetsWord(len(none)), strings.Join(none, ", "))

	return 3
}

// theWorkOfOneTarget is what one release worker says when it finishes:
// the archive it wrote, or the failure it stopped on, named for its target.
type theWorkOfOneTarget struct {
	target string
	path   string
	err    error
}

// theReleaseLog is a target's output in a matrix run: every line arrives whole,
// naming its target, and the lines are written under one lock so none interleaves.
type theReleaseLog struct {
	mu      *sync.Mutex
	target  string
	out     io.Writer
	pending []byte
}

func (l *theReleaseLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.pending = append(l.pending, p...)

	for {
		end := bytes.IndexByte(l.pending, '\n')
		if end < 0 {
			break
		}

		if _, err := fmt.Fprintf(l.out, "%s  %s\n", l.target, l.pending[:end]); err != nil {
			return 0, err
		}

		l.pending = l.pending[end+1:]
	}

	return len(p), nil
}

// buildJobsEnv names the bound an operator sets on the server compiles of a release run.
const buildJobsEnv = "RDIO_BUILD_JOBS"

// distWorkers is how many server compiles a release run runs at once: the cores of the machine the run names,
// ready targets, and the operator's bound, whichever is fewest. The cores come from the machine description the
// run already carries for deciding which targets it can build: the server compiles of a release run run on that
// machine, so its own count is the one to size them with, and the ffmpeg compile's job count comes from the same
// place. A bound that is not a count of work is refused; an empty variable leaves the machine's bound standing.
func distWorkers(ready int, machine host, lookup func(string) (string, bool)) (int, error) {
	if ready == 0 {
		return 0, nil
	}

	workers, err := strconv.Atoi(machine.howManyCoresCanACompileOnMeCheckOut())
	if err != nil {
		return 0, err
	}

	if value, ok := lookup(buildJobsEnv); ok && value != "" {
		named, err := strconv.Atoi(value)
		if err != nil || named < 1 {
			return 0, fmt.Errorf("%s is %q, which is not a count of work: name a whole number of server compiles to run at once, like %s=4",
				buildJobsEnv, value, buildJobsEnv)
		}

		if named < workers {
			workers = named
		}
	}

	if ready < workers {
		workers = ready
	}

	return workers, nil
}

// archivesWord and targetsWord say a count and its noun
// the way the release log reads: one archive, two archives.
func archivesWord(n int) string {
	if n == 1 {
		return "1 archive"
	}

	return fmt.Sprintf("%d archives", n)
}

func targetsWord(n int) string {
	if n == 1 {
		return "1 target"
	}

	return fmt.Sprintf("%d targets", n)
}

// imagesWord says how many images a container run wrote the way the release log
// reads the archives it wrote: one image, two images.
func imagesWord(n int) string {
	if n == 1 {
		return "1 image"
	}

	return fmt.Sprintf("%d images", n)
}

// fail says what stopped the build and returns the exit code that says a step did not do its job.
func fail(errout io.Writer, err error) int {
	fmt.Fprintf(errout, "error  %v\n", err)

	return 1
}

// repositoryRoot is the directory the tool builds: git knows it,
// and a tree without git is built where the operator stood, as go run was.
func repositoryRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}

	return os.Getwd()
}

// resolveExecutable finds a program the tool has to run: npm is npm.cmd on
// Windows, and a failure names the program that was looked for.
func resolveExecutable(name string) (string, error) {
	if runtime.GOOS == "windows" {
		if path, err := exec.LookPath(name + ".cmd"); err == nil {
			return path, nil
		}
	}

	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}

	return path, nil
}

// directoryExists is how the tool knows the client dependencies are already installed.
func directoryExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.IsDir()
}

// cleanTree removes what a clean run removes: the release output, the client
// dependencies, the bundle contents, and the ffmpeg phase's disposable scratch.
// It leaves the compiled ffmpeg standing and prunes the ffmpeg cache.
func cleanTree(repo string) ([]string, error) {
	targets := []string{filepath.Join(repo, "dist"), filepath.Join(repo, presentationDir, "node_modules")}

	targets = append(targets, ffmpegScratch(repo)...)

	for _, app := range []string{subScanner, subAdmin} {
		bundle := filepath.Join(repo, presentationDir, app, "frontend", "bundle")

		entries, err := os.ReadDir(bundle)
		if err != nil {
			// a tree that has never been built has no bundle contents to remove
			if os.IsNotExist(err) {
				continue
			}

			return nil, fmt.Errorf("the clean run cannot read %s: %w", bundle, err)
		}

		for _, entry := range entries {
			if entry.Name() == ".gitkeep" {
				continue
			}

			targets = append(targets, filepath.Join(bundle, entry.Name()))
		}
	}

	var removed []string

	for _, path := range targets {
		if _, err := os.Lstat(path); err != nil {
			continue
		}

		if err := os.RemoveAll(path); err != nil {
			return nil, fmt.Errorf("the clean run cannot remove %s: %w", path, err)
		}

		removed = append(removed, path)
	}

	// The cache is pruned after the scratch is gone, so the tree it is read from is the tree a clean
	// run left.
	pruned, err := pruneFFMpegCache(repo)
	if err != nil {
		return nil, err
	}

	removed = append(removed, pruned...)

	return removed, nil
}

// serverSources is every file the server binary is built from: the Go files,
// both bundle contents, the declared core version file, and the module files.
func serverSources(repo string) ([]string, error) {
	var files []string

	for _, root := range []string{"cmd", "internal"} {
		if err := walkFiles(filepath.Join(repo, root), func(path string) {
			if strings.HasSuffix(path, ".go") {
				files = append(files, path)
			}
		}); err != nil {
			return nil, err
		}
	}

	for _, app := range []string{subScanner, subAdmin} {
		bundle := filepath.Join(repo, appBundle[app])

		if err := walkFiles(bundle, func(path string) {
			if filepath.Base(path) != ".gitkeep" {
				files = append(files, path)
			}
		}); err != nil {
			return nil, err
		}
	}

	// the reported version is stamped from the declared core and the dependencies
	// resolve from the module files, so a binary predating either is not current;
	// a missing file has no date newer than a binary.
	for _, name := range []string{versionFile, "go.mod", "go.sum"} {
		path := filepath.Join(repo, name)

		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return nil, fmt.Errorf("the build cannot read %s: %w", path, err)
		}

		if info.IsDir() {
			continue
		}

		files = append(files, path)
	}

	return files, nil
}

// walkFiles calls visit for every file under a directory;
// a directory that is not there has no files.
func walkFiles(root string, visit func(path string)) error {
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}

			return fmt.Errorf("the build cannot read %s: %w", path, err)
		}

		if !entry.IsDir() {
			visit(path)
		}

		return nil
	})

	return err
}

// serverIsCurrent reports whether the binary is newer than every file it is
// built from; a binary that is not there is not current.
func serverIsCurrent(repo string, output string) (bool, error) {
	return newerThan(repo, filepath.Join(repo, output), serverSources)
}

// newerThan is the one question both checks ask: is the thing that was built newer than every file it
// was built from.
func newerThan(repo string, built string, sources func(string) ([]string, error)) (bool, error) {
	builtInfo, err := os.Stat(built)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, fmt.Errorf("the build cannot read %s: %w", built, err)
	}

	paths, err := sources(repo)
	if err != nil {
		return false, err
	}

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return false, fmt.Errorf("the build cannot read %s: %w", path, err)
		}

		if info.ModTime().After(builtInfo.ModTime()) {
			return false, nil
		}
	}

	return true, nil
}

// buildState is what the tree says about what has already been built,
// checked only for the phases this run is going to run.
func buildState(repo string, o options) (planState, error) {
	state := planState{}

	if o.buildsClients() {
		current, err := bundleIsCurrent(repo, subScanner)
		if err != nil {
			return state, err
		}

		state.scannerCurrent = current

		if current, err := bundleIsCurrent(repo, subAdmin); err != nil {
			return state, err
		} else {
			state.adminCurrent = current
		}
	}

	if o.buildsServerBinary() {
		current, err := serverIsCurrent(repo, o.output)
		if err != nil {
			return state, err
		}

		state.serverCurrent = current
	}

	return state, nil
}

// runStep runs one step in its own directory with its extra environment,
// and lets whatever the program says reach the operator.
func runStep(s step) error {
	cmd := exec.Command(s.name, s.args...)
	cmd.Dir = s.dir
	cmd.Env = append(os.Environ(), s.env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
