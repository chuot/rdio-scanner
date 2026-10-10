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

// Command build compiles this project: the listener application, then the administration
// application, then the server that carries both. It resolves the version the build carries from
// VERSION.txt and from git, and injects it into the binary through the linker and into the bundles
// through the environment, so that no source file has to be edited to say what version is being
// built.
package main

import (
	"fmt"
	"os"
)

func main() {
	repo, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(os.Stdout, "error  %v\n", err)
		os.Exit(1)
	}

	t := tool{
		repo:         repo,
		read:         readSource,
		resolve:      resolveExecutable,
		installed:    directoryExists,
		execute:      runStep,
		archive:      writeArchive,
		clean:        cleanTree,
		state:        buildState,
		releaseFiles: archiveFiles,
	}

	t.ffmpeg = t.runFFmpeg

	os.Exit(t.run(os.Args[1:], os.Stdout))
}
