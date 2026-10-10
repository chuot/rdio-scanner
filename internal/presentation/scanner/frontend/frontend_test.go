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

package frontend

import (
	"os"
	"testing"
	"testing/fstest"
)

// TestThePackageCompilesWithAnUnbuiltClient is the compile-time case. The bundle directory is
// committed with nothing in it but `.gitkeep`, and `//go:embed all:bundle` is the pattern that
// matches a directory whose only entry is a hidden file. A package that compiles on a fresh
// checkout is what makes `go build ./...` and `go test ./...` green before any client build has
// run; the embed failing would turn an unbuilt client into a compile error.
func TestThePackageCompilesWithAnUnbuiltClient(t *testing.T) {
	if _, err := bundle.ReadFile("bundle/.gitkeep"); err != nil {
		t.Fatalf("bundle/.gitkeep is not embedded, so this package would not compile against a placeholder-only bundle: %v", err)
	}
}

// The bundle question is asked of a file set, so the placeholder-only case is testable: a file
// set carrying the application shell is built, and a file set carrying only the placeholder is
// not. The server warns about an unbuilt bundle, and the warning is judged by this question.
func TestABundleIsBuiltWhenItCarriesTheApplicationShell(t *testing.T) {
	built := fstest.MapFS{
		"bundle/.gitkeep":   &fstest.MapFile{Data: []byte("")},
		"bundle/index.html": &fstest.MapFile{Data: []byte("<html>")},
	}

	if !bundleIsBuilt(built) {
		t.Error("a file set carrying bundle/index.html is not seen as built")
	}

	unbuilt := fstest.MapFS{
		"bundle/.gitkeep": &fstest.MapFile{Data: []byte("")},
	}

	if bundleIsBuilt(unbuilt) {
		t.Error("a file set carrying only the placeholder is seen as built")
	}
}

// The same question asked of the package's own embedded bundle answers the way the tree says:
// the embed is the bundle, and the check is the same check the build tool judges a bundle by.
func TestTheEmbeddedBundleIsBuiltWhenTheTreeHasTheApplicationShell(t *testing.T) {
	_, treeErr := os.Stat("bundle/index.html")

	if BundleIsBuilt() != (treeErr == nil) {
		t.Errorf("the embedded bundle is seen as built %v, want what the tree says about bundle/index.html (tree stat error %v)", BundleIsBuilt(), treeErr)
	}
}
