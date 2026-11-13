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
	"strings"
	"testing"

	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// The reads hold against files: no host runs these targets. The fixtures are the shapes each
// read refuses plus two measured pairs: the FreeBSD pair the pinned recipe produced and the
// published linux-amd64 pair to refuse.

// program headers of the two measured pairs, read out of the files with readelf -lW: the
// loader-linked-in read is asked of a file with these.
const (
	elfSegmentLoad        = 1
	elfSegmentNote        = 4
	elfSegmentPHDR        = 6
	elfSegmentTLS         = 7
	elfSegmentGNUEHFrame  = 0x6474e550
	elfSegmentGNUStack    = 0x6474e551
	elfSegmentGNURelRO    = 0x6474e552
	elfSegmentGNUProperty = 0x6474e553
)

// anELFProgramFixture is one compiled program's shape, named the way a record's reads name it.
type anELFProgramFixture struct {
	class    byte
	fileType uint16
	machine  uint16
	osABI    byte
	segments []uint32
	comments []string

	notELF     bool // the bytes are not an ELF program at all
	truncated  bool // the file stops inside the header
	noComment  bool // the file carries sections and no comment among them
	noSegments bool // the file declares no program headers at all
}

// theMeasuredFreeBSDPair is the pinned recipe's FreeBSD pair, read off the files: its ten
// program headers and three comment lines. The reads must accept it: refusing this pair would
// refuse the release.
var theMeasuredFreeBSDPair = anELFProgramFixture{
	class:    elfClass64,
	fileType: elfTypeExec,
	machine:  elfMachineX8664,
	osABI:    elfOSABIFreeBSD,
	segments: []uint32{
		elfSegmentPHDR,
		elfSegmentLoad, elfSegmentLoad, elfSegmentLoad, elfSegmentLoad,
		elfSegmentTLS,
		elfSegmentGNURelRO,
		elfSegmentGNUEHFrame,
		elfSegmentGNUStack,
		elfSegmentNote,
	},
	comments: []string{
		"Linker: Debian LLD 19.1.7",
		"Debian clang version 19.1.7 (3+b1)",
		"FreeBSD clang version 19.1.7 (https://github.com/llvm/llvm-project.git llvmorg-19.1.7-0-gcd708029e0b2)",
	},
}

// thePairThisBuildCompilesForLinux is the published linux-amd64 ffmpeg, read off the bundle's
// gzip: static, no FreeBSD sysroot or FreeBSD objects in the link. The FreeBSD reads must
// refuse it: that release cannot start it.
var thePairThisBuildCompilesForLinux = anELFProgramFixture{
	class:    elfClass64,
	fileType: elfTypeExec,
	machine:  elfMachineX8664,
	osABI:    elfOSABILinux,
	segments: []uint32{
		elfSegmentLoad, elfSegmentLoad, elfSegmentLoad, elfSegmentLoad,
		elfSegmentNote, elfSegmentNote, elfSegmentNote,
		elfSegmentTLS,
		elfSegmentGNUProperty,
		elfSegmentGNUStack,
		elfSegmentGNURelRO,
	},
	comments: []string{"GCC: (Debian 14.2.0-19) 14.2.0"},
}

// theShapeOf copies the accepted shape and changes one thing, so a table of refusals says one
// part is wrong without writing a new file per row.
func theShapeOf(change func(*anELFProgramFixture)) anELFProgramFixture {
	fixture := theMeasuredFreeBSDPair

	fixture.segments = append([]uint32{}, theMeasuredFreeBSDPair.segments...)
	fixture.comments = append([]string{}, theMeasuredFreeBSDPair.comments...)

	change(&fixture)

	return fixture
}

// anELFProgramWrittenTo writes a program of the fixture's shape into dir and answers with its
// path; the bytes sit at the file's own class widths.
func anELFProgramWrittenTo(t *testing.T, dir string, name string, fixture anELFProgramFixture) string {
	t.Helper()

	path := filepath.Join(dir, name)

	if err := os.WriteFile(path, theELFProgramBytes(fixture), 0o755); err != nil {
		t.Fatalf("writing the %s fixture: %v", name, err)
	}

	return path
}

