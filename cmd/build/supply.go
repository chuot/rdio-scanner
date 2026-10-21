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
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// What this build reads out of a Mach-O program, and the rules the macOS ffmpeg binaries are admitted under —
// the pair it compiled in a container, and the programs a maintainer built by hand and dropped.
//
// The machine that produces the releases runs Linux, and it cannot start a macOS program. So the build reads
// what it can read — the header and the load commands the target's own linker wrote — and requires the programs
// to be the programs that target runs, in the form their target's record names:
//
//	macos-amd64, macos-arm64  a thin 64-bit Mach-O holding that architecture's machine code, because an Apple
//	                          Silicon Mac runs arm64 and x86-64 alike and running the binaries cannot say which
//	                          one the compiler produced; and the entry point, the dynamic linker, the system
//	                          library, and on arm64 the code signature a program of that operating system needs
//	                          in front of it before it starts
//
// Every other target this project ships an ffmpeg for is compiled in a container, which produces the program
// that target runs; nothing is admitted into such a target from outside the compile that made it.
//
// A program this build compiled and a program somebody dropped are held to the same reads, which are the ones
// the target's record names: two gates would be two standards, and a release adopting on the weaker one is not
// visible from outside the build. Where a dropped program's recipe came from is the business of the machine that
// built it, and the licence text the archive carries names that machine. That the program is one the target runs
// at all is the build's business, and it is read out of the file.
const (
	machOMagic   = 0xfeedfacf // MH_MAGIC_64: a thin 64-bit image, little-endian
	machO32Magic = 0xfeedface // MH_MAGIC: 32-bit, which no release of this project ships
	fatMagic     = 0xcafebabe // a universal binary, holding more than one architecture

	cpuX8664 = 0x01000007 // CPU_TYPE_X86_64
	cpuArm64 = 0x0100000c // CPU_TYPE_ARM64

	machOHeaderSize = 32

	// The filetype a Mach-O file declares itself to be, and the load commands that say how it was linked and what
	// it has to have in front of it before Apple's loader starts it. These are the numbers the headers of the
	// software development kit this build compiles a macOS ffmpeg against define, and the numbers the linker that
	// compiles with them writes: LC_CODE_SIGNATURE carries no LC_REQ_DYLD bit, and LC_MAIN is the entry-point
	// command that carries no register set, which is why it is the same size on both architectures.
	machOFiletypeObject   = 0x1 // MH_OBJECT: a file that was never linked
	machOFiletypeExecute  = 0x2 // MH_EXECUTE: a demand-paged program the operating system runs
	machOFiletypeDylib    = 0x6 // MH_DYLIB: a library
	machOFiletypeDylinker = 0x7 // MH_DYLINKER: a dynamic linker
	machOFiletypeBundle   = 0x8 // MH_BUNDLE: a bundle a program opens at run time

	machOLoadMain        = 0x80000028 // LC_MAIN: the entry point Apple's loader will use
	machOLoadDylib       = 0x0c       // LC_LOAD_DYLIB: a library the program links against
	machOLoadWeakDylib   = 0x80000018 // LC_LOAD_WEAK_DYLIB: one it links against and can do without
	machOReexportDylib   = 0x8000001f // LC_REEXPORT_DYLIB: a library named through another one
	machOLoadUpwardDylib = 0x80000023 // LC_LOAD_UPWARD_DYLIB: a library the version above it also needs
	machOLoadDylinker    = 0x0e       // LC_LOAD_DYLINKER: the dynamic linker the program names
	machOCodeSignature   = 0x1d       // LC_CODE_SIGNATURE: what macOS checks before starting an arm64 program

	// The two things a dynamically linked macOS program cannot start without: the linker Apple's loader hands it
	// to, and the system library every program of that operating system is under.
	dyldEveryProgramStartsUnder  = "/usr/lib/dyld"
	libSystemEveryProgramLinksTo = "/usr/lib/libSystem.B.dylib"

	// Every load command starts with its number and its size, and that is as short as one gets: a reader that
	// walked past a command naming a shorter size than that would be walking into the middle of the next one.
	aLoadCommandSize = 8
)

// aMachOProgram is what the build reads out of a Mach-O file without running it: what the header says the file
// is, and the load commands that say how it was linked and what it names in front of itself.
type aMachOProgram struct {
	cpuType  uint32
	fileType uint32
	entry    bool     // LC_MAIN: an entry point Apple's loader will use
	dylinker string   // LC_LOAD_DYLINKER: the dynamic linker the program names
	dylibs   []string // the library references, in the order the program lists them
	signed   bool     // LC_CODE_SIGNATURE: the program carries a signature
}

