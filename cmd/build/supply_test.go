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

// These tests pin what the build reads out of a Mach-O program
// without running it: the machine code the file holds and the load
// commands naming how it was linked and what starts it.

// The Mach-O numbers these rules are written in, from the pinned
// SDK headers and checked against real compiles.
const (
	theMachOMagic   = 0xfeedfacf // MH_MAGIC_64, a thin 64-bit image, little-endian
	theMachO32Magic = 0xfeedface // MH_MAGIC, a 32-bit image: no macOS deployment this project ships
	theFatMagic     = 0xcafebabe // a universal binary, holding more than one architecture

	theX8664CPU = 0x01000007 // CPU_TYPE_X86_64
	theArm64CPU = 0x0100000c // CPU_TYPE_ARM64

	theObjectFileType = 1 // MH_OBJECT: a file that was never linked
	theExecFileType   = 2 // MH_EXECUTE: a program the operating system runs, which is what these programs are
	theDylibFileType  = 6 // MH_DYLIB: a library, which the server writes out and cannot run

	theLoadDylib       = 0x0c       // LC_LOAD_DYLIB
	theLoadWeakDylib   = 0x80000018 // LC_LOAD_WEAK_DYLIB
	theReexportDylib   = 0x8000001f // LC_REEXPORT_DYLIB
	theLoadUpwardDylib = 0x80000023 // LC_LOAD_UPWARD_DYLIB
	theCodeSignature   = 0x1d       // LC_CODE_SIGNATURE
)

