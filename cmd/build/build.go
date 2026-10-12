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

// build.go is the orchestrator: the tool, the run that calls the phases in order — the web
// applications, then the ffmpeg, then the server that carries both — and the shared primitives
// every phase asks the tree. The web applications are in webapps.go, the ffmpeg is in ffmpeg.go.

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"rdio-scanner/cmd/build/buildinfo"
)

// tool is the build tool: where the repository is, how it asks that repository what it says about
// its own version, and how it runs one step. Everything it decides is decided before anything runs.
type tool struct {
	repo      string
	read      func(string) (source, error)
	resolve   func(string) (string, error)
	installed func(string) bool
	execute   func(step) error

	// archive writes the release archive. It is not an external command.
	archive func(path string, files []archiveFile) error

	// clean removes what a clean run removes. It is not an external command.
	clean func(repo string) ([]string, error)

	// state is what the tree says about what has already been built. It reads the tree, so the stand-in
	// answers it: what the checks do is tested in build_test.go.
	state func(repo string, o options) (planState, error)

	// releaseFiles reads what the archive carries. It reads the tree, so the stand-in answers it: what it
	// does is tested in archive_test.go.
	releaseFiles func(repo string, tr releaseTarget) ([]archiveFile, error)

	// ffmpeg produces the ffmpeg a server binary built here is going to carry. It is not one external
	// command: it fetches a pinned release, checks it, compiles it in a container, runs what it produced
	// through the converter's own argument list, and puts it where the server embeds it.
	//
	// It reports the target it has nothing to produce for: the three targets whose pair is compiled on
	// their own platform are built by the maintainer, and a release run asked for one of them with
	// nothing dropped for it has no ffmpeg to embed. That is not a failure, and the run that was asked
	// for the whole matrix ships the archives whose ffmpeg it can produce.
	ffmpeg func(options, io.Writer) (bool, error)

	// checkFFmpeg proves the pair the maintainer dropped for one target, on the platform that target
	// runs on, and records what it saw. It is not an external command and it builds nothing.
	checkFFmpeg func(options, io.Writer) error
}

