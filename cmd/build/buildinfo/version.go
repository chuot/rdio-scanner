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

// Package buildinfo is the build tool's own version rule: the core a repository declares in
// VERSION.txt, and the pre-release state of the commit being built. It knows nothing about
// processes, and nothing in the application layers imports it — the tool asks it what version to
// inject, and the tests that hold the wire contract ask it what version that would have been.
package buildinfo

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// assumedCore is what a repository that has never said what version it is gets: a version no
// release ever had, which no client mistakes for one.
const assumedCore = "0.0.0"

// corePattern is a semantic versioning core and nothing else: three dot-separated numbers, each
// either a zero or a run of digits that does not start with one. A prerelease, build metadata, a
// leading v, or a second version on the next line is not a core.
var corePattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// ReadCore reads the core version out of the file a repository declares it in.
//
// A file that is not there is a repository that has not said, and resolves to 0.0.0. A file that is
// there and holds something else is a mistake somebody made while meaning well, and is refused: a
// build that repaired "v8.0.0" into 8.0.0 would be shipping a version the repository never wrote
// down.
func ReadCore(path string) (string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return assumedCore, nil
	}

	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}

	core := strings.TrimSpace(strings.TrimPrefix(string(b), "\ufeff"))

	if !corePattern.MatchString(core) {
		return "", fmt.Errorf("%s must hold one bare core version in the form X.Y.Z, and holds %q", path, core)
	}

	return core, nil
}
