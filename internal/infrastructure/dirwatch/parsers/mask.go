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

// maskTokens is the table of mask tokens, applied longest-first so a
// prefix token never eats a longer one; see the watch-mask maintainer
// note.
var maskTokens = []maskToken{
	{Name: "date", Text: "#DATE", Pattern: `\d{4}[-_]{0,1}\d{2}[-_]{0,1}\d{2}`},
	{Name: "group", Text: "#GROUP", Pattern: `[a-zA-Z0-9\.\ -]+`},
	{Name: "hz", Text: "#HZ", Pattern: `\d+`},
	{Name: "khz", Text: "#KHZ", Pattern: `[\d\.]+`},
	{Name: "mhz", Text: "#MHZ", Pattern: `[\d\.]+`},
	{Name: "sitelbl", Text: "#SITELBL", Pattern: `[a-zA-Z0-9,\.\ -]+`},
	{Name: "site", Text: "#SITE", Pattern: `\d+`},
	{Name: "syslbl", Text: "#SYSLBL", Pattern: `[a-zA-Z0-9,\.\ -]+`},
	{Name: "sys", Text: "#SYS", Pattern: `\d+`},
	{Name: "tag", Text: "#TAG", Pattern: `[a-zA-Z0-9\.\ -]+`},
	{Name: "tgafs", Text: "#TGAFS", Pattern: `\d{2}-\d{3}`},
	{Name: "tghz", Text: "#TGHZ", Pattern: `[\d\.]+`},
	{Name: "tgkhz", Text: "#TGKHZ", Pattern: `[\d\.]+`},
	{Name: "tglbl", Text: "#TGLBL", Pattern: `[a-zA-Z0-9,\.\ -]+`},
	{Name: "tgmhz", Text: "#TGMHZ", Pattern: `[\d\.]+`},
	{Name: "tg", Text: "#TG", Pattern: `\d+`},
	{Name: "time", Text: "#TIME", Pattern: `\d{2}[-:]{0,1}\d{2}[-:]{0,1}\d{2}`},
	{Name: "unit", Text: "#UNIT", Pattern: `\d+`},
	{Name: "unitlbl", Text: "#UNITLBL", Pattern: `[a-zA-Z0-9,\.\ -]+`},
	{Name: "ztime", Text: "#ZTIME", Pattern: `\d{2}[-:]{0,1}\d{2}[-:]{0,1}\d{2}`},
}

// The table is ordered longest-first at init, keeping the relative order
// of same-length tokens as the legacy declared them; see the watch-mask
// maintainer note.
func init() {
	sort.SliceStable(maskTokens, func(i, j int) bool {
		return len(maskTokens[i].Text) > len(maskTokens[j].Text)
	})
}

// maskPosition is where in the mask a token was found, which is what lines the filename's
// captured groups up with the values they name.
type maskPosition struct {
	Name     string
	Position int
}

// MaskWarning reports the mismatch a mask's own regex shows: more
// captured groups than the tokens it names - the call reads the wrong
// pieces; see the watch-mask maintainer note.
func MaskWarning(mask string) string {
	if len(mask) == 0 {
		return ""
	}

	remaining := mask
	tokens := 0

	// The same replacement applyMask makes, applied the same longest-first way: the
	// regex to count groups in is the one the watch will actually compile.
	for _, token := range maskTokens {
		if strings.Index(remaining, token.Text) != -1 {
			tokens++

			remaining = strings.Replace(remaining, token.Text, fmt.Sprintf("(%v)", token.Pattern), 1)
		}
	}

	compiled, err := regexp.Compile(remaining)
	if err != nil {
		// The mask is refused at apply; a mask that cannot compile has
		// nothing left to say about its groups.
		return ""
	}

	if groups := compiled.NumSubexp(); groups > tokens {
		return fmt.Sprintf("the mask %q captures %d groups and names %d tokens: the values it reads will not line up", mask, groups, tokens)
	}

	return ""
}

