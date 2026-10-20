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
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// These are unit tests of the one rule the build reads out of a program it did not compile: which machine
// code the file holds. The build compiles these programs on a machine that runs their operating system and
// runs them there, so nothing else about them is the build's business — not whether they are signed, not
// which libraries they link, not whether they are static. Every rule here is a byte comparison a fixture
// can produce: the fixtures are headers this project writes, not programs it compiled.

// The Mach-O numbers the rules are written in. A macOS ffmpeg is built by the maintainer, and these are
// the numbers his linker writes.
const (
	theMachOMagic   = 0xfeedfacf // MH_MAGIC_64, a thin 64-bit image, little-endian
	theMachO32Magic = 0xfeedface // MH_MAGIC, a 32-bit image: no macOS deployment this project ships
	theFatMagic     = 0xcafebabe // a universal binary, holding more than one architecture

	theX8664CPU = 0x01000007 // CPU_TYPE_X86_64
	theArm64CPU = 0x0100000c // CPU_TYPE_ARM64

	theExecFileType = 2 // MH_EXECUTE: a program the operating system runs, which is what these programs are

	theLoadDylib       = 0x0c       // LC_LOAD_DYLIB
	theLoadWeakDylib   = 0x80000018 // LC_LOAD_WEAK_DYLIB
	theReexportDylib   = 0x8000001f // LC_REEXPORT_DYLIB
	theLoadUpwardDylib = 0x80000023 // LC_LOAD_UPWARD_DYLIB
	theCodeSignature   = 0x8000001d // LC_CODE_SIGNATURE
)

// aMachO is a Mach-O header and the load commands a test cares about, written to a file the build reads.
// Raw, when set, is written alone: a drop that is not a Mach-O at all, and a drop that is empty.
type aMachO struct {
	Magic     uint32
	CPUType   uint32
	Signature bool
	Dylibs    []aDylib
	Mode      os.FileMode
	Body      []byte
	Raw       []byte
}

// an aDylib is one library reference, named the way the linker names it.
type aDylib struct {
	Command uint32
	Path    string
}

func writeMachO(t *testing.T, name string, fixture aMachO) string {
	t.Helper()

	mode := fixture.Mode
	if mode == 0 {
		mode = 0755
	}

	path := filepath.Join(t.TempDir(), name)

	if fixture.Raw != nil {
		if err := os.WriteFile(path, fixture.Raw, mode); err != nil {
			t.Fatalf("writing the fixture: %v", err)
		}

		return path
	}

	if err := os.WriteFile(path, machOBytes(fixture), mode); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	return path
}

// machOBytes is the header and the load commands a fixture describes, laid out the way a Mach-O linker
// lays them out: magic, cpu type, cpu subtype, file type, the number of load commands, the total size of
// those commands, the flags, a reserved word. The count and the size are two different numbers — four
// commands of 24, 24, 24 and 16 bytes are 4 and 88 — and a reader that takes one for the other reads a
// program a real linker produced as a header whose load commands do not fit inside it.
func machOBytes(fixture aMachO) []byte {
	commands := []byte{}
	count := 0

	for _, dylib := range fixture.Dylibs {
		commands = append(commands, aDylibCommand(dylib)...)
		count++
	}

	if fixture.Signature {
		commands = append(commands, aCodeSignatureCommand()...)
		count++
	}

	header := make([]byte, 32)
	putMachOMagic(header, fixture.Magic)
	binary.LittleEndian.PutUint32(header[4:], fixture.CPUType)
	binary.LittleEndian.PutUint32(header[12:], theExecFileType)
	binary.LittleEndian.PutUint32(header[16:], uint32(count))
	binary.LittleEndian.PutUint32(header[20:], uint32(len(commands)))

	return append(append(header, commands...), fixture.Body...)
}

// putMachOMagic writes the magic the way the image carries it on disk. A universal binary's magic is
// big-endian whatever machine produced it; a thin image's magic is the byte order of the image itself,
// which for the images this project ships is little-endian.
func putMachOMagic(header []byte, magic uint32) {
	if magic == theFatMagic {
		binary.BigEndian.PutUint32(header[0:], magic)
		return
	}

	binary.LittleEndian.PutUint32(header[0:], magic)
}

