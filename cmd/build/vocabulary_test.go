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

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The programs this tool compiles, adopts and ships are named **the ffmpeg binaries**. The word *pair*
// is not used for them: it is the name the audit used for the two programs it audited, and the audit is
// gone. The build's own messages, its source, and the two documents an operator follows all say the same
// thing, so this test reads them as the tree they are in.
//
// The web applications are a pair of their own — the two applications a run builds — and that use says
// nothing about these programs, so the file that owns it is named here and left out of the reading.

// theWordPair is the word, whole, in any case: "repaired" is not a use of it.
var theWordPair = regexp.MustCompile(`(?i)\bpair\b`)

// theVocabularyRuleFile is this file: the file that states the rule is the one place allowed to name the
// word it forbids.
const theVocabularyRuleFile = "vocabulary_test.go"

// theVocabularyFiles are the sources and documents that name these programs: this tool's own sources,
// the bundle package it compiles into, and the two documents an operator follows.
func theVocabularyFiles(t *testing.T) []string {
	t.Helper()

	files := []string{}

	for _, dir := range []string{
		".",
		filepath.Join("..", "..", "internal", "infrastructure", "ffmpeg"),
	} {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.IsDir() {
				if info.Name() == ".build" {
					return filepath.SkipDir
				}

				return nil
			}

			if !strings.HasSuffix(path, ".go") {
				return nil
			}

			// The two applications a run builds are a pair, and that is not these programs.
			if filepath.Base(path) == "webapps.go" {
				return nil
			}

			// This file is the rule, and a rule names the word it forbids.
			if filepath.Base(path) == theVocabularyRuleFile {
				return nil
			}

			files = append(files, path)

			return nil
		})

		if err != nil {
			t.Fatalf("%s: the sources cannot be walked: %v", dir, err)
		}
	}

	for _, doc := range theOperatorDocuments(t) {
		files = append(files, doc)
	}

	return files
}

// theOperatorDocuments are the two documents an operator follows to build a release.
func theOperatorDocuments(t *testing.T) []string {
	t.Helper()

	return []string{
		filepath.Join("..", "..", "docs", "admin-guide.html"),
		filepath.Join("..", "..", "COMPILING.md"),
	}
}

func TestTheBuildToolNeverCallsTheseProgramsAPair(t *testing.T) {
	files := theVocabularyFiles(t)

	if len(files) < 20 {
		t.Fatalf("the reading covers %d files, which is not the tool's sources and its documents", len(files))
	}

	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: the file is not readable: %v", path, err)
		}

		for at, line := range strings.Split(string(body), "\n") {
			if !theWordPair.MatchString(line) {
				continue
			}

			t.Errorf("%s:%d names these programs a pair: %s", path, at+1, strings.TrimSpace(line))
		}
	}
}

// The name that replaces it is the one an operator reads: the phrase is in the tool's own source, in the
// guide, and in the compiling notes. A sweep that only deleted the old word would leave the tree naming
// these programs by no name at all.
func TestTheBuildToolSaysTheFfmpegBinaries(t *testing.T) {
	theWanted := "the ffmpeg binaries"

	for _, path := range []string{
		filepath.Join("supply.go"),
		filepath.Join("ffmpeg.go"),
		filepath.Join("..", "..", "internal", "infrastructure", "ffmpeg", "bundle", "bundle.go"),
	} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: the source is not readable: %v", path, err)
		}

		if !strings.Contains(strings.ToLower(string(body)), theWanted) {
			t.Errorf("%s never says %q", path, theWanted)
		}
	}

	for _, path := range theOperatorDocuments(t) {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: the document is not readable: %v", path, err)
		}

		if !strings.Contains(strings.ToLower(string(body)), theWanted) {
			t.Errorf("%s never says %q", path, theWanted)
		}
	}
}
