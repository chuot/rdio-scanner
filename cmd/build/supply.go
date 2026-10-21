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
)

// The rules the macOS ffmpeg binaries are admitted under — the ones this build compiled with a Mac's own
// toolchain, and the ones a maintainer built by hand and dropped.
//
// The machine that produces the releases runs Linux, and it cannot execute a macOS program. So the build
// reads what it can read — the headers the target's own linker wrote — and requires the binaries to be the
// programs that target runs, in the form the bundle's rule asks for:
//
//	macos-amd64, macos-arm64  a thin 64-bit Mach-O holding that architecture's machine code, because an
//	                          Apple Silicon Mac runs arm64 and x86-64 alike and running the binaries
//	                          cannot say which one the compiler produced
//
// Every other target this project ships an ffmpeg for is compiled in a container, which produces the
// program that target runs; nothing is admitted into such a target from outside the compile that made it.
//
// The one thing this build reads out of a program it did not compile is which machine code the file
// holds. The failures the audit layers used to name — a Homebrew bottle, an unsigned Apple Silicon binary —
// are the business of the machine that built the program, and the licence text the archive carries names
// that machine. The architecture is not: an Apple Silicon Mac runs arm64 and x86-64 alike, so running the
// program it compiled for the other architecture cannot tell it what the compiler produced.
const (
	machOMagic   = 0xfeedfacf // MH_MAGIC_64: a thin 64-bit image, little-endian
	machO32Magic = 0xfeedface // MH_MAGIC: 32-bit, which no release of this project ships
	fatMagic     = 0xcafebabe // a universal binary, holding more than one architecture

	cpuX8664 = 0x01000007 // CPU_TYPE_X86_64
	cpuArm64 = 0x0100000c // CPU_TYPE_ARM64

	machOHeaderSize = 32
)

// checkSuppliedProgram reports whether the program at path is the program the given target runs.
//
// It reads the header and never runs the program: this is read alike of a program the build compiled and
// of one the maintainer dropped by hand, and the machine code the file holds is the one fact a run
// cannot give the machine that compiled across the two macOS architectures.
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
		return fmt.Errorf("the supplied %s for %s is not executable: the server writes it out of its own binary and runs it, so the ffmpeg binaries have to be dropped as executable programs", name, target)
	}

	switch target {
	case "macos-amd64":
		return checkMachO(path, name, cpuX8664, target)
	case "macos-arm64":
		return checkMachO(path, name, cpuArm64, target)
	}

	return fmt.Errorf("%s is not a target this project supplies an ffmpeg for", target)
}

// checkMachO reports whether the program at path is the thin 64-bit Mach-O image the given macOS target
// runs.
//
// The architecture is the rule the crossed compile needs: an Apple Silicon Mac compiling the Intel
// binaries runs what it produced under Rosetta 2 and an arm64 program just as well, so only the file
// itself says which machine code the compiler wrote.
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

	// A universal binary's magic is written big-endian by every linker that produces one, whatever the
	// byte order of the images inside it; a thin image's magic is the byte order of that image. So the
	// fat magic is looked for in the big-endian reading of the first four bytes, and the thin magics in
	// the little-endian one.
	if binary.BigEndian.Uint32(header[0:]) == fatMagic {
		return fmt.Errorf("the supplied %s for %s is a universal binary: this project ships one archive per architecture and each binary carries the ffmpeg binaries that architecture runs, so build one program per architecture", name, target)
	}

	switch magic := binary.LittleEndian.Uint32(header[0:]); magic {
	case machOMagic:
	case machO32Magic:
		return fmt.Errorf("the supplied %s for %s is a 32-bit Mach-O program: the macOS releases this project ships are 64-bit", name, target)
	default:
		return fmt.Errorf("the supplied %s for %s is not a Mach-O program: %s carries the ffmpeg binaries its own architecture runs", name, target, target)
	}

	if cpu := binary.LittleEndian.Uint32(header[4:]); cpu != cpuType {
		return fmt.Errorf("the supplied %s is compiled for the %s architecture, and %s runs %s", name, nameOfCPU(cpu), target, nameOfCPU(cpuType))
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
// installed and does not guess at what it can execute: an Intel Mac holding the arm64 binaries, and a
// Linux host holding the macOS binaries, are answered by the programs not starting.
func theBinariesTheHostCannotRun(target string, paths ffmpegPaths, machine host, err error) error {
	return fmt.Errorf("the %s ffmpeg binaries do not run on this machine (%s): %w\n"+
		"the ffmpeg binaries are compiled where they run: run go run ./cmd/build ffmpeg %s on a %s host, and the binaries in %s are the ones this host cannot run",
		target, releaseTargetWordFor(machine.goos, machine.goarch), err, target, target, paths.bundle)
}

// proveSuppliedBinaries runs the ffmpeg binaries sitting in one target's bundle directory, on the machine
// standing here, and says what it saw.
//
// The binaries are read before they are run: the machine code the file holds is the one fact a run cannot
// give the machine that compiled across the two macOS architectures, because an Apple Silicon Mac runs
// arm64 and x86-64 alike.
//
// The compile a macOS host runs and the run that adopts a drop both run the binaries through this: two gates
// would be two standards, and a release adopting on the weaker one is not visible from outside the build.
func (t tool) proveSuppliedBinaries(target string, paths ffmpegPaths, stdout io.Writer) (theRunOfTheBinaries, error) {
	drop := suppliedDropFor(paths, target)

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if !aFileIsThere(drop[name]) {
			return theRunOfTheBinaries{}, fmt.Errorf("there is no %s in %s to run: build the ffmpeg binaries on a %s host, drop ffmpeg and ffprobe in %s, and run this again",
				name, paths.bundle, target, paths.bundle)
		}

		if err := checkSuppliedProgram(drop[name], target); err != nil {
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
// machine can run: the run that adopts binaries runs them on their own platform, so nothing stands
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
