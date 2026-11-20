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

package parsers

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"rdio-scanner/internal/domain"
)

// maskToken is one token an administrator writes into a watch's mask: the text they type,
// and the piece of filename it stands for.
type maskToken struct {
	Name    string
	Text    string
	Pattern string
}

// maskTokens is the table of mask tokens: the text an administrator writes into a mask, and the
// pattern it stands for. The table is applied longest-first, which is the order the init below
// sorts it into, so a token that is the front of a longer token never eats that longer one.
//
// A token stands for the filename piece written at the place the mask puts that token, and
// maskPattern gives each of those pieces a capture group named for the token. Reading the pieces
// by name is what keeps them where the mask wrote them, whatever order the table applies the
// tokens in.
var maskTokens = []maskToken{
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
	sort.SliceStable(maskTokens, func(i, j int) bool {
		return len(maskTokens[i].Text) > len(maskTokens[j].Text)
	})
}

// maskPattern builds the regex a watch's mask becomes: every token the mask names is replaced by
// the pattern that token carries, in the table's longest-first order. Each replacement is
// captured as a group named for the token it came from, which is how a filename's pieces are read
// back: by name, each one at the token it sits under. Counting the groups from the start and
// pairing the count with a remembered place in the mask cannot do that, because every replacement
// shifts the rest of the mask along and the remembered place is then a place in a string that no
// longer exists.
func maskPattern(mask string) (pattern string, tokens int) {
	pattern = mask

	for _, token := range maskTokens {
		if strings.Index(pattern, token.Text) == -1 {
			continue
		}

		tokens++

		pattern = strings.Replace(pattern, token.Text, "(?P<"+token.Name+">"+token.Pattern+")", 1)
	}

	return pattern, tokens
}

// MaskWarning reports the mismatch a mask's own regex shows: more captured groups than the mask
// names tokens for. The extra groups are pieces of the filename no token claims, which is what a
// mask carrying parentheses of its own writes.
func MaskWarning(mask string) string {
	if len(mask) == 0 {
		return ""
	}

	pattern, tokens := maskPattern(mask)

	compiled, err := regexp.Compile(pattern)
	if err != nil {
		// The mask is refused at apply; a mask that cannot compile has
		// nothing left to say about its groups.
		return ""
	}

	if groups := compiled.NumSubexp(); groups > tokens {
		return fmt.Sprintf("the mask %q captures %d groups and names %d tokens: the extra groups are pieces of the "+
			"filename no token names", mask, groups, tokens)
	}

	return ""
}

// applyMask reads the call's metadata out of its filename, at the places
// the watch's mask says each piece sits; a watch with no mask reads
// nothing from the filename.
func (parsers *Parsers) applyMask(ctx context.Context, watch domain.Dirwatch, call *domain.Call) error {
	if len(watch.Mask) == 0 {
		return nil
	}

	pattern, _ := maskPattern(watch.Mask)

	// The mask is matched against the filename without its extension, which
	// already selected the file and the mask has no reason to mention.
	base := strings.TrimSuffix(call.AudioFilename, path.Ext(call.AudioFilename))

	compiled := regexp.MustCompile(pattern)
	pieces := compiled.FindStringSubmatch(base)

	values := map[string]string{}

	for _, token := range maskTokens {
		group := compiled.SubexpIndex(token.Name)

		if group > 0 && group < len(pieces) {
			values[token.Name] = pieces[group]
		}
	}

	// The values apply in the order the legacy applies them: two of them
	// read what an earlier one wrote.
	if err := parsers.applyMaskTimestamps(values, call); err != nil {
		return err
	}

	applyMaskGroups(values, call)
	applyMaskFrequencies(values, call)

	if err := parsers.applyMaskSystem(ctx, values, call); err != nil {
		return err
	}

	applyMaskSites(values, call)
	applyMaskTalkgroup(values, call)
	applyMaskUnits(values, call)

	return nil
}

