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
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// The shape rules a supplied ffmpeg pair is adopted under.
//
// The machine that produces the releases runs Linux, and it cannot execute a macOS program or a FreeBSD
// one. So the build reads what it can read — the headers the target's own linker wrote — and requires the
// pair to be the program that target runs, in the form the bundle's rule asks for:
//
//	macos-amd64, macos-arm64  a thin 64-bit Mach-O of that architecture, carrying a code signature, and
//	                          linking only libraries from /usr/lib/, because macOS cannot link a static
//	                          binary and a deployment has nothing to install
//
//	freebsd-amd64             a 64-bit x86-64 ELF branded FreeBSD with no program interpreter, because
//	                          FreeBSD links statically with its own libc and a deployment has nothing to
//	                          install
//
// The three failures a pair built by hand actually carries are a Homebrew bottle, an unsigned Apple
// Silicon binary, and a dynamically linked FreeBSD binary. Each one ships a server that converts nothing,
// and each one is refused here with the rule named, on the machine that produces the release, instead of
// on the deployment that cannot tell what went wrong.
const (
	machOMagic   = 0xfeedfacf // MH_MAGIC_64: a thin 64-bit image, little-endian
	machO32Magic = 0xfeedface // MH_MAGIC: 32-bit, which no release of this project ships
	fatMagic     = 0xcafebabe // a universal binary, holding more than one architecture

	cpuX8664 = 0x01000007 // CPU_TYPE_X86_64
	cpuArm64 = 0x0100000c // CPU_TYPE_ARM64

	lcLoadDylib       = 0x0c       // LC_LOAD_DYLIB
	lcIdDylib         = 0x0d       // LC_ID_DYLIB
	lcLoadWeakDylib   = 0x80000018 // LC_LOAD_WEAK_DYLIB
	lcCodeSignature   = 0x8000001d // LC_CODE_SIGNATURE
	lcReexportDylib   = 0x8000001f // LC_REEXPORT_DYLIB
	lcLoadUpwardDylib = 0x80000023 // LC_LOAD_UPWARD_DYLIB

	machOHeaderSize = 32

	elfMagic        = "\x7fELF"
	elf64Class      = 2 // EI_CLASS: 64-bit
	elfFreeBSDOSABI = 9 // EI_OSABI: ELFOSABI_FREEBSD
	elfX8664Machine = 62 // e_machine: EM_X86_64
	elfInterpSeg    = 3 // PT_INTERP: a program interpreter, which a static binary has no use for

	elfHeaderSize        = 64
	elfProgramHeaderSize = 56

	// maxHeaders is the ceiling on what this build will read out of a file it has been handed as a
	// program: a header claiming more than a megabyte of load commands is not a program.
	maxHeaders = 1 << 20

	// systemLibraryDir is the only place a macOS program this project ships may link from. macOS cannot
	// link statically, so this is the form its "needs nothing installed" rule takes.
	systemLibraryDir = "/usr/lib/"
)

// checkSuppliedProgram reports whether the program at path is the program the given target runs.
//
// It reads the file and never runs it: the build produces the releases on a machine that cannot execute
// these programs, and running them is what the ffmpeg check on the target's own platform is for.
func checkSuppliedProgram(path string, target string) error {
	name := filepath.Base(path)

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("the supplied %s for %s cannot be read: %w", name, target, err)
	}

	if info.Size() == 0 {
		return fmt.Errorf("the supplied %s for %s is empty: a target carries a program, not a placeholder", name, target)
	}

	if info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("the supplied %s for %s is not executable: the server writes it out of its own binary and runs it, so the pair has to be dropped as an executable program", name, target)
	}

	switch target {
	case "macos-amd64":
		return checkMachO(path, name, cpuX8664, target)
	case "macos-arm64":
		return checkMachO(path, name, cpuArm64, target)
	case "freebsd-amd64":
		return checkFreeBSDProgram(path, name, target)
	}

	return fmt.Errorf("%s is not a target this project supplies an ffmpeg for", target)
}

// checkMachO reports whether the program at path is the thin 64-bit Mach-O image the given macOS target
// runs, carrying a code signature and linking nothing the host has to install.
func checkMachO(path string, name string, cpuType uint32, target string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("the supplied %s for %s cannot be read: %w", name, target, err)
	}
	defer f.Close()

	header := make([]byte, machOHeaderSize)
	if _, err := io.ReadFull(f, header); err != nil {
		return fmt.Errorf("the supplied %s for %s is not a Mach-O program: it is shorter than the header a Mach-O program starts with", name, target)
	}

	switch magic := binary.LittleEndian.Uint32(header[0:]); magic {
	case fatMagic:
		return fmt.Errorf("the supplied %s for %s is a universal binary: this project ships one archive per architecture and each binary carries the pair that architecture runs, so build one program per architecture", name, target)
	case machOMagic:
	case machO32Magic:
		return fmt.Errorf("the supplied %s for %s is a 32-bit Mach-O program: the macOS releases this project ships are 64-bit", name, target)
	default:
		return fmt.Errorf("the supplied %s for %s is not a Mach-O program: %s carries the pair its own architecture runs", name, target, target)
	}

	if cpu := binary.LittleEndian.Uint32(header[4:]); cpu != cpuType {
		return fmt.Errorf("the supplied %s is compiled for the %s architecture, and %s runs %s", name, nameOfCPU(cpu), target, nameOfCPU(cpuType))
	}

	commands := make([]byte, binary.LittleEndian.Uint32(header[16:]))
	if len(commands) == 0 || len(commands) > maxHeaders {
		return fmt.Errorf("the supplied %s for %s is not a Mach-O program this build can read: its header says its load commands are %d bytes", name, target, len(commands))
	}

	if _, err := io.ReadFull(f, commands); err != nil {
		return fmt.Errorf("the supplied %s for %s is not a complete Mach-O program: its load commands are shorter than its header says", name, target)
	}

	signed := false

	for i := 0; i+8 <= len(commands); {
		cmd := binary.LittleEndian.Uint32(commands[i:])
		size := int(binary.LittleEndian.Uint32(commands[i+4:]))

		if size < 8 || i+size > len(commands) {
			return fmt.Errorf("the supplied %s for %s is not a Mach-O program this build can read: its load commands do not fit inside what its header describes", name, target)
		}

		switch cmd {
		case lcCodeSignature:
			signed = true
		case lcLoadDylib, lcIdDylib, lcLoadWeakDylib, lcReexportDylib, lcLoadUpwardDylib:
			lib, err := nameOfDylib(commands[i : i+size])
			if err != nil {
				return fmt.Errorf("the supplied %s for %s is not a Mach-O program this build can read: %v", name, target, err)
			}

			if !strings.HasPrefix(lib, systemLibraryDir) {
				return fmt.Errorf("the supplied %s links %s: the ffmpeg a macOS release carries depends on nothing the host has to install, which is what the recipe in the manifest gives you when no external library is enabled and nothing is found in a prefix", name, lib)
			}
		}

		i += size
	}

	if !signed {
		return fmt.Errorf("the supplied %s carries no code signature: an Apple Silicon Mac refuses to run unsigned code, and the server runs its ffmpeg from a file it writes out of its own binary, so sign the pair with codesign -s - before you drop it", name)
	}

	return nil
}

