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

package docs

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// these checks keep the project's one licence position the only position a reader, an
// operator, or a client can meet, and refuse stale build-route wordings too.

// licenceNotice is the last line of the header every source file carries. Nothing belongs after it.
const licenceNotice = "// along with this program.  If not, see <http://www.gnu.org/licenses/>"

// retiredWordings are the phrasings the project no longer states, each with what it
// used to claim; they match on unwrapped text, wherever the wrapping falls.
var retiredWordings = []struct {
	phrase string
	claim  string
}{
	{"api_access_policy", "names the licence document the project withdrew"},
	{"websocket api access policy", "states a licence position in a source header"},
	{"access policy", "names the licence position the project withdrew"},
	{"reserved exclusively for saubeo solutions", "reserves the real-time interface to one vendor"},
	{"reserved for saubeo solutions", "reserves an interface of the server to one vendor"},
	{"reserves for saubeo solutions", "reserves an interface of the server to one vendor"},
	{"unauthorized access is strictly prohibited", "states a prohibition the project withdrew"},
	{"licence agreement with saubeo solutions", "asks a self-hosted operator for an agreement"},
	{"license agreement with saubeo solutions", "asks a self-hosted operator for an agreement"},
	{"proprietary real-time", "calls an interface of the server proprietary"},
	{"five of the eight release targets", "splits the release matrix into a compiled group and a group compiled at home"},
	{"the drop route is the three targets", "names a drop route wider than the two macOS targets on a macOS host"},
	{"the five this build compiles", "counts the targets this build compiles an ffmpeg for as five"},
	{"the three whose ffmpeg binaries", "counts three targets whose ffmpeg arrives from somewhere else"},
	{"compiled where they run", "says a pair of ffmpeg binaries is compiled on the machine it runs on"},
	{"compiled where the target runs", "says a pair of ffmpeg binaries is compiled on the machine it runs on"},
	{"target's own platform", "names the host compile by the route name that once covered FreeBSD too"},
}

// guardSelf is the one file allowed to name the wordings it refuses, since the list of
// refusals lives here.
const guardSelf = "tests/docs/licence_test.go"

// unwrapped leaves a file's words without comment markers and line breaks, so a claim
// split over lines still reads as one claim.
func unwrapped(body string) string {
	body = strings.ReplaceAll(body, "//", " ")

	return strings.ToLower(strings.Join(strings.Fields(body), " "))
}

// guardRoots are the source and documentation roots the guard reads, and guardFiles the
// documents at the repository root; any position a reader can reach sits in one.
var (
	guardRoots = []string{"cmd", "internal", "tests", "docs"}

	guardFiles = []string{
		"README.md",
		"CHANGELOG.md",
		"CODE_OF_CONDUCT.md",
		"COMPILING.md",
		"CONTRIBUTING.md",
		"ICAA.md",
		"SECURITY.md",
	}
)

// guardExtensions are the files a person or a client reads; binaries and build outputs
// hold no wording of this project's to hold to one position.
var guardExtensions = map[string]bool{
	".go":   true,
	".md":   true,
	".json": true,
	".html": true,
	".js":   true,
	".mjs":  true,
	".css":  true,
	".txt":  true,
	".ini":  true,
}

// skippedAnywhere names directories holding no project files at all, and a built bundle
// is skipped too because the build rewrites it from the sources and the contract.
var skippedAnywhere = map[string]bool{".git": true, "node_modules": true}

func isBuiltBundle(rel string) bool {
	rel = filepath.ToSlash(rel)

	return strings.HasSuffix(rel, "frontend/bundle") || strings.Contains(rel, "frontend/bundle/")
}

func repoRoot(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("the repository root does not resolve: %v", err)
	}

	return path
}

