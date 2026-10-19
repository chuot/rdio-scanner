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

// build.go is the orchestrator: the tool, the run that calls the phases in order — the web
// applications, then the ffmpeg, then the server that carries both — and the shared primitives
// every phase asks the tree. The web applications are in webapps.go, the ffmpeg is in ffmpeg.go.

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

// versionFile is the file the release declares its core version in. The version a server binary
// reports is stamped from it, so the binary is built from it.
const versionFile = "VERSION.txt"

// tool is the build tool: where the repository is, how it asks that repository what it says about
// its own version, and how it runs one step. Everything it decides is decided before anything runs.
type tool struct {
	repo      string
	read      func(string) (source, error)
	resolve   func(string) (string, error)
	installed func(string) bool
	execute   func(step) error

	// errout is the operator's own standard error. The run's lines go to the writer the run was given,
	// and a failure goes here: the line that says the run stopped has to reach the operator even when
	// the run's progress is being written somewhere else.
	errout io.Writer

	// archive writes the release archive. It is not an external command.
	archive func(path string, files []archiveFile) error

	// clean removes what a clean run removes. It is not an external command.
	clean func(repo string) ([]string, error)

	// state is what the tree says about what has already been built. It reads the tree, so the stand-in
	// answers it: what the checks do is tested in build_test.go.
	state func(repo string, o options) (planState, error)

	// releaseFiles reads what the archive carries, including the program the run wrote to. It reads the
	// tree, so the stand-in answers it: what it does is tested in archive_test.go.
	releaseFiles func(repo string, tr releaseTarget, output string) ([]archiveFile, error)

	// ffmpeg produces the ffmpeg a server binary built here is going to carry. It is not one external
	// command: it fetches a pinned release, checks it, compiles it in a container, runs what it produced
	// through the converter's own argument list, and puts it where the server embeds it.
	//
	// It reports the target it has nothing to produce for: the three targets whose ffmpeg binaries are
	// compiled on their own platform are compiled there, by a build standing on that machine, and a release run
	// asked for one of them on a machine that is not that platform, with nothing dropped for it, has no
	// ffmpeg to embed. That is not a failure, and the run that was asked for the whole matrix ships the
	// archives whose ffmpeg it can produce.
	ffmpeg func(options, io.Writer) (bool, error)

	// prove is the gate the ffmpeg binaries pass before anything adopts them: the version line the
	// programs say, the conversions the build runs them through, and the codecs they register. It runs
	// them on the machine standing here, which is the machine that compiled them, and it records nothing.
	prove func(target string, paths ffmpegPaths, stdout io.Writer) (theRunOfTheBinaries, error)

	// source is the ffmpeg release the manifest pins arriving in the build's cache and being recognised
	// as that release: the fetch when the tree does not have it, and the digest that says it is. Every
	// compile passes through it, in a container and on a target's own host alike, because the source is
	// the one input both of them fetch over the network.
	source func(ffmpegSource, bundle.Manifest) error

	// theRun is one program run here, handed a recording on its input, answering with what it wrote. It
	// is how the build runs the ffmpeg binaries it compiled: the container targets run them inside the
	// container that can run them, and a supplied target's binaries run on the machine that compiled them.
	theRun command

	// machine is the platform the build is running on. A supplied target is decided by it — the ffmpeg
	// binaries are compiled by the machine that is the platform they run on, and adopted by every other —
	// so the plan
	// the run composes is composed with it, and the run asks it of the machine rather than reading it in
	// the middle of the plan.
	machine func() host

	// now is the moment the run is making the container image. The image's build time is a fact the run
	// knows and the Containerfile cannot, and a test that reads the command line the run composes has to
	// be told what the clock said.
	now func() time.Time
}

// theMachine is the platform the build is standing on. A tool assembled without it — a test that asks one
// phase and names no machine — stands on this one, which is the answer the field would give.
func (t tool) theMachine() host {
	if t.machine == nil {
		return thisMachine()
	}

	return t.machine()
}

