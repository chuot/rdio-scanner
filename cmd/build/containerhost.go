package main

import (
	"fmt"
	"strings"
)

// containerBaseImage is the image the finished image is taken from: the shipping stage of the
// Containerfile starts from it, and a run that proves this host can build for a platform runs that
// same image for that platform. A case keeps the two equal.
const containerBaseImage = "docker.io/alpine:3.24.2"

// foreignArchitectureLauncher is the program a host runs another architecture's instructions with.
// It is what a host that cannot run one is missing, and installing it is a host change the operator
// makes.
const foreignArchitectureLauncher = "qemu-user-static"

// dockerOCIExportDrivers are the buildx drivers able to write an OCI layout file: the docker driver
// answers a multi-architecture export with a refusal naming itself, and only these two answer it
// with the file.
var dockerOCIExportDrivers = []string{"docker-container", "remote"}

// theBuildxCreateCommand is the line an operator runs to give a host a builder able to export an
// index. The build names it and never runs it: a builder is host state, and host state is the
// operator's.
const theBuildxCreateCommand = "docker buildx create --name rdio-scanner --driver docker-container --use"

// checkContainerAssembly reads the host for what the resolved program needs to assemble an index over
// the targets the run resolved, and answers a host that lacks one of them with the command that
// supplies it. It creates no builder, installs nothing, and changes no host setting.
func (t tool) checkContainerAssembly(targets []releaseTarget, program string) error {
	if program == dockerProgram {
		return t.checkDockerAssembly(targets)
	}

	return t.checkPodmanAssembly(targets, program)
}

// checkPodmanAssembly proves that each architecture this machine is not can be executed here, by
// running the image the finished image is taken from for that platform and asking it what machine it
// is. The architecture this machine is needs no proof: the run is already running on it.
func (t tool) checkPodmanAssembly(targets []releaseTarget, program string) error {
	machine := t.theMachine()

	for _, tr := range targets {
		if tr.goarch == machine.goarch {
			continue
		}

		target := tr.targetWord()
		platform := containerPlatformFor(target)
		argv := []string{program, "run", "--rm", "--platform=" + platform, containerBaseImage, "uname", "-m"}

		if _, err := t.theRun(argv, nil); err != nil {
			return fmt.Errorf("%s cannot run %s's platform %s on this machine: %s answered %v, and %s is the launcher a host runs another architecture's instructions with",
				program, target, platform, strings.Join(argv, " "), err, foreignArchitectureLauncher)
		}
	}

	return nil
}

// checkDockerAssembly reads the tool a docker index is assembled with, the builder the host is already
// using, and what that builder can target. The readings are taken in the order they cost the least, so
// a host missing the cheaper one is refused without being asked the dearer question.
func (t tool) checkDockerAssembly(targets []releaseTarget) error {
	if _, err := t.theRun([]string{dockerProgram, "buildx", "version"}, nil); err != nil {
		return fmt.Errorf("docker buildx version answered nothing on this machine: a container run asked for all three architectures assembles one index through buildx, and installing the buildx plugin is the host change that supplies it")
	}

	out, err := t.theRun([]string{dockerProgram, "buildx", "inspect", "--bootstrap"}, nil)
	if err != nil {
		return fmt.Errorf("docker buildx inspect answered nothing on this machine: a run asked for all three architectures reads which driver its builder runs and which platforms that builder declares, and %s is the line that gives a host a builder to read", theBuildxCreateCommand)
	}

	report := string(out)

	if driver := theBuilderDriver(report); !carriesWord(dockerOCIExportDrivers, driver) {
		return fmt.Errorf("docker's builder runs driver %q, which cannot export an OCI image layout: a run asked for all three architectures writes its index as one layout file, and %s is the line that gives a host a builder able to export it",
			driver, theBuildxCreateCommand)
	}

	declared := theBuilderPlatforms(report)

	for _, tr := range targets {
		platform := containerPlatformFor(tr.targetWord())

		if carriesWord(declared, platform) {
			continue
		}

		return fmt.Errorf("docker's builder declares no %s, which is the platform the %s image is built for: %s is the launcher that runs that architecture's instructions, and %s starts a builder that declares the platforms this host's launcher makes available",
			platform, tr.targetWord(), foreignArchitectureLauncher, theBuildxCreateCommand)
	}

	return nil
}

// theBuilderDriver and theBuilderPlatforms are the two facts about a builder a run assembling an index
// has to know, read out of the report buildx writes for it. A report naming no driver, or no platform,
// answers as one naming neither: a builder this run cannot assemble with.
func theBuilderDriver(report string) string {
	for _, line := range strings.Split(report, "\n") {
		if value, found := strings.CutPrefix(strings.TrimSpace(line), "Driver:"); found {
			return strings.TrimSpace(value)
		}
	}

	return ""
}

func theBuilderPlatforms(report string) []string {
	platforms := []string{}

	for _, line := range strings.Split(report, "\n") {
		value, found := strings.CutPrefix(strings.TrimSpace(line), "Platforms:")
		if !found {
			continue
		}

		for _, one := range strings.Split(value, ",") {
			if one = strings.TrimSpace(one); one != "" {
				platforms = append(platforms, one)
			}
		}
	}

	return platforms
}

// carriesWord reports whether a list read off the host names one word exactly.
func carriesWord(words []string, want string) bool {
	for _, one := range words {
		if one == want {
			return true
		}
	}

	return false
}
