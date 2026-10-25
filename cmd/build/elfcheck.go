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
	"encoding/binary"
	"fmt"
	"os"
	"strings"

	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// The ELF half of the conformance reads: header, program headers, comments. No target program
// runs on the host: the file's own words are the proof. Shipped files keep the proof: strip
// leaves the comment section intact.
// see the elf-check-static-rule maintainer note.

const (
	elfClass32 = 1
	elfClass64 = 2

	// header OS/ABI: Debian's ld stamps GNU's on the FreeBSD sysroot, a valid x86-64 ELF
	// that is not FreeBSD: the mistake this read exists to catch.
	elfOSABISysV    = 0
	elfOSABILinux   = 3
	elfOSABIFreeBSD = 9

	elfTypeRel  = 1
	elfTypeExec = 2
	elfTypeDyn  = 3

	elfMachine386     = 3
	elfMachineARM     = 40
	elfMachineX8664   = 62
	elfMachineAArch64 = 183

	elfSegmentDynamic = 2
	elfSegmentInterp  = 3

	// Same fields at the two widths the class names: reading a 32-bit table at 64-bit
	// widths invent numbers, and invented numbers prove nothing.
	elfHeaderSize32  = 52
	elfHeaderSize64  = 64
	elfEntrySize32   = 32
	elfEntrySize64   = 56
	elfSectionSize32 = 40
	elfSectionSize64 = 64

	elfIdentSize = 16
)

// anELFProgram is an ELF file read without running it: header facts, program headers it
// declares, and the comment section split into its flagged lines.
type anELFProgram struct {
	class      byte
	osABI      byte
	fileType   uint16
	machine    uint16
	segments   []uint32
	comments   []string
	hasComment bool
}

// readAnELFProgram reads the file at path and holds it to every read name the record carries,
// in order, refusing the first thing the file does not say. A read name with no rule here is
// refused too: a record cannot promise a proof this build does not know how to make.
func readAnELFProgram(path string, program string, target string, reads []string, record bundle.Target) error {
	file, said := anELFProgramAt(path)
	if said != "" {
		return aReadRefusal(program, target, strings.Join(reads, " "), said,
			"an ELF program whose header, program headers and comment section are there to read")
	}

	for _, read := range reads {
		if err := theELFReadHolds(read, file, program, target, record); err != nil {
			return err
		}
	}

	return nil
}

// theELFReadHolds answers nil when the file says what one read asks, and the sentence the
// operator acts on when it does not.
func theELFReadHolds(read string, file anELFProgram, program string, target string, record bundle.Target) error {
	switch read {
	case "elf64":
		if file.class != elfClass64 {
			return aReadRefusal(program, target, read,
				fmt.Sprintf("is a 32-bit ELF program (%s)", anELFClassName(file.class)),
				"the 64-bit ELF64 program that target is")
		}

	case "exec":
		if file.fileType != elfTypeExec {
			return aReadRefusal(program, target, read,
				fmt.Sprintf("is %s", anELFTypeName(file.fileType)),
				"the ET_EXEC program a finished link produces")
		}

	case "x86_64":
		if file.machine != elfMachineX8664 {
			return aReadRefusal(program, target, read,
				fmt.Sprintf("holds %s machine code", anELFMachineName(file.machine)),
				"the x86-64 (EM_X86_64) machine code that target runs")
		}

	case "osabi_freebsd":
		if file.osABI != elfOSABIFreeBSD {
			return aReadRefusal(program, target, read,
				fmt.Sprintf("says its operating system ABI is %s", anELFOSABIName(file.osABI)),
				fmt.Sprintf("%s, which is what the sysroot the compile reads stamps on everything it links",
					anELFOSABIName(elfOSABIFreeBSD)))
		}

	case "static":
		if len(file.segments) == 0 {
			return aReadRefusal(program, target, read,
				"declares no program headers at all, so nothing about how it was linked can be read out of it",
				"the program headers of a linked pair, which is where a loader would have been named")
		}

		wants := []string{}

		for _, segment := range file.segments {
			if segment == elfSegmentInterp {
				wants = append(wants, "PT_INTERP")
			}

			if segment == elfSegmentDynamic {
				wants = append(wants, "PT_DYNAMIC")
			}
		}

		if len(wants) > 0 {
			return aReadRefusal(program, target, read,
				fmt.Sprintf("declares %s, which is a program that asks for a loader to start it", strings.Join(wants, " and ")),
				askedForTheStaticRead(record))
		}

	case "comment":
		return theLinkTheCommentSectionNames(file, program, target, read, record)

	default:
		return aRecordNamingAReadWithNoRule(target, read)
	}

	return nil
}

