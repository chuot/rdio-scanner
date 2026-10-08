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

package buildinfo

import (
	"fmt"
	"strings"
)

// CoreOf takes the release out of a version a build carries.
//
// A prerelease says the release has not happened yet, and a commit hash says which build of it this
// was; neither makes it a different release. So a version somebody hands over can be compared with a
// pinned one by its core, and a build that carries 8.0.0-beta1+gdb79311 satisfies a contract pinned
// to 8.0.0 while one that carries 9.0.0 does not.
//
// Something with no core at its start is refused rather than answered with an empty string: a
// comparison between two empty strings looks exactly like a pass.
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
