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

package domain

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDomainImportsStandardLibraryOnly enforces CA-003.1 on every run of the suite,
// so the layer boundary cannot regress unnoticed.
func TestDomainImportsStandardLibraryOnly(t *testing.T) {
	const module = "rdio-scanner"

	fset := token.NewFileSet()

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}

		for _, imp := range file.Imports {
			imported := strings.Trim(imp.Path.Value, `"`)

			if strings.HasPrefix(imported, module) || strings.Contains(imported, ".") {
				t.Errorf("%s imports %s; the domain layer may import the standard library only", path, imported)
			}
		}

		return nil
	})

	if err != nil {
		t.Fatalf("walking the domain package: %v", err)
	}
}

// TestTheDomainPackageImportsNoMathRand (REQ-013, CA-013.1): the signing secret is
// the value every session token is signed with, and math/rand is a clock-seeded
// sequence — an attacker who knows roughly when the server first started can
// reproduce the secret that start wrote, and a signing secret is exactly the value
// an attacker wants: it opens every session. The domain builds that secret, so the
// domain takes crypto/rand and not math/rand, and this is the vet-level check
// CA-013.1 asks for, said as a test so the suite runs it on every go test.
func TestTheDomainPackageImportsNoMathRand(t *testing.T) {
	fset := token.NewFileSet()

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}

		for _, imp := range file.Imports {
			if imported := strings.Trim(imp.Path.Value, `"`); imported == "math/rand" {
				t.Errorf("%s imports %s; the domain builds the signing secret, and a clock-seeded sequence is reproducible by anyone who knows roughly when the server started", path, imported)
			}
		}

		return nil
	})

	if err != nil {
		t.Fatalf("walking the domain package: %v", err)
	}
}