// aMachOProblem is one fact about a Mach-O program that a rule refuses, in the two halves this build's gates
// both say: what the file said, and what the read asked for instead. The gate a dropped program passes puts the
// first half in the sentence it says to the machine that dropped it; the read the build makes of the pair it
// compiled puts both halves in the sentence it says about a program it produced and did not run. An empty "said"
// is the file saying what was asked.
type aMachOProblem struct {
	// noRule is a record naming a read this build has no rule for. That is the record's failure and not the
	// file's, and each gate says it in the words it uses about a record.
	noRule bool

	said  string
	asked string
}

// aMachOProgramAt reads a Mach-O program's header, walks the load commands it describes, and answers either the
// program or what the file said instead of one: a build that could not read a pair has to be able to say why.
//
// Which architecture's machine code the file holds is asked here, and not left to a read a record names, because
// it is a fact the header carries and not a promise the release could have made differently: macos-amd64 runs
// x86-64 and nothing else, and the header is all that stands in front of a machine that cannot start the program
// to say what the compiler wrote.
func aMachOProgramAt(path string, target string) (aMachOProgram, aMachOProblem) {
	body, err := os.ReadFile(path)
	if err != nil {
		return aMachOProgram{}, aMachOProblem{
			said:  fmt.Sprintf("cannot be read: %v", err),
			asked: "the program that target runs",
		}
	}

	if len(body) < machOHeaderSize {
		return aMachOProgram{}, aMachOProblem{
			said:  "is not a Mach-O program: it is shorter than the header a Mach-O program starts with",
			asked: "a Mach-O program",
		}
	}

	// A universal binary's magic is written big-endian by every linker that produces one, whatever the byte
	// order of the images inside it; a thin image's magic is the byte order of that image. So the fat magic is
	// looked for in the big-endian reading of the first four bytes, and the thin magics in the little-endian one.
	if binary.BigEndian.Uint32(body[0:]) == fatMagic {
		return aMachOProgram{}, aMachOProblem{
			said:  "is a universal binary: this project ships one archive per architecture and each binary carries the ffmpeg binaries that architecture runs, so build one program per architecture",
			asked: "one program holding the machine code of one architecture",
		}
	}

	switch magic := binary.LittleEndian.Uint32(body[0:]); magic {
	case machOMagic:
	case machO32Magic:
		return aMachOProgram{}, aMachOProblem{
			said:  "is a 32-bit Mach-O program: the macOS releases this project ships are 64-bit",
			asked: "a 64-bit Mach-O program",
		}
	default:
		return aMachOProgram{}, aMachOProblem{
			said:  fmt.Sprintf("is not a Mach-O program: its first four bytes are % 02x", body[0:4]),
			asked: "a Mach-O program",
		}
	}

	file := aMachOProgram{
		cpuType:  binary.LittleEndian.Uint32(body[4:]),
		fileType: binary.LittleEndian.Uint32(body[12:]),
	}

	// The header names its load commands twice: the count of them, and the size they occupy. They are two
	// different numbers, the walk follows the size each command names, and the area the header describes is what
	// the walk is allowed to read.
	commands := int(binary.LittleEndian.Uint32(body[16:]))
	described := int(binary.LittleEndian.Uint32(body[20:]))

	if machOHeaderSize+described > len(body) {
		return aMachOProgram{}, aMachOProblem{
			said: fmt.Sprintf("describes load commands occupying %d bytes after its header, and the file is only %d bytes long",
				described, len(body)),
			asked: "a Mach-O program whose load commands are there to read",
		}
	}

	end := machOHeaderSize + described

	at := machOHeaderSize

	for i := 0; i < commands; i++ {
		if at+aLoadCommandSize > end {
			return aMachOProgram{}, aMachOProblem{
				said: fmt.Sprintf("names %d load commands occupying %d bytes, and they run past the area its header describes at byte %d",
					commands, described, at),
				asked: "a Mach-O program whose load commands are there to read",
			}
		}

		command := binary.LittleEndian.Uint32(body[at:])
		size := int(binary.LittleEndian.Uint32(body[at+4:]))

		if size < aLoadCommandSize {
			return aMachOProgram{}, aMachOProblem{
				said: fmt.Sprintf("carries a load command numbered 0x%x at byte %d that is %d bytes long, shorter than the 8 bytes every load command starts with",
					command, at, size),
				asked: "a Mach-O program whose load commands are there to read",
			}
		}

		if at+size > end {
			return aMachOProgram{}, aMachOProblem{
				said: fmt.Sprintf("carries a load command at byte %d that is %d bytes long, and the area its header describes for load commands ends at byte %d",
					at, size, end),
				asked: "a Mach-O program whose load commands are there to read",
			}
		}

		switch command {
		case machOLoadMain:
			file.entry = true

		case machOCodeSignature:
			file.signed = true

		case machOLoadDylib, machOLoadWeakDylib, machOReexportDylib, machOLoadUpwardDylib:
			name, problem := aNameTheLoadCommandCarries(body, at, size, "library name")
			if problem.said != "" {
				return aMachOProgram{}, problem
			}

			file.dylibs = append(file.dylibs, name)

		case machOLoadDylinker:
			name, problem := aNameTheLoadCommandCarries(body, at, size, "dynamic linker")
			if problem.said != "" {
				return aMachOProgram{}, problem
			}

			file.dylinker = name
		}

		at += size
	}

	if wants, named := theCPUTypeTheTargetRuns(target); named && file.cpuType != wants {
		return aMachOProgram{}, aMachOProblem{
			said: fmt.Sprintf("holds the %s architecture's machine code, and %s runs %s",
				nameOfCPU(file.cpuType), target, nameOfCPU(wants)),
			asked: fmt.Sprintf("the machine code that %s runs", target),
		}
	}

	return file, aMachOProblem{}
}

