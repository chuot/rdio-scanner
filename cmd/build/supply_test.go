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

	theExecFileType = 2 // MH_EXECUTE: a program the operating system runs, which is what a pair is

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

// aSystemProgram is the pair of fixtures every rule is measured against: a thin 64-bit image of the
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
// architecture's pair. A pair built for the other macOS architecture is a pair that will not run.
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
// and each one carries its own pair, so a fat drop says it was built for a matrix the build does not
// have, and the pair the archive carries is not the pair its architecture runs.
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

// The pair is embedded, gzipped, written out at startup and executed. A file that is not an executable
// program of the target's operating system is not a pair at all: it is a drop that went wrong, and the
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

// The ELF numbers the FreeBSD rule is written in. FreeBSD links statically with its own libc, and these
// are the numbers its linker writes.
const (
	theELFMagic      = "\x7fELF"
	theELF64Class    = 2
	theELF32Class    = 1
	theFreeBSDOSABI  = 9
	theLinuxOSABI    = 3
	theNoneOSABI     = 0
	theX8664Machine  = 0x3e
	theI386Machine   = 0x03
	theExecType      = 2
	theLoadSegment   = 1
	theInterpSegment = 3
)

// aFreeBSDProgram is an ELF header and the program headers a test cares about. Raw, when set, is written
// alone: a drop that is not an ELF at all.
type aFreeBSDProgram struct {
	Class    byte
	OSABI    byte
	Machine  uint16
	Type     uint16
	Segments []uint32
	Mode     os.FileMode
	Raw      []byte
}

func writeELF(t *testing.T, name string, fixture aFreeBSDProgram) string {
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

	if err := os.WriteFile(path, elfBytes(fixture), mode); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	return path
}

// elfBytes is the header and the program headers a fixture describes, laid out the way an ELF linker
// lays them out. The identification area after the magic is the class, the byte order, the version, the
// brand, the ABI version, and padding: the class is the fifth byte of the header and the brand the
// eighth. The program header offset, the size of one entry, and the count of them are the words at
// offsets 32, 54 and 56 of a 64-bit header.
func elfBytes(fixture aFreeBSDProgram) []byte {
	header := make([]byte, 64)
	copy(header[0:], theELFMagic)
	header[4] = fixture.Class
	header[5] = 1 // EI_DATA: little-endian, which is both architectures this project ships for FreeBSD
	header[6] = 1 // EI_VERSION
	header[7] = fixture.OSABI
	binary.LittleEndian.PutUint16(header[16:], fixture.Type)
	binary.LittleEndian.PutUint16(header[18:], fixture.Machine)
	binary.LittleEndian.PutUint64(header[32:], 64) // e_phoff: the program headers follow the header
	binary.LittleEndian.PutUint16(header[52:], 64) // e_ehsize
	binary.LittleEndian.PutUint16(header[54:], 56) // e_phentsize
	binary.LittleEndian.PutUint16(header[56:], uint16(len(fixture.Segments)))

	body := append([]byte{}, header...)

	for _, segment := range fixture.Segments {
		entry := make([]byte, 56)
		binary.LittleEndian.PutUint32(entry[0:], segment)

		body = append(body, entry...)
	}

	return body
}

// aStaticFreeBSDProgram is the fixture every FreeBSD rule is measured against: a 64-bit x86-64
// executable branded FreeBSD, with no program interpreter, which is what needs nothing installed on the
// host it runs on.
func aStaticFreeBSDProgram() aFreeBSDProgram {

	return aFreeBSDProgram{
		Class:    theELF64Class,
		OSABI:    theFreeBSDOSABI,
		Machine:  theX8664Machine,
		Type:     theExecType,
		Segments: []uint32{theLoadSegment},
	}
}

// aSuppliedProgramBytes is the program a maintainer drops for one target: the shape the build adopts.
func aSuppliedProgramBytes(target string) []byte {
	switch target {
	case "macos-amd64":
		return machOBytes(aSystemProgram(theX8664CPU))
	case "macos-arm64":
		return machOBytes(aSystemProgram(theArm64CPU))
	case "freebsd-amd64":
		return elfBytes(aStaticFreeBSDProgram())
	}

	panic("no supplied program fixture for " + target)
}