// nameOfDylib is the library one load command refers to. The name starts at the offset the command
// carries and ends at its first NUL, inside the command's own size.
func nameOfDylib(command []byte) (string, error) {
	offset := int(binary.LittleEndian.Uint32(command[8:]))

	if offset < 12 || offset >= len(command) {
		return "", fmt.Errorf("a library reference names its path at offset %d, outside the %d bytes of its own load command", offset, len(command))
	}

	body := command[offset:]

	end := strings.IndexByte(string(body), 0)
	if end < 0 {
		return "", fmt.Errorf("a library reference names a path with no end inside its own %d byte load command", len(command))
	}

	return string(body[:end]), nil
}

// checkFreeBSDProgram reports whether the program at path is a static 64-bit x86-64 ELF branded FreeBSD,
// which is the pair freebsd-amd64 carries: a program the host needs nothing installed for.
func checkFreeBSDProgram(path string, name string, target string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("the supplied %s for %s cannot be read: %w", name, target, err)
	}
	defer f.Close()

	header := make([]byte, elfHeaderSize)
	if _, err := io.ReadFull(f, header); err != nil {
		return fmt.Errorf("the supplied %s for %s is not an ELF program: it is shorter than the header an ELF program starts with", name, target)
	}

	if string(header[0:4]) != elfMagic {
		return fmt.Errorf("the supplied %s for %s is not an ELF program: %s carries the pair its own architecture runs", name, target, target)
	}

	if header[4] != elf64Class {
		return fmt.Errorf("the supplied %s for %s is a 32-bit program: the FreeBSD release this project ships is 64-bit", name, target)
	}

	if osabi := header[7]; osabi != elfFreeBSDOSABI {
		return fmt.Errorf("the supplied %s is branded %s, and %s runs the pair built on FreeBSD: build it on FreeBSD, with FreeBSD's own libc", name, nameOfOSABI(osabi), target)
	}

	if machine := binary.LittleEndian.Uint16(header[18:]); machine != elfX8664Machine {
		return fmt.Errorf("the supplied %s is compiled for the %s architecture, and %s runs x86-64", name, nameOfMachine(machine), target)
	}

	offset := int(binary.LittleEndian.Uint64(header[32:]))
	stride := int(binary.LittleEndian.Uint16(header[54:]))
	count := int(binary.LittleEndian.Uint16(header[56:]))

	if offset < elfHeaderSize || stride < elfProgramHeaderSize || count == 0 {
		return fmt.Errorf("the supplied %s for %s is not an ELF program this build can read: its header describes %d program headers of %d bytes at offset %d", name, target, count, stride, offset)
	}

	if size := stride * count; size > maxHeaders {
		return fmt.Errorf("the supplied %s for %s is not an ELF program this build can read: its header describes %d bytes of program headers", name, target, size)
	}

	if _, err := f.Seek(int64(offset), io.SeekStart); err != nil {
		return fmt.Errorf("the supplied %s for %s is not an ELF program this build can read: %w", name, target, err)
	}

	entries := make([]byte, stride*count)
	if _, err := io.ReadFull(f, entries); err != nil {
		return fmt.Errorf("the supplied %s for %s is not a complete ELF program: its program headers are shorter than its header says", name, target)
	}

	for i := 0; i < count; i++ {
		if kind := binary.LittleEndian.Uint32(entries[i*stride:]); kind == elfInterpSeg {
			return fmt.Errorf("the supplied %s is linked against a program interpreter: the FreeBSD release this project ships carries a statically linked ffmpeg, which needs nothing installed on the host, so build it with the recipe's --extra-ldflags=-static", name)
		}
	}

	return nil
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

func nameOfOSABI(osabi byte) string {
	switch osabi {
	case 0:
		return "nothing at all"
	case 3:
		return "Linux"
	case 9:
		return "FreeBSD"
	case 10:
		return "OpenBSD"
	case 6:
		return "Solaris"
	}

	return fmt.Sprintf("brand %d", osabi)
}

func nameOfMachine(machine uint16) string {
	switch machine {
	case 3:
		return "i386"
	case 62:
		return "x86-64"
	case 180:
		return "aarch64"
	}

	return fmt.Sprintf("0x%x", machine)
}