// applyMask reads the call's metadata out of its filename, at the places
// the watch's mask says each piece sits; a watch with no mask reads
// nothing from the filename; see the watch-mask maintainer note.
func (parsers *Parsers) applyMask(ctx context.Context, watch domain.Dirwatch, call *domain.Call) error {
	if len(watch.Mask) == 0 {
		return nil
	}

	mask := watch.Mask
	positions := []maskPosition{}

	for _, token := range maskTokens {
		if i := strings.Index(mask, token.Text); i != -1 {
			positions = append(positions, maskPosition{Name: token.Name, Position: i})

			mask = strings.Replace(mask, token.Text, fmt.Sprintf("(%v)", token.Pattern), 1)
		}
	}

	sort.Slice(positions, func(i, j int) bool {
		return positions[i].Position < positions[j].Position
	})

	// The mask is matched against the filename without its extension, which
	// already selected the file and the mask has no reason to mention.
	base := strings.TrimSuffix(call.AudioFilename, path.Ext(call.AudioFilename))

	values := map[string]string{}

	for i, value := range regexp.MustCompile(mask).FindStringSubmatch(base) {
		if i > 0 && i-1 < len(positions) {
			values[positions[i-1].Name] = value
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
// and neither time token records a call in 1970; see the watch-mask
// maintainer note.
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
// reference, and a matching name attaches the stored system; see the
// watch-mask maintainer note.
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

// applyMaskFrequencies reads the carrier the call was heard on, in whichever unit the mask
// names.
func applyMaskFrequencies(values map[string]string, call *domain.Call) {
	if value, ok := values["hz"]; ok {
		if hz, err := strconv.ParseFloat(value, 32); err == nil {
			call.Frequencies = append(call.Frequencies, domain.CallFrequency{
				Frequency: uint(hz),
				Offset:    0,
			})
		}

		return
	}

	if value, ok := values["khz"]; ok {
		if khz, err := strconv.ParseFloat(value, 32); err == nil {
			call.Frequencies = append(call.Frequencies, domain.CallFrequency{
				Frequency: uint(khz * 1e3),
				Offset:    0,
			})
		}

		return
	}

	if value, ok := values["mhz"]; ok {
		if mhz, err := strconv.ParseFloat(value, 32); err == nil {
			call.Frequencies = append(call.Frequencies, domain.CallFrequency{
				Frequency: uint(mhz * 1e6),
				Offset:    0,
			})
		}
	}
}

// applyMaskSites reads the site the call was heard at; both tokens only
// do anything when the call already has a system attached, because a
// site has nowhere to be looked up without one; see the watch-mask
// maintainer note.
func applyMaskSites(values map[string]string, call *domain.Call) {
	if value, ok := values["site"]; ok {
		ref, err := strconv.Atoi(value)
		if err != nil {
			return
		}

		if !call.HasSystem() {
			return
		}

		if site, ok := call.System.Sites.ByRef(uint(ref)); ok {
			call.SiteRef = site.SiteRef
		}

		return
	}

	value, ok := values["sitelbl"]
	if !ok {
		return
	}

	if !call.HasSystem() {
		return
	}

	if site, ok := call.System.Sites.ByLabel(value); ok {
		call.SiteRef = site.SiteRef
	}
}

// applyMaskTalkgroup reads the talkgroup the call was on, in whichever
// of the five shapes the mask names, and its name and tag: the three
// frequency-shaped tokens each name a carrier and derive the talkgroup
// reference from it; see the watch-mask maintainer note.
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
		if hz, err := strconv.ParseFloat(value, 32); err == nil {
			call.Frequencies = append(call.Frequencies, domain.CallFrequency{
				Frequency: uint(hz),
				Offset:    0,
			})

			call.Meta.TalkgroupRef = uint(hz / 1e3)
		}
	} else if value, ok := values["tgkhz"]; ok {
		if khz, err := strconv.ParseFloat(value, 32); err == nil {
			call.Frequencies = append(call.Frequencies, domain.CallFrequency{
				Frequency: uint(khz * 1e3),
				Offset:    0,
			})

			call.Meta.TalkgroupRef = uint(khz)
		}
	} else if value, ok := values["tgmhz"]; ok {
		if mhz, err := strconv.ParseFloat(value, 32); err == nil {
			call.Frequencies = append(call.Frequencies, domain.CallFrequency{
				Frequency: uint(mhz * 1e6),
				Offset:    0,
			})

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
// only lands when the mask also named the radio itself; see the
// watch-mask maintainer note.
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