// aNameTheLoadCommandCarries reads the string one load command names as an offset from its own start, bounded by
// the command that carries it. A header describes a program by naming offsets inside it, and a reader that trusted
// an offset it was given would read past the command it is reading, and past whatever the next command is.
func aNameTheLoadCommandCarries(body []byte, at int, size int, what string) (string, aMachOProblem) {
	if size < aLoadCommandSize+4 {
		return "", aMachOProblem{
			said:  fmt.Sprintf("carries a load command that is %d bytes long, which is not long enough to name a %s", size, what),
			asked: fmt.Sprintf("a load command naming the %s it needs", what),
		}
	}

	offset := int(binary.LittleEndian.Uint32(body[at+8:]))

	if offset < aLoadCommandSize+4 || at+offset >= at+size {
		return "", aMachOProblem{
			said:  fmt.Sprintf("names its %s at %d bytes from its start, and the command is %d bytes long", what, offset, size),
			asked: fmt.Sprintf("the %s it needs, named inside the load command that carries it", what),
		}
	}

	name := body[at+offset : at+size]

	if cut := bytes.IndexByte(name, 0); cut >= 0 {
		name = name[:cut]
	}

	return string(name), aMachOProblem{}
}

// checkSuppliedProgram reports whether the program at path is the program the given target runs, holding it to
// the reads that target's record names for the pair this build compiles. The gate a dropped program passes and
// the read the build makes of its own compile are one rule, said in the voice each of them is read by.
func checkSuppliedProgram(path string, target string, record bundle.Target) error {
	name := filepath.Base(path)

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("the supplied %s for %s cannot be read: %w", name, target, err)
	}

	if info.Size() == 0 {
		return fmt.Errorf("the supplied %s for %s is empty: a target carries a program, not a placeholder", name, target)
	}

	if info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("the supplied %s for %s is not executable: the server writes it out of its own binary and runs it, so the ffmpeg binaries have to be dropped as executable programs", name, target)
	}

	if theTargetRunsMachOPrograms(target) {
		return checkTheSuppliedMachOProgram(path, name, target, record)
	}

	return fmt.Errorf("%s is not a target this project supplies an ffmpeg for", target)
}

// checkTheSuppliedMachOProgram reports whether the program somebody dropped is the program the given macOS
// target runs.
//
// The architecture is the rule a crossed compile needs: an Apple Silicon Mac compiling the Intel binaries runs
// what it produced under Rosetta 2 just as well, so only the file itself says which machine code the compiler
// wrote. The entry point, the dynamic linker, the system library and the code signature are the rules the release
// needs: the server writes the program out of its own binary and starts it, and a program that target's loader
// will not start is not the ffmpeg the release ships.
func checkTheSuppliedMachOProgram(path string, name string, target string, record bundle.Target) error {
	file, problem := aMachOProgramAt(path, target)

	if problem.said != "" {
		return theSuppliedProgramRefuses(name, target, problem.said)
	}

	for _, read := range record.Conformance.Reads {
		failure := theMachOReadFailure(read, file)

		if failure.noRule {
			return aRecordNamingAReadWithNoRule(target, read)
		}

		if failure.said != "" {
			return theSuppliedProgramRefuses(name, target, failure.said)
		}
	}

	return nil
}