func theELFProgramBytes(fixture anELFProgramFixture) []byte {
	if fixture.notELF {
		// MH_MAGIC_64: what this build would have written for a macOS target, sitting in an ELF target's place.
		return append([]byte{0xcf, 0xfa, 0xed, 0xfe, 0, 0, 0, 0}, make([]byte, 64)...)
	}

	if fixture.truncated {
		// A file that stops inside its own header: the ident is whole, the fields after it are not.
		return append([]byte{0x7f, 'E', 'L', 'F', elfClass64, 1, 1, elfOSABIFreeBSD, 0, 0, 0, 0, 0, 0, 0, 0}, elfTypeExec, 0, elfMachineX8664, 0, 1, 0, 0, 0)
	}

	class64 := fixture.class == elfClass64

	headerSize, entrySize, shentsize := 52, 32, 40
	if class64 {
		headerSize, entrySize, shentsize = 64, 56, 64
	}

	// The code is four bytes of padding: no read looks inside machine code, and the
	// fixtures speak only of how the file claims it was made.
	code := []byte{0x2c, 0x60, 0x75, 0x02}

	comment := []byte{0}
	for _, line := range fixture.comments {
		comment = append(comment, []byte(line)...)
		comment = append(comment, 0)
	}

	sectionNames := []string{".text"}
	if !fixture.noComment {
		sectionNames = append(sectionNames, ".comment")
	}
	sectionNames = append(sectionNames, ".shstrtab")

	shstrtab := []byte{0}

	nameAt := map[string]int{}

	for _, section := range sectionNames {
		nameAt[section] = len(shstrtab)

		shstrtab = append(shstrtab, []byte(section)...)
		shstrtab = append(shstrtab, 0)
	}

	segments := fixture.segments
	if fixture.noSegments {
		segments = []uint32{}
	}

	phoff := headerSize
	codeOff := phoff + len(segments)*entrySize
	commentOff := codeOff + len(code)
	shstrtabOff := commentOff + len(comment)
	shoff := shstrtabOff + len(shstrtab)

	sections := 1 + len(sectionNames)

	body := make([]byte, shoff+sections*shentsize)

	copy(body, []byte{0x7f, 'E', 'L', 'F', fixture.class, 1, 1, fixture.osABI, 0, 0, 0, 0, 0, 0, 0, 0})

	file := &anELFByteLayout{body: body, at: 16, class64: class64}

	file.u16(fixture.fileType)
	file.u16(fixture.machine)
	file.u32(1)
	file.word(uint64(codeOff))
	file.word(uint64(phoff))
	file.word(uint64(shoff))
	file.u32(0)
	file.u16(uint16(headerSize))
	file.u16(uint16(entrySize))
	file.u16(uint16(len(segments)))
	file.u16(uint16(shentsize))
	file.u16(uint16(sections))
	file.u16(uint16(sections - 1))

	file.at = phoff

	for _, segment := range segments {
		file.u32(segment)

		if class64 {
			file.u32(0)
		}

		file.word(uint64(codeOff))
		file.word(uint64(codeOff))
		file.word(uint64(codeOff))
		file.word(uint64(len(code)))
		file.word(uint64(len(code)))

		if !class64 {
			file.u32(0)
		}

		file.word(0x1000)
	}

	copy(body[codeOff:], code)
	copy(body[commentOff:], comment)
	copy(body[shstrtabOff:], shstrtab)

	file.at = shoff

	// Section zero is the null section, and the rest are the ones this file carries.
	for _, section := range append([]string{""}, sectionNames...) {
		if section == "" {
			file.at = shoff

			for i := 0; i < shentsize; i++ {
				body[shoff+i] = 0
			}

			file.at += shentsize

			continue
		}

		file.u32(uint32(nameAt[section]))

		switch section {
		case ".comment":
			file.u32(1) // SHT_PROGBITS
		case ".shstrtab":
			file.u32(3) // SHT_STRTAB
		default:
			file.u32(1)
		}

		file.word(0)
		file.word(0)

		switch section {
		case ".comment":
			file.word(uint64(commentOff))
			file.word(uint64(len(comment)))
		case ".shstrtab":
			file.word(uint64(shstrtabOff))
			file.word(uint64(len(shstrtab)))
		default:
			file.word(uint64(codeOff))
			file.word(uint64(len(code)))
		}

		file.u32(0)
		file.u32(0)
		file.word(1)
		file.word(0)
	}

	return body
}

