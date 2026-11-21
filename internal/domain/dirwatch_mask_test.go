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

package domain

import (
	"os"
	"strings"
	"testing"
)

// TestDirwatchMaskRegexpCompilesTheStringTheWatchReadsThrough is the door's one string: the watch's
// mask with every token replaced by the pattern the table gives it, compiled, and each piece read
// back at the name of the token that wrote it. The check a mask is given when the watch is saved and
// the read that takes a filename apart must compile this same string, because a door that checks a
// different string agrees with the read only by luck.
func TestDirwatchMaskRegexpCompilesTheStringTheWatchReadsThrough(t *testing.T) {
	dirwatch := Dirwatch{ID: 1, Directory: "/var/spool/rdio", Kind: "wild", Mask: "#SYS-#TG-#DATE-#TIME", Extension: "wav"}

	compiled, err := dirwatch.MaskRegexp()
	if err != nil {
		t.Fatalf("MaskRegexp of the mask %q = %v, want the string its tokens become compiled", dirwatch.Mask, err)
	}

	if compiled == nil {
		t.Fatalf("MaskRegexp of the mask %q = no pattern, want the pattern the watch reads its filenames through", dirwatch.Mask)
	}

	pieces := compiled.FindStringSubmatch("10001-1514000-20261006-120000")
	if pieces == nil {
		t.Fatalf("the compiled mask does not match %q, want the file name that mask was written for", "10001-1514000-20261006-120000")
	}

	for name, want := range map[string]string{
		"sys":  "10001",
		"tg":   "1514000",
		"date": "20261006",
		"time": "120000",
	} {
		group := compiled.SubexpIndex(name)
		if group < 0 || group >= len(pieces) {
			t.Errorf("the compiled mask carries no group named %q, want the piece the token that names it reads", name)

			continue
		}

		if pieces[group] != want {
			t.Errorf("the compiled mask reads %q for the group named %q, want %q", pieces[group], name, want)
		}
	}
}

// TestDirwatchMaskRegexpRefusesEachMaskWhoseTokensLeaveItUnreadable is the whole token table wrapped
// in the brackets an operator types around a token. Typed, those brackets are a character class, so
// the string the tokens become either no longer parses or keeps no capture group for the token it
// names. Both shapes are refused here, with the mask named, because that is the answer the save gives
// the operator and the answer the read gives the watcher.
func TestDirwatchMaskRegexpRefusesEachMaskWhoseTokensLeaveItUnreadable(t *testing.T) {
	for _, mask := range bracketsAroundTokens() {
		dirwatch := Dirwatch{ID: 1, Directory: "/var/spool/rdio", Kind: "wild", Mask: mask, Extension: "wav"}

		compiled, err := dirwatch.MaskRegexp()
		if err == nil {
			t.Errorf("MaskRegexp of the mask %q = %v, want it refused: the string its tokens become cannot read a "+
				"filename's pieces", mask, compiled)

			continue
		}

		if !strings.Contains(err.Error(), mask) {
			t.Errorf("MaskRegexp refused the mask %q with %v, want the reason to name the mask", mask, err)
		}
	}
}

// TestDirwatchMaskRegexpRefusesAMaskWhoseTokensCaptureFewerPiecesThanItNames: these masks parse
// before and after their tokens are replaced — the brackets sit between two tokens, and one of them
// swallows a token's capture group into a character class — while naming two tokens and capturing one
// piece. The watch then lines a filename's pieces up with the wrong values, which is a watch reading
// its recordings wrong rather than one that says it cannot read them.
func TestDirwatchMaskRegexpRefusesAMaskWhoseTokensCaptureFewerPiecesThanItNames(t *testing.T) {
	for _, mask := range []string{"#DATE[#TG]", "[#TG]-#DATE"} {
		dirwatch := Dirwatch{ID: 1, Directory: "/var/spool/rdio", Kind: "wild", Mask: mask, Extension: "wav"}

		compiled, err := dirwatch.MaskRegexp()
		if err == nil {
			t.Errorf("MaskRegexp of the mask %q = %v, want it refused: it captures one piece and names two tokens",
				mask, compiled)

			continue
		}

		if !strings.Contains(err.Error(), mask) {
			t.Errorf("MaskRegexp refused the mask %q with %v, want the reason to name the mask", mask, err)
		}
	}
}

// TestDirwatchMaskRegexpAcceptsAMaskThatMeansTheBracketsItCarries keeps the refusal narrow: a mask
// that means the brackets a recording tool wrote into the file name escapes them, the escaped brackets
// survive the substitution as the literal characters they are, and each token keeps the capture group
// the mask names it for.
func TestDirwatchMaskRegexpAcceptsAMaskThatMeansTheBracketsItCarries(t *testing.T) {
	for _, token := range MaskTokens {
		mask := "\\[" + token.Text + "\\]"

		dirwatch := Dirwatch{ID: 1, Directory: "/var/spool/rdio", Kind: "wild", Mask: mask, Extension: "wav"}

		compiled, err := dirwatch.MaskRegexp()
		if err != nil {
			t.Errorf("MaskRegexp of the mask %q = %v, want the mask accepted: its escaped brackets are the file "+
				"name's own", mask, err)

			continue
		}

		if groups := compiled.NumSubexp(); groups != 1 {
			t.Errorf("the compiled mask for %q captures %d pieces, want the one piece the token %s names",
				mask, groups, token.Text)

			continue
		}

		if group := compiled.SubexpIndex(token.Name); group != 1 {
			t.Errorf("the compiled mask for %q carries group %d for the token %s, want the piece that token names "+
				"kept under its own name", mask, group, token.Text)
		}
	}
}

