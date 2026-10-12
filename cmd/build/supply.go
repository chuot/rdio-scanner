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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
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
// The one thing this build reads out of a program it did not compile is which machine code the file
// holds. The failures the audit layers used to name — a Homebrew bottle, an unsigned Apple Silicon
// binary, a dynamically linked FreeBSD binary — are the business of the machine that built the program,
// and the licence text the archive carries names that machine. The architecture is not: an Apple Silicon
// Mac runs arm64 and x86-64 alike, so running the program it compiled for the other architecture cannot
// tell it what the compiler produced.
const (
	machOMagic   = 0xfeedfacf // MH_MAGIC_64: a thin 64-bit image, little-endian
	machO32Magic = 0xfeedface // MH_MAGIC: 32-bit, which no release of this project ships
	fatMagic     = 0xcafebabe // a universal binary, holding more than one architecture

	cpuX8664 = 0x01000007 // CPU_TYPE_X86_64
	cpuArm64 = 0x0100000c // CPU_TYPE_ARM64

	machOHeaderSize = 32

	elfMagic        = "\x7fELF"
	elf64Class      = 2  // EI_CLASS: 64-bit
	elfFreeBSDOSABI = 9  // EI_OSABI: ELFOSABI_FREEBSD
	elfX8664Machine = 62 // e_machine: EM_X86_64

	elfHeaderSize = 64
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
	case "freebsd-amd64":
		return checkFreeBSDProgram(path, name, target)
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

// checkFreeBSDProgram reports whether the program at path is a 64-bit x86-64 ELF branded FreeBSD, which is
// the machine code freebsd-amd64 runs. Whether it links FreeBSD's loader and libraries is the business of
// the FreeBSD box that built it, and the licence text names that box.
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
		return fmt.Errorf("the supplied %s for %s is not an ELF program: %s carries the ffmpeg binaries its own architecture runs", name, target, target)
	}

	// The identification area is the magic, the class, the byte order, the version, the brand, the ABI
	// version, and then padding: the class is the fifth byte of the header and the brand the eighth.
	if header[4] != elf64Class {
		return fmt.Errorf("the supplied %s for %s is a 32-bit program: the FreeBSD release this project ships is 64-bit", name, target)
	}

	if osabi := header[7]; osabi != elfFreeBSDOSABI {
		return fmt.Errorf("the supplied %s is branded %s, and %s runs the ffmpeg binaries built on FreeBSD: build them on FreeBSD, with FreeBSD's own libc", name, nameOfOSABI(osabi), target)
	}

	if machine := binary.LittleEndian.Uint16(header[18:]); machine != elfX8664Machine {
		return fmt.Errorf("the supplied %s is compiled for the %s architecture, and %s runs x86-64", name, nameOfMachine(machine), target)
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

// The ffmpeg check is the gate this project already trusts, and for a supplied pair it is also the only
// evidence it can have: the build cannot run a Mach-O or a FreeBSD program, so the check has to run on
// the pair's own platform and leave a record the build can read.
//
// The record is specific so it cannot be reused: it names the target it ran for, both programs by
// digest, the machine it ran on, and the six conversions it proved. A proof recorded for a pair the build
// has since replaced is not a proof of that pair, and a proof recorded on a machine that is not the
// target's is a proof of a program the release does not ship.
//
// It lives in the build's transient tree, not in the bundle directory: the bundle directory is what a
// server binary embeds, and a proof is the build's own business.
type anFfmpegCheck struct {
	Target      string        `json:"target"`
	Host        string        `json:"host"`
	FFmpeg      string        `json:"ffmpeg"`
	FFProbe     string        `json:"ffprobe"`
	Version     string        `json:"version"`
	Checked     string        `json:"checked"`
	Codecs      codecRegistry `json:"codecs"`
	Conversions []production  `json:"conversions"`
}

func ffmpegCheckFile(repo string, target string) string {
	return filepath.Join(repo, ".build", "ffmpeg", "checks", target+".json")
}

// writeFfmpegCheck records the proof the check produced, named for the target it ran for.
func writeFfmpegCheck(repo string, check anFfmpegCheck) error {
	body, err := json.MarshalIndent(check, "", "  ")
	if err != nil {
		return fmt.Errorf("the ffmpeg check for %s cannot be recorded: %w", check.Target, err)
	}

	dir := filepath.Join(repo, ".build", "ffmpeg", "checks")

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("the ffmpeg check for %s cannot be recorded: %w", check.Target, err)
	}

	if err := os.WriteFile(ffmpegCheckFile(repo, check.Target), append(body, '\n'), 0644); err != nil {
		return fmt.Errorf("the ffmpeg check for %s cannot be recorded: %w", check.Target, err)
	}

	return nil
}