// anELFByteLayout writes each ELF field at the width the file's class names, so a fixture is
// never refused for the wrong reason.
type anELFByteLayout struct {
	body    []byte
	at      int
	class64 bool
}

func (l *anELFByteLayout) u16(v uint16) {
	binary.LittleEndian.PutUint16(l.body[l.at:], v)
	l.at += 2
}

func (l *anELFByteLayout) u32(v uint32) {
	binary.LittleEndian.PutUint32(l.body[l.at:], v)
	l.at += 4
}

func (l *anELFByteLayout) word(v uint64) {
	if l.class64 {
		binary.LittleEndian.PutUint64(l.body[l.at:], v)
		l.at += 8

		return
	}

	binary.LittleEndian.PutUint32(l.body[l.at:], uint32(v))
	l.at += 4
}

// the reads accept the exact pair the pinned FreeBSD recipe produced, the pair the release
// ships.
func TestTheELFReadsAcceptThePairTheFreeBSDCompileProduced(t *testing.T) {
	record := bundle.ReadManifest().Targets["freebsd-amd64"]

	dir := t.TempDir()

	for _, program := range []string{"ffmpeg", "ffprobe"} {
		path := anELFProgramWrittenTo(t, dir, program, theMeasuredFreeBSDPair)

		if err := readAnELFProgram(path, program, "freebsd-amd64", record.Conformance.Reads, record); err != nil {
			t.Errorf("%s is refused by the reads its own record names: %v", program, err)
		}
	}
}

// each read name refuses the one shape it was written for, and the refusal sentence names what
// was read, what was asked, and that nothing was run.
func TestEveryELFReadRefusesTheShapeItWasWrittenToRefuse(t *testing.T) {
	record := bundle.ReadManifest().Targets["freebsd-amd64"]

	cases := []struct {
		read    string
		fixture anELFProgramFixture
		says    []string
	}{
		{
			read:    "elf64",
			fixture: theShapeOf(func(f *anELFProgramFixture) { f.class = elfClass32 }),
			says:    []string{"32-bit", "ELF32", "ELF64"},
		},
		{
			read:    "exec",
			fixture: theShapeOf(func(f *anELFProgramFixture) { f.fileType = elfTypeDyn }),
			says:    []string{"ET_DYN", "ET_EXEC"},
		},
		{
			read:    "exec",
			fixture: theShapeOf(func(f *anELFProgramFixture) { f.fileType = elfTypeRel }),
			says:    []string{"ET_REL", "ET_EXEC"},
		},
		{
			read:    "x86_64",
			fixture: theShapeOf(func(f *anELFProgramFixture) { f.machine = elfMachineAArch64 }),
			says:    []string{"AArch64", "x86-64"},
		},
		{
			// what Debian's own ld.bfd stamps on this source and sysroot: a valid
			// x86-64 ELF program that is not a FreeBSD one.
			read:    "osabi_freebsd",
			fixture: theShapeOf(func(f *anELFProgramFixture) { f.osABI = elfOSABILinux }),
			says:    []string{"GNU (3)", "FreeBSD (9)"},
		},
		{
			read:    "osabi_freebsd",
			fixture: theShapeOf(func(f *anELFProgramFixture) { f.osABI = elfOSABISysV }),
			says:    []string{"System V (0)", "FreeBSD (9)"},
		},
		{
			read: "static",
			fixture: theShapeOf(func(f *anELFProgramFixture) {
				f.segments = append(f.segments, elfSegmentInterp, elfSegmentDynamic)
			}),
			says: []string{"PT_INTERP", "PT_DYNAMIC"},
		},
		{
			// the pair a host producing past its own --sysroot would leave: the
			// host's linker, the host's libraries.
			read: "comment",
			fixture: theShapeOf(func(f *anELFProgramFixture) {
				f.comments = []string{"GCC: (Debian 14.2.0-19) 14.2.0"}
			}),
			says: []string{"lld", "GCC: (Debian 14.2.0-19) 14.2.0", "/sysroot"},
		},
		{
			// lld with the sysroot but no FreeBSD crt or libc in the link: the
			// comment section names no FreeBSD build.
			read: "comment",
			fixture: theShapeOf(func(f *anELFProgramFixture) {
				f.comments = []string{"Linker: Debian LLD 19.1.7", "Debian clang version 19.1.7 (3+b1)"}
			}),
			says: []string{"Linker: Debian LLD 19.1.7", "FreeBSD", "docker.io/freebsd/freebsd-toolchain:15.1"},
		},
		{
			read:    "comment",
			fixture: theShapeOf(func(f *anELFProgramFixture) { f.noComment = true }),
			says:    []string{"carries no comment section"},
		},
		{
			read:    "comment",
			fixture: theShapeOf(func(f *anELFProgramFixture) { f.comments = []string{} }),
			says:    []string{"names nothing"},
		},
		{
			read:    "elf64",
			fixture: anELFProgramFixture{notELF: true},
			says:    []string{"not an ELF program"},
		},
		{
			read:    "elf64",
			fixture: anELFProgramFixture{truncated: true},
			says:    []string{"shorter than the header an ELF program starts with"},
		},
		{
			// A record cannot promise a proof this build does not know how to make.
			read:    "mh_execute",
			fixture: theMeasuredFreeBSDPair,
			says:    []string{"mh_execute", "no rule for that read"},
		},
	}

	dir := t.TempDir()

	for _, c := range cases {
		path := anELFProgramWrittenTo(t, dir, "ffmpeg", c.fixture)

		err := readAnELFProgram(path, "ffmpeg", "freebsd-amd64", []string{c.read}, record)
		if err == nil {
			t.Errorf("%s accepted the shape its %s was written to refuse: %v", c.read, c.read, c.fixture)
			continue
		}

		for _, say := range append(c.says, "runs no freebsd-amd64 program") {
			if !strings.Contains(err.Error(), say) {
				t.Errorf("the %s refusal %q does not name %s", c.read, err.Error(), say)
			}
		}
	}
}

