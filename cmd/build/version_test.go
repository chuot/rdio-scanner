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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"rdio-scanner/cmd/build/buildinfo"
)

// the changed paths are the paths git says are not committed, so the
// operator sees the work to commit.
func TestTheChangedPathsAreThePathsGitSaysAreNotCommitted(t *testing.T) {
	status := strings.TrimSpace(strings.Join([]string{
		" M cmd/build/build.go",
		"?? cmd/build/a_file_nobody_committed.go",
		"M  cmd/build/a_staged_file.go",
		"R  cmd/build/the_old_name.go -> cmd/build/the_new_name.go",
	}, "\n"))

	got := changedPaths(status)

	want := []string{
		"cmd/build/build.go",
		"cmd/build/a_file_nobody_committed.go",
		"cmd/build/a_staged_file.go",
		"cmd/build/the_new_name.go",
	}

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the changed paths are\n  %v\nwant\n  %v", got, want)
	}
}

// readSource is the only part of the tool that talks to git, so these
// tests ask it in the two places the answers matter: this repository,
// which is what gets built, and a scratch repository made to be the
// states this one is not.

// scratchRepository makes a repository in a temporary directory, with
// one empty commit and, when a tag name is given, that tag naming it.
// The tag is written as a ref file rather than with the git tag command.
func scratchRepository(t *testing.T, tag string) string {
	t.Helper()

	dir := t.TempDir()

	// The identity and the signing settings of whoever runs the test are none of a scratch
	// repository's business.
	for _, args := range [][]string{
		{"init", "-q", "--initial-branch", "main"},
		{"commit", "-q", "--allow-empty", "-m", "one commit"},
	} {
		out := scratchGit(t, dir, args...)
		if out != "" {
			t.Fatalf("git %s in the scratch repository printed %q", strings.Join(args, " "), out)
		}
	}

	if tag != "" {
		sha := scratchGit(t, dir, "rev-parse", "HEAD")

		path := filepath.Join(dir, ".git", "refs", "tags", tag)

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("making the tag directory: %v", err)
		}

		if err := os.WriteFile(path, []byte(sha+"\n"), 0o644); err != nil {
			t.Fatalf("writing the tag %s: %v", tag, err)
		}
	}

	return dir
}

// scratchGit runs one git command inside a scratch repository with an identity of its own.
func scratchGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", append([]string{
		"-c", "user.name=Build Test",
		"-c", "user.email=build-test@invalid",
		"-c", "commit.gpgsign=false",
		"-c", "init.defaultBranch=main",
	}, args...)...)

	cmd.Dir = dir
	cmd.Env = []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "LC_ALL=C"}

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}

	return strings.TrimSpace(string(out))
}

func writeScratchVersionFile(t *testing.T, dir, contents string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, "VERSION.txt"), []byte(contents), 0o644); err != nil {
		t.Fatalf("writing the scratch VERSION.txt: %v", err)
	}
}

// readSource reads this repository's own state: a declared core and
// a commit no tag names, the state every developer's clone is in.
func TestReadSourceReadsThisRepositorysOwnState(t *testing.T) {
	repo, err := repositoryRoot()
	if err != nil {
		t.Fatalf("finding the repository to build: %v", err)
	}

	src, err := readSource(repo)
	if err != nil {
		t.Fatalf("reading the version this repository declares: %v", err)
	}

	if !src.declared {
		t.Error("VERSION.txt at the repository root is not reported as declared")
	}

	if src.core != "8.0.0" {
		t.Errorf("core = %q, want the 8.0.0 the repository declares", src.core)
	}

	if !regexp.MustCompile(`^[0-9a-f]{7,40}$`).MatchString(src.commit) {
		t.Errorf("commit = %q, which is not a commit hash", src.commit)
	}

	// HEAD is not tagged on this branch, and a tag here would have to be one git names exactly.
	want, _ := git(repo, "describe", "--tags", "--exact-match")
	if src.tag != want {
		t.Errorf("tag = %q, want what git says names HEAD exactly: %q", src.tag, want)
	}

	// A suite running alongside created a directory the tool saw and that was
	// gone again; dirtiness is proven in the scratch repository beside it.
}

func TestReadSourceReportsTheTagThatNamesTheCommit(t *testing.T) {
	dir := scratchRepository(t, "v8.0.0")
	writeScratchVersionFile(t, dir, "8.0.0\n")

	src, err := readSource(dir)
	if err != nil {
		t.Fatalf("reading a scratch repository that is tagged: %v", err)
	}

	if src.tag != "v8.0.0" {
		t.Errorf("tag = %q, want v8.0.0, the tag that names HEAD", src.tag)
	}

	version, err := buildinfo.Derive(src.core, src.tag, src.commit, "")
	if err != nil {
		t.Fatalf("deriving the version of a tagged scratch repository: %v", err)
	}

	// A tag says which release this is, so the commit that happens to be it is not part of the
	// version a client reads.
	if version.String() != "8.0.0" {
		t.Errorf("version = %q, want 8.0.0 with no commit in it", version.String())
	}
}

// readSource reports no tag when the tag names another commit: a tag
// elsewhere must not suppress this build's commit hash.
func TestReadSourceReportsNoTagWhenTheTagNamesAnotherCommit(t *testing.T) {
	dir := scratchRepository(t, "v7.9.0")
	writeScratchVersionFile(t, dir, "8.0.0\n")

	scratchGit(t, dir, "commit", "-q", "--allow-empty", "-m", "a second commit")

	src, err := readSource(dir)
	if err != nil {
		t.Fatalf("reading a scratch repository whose tag names an older commit: %v", err)
	}

	if src.tag != "" {
		t.Errorf("tag = %q, want no tag: v7.9.0 names a commit this is not", src.tag)
	}

	version, err := buildinfo.Derive(src.core, src.tag, src.commit, "")
	if err != nil {
		t.Fatalf("deriving the version of an untagged scratch repository: %v", err)
	}

	if version.String() != "8.0.0+g"+src.commit {
		t.Errorf("version = %q, want 8.0.0+g%s", version.String(), src.commit)
	}
}