// readFfmpegCheck reads the proof recorded for one target and requires it to be the proof of the pair the
// build is adopting.
func readFfmpegCheck(repo string, target string, ffmpegDigest string, ffprobeDigest string) (anFfmpegCheck, error) {
	body, err := os.ReadFile(ffmpegCheckFile(repo, target))
	if err != nil {
		return anFfmpegCheck{}, fmt.Errorf("%s: %s", missingCheckFor(target), strings.Join(theChecksRecorded(repo), ", "))
	}

	var check anFfmpegCheck

	if err := json.Unmarshal(body, &check); err != nil {
		return anFfmpegCheck{}, fmt.Errorf("the ffmpeg check recorded for %s is not readable: %s: %v", target, ffmpegCheckFile(repo, target), err)
	}

	if check.Target != target {
		return anFfmpegCheck{}, fmt.Errorf("the ffmpeg check recorded for %s names %s: a proof is evidence for the target it ran on and nothing else", target, check.Target)
	}

	if check.FFmpeg != ffmpegDigest || check.FFProbe != ffprobeDigest {
		return anFfmpegCheck{}, fmt.Errorf("the ffmpeg check recorded for %s names the pair %s / %s and the pair here is %s / %s: the proof has to be recorded for the pair whose digest is adopted", target, check.FFmpeg, check.FFProbe, ffmpegDigest, ffprobeDigest)
	}

	if goos, _, _ := strings.Cut(check.Host, "-"); goos != serverGoos(target) {
		return anFfmpegCheck{}, fmt.Errorf("the ffmpeg check recorded for %s ran on %s: run go run ./cmd/build ffmpeg-check %s on a %s host, where the pair runs", target, check.Host, target, target)
	}

	if len(check.Conversions) == 0 {
		return anFfmpegCheck{}, fmt.Errorf("the ffmpeg check recorded for %s names no conversions: the proof is the six conversions the check ran", target)
	}

	// The proof is recorded by the host that compiled the pair, and it is recorded by the build that
	// asks the pair what it registers. A proof from a build that never asked is the proof of a pair
	// nobody looked at, and a proof naming video codecs is that host telling the release it compiled a
	// program this project does not ship.
	if len(check.Codecs.Audio) == 0 {
		return anFfmpegCheck{}, fmt.Errorf("the ffmpeg check recorded for %s names no codecs: run go run ./cmd/build ffmpeg-check %s again, which asks the pair what it registers", target, target)
	}

	if len(check.Codecs.Video) > 0 {
		return anFfmpegCheck{}, fmt.Errorf(
			"the ffmpeg check recorded for %s names video codecs: %s. This project ships an ffmpeg compiled for audio, and the recipe that produces it is internal/infrastructure/ffmpeg/bundle/manifest.json, which enables no video component",
			target, strings.Join(check.Codecs.Video, ", "),
		)
	}

	return check, nil
}

// checkFfmpegCheckHost reports whether a machine is a host the ffmpeg check has to run on.
//
// The check runs the pair, so it runs where the pair runs. A check that quietly accepted a target it
// cannot execute would leave a proof of nothing, and the build would have no way to tell that from a pair
// that converts.
//
// The rule is the operating system, and not the architecture inside it. A macOS pair is proved on a macOS
// host of either architecture: an Apple Silicon Mac executes an Intel program through Rosetta 2, and the
// proof it records is the proof of the program the Intel machine runs. The proof file keeps naming the host
// that recorded it, which is what says which machine proved the pair. A host that cannot execute the pair it
// is asked to prove — an Intel Mac holding the arm64 pair — is answered by the operating system when the
// pair is run, and not by a guess about what the host has installed.
func checkFfmpegCheckHost(target string, machine host) error {
	tr, ok := targetFor(target)
	if !ok {
		return fmt.Errorf("%s is not a release target this project ships", target)
	}

	if machine.goos == tr.goos {
		return nil
	}

	return fmt.Errorf("the ffmpeg check runs the pair, so it runs where the pair runs: run go run ./cmd/build ffmpeg-check %s on a %s host, and this machine is %s",
		target, target, releaseTargetWordFor(machine.goos, machine.goarch))
}

