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
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// MaskToken is one token an administrator writes into a watch's mask: the text they type,
// and the piece of filename it stands for.
type MaskToken struct {
	Name    string
	Text    string
	Pattern string
}

// MaskTokens is the table of mask tokens: the text an administrator writes into a mask, and the
// pattern it stands for. The table is applied longest-first, which is the order the init below
// sorts it into, so a token that is the front of a longer token never eats that longer one.
//
// A token stands for the filename piece written at the place the mask puts that token, and
// MaskPattern gives each of those pieces a capture group named for the token. Reading the pieces
// by name is what keeps them where the mask wrote them, whatever order the table applies the
// tokens in.
var MaskTokens = []MaskToken{
	{Name: "date", Text: "#DATE", Pattern: `\d{4}[-_]{0,1}\d{2}[-_]{0,1}\d{2}`},
	{Name: "group", Text: "#GROUP", Pattern: `[a-zA-Z0-9\.\ -]+`},
	{Name: "hz", Text: "#HZ", Pattern: `\d+`},
	{Name: "khz", Text: "#KHZ", Pattern: `[\d\.]+`},
	{Name: "mhz", Text: "#MHZ", Pattern: `[\d\.]+`},
	// The name of the site the recording was heard at, written here.
	{Name: "sitelbl", Text: "#SITELBL", Pattern: `[a-zA-Z0-9,\.\ -]+`},
	// The site the recording was heard at, written here as the reference that site carries
	// under its system.
	{Name: "site", Text: "#SITE", Pattern: `\d+`},
	// The name of the system the call belongs to, written here. A name the configuration has
	// attaches that system; a name it does not claim a system to be created.
	{Name: "syslbl", Text: "#SYSLBL", Pattern: `[a-zA-Z0-9,\.\ -]+`},
	// The system the call belongs to, written here as the reference that system carries.
	{Name: "sys", Text: "#SYS", Pattern: `\d+`},
	{Name: "tag", Text: "#TAG", Pattern: `[a-zA-Z0-9\.\ -]+`},
	{Name: "tgafs", Text: "#TGAFS", Pattern: `\d{2}-\d{3}`},
	// #TGHZ names a whole hertz count, which is what a hertz piece of a filename is: a decimal
	// carrier belongs to the kilohertz and megahertz tokens. A pattern that accepted a decimal
	// point here would read past it and take the digits a later token in the same mask needs.
	{Name: "tghz", Text: "#TGHZ", Pattern: `\d+`},
	{Name: "tgkhz", Text: "#TGKHZ", Pattern: `[\d\.]+`},
	// The name of the talkgroup the call was on, written here. A name is not a talkgroup on its
	// own: the mask has to name the talkgroup too, by number or by one of the carrier tokens.
	{Name: "tglbl", Text: "#TGLBL", Pattern: `[a-zA-Z0-9,\.\ -]+`},
	{Name: "tgmhz", Text: "#TGMHZ", Pattern: `[\d\.]+`},
	// The talkgroup the call was on, written here as its number.
	{Name: "tg", Text: "#TG", Pattern: `\d+`},
	{Name: "time", Text: "#TIME", Pattern: `\d{2}[-:]{0,1}\d{2}[-:]{0,1}\d{2}`},
	{Name: "unit", Text: "#UNIT", Pattern: `\d+`},
	{Name: "unitlbl", Text: "#UNITLBL", Pattern: `[a-zA-Z0-9,\.\ -]+`},
	{Name: "ztime", Text: "#ZTIME", Pattern: `\d{2}[-:]{0,1}\d{2}[-:]{0,1}\d{2}`},
}

// The table is ordered longest-first at init, keeping the relative order
// of same-length tokens as the legacy declared them.
func init() {
	sort.SliceStable(MaskTokens, func(i, j int) bool {
		return len(MaskTokens[i].Text) > len(MaskTokens[j].Text)
	})
}

// MaskPattern builds the regex a watch's mask becomes: every token the mask names is replaced by
// the pattern that token carries, in the table's longest-first order. Each replacement is
// captured as a group named for the token it came from, which is how a filename's pieces are read
// back: by name, each one at the token it sits under. Counting the groups from the start and
// pairing the count with a remembered place in the mask cannot do that, because every replacement
// shifts the rest of the mask along and the remembered place is then a place in a string that no
// longer exists.
func MaskPattern(mask string) (pattern string, tokens int) {
	pattern = mask

	for _, token := range MaskTokens {
		if strings.Index(pattern, token.Text) == -1 {
			continue
		}

		tokens++

		pattern = strings.Replace(pattern, token.Text, "(?P<"+token.Name+">"+token.Pattern+")", 1)
	}

	return pattern, tokens
}

// MaskRegexp compiles the string a watch's mask becomes once every token the mask names has been
// replaced by the pattern the table gives it. That one string is what a mask is checked against when
// the watch is saved and what the watch reads a filename through, so the two ends cannot disagree
// about which masks a watch may carry. A mask whose substituted form does not parse, or which parses
// while capturing fewer pieces than the mask names tokens for, is answered with an error naming the
// mask: the first reads nothing at all, the second reads a filename's pieces onto the wrong values.
// A watch with no mask reads nothing out of a filename and has no pattern to compile.
func (dirwatch Dirwatch) MaskRegexp() (*regexp.Regexp, error) {
	if len(dirwatch.Mask) == 0 {
		return nil, nil
	}

	pattern, tokens := MaskPattern(dirwatch.Mask)

	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("the mask %q does not compile once its tokens are replaced: %v", dirwatch.Mask, err)
	}

	if groups := compiled.NumSubexp(); groups < tokens {
		return nil, fmt.Errorf("the mask %q compiles while capturing %d pieces and naming %d tokens: a token it names "+
			"has no piece of the filename to read", dirwatch.Mask, groups, tokens)
	}

	return compiled, nil
}

// MaskWarning reports the mismatch a watch's mask shows in its own regex: more captured groups than
// the mask names tokens for. The extra groups are pieces of the filename no token claims, which is
// what a mask carrying parentheses of its own writes. They are said, not refused: the mask still
// compiles, and the pieces it names are still read.
func (dirwatch Dirwatch) MaskWarning() string {
	if len(dirwatch.Mask) == 0 {
		return ""
	}

	pattern, tokens := MaskPattern(dirwatch.Mask)

	compiled, err := regexp.Compile(pattern)
	if err != nil {
		// The mask is refused at the read; a mask that cannot compile has
		// nothing left to say about its groups.
		return ""
	}

	if groups := compiled.NumSubexp(); groups > tokens {
		return fmt.Sprintf("the mask %q captures %d groups and names %d tokens: the extra groups are pieces of the "+
			"filename no token names", dirwatch.Mask, groups, tokens)
	}

	return ""
}
