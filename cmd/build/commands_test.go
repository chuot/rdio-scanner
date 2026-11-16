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

	version, err := buildinfo.Derive("8.0.0", "", "db79311", "", false)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}

	return version
}

// thePrerelease is the version a plan is composed for when the operator asked for a name and
// the tree had nothing uncommitted: the shape a prerelease that stands on its own has.
func thePrerelease(t *testing.T) buildinfo.Version {
	t.Helper()

	version, err := buildinfo.Derive("8.0.0", "", "db79311", "beta1", false)
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

	for _, c := range []struct {
		name    string
		version func(*testing.T) buildinfo.Version
		want    string
	}{
		{
			name:    "a development build",
			version: theVersion,
			want:    "go build -trimpath -ldflags -s -w -X main.Version=8.0.0+gdb79311 -o " + defaultOutputPath("") + " ./cmd/rdio-scanner",
		},
		{
			name:    "a prerelease of a committed tree",
			version: thePrerelease,
			want:    "go build -trimpath -ldflags -s -w -X main.Version=8.0.0-beta1 -o " + defaultOutputPath("") + " ./cmd/rdio-scanner",
		},
	} {
		s := serverStep(o, "/repo", c.version(t))

		got := strings.Join(append([]string{s.name}, s.args...), " ")
		if got != c.want {
			t.Errorf("%s is built with\n  %s\nwant\n  %s", c.name, got, c.want)
		}

		if s.dir != "/repo" {
			t.Errorf("the server is built in %q, want the repository root", s.dir)
		}
	}
}

// the container step asks for the architecture the run resolves, tags the image with the
// resolved version named for that architecture, and keeps the unarch'd pair for the
// architecture the published image has always carried.
func TestTheContainerStepTagsTheImageWithTheVersionTheRunResolved(t *testing.T) {
	for _, c := range []struct {
		name    string
		target  string
		version func(*testing.T) buildinfo.Version
		args    []string
		result  string
	}{
		{
			name:    "a development build for linux-amd64",
			target:  "linux-amd64",
			version: theVersion,
			args: []string{
				"build",
				"--pull",
				"--platform=linux/amd64",
				"-f", "Containerfile",
				"--build-arg", "VERSION=8.0.0+gdb79311",
				"--build-arg", "BUILD_TIME=2026-10-13T12:30:45Z",
				"-t", "rdio-scanner:8.0.0-gdb79311-linux-amd64",
				"-t", "rdio-scanner:latest-linux-amd64",
				"-t", "rdio-scanner:8.0.0-gdb79311",
				"-t", "rdio-scanner:latest",
				".",
			},
			result: "rdio-scanner:8.0.0-gdb79311-linux-amd64",
		},
		{
			name:    "a prerelease of a committed tree for linux-amd64",
			target:  "linux-amd64",
			version: thePrerelease,
			args: []string{
				"build",
				"--pull",
				"--platform=linux/amd64",
				"-f", "Containerfile",
				"--build-arg", "VERSION=8.0.0-beta1",
				"--build-arg", "BUILD_TIME=2026-10-13T12:30:45Z",
				"-t", "rdio-scanner:8.0.0-beta1-linux-amd64",
				"-t", "rdio-scanner:latest-linux-amd64",
				"-t", "rdio-scanner:8.0.0-beta1",
				"-t", "rdio-scanner:latest",
				".",
			},
			result: "rdio-scanner:8.0.0-beta1-linux-amd64",
		},
		{
			name:    "a development build for linux-arm64",
			target:  "linux-arm64",
			version: theVersion,
			args: []string{
				"build",
				"--pull",
				"--platform=linux/arm64",
				"-f", "Containerfile",
				"--build-arg", "VERSION=8.0.0+gdb79311",
				"--build-arg", "BUILD_TIME=2026-10-13T12:30:45Z",
				"-t", "rdio-scanner:8.0.0-gdb79311-linux-arm64",
				"-t", "rdio-scanner:latest-linux-arm64",
				".",
			},
			result: "rdio-scanner:8.0.0-gdb79311-linux-arm64",
		},
		{
			name:    "a development build for linux-arm",
			target:  "linux-arm",
			version: theVersion,
			args: []string{
				"build",
				"--pull",
				"--platform=linux/arm/v7",
				"-f", "Containerfile",
				"--build-arg", "VERSION=8.0.0+gdb79311",
				"--build-arg", "BUILD_TIME=2026-10-13T12:30:45Z",
				"-t", "rdio-scanner:8.0.0-gdb79311-linux-arm",
				"-t", "rdio-scanner:latest-linux-arm",
				".",
			},
			result: "rdio-scanner:8.0.0-gdb79311-linux-arm",
		},
	} {
		s := containerStep(options{sub: subContainer, target: c.target}, "/repo", c.version(t), theMoment, podmanProgram)

		if strings.Join(s.args, " ") != strings.Join(c.args, " ") {
			t.Errorf("%s: podman is asked to run\n  %v\nwant\n  %v", c.name, s.args, c.args)
		}

		if s.result != c.result {
			t.Errorf("%s: the run reports the image as %q, want the image named for the version and the architecture the run resolved", c.name, s.result)
		}
	}
}