// The proof is the only evidence a supplied pair converts audio: the build cannot run it. So the proof
// names the pair it ran, the target it ran for, and the machine it ran on, and a proof that does not match
// the pair being adopted is not a proof at all.
func TestTheFfmpegCheckProofNamesThePairItRan(t *testing.T) {
	repo := t.TempDir()

	ffmpegDigest := strings.Repeat("a", 64)
	ffprobeDigest := strings.Repeat("b", 64)

	record := aCheckFor("macos-arm64", ffmpegDigest, ffprobeDigest)

	if err := writeFfmpegCheck(repo, record); err != nil {
		t.Fatalf("writeFfmpegCheck: %v", err)
	}

	got, err := readFfmpegCheck(repo, "macos-arm64", ffmpegDigest, ffprobeDigest)
	if err != nil {
		t.Fatalf("the proof the check recorded for its own pair was refused: %v", err)
	}

	if got.Target != "macos-arm64" {
		t.Errorf("the proof names %s, want macos-arm64", got.Target)
	}

	if got.Host != record.Host {
		t.Errorf("the proof names the host %s, want %s", got.Host, record.Host)
	}

	if got.FFmpeg != ffmpegDigest || got.FFProbe != ffprobeDigest {
		t.Errorf("the proof names the pair %s / %s, want the digests it was recorded with", got.FFmpeg, got.FFProbe)
	}

	if len(got.Conversions) != 6 {
		t.Errorf("the proof records %d conversions, want the six the check runs: %+v", len(got.Conversions), got.Conversions)
	}

	if got.Checked == "" || got.Version == "" {
		t.Errorf("the proof names no ffmpeg version %q and no check time %q", got.Version, got.Checked)
	}
}

// A proof is only worth what it proves about the pair in front of the build. A proof for another pair,
// another target, another platform, or none at all, is a pair the build ships without evidence that it
// converts anything, which is the promise the compiled targets do not make.
func TestTheBuildRefusesAPairTheFfmpegCheckDidNotProve(t *testing.T) {
	for _, tc := range []struct {
		name   string
		record func(repo string)
		rule   string
	}{
		{
			name:   "no proof at all",
			record: func(repo string) {},
			rule:   "ffmpeg-check",
		},
		{
			name: "a proof recorded for the other macOS architecture",
			record: func(repo string) {
				writeFfmpegCheck(repo, aCheckFor("macos-amd64", theFFmpegDigest, theFFProbeDigest))
			},
			rule: "macos-amd64",
		},
		{
			name: "a proof recorded for a pair that has since been rebuilt",
			record: func(repo string) {
				writeFfmpegCheck(repo, aCheckFor("macos-arm64", strings.Repeat("9", 64), theFFProbeDigest))
			},
			rule: "digest",
		},
		{
			name: "a proof that is not readable",
			record: func(repo string) {
				if err := os.MkdirAll(filepath.Dir(theCheckFile(repo, "macos-arm64")), 0755); err != nil {
					t.Fatalf("making the checks directory: %v", err)
				}

				if err := os.WriteFile(theCheckFile(repo, "macos-arm64"), []byte("the maintainer says it works"), 0644); err != nil {
					t.Fatalf("writing the proof: %v", err)
				}
			},
			rule: "check",
		},
		{
			name: "a proof recorded on another platform",
			record: func(repo string) {
				record := aCheckFor("macos-arm64", theFFmpegDigest, theFFProbeDigest)
				record.Host = "linux-amd64"

				writeFfmpegCheck(repo, record)
			},
			rule: "host",
		},
	} {
		repo := t.TempDir()
		tc.record(repo)

		_, err := readFfmpegCheck(repo, "macos-arm64", theFFmpegDigest, theFFProbeDigest)
		if err == nil {
			t.Errorf("%s: the build adopted a pair with %s", tc.name, tc.name)
			continue
		}

		if !strings.Contains(err.Error(), tc.rule) {
			t.Errorf("%s: the failure %q does not name the %s it is missing", tc.name, err.Error(), tc.rule)
		}
	}
}