// aMachO is a Mach-O header and the load commands a test cares about,
// written to a file the build reads: a fixture admitting to the rules
// carries all of them, and a test removes just the one it asks about.
type aMachO struct {
	Magic     uint32
	CPUType   uint32
	FileType  uint32 // when zero, the executable file every one of these programs is
	Entry     bool   // LC_MAIN: the entry point Apple's loader will use
	Dylinker  string // LC_LOAD_DYLINKER: the dynamic linker the program names
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

// machOBytes lays out a fixture's header and load commands the way a
// Mach-O linker lays them out. The command count and the command
// size are different numbers living in different header words.
func machOBytes(fixture aMachO) []byte {
	commands := theFixtureCommands(fixture)

	size := 0

	for _, command := range commands {
		size += len(command)
	}

	fileType := fixture.FileType
	if fileType == 0 {
		fileType = theExecFileType
	}

	header := make([]byte, 32)
	putMachOMagic(header, fixture.Magic)
	binary.LittleEndian.PutUint32(header[4:], fixture.CPUType)
	binary.LittleEndian.PutUint32(header[12:], fileType)
	binary.LittleEndian.PutUint32(header[16:], uint32(len(commands)))
	binary.LittleEndian.PutUint32(header[20:], uint32(size))

	body := append([]byte{}, header...)

	for _, command := range commands {
		body = append(body, command...)
	}

	return append(body, fixture.Body...)
}

// theFixtureCommands are the load commands a fixture describes, in
// the order it describes them: an admitted program carries every one,
// and a fixture asking what one absent does leaves that one out.
func theFixtureCommands(fixture aMachO) [][]byte {
	commands := [][]byte{}

	for _, dylib := range fixture.Dylibs {
		commands = append(commands, aDylibCommand(dylib))
	}

	if fixture.Dylinker != "" {
		commands = append(commands, aDylinkerCommand(fixture.Dylinker))
	}

	if fixture.Entry {
		commands = append(commands, aCommandOf(theMainCommand, theMainCommandSize))
	}

	if fixture.Signature {
		commands = append(commands, aCodeSignatureCommand())
	}

	return commands
}

// putMachOMagic writes the magic as the image carries it on disk: a
// universal binary's is big-endian whatever produced it, a thin
// image's takes the image's own byte order, little-endian here.
func putMachOMagic(header []byte, magic uint32) {
	if magic == theFatMagic {
		binary.BigEndian.PutUint32(header[0:], magic)
		return
	}

	binary.LittleEndian.PutUint32(header[0:], magic)
}

// aDylibCommand is one library reference as a Mach-O linker writes
// it: number, size, the name's offset from the command's own start,
// timestamp and versions, then the name; the rule reads the name out
// of the offset the command names.
func aDylibCommand(dylib aDylib) []byte {
	command := make([]byte, theDylibNameStart+len(dylib.Path)+1)
	if len(command)%8 != 0 {
		command = append(command, make([]byte, 8-len(command)%8)...)
	}

	binary.LittleEndian.PutUint32(command[0:], dylib.Command)
	binary.LittleEndian.PutUint32(command[4:], uint32(len(command)))
	binary.LittleEndian.PutUint32(command[8:], uint32(theDylibNameStart))
	copy(command[theDylibNameStart:], dylib.Path)

	return command
}

func aCodeSignatureCommand() []byte {
	command := make([]byte, 16)

	binary.LittleEndian.PutUint32(command[0:], theCodeSignature)
	binary.LittleEndian.PutUint32(command[4:], uint32(len(command)))

	return command
}

// The command numbers a Mach-O linker writes beyond the ones the
// rules above are about; LC_MAIN is 24 bytes on both architectures,
// naming the entry point as an offset and a stack size.
const (
	theSegment64Command    = 0x19       // LC_SEGMENT_64, naming one segment of the program
	theSymtabCommand       = 0x02       // LC_SYMTAB
	theDysymtabCommand     = 0x0b       // LC_DYSYMTAB
	theLoadDylinkerCommand = 0x0e       // LC_LOAD_DYLINKER: the dynamic linker the program needs
	theUUIDCommand         = 0x1b       // LC_UUID
	theDyldInfoCommand     = 0x80000022 // LC_DYLD_INFO_ONLY (0x22 | LC_REQ_DYLD)
	theFunctionStarts      = 0x26       // LC_FUNCTION_STARTS
	theDataInCode          = 0x29       // LC_DATA_IN_CODE
	theBuildVersion        = 0x32       // LC_BUILD_VERSION
	theMainCommand         = 0x80000028 // LC_MAIN (0x28 | LC_REQ_DYLD)

	// A load command naming a string names it as an offset from the
	// command's own start; a library reference reserves the timestamp
	// and version words before its name, a dynamic linker reference does not.
	theDylinkerNameStart = 12
	theDylibNameStart    = 24
	theMainCommandSize   = 24
)

// aCommandOf is a load command of the size the linker writes for it:
// its number, that size, and filler in the rest.
func aCommandOf(command uint32, size int) []byte {
	bytes := make([]byte, size)

	binary.LittleEndian.PutUint32(bytes[0:], command)
	binary.LittleEndian.PutUint32(bytes[4:], uint32(size))

	return bytes
}

// aSegmentCommand is the command a Mach-O linker writes for one
// segment of a program, naming the segment, as long as the sections
// it lists are many: the counts and sizes vary with the program.
func aSegmentCommand(name string, size int) []byte {
	bytes := aCommandOf(theSegment64Command, size)

	copy(bytes[8:24], name)

	return bytes
}

// aDylinkerCommand names the dynamic linker a program needs, the way
// the linker writes it: the name starts after the number, size, and offset.
func aDylinkerCommand(path string) []byte {
	size := theDylinkerNameStart + len(path) + 1
	if size%8 != 0 {
		size += 8 - size%8
	}

	bytes := aCommandOf(theLoadDylinkerCommand, size)

	binary.LittleEndian.PutUint32(bytes[8:], uint32(theDylinkerNameStart))
	copy(bytes[theDylinkerNameStart:], path)

	return bytes
}

// theDarwinLinkersCommands are the load commands the linker in the
// container this build compiles a macOS ffmpeg in wrote into the
// pair it produced, in the order and sizes it wrote them.
func theDarwinLinkersCommands(cpuType uint32) [][]byte {
	commands := [][]byte{
		aSegmentCommand("__PAGEZERO", 72),
		aSegmentCommand("__TEXT", 712),
		aSegmentCommand("__DATA_CONST", 232),
		aSegmentCommand("__DATA", 392),
		aSegmentCommand("__LINKEDIT", 72),
		aCommandOf(theDyldInfoCommand, 48),
		aCommandOf(theSymtabCommand, 24),
		aCommandOf(theDysymtabCommand, 80),
		aDylinkerCommand("/usr/lib/dyld"),
		aCommandOf(theUUIDCommand, 24),
		aCommandOf(theBuildVersion, 32),
		aCommandOf(theMainCommand, theMainCommandSize),
		aDylibCommand(aDylib{Command: theLoadDylib, Path: "/usr/lib/libSystem.B.dylib"}),
		aDylibCommand(aDylib{Command: theLoadDylib, Path: "/System/Library/Frameworks/CoreFoundation.framework/Versions/A/CoreFoundation"}),
		aDylibCommand(aDylib{Command: theLoadDylib, Path: "/System/Library/Frameworks/CoreVideo.framework/Versions/A/CoreVideo"}),
		aDylibCommand(aDylib{Command: theLoadDylib, Path: "/System/Library/Frameworks/CoreMedia.framework/Versions/A/CoreMedia"}),
		aCommandOf(theFunctionStarts, 16),
		aCommandOf(theDataInCode, 16),
	}

	if cpuType == theArm64CPU {
		commands = append(commands, aCodeSignatureCommand())
	}

	return commands
}

// theProgramALinkerLaidOut is a Mach-O program carrying the load
// commands the test names, then the bytes a program's own sections occupy.
func theProgramALinkerLaidOut(cpuType uint32, commands [][]byte) []byte {
	size := 0

	for _, command := range commands {
		size += len(command)
	}

	header := make([]byte, 32) // the words a 64-bit Mach-O header is: magic, cpu, subtype, filetype, ncmds, sizeofcmds, flags, reserved
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

// theProgramALinkerProduced is the program the Darwin link produced, one architecture's worth.
func theProgramALinkerProduced(cpuType uint32) []byte {
	return theProgramALinkerLaidOut(cpuType, theDarwinLinkersCommands(cpuType))
}

// aSystemProgram is the program this build's Mach-O rules admit:
// a thin 64-bit image of the architecture the target runs, an
// executable file with an entry point, the dynamic linker, a
// reference to the system library, and on arm64 a code signature.
func aSystemProgram(cpuType uint32) aMachO {
	return aMachO{
		Magic:    theMachOMagic,
		CPUType:  cpuType,
		Entry:    true,
		Dylinker: "/usr/lib/dyld",
		Dylibs: []aDylib{
			{Command: theLoadDylib, Path: "/usr/lib/libSystem.B.dylib"},
			{Command: theLoadDylib, Path: "/usr/lib/libz.1.2.13.dylib"},
			{Command: theLoadWeakDylib, Path: "/usr/lib/libiconv.2.dylib"},
		},
		Signature: cpuType == theArm64CPU,
	}
}

// the build admits a supplied Mach-O only as the program its target runs: the
// file's own header and load commands say the architecture, the link, and
// what starts it, and a dropped pair is held to the same reads.
func TestASuppliedMachOIsTheProgramItsTargetRuns(t *testing.T) {
	manifest := bundle.ReadManifest()

	for _, tc := range []struct {
		name    string
		target  string
		fixture aMachO
	}{
		{
			// The Darwin link writes no code signature for x86-64, and the x86-64 target's record asks for none.
			name:    "an unsigned x86-64 program linking only the libraries the operating system provides",
			target:  "macos-amd64",
			fixture: aSystemProgram(theX8664CPU),
		},
		{
			name:    "the arm64 program carrying the code signature macOS starts an arm64 program under",
			target:  "macos-arm64",
			fixture: aSystemProgram(theArm64CPU),
		},
		{
			name:    "the program a real Darwin link laid out: eighteen commands, whose count and whose size are two different numbers",
			target:  "macos-amd64",
			fixture: aMachO{Raw: theProgramALinkerProduced(theX8664CPU)},
		},
		{
			name:    "the arm64 program the same link laid out: nineteen commands, the last of them the code signature",
			target:  "macos-arm64",
			fixture: aMachO{Raw: theProgramALinkerProduced(theArm64CPU)},
		},
		{
			// the recipe enables no external library, so the compile links only
			// what the operating system provides; a maintainer's own library is
			// his choice, and the licence text names the host that compiled it.
			name:    "a Homebrew library alongside the system library",
			target:  "macos-amd64",
			fixture: aProgramLinking(theX8664CPU, aDylib{Command: theLoadDylib, Path: "/opt/homebrew/opt/x264/lib/libx264.164.dylib"}),
		},
		{
			name:    "a library path resolved at run time",
			target:  "macos-amd64",
			fixture: aProgramLinking(theX8664CPU, aDylib{Command: theLoadDylib, Path: "@rpath/libavcodec.dylib"}),
		},
		{
			name:    "a weakly linked library outside the system",
			target:  "macos-amd64",
			fixture: aProgramLinking(theX8664CPU, aDylib{Command: theLoadWeakDylib, Path: "/usr/local/lib/libpng.dylib"}),
		},
		{
			name:    "a reexported library outside the system",
			target:  "macos-arm64",
			fixture: aProgramLinking(theArm64CPU, aDylib{Command: theReexportDylib, Path: "/opt/homebrew/opt/zstd/lib/libzstd.1.dylib"}),
		},
		{
			name:    "an upward-linked library outside the system",
			target:  "macos-amd64",
			fixture: aProgramLinking(theX8664CPU, aDylib{Command: theLoadUpwardDylib, Path: "/Users/maintainer/build/lib/libssl.dylib"}),
		},
	} {
		path := writeMachO(t, "ffmpeg", tc.fixture)

		record := manifest.Targets[tc.target]

		if err := checkSuppliedProgram(path, tc.target, record); err != nil {
			t.Errorf("%s refused %s: %v", tc.target, tc.name, err)
		}
	}
}

// aProgramLinking is the program the build's Mach-O rules admit, with one more library reference added to it.
func aProgramLinking(cpuType uint32, dylib aDylib) aMachO {
	fixture := aSystemProgram(cpuType)

	fixture.Dylibs = append(fixture.Dylibs, dylib)

	return fixture
}

// the build refuses a supplied program that is not the program its
// target runs, and each refusal is a shape a dropped program can
// have, read out of the bytes and never inferred from provenance.
func TestTheBuildRefusesASuppliedProgramThatIsNotTheProgramItsTargetRuns(t *testing.T) {
	manifest := bundle.ReadManifest()

	for _, tc := range []struct {
		name    string
		target  string
		fixture aMachO
		says    []string
	}{
		{
			name:    "the machine code the other macOS architecture runs",
			target:  "macos-amd64",
			fixture: aSystemProgram(theArm64CPU),
			says:    []string{"arm64", "architecture", "x86-64"},
		},
		{
			name:   "one file holding both macOS architectures",
			target: "macos-amd64",
			fixture: aMachO{Magic: theFatMagic, CPUType: theX8664CPU, Entry: true, Dylinker: "/usr/lib/dyld",
				Dylibs: []aDylib{{Command: theLoadDylib, Path: "/usr/lib/libSystem.B.dylib"}}},
			says: []string{"universal", "one program per architecture"},
		},
		{
			name:   "a 32-bit Mach-O program",
			target: "macos-amd64",
			fixture: aMachO{Magic: theMachO32Magic, CPUType: 7, Entry: true, Dylinker: "/usr/lib/dyld",
				Dylibs: []aDylib{{Command: theLoadDylib, Path: "/usr/lib/libSystem.B.dylib"}}},
			says: []string{"32-bit", "64-bit"},
		},
		{
			name:    "a file that was never linked",
			target:  "macos-amd64",
			fixture: anUnlinkedProgram(theX8664CPU),
			says:    []string{"MH_OBJECT", "not an executable file"},
		},
		{
			name:    "a library, which the server writes out and cannot run",
			target:  "macos-arm64",
			fixture: aProgramInTheFormOfALibrary(),
			says:    []string{"MH_DYLIB", "not an executable file"},
		},
		{
			name:    "a program with no entry point",
			target:  "macos-arm64",
			fixture: aProgramWithNoEntryPoint(),
			says:    []string{"LC_MAIN", "entry point"},
		},
		{
			name:    "a program that names no dynamic linker",
			target:  "macos-arm64",
			fixture: aProgramWithNoDynamicLinker(),
			says:    []string{"LC_LOAD_DYLINKER", "/usr/lib/dyld"},
		},
		{
			name:    "a program naming a dynamic linker other than the one macOS starts under",
			target:  "macos-arm64",
			fixture: aProgramNamingAnotherDynamicLinker(),
			says:    []string{"/usr/lib/dyld2", "/usr/lib/dyld"},
		},
		{
			name:    "a program holding an entry point and a dynamic linker and no library reference at all",
			target:  "macos-arm64",
			fixture: aFreestandingProgram(),
			says:    []string{"LC_LOAD_DYLIB", "libSystem.B.dylib", "freestanding"},
		},
		{
			name:    "a program linked only against libraries the operating system does not provide",
			target:  "macos-amd64",
			fixture: aProgramLinkedOutsideTheSystem(theX8664CPU),
			says:    []string{"libSystem.B.dylib", "/opt/homebrew/opt/x264/lib/libx264.164.dylib"},
		},
		{
			name:    "an arm64 program carrying no code signature",
			target:  "macos-arm64",
			fixture: anUnsignedArm64Program(),
			says:    []string{"LC_CODE_SIGNATURE", "arm64"},
		},
		{
			name:    "a program that is not a Mach-O program at all",
			target:  "macos-amd64",
			fixture: aMachO{Raw: aFileInTheShapeOfAnELFProgram()},
			says:    []string{"not a Mach-O program", "7f 45 4c 46"},
		},
		{
			name:    "a placeholder instead of a program",
			target:  "macos-amd64",
			fixture: aMachO{Raw: []byte{}},
			says:    []string{"empty"},
		},
		{
			name:   "a program dropped as a data file",
			target: "macos-amd64",
			fixture: func() aMachO {
				fixture := aSystemProgram(theX8664CPU)
				fixture.Mode = 0644

				return fixture
			}(),
			says: []string{"not executable"},
		},
		{
			name:    "a program cut off inside its own header",
			target:  "macos-amd64",
			fixture: aMachO{Raw: aProgramShorterThanItsOwnHeader(theX8664CPU)},
			says:    []string{"shorter than the header"},
		},
		{
			name:    "a header naming load commands the file does not have",
			target:  "macos-amd64",
			fixture: aMachO{Raw: aProgramDescribingMoreThanItHas(theX8664CPU)},
			says:    []string{"load commands", "the file is only"},
		},
		{
			name:    "load commands running past the area the header describes",
			target:  "macos-arm64",
			fixture: aMachO{Raw: aProgramNamingMoreCommandsThanItCarries(theArm64CPU)},
			says:    []string{"load commands", "past the area"},
		},
		{
			name:    "a load command reaching past the area the header describes",
			target:  "macos-amd64",
			fixture: aMachO{Raw: aProgramWhoseLastCommandRunsPastItsArea(theX8664CPU)},
			says:    []string{"load command", "ends at byte"},
		},
		{
			name:    "a load command shorter than the two words every command starts with",
			target:  "macos-amd64",
			fixture: aMachO{Raw: theProgramALinkerLaidOut(theX8664CPU, [][]byte{aCommandWhoseSizeIsTooSmall()})},
			says:    []string{"load command", "shorter than the 8 bytes"},
		},
		{
			name:   "a library reference naming its name outside the command that carries it",
			target: "macos-amd64",
			fixture: aMachO{Raw: theProgramALinkerLaidOut(theX8664CPU, [][]byte{
				aDylinkerCommand("/usr/lib/dyld"),
				aCommandNamingItsNameOutsideItself(),
				aCommandOf(theMainCommand, theMainCommandSize),
			})},
			says: []string{"library name", "bytes from its start"},
		},
	} {
		path := writeMachO(t, "ffmpeg", tc.fixture)

		record := manifest.Targets[tc.target]

		err := checkSuppliedProgram(path, tc.target, record)
		if err == nil {
			t.Fatalf("%s adopted a program that is not the program %s runs", tc.name, tc.target)
		}

		for _, say := range append(tc.says, "for "+tc.target) {
			if !strings.Contains(err.Error(), say) {
				t.Errorf("%s: the failure %q does not say %q", tc.name, err.Error(), say)
			}
		}
	}
}

// the Mach-O reads accept the pair the darwin compile produced: the
// same rules are said twice, to whoever dropped a program and in the
// voice the build uses about the pair it compiled and did not run.
func TestTheMachOReadsAcceptThePairTheDarwinCompileProduced(t *testing.T) {
	manifest := bundle.ReadManifest()

	for _, tc := range []struct {
		target  string
		cpuType uint32
	}{
		{target: "macos-amd64", cpuType: theX8664CPU},
		{target: "macos-arm64", cpuType: theArm64CPU},
	} {
		record := manifest.Targets[tc.target]

		dir := t.TempDir()

		for _, program := range []string{"ffmpeg", "ffprobe"} {
			path := aMachOProgramWrittenTo(t, dir, program, theProgramALinkerProduced(tc.cpuType))

			if err := readAMachOProgram(path, program, tc.target, record.Conformance.Reads); err != nil {
				t.Errorf("%s refused the %s its own compile produced: %v", tc.target, program, err)
			}
		}
	}
}

// each mach-o read refuses the one shape it was written to refuse,
// and a refusal names the read, what the file said instead, and the
// fact that nothing here was run to find it out.
func TestEveryMachOReadRefusesTheShapeItWasWrittenToRefuse(t *testing.T) {
	manifest := bundle.ReadManifest()

	record := manifest.Targets["macos-arm64"]

	for _, tc := range []struct {
		name    string
		fixture aMachO
		reads   []string
		says    []string
	}{
		{
			name:    "mh_execute refuses a file that was never linked",
			fixture: anUnlinkedProgram(theArm64CPU),
			says:    []string{"mh_execute", "MH_OBJECT", "not an executable file"},
		},
		{
			name:    "mh_execute refuses a library",
			fixture: aProgramInTheFormOfALibrary(),
			says:    []string{"mh_execute", "MH_DYLIB"},
		},
		{
			name:    "lc_main refuses a program with no entry point",
			fixture: aProgramWithNoEntryPoint(),
			says:    []string{"lc_main", "LC_MAIN", "entry point"},
		},
		{
			name:    "lc_load_dylinker refuses a program naming no dynamic linker",
			fixture: aProgramWithNoDynamicLinker(),
			says:    []string{"lc_load_dylinker", "LC_LOAD_DYLINKER", "/usr/lib/dyld"},
		},
		{
			name:    "lc_load_dylinker refuses a dynamic linker other than the one macOS starts under",
			fixture: aProgramNamingAnotherDynamicLinker(),
			says:    []string{"lc_load_dylinker", "/usr/lib/dyld2", "/usr/lib/dyld"},
		},
		{
			name:    "lc_load_libsystem refuses a freestanding program",
			fixture: aFreestandingProgram(),
			says:    []string{"lc_load_libsystem", "LC_LOAD_DYLIB", "freestanding"},
		},
		{
			name:    "lc_load_libsystem refuses a program linked only outside the system",
			fixture: aProgramLinkedOutsideTheSystem(theArm64CPU),
			says:    []string{"lc_load_libsystem", "libSystem.B.dylib"},
		},
		{
			name:    "lc_code_signature refuses an unsigned arm64 program",
			fixture: anUnsignedArm64Program(),
			says:    []string{"lc_code_signature", "LC_CODE_SIGNATURE", "arm64"},
		},
		{
			name: "a 32-bit program is refused before any read is asked",
			fixture: aMachO{Magic: theMachO32Magic, CPUType: theArm64CPU, Entry: true, Dylinker: "/usr/lib/dyld",
				Dylibs: []aDylib{{Command: theLoadDylib, Path: "/usr/lib/libSystem.B.dylib"}}},
			says: []string{"32-bit", "mh_execute lc_main lc_load_dylinker lc_load_libsystem lc_code_signature"},
		},
		{
			name: "a universal binary is refused before any read is asked",
			fixture: aMachO{Magic: theFatMagic, CPUType: theArm64CPU, Entry: true, Dylinker: "/usr/lib/dyld",
				Dylibs: []aDylib{{Command: theLoadDylib, Path: "/usr/lib/libSystem.B.dylib"}}},
			says: []string{"universal", "mh_execute lc_main lc_load_dylinker lc_load_libsystem lc_code_signature"},
		},
		{
			name:    "a program of another file format is refused before any read is asked",
			fixture: aMachO{Raw: aFileInTheShapeOfAnELFProgram()},
			says:    []string{"not a Mach-O program", "7f 45 4c 46"},
		},
		{
			name:    "a program cut off inside its own header is refused",
			fixture: aMachO{Raw: aProgramShorterThanItsOwnHeader(theArm64CPU)},
			says:    []string{"shorter than the header"},
		},
		{
			name:    "a header naming load commands the file does not have is refused",
			fixture: aMachO{Raw: aProgramDescribingMoreThanItHas(theArm64CPU)},
			says:    []string{"load commands", "the file is only"},
		},
		{
			name:    "load commands running past the area the header describes are refused",
			fixture: aMachO{Raw: aProgramNamingMoreCommandsThanItCarries(theArm64CPU)},
			says:    []string{"load commands", "past the area"},
		},
		{
			name:    "a load command reaching past the area the header describes is refused",
			fixture: aMachO{Raw: aProgramWhoseLastCommandRunsPastItsArea(theArm64CPU)},
			says:    []string{"load command", "ends at byte"},
		},
		{
			name:    "a load command shorter than the two words every command starts with is refused",
			fixture: aMachO{Raw: theProgramALinkerLaidOut(theArm64CPU, [][]byte{aCommandWhoseSizeIsTooSmall()})},
			says:    []string{"load command", "shorter than the 8 bytes"},
		},
		{
			name: "a library reference naming its name outside the command that carries it is refused",
			fixture: aMachO{Raw: theProgramALinkerLaidOut(theArm64CPU, [][]byte{
				aDylinkerCommand("/usr/lib/dyld"),
				aCommandNamingItsNameOutsideItself(),
				aCommandOf(theMainCommand, theMainCommandSize),
			})},
			says: []string{"library name", "bytes from its start"},
		},
		{
			name:    "a record naming a read this build has no rule for is refused",
			fixture: aSystemProgram(theArm64CPU),
			reads:   []string{"elf64"},
			says:    []string{"elf64", "no rule for that read"},
		},
	} {
		path := writeMachO(t, "ffmpeg", tc.fixture)

		reads := record.Conformance.Reads

		if tc.reads != nil {
			reads = tc.reads
		}

		err := readAMachOProgram(path, "ffmpeg", "macos-arm64", reads)
		if err == nil {
			t.Fatalf("%s: a program was read as the program macos-arm64 runs", tc.name)
		}

		for _, say := range append(tc.says, "runs no macos-arm64 program") {
			if !strings.Contains(err.Error(), say) {
				t.Errorf("%s: the failure %q does not say %q", tc.name, err.Error(), say)
			}
		}
	}
}

// The programs the rules are measured against, one thing short of the program a Darwin link produced.
func anUnlinkedProgram(cpuType uint32) aMachO {
	fixture := aSystemProgram(cpuType)
	fixture.FileType = theObjectFileType

	return fixture
}

func aProgramInTheFormOfALibrary() aMachO {
	fixture := aSystemProgram(theArm64CPU)
	fixture.FileType = theDylibFileType

	return fixture
}

func aProgramWithNoEntryPoint() aMachO {
	fixture := aSystemProgram(theArm64CPU)
	fixture.Entry = false

	return fixture
}

func aProgramWithNoDynamicLinker() aMachO {
	fixture := aSystemProgram(theArm64CPU)
	fixture.Dylinker = ""

	return fixture
}

func aProgramNamingAnotherDynamicLinker() aMachO {
	fixture := aSystemProgram(theArm64CPU)
	fixture.Dylinker = "/usr/lib/dyld2"

	return fixture
}

// aFreestandingProgram is the shape a compile linked against nothing
// produces: an entry point and a dynamic linker, and no library
// reference at all; the ffmpeg the recipe compiles is under the
// operating system's own library.
func aFreestandingProgram() aMachO {
	fixture := aSystemProgram(theArm64CPU)
	fixture.Dylibs = nil

	return fixture
}

// aProgramLinkedOutsideTheSystem links two libraries the operating
// system does not provide, and not the system library: the shape of
// a program somebody built with his own libraries only.
func aProgramLinkedOutsideTheSystem(cpuType uint32) aMachO {
	fixture := aSystemProgram(cpuType)
	fixture.Dylibs = []aDylib{
		{Command: theLoadDylib, Path: "/opt/homebrew/opt/x264/lib/libx264.164.dylib"},
		{Command: theLoadWeakDylib, Path: "/opt/homebrew/opt/zstd/lib/libzstd.1.dylib"},
	}

	return fixture
}

func anUnsignedArm64Program() aMachO {
	fixture := aSystemProgram(theArm64CPU)
	fixture.Signature = false

	return fixture
}

// aFileInTheShapeOfAnELFProgram is the file format the other half of
// this project's targets runs, whole-header long: the refusal names
// the four bytes that say the format, not that the file is short.
func aFileInTheShapeOfAnELFProgram() []byte {
	body := make([]byte, 64)

	copy(body, []byte("\x7fELF\x02\x01\x01\x09\x00\x00\x00\x00\x00\x00\x00\x00"))

	return body
}

// aProgramShorterThanItsOwnHeader is the laid-out program cut off
// before its header ends: a reader would read bytes it does not have.
func aProgramShorterThanItsOwnHeader(cpuType uint32) []byte {
	return theProgramALinkerProduced(cpuType)[:16]
}

// aProgramDescribingMoreThanItHas is that same program with the size
// its header names for its load commands grown past the file.
func aProgramDescribingMoreThanItHas(cpuType uint32) []byte {
	body := theProgramALinkerProduced(cpuType)

	binary.LittleEndian.PutUint32(body[20:], uint32(len(body))*2)

	return body
}

// aProgramNamingMoreCommandsThanItCarries is that same program with
// the count of its load commands grown past its described area.
func aProgramNamingMoreCommandsThanItCarries(cpuType uint32) []byte {
	body := theProgramALinkerProduced(cpuType)

	binary.LittleEndian.PutUint32(body[16:], uint32(len(body)))

	return body
}

// aProgramWhoseLastCommandRunsPastItsArea grows the size of the last
// load command past the area the header describes: a reader that
// checked only where commands begin would walk past that claim.
func aProgramWhoseLastCommandRunsPastItsArea(cpuType uint32) []byte {
	body := theProgramALinkerProduced(cpuType)

	end := machOHeaderSize + int(binary.LittleEndian.Uint32(body[20:]))

	at := machOHeaderSize

	for {
		size := int(binary.LittleEndian.Uint32(body[at+4:]))

		if at+size >= end {
			break
		}

		at += size
	}

	binary.LittleEndian.PutUint32(body[at+4:], uint32(end-at+24))

	return body
}

// aCommandWhoseSizeIsTooSmall names a size shorter than the two
// words every load command starts with; trusting it walks into the next.
func aCommandWhoseSizeIsTooSmall() []byte {
	command := aCommandOf(theSegment64Command, 16)

	binary.LittleEndian.PutUint32(command[4:], 4)

	return command
}

// aCommandNamingItsNameOutsideItself moves the system library's name
// offset past the end of the command carrying it: trusting an offset
// reads past the command it is reading.
func aCommandNamingItsNameOutsideItself() []byte {
	command := aDylibCommand(aDylib{Command: theLoadDylib, Path: "/usr/lib/libSystem.B.dylib"})

	binary.LittleEndian.PutUint32(command[8:], uint32(len(command)+8))

	return command
}

// aMachOProgramWrittenTo writes a program into a test directory,
// where the container this build compiles in leaves the pair it made.
func aMachOProgramWrittenTo(t *testing.T, dir string, name string, body []byte) string {
	t.Helper()

	path := filepath.Join(dir, name)

	if err := os.WriteFile(path, body, 0755); err != nil {
		t.Fatalf("writing the %s: %v", name, err)
	}

	return path
}

// theCommandsLinkingNothingTheSystemProvides are a real Darwin link's
// commands with the system library reference removed.
func theCommandsLinkingNothingTheSystemProvides(commands [][]byte) [][]byte {
	kept := [][]byte{}

	for _, command := range commands {
		if bytes.Contains(command, []byte("/usr/lib/libSystem.B.dylib")) {
			continue
		}

		kept = append(kept, command)
	}

	return kept
}

// a macos run reads the pair it compiled under the names its record
// carries and says what it read: no conformance container, nothing
// run, and the operator's line names the reads instead of
// conversions nobody made.
func TestAMacOSRunReadsThePairItCompiledAndSaysWhatItReads(t *testing.T) {
	repo := t.TempDir()

	tl, out := aBuildCompilingInAContainer(t, repo, "macos-arm64", func(t *testing.T, dir string, name string) {
		aMachOProgramWrittenTo(t, dir, name, theProgramALinkerProduced(theArm64CPU))
	})

	if _, err := tl.runFFmpeg(options{target: "macos-arm64"}, out); err != nil {
		t.Fatalf("the build refused the pair its own record accepts: %v\n%s", err, out.String())
	}

	said := out.String()

	if !strings.Contains(said, "compiled and not run") {
		t.Errorf("the compile step does not say the pair was compiled and not run:\n%s", said)
	}

	if !strings.Contains(said, "read") {
		t.Errorf("the run never said it read the pair it compiled:\n%s", said)
	}

	for _, read := range bundle.ReadManifest().Targets["macos-arm64"].Conformance.Reads {
		if !strings.Contains(said, read) {
			t.Errorf("the run does not say it read %s:\n%s", read, said)
		}
	}

	if strings.Contains(said, "conversions") {
		t.Errorf("the run reports conversions for a pair it was not allowed to run:\n%s", said)
	}

	if strings.Contains(said, "runner") {
		t.Errorf("the run prepared a conformance container for a target nothing runs:\n%s", said)
	}

	paths := ffmpegPathsFor(repo, bundle.ReadManifest(), "macos-arm64")

	for _, name := range []string{"ffmpeg.gz", "ffprobe.gz"} {
		if !aFileIsThere(paths.cachedProgram(name)) {
			t.Errorf("the read pair was not cached at %s", paths.cachedProgram(name))
		}

		if !aFileIsThere(filepath.Join(paths.bundle, name)) {
			t.Errorf("the read pair was not published into the bundle at %s", paths.bundle)
		}
	}

	if !aFileIsThere(filepath.Join(paths.bundle, "LICENCE.md")) {
		t.Errorf("the bundle carries no licence text for the pair the build read: %s", paths.bundle)
	}
}

// a pair the build's own read refused is cached, licensed, and
// published by nothing: a release carrying it would carry a program
// its own recipe did not compile.
func TestAMacOSRunKeepsNothingForAPairItsReadRefuses(t *testing.T) {
	repo := t.TempDir()

	tl, out := aBuildCompilingInAContainer(t, repo, "macos-arm64", func(t *testing.T, dir string, name string) {
		commands := theCommandsLinkingNothingTheSystemProvides(theDarwinLinkersCommands(theArm64CPU))

		aMachOProgramWrittenTo(t, dir, name, theProgramALinkerLaidOut(theArm64CPU, commands))
	})

	_, err := tl.runFFmpeg(options{target: "macos-arm64"}, out)
	if err == nil {
		t.Fatal("the run adopted a pair its own read refused")
	}

	if !strings.Contains(err.Error(), "lc_load_libsystem") {
		t.Errorf("the refusal %q does not name the read the pair it compiled failed", err.Error())
	}

	if !strings.Contains(err.Error(), "runs no macos-arm64 program") {
		t.Errorf("the refusal %q does not say this build runs no macos-arm64 program", err.Error())
	}

	paths := ffmpegPathsFor(repo, bundle.ReadManifest(), "macos-arm64")

	for _, name := range []string{"ffmpeg.gz", "ffprobe.gz"} {
		if aFileIsThere(paths.cachedProgram(name)) {
			t.Errorf("a refused pair was cached at %s", paths.cachedProgram(name))
		}

		if aFileIsThere(filepath.Join(paths.bundle, name)) {
			t.Errorf("a refused pair was published at %s", filepath.Join(paths.bundle, name))
		}
	}

	if aFileIsThere(filepath.Join(paths.bundle, "LICENCE.md")) {
		t.Errorf("a licence text was written for a pair that was refused: %s", paths.bundle)
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

// a host that cannot run the binaries adopts nothing and is told
// which host compiles them. The gate is the binaries' own execution:
// the operating system answers, and nothing is recorded about them.
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

		_, err := tl.proveSuppliedBinaries(target, bundle.ReadManifest().Targets[target], paths, io.Discard)

		if err == nil {
			t.Fatalf("%s: the run adopted binaries this %s host cannot execute", target, runtime.GOOS)
		}

		for _, want := range []string{
			target,
			"go run ./cmd/build ffmpeg " + target,
			"a " + target + " host",
			paths.bundle,
			// the operating system's own refusal, carried out of the run: the
			// binaries never started, and the reason is the operator's answer.
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

// theProofFileInTheBuildTree is where this build used to keep a
// proof of a target's binaries; tests name it to say nothing is written.
func theProofFileInTheBuildTree(repo string, target string) string {
	return filepath.Join(repo, ".build", "ffmpeg", "checks", target+".json")
}

// aDropInTheBundle lays a maintainer's binaries into one target's
// bundle directory with nothing recorded: no proof file, digest, or pin.
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

// aHostRunningTheBinaries answers the run's commands the way a
// working build does, and says which release it is when asked.
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

// aBuildOnAHost is the build standing on the host that runs one
// target's binaries, running them through that working stand-in.
func aBuildOnAHost(t *testing.T, repo string, target string, theHost host, theRun command) *tool {
	t.Helper()

	return &tool{
		repo:    repo,
		machine: func() host { return theHost },
		theRun:  theRun,
		errout:  io.Discard,
	}
}

// the run of the ffmpeg binaries is the gate, and it is a run: it
// prints what it saw and keeps nothing, no proof file and no digest.
func TestTheRunOfTheBinariesRecordsNothing(t *testing.T) {
	repo := t.TempDir()

	paths := aDropInTheBundle(t, repo, "macos-arm64")

	stand := &aHostRunningTheBinaries{}

	tl := aBuildOnAHost(t, repo, "macos-arm64", host{goos: "darwin", goarch: "arm64"}, stand.run)

	out := &bytes.Buffer{}

	run, err := tl.proveSuppliedBinaries("macos-arm64", bundle.ReadManifest().Targets["macos-arm64"], paths, out)
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

// the run refuses binaries that register video codecs, on the host
// that compiled them: this project ships an ffmpeg compiled for audio.
func TestTheRunRefusesBinariesThatRegisterVideoCodecs(t *testing.T) {
	repo := t.TempDir()

	paths := aDropInTheBundle(t, repo, "macos-arm64")

	stand := &aHostRunningTheBinaries{}
	stand.codecs = append(theAudioCodecLines, "DEVIL. h264                 H.264 / AVC / MPEG-4 AVC / MPEG-4 part 10")

	tl := aBuildOnAHost(t, repo, "macos-arm64", host{goos: "darwin", goarch: "arm64"}, stand.run)

	_, err := tl.proveSuppliedBinaries("macos-arm64", bundle.ReadManifest().Targets["macos-arm64"], paths, &bytes.Buffer{})
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

// the run refuses binaries that convert nothing, and the failure
// carries the operating system's refusal when the host cannot start them.
func TestTheRunRefusesBinariesThatDoNotConvert(t *testing.T) {
	repo := t.TempDir()

	paths := aDropInTheBundle(t, repo, "macos-arm64")

	tl := aBuildOnAHost(t, repo, "macos-arm64", host{goos: "darwin", goarch: "arm64"}, aCheckThatRefuses("the binaries said no"))

	_, err := tl.proveSuppliedBinaries("macos-arm64", bundle.ReadManifest().Targets["macos-arm64"], paths, &bytes.Buffer{})
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

// theManifestFile is this repository's manifest copied into a tree
// the build may write a pin into, worked on as written.
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

// the fixture programs are laid out the way linkers lay programs
// out: a header names its commands twice, a count and a size, and a
// universal magic is big-endian on disk.
func TestTheFixtureProgramsAreLaidOutTheWayLinkersLayThemOut(t *testing.T) {
	t.Run("the Mach-O header names its load commands twice", func(t *testing.T) {
		fixture := aSystemProgram(theArm64CPU)

		commands := theFixtureCommands(fixture)

		count := len(commands)

		size := 0

		for _, command := range commands {
			size += len(command)
		}

		bytes := machOBytes(fixture)

		if got := binary.LittleEndian.Uint32(bytes[16:]); got != uint32(count) {
			t.Errorf("the header names %d load commands at offset 16, where the count lives; want %d", got, count)
		}

		if got := binary.LittleEndian.Uint32(bytes[20:]); got != uint32(size) {
			t.Errorf("the header says its load commands occupy %d bytes at offset 20, where the size lives; want %d", got, size)
		}

		if count == size {
			t.Errorf("the count of commands (%d) and the size they occupy (%d) are the same number: a fixture that writes one number in both places lets a reader that reads the count for the size pass", count, size)
		}

		// walking the commands by the size each names must land exactly on
		// the end of the described area: the walk the build performs.
		walked := 0

		for walked < size {
			walked += int(binary.LittleEndian.Uint32(bytes[machOHeaderSize+walked+4:]))
		}

		if walked != size {
			t.Errorf("the load commands walked to %d bytes, and the header describes %d", walked, size)
		}
	})

	t.Run("the fixture that reproduces a real Darwin link reproduces it command for command", func(t *testing.T) {
		for _, tc := range []struct {
			cpuType uint32
			count   int
			size    int
		}{
			{cpuType: theX8664CPU, count: 18, size: 2128},
			{cpuType: theArm64CPU, count: 19, size: 2144},
		} {
			commands := theDarwinLinkersCommands(tc.cpuType)

			size := 0

			for _, command := range commands {
				size += len(command)
			}

			if len(commands) != tc.count || size != tc.size {
				t.Errorf("the fixture lays out %d commands occupying %d bytes, and the %s program this build compiled carries %d commands occupying %d bytes",
					len(commands), size, nameOfCPU(tc.cpuType), tc.count, tc.size)
			}

			body := theProgramALinkerProduced(tc.cpuType)

			if got := binary.LittleEndian.Uint32(body[16:]); got != uint32(tc.count) {
				t.Errorf("the %s fixture names %d load commands at offset 16, want %d", nameOfCPU(tc.cpuType), got, tc.count)
			}

			if got := binary.LittleEndian.Uint32(body[20:]); got != uint32(tc.size) {
				t.Errorf("the %s fixture says its load commands occupy %d bytes at offset 20, want %d", nameOfCPU(tc.cpuType), got, tc.size)
			}
		}
	})

	t.Run("a library reference reserves the words its version numbers live in before its name", func(t *testing.T) {
		// The name of a library a program links is 24 bytes into the command that
		// references: 56 bytes for the system library, 104 for the longest one.
		for _, tc := range []struct {
			path string
			size int
		}{
			{path: "/usr/lib/libSystem.B.dylib", size: 56},
			{path: "/System/Library/Frameworks/CoreFoundation.framework/Versions/A/CoreFoundation", size: 104},
			{path: "/System/Library/Frameworks/CoreVideo.framework/Versions/A/CoreVideo", size: 96},
		} {
			command := aDylibCommand(aDylib{Command: theLoadDylib, Path: tc.path})

			if len(command) != tc.size {
				t.Errorf("the reference to %s is %d bytes long, and the same reference in a program this build compiled is %d", tc.path, len(command), tc.size)
			}

			if got := binary.LittleEndian.Uint32(command[8:]); got != theDylibNameStart {
				t.Errorf("the reference to %s names its name at %d bytes from the command's start, want %d", tc.path, got, theDylibNameStart)
			}
		}
	})

	t.Run("a universal binary's magic is big-endian on disk", func(t *testing.T) {
		bytes := machOBytes(aMachO{Magic: theFatMagic, CPUType: theArm64CPU, Signature: true})

		if got := binary.BigEndian.Uint32(bytes[0:]); got != theFatMagic {
			t.Errorf("the fixture writes the universal magic as the bytes %x, want ca fe ba be", bytes[0:4])
		}
	})
}