// missingCheckFor is what an operator is told when a supplied target has no proof, and the command that
// produces one.
func missingCheckFor(target string) string {
	return fmt.Sprintf("the ffmpeg check was never run for %s: run go run ./cmd/build ffmpeg-check %s on a %s host, and carry the proof it writes to %s", target, target, target, ".build/ffmpeg/checks/"+target+".json")
}

// theChecksRecorded names the proofs that are in the tree, so a build handed the proof of the other
// architecture says which proof it was handed.
func theChecksRecorded(repo string) []string {
	entries, err := os.ReadDir(filepath.Join(repo, ".build", "ffmpeg", "checks"))
	if err != nil {
		return []string{"no ffmpeg check has been recorded for any target"}
	}

	recorded := []string{}

	for _, entry := range entries {
		name := entry.Name()

		if filepath.Ext(name) != ".json" {
			continue
		}

		recorded = append(recorded, "the proof recorded for "+strings.TrimSuffix(name, ".json"))
	}

	if len(recorded) == 0 {
		return []string{"no ffmpeg check has been recorded for any target"}
	}

	return recorded
}

// runFFmpegCheck proves the pair dropped for one target, on the platform that target runs on, and writes
// down what it saw.
//
// The gate is the one a compiled pair goes through: the converter's own argument list, the recordings the
// server stores, the lengths the filters ask for, the tags the server writes. Here the pair is the pair
// sitting in the bundle directory, and this machine is the container.
//
// The record it leaves is the evidence a pair built by hand has, because the machine that produces the
// releases cannot run it: the release build adopts the pair on the digest the manifest pins and on this
// record, and on nothing else. The compile of a supplied pair proves its own pair through the same code.
func (t tool) runFFmpegCheck(o options, stdout io.Writer) error {
	target := ffmpegTarget(o)

	if err := checkFfmpegCheckHost(target, t.theMachine()); err != nil {
		return err
	}

	manifest := bundle.ReadManifest()

	// The check proves a pair that was dropped. A target this build compiles in a container is proved by
	// the compile itself, inside the container that can run it, and a check pointed at that target would
	// be looking for a raw pair its bundle directory has never held.
	record, found := manifest.Targets[target]

	if !found || record.Produced != suppliedProduction {
		return fmt.Errorf("the ffmpeg check proves a pair dropped for a target this build compiles in a container: %s is compiled here, and go run ./cmd/build ffmpeg %s runs this same check on it inside the container that can run it",
			target, target)
	}

	_, err := t.prove(target, ffmpegPathsFor(t.repo, manifest, target), stdout)

	return err
}

// thePairTheHostCannotRun is what a host is told when the pair it is asked to prove does not run on it.
//
// The proof is the pair's own execution, so a host that cannot execute the pair has no proof to record, and
// the answer it gets names the host that can, the command that proves the pair there, and the directory the
// pair is sitting in. The pair does not move and the operator carries nothing: the proof is recorded where
// the pair runs, and the proof file names the host that recorded it.
//
// The operating system's own refusal is carried inside it. This build does not ask the host what it has
// installed and does not guess at what it can execute: an Intel Mac holding the arm64 pair, and a Linux host
// holding a macOS pair, are answered by the programs not starting.
func thePairTheHostCannotRun(target string, paths ffmpegPaths, machine host, err error) error {
	return fmt.Errorf("the %s pair does not run on this machine (%s): %w\n"+
		"the proof is the pair's own execution, so it is recorded by a host that runs it: "+
		"run go run ./cmd/build ffmpeg-check %s on a %s host, and the pair is in %s",
		target, releaseTargetWordFor(machine.goos, machine.goarch), err, target, target, paths.bundle)
}