// The proof of a macOS pair is valid from a macOS host of either architecture, and the proof file keeps
// naming the host that recorded it. An Apple Silicon Mac proving the Intel pair through Rosetta 2 records
// the proof of the program the Intel machine runs, and the release record says which machine proved it. A
// proof from a host of another operating system is the proof of a pair that host cannot run at all.
func TestTheProofOfAMacOSTargetIsRecordedByAMacOSHostOfEitherArchitecture(t *testing.T) {
	for _, tc := range []struct {
		name, host string
		adopted    bool
	}{
		{name: "the other macOS architecture", host: "darwin-arm64", adopted: true},
		{name: "the target's own architecture", host: "darwin-amd64", adopted: true},
		{name: "a Linux host", host: "linux-amd64", adopted: false},
		{name: "a FreeBSD host", host: "freebsd-amd64", adopted: false},
		{name: "a Windows host", host: "windows-amd64", adopted: false},
	} {
		repo := t.TempDir()

		record := aCheckFor("macos-amd64", theFFmpegDigest, theFFProbeDigest)
		record.Host = tc.host

		if err := writeFfmpegCheck(repo, record); err != nil {
			t.Fatalf("%s: writeFfmpegCheck: %v", tc.name, err)
		}

		got, err := readFfmpegCheck(repo, "macos-amd64", theFFmpegDigest, theFFProbeDigest)

		if tc.adopted {
			if err != nil {
				t.Errorf("%s: the build refused a proof of macos-amd64 recorded on %s: %v", tc.name, tc.host, err)
			}

			if got.Host != tc.host {
				t.Errorf("%s: the proof adopted names the host %s, want %s: the proof file says which machine proved the pair", tc.name, got.Host, tc.host)
			}

			continue
		}

		if err == nil {
			t.Errorf("%s: the build adopted a pair proved on %s, a host that cannot run the pair", tc.name, tc.host)
		}
	}
}

// The proof is the pair's own execution. A host that cannot execute the pair it is asked to prove — an
// Intel Mac holding the arm64 pair, this Linux host holding a macOS pair — has no proof to record, and it is
// not refused by a guess about what the host has installed: the pair is run, the operating system answers,
// and the build's answer names the host that can prove the pair, the command that proves it, and the
// directory the pair is sitting in. Nothing is recorded and nothing is pinned: a proof of a pair the host
// cannot run is a proof of nothing, and the release would have no way to tell it from a pair that converts.
//
// The pair here is the fixture shape FEAT-033's shape rules adopt, so the shape rules let it through to the
// run, and the run is where this host answers.
func TestAHostThatCannotRunThePairIsToldWhichHostCanProveIt(t *testing.T) {
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

		tl := tool{repo: repo}

		_, err := tl.proveSuppliedPair(target, paths, io.Discard)

		if err == nil {
			t.Fatalf("%s: the proof recorded a pair this %s host cannot execute", target, runtime.GOOS)
		}

		for _, want := range []string{
			target,
			"go run ./cmd/build ffmpeg-check " + target,
			"a " + target + " host",
			paths.bundle,
			// The operating system's own refusal, carried out of the run: the pair never started, and the
			// reason is the answer the operator needs.
			"fork/exec",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: the failure %q does not name %q", target, err.Error(), want)
			}
		}

		if _, err := os.Stat(theCheckFile(repo, target)); err == nil {
			t.Errorf("%s: the run recorded a proof for a pair it could not run", target)
		}
	}
}

// The proof is the evidence a supplied pair is adopted on, and the host that compiled the pair is the
// only machine that can record it. A proof naming video codecs is that host telling the release it
// compiled a program this project does not ship; a proof recorded by a build that never asked what the
// pair registers is the proof of a pair nobody looked at.
func TestTheBuildRefusesAProofThatNamesVideoCodecs(t *testing.T) {
	repo := t.TempDir()

	record := aCheckFor("macos-arm64", theFFmpegDigest, theFFProbeDigest)
	record.Codecs = codecRegistry{Audio: []string{"aac"}, Video: []string{"h264", "mpeg4"}}

	if err := writeFfmpegCheck(repo, record); err != nil {
		t.Fatalf("writeFfmpegCheck: %v", err)
	}

	_, err := readFfmpegCheck(repo, "macos-arm64", theFFmpegDigest, theFFProbeDigest)
	if err == nil {
		t.Fatal("the build adopted a pair whose proof names video codecs")
	}

	for _, want := range []string{"h264", "mpeg4"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure %q does not name the video codec %s the proof recorded", err.Error(), want)
		}
	}
}