// newTool is the tool the build command runs with: the tree it works in, and every part the phases ask
// of it.
//
// The parts the phases ask for are wired before the phases themselves are bound, because taking a method
// value of the tool captures the tool as it is at that moment: a phase bound first carries the copy made
// before the fields after it were set, and the panic is in the phase, on the machine the operator is
// standing on, where no test runs.
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

		// The machine standing here and the way one program is run on it are wired with the fields,
		// before any phase is bound: the run that adopts a drop asks the machine what it ran, and a
		// phase bound now would carry the copy made before them.
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
		// A command line the tool does not accept is a failure, and a failure reaches the operator's
		// standard error. The word that was refused and the line naming the commands the tool has are
		// the explanation of that failure, not a line of the run's progress, and a refused command line
		// builds nothing: the writer the run was given carries no progress at all.
		fmt.Fprintf(t.errout, "error  %v\n\nusage: go run ./cmd/build [client|scanner|admin|ffmpeg [goos-goarch]|server|dist [name-arch] [--all] [--allow-dirty]|container|test|clean|version] [--pre <name>] [-o <path>, for the runs that write a server binary]\n", err)

		return 2
	}

	src, err := t.read(t.repo)
	if err != nil {
		return fail(t.errout, err)
	}

	version, err := buildinfo.Derive(src.core, src.tag, src.commit, o.pre)
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

	// The release is built from a commit, and the archive a release writes is named for it. A tree with
	// uncommitted work in it is not that commit, and the person who downloads the archive cannot tell
	// which of the two the binary came from. The refusal comes before anything is built, so a release
	// run asked to release an uncommitted tree builds nothing.
	if o.sub == subDist && src.dirty && !o.allowDirty {
		return fail(t.errout, fmt.Errorf("dist releases the tree as it is committed, and this tree has uncommitted work: %s\ncommit it, or say --allow-dirty to release the tree as it stands", strings.Join(src.changed, ", ")))
	}

	// The override is a maintainer releasing work in progress, and the log says so on every run that
	// used it: an archive built from a tree that is not the commit it names is only honest when the run
	// that wrote it said so out loud.
	if o.sub == subDist && src.dirty && o.allowDirty {
		fmt.Fprintln(stdout, "note   dist --allow-dirty: this release is built from an uncommitted tree")
	}

	// A clean run removes what a release and a client build leave behind, and builds nothing. It needs
	// no npm, no ffmpeg, and no compiler, so it ends the run where it started.
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

	// The verification run runs the suites and builds nothing: the command a developer runs before
	// pushing is the command a release is gated on, and a run that built something first reported the
	// build's result and not the suites'. It needs no build output, no installed client dependencies,
	// and no container program: the suites read the sources.
	if o.sub == subTest {
		return t.runTest(o, stdout)
	}

	state, err := t.state(t.repo, o)
	if err != nil {
		return fail(t.errout, err)
	}

	// The web applications come first: the server embeds both bundles, so a bundle built in this
	// run has to be on disk before the binary that embeds it is compiled.
	if err := t.runWebapps(o, version, stdout, state); err != nil {
		return fail(t.errout, err)
	}

	// The release matrix builds the applications once, because the bundles are the same for every
	// target, and then each target's ffmpeg before the server that embeds it.
	if o.sub == subDist {
		return t.runDist(o, version, stdout)
	}

	// The ffmpeg is produced before the server is compiled, because the server embeds it: a server built
	// before the ffmpeg its target carries exists is a server that quietly ships without one.
	if o.buildsFFmpeg() {
		skipped, err := t.ffmpeg(o, stdout)
		if err != nil {
			return fail(t.errout, fmt.Errorf("%s: %w", subFFmpeg, err))
		}

		// The ffmpeg binaries this target carries are compiled on their own platform and were not
		// dropped for it, and the message the ffmpeg phase printed says where to build them and where to
		// drop them. A server
		// compiled now embeds no ffmpeg and converts with whatever the host happens to have, which is
		// not what this target releases, so the run ends here, successfully, and compiles no binary.
		if skipped {
			return 0
		}
	}

	// The container image compiles its own server, so the run that prepares it ends with the container
	// program: the applications and the ffmpeg the image's architecture embeds are what the Containerfile
	// needs on disk, and a server binary built here is one the image throws away.
	if o.sub == subContainer {
		program, err := resolveContainerProgram(t.resolve, os.LookupEnv)
		if err != nil {
			return fail(t.errout, err)
		}

		s := containerStep(o, t.repo, version, t.now(), program)

		if err := t.execute(s); err != nil {
			return fail(t.errout, fmt.Errorf("%s: %w", s.label, err))
		}

		fmt.Fprintf(stdout, "%-7s %s\n", s.label, s.result)

		return 0
	}

	if o.buildsServer() {
		if err := t.runServer(o, version, stdout); err != nil {
			return fail(t.errout, fmt.Errorf("%s: %w", subServer, err))
		}
	}

	return 0
}

// runTest runs the two suites this project's verification is made of. A suite that fails ends the run,
// and names the suite that failed: the release is gated on this command, and a run that carried on past
// a failing suite would report success to whatever gates on it.
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

// runServer compiles the server binary for the target the run is building for. The server embeds
// both bundles and the ffmpeg, so the state is asked again after the phases that write them: the
// bundles built in this run are newer than the binary built before them, and a run that rebuilt a
// bundle and left the binary standing ships the bundles the binary was built before. A binary that
// is current is not compiled again.
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