// the image tag is the version spelled the way an image tag's grammar allows.
func TestTheImageTagIsTheVersionSpelledTheWayAnImageTagAllows(t *testing.T) {
	for _, version := range []string{"8.0.0", "8.0.0+g1885b7e2", "8.0.0-rc1+g1885b7e2", "8.0.0-beta1"} {
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

	// A version carrying no git metadata is already a tag: the name the operator
	// asked for, standing on its own, is written into the image unchanged.
	if tag := imageTagFor("8.0.0-beta1"); tag != "8.0.0-beta1" {
		t.Errorf("the tag for 8.0.0-beta1 is %q, want 8.0.0-beta1: there is no git metadata in that version to rewrite", tag)
	}
}

// theImageTagGrammar is the tag grammar a registry accepts.
var theImageTagGrammar = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// theContainerfileStage is one build stage of the Containerfile: the FROM line that starts it, and
// the instructions that follow it up to the next FROM.
type theContainerfileStage struct {
	from string
	text string
}

// theContainerfileStages splits the Containerfile at its FROM lines. The lines before the first FROM
// declare the build arguments the stages are built with, and a line that is only a comment is left
// out: what these cases read is what the file asks the container program to do.
func theContainerfileStages(contents string) []theContainerfileStage {
	stages := []theContainerfileStage{}

	for _, line := range strings.Split(contents, "\n") {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.HasPrefix(trimmed, "FROM ") {
			stages = append(stages, theContainerfileStage{from: trimmed})

			continue
		}

		if len(stages) == 0 {
			continue
		}

		last := &stages[len(stages)-1]

		if last.text == "" {
			last.text = trimmed
		} else {
			last.text += "\n" + trimmed
		}
	}

	return stages
}

// theCompilingContainerfileStage is the stage that runs the Go compiler, and the second result says
// whether the file has one.
func theCompilingContainerfileStage(stages []theContainerfileStage) (theContainerfileStage, bool) {
	for _, stage := range stages {
		if strings.Contains(stage.text, "go build ") {
			return stage, true
		}
	}

	return theContainerfileStage{}, false
}

// theShippingContainerfileStage is the stage the finished image is taken from: the one that does not
// run the Go compiler.
func theShippingContainerfileStage(stages []theContainerfileStage) (theContainerfileStage, bool) {
	for _, stage := range stages {
		if !strings.Contains(stage.text, "go build ") {
			return stage, true
		}
	}

	return theContainerfileStage{}, false
}

// theGuardedFfmpegPath is the bundle path the Containerfile looks for before it compiles, or an empty
// string when it looks for none.
func theGuardedFfmpegPath(contents string) string {
	const guard = "test -f internal/infrastructure/ffmpeg/bundle/"

	_, rest, found := strings.Cut(contents, guard)
	if !found {
		return ""
	}

	path, _, _ := strings.Cut(rest, " ")

	return path
}

// the Containerfile builds the image in two stages, names each base image by version, asks for the
// machine doing the build on the stage that compiles and on no other, and carries the image's own
// account of itself in the stage that ships.
func TestTheContainerfileNamesItsBaseImageByVersionAndCarriesTheImageLabels(t *testing.T) {
	text, err := os.ReadFile(filepath.Join("..", "..", "Containerfile"))
	if err != nil {
		t.Fatalf("reading the Containerfile: %v", err)
	}

	contents := string(text)
	stages := theContainerfileStages(contents)

	if len(stages) != 2 {
		t.Errorf("the Containerfile builds %d stages, want two: one that compiles the server and one that ships it", len(stages))
	}

	// Every part of the build reaches the architecture through the platform the container program is
	// given. A literal architecture anywhere in the file, or a package named for one, builds the same
	// image whatever the run asked for.
	for _, literal := range []string{"x86_64", "aarch64", "armv7", "amd64", "arm64", "/arm/"} {
		if strings.Contains(contents, literal) {
			t.Errorf("the Containerfile names %q, which fixes the image to one architecture instead of the one the build was asked for", literal)
		}
	}

	compiling, compilingFound := theCompilingContainerfileStage(stages)
	shipping, shippingFound := theShippingContainerfileStage(stages)

	platforms := 0

	for _, stage := range stages {
		from := strings.TrimPrefix(stage.from, "FROM ")

		if platform, rest, _ := strings.Cut(from, " "); strings.HasPrefix(platform, "--platform=") {
			platforms++

			if platform != "--platform=$BUILDPLATFORM" {
				t.Errorf("the stage %q asks for the platform %q, want the machine doing the build through the BUILDPLATFORM build argument",
					stage.from, platform)
			}

			from = rest
		}

		// A stage is named so that the stage after it can copy from it, and the name is not part of the
		// base image the line pulls.
		if fields := strings.Fields(from); len(fields) > 1 {
			for i, field := range fields {
				if strings.EqualFold(field, "AS") && i+1 < len(fields) {
					from = strings.Join(append(fields[:i], fields[i+2:]...), " ")

					break
				}
			}
		}

		if strings.Contains(from, ":latest") {
			t.Errorf("a stage is built from %q, and `latest` is a moving version: the image built today is not the image built last week", stage.from)
		}

		base, named := strings.CutPrefix(from, "docker.io/")
		if !named {
			t.Errorf("a stage is built from %q, want the base image named on the registry it is pulled from", stage.from)

			continue
		}

		name, version, withVersion := strings.Cut(base, ":")
		if !withVersion || name != "alpine" || version == "" {
			t.Errorf("a stage is built from %q, want alpine named by version", stage.from)

			continue
		}

		for _, part := range strings.Split(version, ".") {
			if _, err := strconv.Atoi(part); err != nil {
				t.Errorf("a stage is built from %q, which is not a release number", stage.from)
			}
		}
	}

	if platforms != 1 {
		t.Errorf("the Containerfile names a platform on %d of its %d FROM lines, want the one stage that compiles: the stage that ships takes the architecture the build was asked for",
			platforms, len(stages))
	}

	// The argument naming the machine doing the build is declared above the FROM that reads it: below
	// that line the argument has no value yet, and the stage is built for the wrong architecture.
	argLine, fromLine := 0, 0

	for i, line := range strings.Split(contents, "\n") {
		trimmed := strings.TrimSpace(line)

		if argLine == 0 && (trimmed == "ARG BUILDPLATFORM" || strings.HasPrefix(trimmed, "ARG BUILDPLATFORM ")) {
			argLine = i + 1
		}

		if fromLine == 0 && strings.HasPrefix(trimmed, "FROM --platform=$BUILDPLATFORM") {
			fromLine = i + 1
		}
	}

	if argLine == 0 {
		t.Error("the Containerfile declares no ARG BUILDPLATFORM build argument, so no stage can name the architecture of the machine doing the build")
	}

	if fromLine == 0 {
		t.Error("no FROM line asks for a platform through $BUILDPLATFORM, so the compile runs on the architecture being built for and needs a cross-compiler")
	}

	if argLine != 0 && fromLine != 0 && argLine > fromLine {
		t.Errorf("ARG BUILDPLATFORM is declared on line %d, below the FROM that reads it on line %d: the container program has no value for it when that line is reached",
			argLine, fromLine)
	}

	if !compilingFound {
		t.Fatal("no stage of the Containerfile compiles the server, so the image builds no binary")
	}

	if !shippingFound {
		t.Fatal("every stage of the Containerfile runs the Go compiler, so the image ships the toolchain that built it")
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
		if !strings.Contains(shipping.text, "LABEL "+label) {
			t.Errorf("the stage that ships carries no %s label", strings.TrimSuffix(label, "="))
		}
	}

	for _, label := range [][2]string{
		{"org.opencontainers.image.version=", "VERSION"},
		{"org.opencontainers.image.created=", "BUILD_TIME"},
		{"org.opencontainers.image.ref.name=", "VERSION"},
	} {
		if !strings.Contains(shipping.text, "ARG "+label[1]+"=") {
			t.Errorf("the stage that ships declares no %s build argument, so its %s label cannot carry what the run knows",
				label[1], strings.TrimSuffix(label[0], "="))
		}

		if !strings.Contains(shipping.text, "LABEL "+label[0]+`"`+`$`+label[1]+`"`) {
			t.Errorf("the Containerfile's %s label does not read the %s build argument", strings.TrimSuffix(label[0], "="), label[1])
		}
	}

	// What the shipped stage carries is the server, the directory it writes, and the packages it
	// converts with: the source tree and the toolchain that built the binary stay behind.
	for _, want := range []string{
		"WORKDIR /app",
		"ENV DOCKER=1",
		"COPY --from=",
		"USER 10001",
		`VOLUME [ "/app/data" ]`,
		"EXPOSE 3000",
		`ENTRYPOINT [ "./rdio-scanner", "-base_dir", "/app/data" ]`,
	} {
		if !strings.Contains(shipping.text, want) {
			t.Errorf("the stage that ships carries no %s instruction", want)
		}
	}

	for _, notWant := range []string{"go build ", "COPY cmd/", "COPY internal/", "apk --no-cache --no-progress --virtual"} {
		if strings.Contains(shipping.text, notWant) {
			t.Errorf("the stage that ships carries %q, so the image holds a source tree or a toolchain it does not run", notWant)
		}
	}

	for _, notWant := range []string{"ENTRYPOINT", "USER 10001", `VOLUME [`, "ENV DOCKER=1", "LABEL "} {
		if strings.Contains(compiling.text, notWant) {
			t.Errorf("the stage that compiles carries %q, which belongs in the stage that ships", notWant)
		}
	}

	// Every part of the build reaches the architecture through the platform the container program is
	// given. A literal architecture anywhere in the file, or a package named for one, builds the same
	// image whatever the run asked for.
	for _, literal := range []string{"x86_64", "aarch64", "armv7", "amd64", "arm64", "/arm/"} {
		if strings.Contains(contents, literal) {
			t.Errorf("the Containerfile names %q, which fixes the image to one architecture instead of the one the build was asked for", literal)
		}
	}
}

// the Containerfile compiles the server with the posture the build tool composes, for the architecture
// the image build was asked for. The flags are read from the compiler's own line; the operating system
// and architecture it compiles for are read from the instruction that line continues from, because the
// guard that stops an image shipping without a matching ffmpeg shares that instruction.
func TestTheContainerfileCompilesTheServerTheWayTheBuildToolDoes(t *testing.T) {
	text, err := os.ReadFile(filepath.Join("..", "..", "Containerfile"))
	if err != nil {
		t.Fatalf("reading the Containerfile: %v", err)
	}

	contents := string(text)
	lines := strings.Split(contents, "\n")

	at := -1

	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "go build ") {
			at = i

			break
		}
	}

	if at < 0 {
		t.Fatal("the Containerfile has no go build line, so nothing here holds how the image compiles the server")
	}

	compile := strings.TrimSpace(lines[at])
	instruction := compile

	for at > 0 && strings.HasSuffix(strings.TrimSpace(lines[at-1]), "\\") {
		at--

		instruction = strings.TrimSpace(lines[at]) + " " + instruction
	}

	if !strings.Contains(compile, " -trimpath ") {
		t.Errorf("the Containerfile compiles the server as %q, which does not ask the compiler to record source paths without the folders of the machine it ran on", compile)
	}

	// the trimming flag is a build argument, and the quoted linker value is not where the compiler takes it.
	_, linker, found := strings.Cut(compile, `"`)
	if !found {
		t.Fatalf("the Containerfile compiles the server as %q, which passes no quoted value to -ldflags", compile)
	}

	flags, _, found := strings.Cut(linker, `"`)
	if !found {
		t.Fatalf("the Containerfile compiles the server as %q, whose -ldflags value is not closed", compile)
	}

	for _, want := range []string{"-s", "-w", "-X main.Version=$VERSION"} {
		if !strings.Contains(flags, want) {
			t.Errorf("the Containerfile links the server with %q, which does not carry %s", flags, want)
		}
	}

	if strings.Contains(flags, "-trimpath") {
		t.Errorf("the Containerfile links the server with %q, which carries the trimming flag inside the linker flags, where the compiler does not take it", flags)
	}

	// the version arrives through the build argument, so the line does not go stale with the release.
	if literal := regexp.MustCompile(`main\.Version=[^$]`).FindString(compile); literal != "" {
		t.Errorf("the Containerfile stamps the version with %q, which is a literal version rather than the VERSION build argument", literal)
	}

	// The compile is for the architecture the image build was asked for, named through the build
	// arguments the container program fills in for the platform it was given: the same way the build
	// tool names the pair when it compiles this server for a release.
	for _, want := range []string{"CGO_ENABLED=0 ", "GOOS=$TARGETOS ", "GOARCH=$TARGETARCH "} {
		if !strings.Contains(instruction, want) {
			t.Errorf("the Containerfile compiles the server as %q, which does not carry %s, so the compile is not the one the build tool composes for the architecture the run resolves", instruction, want)
		}
	}

	// The architecture is named once. A second spelling, a variant, a full platform, or a Go ARM
	// setting, is a second answer to the question the platform already answered, and the two drift.
	for _, notWant := range []string{"GOARM", "TARGETVARIANT", "TARGETPLATFORM", "CGO_ENABLED=1"} {
		if strings.Contains(instruction, notWant) {
			t.Errorf("the Containerfile compiles the server as %q, which names %s beside the platform the build was asked for", instruction, notWant)
		}
	}

	if literal := regexp.MustCompile(`GO(OS|ARCH)=[^$]`).FindString(instruction); literal != "" {
		t.Errorf("the Containerfile compiles the server with %q, which names an operating system or architecture as a literal instead of reading the build arguments the container program fills in", literal)
	}

	// The ffmpeg the binary embeds is checked for the architecture being built for. A guard that
	// names one architecture lets an arm image ship a server with an amd64 ffmpeg, or none at all.
	guard := theGuardedFfmpegPath(contents)

	if guard == "" {
		t.Error("the Containerfile has no ffmpeg guard naming the bundle directory, so an image can ship a server that cannot convert anything")
	} else if !strings.Contains(guard, "${TARGETOS}-${TARGETARCH}") {
		t.Errorf("the Containerfile checks for the ffmpeg at %q, which names a fixed architecture rather than the one the image build was asked for", guard)
	}
}