// a pair with no program headers says nothing about a linked-in loader, and a read that
// accepted it would accept a pair it had not read.
func TestAProgramWithNoProgramHeadersSaysNothingAboutHowItWasLinked(t *testing.T) {
	record := bundle.ReadManifest().Targets["freebsd-amd64"]

	path := anELFProgramWrittenTo(t, t.TempDir(), "ffmpeg", theShapeOf(func(f *anELFProgramFixture) {
		f.noSegments = true
	}))

	err := readAnELFProgram(path, "ffmpeg", "freebsd-amd64", []string{"static"}, record)
	if err == nil {
		t.Fatal("a program with no program headers was accepted by the read that asks for none")
	}

	if !strings.Contains(err.Error(), "declares no program headers") {
		t.Errorf("the refusal %q does not say the file declares no program headers", err.Error())
	}
}

// theContainerMachineHasTheseCores is what the container-engine stand-in in the harness below reports for
// the machine it runs compiles on: a count of its own, deliberately not this process's own core count.
const theContainerMachineHasTheseCores = "4"

// aBuildCompilingInAContainer stands the ffmpeg phase on a host with the container program:
// the steps write the test's pair into the mounted directory, and the build's own read then
// reaches real files.
func aBuildCompilingInAContainer(t *testing.T, repo string, target string, produced func(t *testing.T, dir string, name string)) (*tool, *bytes.Buffer) {
	t.Helper()

	manifest := bundle.ReadManifest()

	out := &bytes.Buffer{}

	record := manifest.Targets[target]

	if record.Sysroot != (bundle.Sysroot{}) {
		aHomeWithTheSysrootPreparedInIt(t, record)
	}

	tl := &tool{
		repo:   repo,
		errout: io.Discard,
		resolve: func(name string) (string, error) {
			return name, nil
		},
		machine: func() host { return theReleaseHost },
		theRun: func(argv []string, stdin []byte) ([]byte, error) {
			// The harness stands in for the container engine, and answers the CPU-count question the run
			// asks before planning a compile in that engine's machine.
			return []byte(theContainerMachineHasTheseCores + "\n"), nil
		},
		source: func(ffmpegSource, bundle.Manifest) error {
			return nil
		},
		execute: func(s step) error {
			if s.label != "make" {
				return nil
			}

			// the container compiles in the directory mounted at /work, and the
			// pair it produced sits there when the plan's steps end.
			dir := filepath.Join(ffmpegPathsFor(repo, manifest, target).scratch, sourceDir(manifest))

			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("making the tree the container compiled in: %v", err)
			}

			for _, program := range []string{"ffmpeg", "ffprobe"} {
				produced(t, dir, program+programSuffix(target))
			}

			return nil
		},
	}

	return tl, out
}