// theSuppliedProgramRefuses says to the machine that dropped a program what the program it handed over said
// instead of what its target's record asks a program to say. It is this gate's own sentence, and not the one the
// build says about a pair it compiled: whoever dropped the program is told which rule it broke, and is not told
// that something was run to find that out.
func theSuppliedProgramRefuses(name string, target string, said string) error {
	return fmt.Errorf("the supplied %s for %s %s", name, target, said)
}

// readAMachOProgram reads the file at path and holds it to each name the record carries, in the order the record
// carries them, refusing at the first thing the file does not say. It is the Mach-O half of what a target's record
// names in its conformance reads, and it is what a pair this build compiled in a container is proved by on a
// machine that cannot start either program.
func readAMachOProgram(path string, program string, target string, reads []string) error {
	file, problem := aMachOProgramAt(path, target)

	if problem.said != "" {
		return aReadRefusal(program, target, strings.Join(reads, " "), problem.said,
			"the thin 64-bit Mach-O program holding the machine code that target runs, with its load commands there to read")
	}

	for _, read := range reads {
		failure := theMachOReadFailure(read, file)

		if failure.noRule {
			return aRecordNamingAReadWithNoRule(target, read)
		}

		if failure.said != "" {
			return aReadRefusal(program, target, read, failure.said, failure.asked)
		}
	}

	return nil
}

// theMachOReadFailure answers no problem at all when a program says what one read asks it to say, and the two
// halves of an answer an operator acts on when it does not. Each name refuses one shape a compiler can produce,
// and every refusal is read out of the load commands the program carries.
//
// A name this build has no rule for is the record's failure and not the file's, which is why it is answered inside
// the switch: a rule that holds and a rule that does not exist are two different answers, and a build that let the
// first fall through into the second would refuse the pair its own compile produced for having no rule.
func theMachOReadFailure(read string, file aMachOProgram) aMachOProblem {
	switch read {
	case "mh_execute":
		if file.fileType != machOFiletypeExecute {
			return aMachOProblem{
				said:  fmt.Sprintf("is not an executable file: its header says %s", aMachOFileTypeName(file.fileType)),
				asked: "the MH_EXECUTE program Apple's loader starts",
			}
		}

	case "lc_main":
		if !file.entry {
			return aMachOProblem{
				said:  "carries no LC_MAIN, so nothing in it names an entry point Apple's loader will use",
				asked: "the entry point the program starts at",
			}
		}

	case "lc_load_dylinker":
		if file.dylinker == "" {
			return aMachOProblem{
				said: fmt.Sprintf("carries no LC_LOAD_DYLINKER, and Apple's loader starts a program of this target's operating system under %s",
					dyldEveryProgramStartsUnder),
				asked: dyldEveryProgramStartsUnder + ", the dynamic linker every macOS program starts under",
			}
		}

		if file.dylinker != dyldEveryProgramStartsUnder {
			return aMachOProblem{
				said:  fmt.Sprintf("names %s as the dynamic linker it needs, and Apple's loader starts a macOS program under %s", file.dylinker, dyldEveryProgramStartsUnder),
				asked: dyldEveryProgramStartsUnder,
			}
		}

	case "lc_load_libsystem":
		if len(file.dylibs) == 0 {
			return aMachOProblem{
				said: fmt.Sprintf("carries no LC_LOAD_DYLIB at all: it is a freestanding program, linked against no library, and not the ffmpeg this record's recipe compiles, which links %s",
					libSystemEveryProgramLinksTo),
				asked: libSystemEveryProgramLinksTo + ", the system library every macOS program links against",
			}
		}

		if !theProgramLinksTheSystemLibrary(file) {
			return aMachOProblem{
				said: fmt.Sprintf("links %s, and not %s, the system library every macOS program links against",
					strings.Join(file.dylibs, ", "), libSystemEveryProgramLinksTo),
				asked: libSystemEveryProgramLinksTo,
			}
		}

	case "lc_code_signature":
		if !file.signed {
			return aMachOProblem{
				said:  "carries no LC_CODE_SIGNATURE, and macOS 11 and later refuse an unsigned arm64 program at exec",
				asked: "an arm64 program its own link signed",
			}
		}

	default:
		return aMachOProblem{noRule: true}
	}

	// The file said what this read asked it to say.
	return aMachOProblem{}
}