// readSource stops the build when a real tag disagrees with the
// declared core: two sources disagreeing about one commit fails loudly.
func TestARealTagThatDisagreesWithTheDeclaredCoreStopsTheBuild(t *testing.T) {
	dir := scratchRepository(t, "v7.9.0")
	writeScratchVersionFile(t, dir, "8.0.0\n")

	src, err := readSource(dir)
	if err != nil {
		t.Fatalf("reading a scratch repository that declares 8.0.0 and is tagged v7.9.0: %v", err)
	}

	if src.tag != "v7.9.0" {
		t.Fatalf("tag = %q, want v7.9.0, so this test is testing what it says it tests", src.tag)
	}

	_, err = buildinfo.Derive(src.core, src.tag, src.commit, "")
	if err == nil {
		t.Fatal("VERSION.txt saying 8.0.0 on a commit tagged v7.9.0 was accepted, and must stop the build")
	}

	for _, want := range []string{"8.0.0", "v7.9.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not name %q", err, want)
		}
	}
}

// readSource reports a dirty working tree without letting it into
// the version: the version describes a commit, not uncommitted work.
func TestReadSourceReportsADirtyWorkingTreeWithoutLettingItIntoTheVersion(t *testing.T) {
	dir := scratchRepository(t, "")
	writeScratchVersionFile(t, dir, "8.0.0\n")

	if err := os.WriteFile(filepath.Join(dir, "uncommitted.txt"), []byte("not committed\n"), 0o644); err != nil {
		t.Fatalf("making the working tree dirty: %v", err)
	}

	src, err := readSource(dir)
	if err != nil {
		t.Fatalf("reading a scratch repository with uncommitted changes: %v", err)
	}

	if !src.dirty {
		t.Error("an uncommitted file is not reported as a dirty working tree")
	}

	version, err := buildinfo.Derive(src.core, src.tag, src.commit, "")
	if err != nil {
		t.Fatalf("deriving the version of a dirty scratch repository: %v", err)
	}

	if strings.Contains(version.String(), "dirty") {
		t.Errorf("version = %q, which carries the working tree state it must not carry", version.String())
	}

	if version.String() != "8.0.0+g"+src.commit {
		t.Errorf("version = %q, want 8.0.0+g%s: the same version a clean tree of the same commit gets", version.String(), src.commit)
	}
}

// readSource reports an undeclared version as assumed: a repository
// with no VERSION.txt has not said what version it is.
func TestReadSourceReportsAVersionTheRepositoryNeverDeclaredAsAssumed(t *testing.T) {
	dir := scratchRepository(t, "")

	src, err := readSource(dir)
	if err != nil {
		t.Fatalf("reading a scratch repository with no VERSION.txt: %v", err)
	}

	if src.declared {
		t.Error("a scratch repository with no VERSION.txt is reported as declaring one")
	}

	if src.core != "0.0.0" {
		t.Errorf("core = %q, want the assumed 0.0.0", src.core)
	}
}

// scratchWithoutARepository is a scratch directory no repository
// covers, which is what the tests below need git to refuse.
func scratchWithoutARepository(t *testing.T) string {
	t.Helper()

	below, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("the scratch directory cannot be resolved: %v", err)
	}

	dir := filepath.Join(below, "tree")

	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("making the scratch directory: %v", err)
	}

	t.Setenv("GIT_CEILING_DIRECTORIES", below)

	return dir
}

// readSource refuses a directory that is not a repository: saying so
// beats building something whose version is a guess.
func TestReadSourceRefusesADirectoryThatIsNotARepository(t *testing.T) {
	dir := scratchWithoutARepository(t)
	writeScratchVersionFile(t, dir, "8.0.0\n")

	if _, err := readSource(dir); err == nil {
		t.Fatal("a directory that is not a repository was accepted, and reading its commit has to fail")
	}
}

// readSource refuses an unreadable VERSION.txt before asking git
// anything, the order the contract states.
func TestReadSourceRefusesAVersionFileItCannotReadBeforeAskingGit(t *testing.T) {
	dir := scratchRepository(t, "")
	writeScratchVersionFile(t, dir, "v8.0.0\n")

	_, err := readSource(dir)
	if err == nil {
		t.Fatal("VERSION.txt holding v8.0.0 was accepted, and must be refused")
	}

	if !strings.Contains(err.Error(), "VERSION.txt") {
		t.Errorf("the refusal %q does not name the file at fault", err)
	}
}

// a tree git cannot answer for names git, the directory, and the
// command, because git's own complaint is the part that says what
// to do and exit status 128 alone does not.
func TestATreeGitCannotAnswerForNamesGitTheDirectoryAndTheCommand(t *testing.T) {
	dir := scratchWithoutARepository(t)

	_, err := git(dir, "rev-parse", "--show-toplevel")
	if err == nil {
		t.Fatal("a tree git cannot answer for was accepted, and the run has nothing to tell the operator")
	}

	for _, want := range []string{"git", "rev-parse --show-toplevel", dir, "not a git repository"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message %q does not name %s", err, want)
		}
	}
}