func aDylibCommand(dylib aDylib) []byte {
	command := make([]byte, 12+len(dylib.Path)+1)
	if len(command)%8 != 0 {
		command = append(command, make([]byte, 8-len(command)%8)...)
	}

	binary.LittleEndian.PutUint32(command[0:], dylib.Command)
	binary.LittleEndian.PutUint32(command[4:], uint32(len(command)))
	binary.LittleEndian.PutUint32(command[8:], 12)
	copy(command[12:], dylib.Path)

	return command
}

func aCodeSignatureCommand() []byte {
	command := make([]byte, 16)

	binary.LittleEndian.PutUint32(command[0:], theCodeSignature)
	binary.LittleEndian.PutUint32(command[4:], uint32(len(command)))

	return command
}

// The command numbers a Mach-O linker writes into a program, beyond the library references and the
// signature the rules above are about. The sizes are the sizes the linker writes for each of them.
const (
	theSegment64Command    = 0x19 // LC_SEGMENT_64, naming one segment of the program
	theSymtabCommand       = 0x02 // LC_SYMTAB
	theDysymtabCommand     = 0x0b // LC_DYSYMTAB
	theLoadDylinkerCommand = 0x0e // LC_LOAD_DYLINKER: the dynamic linker the program needs
	theUUIDCommand         = 0x1b // LC_UUID
	theDyldInfoCommand     = 0x22 // LC_DYLD_INFO_ONLY
	theFunctionStarts      = 0x26 // LC_FUNCTION_STARTS
	theDataInCode          = 0x29 // LC_DATA_IN_CODE
	theBuildVersion        = 0x2c // LC_BUILD_VERSION
	theMainCommand         = 0x80000028
	theExportsTrie         = 0x80000033 // LC_DYLD_EXPORTS_TRIE
	theChainedFixups       = 0x80000034 // LC_DYLD_CHAINED_FIXUPS
)

// aCommandOf is a load command of the size the linker writes for it: its number, the size it occupies,
// and the bytes the linker leaves in the rest.
func aCommandOf(command uint32, size int) []byte {
	bytes := make([]byte, size)

	binary.LittleEndian.PutUint32(bytes[0:], command)
	binary.LittleEndian.PutUint32(bytes[4:], uint32(size))

	return bytes
}

// aSegmentCommand is the 72-byte command a Mach-O linker writes for one segment of a program, naming the
// segment inside it.
func aSegmentCommand(name string) []byte {
	bytes := aCommandOf(theSegment64Command, 72)

	copy(bytes[8:24], name)

	return bytes
}

// aDylinkerCommand names the dynamic linker a program needs, the way the linker writes it: the name
// starts after the command's fixed part.
func aDylinkerCommand(path string) []byte {
	size := 12 + len(path) + 1
	if size%8 != 0 {
		size += 8 - size%8
	}

	bytes := aCommandOf(theLoadDylinkerCommand, size)

	binary.LittleEndian.PutUint32(bytes[8:], 12)
	copy(bytes[12:], path)

	return bytes
}

// aMainCommandSize is the size of the entry-point command for one architecture: it carries the machine's
// register set, so it is a different size for each.
func aMainCommandSize(cpuType uint32) int {
	if cpuType == theArm64CPU {
		return 152
	}

	return 248
}