// a FreeBSD run compiles the pair in the container, reads it, and reports what it read, with
// no conformance container prepared and nothing run.
func TestAFreeBSDRunReadsThePairItCompiledAndSaysWhatItReads(t *testing.T) {
	repo := t.TempDir()

	tl, out := aBuildCompilingInAContainer(t, repo, "freebsd-amd64", func(t *testing.T, dir string, name string) {
		anELFProgramWrittenTo(t, dir, name, theMeasuredFreeBSDPair)
	})

	if _, err := tl.runFFmpeg(options{target: "freebsd-amd64"}, out); err != nil {
		t.Fatalf("the build refused the pair its own record accepts: %v\n%s", err, out.String())
	}

	said := out.String()

	if !strings.Contains(said, "read") {
		t.Errorf("the run never said it read the pair it compiled:\n%s", said)
	}

	for _, read := range theFreeBSDReads() {
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

	if !strings.Contains(said, "compiled and not run") {
		t.Errorf("the compile step does not say the pair was compiled and not run:\n%s", said)
	}

	paths := ffmpegPathsFor(repo, bundle.ReadManifest(), "freebsd-amd64")

	for _, name := range []string{"ffmpeg.gz", "ffprobe.gz"} {
		if !aFileIsThere(paths.cachedProgram(name)) {
			t.Errorf("the read pair was not cached at %s", paths.cachedProgram(name))
		}

		if !aFileIsThere(filepath.Join(paths.bundle, name)) {
			t.Errorf("the read pair was not published into the bundle at %s", paths.bundle)
		}
	}

	if !aFileIsThere(filepath.Join(paths.bundle, "FFMPEG.txt")) {
		t.Errorf("the bundle carries no licence text for the pair the build read: %s", paths.bundle)
	}
}

// a pair failing one read is refused before any of it is cached, licensed, or published: the
// pair here is the published linux-amd64 one, left by a cross compile that never read its
// sysroot.
func TestAFreeBSDRunKeepsNothingForAPairItsReadRefuses(t *testing.T) {
	repo := t.TempDir()

	tl, out := aBuildCompilingInAContainer(t, repo, "freebsd-amd64", func(t *testing.T, dir string, name string) {
		anELFProgramWrittenTo(t, dir, name, thePairThisBuildCompilesForLinux)
	})

	_, err := tl.runFFmpeg(options{target: "freebsd-amd64"}, out)
	if err == nil {
		t.Fatal("the run adopted a pair its own read refused")
	}

	if !strings.Contains(err.Error(), "GNU (3)") {
		t.Errorf("the refusal %q does not name the operating system ABI the pair it compiled says", err.Error())
	}

	if !strings.Contains(err.Error(), "runs no freebsd-amd64 program") {
		t.Errorf("the refusal %q does not say this build runs no FreeBSD program", err.Error())
	}

	paths := ffmpegPathsFor(repo, bundle.ReadManifest(), "freebsd-amd64")

	for _, name := range []string{"ffmpeg.gz", "ffprobe.gz"} {
		if aFileIsThere(paths.cachedProgram(name)) {
			t.Errorf("a refused pair was cached at %s", paths.cachedProgram(name))
		}

		if aFileIsThere(filepath.Join(paths.bundle, name)) {
			t.Errorf("a refused pair was published at %s", filepath.Join(paths.bundle, name))
		}
	}

	if aFileIsThere(filepath.Join(paths.bundle, "FFMPEG.txt")) {
		t.Errorf("a licence text was written for a pair that was refused: %s", paths.bundle)
	}
}

func theFreeBSDReads() []string {
	return bundle.ReadManifest().Targets["freebsd-amd64"].Conformance.Reads
}
