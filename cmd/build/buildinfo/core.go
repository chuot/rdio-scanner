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

package buildinfo

import (
	"fmt"
	"strings"
)

// CoreOf takes the release out of a version a build carries.
// A prerelease or commit hash says which build of a release this
// is, never a different release, so a pinned contract compares cores,
// and text with no core at its start is refused.
func CoreOf(version string) (string, error) {
	core := version

	if at := strings.IndexAny(core, "-+"); at >= 0 {
		core = core[:at]
	}

	if !corePattern.MatchString(core) {
		return "", fmt.Errorf("%q is not a version: it carries no core in the form X.Y.Z", version)
	}

	return core, nil
}