// theLinkerShapedMachO is the program a Mac's own linker produces for one architecture: five 72-byte
// segments, the symbol tables, the dynamic linker's name, the uuid, the build version, the entry point,
// the fixups, the two libraries the operating system provides, and the code signature. Nineteen commands
// occupying 860 bytes: the count is a small number and the size is in the hundreds, and they live in
// different words of the header. A reader that takes the count for the size reads this program as a
// header whose load commands do not fit inside what it describes.
func theLinkerShapedMachO(cpuType uint32) []byte {
	commands := [][]byte{
		aSegmentCommand("__PAGEZERO"),
		aSegmentCommand("__TEXT"),
		aSegmentCommand("__DATA_CONST"),
		aSegmentCommand("__DATA"),
		aSegmentCommand("__LINKEDIT"),
		aCommandOf(theSymtabCommand, 24),
		aCommandOf(theDysymtabCommand, 52),
		aDylinkerCommand("/usr/lib/dyld"),
		aCommandOf(theUUIDCommand, 24),
		aCommandOf(theBuildVersion, 24),
		aCommandOf(theDyldInfoCommand, 48),
		aCommandOf(theMainCommand, aMainCommandSize(cpuType)),
		aCommandOf(theFunctionStarts, 16),
		aCommandOf(theDataInCode, 16),
		aCommandOf(theChainedFixups, 16),
		aCommandOf(theExportsTrie, 16),
		aDylibCommand(aDylib{Command: theLoadDylib, Path: "/usr/lib/libSystem.B.dylib"}),
		aDylibCommand(aDylib{Command: theLoadDylib, Path: "/usr/lib/libz.1.2.13.dylib"}),
		aCodeSignatureCommand(),
	}

	size := 0

	for _, command := range commands {
		size += len(command)
	}

	header := make([]byte, 32)
	putMachOMagic(header, theMachOMagic)
	binary.LittleEndian.PutUint32(header[4:], cpuType)
	binary.LittleEndian.PutUint32(header[12:], theExecFileType)
	binary.LittleEndian.PutUint32(header[16:], uint32(len(commands)))
	binary.LittleEndian.PutUint32(header[20:], uint32(size))

	body := append([]byte{}, header...)

	for _, command := range commands {
		body = append(body, command...)
	}

	return append(body, make([]byte, 4096)...)
}

// aSystemProgram is one of the two fixtures every rule is measured against: a thin 64-bit image of the
// architecture the target runs, carrying a code signature, linking only libraries the operating system
// itself provides.
func aSystemProgram(cpuType uint32) aMachO {

	return aMachO{
		Magic:     theMachOMagic,
		CPUType:   cpuType,
		Signature: true,
		Dylibs: []aDylib{
			{Command: theLoadDylib, Path: "/usr/lib/libSystem.B.dylib"},
			{Command: theLoadDylib, Path: "/usr/lib/libz.1.2.13.dylib"},
			{Command: theLoadWeakDylib, Path: "/usr/lib/libiconv.2.dylib"},
		},
	}
}

// The shape rule is what lets a macOS archive carry its ffmpeg: the server writes the program out of its
// own binary and runs it, so the file has to be the program that machine runs, and nothing it has to
// install first.
func TestASuppliedMachOIsTheProgramItsTargetRuns(t *testing.T) {
	for _, target := range []string{"macos-amd64", "macos-arm64"} {
		cpuType := uint32(theX8664CPU)
		if target == "macos-arm64" {
			cpuType = theArm64CPU
		}

		fixture := aSystemProgram(cpuType)

		path := writeMachO(t, "ffmpeg", fixture)

		if err := checkSuppliedProgram(path, target); err != nil {
			t.Errorf("%s refused a signed thin %s Mach-O linking only system libraries: %v", target, nameOfCPU(cpuType), err)
		}
	}
}

// The binaries the owner's Mac compiles are a program that machine's own linker shaped: nineteen load
// commands, five of them 72-byte segments, the count of them a small number and the size they occupy in
// the hundreds. The build reads the first words of that header and nothing after it, and accepts the
// program: a reader that walked the load commands read this program as a header whose load commands do
// not fit inside what it describes, which is the answer a Mac that compiled its binaries successfully
// printed when the build came to prove them.
func TestTheBuildReadsAProgramTheMacsLinkerShaped(t *testing.T) {
	for _, tc := range []struct {
		target  string
		cpuType uint32
	}{
		{target: "macos-amd64", cpuType: theX8664CPU},
		{target: "macos-arm64", cpuType: theArm64CPU},
	} {
		path := writeMachO(t, "ffmpeg", aMachO{Raw: theLinkerShapedMachO(tc.cpuType)})

		if err := checkSuppliedProgram(path, tc.target); err != nil {
			t.Errorf("%s refused the program its own linker shapes: %v", tc.target, err)
		}
	}
}