// runDist builds the release for the targets the run was asked for: the platform it is standing on, the
// whole matrix when the whole matrix was asked for, or the one target the operator named. Each target's
// ffmpeg is produced before the server that embeds it, for the same reason a single run produces its
// ffmpeg first.
//
// A target whose ffmpeg fails ends the run and names the target it stopped at. A half matrix is not a
// release, and a run that carried on would leave an archive directory with a server in it and no ffmpeg
// to embed.
//
// A target whose ffmpeg binaries are compiled on its own platform is not compiled, and not dropped for
// it, by the machine running a matrix run: they are produced on a machine the release machine does not
// control,
// and a run that stopped for it would ship nothing at all. It is skipped, the run ships the rest, and
// the run's last line names the targets that wrote no archive and says, by its exit status, that the
// release it was asked for is not the release it wrote.
func (t tool) runDist(o options, version buildinfo.Version, stdout io.Writer) int {
	// The list is decided once, before any of it runs, so that a run asked for the machine it is
	// standing on is refused before a single application is built, and not after the first archive.
	targets, err := distTargets(o, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return fail(t.errout, err)
	}

	// A run asked for one target writes one target's lines, and those lines need no owner: the log of a
	// matrix run is written by several targets, and every line of it has to say which one it is about.
	prefix := len(targets) > 1

	var mu sync.Mutex

	log := func(target string) io.Writer {
		if !prefix {
			return stdout
		}

		return &theReleaseLog{mu: &mu, target: target, out: stdout}
	}

	// The run keeps what it wrote and what wrote none, because the release's last line is the answer to
	// the question the operator asked: is the release I asked for the release I have.
	var written []string
	var none []string

	// The ffmpeg phase stays serial: it builds container images, runs qemu and wine checks, and already
	// saturates the machine inside one target. It finishes before any server compile starts, so the log
	// that records what was adopted is one target's block of lines at a time.
	ready := make([]releaseTarget, 0, len(targets))

	for _, tr := range targets {
		o.target = tr.targetWord()
		o.output = defaultOutputPath(o.target)

		skipped, err := t.ffmpeg(o, log(o.target))
		if err != nil {
			return fail(t.errout, fmt.Errorf("%s for %s: %w", subFFmpeg, o.target, err))
		}

		// The message the ffmpeg phase printed for this target says which host compiles the binaries,
		// the command that compiles them there, and the directory to drop them in. The run carries on to
		// the targets whose ffmpeg it can produce, and says at the end which targets wrote none.
		if skipped {
			none = append(none, o.target)

			continue
		}

		ready = append(ready, tr)
	}

	workers, err := distWorkers(len(ready), os.LookupEnv)
	if err != nil {
		return fail(t.errout, err)
	}

	// The server compiles and the archive writes run at once. Each target's binary is a different file,
	// built from the same sources, sharing only Go's own build cache, which is safe under concurrent
	// use, and each target's archive is read from the path its own compile wrote to.
	work := make([]theWorkOfOneTarget, len(ready))

	var wg sync.WaitGroup

	sem := make(chan struct{}, workers)

	for i, tr := range ready {
		wg.Add(1)

		go func(i int, tr releaseTarget) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			// The target each worker is building for is written into the options the server step and
			// the archive path are read from, because each target's binary is a different file.
			per := o
			per.target = tr.targetWord()
			per.output = defaultOutputPath(per.target)

			out := log(per.target)

			// The binary for this target is current when it is newer than every file it is built from,
			// and the bundles the client steps built are among them.
			if err := t.runServer(per, version, out); err != nil {
				work[i] = theWorkOfOneTarget{target: per.target, err: fmt.Errorf("%s for %s: %w", subServer, per.target, err)}

				return
			}

			// The archive carries the binary this run wrote, the documentation as the files it is
			// written in, and the ffmpeg licence when the target carries an ffmpeg.
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

	// The release reports what it wrote in the order the matrix lists the targets, whichever order the
	// workers finished in: the summary is the answer to the question the operator asked, and the answer
	// is the same answer however the work was scheduled.
	for _, w := range work {
		if w.err != nil {
			return fail(t.errout, w.err)
		}

		written = append(written, w.path)
	}

	// The release folder is the set to upload, so the archives of the versions this run did not release
	// are removed as soon as its own are written, and each removal is named.
	pruned, err := pruneReleaseFolder(t.repo, version.String())
	if err != nil {
		return fail(t.errout, err)
	}

	for _, path := range pruned {
		fmt.Fprintf(stdout, "%-7s removed %s\n", subDist, path)
	}

	// The release says what it wrote, and what it wrote none for, and its exit status says whether the
	// release it was asked for is the release it wrote. A target whose binaries are compiled on its own
	// platform is not a failure, and a run that ships the archives it can produce is doing its job; it is
	// an incomplete release, which is a fact a script has to be able to tell from a step that broke.
	if len(none) == 0 {
		fmt.Fprintf(stdout, "%-7s %s written\n", subDist, archivesWord(len(written)))

		return 0
	}

	fmt.Fprintf(stdout, "%-7s %s written, %s wrote none: %s\n",
		subDist, archivesWord(len(written)), targetsWord(len(none)), strings.Join(none, ", "))

	return 3
}

// theWorkOfOneTarget is what one worker of a release run has to say when it finishes: the archive it
// wrote, or the failure the run stops on, named for the target the work was for.
type theWorkOfOneTarget struct {
	target string
	path   string
	err    error
}

// theReleaseLog is the output a target's work writes to in a matrix run: every line arrives as one
// whole line naming the target that produced it. The lines are written under one lock, because a log
// that interleaves half a line of one target with half a line of another names nothing.
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

// distWorkers is how many server compiles a release run runs at once: the cores the machine has, the
// targets it is ready to build, and the bound the operator named, whichever is the fewest. The compiles
// are independent — each target's binary is a different file, built from the same sources, sharing only
// Go's own build cache — so the bound is about the machine's memory and the log's readability, not
// about correctness: a release run on a small machine compiling five toolchains at once is a run that
// trades the release for a machine that stops responding.
//
// A value that is not a count of work is refused, because a run that quietly ignored the bound the
// operator named would be a run doing more than the operator said. A variable set to nothing says
// nothing, and the machine's own bound stands.
func distWorkers(ready int, lookup func(string) (string, bool)) (int, error) {
	if ready == 0 {
		return 0, nil
	}

	workers := runtime.NumCPU()

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

// archivesWord and targetsWord say a count and its noun the way the release log reads: one archive,
// five archives, one target, three targets.
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

// fail says what stopped the build and returns the exit code that says a step did not do its job.
func fail(errout io.Writer, err error) int {
	fmt.Fprintf(errout, "error  %v\n", err)

	return 1
}

// repositoryRoot is the directory the tool builds. git knows it, and a tree without git is built
// where the operator stood, which is where go run stood too.
func repositoryRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}

	return os.Getwd()
}

