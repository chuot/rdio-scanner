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
	"fmt"
	"regexp"
)

// The four directory-watch parsers a watched directory can be configured with. The
// value is part of the administrator payload and selects which parser reads the files
// that appear in the directory.
const (
	DirwatchTypeDefault       = "default"
	DirwatchTypeDSDPlus       = "dsdplus"
	DirwatchTypeSdrTrunk      = "sdr-trunk"
	DirwatchTypeTrunkRecorder = "trunk-recorder"
)

// Dirwatch defaults, matching the legacy server's defaults.
const (
	defaultDirwatchDelay       = uint(2000)
	defaultDirwatchDeleteAfter = true
	defaultDirwatchKind        = DirwatchTypeDefault
)

// Dirwatch is a directory the server watches for recorded calls arriving from another
// application, and how to interpret the files that appear in it.
type Dirwatch struct {
	ID          uint64
	Delay       uint
	DeleteAfter bool
	Directory   string
	Disabled    bool
	Extension   string
	Frequency   uint
	Kind        string
	Mask        string
	Order       uint
	SiteID      uint64
	SystemID    uint64
	TalkgroupID uint64
}

// NewDirwatch returns a directory watch with the defaults a freshly created one has in
// the legacy server.
func NewDirwatch() Dirwatch {
	return Dirwatch{
		Delay:       defaultDirwatchDelay,
		DeleteAfter: defaultDirwatchDeleteAfter,
		Kind:        defaultDirwatchKind,
	}
}

// ApplyMap reads a directory watch out of an administrator configuration payload.
//
// The parser type is carried under the key "type" but is named Kind here, because the
// word "type" is already taken by the Go type system in every other part of this
// server. The key is what the wire and the administrator client use, so it stays.
func (dirwatch Dirwatch) ApplyMap(m map[string]any) Dirwatch {
	if v, ok := m["id"].(float64); ok {
		dirwatch.ID = uint64(v)
	}

	if v, ok := m["delay"].(float64); ok {
		dirwatch.Delay = uint(v)

		// A hold over a day is clamped: the legacy minute counter wraps
		// silently at its ceiling, and a watch naming a week is a watch nobody configured.
		if dirwatch.Delay > maxDirwatchDelayMinutes {
			dirwatch.Delay = maxDirwatchDelayMinutes
		}
	}

	if v, ok := m["deleteAfter"].(bool); ok {
		dirwatch.DeleteAfter = v
	}

	if v, ok := m["directory"].(string); ok {
		dirwatch.Directory = v
	}

	if v, ok := m["disabled"].(bool); ok {
		dirwatch.Disabled = v
	}

	if v, ok := m["extension"].(string); ok {
		dirwatch.Extension = v
	}

	if v, ok := m["frequency"].(float64); ok {
		dirwatch.Frequency = uint(v)
	}

	if v, ok := m["type"].(string); ok {
		dirwatch.Kind = v
	}

	if v, ok := m["mask"].(string); ok {
		dirwatch.Mask = v
	}

	if v, ok := m["order"].(float64); ok {
		dirwatch.Order = uint(v)
	}

	if v, ok := m["siteId"].(float64); ok {
		dirwatch.SiteID = uint64(v)
	}

	if v, ok := m["systemId"].(float64); ok {
		dirwatch.SystemID = uint64(v)
	}

	if v, ok := m["talkgroupId"].(float64); ok {
		dirwatch.TalkgroupID = uint64(v)
	}

	return dirwatch
}

// Watches reports whether this directory watch is worth watching at all: a watch with
// no directory watches nothing.
func (dirwatch Dirwatch) Watches() bool {
	return !dirwatch.Disabled && dirwatch.Directory != ""
}

// Dirwatches is the server's set of watched directories.
type Dirwatches []Dirwatch

// maxDirwatchDelayMinutes is the longest hold a watch can name: one day. A delay over it is
// clamped at apply: the legacy server's minute counter wraps silently at the
// ceiling, and a watch that names a week is a watch whose files arrive on a schedule nobody
// configured.
const maxDirwatchDelayMinutes = 24 * 60

// extensionPattern is what a file extension may be: letters and digits, one to ten of them.
// The extension composes filesystem paths, so a name carrying a slash, a
// backslash, a dot, or ".." is not an extension — it is a path walk wearing one.
var extensionPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,10}$`)

// Validate refuses a watch list carrying a mask that does not compile or an
// extension that is not a file extension.
//
// An uncompilable mask is stored today and crashes the server from a timer goroutine on the
// next matching file — a panic with no recovery, on the server's own clock, in a build whose
// web endpoints are answering while the part that watches directories is gone. The refusal
// belongs to the list itself, at apply, where an administrator can be told, not in the dark
// of a timer.
func (dirwatches Dirwatches) Validate() error {
	for _, dirwatch := range dirwatches {
		if err := dirwatch.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// Validate refuses this watch's mask when it does not compile, and its extension when it is
// not a file extension. A watch with no mask parses generically and a watch with no
// extension uses the parser's own default, as they always have; the rule is on the values
// that are named.
func (dirwatch Dirwatch) Validate() error {
	if dirwatch.Mask != "" {
		if _, err := regexp.Compile(dirwatch.Mask); err != nil {
			return fmt.Errorf("the dirwatch on %q has a mask that does not compile: %v", dirwatch.Directory, err)
		}
	}

	if dirwatch.Extension != "" && !extensionPattern.MatchString(dirwatch.Extension) {
		return fmt.Errorf("the dirwatch on %q has an extension that is not a file extension: %q", dirwatch.Directory, dirwatch.Extension)
	}

	return nil
}

// ApplyList replaces the whole set with an administrator payload, which is what the
// legacy server does: a payload that lists fewer watches removes the rest.
func (dirwatches Dirwatches) ApplyList(list []any) Dirwatches {
	out := Dirwatches{}

	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, NewDirwatch().ApplyMap(m))
		}
	}

	return out
}

// Enabled returns the watches that should actually be started.
func (dirwatches Dirwatches) Enabled() Dirwatches {
	out := Dirwatches{}

	for _, dirwatch := range dirwatches {
		if dirwatch.Watches() {
			out = append(out, dirwatch)
		}
	}

	return out
}