// One archive per architecture is what the release matrix ships, and each binary embeds its own
// architecture's ffmpeg binaries. Binaries built for the other macOS architecture are binaries that
// will not run.
func TestTheBuildRefusesASuppliedMachOOfTheOtherArchitecture(t *testing.T) {
	fixture := aSystemProgram(theArm64CPU)

	path := writeMachO(t, "ffmpeg", fixture)

	err := checkSuppliedProgram(path, "macos-amd64")
	if err == nil {
		t.Fatal("an arm64 program was adopted for macos-amd64, which runs x86-64")
	}

	if !strings.Contains(err.Error(), "architecture") {
		t.Errorf("the failure %q does not name the architecture rule", err.Error())
	}
}

// A universal binary is one file for two architectures. This project ships one archive per architecture
// and each one carries its own ffmpeg binaries, so a fat drop says it was built for a matrix the build
// does not have, and the binaries the archive carries are not the binaries its architecture runs.
func TestTheBuildRefusesAUniversalMachO(t *testing.T) {
	fixture := aSystemProgram(theX8664CPU)
	fixture.Magic = theFatMagic

	path := writeMachO(t, "ffmpeg", fixture)

	err := checkSuppliedProgram(path, "macos-amd64")
	if err == nil {
		t.Fatal("a universal binary was adopted for a single-architecture target")
	}

	if !strings.Contains(err.Error(), "universal") {
		t.Errorf("the failure %q does not name the universal binary rule", err.Error())
	}
}

// The compile signs the programs it produces as the last step of building them, and a program the
// maintainer built by hand is signed by the machine that built it. Whether the code inside the archive is
// signed is that machine's business and macOS's, not the build's: the build adopts it and the licence
// text names the host it came from, which is the evidence a deployment has.
func TestTheBuildAdoptsAMachOWithNoCodeSignature(t *testing.T) {
	fixture := aSystemProgram(theArm64CPU)
	fixture.Signature = false

	path := writeMachO(t, "ffmpeg", fixture)

	if err := checkSuppliedProgram(path, "macos-arm64"); err != nil {
		t.Errorf("an unsigned program was refused for macos-arm64: %v", err)
	}
}

// The recipe the manifest records enables no external library, so the compile links only what the
// operating system provides. A program the maintainer built by hand that links a Homebrew library, a
// library resolved at run time, or a library from his own build directory is his choice: the build adopts
// it, and the licence text names the host that compiled it, which is where a deployment checks whether
// that host's libraries are there.
func TestTheBuildAdoptsAMachOLinkingTheHostsOwnLibraries(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		command    uint32
	}{
		{"a Homebrew library", "/opt/homebrew/opt/x264/lib/libx264.164.dylib", theLoadDylib},
		{"a library path resolved at run time", "@rpath/libavcodec.dylib", theLoadDylib},
		{"a weakly linked library outside the system", "/usr/local/lib/libpng.dylib", theLoadWeakDylib},
		{"a reexported library outside the system", "/opt/homebrew/opt/zstd/lib/libzstd.1.dylib", theReexportDylib},
		{"an upward-linked library outside the system", "/Users/maintainer/build/lib/libssl.dylib", theLoadUpwardDylib},
	} {
		fixture := aSystemProgram(theX8664CPU)
		fixture.Dylibs = append(fixture.Dylibs, aDylib{Command: tc.command, Path: tc.path})

		path := writeMachO(t, "ffmpeg", fixture)

		if err := checkSuppliedProgram(path, "macos-amd64"); err != nil {
			t.Errorf("%s was refused for macos-amd64: %v", tc.name, err)
			continue
		}
	}
}

// The ffmpeg binaries are embedded, gzipped, written out at startup and executed. A file that is not an
// executable program of the target's operating system is not a program at all: it is a drop that went
// wrong, and the
// build says which rule it broke rather than shipping it.
func TestTheBuildRefusesAFileThatIsNotAProgramForItsTarget(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fixture aMachO
		rule    string
	}{
		{
			name:    "a 32-bit program",
			fixture: aMachO{Magic: theMachO32Magic, CPUType: 7, Signature: true},
			rule:    "64-bit",
		},
		{
			name:    "not a Mach-O at all",
			fixture: aMachO{Raw: []byte("\x7fELF\x02\x01\x01\x09\x00\x00\x00\x00\x00\x00\x00\x00")},
			rule:    "Mach-O",
		},
		{
			name:    "an empty file",
			fixture: aMachO{Raw: []byte{}},
			rule:    "empty",
		},
		{
			name:    "a file that is not executable",
			fixture: aMachO{Magic: theMachOMagic, CPUType: theX8664CPU, Signature: true, Mode: 0644},
			rule:    "executable",
		},
	} {
		path := writeMachO(t, "ffprobe", tc.fixture)

		err := checkSuppliedProgram(path, "macos-amd64")
		if err == nil {
			t.Errorf("%s was adopted for macos-amd64", tc.name)
			continue
		}

		if !strings.Contains(err.Error(), tc.rule) {
			t.Errorf("%s: the failure %q does not name the %s rule", tc.name, err.Error(), tc.rule)
		}
	}
}