// askedForTheStaticRead names the record's ask in the record's own words: a refusal must name
// something the operator can check.
func askedForTheStaticRead(record bundle.Target) string {
	for _, flag := range append(append([]string{}, record.CFlags...), record.LDFlags...) {
		if flag == "-static" || strings.HasPrefix(flag, "-static ") {
			return fmt.Sprintf("a pair the recipe linked with %s, which needs no loader this host could not give it", flag)
		}
	}

	return "a pair linked with no dynamic loader, which is what a host that cannot run the target's programs is left with"
}

// theLinkTheCommentSectionNames holds the pair to both halves of the story: the record's
// ldflags linker, and FreeBSD's crt and libc, which enter the link only if the compile read
// the sysroot the record names. It gathers lines from every object, so a host-linked pair
// shows itself.
func theLinkTheCommentSectionNames(file anELFProgram, program string, target string, read string, record bundle.Target) error {
	linker := theLinkerTheRecordNames(record)

	if linker == "" {
		return aReadRefusal(program, target, read,
			"sits in front of a record that asks what linked it",
			"an ldflag naming the linker the compile is told to link with")
	}

	if !file.hasComment {
		return aReadRefusal(program, target, read,
			"carries no comment section, so nothing in it says what linked it",
			fmt.Sprintf("the comment section of a program linked by %s", linker))
	}

	named := ""

	for _, line := range file.comments {
		if strings.HasPrefix(strings.TrimSpace(line), "Linker:") && strings.Contains(strings.ToLower(line), strings.ToLower(linker)) {
			named = line

			break
		}
	}

	if named == "" {
		return aReadRefusal(program, target, read,
			fmt.Sprintf("names nothing in its comment section that names %s: it names %s", linker, strings.Join(file.comments, ", ")),
			fmt.Sprintf("the linker its ldflags name, linked against the sysroot at %s", theSysrootTheRecordNames(record)))
	}

	for _, line := range file.comments {
		if strings.Contains(line, "FreeBSD clang version") {
			return nil
		}
	}

	return aReadRefusal(program, target, read,
		fmt.Sprintf("names %s in its comment section, and names no FreeBSD build among %s", named, strings.Join(file.comments, ", ")),
		fmt.Sprintf("the FreeBSD startup file and library from %s, which are in the link only when the compile read the sysroot at %s",
			record.SysrootImage, theSysrootTheRecordNames(record)))
}

