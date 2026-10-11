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

package application

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestApplicationImportsInwardOnly enforces the criterion on every run of the suite: the
// application layer may use the domain, its own ports, and the standard library, but
// never infrastructure, presentation, or a third-party module.
func TestApplicationImportsInwardOnly(t *testing.T) {
	const (
		module        = "rdio-scanner"
		allowedDomain = module + "/internal/domain"
		allowedPorts  = module + "/internal/application"
	)

	fset := token.NewFileSet()

	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		if !strings.Contains(path, "/application") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}

		for _, imp := range file.Imports {
			imported := strings.Trim(imp.Path.Value, `"`)

			if !strings.HasPrefix(imported, module) {
				// Anything that is not the standard library is a third-party module:
				// module paths always contain a dot, standard-library paths never do.
				if strings.Contains(imported, ".") {
					t.Errorf("%s imports %s; the application layer may not import third-party modules", path, imported)
				}

				continue
			}

			if imported == allowedDomain || strings.HasPrefix(imported, allowedPorts) {
				continue
			}

			t.Errorf("%s imports %s; the application layer may import the domain and its own ports only", path, imported)
		}

		return nil
	})

	if err != nil {
		t.Fatalf("walking the application packages: %v", err)
	}
}