// applyMaskTimestamps reads the instant the call happened, from
// whichever of the three shapes the mask offers; a mask naming #DATE
// and neither time token records a call in 1970.
func (parsers *Parsers) applyMaskTimestamps(values map[string]string, call *domain.Call) error {
	value, ok := values["date"]
	if !ok {
		return nil
	}

	value = regexp.MustCompile(`(\d{4})(\d{2})(\d{2})`).ReplaceAllString(value, "$1-$2-$3")

	if local, ok := values["time"]; ok {
		local = regexp.MustCompile(`(\d{2})[^\d]*(\d{2})[^\d]*(\d{2})`).ReplaceAllString(local, "$1:$2:$3")

		if timestamp, err := time.ParseInLocation("2006-01-02T15:04:05", fmt.Sprintf("%vT%v", value, local), parsers.clock.Now().Location()); err == nil {
			call.Timestamp = timestamp.UTC()
		}

		return nil
	}

	if utc, ok := values["ztime"]; ok {
		utc = regexp.MustCompile(`(\d{2})[^\d]*(\d{2})[^\d]*(\d{2})`).ReplaceAllString(utc, "$1:$2:$3")

		if timestamp, err := time.Parse("2006-01-02T15:04:05", fmt.Sprintf("%vT%v", value, utc)); err == nil {
			call.Timestamp = timestamp.UTC()
		}

		return nil
	}

	digits := regexp.MustCompile(`[^\d]`).ReplaceAllString(value, "")

	if seconds, err := strconv.Atoi(digits); err == nil {
		call.Timestamp = time.Unix(int64(seconds), 0).UTC()
	}

	return nil
}

// applyMaskSystem reads the system the call belongs to, by reference or
// by name: a name matching no configured system claims the next free
// reference, and a matching name attaches the stored system.
func (parsers *Parsers) applyMaskSystem(ctx context.Context, values map[string]string, call *domain.Call) error {
	if value, ok := values["sys"]; ok {
		if ref, err := strconv.Atoi(value); err == nil {
			call.Meta.SystemRef = uint(ref)
		}

		return nil
	}

	value, ok := values["syslbl"]
	if !ok {
		return nil
	}

	// A system named by label is looked up in the running configuration, as
	// the legacy looks it up; a read that fails is reported.
	systems, err := parsers.systems.AllSystems(ctx)
	if err != nil {
		return err
	}

	if system, ok := systems.ByLabel(value); ok {
		call.System = system

		return nil
	}

	// The next free reference is claimed here, as the legacy claims it, not
	// taken from a sequence.
	call.Meta.SystemRef = systems.NextSystemRef()
	call.Meta.SystemLabel = value

	return nil
}

// applyMaskGroups reads the one group the filename names. A dash is what the recording tools
// write for "no group", and it is not a group name.
func applyMaskGroups(values map[string]string, call *domain.Call) {
	if value, ok := values["group"]; ok {
		if len(value) > 0 && value != "-" {
			call.Meta.TalkgroupGroup = []string{value}
		}
	}
}

// fileCarrier files the carrier the filename names as the call's only carrier. A watch's own
// carrier says where in the directory its recordings came from; a name that says which carrier
// one recording was heard on replaces it, the way the legacy assigned a call's single frequency,
// rather than filing both on the same call.
func fileCarrier(call *domain.Call, hertz uint) {
	call.Frequencies = []domain.CallFrequency{{
		Frequency: hertz,
		Offset:    0,
	}}
}

