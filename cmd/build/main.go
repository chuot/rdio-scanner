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

// Command build compiles this project: the listener application, then the administration
// application, then the server that carries both. It resolves the version the build carries from
// VERSION.txt and from git, and injects it into the binary through the linker and into the bundles
// through the environment, so that no source file has to be edited to say what version is being
// built.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"rdio-scanner/internal/buildinfo"
)

func main() {
	repo, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(os.Stdout, "error  %v\n", err)
		os.Exit(1)
	}

	t := tool{
		repo:         repo,
		read:         readSource,
		resolve:      resolveExecutable,
		installed:    directoryExists,
		execute:      runStep,
		write:        writeFile,
		archive:      writeArchive,
		clean:        cleanTree,
		document:     renderDocument,
		releaseFiles: archiveFiles,
	}

	t.ffmpeg = t.runFFmpeg

	os.Exit(t.run(os.Args[1:], os.Stdout))
}

// tool is the build tool: where the repository is, how it asks that repository what it says about
// its own version, and how it runs one step. Everything it decides is decided before anything runs.
type tool struct {
	repo      string
	read      func(string) (source, error)
	resolve   func(string) (string, error)
	installed func(string) bool
	execute   func(step) error

	// write puts a file the release produces into the tree, and archive writes the release archive.
	// Neither is an external command.
	write   func(path string, body []byte) error
	archive func(path string, files []archiveFile) error

	// clean removes what a clean run removes. It is not an external command.
	clean func(repo string) ([]string, error)

	// document renders the archive document for a target, and releaseFiles reads what the archive
	// carries. Both read the tree, so the stand-in answers them: what they do is tested in docs_test.go
	// and archive_test.go.
	document     func(repo string, tr releaseTarget, version string, date string) (string, error)
	releaseFiles func(repo string, tr releaseTarget) ([]archiveFile, error)

	// ffmpeg produces the ffmpeg a server binary built here is going to carry. It is not one external
	// command: it fetches a pinned release, checks it, compiles it in a container, runs what it produced
	// through the converter's own argument list, and puts it where the server embeds it.
	ffmpeg func(options, io.Writer) error
}

// run resolves the version, says what it resolved and why, and then runs the steps that build it.
func (t tool) run(args []string, stdout io.Writer) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stdout, "error  %v\n\nusage: go run ./cmd/build [client|scanner|admin|ffmpeg [goos-goarch]|server|dist [name-arch]|container|clean|version] [--pre <name>] [-o <path>]\n", err)
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

	npmName := ""

	if o.buildsClients() {
		npmName, err = t.resolve("npm")
		if err != nil {
			return fail(stdout, fmt.Errorf("building the applications needs npm: %w", err))
		}

		o.installClients = !t.installed(filepath.Join(t.repo, presentationDir, "node_modules"))
	}

	for _, s := range plan(o, t.repo, version, npmName) {
		if err := t.execute(s); err != nil {
			return fail(stdout, fmt.Errorf("%s: %w", s.label, err))
		}

		fmt.Fprintf(stdout, "%-7s %s\n", s.label, s.result)
	}

	// The release matrix builds the applications once, because the bundles are the same for every
	// target, and then each target's ffmpeg before the server that embeds it.
	if o.sub == subDist {
		return t.runDist(o, version, src.date, stdout)
	}

	// The ffmpeg is produced before the server is compiled, because the server embeds it: a server built
	// before the ffmpeg its target carries exists is a server that quietly ships without one.
	if o.buildsFFmpeg() {
		if err := t.ffmpeg(o, stdout); err != nil {
			return fail(stdout, fmt.Errorf("%s: %w", subFFmpeg, err))
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
		s := serverStep(o, t.repo, version)

		if err := t.execute(s); err != nil {
			return fail(stdout, fmt.Errorf("%s: %w", s.label, err))
		}

		fmt.Fprintf(stdout, "%-7s %s\n", s.label, s.result)
	}

	return 0
}

// runDist builds the release for the targets the run was asked for: the whole matrix, or the one the
// operator named. Each target's ffmpeg is produced before the server that embeds it, for the same
// reason a single run produces its ffmpeg first.
//
// A target whose ffmpeg fails ends the run and names the target it stopped at. A half matrix is not a
// release, and a run that carried on would leave an archive directory with a server in it and no ffmpeg
// to embed.
func (t tool) runDist(o options, version buildinfo.Version, date string, stdout io.Writer) int {
	// The target the operator named is kept apart from the target each iteration is building for. A
	// loop that wrote the target it is building into the target the run was asked for would skip every
	// target after the first.
	requested := o.target

	for _, tr := range releaseTargets {
		if requested != "" && requested != tr.targetWord() {
			continue
		}

		o.target = tr.targetWord()

		// The matrix writes one binary per target, so the default path is decided again for each one. A
		// path the operator named is not overwritten.
		if !o.outputNamed {
			o.output = defaultOutputPath(o.target)
		}

		if err := t.ffmpeg(o, stdout); err != nil {
			return fail(stdout, fmt.Errorf("%s for %s: %w", subFFmpeg, o.target, err))
		}

		s := serverStep(o, t.repo, version)

		if err := t.execute(s); err != nil {
			return fail(stdout, fmt.Errorf("%s for %s: %w", s.label, o.target, err))
		}

		fmt.Fprintf(stdout, "%-7s %s\n", s.label, s.result)

		// The document is rendered from the target's own sources, with the version the release resolved
		// and the date of the commit being built.
		html, err := t.document(t.repo, tr, version.String(), date)
		if err != nil {
			return fail(stdout, fmt.Errorf("%s for %s: %w", subDocument, o.target, err))
		}

		if err := t.write(documentPath(o.target), []byte(html)); err != nil {
			return fail(stdout, fmt.Errorf("%s for %s: %w", subDocument, o.target, err))
		}

		fmt.Fprintf(stdout, "%-7s %s\n", subDocument, documentPath(o.target))

		// The archive carries the binary, the document, and the ffmpeg licence when the target carries
		// an ffmpeg.
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

// explain says where the resolved version came from, because a version nobody can explain is the
// drift this tool replaced with a file.
func explain(src source, pre string) string {
	parts := []string{}

	if src.declared {
		parts = append(parts, "VERSION.txt "+src.core)
	} else {
		parts = append(parts, "VERSION.txt is missing, assuming "+src.core)
	}

	if src.tag != "" {
		parts = append(parts, "HEAD tagged "+src.tag)
	} else {
		parts = append(parts, "HEAD "+src.commit+" is not tagged")
	}

	if pre != "" {
		parts = append(parts, "prerelease requested by --pre")
	}

	return strings.Join(parts, ", ")
}

// fail says what stopped the build and returns the exit code that says a step did not do its job.
func fail(stdout io.Writer, err error) int {
	fmt.Fprintf(stdout, "error  %v\n", err)

	return 1
}