func TestTheBuildRefusesAProofThatNamesNoCodecs(t *testing.T) {
	repo := t.TempDir()

	record := aCheckFor("macos-arm64", theFFmpegDigest, theFFProbeDigest)
	record.Codecs = codecRegistry{}

	if err := writeFfmpegCheck(repo, record); err != nil {
		t.Fatalf("writeFfmpegCheck: %v", err)
	}

	_, err := readFfmpegCheck(repo, "macos-arm64", theFFmpegDigest, theFFProbeDigest)
	if err == nil {
		t.Fatal("the build adopted a pair whose proof names no codecs")
	}

	if !strings.Contains(err.Error(), "ffmpeg-check") {
		t.Errorf("the failure %q does not name the command that records what the pair registers", err.Error())
	}
}

// The check proves the pair a maintainer drops. A target this build compiles an ffmpeg for is proved by
// the compile itself, inside the container that can run it, and a check pointed at it would be looking for
// a raw pair that directory has never held.
func TestTheFfmpegCheckRefusesATargetThisBuildCompilesAnFfmpegFor(t *testing.T) {
	tl := tool{repo: t.TempDir()}

	err := tl.runFFmpegCheck(options{target: "linux-amd64"}, io.Discard)
	if err == nil {
		t.Fatal("the check accepted a target whose pair this build compiles in a container")
	}

	if !strings.Contains(err.Error(), "linux-amd64") || !strings.Contains(err.Error(), "go run ./cmd/build ffmpeg linux-amd64") {
		t.Errorf("the failure %q does not name the target and the command that proves it", err.Error())
	}
}

// The check runs the pair, so it runs where the pair runs. Asked for a target this machine is not, it
// names the machine to run it on: a check that quietly skipped the pair it cannot execute would be a
// proof of nothing, and the build would have no way to tell.
func TestTheFfmpegCheckRefusesAHostThatIsNotItsTargets(t *testing.T) {
	if err := checkFfmpegCheckHost("linux-amd64", thisMachine()); err != nil {
		t.Errorf("the check refused the host it is standing on: %v", err)
	}

	for _, target := range []string{"macos-amd64", "macos-arm64", "freebsd-amd64"} {
		err := checkFfmpegCheckHost(target, thisMachine())
		if err == nil {
			t.Errorf("the check accepted %s on a machine that is not a %s host", target, target)
			continue
		}

		if !strings.Contains(err.Error(), target) {
			t.Errorf("the failure %q does not name the %s host to run it on", err.Error(), target)
		}
	}

	// A platform outside the release matrix has no pair to prove, and the check is the proof of a pair.
	if err := checkFfmpegCheckHost("netbsd-amd64", thisMachine()); err == nil {
		t.Error("the check accepted a platform this project releases no archive for")
	} else if !strings.Contains(err.Error(), "netbsd-amd64") {
		t.Errorf("the failure %q does not name the platform it refused", err.Error())
	}
}