// theProgramLinksTheSystemLibrary says whether one of the references the program carries is the system library a
// macOS program is under. A program that links the system library and three libraries a maintainer installed at
// home is the program the target runs; a program that links only the ones he installed is a program the recipe
// never compiled.
func theProgramLinksTheSystemLibrary(file aMachOProgram) bool {
	for _, dylib := range file.dylibs {
		if dylib == libSystemEveryProgramLinksTo {
			return true
		}
	}

	return false
}

// aMachOFileTypeName names the filetype a Mach-O header declares itself to be, which is what a refusal says a
// program is when it is not the executable file its target runs.
func aMachOFileTypeName(fileType uint32) string {
	switch fileType {
	case machOFiletypeObject:
		return "MH_OBJECT, a file that was never linked"
	case machOFiletypeExecute:
		return "MH_EXECUTE"
	case machOFiletypeDylib:
		return "MH_DYLIB, a library the server writes out of its own binary and cannot run"
	case machOFiletypeDylinker:
		return "MH_DYLINKER, a dynamic linker"
	case machOFiletypeBundle:
		return "MH_BUNDLE, a bundle a program opens at run time"
	}

	return fmt.Sprintf("the Mach-O filetype 0x%x", fileType)
}

// theTargetRunsMachOPrograms says whether the programs one target runs are Mach-O programs, which is a fact of the
// operating system that target is and not a thing a record can name differently: the two macOS targets run Mach-O
// programs, and every other target this project ships an ffmpeg for runs an ELF program. The same answer decides
// which half of this build's read rule reaches a pair it compiled.
func theTargetRunsMachOPrograms(target string) bool {
	tr, found := targetFor(target)

	return found && tr.goos == "darwin"
}

// theCPUTypeTheTargetRuns is the machine code one target runs, which is the architecture its release archive is
// named for. Only the Mach-O rule asks it, because only a Mach-O header answers it.
func theCPUTypeTheTargetRuns(target string) (uint32, bool) {
	tr, found := targetFor(target)
	if !found {
		return 0, false
	}

	switch tr.goarch {
	case "amd64":
		return cpuX8664, true
	case "arm64":
		return cpuArm64, true
	}

	return 0, false
}

func nameOfCPU(cpuType uint32) string {
	switch cpuType {
	case cpuX8664:
		return "x86-64"
	case cpuArm64:
		return "arm64"
	}

	return fmt.Sprintf("0x%x", cpuType)
}

// theRunOfTheBinaries is what the build saw when it ran the ffmpeg binaries: the machine that ran them,
// the version they said they are, the codecs they registered, and the recordings they converted.
//
// It is carried from the run to the licence text the archive ships, and it is kept nowhere else. There is
// no proof file for a later build to read: the run that adopted the binaries is the run that ran them,
// and the licence text says which machine that was.
type theRunOfTheBinaries struct {
	Host        string
	Version     string
	Codecs      codecRegistry
	Conversions int
	Productions []production
}

// theBinariesTheHostCannotRun is what a host is told when the ffmpeg binaries in front of it do not run on it.
//
// The gate is the binaries' own execution, so a host that cannot execute them adopts nothing, and the
// answer it gets names the host that can run them, the command that compiles them there, and the directory
// the ones it cannot run are sitting in. The binaries in front of it are the ones it compiled, or the ones
// somebody dropped.
//
// The operating system's own refusal is carried inside it. This build does not ask the host what it has
// installed and does not guess at what it can execute: an Intel Mac holding the arm64 pair, and an Apple
// Silicon Mac holding the Intel pair with no translator installed to start it, are answered by the programs
// not starting. The gate is asked of a pair the host was about to adopt, and that is a macOS pair on a macOS
// host: the container route never starts a pair this host cannot start, it reads what it produced instead.
func theBinariesTheHostCannotRun(target string, paths ffmpegPaths, machine host, err error) error {
	return fmt.Errorf("the %s ffmpeg binaries do not run on this machine (%s): %w\n"+
		"the pair in %s is adopted by being run, and this host cannot run it: run go run ./cmd/build ffmpeg %s on a %s host",
		target, releaseTargetWordFor(machine.goos, machine.goarch), err, paths.bundle, target, target)
}