// run resolves the version, says what it resolved and why, and then runs the steps that build it.
func (t tool) run(args []string, stdout io.Writer) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stdout, "error  %v\n\nusage: go run ./cmd/build [client|scanner|admin|ffmpeg [goos-goarch]|ffmpeg-check goos-goarch|server|dist [name-arch]|container|clean|version] [--pre <name>] [-o <path>]\n", err)
		return 2
	}

	src, err := t.read(t.repo)
	if err != nil {
		return fail(stdout, err)
	}

	version, err := buildinfo.Derive(src.core, src.tag, src.commit, o.pre)
	if err != nil {
		return fail(stdout, err)
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

	// A clean run removes what a release and a client build leave behind, and builds nothing. It needs
	// no npm, no ffmpeg, and no compiler, so it ends the run where it started.
	if o.sub == subClean {
		removed, err := t.clean(t.repo)
		if err != nil {
			return fail(stdout, fmt.Errorf("%s: %w", subClean, err))
		}

		for _, path := range removed {
			fmt.Fprintf(stdout, "%-7s %s\n", subClean, path)
		}

		return 0
	}

	// The ffmpeg check proves a pair this build cannot run, on the platform that pair runs on, and
	// writes down what it saw. It builds nothing: the machine it runs on is a Mac or a FreeBSD box, and
	// the programs it proves already exist.
	if o.sub == subFFmpegCheck {
		if err := t.checkFFmpeg(o, stdout); err != nil {
			return fail(stdout, fmt.Errorf("%s: %w", subFFmpegCheck, err))
		}

		return 0
	}

	state, err := t.state(t.repo, o)
	if err != nil {
		return fail(stdout, err)
	}

	// The web applications come first: the server embeds both bundles, so a bundle built in this
	// run has to be on disk before the binary that embeds it is compiled.
	if err := t.runWebapps(o, version, stdout, state); err != nil {
		return fail(stdout, err)
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
			return fail(stdout, fmt.Errorf("%s: %w", subFFmpeg, err))
		}

		// The pair this target carries is compiled on its own platform and was not dropped for it, and
		// the message the ffmpeg phase printed says where to drop it and what proves it. A server
		// compiled now embeds no ffmpeg and converts with whatever the host happens to have, which is
		// not what this target releases, so the run ends here, successfully, and compiles no binary.
		if skipped {
			return 0
		}
	}

	// The container image compiles its own server, so the run that prepares it ends with podman: the
	// applications and the ffmpeg the image's architecture embeds are what the Containerfile needs on
	// disk, and a server binary built here is one the image throws away.
	if o.sub == subContainer {
		s := containerStep(o, t.repo, version)

		if err := t.execute(s); err != nil {
			return fail(stdout, fmt.Errorf("%s: %w", s.label, err))
		}

		fmt.Fprintf(stdout, "%-7s %s\n", s.label, s.result)

		return 0
	}

	if o.buildsServer() {
		if err := t.runServer(o, version, stdout); err != nil {
			return fail(stdout, fmt.Errorf("%s: %w", subServer, err))
		}
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
// A target whose pair is compiled on its own platform and was not dropped for it is not a failure, and
// does not stop the targets after it: that pair is built by hand on a machine the release machine does
// not control, and a run that stopped for it would ship nothing at all.
func (t tool) runDist(o options, version buildinfo.Version, stdout io.Writer) int {
	// The list is decided once, before any of it runs, so that a run asked for the machine it is
	// standing on is refused before a single application is built, and not after the first archive.
	targets, err := distTargets(o, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return fail(stdout, err)
	}

	for _, tr := range targets {
		// The target each iteration is building for is written into the options the server step and the
		// archive path are read from, because each target's binary is a different file.
		o.target = tr.targetWord()

		// The matrix writes one binary per target, so the default path is decided again for each one. A
		// path the operator named is not overwritten.
		if !o.outputNamed {
			o.output = defaultOutputPath(o.target)
		}

		skipped, err := t.ffmpeg(o, stdout)
		if err != nil {
			return fail(stdout, fmt.Errorf("%s for %s: %w", subFFmpeg, o.target, err))
		}

		// The message the ffmpeg phase printed for this target says where to drop the pair, what proves
		// it, and what pins it. The run carries on to the targets whose ffmpeg it can produce, and ends
		// successfully with the archives it wrote.
		if skipped {
			continue
		}

		// The binary for this target is current when it is newer than every file it is built from, and
		// the bundles the client steps built are among them. The check is made for each target, because
		// each target's binary is a different file.
		if err := t.runServer(o, version, stdout); err != nil {
			return fail(stdout, fmt.Errorf("%s for %s: %w", subServer, o.target, err))
		}

		// The archive carries the binary, the documentation as the files it is written in, the pictures
		// those files point at, and the ffmpeg licence when the target carries an ffmpeg.
		files, err := t.releaseFiles(t.repo, tr)
		if err != nil {
			return fail(stdout, fmt.Errorf("%s for %s: %w", subArchive, o.target, err))
		}

		path := archivePath(o.target, version.String())

		if err := t.archive(path, files); err != nil {
			return fail(stdout, fmt.Errorf("%s for %s: %w", subArchive, o.target, err))
		}

		fmt.Fprintf(stdout, "%-7s %s\n", subArchive, path)
	}

	return 0
}

// fail says what stopped the build and returns the exit code that says a step did not do its job.
func fail(stdout io.Writer, err error) int {
	fmt.Fprintf(stdout, "error  %v\n", err)

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

// cleanTree removes what a clean run removes: the release output, the client dependencies, and the
// contents of both bundles. The `.gitkeep` file in each bundle is left standing, because it is the
// placeholder that keeps a plain go build compiling: the bundle directory has to exist for the Go
// package that embeds it, and the contents are what a build puts there.
//
// The run names what it removed, so a tree that was already clean says so by naming nothing.
func cleanTree(repo string) ([]string, error) {
	targets := []string{filepath.Join(repo, "dist"), filepath.Join(repo, presentationDir, "node_modules")}

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

	return removed, nil
}

// serverSources is every file the server binary is built from: the Go files, and the contents of both
// bundles, which the server embeds. A binary that is newer than every Go file and older than the bundle
// it embeds is a binary that ships the bundle it was built before.
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