// The rule the check enforces is the operating system, not the architecture inside it. A macOS pair is
// proved on a macOS host of either architecture: an Apple Silicon Mac executes an Intel program through
// Rosetta 2, and the proof is the pair's own execution, so the proof an Apple Silicon Mac records for the
// Intel pair is the proof of the program the Intel machine runs. The proof file keeps naming the host that
// recorded it, which is what says which machine proved the pair.
//
// A host that cannot execute the pair it is asked to prove is not this rule's business: the pair is run,
// and the operating system answers. FreeBSD is one architecture in this release, and its pair is proved on
// FreeBSD.
func TestTheFfmpegCheckOfAMacOSTargetRunsOnEitherMacOSArchitecture(t *testing.T) {
	for _, tc := range []struct {
		target  string
		machine host
		runs    bool
	}{
		{target: "macos-amd64", machine: host{goos: "darwin", goarch: "arm64"}, runs: true},
		{target: "macos-arm64", machine: host{goos: "darwin", goarch: "amd64"}, runs: true},
		{target: "macos-amd64", machine: host{goos: "darwin", goarch: "amd64"}, runs: true},
		{target: "macos-arm64", machine: host{goos: "darwin", goarch: "arm64"}, runs: true},
		{target: "freebsd-amd64", machine: host{goos: "freebsd", goarch: "amd64"}, runs: true},
		{target: "linux-amd64", machine: host{goos: "linux", goarch: "amd64"}, runs: true},
		{target: "macos-amd64", machine: host{goos: "linux", goarch: "amd64"}, runs: false},
		{target: "macos-arm64", machine: host{goos: "freebsd", goarch: "amd64"}, runs: false},
		{target: "freebsd-amd64", machine: host{goos: "darwin", goarch: "arm64"}, runs: false},
		{target: "freebsd-amd64", machine: host{goos: "linux", goarch: "amd64"}, runs: false},
	} {
		err := checkFfmpegCheckHost(tc.target, tc.machine)

		if tc.runs {
			if err == nil {
				continue
			}

			t.Errorf("%s on a %s/%s host: the check refused a host that runs the pair: %v", tc.target, tc.machine.goos, tc.machine.goarch, err)
			continue
		}

		if err == nil {
			t.Errorf("%s on a %s/%s host: the check accepted a host of another operating system, which cannot run the pair at all", tc.target, tc.machine.goos, tc.machine.goarch)
			continue
		}

		if !strings.Contains(err.Error(), tc.target) {
			t.Errorf("the failure %q does not name the %s host to run it on", err.Error(), tc.target)
		}
	}
}

// theCheckFile is where the proof for one target is kept, and theFFmpegDigest and theFFProbeDigest are
// the pair a test is adopting, so a test can change one of them and say which one the proof named.
func theCheckFile(repo string, target string) string {
	return filepath.Join(repo, ".build", "ffmpeg", "checks", target+".json")
}

const (
	theFFmpegDigest  = "1111111111111111111111111111111111111111111111111111111111111111"
	theFFProbeDigest = "2222222222222222222222222222222222222222222222222222222222222222"
)

func aCheckFor(target string, ffmpegDigest string, ffprobeDigest string) anFfmpegCheck {
	return anFfmpegCheck{
		Target:  target,
		Host:    aHostForTarget(target),
		FFmpeg:  ffmpegDigest,
		FFProbe: ffprobeDigest,
		Version: "ffmpeg version 9.0.2 Copyright (c) 2000-2025 the FFmpeg developers",
		Checked: "2026-10-12T09:00:00Z",
		Codecs:  codecRegistry{Audio: theAudioCodecNames},
		Conversions: []production{
			{Input: "wav", Mode: "plain", Bytes: 7675, Duration: "2.128000"},
			{Input: "wav", Mode: "norm", Bytes: 12251, Duration: "3.010667"},
			{Input: "wav", Mode: "loudnorm", Bytes: 12131, Duration: "3.010667"},
			{Input: "mp3", Mode: "plain", Bytes: 7756, Duration: "2.128000"},
			{Input: "mp3", Mode: "norm", Bytes: 12251, Duration: "3.010667"},
			{Input: "mp3", Mode: "loudnorm", Bytes: 12131, Duration: "3.010667"},
		},
	}
}

func aHostForTarget(target string) string {
	goos := "linux"

	switch {
	case strings.HasPrefix(target, "macos-"):
		goos = "darwin"
	case strings.HasPrefix(target, "freebsd-"):
		goos = "freebsd"
	case strings.HasPrefix(target, "windows-"):
		goos = "windows"
	}

	goarch := strings.SplitN(target, "-", 2)[1]

	return goos + "-" + goarch
}

