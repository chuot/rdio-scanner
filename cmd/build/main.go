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

// Command build compiles this project: the scanner webapp, then the administration
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
		// The line that says the run stopped reaches the operator's standard error: a tree the tool cannot
		// even locate is a failure, and a failure written where the progress goes is a failure a log of the
		// build reads as progress.
		fmt.Fprintf(os.Stderr, "error  %v\n", err)
		os.Exit(1)
	}

	t := newTool(repo)

	os.Exit(t.run(os.Args[1:], os.Stdout))
}