// aSuppliedProgramBytes is the program a maintainer drops for one target: the shape the build adopts.
func aSuppliedProgramBytes(target string) []byte {
	switch target {
	case "macos-amd64":
		return machOBytes(aSystemProgram(theX8664CPU))
	case "macos-arm64":
		return machOBytes(aSystemProgram(theArm64CPU))
	}

	panic("no supplied program fixture for " + target)
}

// The gate is the binaries' own execution. A host that cannot execute the binaries it compiled — an Intel
// Mac holding the arm64 binaries, this Linux host holding the macOS binaries — adopts nothing, and it is not
// refused by a guess about what the host has installed: the binaries are run, the operating system answers,
// and the build's answer names the host that compiles them, the command that compiles them there, and the
// directory the ones this run compiled are sitting in. Nothing is recorded: binaries a host cannot run are
// binaries the release ships nothing about.
//
// The binaries here are the fixture shape the shape rules adopt, so the shape rules let them through to the
// run, and the run is where this host answers.
func TestAHostThatCannotRunTheBinariesIsToldWhichHostCompilesThem(t *testing.T) {
	for _, target := range []string{"macos-amd64", "macos-arm64"} {
		repo := t.TempDir()

		paths := ffmpegPathsFor(repo, bundle.Manifest{FFmpeg: "9.0.2"}, target)

		if err := os.MkdirAll(paths.bundle, 0755); err != nil {
			t.Fatalf("%s: making the bundle directory: %v", target, err)
		}

		for _, name := range []string{"ffmpeg", "ffprobe"} {
			if err := os.WriteFile(suppliedDropFor(paths, target)[name], aSuppliedProgramBytes(target), 0755); err != nil {
				t.Fatalf("%s: writing the %s: %v", target, name, err)
			}
		}

		tl := tool{repo: repo, theRun: onThisMachine(), errout: io.Discard}

		_, err := tl.proveSuppliedBinaries(target, paths, io.Discard)

		if err == nil {
			t.Fatalf("%s: the run adopted binaries this %s host cannot execute", target, runtime.GOOS)
		}

		for _, want := range []string{
			target,
			"go run ./cmd/build ffmpeg " + target,
			"a " + target + " host",
			paths.bundle,
			// The operating system's own refusal, carried out of the run: the binaries never started,
			// and the reason is the answer the operator needs.
			"fork/exec",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: the failure %q does not name %q", target, err.Error(), want)
			}
		}

		if aFileIsThere(theProofFileInTheBuildTree(repo, target)) {
			t.Errorf("%s: the run recorded a proof of binaries it could not run", target)
		}
	}
}

// theProofFileInTheBuildTree is where this build used to keep a proof of one target's binaries. The tests
// name the path to say the build writes nothing there.
func theProofFileInTheBuildTree(repo string, target string) string {
	return filepath.Join(repo, ".build", "ffmpeg", "checks", target+".json")
}

// aDropInTheBundle lays the binaries a maintainer dropped into one target's bundle directory, with
// nothing recorded about them: no proof file, no digest, no pin.
func aDropInTheBundle(t *testing.T, repo string, target string) ffmpegPaths {
	t.Helper()

	dir := filepath.Join(repo, "internal", "infrastructure", "ffmpeg", "bundle", target)

	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("making the bundle directory: %v", err)
	}

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		body := aSuppliedProgramBytes(target)

		if err := os.WriteFile(filepath.Join(dir, name+programSuffix(target)), body, 0755); err != nil {
			t.Fatalf("dropping the %s: %v", name, err)
		}
	}

	return ffmpegPathsFor(repo, bundle.ReadManifest(), target)
}