// resolveExecutable finds a program the tool has to run. npm is npm.cmd on Windows, and naming the
// name that was looked for is the difference between "npm is not installed" and a confusing failure.
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

// cleanTree removes what a clean run removes: the release output, the client dependencies, the contents
// of both bundles, and the ffmpeg phase's disposable scratch — the configured source trees it compiled
// in, the tarball it extracted them from, the container context it built in, and the working copy of the
// binaries it ran. It leaves the compiled ffmpeg standing, because every later build reads it, and
// prunes it to the entries the manifest the build reads can produce, because an entry no target computes
// is an entry no build reads.
//
// The `.gitkeep` file in each bundle is left standing, because it is the placeholder that keeps a plain
// go build compiling: the bundle directory has to exist for the Go package that embeds it, and the
// contents are what a build puts there.
//
// The run names what it removed, so a tree that was already clean says so by naming nothing.
func cleanTree(repo string) ([]string, error) {
	targets := []string{filepath.Join(repo, "dist"), filepath.Join(repo, presentationDir, "node_modules")}

	targets = append(targets, ffmpegScratch(repo)...)

	for _, app := range []string{subScanner, subAdmin} {
		bundle := filepath.Join(repo, presentationDir, app, "frontend", "bundle")

		entries, err := os.ReadDir(bundle)
		if err != nil {
			// A tree that has never been built has no bundle contents to remove.
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

// serverSources is every file the server binary is built from: the Go files, the contents of both
// bundles, which the server embeds, the file the release declares its core version in, and the module
// files its dependencies are resolved from. A binary that is newer than every one of those files is a
// binary that reports the version the release ships and carries the bundles it was built with.
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

	// The version a binary reports is stamped from the declared core, and the dependency set is
	// resolved from the module files: a binary compiled before either of them moved reports and carries
	// what moved. A tree with no version file says nothing about its version, which the version
	// derivation answers by itself, and a file that is not there has no date newer than a binary.
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

// walkFiles calls visit for every file under a directory. A directory that is not there has no files,
// which is what a tree that has never been built is.
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

// serverIsCurrent reports whether the server binary is newer than every file it is built from. A binary
// that is not there is not current.
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

// buildState is what the tree says about what has already been built. The checks are made only for the
// phases the run is going to run: a run asked to build one application is not asked what it thinks about
// the server binary.
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

// runStep runs one step, with its own directory and its extra environment, and lets whatever the
// program says reach the operator.
func runStep(s step) error {
	cmd := exec.Command(s.name, s.args...)
	cmd.Dir = s.dir
	cmd.Env = append(os.Environ(), s.env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