// FreeBSD is the release target this project ships whose pair is built on FreeBSD itself, and the bundle's
// rule for it is the rule the Linux targets have: the program the server writes out and runs needs
// nothing installed on the host.
func TestASuppliedFreeBSDProgramIsAStaticBinaryForItsTarget(t *testing.T) {
	path := writeELF(t, "ffmpeg", aStaticFreeBSDProgram())

	if err := checkSuppliedProgram(path, "freebsd-amd64"); err != nil {
		t.Errorf("a static x86-64 FreeBSD program was refused: %v", err)
	}
}

// The bundle carries the operating system's own brand in the header, and the build reads it: a pair
// linked against glibc is a pair that runs on Linux and not on FreeBSD, and a pair branded nothing is a
// pair nobody can say anything about.
func TestTheBuildRefusesASuppliedProgramThatIsNotFreeBSDs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		osabi byte
		rule  string
	}{
		{"a Linux program", theLinuxOSABI, "FreeBSD"},
		{"a program branded nothing", theNoneOSABI, "FreeBSD"},
	} {
		fixture := aStaticFreeBSDProgram()
		fixture.OSABI = tc.osabi

		path := writeELF(t, "ffmpeg", fixture)

		err := checkSuppliedProgram(path, "freebsd-amd64")
		if err == nil {
			t.Errorf("%s was adopted for freebsd-amd64", tc.name)
			continue
		}

		if !strings.Contains(err.Error(), tc.rule) {
			t.Errorf("the failure %q does not name the %s rule", err.Error(), tc.rule)
		}
	}
}

// The recipe the manifest records links FreeBSD statically, and that is what the compile produces. A
// program the maintainer built by hand with a program interpreter is his choice, the same as a macOS
// program linking a Homebrew library: the build adopts it, and the licence text names the host that
// compiled it.
func TestTheBuildAdoptsADynamicFreeBSDProgram(t *testing.T) {
	fixture := aStaticFreeBSDProgram()
	fixture.Segments = []uint32{theInterpSegment, theLoadSegment}

	path := writeELF(t, "ffprobe", fixture)

	if err := checkSuppliedProgram(path, "freebsd-amd64"); err != nil {
		t.Errorf("a program with a program interpreter was refused for freebsd-amd64: %v", err)
	}
}

// One archive per architecture, and the FreeBSD archive this project ships is the x86-64 one.
func TestTheBuildRefusesAFreeBSDProgramOfTheOtherWidthAndArchitecture(t *testing.T) {
	for _, tc := range []struct {
		name  string
		patch func(*aFreeBSDProgram)
		rule  string
	}{
		{
			name: "an i386 program",
			patch: func(f *aFreeBSDProgram) {
				f.Machine = theI386Machine
			},
			rule: "architecture",
		},
		{
			name: "a 32-bit program",
			patch: func(f *aFreeBSDProgram) {
				f.Class = theELF32Class
			},
			rule: "64-bit",
		},
		{
			name: "not an ELF at all",
			patch: func(f *aFreeBSDProgram) {
				f.Raw = []byte{0xcf, 0xfa, 0xed, 0xfe} // the Mach-O 64-bit magic, which is not an ELF
			},
			rule: "ELF",
		},
	} {
		fixture := aStaticFreeBSDProgram()
		tc.patch(&fixture)

		path := writeELF(t, "ffmpeg", fixture)

		err := checkSuppliedProgram(path, "freebsd-amd64")
		if err == nil {
			t.Errorf("%s was adopted for freebsd-amd64", tc.name)
			continue
		}

		if !strings.Contains(err.Error(), tc.rule) {
			t.Errorf("%s: the failure %q does not name the %s rule", tc.name, err.Error(), tc.rule)
		}
	}
}