// TestDirwatchMaskRegexpAcceptsThePunctuationAOperatorWritesAroundAFilenamePiece is the boundary the
// refusal must not cross: parentheses an operator wrote around a piece of the file name, not around a
// token, keep their regular-expression meaning and outnumber the tokens the mask names. That is a
// warning about the pieces no token claims, which the watch's own start says; it is not a mask that
// cannot be read.
func TestDirwatchMaskRegexpAcceptsThePunctuationAOperatorWritesAroundAFilenamePiece(t *testing.T) {
	for _, mask := range []string{"call(1)-#TG", "#(UNITlbl)"} {
		dirwatch := Dirwatch{ID: 1, Directory: "/var/spool/rdio", Kind: "wild", Mask: mask, Extension: "wav"}

		compiled, err := dirwatch.MaskRegexp()
		if err != nil {
			t.Errorf("MaskRegexp of the mask %q = %v, want the mask accepted: its parentheses are the file name's "+
				"own, not a token's", mask, err)

			continue
		}

		if compiled.NumSubexp() == 0 {
			t.Errorf("the compiled mask for %q captures nothing, want the pieces the operator's parentheses wrote", mask)
		}
	}
}

// TestDirwatchMaskRegexpRefusesAMaskThatDoesNotParse keeps the door it already has: a mask that is not
// a pattern at all cannot be read by the watch that carries it, whether or not it names a token.
func TestDirwatchMaskRegexpRefusesAMaskThatDoesNotParse(t *testing.T) {
	for _, mask := range []string{"(", "#TG-#DATE)"} {
		dirwatch := Dirwatch{ID: 1, Directory: "/var/spool/rdio", Kind: "wild", Mask: mask, Extension: "wav"}

		_, err := dirwatch.MaskRegexp()
		if err == nil {
			t.Errorf("MaskRegexp of the mask %q = nil, want it refused: the mask is not a pattern the watch can read", mask)

			continue
		}

		if !strings.Contains(err.Error(), mask) {
			t.Errorf("MaskRegexp refused the mask %q with %v, want the reason to name the mask", mask, err)
		}
	}
}

// TestDirwatchMaskRegexpOnAWatchWithNoMask: a watch with no mask reads nothing out of a file name and
// has no pattern to compile. It is not a mask that cannot be read, and it is refused by no rule.
func TestDirwatchMaskRegexpOnAWatchWithNoMask(t *testing.T) {
	dirwatch := Dirwatch{ID: 1, Directory: "/var/spool/rdio", Kind: "generic", Extension: "m4a"}

	compiled, err := dirwatch.MaskRegexp()
	if err != nil {
		t.Errorf("MaskRegexp of a watch with no mask = %v, want no error: a watch with no mask reads nothing", err)
	}

	if compiled != nil {
		t.Errorf("MaskRegexp of a watch with no mask = %v, want no pattern to read a file name through", compiled)
	}
}

// TestTheWatchMaskTokenTableCommentsSayEachTokenNamesAPieceInTheOrderWritten: the table's
// comments are the account of how a mask is read that an administrator is given, and after the
// change that makes a mask read each piece at the token that named it, each token needs a comment
// of its own saying what it reads. Six tokens carry none today, and several of the comments that
// do exist are fragments cut in the middle of a sentence, which says nothing to anyone.
func TestTheWatchMaskTokenTableCommentsSayEachTokenNamesAPieceInTheOrderWritten(t *testing.T) {
	source, err := os.ReadFile("dirwatch_mask.go")
	if err != nil {
		t.Fatalf("reading the watch mask source: %v", err)
	}

	text := string(source)

	start := strings.Index(text, "var MaskTokens = []MaskToken{")

	if start == -1 {
		t.Fatal("the mask token table is not in the source the comments are read from")
	}

	table := text[start:]

	if end := strings.Index(table, "\nfunc "); end != -1 {
		table = table[:end]
	}

	lines := strings.Split(table, "\n")

	for _, name := range []string{"#SITE", "#SITELBL", "#SYS", "#SYSLBL", "#TG", "#TGLBL"} {
		doc := ""

		for i, line := range lines {
			if !strings.Contains(line, "\""+name+"\"") {
				continue
			}

			for j := i - 1; j >= 0; j-- {
				previous := strings.TrimSpace(lines[j])

				if !strings.HasPrefix(previous, "//") {
					break
				}

				doc = strings.TrimSpace(strings.TrimPrefix(previous, "//")) + " " + doc
			}

			break
		}

		words := strings.Fields(doc)

		if len(words) < 4 {
			t.Errorf("the token %s carries %d words of its own (%q), want a comment of its own saying what the "+
				"token reads and which piece of the filename it is", name, len(words), doc)

			continue
		}

		if !strings.HasSuffix(words[len(words)-1], ".") {
			t.Errorf("the token %s comment %q stops in the middle of a sentence, want it to end one: the comment is "+
				"what an administrator is told the token reads", name, doc)
		}
	}
}