// applyMaskFrequencies reads the carrier the call was heard on, in whichever unit the mask
// names. The whole value is read at full width: a carrier in hertz is well past what a
// 32-bit read reaches, and a decimal in kilohertz or megahertz loses its last digits there.
func applyMaskFrequencies(values map[string]string, call *domain.Call) {
	if value, ok := values["hz"]; ok {
		if hz, err := strconv.ParseFloat(value, 64); err == nil {
			fileCarrier(call, uint(hz))
		}

		return
	}

	if value, ok := values["khz"]; ok {
		if khz, err := strconv.ParseFloat(value, 64); err == nil {
			fileCarrier(call, uint(khz*1e3))
		}

		return
	}

	if value, ok := values["mhz"]; ok {
		if mhz, err := strconv.ParseFloat(value, 64); err == nil {
			fileCarrier(call, uint(mhz*1e6))
		}
	}
}

// applyMaskSites claims the site the recording was heard at. A site is a row that belongs to a
// system, and the system a call lands on is not settled until after the parser has run, so the
// token claims what its own name says and the ingest path looks it up under that system — the way
// a system named by reference and a talkgroup named by number are settled.
func applyMaskSites(values map[string]string, call *domain.Call) {
	if value, ok := values["site"]; ok {
		if ref, err := strconv.Atoi(value); err == nil {
			call.Meta.SiteRef = uint(ref)
		}

		return
	}

	if value, ok := values["sitelbl"]; ok {
		call.Meta.SiteLabel = value
	}
}

// applyMaskTalkgroup reads the talkgroup the call was on, in whichever
// of the five shapes the mask names, and its name and tag: the three
// frequency-shaped tokens each name a carrier and derive the talkgroup
// reference from it.
func applyMaskTalkgroup(values map[string]string, call *domain.Call) {
	if value, ok := values["tag"]; ok {
		if len(value) > 0 && value != "-" {
			call.Meta.TalkgroupTag = value
		}
	}

	if value, ok := values["tg"]; ok {
		if ref, err := strconv.Atoi(value); err == nil {
			call.Meta.TalkgroupRef = uint(ref)
		}
	} else if value, ok := values["tgafs"]; ok {
		// An Alpha-Net talkgroup is written as three octal-ish fields, and the reference is
		// those fields packed into one number.
		if len(value) == 6 && value[2] == '-' {
			a, errA := strconv.Atoi(value[:2])
			b, errB := strconv.Atoi(value[3:5])
			c, errC := strconv.Atoi(value[5:])

			if errA == nil && errB == nil && errC == nil {
				call.Meta.TalkgroupRef = uint(a<<7 | b<<3 | c)
			}
		}
	} else if value, ok := values["tghz"]; ok {
		if hz, err := strconv.ParseFloat(value, 64); err == nil {
			fileCarrier(call, uint(hz))

			call.Meta.TalkgroupRef = uint(hz / 1e3)
		}
	} else if value, ok := values["tgkhz"]; ok {
		if khz, err := strconv.ParseFloat(value, 64); err == nil {
			fileCarrier(call, uint(khz*1e3))

			call.Meta.TalkgroupRef = uint(khz)
		}
	} else if value, ok := values["tgmhz"]; ok {
		if mhz, err := strconv.ParseFloat(value, 64); err == nil {
			fileCarrier(call, uint(mhz*1e6))

			call.Meta.TalkgroupRef = uint(mhz * 1e3)
		}
	}

	if value, ok := values["tglbl"]; ok {
		if len(value) > 0 {
			call.Meta.TalkgroupLabel = value
		}
	}
}

// applyMaskUnits reads the radio that spoke, and its name: the name
// only lands when the mask also named the radio itself.
func applyMaskUnits(values map[string]string, call *domain.Call) {
	if value, ok := values["unit"]; ok {
		if ref, err := strconv.Atoi(value); err == nil {
			call.Units = append(call.Units, domain.CallUnit{
				UnitRef: uint(ref),
				Offset:  0,
			})
		}
	}

	value, ok := values["unitlbl"]
	if !ok {
		return
	}

	if len(value) > 0 && len(call.Units) > 0 {
		call.Meta.UnitLabels = []string{value}
		call.Meta.UnitRefs = []uint{call.Units[0].UnitRef}
	}
}
