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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"rdio-scanner/cmd/build/buildinfo"
)

// these tests refuse the command lines the tool cannot honour and
// read the server and container steps it composes; the webapps plan
// is tested in webapps_test.go, and nothing here runs a build.

// theMoment is the moment a plan is composed at: the clock a run
// reports is a fact of the machine, so a test must fix what it said.
var theMoment = time.Date(2026, 10, 13, 12, 30, 45, 0, time.UTC)

// theVersion is the version a plan is composed for: an untagged
// build of the declared core, the shape a development build has.
func theVersion(t *testing.T) buildinfo.Version {
	t.Helper()

	version, err := buildinfo.Derive("8.0.0", "", "db79311", "")
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}

	return version
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}

	return false
}

// the server step carries the resolved version in its linker flags.
func TestTheServerBuildCarriesTheResolvedVersionInTheLinkerFlags(t *testing.T) {
	o, err := parseArgs([]string{"server"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	s := serverStep(o, "/repo", theVersion(t))

	want := "go build -ldflags -s -w -X main.Version=8.0.0+gdb79311 -o " + defaultOutputPath("") + " ./cmd/rdio-scanner"
	got := strings.Join(append([]string{s.name}, s.args...), " ")
	if got != want {
		t.Errorf("the server is built with\n  %s\nwant\n  %s", got, want)
	}

	if s.dir != "/repo" {
		t.Errorf("the server is built in %q, want the repository root", s.dir)
	}
}

// the container step tags the image with the resolved version and latest.
func TestTheContainerStepTagsTheImageWithTheVersionTheRunResolved(t *testing.T) {
	s := containerStep(options{}, "/repo", theVersion(t), theMoment, podmanProgram)

	want := []string{
		"build",
		"--pull",
		"-f", "Containerfile",
		"--build-arg", "VERSION=8.0.0+gdb79311",
		"--build-arg", "BUILD_TIME=2026-10-13T12:30:45Z",
		"-t", "rdio-scanner:8.0.0-gdb79311",
		"-t", "rdio-scanner:latest",
		".",
	}

	if strings.Join(s.args, " ") != strings.Join(want, " ") {
		t.Errorf("podman is asked to run\n  %v\nwant\n  %v", s.args, want)
	}

	if s.result != "rdio-scanner:8.0.0-gdb79311" {
		t.Errorf("the run reports the image as %q, want the image named for the version the run resolved", s.result)
	}
}

// the image tag is the version spelled the way an image tag's grammar allows.
func TestTheImageTagIsTheVersionSpelledTheWayAnImageTagAllows(t *testing.T) {
	for _, version := range []string{"8.0.0", "8.0.0+g1885b7e2", "8.0.0-rc1+g1885b7e2"} {
		tag := imageTagFor(version)

		if strings.Contains(tag, "+") {
			t.Errorf("the tag for %s is %q, and an image tag carries no +", version, tag)
		}

		if !theImageTagGrammar.MatchString(tag) {
			t.Errorf("the tag for %s is %q, which is not a tag a registry accepts", version, tag)
		}

		// the tag says the same release as the version, and rewrites only
		// the sign introducing the git metadata as a dash.
		if tag != strings.Replace(version, "+", "-", 1) {
			t.Errorf("the tag for %s is %q, want the version with the sign that introduces the git metadata written as a dash", version, tag)
		}
	}
}

// theImageTagGrammar is the tag grammar a registry accepts.
var theImageTagGrammar = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// the Containerfile names its base image by version and carries the image labels.
func TestTheContainerfileNamesItsBaseImageByVersionAndCarriesTheImageLabels(t *testing.T) {
	text, err := os.ReadFile(filepath.Join("..", "..", "Containerfile"))
	if err != nil {
		t.Fatalf("reading the Containerfile: %v", err)
	}

	contents := string(text)

	from := ""

	for _, line := range strings.Split(contents, "\n") {
		if strings.HasPrefix(line, "FROM ") {
			from = line

			break
		}
	}

	if from == "" {
		t.Fatal("the Containerfile has no FROM line")
	}

	if strings.Contains(from, ":latest") {
		t.Errorf("the base image is named %q, and `latest` is a moving version: the image built today is not the image built last week", from)
	}

	base, found := strings.CutPrefix(from, "FROM docker.io/")
	if !found {
		t.Fatalf("the base image is %q, want it named on the registry it is pulled from", from)
	}

	name, version, found := strings.Cut(base, ":")
	if !found || name != "alpine" || version == "" {
		t.Fatalf("the base image is %q, want alpine named by version", from)
	}

	for _, part := range strings.Split(version, ".") {
		if _, err := strconv.Atoi(part); err != nil {
			t.Fatalf("the base image is named %q, which is not a release number", version)
		}
	}

	// the labels the image carries and the build arguments they arrive
	// through: a label repeating a literal goes stale with the release.
	for _, label := range []string{
		"org.opencontainers.image.title=",
		"org.opencontainers.image.source=",
		"org.opencontainers.image.version=",
		"org.opencontainers.image.created=",
		"org.opencontainers.image.documentation=",
		"org.opencontainers.image.ref.name=",
	} {
		if !strings.Contains(contents, "LABEL "+label) {
			t.Errorf("the Containerfile carries no %s label", strings.TrimSuffix(label, "="))
		}
	}

	for _, label := range [][2]string{
		{"org.opencontainers.image.version=", "VERSION"},
		{"org.opencontainers.image.created=", "BUILD_TIME"},
		{"org.opencontainers.image.ref.name=", "VERSION"},
	} {
		if !strings.Contains(contents, "ARG "+label[1]+"=") {
			t.Errorf("the Containerfile declares no %s build argument, so its %s label cannot carry what the run knows",
				label[1], strings.TrimSuffix(label[0], "="))
		}

		if !strings.Contains(contents, "LABEL "+label[0]+`"`+`$`+label[1]+`"`) {
			t.Errorf("the Containerfile's %s label does not read the %s build argument", strings.TrimSuffix(label[0], "="), label[1])
		}
	}
}

// The server binary goes where the operator said, and where the release matrix expects when they
// did not.
func TestTheOutputPathIsTheOneTheOperatorNamed(t *testing.T) {
	o, err := parseArgs([]string{"server", "-o", "./rdio-scanner"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if !contains(serverStep(o, "/repo", theVersion(t)).args, "./rdio-scanner") {
		t.Errorf("the server is built with %v, which does not write to the named path", serverStep(o, "/repo", theVersion(t)).args)
	}

	o, err = parseArgs([]string{"server"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	want := filepath.Join("dist", runtime.GOOS+"-"+runtime.GOARCH, "rdio-scanner")
	if o.output != want {
		t.Errorf("the server is built to %q, want the per-platform directory %q", o.output, want)
	}

	if serverStep(o, "/repo", theVersion(t)).result != want {
		t.Errorf("the server step says it leaves behind %q, want the path it writes to", serverStep(o, "/repo", theVersion(t)).result)
	}
}

// -o is accepted only by the runs that compile one server binary.
func TestTheOutputPathIsAcceptedOnlyByTheRunsThatCompileAServerBinary(t *testing.T) {
	for _, args := range [][]string{
		{"-o", "./built"},
		{"server", "-o", "./built"},
		{"-o", "./built", "server"},
	} {
		o, err := parseArgs(args)
		if err != nil {
			t.Errorf("parseArgs(%v): %v, want the named path accepted", args, err)
			continue
		}

		if o.output != "./built" {
			t.Errorf("parseArgs(%v) writes to %q, want ./built", args, o.output)
		}
	}

	for _, args := range [][]string{
		{"client", "-o", "./built"},
		{"-o", "./built", "client"},
		{"scanner", "-o", "./built"},
		{"admin", "-o", "./built"},
		{"ffmpeg", "-o", "./built"},
		{"ffmpeg", "linux-arm64", "-o", "./built"},
		{"dist", "-o", "./built"},
		{"-o", "./built", "dist"},
		{"dist", "--all", "-o", "./built"},
		{"container", "-o", "./built"},
		{"clean", "-o", "./built"},
		{"version", "-o", "./built"},
		{"test", "-o", "./built"},
	} {
		_, err := parseArgs(args)
		if err == nil {
			t.Errorf("%v was accepted, want a refusal naming the runs that write a server binary", args)
			continue
		}

		if !strings.Contains(err.Error(), "server") {
			t.Errorf("the refusal of %v is %q, which does not name the runs that write a server binary", args, err)
		}
	}
}

// the test run composes this project's two suites and builds nothing.
func TestTheTestRunComposesTheTwoSuitesOfThisProject(t *testing.T) {
	o, err := parseArgs([]string{"test"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	steps := testSteps(o, "/repo")

	if len(steps) != 2 {
		t.Fatalf("the test run composes %d steps, want the Go suite and the client suite", len(steps))
	}

	goStep := steps[0]

	if goStep.name != "go" || !contains(goStep.args, "test") || !contains(goStep.args, "./...") {
		t.Errorf("the Go suite is run as %s %v, want go test ./...", goStep.name, goStep.args)
	}

	if goStep.dir != "/repo" {
		t.Errorf("the Go suite runs in %q, want the repository root", goStep.dir)
	}

	npmStep := steps[1]

	if npmStep.name != "npm" || !contains(npmStep.args, "test") {
		t.Errorf("the client suite is run as %s %v, want npm test", npmStep.name, npmStep.args)
	}

	if npmStep.dir != filepath.Join("/repo", presentationDir) {
		t.Errorf("the client suite runs in %q, want %s, where the client suites are", npmStep.dir, presentationDir)
	}
}

// the test run refuses the flags that would decide what a build
// builds, whichever side of the flag the command arrived on.
func TestTheTestRunRefusesWhatItCannotRun(t *testing.T) {
	for _, args := range [][]string{
		{"test", "linux-amd64"},
		{"test", "-o", "./built"},
		{"-o", "./built", "test"},
		{"test", "--all"},
		{"--all", "test"},
		{"test", "--pre", "beta1"},
		{"--pre", "beta1", "test"},
		{"test", "--allow-dirty"},
		{"--allow-dirty", "test"},
	} {
		_, err := parseArgs(args)
		if err == nil {
			t.Errorf("%v was accepted, want a refusal naming the test run", args)
			continue
		}

		if !strings.Contains(err.Error(), "test") {
			t.Errorf("the refusal of %v is %q, which does not name the test run", args, err)
		}
	}
}

// --allow-dirty is an instruction about a release, so only the release run accepts it.
func TestTheAllowDirtyFlagIsAcceptedOnlyByTheReleaseRun(t *testing.T) {
	for _, args := range [][]string{
		{"dist", "--allow-dirty"},
		{"--allow-dirty", "dist"},
		{"dist", "--all", "--allow-dirty"},
	} {
		o, err := parseArgs(args)
		if err != nil {
			t.Errorf("parseArgs(%v): %v, want the release run to accept the override", args, err)
			continue
		}

		if !o.allowDirty {
			t.Errorf("parseArgs(%v) does not record the override", args)
		}
	}

	for _, args := range [][]string{
		{"--allow-dirty"},
		{"client", "--allow-dirty"},
		{"--allow-dirty", "client"},
		{"scanner", "--allow-dirty"},
		{"server", "--allow-dirty"},
		{"ffmpeg", "--allow-dirty"},
		{"container", "--allow-dirty"},
		{"clean", "--allow-dirty"},
		{"version", "--allow-dirty"},
	} {
		_, err := parseArgs(args)
		if err == nil {
			t.Errorf("%v was accepted, want a refusal naming the release run", args)
			continue
		}

		if !strings.Contains(err.Error(), "dist") {
			t.Errorf("the refusal of %v is %q, which does not name the release run", args, err)
		}
	}
}

// the shipped server binary is compiled static and stripped so it
// runs on machines the release machine has never seen.
func TestTheServerBinaryIsCompiledStaticAndStripped(t *testing.T) {
	o, err := parseArgs([]string{"server"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	s := serverStep(o, "/repo", theVersion(t))

	if !contains(s.env, "CGO_ENABLED=0") {
		t.Errorf("the server is built with env %v, which does not turn cgo off", s.env)
	}

	flags := ""

	for i, arg := range s.args {
		if arg == "-ldflags" && i+1 < len(s.args) {
			flags = s.args[i+1]
		}
	}

	for _, want := range []string{"-s", "-w", "-X main.Version=8.0.0+gdb79311"} {
		if !strings.Contains(flags, want) {
			t.Errorf("the server is linked with %q, which does not carry %s", flags, want)
		}
	}
}

// a prerelease name that does not fit the grammar is rejected at the command line.
func TestAPreReleaseNameThatIsNotAPrereleaseIsRejected(t *testing.T) {
	_, err := parseArgs([]string{"--pre", "beta 1"})
	if err == nil {
		t.Fatal("a prerelease name with a space in it was accepted")
	}

	if !strings.Contains(err.Error(), "beta 1") {
		t.Errorf("the refusal %q does not name the prerelease it rejected", err.Error())
	}

	o, err := parseArgs([]string{"--pre", "beta1"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if o.pre != "beta1" {
		t.Errorf("the prerelease is %q, want beta1", o.pre)
	}
}

func TestACommandTheToolDoesNotKnowIsRejected(t *testing.T) {
	for _, args := range [][]string{
		{"--not_a_flag"},
		{"not_a_subcommand"},
		{"scanner", "admin"},
		{"-o"},
		{"--pre"},
	} {
		_, err := parseArgs(args)
		if err == nil {
			t.Errorf("%v was accepted, want a refusal", args)
		}
	}
}

// the server is built for the target the run is building for, not the host.
func TestTheServerIsBuiltForTheTargetTheRunIsBuildingFor(t *testing.T) {
	o := options{target: "linux-arm64", output: defaultOutputPath("linux-arm64")}

	s := serverStep(o, "/repo", theVersion(t))

	if !contains(s.env, "GOOS=linux") {
		t.Errorf("the server is built with env %v, which does not name the target's operating system", s.env)
	}

	if !contains(s.env, "GOARCH=arm64") {
		t.Errorf("the server is built with env %v, which does not name the target's architecture", s.env)
	}

	want := filepath.Join("dist", "linux-arm64", "rdio-scanner")
	if s.result != want {
		t.Errorf("the server for linux-arm64 is written to %q, want %q", s.result, want)
	}

	// a windows target's output carries the .exe suffix its operating system runs.
	o = options{target: "windows-amd64", output: defaultOutputPath("windows-amd64")}

	want = filepath.Join("dist", "windows-amd64", "rdio-scanner.exe")
	if serverStep(o, "/repo", theVersion(t)).result != want {
		t.Errorf("the server for windows-amd64 is written to %q, want %q", serverStep(o, "/repo", theVersion(t)).result, want)
	}

	// a run that named no target builds for the machine it is running on.
	o = options{output: defaultOutputPath("")}

	want = filepath.Join("dist", runtime.GOOS+"-"+runtime.GOARCH, "rdio-scanner")
	if serverStep(o, "/repo", theVersion(t)).result != want {
		t.Errorf("a run that named no target writes to %q, want %q", serverStep(o, "/repo", theVersion(t)).result, want)
	}
}

// an ffmpeg check is not a command: the build runs the binaries it compiled.
func TestTheFfmpegCheckIsNotACommandThisToolAccepts(t *testing.T) {
	for _, args := range [][]string{
		{"ffmpeg-check"},
		{"ffmpeg-check", "macos-arm64"},
		{"ffmpeg-check", "darwin-amd64"},
		{"ffmpeg-check", "freebsd-amd64"},
	} {
		if _, err := parseArgs(args); err == nil {
			t.Errorf("%v is a command line this tool honours, and it is not one", args)
		}
	}
}

// the usage line names the commands the tool has.
func TestTheUsageLineNamesTheCommandsTheToolHas(t *testing.T) {
	out := &bytes.Buffer{}
	errout := &bytes.Buffer{}

	tl := &tool{errout: errout}

	if code := tl.run([]string{"nonsense"}, out); code == 0 {
		t.Fatal("a run naming no command succeeded")
	}

	line := errout.String()

	for _, want := range []string{"nonsense", "client", "scanner", "admin", "ffmpeg", "server", "dist", "container", "clean", "version"} {
		if !strings.Contains(line, want) {
			t.Errorf("the usage line %q does not name %s", line, want)
		}
	}

	if strings.Contains(line, "ffmpeg-check") {
		t.Errorf("the usage line offers a command the tool does not have: %q", line)
	}

	if out.Len() != 0 {
		t.Errorf("a refused command line wrote %q to the writer the run was given, which carries the run's progress",
			out.String())
	}
}

// a target named on the command line is resolved to the release word the run builds for.
func TestTheTargetACommandNamesIsTheReleaseWordTheRunBuildsFor(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		target string
	}{
		{args: []string{"ffmpeg", "darwin-arm64"}, target: "macos-arm64"},
		{args: []string{"ffmpeg", "darwin-amd64"}, target: "macos-amd64"},
		{args: []string{"dist", "darwin-arm64"}, target: "macos-arm64"},
		{args: []string{"ffmpeg", "macos-arm64"}, target: "macos-arm64"},
		{args: []string{"ffmpeg", "linux-amd64"}, target: "linux-amd64"},
		{args: []string{"ffmpeg", "freebsd-amd64"}, target: "freebsd-amd64"},
	} {
		o, err := parseArgs(tc.args)
		if err != nil {
			t.Errorf("%v: the command was refused: %v", tc.args, err)
			continue
		}

		if o.target != tc.target {
			t.Errorf("%v names the target %q, want the release word %q", tc.args, o.target, tc.target)
		}
	}
}

// a release run builds its own platform unless --all asks for the whole matrix.
func TestDistBuildsThePlatformItIsRunOnUnlessTheWholeMatrixIsAskedFor(t *testing.T) {
	o, err := parseArgs([]string{"dist"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if o.all {
		t.Error("dist alone asks for the whole matrix, and the machine running it is one platform")
	}

	if o.target != "" {
		t.Errorf("dist alone names %s, and the machine running it decides the target", o.target)
	}

	o, err = parseArgs([]string{"dist", "--all"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if !o.all {
		t.Error("dist --all does not ask for the whole matrix")
	}

	o, err = parseArgs([]string{"dist", "linux-arm64"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if o.all {
		t.Error("dist linux-arm64 asks for the whole matrix, and one target was named")
	}

	if o.target != "linux-arm64" {
		t.Errorf("dist builds %s, want linux-arm64", o.target)
	}

	for _, args := range [][]string{
		{"dist", "--all", "linux-arm64"},
		{"dist", "linux-arm64", "--all"},
		{"--all"},
		{"ffmpeg", "--all"},
		{"server", "--all"},
		{"client", "--all"},
		{"container", "--all"},
		{"clean", "--all"},
		{"version", "--all"},
	} {
		if _, err := parseArgs(args); err == nil {
			t.Errorf("%v is a command line this tool honours, and it is not one", args)
		}
	}
}

// the ffmpeg target must be the word right after the ffmpeg command.
func TestTheFFmpegTargetIsTheWordRightAfterIt(t *testing.T) {
	o, err := parseArgs([]string{"ffmpeg", "linux-arm64"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if o.target != "linux-arm64" {
		t.Errorf("the ffmpeg is compiled for %q, want linux-arm64", o.target)
	}

	o, err = parseArgs([]string{"ffmpeg"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if o.target != "" {
		t.Errorf("the ffmpeg is compiled for %q, want the platform this tool runs on", o.target)
	}

	for _, word := range []string{"linux-arm64", "windows-386", "linux-arm"} {
		if !isTarget(word) {
			t.Errorf("%q is not a platform, and the tool refuses a build for it", word)
		}
	}

	for _, word := range []string{"linux", "-arm64", "linux-arm64-x", "Linux-arm64", "linux--arm64"} {
		if isTarget(word) {
			t.Errorf("%q is accepted as a platform, want it refused", word)
		}
	}
}

// the build resolves the container program the machine has, preferring podman.
func TestTheBuildSpeaksToTheContainerProgramTheMachineHas(t *testing.T) {
	// Both programs on the machine: the one the build prefers.
	program, err := resolveContainerProgram(aMachineWith(podmanProgram, dockerProgram), noEnvironment)
	if err != nil {
		t.Fatalf("resolveContainerProgram: %v", err)
	}

	if program != podmanProgram {
		t.Errorf("the machine has both programs and the build speaks to %q, want %q", program, podmanProgram)
	}

	// a machine with only docker can build container images.
	program, err = resolveContainerProgram(aMachineWith(dockerProgram), noEnvironment)
	if err != nil {
		t.Fatalf("resolveContainerProgram: %v", err)
	}

	if program != dockerProgram {
		t.Errorf("the machine has only docker and the build speaks to %q, want %q", program, dockerProgram)
	}

	// Only podman.
	program, err = resolveContainerProgram(aMachineWith(podmanProgram), noEnvironment)
	if err != nil {
		t.Fatalf("resolveContainerProgram: %v", err)
	}

	if program != podmanProgram {
		t.Errorf("the machine has only podman and the build speaks to %q, want %q", program, podmanProgram)
	}

	// Neither: refused, and the refusal names both programs the build looked for.
	program, err = resolveContainerProgram(aMachineWith(), noEnvironment)
	if err == nil {
		t.Fatal("a machine with no container program was accepted, and the build cannot compile an ffmpeg there")
	}

	for _, name := range []string{podmanProgram, dockerProgram} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the refusal %q does not name %s, which the build looked for", err, name)
		}
	}

	if program != "" {
		t.Errorf("the refusal still speaks to %q", program)
	}

	// the name the operator gives stands over the machine's preference.
	program, err = resolveContainerProgram(aMachineWith(podmanProgram, dockerProgram),
		anEnvironment(map[string]string{containerProgramEnv: dockerProgram}))
	if err != nil {
		t.Fatalf("resolveContainerProgram: %v", err)
	}

	if program != dockerProgram {
		t.Errorf("the operator named %s and the build speaks to %q", dockerProgram, program)
	}

	// The operator names a program the machine does not have: refused, naming the one they named.
	_, err = resolveContainerProgram(aMachineWith(dockerProgram),
		anEnvironment(map[string]string{containerProgramEnv: podmanProgram}))
	if err == nil {
		t.Fatal("the operator named a program this machine does not have, and the run was accepted")
	}

	if !strings.Contains(err.Error(), podmanProgram) {
		t.Errorf("the refusal %q does not name %s, which the operator named", err, podmanProgram)
	}

	// The operator names something that is not a container program.
	_, err = resolveContainerProgram(aMachineWith(podmanProgram),
		anEnvironment(map[string]string{containerProgramEnv: "container"}))
	if err == nil {
		t.Fatalf("the operator named %q, which is not a container program, and the run was accepted", "container")
	}

	if !strings.Contains(err.Error(), "container") {
		t.Errorf("the refusal %q does not name %q, which the operator wrote", err, "container")
	}

	// A variable set to nothing says nothing: the machine's own preference stands.
	program, err = resolveContainerProgram(aMachineWith(dockerProgram),
		anEnvironment(map[string]string{containerProgramEnv: ""}))
	if err != nil {
		t.Fatalf("resolveContainerProgram: %v", err)
	}

	if program != dockerProgram {
		t.Errorf("the variable was set to nothing and the build speaks to %q, want the machine's own program", program)
	}
}

// aMachineWith is the machine's PATH: the programs it has, and nothing else.
func aMachineWith(programs ...string) func(string) (string, error) {
	there := map[string]bool{}

	for _, name := range programs {
		there[name] = true
	}

	return func(name string) (string, error) {
		if there[name] {
			return "/usr/bin/" + name, nil
		}

		return "", fmt.Errorf("%s is not on this machine", name)
	}
}

// the container step names the resolved program and the Containerfile it builds from.
func TestTheReleaseImageBuildNamesTheProgramAndTheFileItBuildsFrom(t *testing.T) {
	for _, program := range []string{podmanProgram, dockerProgram} {
		s := containerStep(options{}, "/repo", theVersion(t), theMoment, program)

		if s.name != program {
			t.Errorf("the release image build names %q, want the program the run resolved: %q", s.name, program)
		}

		if len(s.args) == 0 || s.args[0] != "build" {
			t.Fatalf("the release image build runs %v, want a build", s.args)
		}

		if file := theFlagValue(s.args, "-f"); file != "Containerfile" {
			t.Errorf("the release image build names %q as the file it builds from, want Containerfile: docker looks for Dockerfile and does not find Containerfile",
				file)
		}
	}
}

// the container run arguments keep files the container writes in
// the operator's tree owned by the operator.
func TestTheContainerRunArgsAreTheOnesTheProgramUnderstands(t *testing.T) {
	podman := theContainerProgram{name: podmanProgram}
	docker := theContainerProgram{name: dockerProgram}

	want := []string{"run", "--rm", "--userns=host"}

	if got := podman.run(); !sameArgs(got, want) {
		t.Errorf("podman is asked to run with %v, want %v", got, want)
	}

	want = []string{"run", "--rm", "--user", strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid())}

	if got := docker.run(); !sameArgs(got, want) {
		t.Errorf("docker is asked to run with %v, want %v: the files it writes have to belong to the operator who ran the build",
			got, want)
	}
}

// sameArgs says two argument lists are the same arguments in the same order.
func sameArgs(got, want []string) bool {
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

// theFlagValue reads the value a flag carries in a command line, and says nothing when the flag is not
// there.
func theFlagValue(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}

	return ""
}

// the compile guide names every command the tool accepts.
func TestTheCompileGuideNamesEveryCommandTheToolAccepts(t *testing.T) {
	section := theGuideSection(t, "## What to type")

	for _, name := range knownCommands {
		if !strings.Contains(section, "cmd/build "+name) {
			t.Errorf("the compile guide's command section does not name cmd/build %s, which the tool accepts", name)
		}
	}
}

// the compile guide says how to verify a build.
func TestTheCompileGuideSaysHowToVerifyABuild(t *testing.T) {
	section := theGuideSection(t, "## How to verify a build")

	for _, want := range []string{"go test ./...", "npm test", "cmd/build test"} {
		if !strings.Contains(section, want) {
			t.Errorf("the compile guide's verification section does not name %s, which is how a build is verified", want)
		}
	}
}

// the compile guide names the container builder the ffmpeg compiles need: a container program
// without it stops at the FreeBSD sysroot stage before a compiler starts, and what it answers
// there names no file and no instruction, only a step number.
// see the container-image-build-contract maintainer note.
func TestTheCompileGuideNamesTheContainerBuilderTheCompilesNeed(t *testing.T) {
	path := filepath.Join("..", "..", "COMPILING.md")

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}

	notes := strings.ToLower(string(body))

	for _, want := range []string{
		"buildkit",
		"buildx",
		"operating system is not supported",
		"from --platform=freebsd/amd64",
		"step 4/11",
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("the compile guide never says %q, which is what an operator whose container program "+
				"cannot build the FreeBSD stage has to match against their own failure", want)
		}
	}
}

// theGuideSection reads one section of the compile guide: the lines
// under the heading named, up to the next heading of the same level.
func theGuideSection(t *testing.T, heading string) string {
	t.Helper()

	path := filepath.Join("..", "..", "COMPILING.md")

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}

	lines := strings.Split(string(body), "\n")

	start := -1

	for i, line := range lines {
		if strings.TrimSpace(line) == heading {
			start = i

			break
		}
	}

	if start == -1 {
		t.Fatalf("%s has no %q section, which is where the operator looks for it", path, heading)
	}

	level := strings.HasPrefix(heading, "## ")

	var section []string

	for _, line := range lines[start+1:] {
		if level && strings.HasPrefix(line, "## ") {
			break
		}

		section = append(section, line)
	}

	return strings.Join(section, "\n")
}