// The manifest is the file the release machine reads, and the build writing a pin into it is the one
// write the build makes to a tracked file. The record it changes is the one record of the target the pair
// was compiled for: every other byte of the file is what the operator committed, including the shape the
// file is written in, because the operator reads this file and commits what they read of it.
func TestPinningASuppliedPairWritesOnlyThatTargetsRecord(t *testing.T) {
	repo := t.TempDir()

	original := theManifestFile(t, repo)

	check := aCheckFor("macos-arm64", theFFmpegDigest, theFFProbeDigest)

	var out bytes.Buffer

	manifest, err := pinSuppliedPair(repo, "macos-arm64", check, &out)
	if err != nil {
		t.Fatalf("the build could not pin the pair it proved: %v", err)
	}

	record := manifest.Targets["macos-arm64"]

	if record.Programs["ffmpeg"] != theFFmpegDigest || record.Programs["ffprobe"] != theFFProbeDigest {
		t.Errorf("the manifest the build returns pins %v", record.Programs)
	}

	body, err := os.ReadFile(filepath.Join(repo, "internal", "infrastructure", "ffmpeg", "bundle", "manifest.json"))
	if err != nil {
		t.Fatalf("the manifest the build pinned is not there to read: %v", err)
	}

	if mode, err := os.Stat(filepath.Join(repo, "internal", "infrastructure", "ffmpeg", "bundle", "manifest.json")); err != nil || mode.Mode().Perm() != 0644 {
		t.Errorf("the manifest is written mode %v, want 0644: the build writes a tracked file, and its mode is part of it", mode.Mode().Perm())
	}

	// The file is compared line by line, because the rule is about the bytes around the pins: a manifest
	// rewritten by a JSON encoder loses the order its records were written in and the shape of its
	// indentation, and the operator's next diff is the whole file.
	got := strings.Split(string(body), "\n")
	want := strings.Split(string(original), "\n")

	if len(got) != len(want) {
		t.Fatalf("the pinned manifest has %d lines and the file it was written into has %d: the pins were not written into the file, they replaced it", len(got), len(want))
	}

	differing := []string{}

	for i := range want {
		if got[i] != want[i] {
			differing = append(differing, got[i])
		}
	}

	if len(differing) != 2 {
		t.Fatalf("the pinned manifest differs from the file it was written into in %d lines (%v), want the two lines carrying the digests", len(differing), differing)
	}

	for _, line := range differing {
		if !strings.Contains(line, theFFmpegDigest) && !strings.Contains(line, theFFProbeDigest) {
			t.Errorf("the line %q changed and carries neither digest", line)
		}
	}

	says := out.String()

	for _, want := range []string{"macos-arm64", theFFmpegDigest, theFFProbeDigest} {
		if !strings.Contains(says, want) {
			t.Errorf("the run prints %q nowhere, and the operator copies the pins from what it prints", says)
		}
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
// twice — the count of them at offset 16, and the size they occupy at offset 20, two different numbers.
// An ELF program's class is the sixth byte of its identification area and its brand the ninth, with a
// padding byte between the magic and the class. A universal binary's magic is big-endian on disk,
// whatever machine produced it. A fixture that writes the bytes its own reader happens to look at,
// instead of the bytes a linker writes, proves the reader agrees with itself and proves nothing about a
// program: that is how a pair compiled on a Mac came to be refused as a header whose load commands do
// not fit inside what it describes, and how a FreeBSD program compiled on FreeBSD came to be refused as
// a 32-bit one. This test reads the fixtures' own bytes back at the published offsets.
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

	t.Run("the ELF identification area carries the class and the brand", func(t *testing.T) {
		bytes := elfBytes(aStaticFreeBSDProgram())

		if bytes[4] != theELF64Class {
			t.Errorf("the class is the byte %d at offset 4, want %d: the fifth byte of the header is the class", bytes[4], theELF64Class)
		}

		if bytes[5] != 1 {
			t.Errorf("the byte order is %d at offset 5, want 1: the sixth byte says how the words are ordered", bytes[5])
		}

		if bytes[6] != 1 {
			t.Errorf("the version is the byte %d at offset 6, want 1: the seventh byte is the version", bytes[6])
		}

		if bytes[7] != theFreeBSDOSABI {
			t.Errorf("the brand is the byte %d at offset 7, want %d: the eighth byte of the header is the brand", bytes[7], theFreeBSDOSABI)
		}

		if bytes[9] != 0 {
			t.Errorf("the padding at offset 9 holds %d: a program puts nothing in the bytes after its ABI version", bytes[9])
		}
	})
}