// aHostRunningTheBinaries answers the run's commands the way a working build answers them, and says
// which release it is when the build asks it what it is.
type aHostRunningTheBinaries struct {
	aStandIn
}

func (s *aHostRunningTheBinaries) run(argv []string, stdin []byte) ([]byte, error) {
	for _, arg := range argv {
		if arg == "-version" {
			s.runs = append(s.runs, strings.Join(argv, " "))

			return []byte("ffmpeg version 9.0.2 Copyright (c) 2000-2025 the FFmpeg developers\n"), nil
		}
	}

	return s.aStandIn.run(argv, stdin)
}

// aBuildOnAHost is the build standing on the host that runs one target's binaries, running them
// through the stand-in that answers like a working build.
func aBuildOnAHost(t *testing.T, repo string, target string, theHost host, theRun command) *tool {
	t.Helper()

	return &tool{
		repo:    repo,
		machine: func() host { return theHost },
		theRun:  theRun,
		errout:  io.Discard,
	}
}

// The run of the ffmpeg binaries is the gate, and it is a run: it prints what it saw and keeps nothing.
// No proof file is left for a later build to read, and no digest is printed for a manifest to pin.
func TestTheRunOfTheBinariesRecordsNothing(t *testing.T) {
	repo := t.TempDir()

	paths := aDropInTheBundle(t, repo, "macos-arm64")

	stand := &aHostRunningTheBinaries{}

	tl := aBuildOnAHost(t, repo, "macos-arm64", host{goos: "darwin", goarch: "arm64"}, stand.run)

	out := &bytes.Buffer{}

	run, err := tl.proveSuppliedBinaries("macos-arm64", paths, out)
	if err != nil {
		t.Fatalf("the run of the binaries the host compiled: %v", err)
	}

	if !strings.Contains(run.Version, "9.0.2") {
		t.Errorf("the run records %q as the version the binaries said they are", run.Version)
	}

	if len(run.Codecs.Audio) == 0 {
		t.Error("the run records no codecs")
	}

	if run.Conversions == 0 {
		t.Error("the run records no conversions")
	}

	if run.Host != "darwin-arm64" {
		t.Errorf("the run names %s as the machine that ran the binaries, want the host it stood on", run.Host)
	}

	if aFileIsThere(theProofFileInTheBuildTree(repo, "macos-arm64")) {
		t.Errorf("the run recorded a proof file at %s", theProofFileInTheBuildTree(repo, "macos-arm64"))
	}

	if strings.Contains(out.String(), "sha256") {
		t.Errorf("the run printed digests for a manifest to pin:\n%s", out.String())
	}

	if !strings.Contains(out.String(), "check") {
		t.Errorf("the run printed no summary of what it saw:\n%s", out.String())
	}
}

// Binaries that register a video codec are refused by the run that asked them, on the host that
// compiled them: this project ships an ffmpeg compiled for audio.
func TestTheRunRefusesBinariesThatRegisterVideoCodecs(t *testing.T) {
	repo := t.TempDir()

	paths := aDropInTheBundle(t, repo, "macos-arm64")

	stand := &aHostRunningTheBinaries{}
	stand.codecs = append(theAudioCodecLines, "DEVIL. h264                 H.264 / AVC / MPEG-4 AVC / MPEG-4 part 10")

	tl := aBuildOnAHost(t, repo, "macos-arm64", host{goos: "darwin", goarch: "arm64"}, stand.run)

	_, err := tl.proveSuppliedBinaries("macos-arm64", paths, &bytes.Buffer{})
	if err == nil {
		t.Fatal("the run adopted binaries that register a video codec")
	}

	if !strings.Contains(err.Error(), "h264") {
		t.Errorf("the failure %q does not name the video codec the binaries register", err.Error())
	}

	if aFileIsThere(theProofFileInTheBuildTree(repo, "macos-arm64")) {
		t.Errorf("a proof file was recorded for binaries that were refused: %s", theProofFileInTheBuildTree(repo, "macos-arm64"))
	}
}