// anELFProgramAt reads a file's header, program headers, and comment section; on a non-ELF
// file or broken tables it answers with no program and with what the file did say, in refusal
// words: an unread pair needs a stated reason.
func anELFProgramAt(path string) (anELFProgram, string) {
	body, err := os.ReadFile(path)
	if err != nil {
		return anELFProgram{}, fmt.Sprintf("is not there to read: %v", err)
	}

	if len(body) < elfIdentSize {
		return anELFProgram{}, fmt.Sprintf("is %d bytes long, shorter than the ident every ELF program starts with", len(body))
	}

	if body[0] != 0x7f || body[1] != 'E' || body[2] != 'L' || body[3] != 'F' {
		return anELFProgram{}, fmt.Sprintf("is not an ELF program: its first four bytes are % 02x", body[:4])
	}

	if body[4] != elfClass32 && body[4] != elfClass64 {
		return anELFProgram{}, fmt.Sprintf("names the ELF class %d, which is neither 32-bit nor 64-bit", body[4])
	}

	if body[5] != 1 {
		return anELFProgram{}, "is a big-endian ELF program, and this build reads the little-endian ones"
	}

	class64 := body[4] == elfClass64

	headerSize, entrySize, sectionSize := elfHeaderSize32, elfEntrySize32, elfSectionSize32
	if class64 {
		headerSize, entrySize, sectionSize = elfHeaderSize64, elfEntrySize64, elfSectionSize64
	}

	if len(body) < headerSize {
		return anELFProgram{}, fmt.Sprintf("is shorter than the header an ELF program starts with: %d bytes", len(body))
	}

	file := anELFProgram{class: body[4], osABI: body[7]}

	header := &theELFBytes{body: body, at: 16, class64: class64}

	file.fileType = header.u16()
	file.machine = header.u16()

	header.u32()
	header.word()

	phoff := header.word()
	shoff := header.word()

	header.u32()

	ehsize := header.u16()
	phentsize := header.u16()
	phnum := header.u16()
	shentsize := header.u16()
	shnum := header.u16()
	shstrndx := header.u16()

	if int(ehsize) < headerSize {
		return anELFProgram{}, fmt.Sprintf("says its own header is %d bytes, which is shorter than the %d bytes an %s header is",
			ehsize, headerSize, anELFClassName(body[4]))
	}

	if int(phentsize) < entrySize {
		return anELFProgram{}, fmt.Sprintf("says its %d program headers are %d bytes each, which is shorter than the %d bytes an %s program header is",
			phnum, phentsize, entrySize, anELFClassName(body[4]))
	}

	if int(shentsize) < sectionSize {
		return anELFProgram{}, fmt.Sprintf("says its %d section headers are %d bytes each, which is shorter than the %d bytes an %s section header is",
			shnum, shentsize, sectionSize, anELFClassName(body[4]))
	}

	if end := phoff + uint64(phnum)*uint64(phentsize); phoff != 0 && uint64(len(body)) < end {
		return anELFProgram{}, fmt.Sprintf("names %d program headers at %d, and the file is %d bytes long: the table it names is not in it",
			phnum, phoff, len(body))
	}

	if end := shoff + uint64(shnum)*uint64(shentsize); shoff != 0 && uint64(len(body)) < end {
		return anELFProgram{}, fmt.Sprintf("names %d section headers at %d, and the file is %d bytes long: the table it names is not in it",
			shnum, shoff, len(body))
	}

	for i := 0; phoff != 0 && i < int(phnum); i++ {
		file.segments = append(file.segments, binary.LittleEndian.Uint32(body[int(phoff)+i*int(phentsize):]))
	}

	if shoff == 0 || shnum == 0 {
		return file, ""
	}

	if int(shstrndx) >= int(shnum) {
		return anELFProgram{}, fmt.Sprintf("names section %d as its section name table, and it carries %d sections",
			shstrndx, shnum)
	}

	shstrtab := int(shoff) + int(shstrndx)*int(shentsize)

	strOff := int(fieldAt(body, shstrtab+sectionOffsetField(class64), class64))
	strSize := int(fieldAt(body, shstrtab+sectionSizeField(class64), class64))

	if strOff < 0 || strSize < 0 || strOff+strSize > len(body) {
		return anELFProgram{}, fmt.Sprintf("names its section name table at %d for %d bytes, and the file is %d bytes long",
			strOff, strSize, len(body))
	}

	for i := 0; i < int(shnum); i++ {
		entry := int(shoff) + i*int(shentsize)

		nameAt := int(binary.LittleEndian.Uint32(body[entry:]))
		if nameAt >= strSize {
			return anELFProgram{}, fmt.Sprintf("names a section at %d in a section name table that is %d bytes long", nameAt, strSize)
		}

		name := anELFString(body[strOff+nameAt:])

		if name != ".comment" {
			continue
		}

		off, size := int(binary.LittleEndian.Uint32(body[entry+sectionOffsetField(class64):])), int(fieldAt(body, entry+sectionSizeField(class64), class64))
		if off < 0 || size < 0 || off+size > len(body) {
			return anELFProgram{}, fmt.Sprintf("names its comment section at %d for %d bytes, and the file is %d bytes long",
				off, size, len(body))
		}

		file.hasComment = true

		for _, line := range strings.Split(string(body[off:off+size]), "\x00") {
			if line != "" {
				file.comments = append(file.comments, line)
			}
		}

		break
	}

	return file, ""
}

// A section header's offset and size fields move with the class: fixed at one width, a 32-bit
// pair's comment section reads as garbage.
func sectionOffsetField(class64 bool) int {
	if class64 {
		return 24
	}

	return 16
}