// The message an image build writes when the ffmpeg for the architecture it was asked for is not on
// disk names that architecture and the run that compiles it: whoever reads it is holding a failed image
// build, and the fix is a build-tool run.
func TestTheContainerfileRefusesAnImageWhoseFfmpegIsNotCompiledForTheTarget(t *testing.T) {
	text, err := os.ReadFile(filepath.Join("..", "..", "Containerfile"))
	if err != nil {
		t.Fatalf("reading the Containerfile: %v", err)
	}

	contents := string(text)

	_, rest, found := strings.Cut(contents, "test -f internal/infrastructure/ffmpeg/bundle/")
	if !found {
		t.Fatal("the Containerfile has no ffmpeg guard naming the bundle directory, so an image can ship a server that cannot convert anything")
	}

	_, after, found := strings.Cut(rest, `echo "`)
	if !found {
		t.Fatal("the Containerfile's ffmpeg guard writes no message, so the build stops without saying why")
	}

	message, _, closed := strings.Cut(after, `"`)
	if !closed {
		t.Fatal("the Containerfile's ffmpeg guard message is not closed")
	}

	// The architecture that is missing and the run that produces it both arrive through the build
	// arguments the container program fills in. A message naming one architecture sends the reader to
	// the wrong build.
	for _, want := range []string{"${TARGETOS}-${TARGETARCH}", "go run ./cmd/build container"} {
		if !strings.Contains(message, want) {
			t.Errorf("the Containerfile refuses a missing ffmpeg with %q, which does not name %s", message, want)
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
// runs on machines the release machine has never seen, and the source paths it records name the module they
// belong to rather than the folders of the machine that compiled it.
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

	if !contains(s.args, "-trimpath") {
		t.Errorf("the server is built with %v, which does not ask the compiler to record source paths without the folders of the machine it ran on", s.args)
	}

	if strings.Contains(flags, "-trimpath") {
		t.Errorf("the server is linked with %q, which carries the trimming flag inside the linker flags, where the compiler does not take it", flags)
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

// the usage line names the commands the tool has, and names the three architectures a container run
// builds for rather than offering a target the run refuses.
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

	// A container run builds one of three architectures, and the line names those three: a line naming
	// only the command sends the operator to a target the run refuses. `linux-arm` is read where no digit
	// follows it, because `linux-arm64` carries the shorter word inside itself.
	for _, tc := range []struct {
		target string
		named  *regexp.Regexp
	}{
		{target: "linux-amd64", named: regexp.MustCompile(`linux-amd64`)},
		{target: "linux-arm64", named: regexp.MustCompile(`linux-arm64`)},
		{target: "linux-arm", named: regexp.MustCompile(`linux-arm[^0-9]`)},
	} {
		if !tc.named.MatchString(line) {
			t.Errorf("the usage line %q does not name %s, an architecture a container run builds for", line, tc.target)
		}
	}

	// The all-architectures request is spelled for the container run the way it is spelled for the
	// release run: the flag sits beside the target words that run accepts, so an operator who wants
	// all three reads it from the same place in both halves of the line.
	if !regexp.MustCompile(`container \[linux-amd64\|linux-arm64\|linux-arm\] \[--all\]`).MatchString(line) {
		t.Errorf("the usage line %q does not spell the container run's all-architectures request beside the three architectures it names", line)
	}

	for _, notWant := range []string{"linux-386", "freebsd-amd64"} {
		if strings.Contains(line, notWant) {
			t.Errorf("the usage line offers a container target the run refuses: %s in %q", notWant, line)
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

// a container run builds one of three architectures: the word after `container` names one, --all
// asks for all three, and a run naming neither decides the architecture it builds for from the
// machine it runs on. A word outside the three is refused by a message naming them, because a
// container image has no build for any other platform; a word that is no target at all is a stray word.
func TestTheTargetAContainerRunNamesIsOneOfTheThreeArchitecturesItsImageIsBuiltFor(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		target string
		all    bool
	}{
		{args: []string{"container"}},
		{args: []string{"container", "--all"}, all: true},
		{args: []string{"--all", "container"}, all: true},
		{args: []string{"container", "linux-amd64"}, target: "linux-amd64"},
		{args: []string{"container", "linux-arm"}, target: "linux-arm"},
		{args: []string{"container", "linux-arm64"}, target: "linux-arm64"},
	} {
		o, err := parseArgs(tc.args)
		if err != nil {
			t.Errorf("%v: the command was refused: %v", tc.args, err)
			continue
		}

		if o.sub != subContainer {
			t.Errorf("%v runs %q, want a container run", tc.args, o.sub)
		}

		if o.target != tc.target {
			if tc.target == "" {
				t.Errorf("%v names the target %q, and a run that names no target decides it from the machine it runs on", tc.args, o.target)
			} else {
				t.Errorf("%v names the target %q, want the architecture its image is built for: %s", tc.args, o.target, tc.target)
			}
		}

		if o.all != tc.all {
			t.Errorf("%v asks for the three architectures a container image is built for as %t, want %t", tc.args, o.all, tc.all)
		}
	}

	for _, args := range [][]string{
		{"container", "linux-386"},
		{"container", "freebsd-amd64"},
		{"container", "macos-arm64"},
		{"container", "windows-amd64"},
	} {
		_, err := parseArgs(args)
		if err == nil {
			t.Errorf("%v is accepted, and no container image is built for %s", args, args[1])
			continue
		}

		for _, want := range []string{"linux-amd64", "linux-arm", "linux-arm64"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal %q does not name %s, an architecture a container run builds for", err, want)
			}
		}
	}

	if _, err := parseArgs([]string{"container", "nonsense"}); err == nil {
		t.Error(`container nonsense is accepted, and "nonsense" names no target`)
	}

	o, err := parseArgs([]string{"container", "linux-arm64", "--pre", "alpha"})
	if err != nil {
		t.Fatalf("container linux-arm64 --pre alpha was refused: %v", err)
	}

	if o.target != "linux-arm64" {
		t.Errorf("container linux-arm64 --pre alpha names the target %q, want linux-arm64", o.target)
	}

	if o.pre != "alpha" {
		t.Errorf("container linux-arm64 --pre alpha carries the prerelease %q, want alpha", o.pre)
	}
}

// --all and a target word are one request each, so a container command line carrying both is
// refused however the words arrive in order. A target the image is built for is refused by the
// sentence naming both; a target it is not built for is refused by the sentence naming the three,
// because no container image is built for it.
func TestAContainerRunAskedForAllAndATargetIsRefusedByTheSentenceNamingThem(t *testing.T) {
	for _, tc := range []struct {
		args []string
		says []string
	}{
		{args: []string{"container", "--all", "linux-arm64"},
			says: []string{"--all builds every architecture a container image is built for", "linux-arm64", "name one target or --all, not both"}},
		{args: []string{"container", "linux-arm64", "--all"},
			says: []string{"--all builds every architecture a container image is built for", "linux-arm64", "name one target or --all, not both"}},
		{args: []string{"--all", "container", "linux-arm64"},
			says: []string{"--all builds every architecture a container image is built for", "linux-arm64", "name one target or --all, not both"}},
		{args: []string{"container", "--all", "linux-386"},
			says: []string{"a container image is built for linux-amd64, linux-arm, linux-arm64", "is not a target a container run builds for"}},
		{args: []string{"container", "macos-arm64", "--all"},
			says: []string{"a container image is built for linux-amd64, linux-arm, linux-arm64", "is not a target a container run builds for"}},
	} {
		_, err := parseArgs(tc.args)
		if err == nil {
			t.Errorf("%v is accepted, and --all and a target word are not one request", tc.args)
			continue
		}

		for _, want := range tc.says {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal %q does not say %q, which is what %v asked for", err, want, tc.args)
			}
		}
	}

	// The flag arriving first still reaches the container run, so a word that names no target is
	// refused for that word and not for the flag.
	_, err := parseArgs([]string{"--all", "container", "nonsense"})
	if err == nil {
		t.Fatal("--all container nonsense is accepted, and nonsense names no target")
	}

	if !strings.Contains(err.Error(), "nonsense") {
		t.Errorf("the refusal %q does not name nonsense, the word %v got wrong", err, []string{"--all", "container", "nonsense"})
	}
}

// --all stays the release matrix's flag for every command that builds one thing, and the refusal
// each one answers today stays the same. `--all dist` is refused today even though
// `dist --all` is the release run that asks for the matrix, and stays refused: a finding this
// feature records without taking on.
func TestAllStaysRefusedForTheCommandsThatBuildOneThing(t *testing.T) {
	for _, tc := range []struct {
		args []string
		says string
	}{
		{args: []string{"--all"}, says: "--all is the release matrix, and a run that names no command builds this machine's whole tree"},
		{args: []string{"ffmpeg", "--all"}, says: "--all is the release matrix, and ffmpeg is not it"},
		{args: []string{"server", "--all"}, says: "--all is the release matrix, and server is not it"},
		{args: []string{"clean", "--all"}, says: "--all is the release matrix, and clean is not it"},
		{args: []string{"--all", "dist"}, says: "--all is the release matrix, and dist is not it"},
		{args: []string{"dist", "--all", "linux-arm64"}, says: "--all builds every platform this project releases, and linux-arm64 is one of them: name one target or --all, not both"},
	} {
		_, err := parseArgs(tc.args)
		if err == nil {
			t.Errorf("%v is accepted, and --all is not a flag it is asked for", tc.args)
			continue
		}

		if !strings.Contains(err.Error(), tc.says) {
			t.Errorf("the refusal %q does not say %q", err, tc.says)
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
		{"container", "--all", "linux-arm64"},
		{"container", "linux-arm64", "--all"},
		{"--all", "container", "linux-arm64"},
		{"--all"},
		{"ffmpeg", "--all"},
		{"server", "--all"},
		{"client", "--all"},
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

// the container step names the resolved program and the Containerfile it builds from, asks for
// the architecture exactly once, and never publishes: the arguments name no build argument
// carrying an architecture, no push, no manifest list, and no registry to push to.
func TestTheReleaseImageBuildNamesTheProgramAndTheFileItBuildsFrom(t *testing.T) {
	for _, program := range []string{podmanProgram, dockerProgram} {
		for _, target := range containerTargets {
			s := containerStep(options{sub: subContainer, target: target}, "/repo", theVersion(t), theMoment, program)

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

			// the architecture is named once, by the flag the container program selects
			// the build architecture with, and by nothing else: the automatic build
			// arguments carry what that flag asked for.
			named := 0

			for _, arg := range s.args {
				if strings.HasPrefix(arg, "--platform=") {
					named++
				}
			}

			if named != 1 {
				t.Errorf("the %s image build names the build architecture %d times, want once: %v", target, named, s.args)
			}

			for _, arg := range s.args {
				if !strings.HasPrefix(arg, "--build-arg") {
					continue
				}

				for _, word := range []string{"TARGETARCH", "TARGETOS", "TARGETPLATFORM", "TARGETVARIANT", "BUILDPLATFORM", "GOARCH", "GOOS", "GOARM"} {
					if strings.Contains(arg, word) {
						t.Errorf("the %s image build passes %q, which names the architecture as a build argument: the container program fills %s itself from the platform it was asked for",
							target, arg, word)
					}
				}
			}

			for _, arg := range s.args {
				if arg == "push" || arg == "manifest" {
					t.Errorf("the %s image build runs %q, and the build publishes nothing: it writes an image into this machine's container store", target, arg)
				}

				if strings.HasPrefix(arg, "-t") || strings.Contains(arg, "rdio-scanner:") {
					if strings.Contains(arg, "/") {
						t.Errorf("the %s image build tags %q, which names a registry or a namespace the build publishes to", target, arg)
					}
				}
			}
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

// the core count of the machine a program runs containers on is asked for in that program's own
// words: each program templates its own info payload, and a field written for one is a field the
// other does not have.
func TestTheContainerProgramAsksForItsCoresInItsOwnForm(t *testing.T) {
	podman := theContainerProgram{name: podmanProgram}
	docker := theContainerProgram{name: dockerProgram}

	if got := podman.coresAsk(); !sameArgs(got, []string{"info", "--format", "{{.Host.CPUs}}"}) {
		t.Errorf("podman is asked for its cores with %v, want info --format {{.Host.CPUs}}: podman answers its info with a "+
			"payload that has no NCPU field, and says so instead of giving a count", got)
	}

	if got := docker.coresAsk(); !sameArgs(got, []string{"info", "--format", "{{.NCPU}}"}) {
		t.Errorf("docker is asked for its cores with %v, want info --format {{.NCPU}}: docker answers its info with a "+
			"payload that has no Host field, and says so instead of giving a count", got)
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

// the compile guide says what the build tool asks the compiler to record
// about a source file: the paths a shipped server binary carries are
// rooted in the module that holds the file, the flag that makes it so is
// named as the tool's own doing for every target a release writes, and a
// server built without the tool is described as keeping the paths of the
// machine it was built on.
//
// the guide is read whole, the way the test above reads it, and not
// through theGuideSection: that helper stops only at the next `## `
// heading, so a `### ` heading handed to it answers with everything from
// there to the end of the file, and a test built on it would pass against
// text it never read.
func TestTheCompileGuideSaysWhatTheToolAsksTheCompilerToRecord(t *testing.T) {
	path := filepath.Join("..", "..", "COMPILING.md")

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}

	guide := string(body)

	if !strings.Contains(guide, "-trimpath") {
		t.Fatal("the compile guide names no compile flag at all, so it never says what the tool asks the compiler to record about a source file")
	}

	// the flag is the tool's own: the guide does not offer it as a flag an operator has to add.
	if typed := regexp.MustCompile("go build[^\n`]*-trimpath").FindString(guide); typed != "" {
		t.Errorf("the compile guide shows %q, which is a flag an operator would have to add to a command the build tool already composes", typed)
	}

	paragraph := ""

	for _, candidate := range strings.Split(guide, "\n\n") {
		if strings.Contains(candidate, "-trimpath") {
			paragraph = strings.ToLower(candidate)

			break
		}
	}

	if paragraph == "" {
		t.Fatal("the compile guide names -trimpath only outside a paragraph of its own, where nothing says what the compiler records")
	}

	for _, want := range []string{"module", "build machine", "crash log", "documented", "every release target", "go build"} {
		if !strings.Contains(paragraph, want) {
			t.Errorf("the compile guide's passage about -trimpath never says %q, which is what tells a reader what the recorded paths name, for which binaries they do, and where the tool's own flag comes from", want)
		}
	}
}

// the compile guide says which architectures a container image is built for, how the
// architecture reaches the container program, what tags the run writes, and what host the
// arm image needs. A maintainer who builds an arm image from a source tree reads these facts
// off the guide, and no other document in the published repository states them.
func TestTheCompileGuideNamesTheArchitecturesAContainerImageIsBuiltFor(t *testing.T) {
	section := theGuideSection(t, "## What to type")

	for _, want := range []string{"cmd/build container", "cmd/build container linux-arm64"} {
		if !strings.Contains(section, want) {
			t.Errorf("the compile guide's command section does not type %s, which is how a container image for another architecture is built", want)
		}
	}

	passage := theGuideSubsection(t, "### The container image, and the architectures it is built for")

	// `linux-arm` is read where no digit follows it, because `linux-arm64` carries the shorter
	// word inside itself.
	for _, tc := range []struct {
		target string
		named  *regexp.Regexp
	}{
		{target: "linux-amd64", named: regexp.MustCompile(`linux-amd64`)},
		{target: "linux-arm64", named: regexp.MustCompile(`linux-arm64`)},
		{target: "linux-arm", named: regexp.MustCompile(`linux-arm[^0-9]`)},
	} {
		if !tc.named.MatchString(passage) {
			t.Errorf("the compile guide's container image section does not name %s, which is one of the three architectures a container run builds for", tc.target)
		}
	}

	for _, want := range []string{
		"go run ./cmd/build container",
		"--platform=linux/amd64",
		"--platform=linux/arm64",
		"--platform=linux/arm/v7",
		"$BUILDPLATFORM",
		"GOOS=$TARGETOS",
		"GOARCH=$TARGETARCH",
		"compiles that target's ffmpeg pair first",
		"never pushed",
		"hard-float",
	} {
		if !strings.Contains(passage, want) {
			t.Errorf("the compile guide's container image section never says %q, which is what a maintainer building an image for another architecture has to know", want)
		}
	}

	// the tags: the architecture-named pair every run writes, and the plain pair the amd64 run
	// also writes. A plain search for the unarch'd forms would pass on the arch'd ones, so each
	// is read only where no dash follows it.
	for _, tc := range []struct {
		tag   string
		named *regexp.Regexp
	}{
		{tag: "rdio-scanner:<version>-<target>", named: regexp.MustCompile(`rdio-scanner:<version>-<target>`)},
		{tag: "rdio-scanner:latest-<target>", named: regexp.MustCompile(`rdio-scanner:latest-<target>`)},
		{tag: "rdio-scanner:<version> with no architecture", named: regexp.MustCompile(`rdio-scanner:<version>[^-]`)},
		{tag: "rdio-scanner:latest with no architecture", named: regexp.MustCompile(`rdio-scanner:latest[^-]`)},
	} {
		if !tc.named.MatchString(passage) {
			t.Errorf("the compile guide's container image section never names %s, which is a tag a container run writes", tc.tag)
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

// theGuideSubsection reads one named subsection of the compile guide: the lines under the
// heading named, up to the next heading of any level. theGuideSection stops only at a heading
// of the level it was given, so a subsection read through it answers with everything from
// there to the end of the file.
func theGuideSubsection(t *testing.T, heading string) string {
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
		t.Fatalf("%s has no %q subsection, which is where the reader looks for it", path, heading)
	}

	var section []string

	for _, line := range lines[start+1:] {
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") {
			break
		}

		section = append(section, line)
	}

	return strings.Join(section, "\n")
}