// Binaries that convert nothing are refused by the run, and the failure carries the operating system's
// own refusal when the host cannot start them at all.
func TestTheRunRefusesBinariesThatDoNotConvert(t *testing.T) {
	repo := t.TempDir()

	paths := aDropInTheBundle(t, repo, "macos-arm64")

	tl := aBuildOnAHost(t, repo, "macos-arm64", host{goos: "darwin", goarch: "arm64"}, aCheckThatRefuses("the binaries said no"))

	_, err := tl.proveSuppliedBinaries("macos-arm64", paths, &bytes.Buffer{})
	if err == nil {
		t.Fatal("the run adopted binaries that convert nothing")
	}

	if !strings.Contains(err.Error(), "the binaries said no") {
		t.Errorf("the failure %q does not carry what the binaries answered with", err.Error())
	}

	if aFileIsThere(theProofFileInTheBuildTree(repo, "macos-arm64")) {
		t.Errorf("a proof file was recorded for binaries that were refused: %s", theProofFileInTheBuildTree(repo, "macos-arm64"))
	}
}

// theManifestFile is the manifest this repository tracks, copied into a tree the build may write a pin
// into. The test works on the file as it is written, because the rule is which bytes of it move.
func theManifestFile(t *testing.T, repo string) []byte {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("..", "..", "internal", "infrastructure", "ffmpeg", "bundle", "manifest.json"))
	if err != nil {
		t.Fatalf("the manifest this repository tracks cannot be read: %v", err)
	}

	dir := filepath.Join(repo, "internal", "infrastructure", "ffmpeg", "bundle")

	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("the tree cannot hold a manifest: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), body, 0644); err != nil {
		t.Fatalf("the manifest cannot be put where the build writes its pin: %v", err)
	}

	return body
}

// The header a program carries is the promise its reader keeps. A Mach-O header names its load commands
// twice — the count of them at offset 16, and the size they occupy at offset 20, two different numbers. A
// universal binary's magic is big-endian on disk, whatever machine produced it. A fixture that writes the
// bytes its own reader happens to look at, instead of the bytes a linker writes, proves the reader agrees
// with itself and proves nothing about a program: that is how a program compiled on a Mac came to be refused
// as a header whose load commands do not fit inside what it describes. This test reads the fixtures' own
// bytes back at the published offsets.
func TestTheFixtureProgramsAreLaidOutTheWayLinkersLayThemOut(t *testing.T) {
	t.Run("the Mach-O header names its load commands twice", func(t *testing.T) {
		fixture := aSystemProgram(theArm64CPU)

		bytes := machOBytes(fixture)

		count := len(fixture.Dylibs)
		size := 0

		for _, dylib := range fixture.Dylibs {
			size += len(aDylibCommand(dylib))
		}

		if fixture.Signature {
			count++
			size += len(aCodeSignatureCommand())
		}

		if got := binary.LittleEndian.Uint32(bytes[16:]); got != uint32(count) {
			t.Errorf("the header names %d load commands at offset 16, where the count lives; want %d", got, count)
		}

		if got := binary.LittleEndian.Uint32(bytes[20:]); got != uint32(size) {
			t.Errorf("the header says its load commands occupy %d bytes at offset 20, where the size lives; want %d", got, size)
		}

		if count == size {
			t.Errorf("the count of commands (%d) and the size they occupy (%d) are the same number: a fixture that writes one number in both places lets a reader that reads the count for the size pass", count, size)
		}

		// Walking the commands by the size each one names has to land exactly on the end of the area the
		// header describes: that is the walk the build performs on a program it has been handed.
		walked := 0

		for walked < size {
			walked += int(binary.LittleEndian.Uint32(bytes[machOHeaderSize+walked+4:]))
		}

		if walked != size {
			t.Errorf("the load commands walked to %d bytes, and the header describes %d", walked, size)
		}
	})

	t.Run("a universal binary's magic is big-endian on disk", func(t *testing.T) {
		bytes := machOBytes(aMachO{Magic: theFatMagic, CPUType: theArm64CPU, Signature: true})

		if got := binary.BigEndian.Uint32(bytes[0:]); got != theFatMagic {
			t.Errorf("the fixture writes the universal magic as the bytes %x, want ca fe ba be", bytes[0:4])
		}
	})
}