func sectionSizeField(class64 bool) int {
	if class64 {
		return 32
	}

	return 20
}

func fieldAt(body []byte, at int, class64 bool) uint64 {
	if class64 {
		return binary.LittleEndian.Uint64(body[at:])
	}

	return uint64(binary.LittleEndian.Uint32(body[at:]))
}

// anELFString reads one NUL-terminated string out of a string table.
func anELFString(body []byte) string {
	if at := strings.IndexByte(string(body), 0); at >= 0 {
		return string(body[:at])
	}

	return string(body)
}

// theELFBytes reads the fields of one ELF header, at the width that file's class says they have.
type theELFBytes struct {
	body    []byte
	at      int
	class64 bool
}

func (e *theELFBytes) u16() uint16 {
	v := binary.LittleEndian.Uint16(e.body[e.at:])
	e.at += 2

	return v
}

func (e *theELFBytes) u32() uint32 {
	v := binary.LittleEndian.Uint32(e.body[e.at:])
	e.at += 4

	return v
}

func (e *theELFBytes) word() uint64 {
	if e.class64 {
		v := binary.LittleEndian.Uint64(e.body[e.at:])
		e.at += 8

		return v
	}

	v := uint64(binary.LittleEndian.Uint32(e.body[e.at:]))
	e.at += 4

	return v
}

// aReadRefusal builds every refusal the reads make: what the file said, what the record asked,
// and that the build host ran nothing, so the operator knows which of the record's names the
// pair failed.
func aReadRefusal(program string, target string, read string, said string, asked string) error {
	return fmt.Errorf("the compiled %s for %s %s; its record names %s, which asks for %s, and the machine that compiled it runs no %s program",
		program, target, said, read, asked, target)
}

// theLinkerTheRecordNames is the linker the record's ldflags tell the compile to link with, which is the one
// the program's comment section has to name for the record's provenance claim to be checked against the file.
func theLinkerTheRecordNames(record bundle.Target) string {
	for _, flag := range record.LDFlags {
		if value := strings.TrimPrefix(flag, "-fuse-ld="); value != flag {
			return value
		}
	}

	return ""
}

// theSysrootTheRecordNames is the sysroot the compile was told to read, which is what a pair that carries the
// wrong libraries was linked against instead.
func theSysrootTheRecordNames(record bundle.Target) string {
	for _, flag := range append(append([]string{}, record.CFlags...), record.LDFlags...) {
		if value := strings.TrimPrefix(flag, "--sysroot="); value != flag {
			return value
		}
	}

	if record.SysrootImage != "" {
		return record.SysrootImage
	}

	return "what its record names"
}

// The names below are the ones the header itself writes, so an operator told a pair "is ET_DYN" can look for
// ET_DYN with readelf and find the same fact the build found.
func anELFClassName(class byte) string {
	switch class {
	case elfClass32:
		return "ELF32"
	case elfClass64:
		return "ELF64"
	}

	return fmt.Sprintf("class %d", class)
}

func anELFTypeName(fileType uint16) string {
	switch fileType {
	case 0:
		return "ET_NONE"
	case elfTypeRel:
		return "ET_REL, an object file that was never linked"
	case elfTypeExec:
		return "ET_EXEC"
	case elfTypeDyn:
		return "ET_DYN, a program that wants a loader"
	case 6:
		return "ET_CORE"
	}

	return fmt.Sprintf("the ELF type %d", fileType)
}

func anELFMachineName(machine uint16) string {
	switch machine {
	case elfMachine386:
		return "i386 (EM_386)"
	case elfMachineARM:
		return "ARM (EM_ARM)"
	case elfMachineX8664:
		return "x86-64 (EM_X86_64)"
	case elfMachineAArch64:
		return "AArch64 (EM_AARCH64)"
	}

	return fmt.Sprintf("machine %d", machine)
}

func anELFOSABIName(osABI byte) string {
	switch osABI {
	case elfOSABISysV:
		return "System V (0)"
	case 2:
		return "NetBSD (2)"
	case elfOSABILinux:
		return "GNU (3)"
	case 6:
		return "Solaris (6)"
	case elfOSABIFreeBSD:
		return "FreeBSD (9)"
	}

	return fmt.Sprintf("ABI %d", osABI)
}