// proveSuppliedBinaries runs the ffmpeg binaries sitting in one target's bundle directory, on the machine
// standing here, and says what it saw.
//
// The binaries are read before they are run: the header and the load commands the target's own linker writes are
// what a run cannot say across the two macOS architectures, because an Apple Silicon Mac starts arm64 and x86-64
// programs alike. A run answers what the program does; the read answers which program it is.
//
// The compile a macOS host runs and the run that adopts a drop both run the binaries through this: two gates
// would be two standards, and a release adopting on the weaker one is not visible from outside the build.
func (t tool) proveSuppliedBinaries(target string, record bundle.Target, paths ffmpegPaths, stdout io.Writer) (theRunOfTheBinaries, error) {
	drop := suppliedDropFor(paths, target)

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if !aFileIsThere(drop[name]) {
			return theRunOfTheBinaries{}, fmt.Errorf("there is no %s in %s to run: build the ffmpeg binaries on a %s host, drop ffmpeg and ffprobe in %s, and run this again",
				name, paths.bundle, target, paths.bundle)
		}

		if err := checkSuppliedProgram(drop[name], target, record); err != nil {
			return theRunOfTheBinaries{}, err
		}
	}

	work := filepath.Join(t.repo, ".build", "ffmpeg", "run", target)

	if err := os.MkdirAll(work, 0755); err != nil {
		return theRunOfTheBinaries{}, fmt.Errorf("the build cannot prepare %s: %w", work, err)
	}

	programs := checkPrograms{FFmpeg: []string{drop["ffmpeg"]}, FFProbe: []string{drop["ffprobe"]}, Work: work}

	machine := t.theMachine()

	version, err := theProgramVersion(t.theRun, programs.FFmpeg)
	if err != nil {
		return theRunOfTheBinaries{}, theBinariesTheHostCannotRun(target, paths, machine, err)
	}

	report, err := ffmpegCheck(t.theRun, programs, work)
	if err != nil {
		return theRunOfTheBinaries{}, theBinariesTheHostCannotRun(target, paths, machine, err)
	}

	fmt.Fprintf(stdout, "%-7s %d conversions, %d codecs, %s s to %s s\n", "check", report.Conversions,
		len(report.Codecs.Audio),
		strconv.FormatFloat(report.Shortest, 'f', 6, 64),
		strconv.FormatFloat(report.Longest, 'f', 6, 64),
	)

	return theRunOfTheBinaries{
		Host:        machine.goos + "-" + machine.goarch,
		Version:     version,
		Codecs:      report.Codecs,
		Conversions: report.Conversions,
		Productions: report.Productions,
	}, nil
}

// theProgramVersion is the first line the program says about itself, which is the release the run
// records: the manifest pins one release, and binaries built from another are binaries this release does
// not ship.
func theProgramVersion(run command, program []string) (string, error) {
	body, err := run(starts(program, "-version"), nil)
	if err != nil {
		return "", fmt.Errorf("the ffmpeg binaries cannot say what they are: %s -version: %w", program[0], err)
	}

	line, _, _ := strings.Cut(strings.TrimSpace(string(body)), "\n")
	if line == "" {
		return "", fmt.Errorf("the ffmpeg binaries say nothing about what they are: %s -version printed nothing", program[0])
	}

	return line, nil
}

// onThisMachine runs one program here, hands it a recording on its input, and hands back what it wrote.
//
// It is the shape the check runs compiled binaries through a container in, used for the binaries this
// machine can run: the pair a host adopts is a pair built for the machine standing there, so nothing stands
// between the check and the programs.
//
// A program that ran and answered badly answers with its own words, which is what the check reads. A
// program that never started has no words of its own: the operating system refused to start it, and that
// refusal is the whole answer, so it is carried out and not thrown away. It is the answer a host gives that
// was handed the binaries of the other macOS architecture.
func onThisMachine() command {
	return func(argv []string, stdin []byte) ([]byte, error) {
		out := bytes.NewBuffer([]byte(nil))
		errout := bytes.NewBuffer([]byte(nil))

		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdin = bytes.NewReader(stdin)
		cmd.Stdout = out
		cmd.Stderr = errout

		if err := cmd.Run(); err != nil {
			var exit *exec.ExitError

			if errors.As(err, &exit) {
				return nil, fmt.Errorf("%s", strings.TrimSpace(errout.String()))
			}

			return nil, fmt.Errorf("%w", err)
		}

		return out.Bytes(), nil
	}
}
