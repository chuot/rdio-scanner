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

// Package buildinfo holds the build tool's version rule: the core
// declared in VERSION.txt and the state of the commit being built;
// it starts no processes and no application layer imports it.
package buildinfo

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// assumedCore is what a repository that never declared a version
// gets: a version no release ever had, so no client mistakes it.
const assumedCore = "0.0.0"

// corePattern is a semantic versioning core and nothing else: three
// dot-separated numbers with no leading zeros; no prerelease,
// metadata, leading v, or second line.
var corePattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// ReadCore reads the core version out of the file a repository
// declares it in. A file that is not there resolves to 0.0.0; a
// file holding anything else is refused rather than repaired.
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