// guardReadFiles returns every guarded file as a path relative to the repository root,
// in a fixed order, so a failure reads the same on every run.
func guardReadFiles(t *testing.T) []string {
	t.Helper()

	root := repoRoot(t)

	var files []string

	for _, name := range guardFiles {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			files = append(files, name)
		}
	}

	for _, dir := range guardRoots {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if entry.IsDir() {
				if skippedAnywhere[entry.Name()] {
					return filepath.SkipDir
				}

				rel, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}

				if isBuiltBundle(rel) {
					return filepath.SkipDir
				}

				return nil
			}

			if !guardExtensions[filepath.Ext(entry.Name())] {
				return nil
			}

			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}

			files = append(files, rel)

			return nil
		})

		if err != nil {
			t.Fatalf("%s is not walkable: %v", dir, err)
		}
	}

	return files
}

// the wordings the project withdrew appear nowhere a reader, an operator, or a client
// of the published contract meets them.
func TestNoSourceOrDocumentStatesTheWithdrawnPosition(t *testing.T) {
	root := repoRoot(t)

	files := guardReadFiles(t)

	if len(files) == 0 {
		t.Fatalf("the guard reads no file at all, so it proves nothing")
	}

	stillStates := []string{}

	for _, rel := range files {
		if rel == guardSelf {
			continue
		}

		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s is not readable: %v", rel, err)
		}

		text := unwrapped(string(body))

		for _, retired := range retiredWordings {
			if strings.Contains(text, retired.phrase) {
				stillStates = append(stillStates, rel+" — "+retired.claim)

				break
			}
		}
	}

	if len(stillStates) > 0 {
		shown := stillStates
		if len(shown) > 20 {
			shown = shown[:20]
		}

		t.Errorf("%d file(s) still state a position the project withdrew: %s", len(stillStates), strings.Join(shown, "; "))
	}
}

// a source header carrying the licence notice carries nothing after it.
func TestEverySourceFileHeaderEndsAtTheLicenceNotice(t *testing.T) {
	root := repoRoot(t)

	reaches := []string{}

	for _, rel := range guardReadFiles(t) {
		if filepath.Ext(rel) != ".go" {
			continue
		}

		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s is not readable: %v", rel, err)
		}

		at := strings.Index(string(body), licenceNotice)
		if at < 0 {
			continue
		}

		next := string(body[at+len(licenceNotice):])
		next = strings.TrimPrefix(next, "\r\n")
		next = strings.TrimPrefix(next, "\n")

		if strings.HasPrefix(next, "//") {
			reaches = append(reaches, rel)
		}
	}

	if len(reaches) > 0 {
		t.Errorf("%d source file(s) state something after the licence notice, first: %s", len(reaches), reaches[0])
	}
}

// the published contract states the behavior the server has and reserves no interface to
// any vendor.
func TestThePublishedContractReservesNoInterface(t *testing.T) {
	var contract struct {
		Info struct {
			Description string `json:"description"`
		} `json:"info"`

		Components struct {
			WebSockets map[string]struct {
				Path        string `json:"path"`
				Summary     string `json:"summary"`
				Description string `json:"description"`
				Framing     string `json:"framing"`
				Auth        string `json:"auth"`
			} `json:"x-websockets"`
		} `json:"components"`
	}

	body := repoFile(t, "docs", "openapi.json")

	if err := json.Unmarshal([]byte(body), &contract); err != nil {
		t.Fatalf("the published contract does not read: %v", err)
	}

	listener, ok := contract.Components.WebSockets["listener"]
	if !ok {
		t.Fatalf("the contract publishes no listener entry under components.x-websockets")
	}

	if listener.Path != "*" {
		t.Errorf("the listener entry is taken over path %q, want the whole surface", listener.Path)
	}

	// the contract states the real gate: the deployment's own access code.
	if !strings.Contains(listener.Description, "asks for one (PIN)") {
		t.Errorf("the listener entry does not say a server with access codes asks for one: %s", listener.Description)
	}

	for _, place := range []struct {
		what string
		text string
	}{
		{"the contract's own description", contract.Info.Description},
		{"the listener entry's description", listener.Description},
	} {
		lowered := strings.ToLower(place.text)

		for _, retired := range retiredWordings {
			if strings.Contains(lowered, retired.phrase) {
				t.Errorf("%s still states the withdrawn position (%s): %s", place.what, retired.phrase, place.text)
			}
		}
	}
}