// proveSuppliedPair proves the pair sitting in one target's bundle directory, on the machine that runs it,
// and records what it saw.
//
// The pair is read before it is run: a Homebrew bottle, an unsigned program, and a dynamically linked
// FreeBSD program all start on the machine that built them, and a check that ran them would pass them and
// record a proof of a pair no deployment can run.
//
// The record is the evidence a pair built by hand has, and the digests printed here are what the manifest
// pins. The ffmpeg check command proves a dropped pair with this, and the compile of a supplied pair
// proves the pair it just produced with this same code: two gates would be two standards, and a release
// adopting on the weaker one is not visible from outside the build.
func (t tool) proveSuppliedPair(target string, paths ffmpegPaths, stdout io.Writer) (anFfmpegCheck, error) {
	drop := suppliedDropFor(paths, target)

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if !aFileIsThere(drop[name]) {
			return anFfmpegCheck{}, fmt.Errorf("there is no %s in %s to check: build the pair on a %s host, drop ffmpeg and ffprobe in %s, and run this again",
				name, paths.bundle, target, paths.bundle)
		}

		if err := checkSuppliedProgram(drop[name], target); err != nil {
			return anFfmpegCheck{}, err
		}
	}

	work := filepath.Join(t.repo, ".build", "ffmpeg", "checks", target)

	if err := os.MkdirAll(work, 0755); err != nil {
		return anFfmpegCheck{}, fmt.Errorf("the build cannot prepare %s: %w", work, err)
	}

	programs := checkPrograms{FFmpeg: []string{drop["ffmpeg"]}, FFProbe: []string{drop["ffprobe"]}, Work: work}

	run := onThisMachine()

	version, err := theProgramVersion(run, programs.FFmpeg)
	if err != nil {
		return anFfmpegCheck{}, thePairTheHostCannotRun(target, paths, t.theMachine(), err)
	}

	report, err := ffmpegCheck(run, programs, work)
	if err != nil {
		return anFfmpegCheck{}, thePairTheHostCannotRun(target, paths, t.theMachine(), err)
	}

	digests := map[string]string{}

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		body, err := os.ReadFile(drop[name])
		if err != nil {
			return anFfmpegCheck{}, fmt.Errorf("the supplied %s for %s cannot be read: %w", name, target, err)
		}

		digests[name] = sha256Of(body)
	}

	check := anFfmpegCheck{
		Target:      target,
		Host:        runtime.GOOS + "-" + runtime.GOARCH,
		FFmpeg:      digests["ffmpeg"],
		FFProbe:     digests["ffprobe"],
		Version:     version,
		Checked:     time.Now().UTC().Format(time.RFC3339),
		Codecs:      report.Codecs,
		Conversions: report.Productions,
	}

	if err := writeFfmpegCheck(t.repo, check); err != nil {
		return anFfmpegCheck{}, err
	}

	fmt.Fprintf(stdout, "%-7s %d conversions, %d codecs, %s s to %s s\n", "check", report.Conversions,
		len(report.Codecs.Audio),
		strconv.FormatFloat(report.Shortest, 'f', 6, 64),
		strconv.FormatFloat(report.Longest, 'f', 6, 64),
	)

	// The digests are what the manifest has to pin, printed here so the operator copies the pair's own
	// digests and not the ones a rebuild produced.
	fmt.Fprintf(stdout, "%-7s ffmpeg  sha256 %s\n", "digest", check.FFmpeg)
	fmt.Fprintf(stdout, "%-7s ffprobe sha256 %s\n", "digest", check.FFProbe)
	fmt.Fprintf(stdout, "%-7s %s\n", subFFmpegCheck, ffmpegCheckFile(t.repo, target))

	return check, nil
}

// theProgramVersion is the first line the program says about itself, which is the release the proof
// records: the manifest pins one release, and a pair built from another is a pair this release does not
// ship.
func theProgramVersion(run command, program []string) (string, error) {
	body, err := run(starts(program, "-version"), nil)
	if err != nil {
		return "", fmt.Errorf("the pair cannot say what it is: %s -version: %w", program[0], err)
	}

	line, _, _ := strings.Cut(strings.TrimSpace(string(body)), "\n")
	if line == "" {
		return "", fmt.Errorf("the pair says nothing about what it is: %s -version printed nothing", program[0])
	}

	return line, nil
}

// onThisMachine runs one program here, hands it a recording on its input, and hands back what it wrote.
//
// It is the shape the check runs a compiled pair through a container in, used for a pair this machine can
// run: the ffmpeg check for a supplied pair runs on the pair's own platform, so nothing stands between the
// check and the programs.
//
// A program that ran and answered badly answers with its own words, which is what the check reads. A
// program that never started has no words of its own: the operating system refused to start it, and that
// refusal is the whole answer, so it is carried out and not thrown away. It is the answer a host gives that
// was handed a pair of the other macOS architecture.
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
