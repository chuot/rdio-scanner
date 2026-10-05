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

import "sort"

// Downstream is another Rdio Scanner server this one re-posts its calls to, and the
// scope of systems and talkgroups whose calls get re-posted.
type Downstream struct {
	ID       uint64
	Apikey   string
	Disabled bool
	Order    uint
	Scope    AccessScope
	URL      string
}

// ApplyMap reads a downstream out of an administrator configuration payload. As with an
// access and an apikey, omitting the systems key clears the scope — and a downstream
// with no scope forwards nothing, which is how the legacy server behaves.
func (downstream Downstream) ApplyMap(m map[string]any) Downstream {
	if v, ok := m["id"].(float64); ok {
		downstream.ID = uint64(v)
	}

	if v, ok := m["apikey"].(string); ok {
		downstream.Apikey = v
	}

	if v, ok := m["disabled"].(bool); ok {
		downstream.Disabled = v
	}

	if v, ok := m["order"].(float64); ok {
		downstream.Order = uint(v)
	}

	downstream.Scope, _ = ParseAccessScope(m["systems"])

	if v, ok := m["url"].(string); ok {
		downstream.URL = v
	}

	return downstream
}

// Accepts reports whether this downstream should receive a given call. A disabled
// downstream receives nothing, and so does one with no scope.
func (downstream Downstream) Accepts(call Call) bool {
	if downstream.Disabled {
		return false
	}

	return downstream.Scope.Grants(call)
}

// Forwards reports whether there is anything to forward to: a downstream with no URL
// cannot be posted to.
func (downstream Downstream) Forwards() bool {
	return !downstream.Disabled && downstream.URL != ""
}

// Downstreams is the server's list of downstream servers.
type Downstreams []Downstream

// ApplyList replaces the whole list with an administrator payload.
func (downstreams Downstreams) ApplyList(list []any) Downstreams {
	out := Downstreams{}

	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, Downstream{}.ApplyMap(m))
		}
	}

	return out
}

// Ordered returns the downstreams sorted by their display order.
func (downstreams Downstreams) Ordered() Downstreams {
	out := append(Downstreams{}, downstreams...)

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Order < out[j].Order
	})

	return out
}

// Accepting reports the downstreams that should receive a given call.
func (downstreams Downstreams) Accepting(call Call) Downstreams {
	out := Downstreams{}

	for _, downstream := range downstreams {
		if downstream.Forwards() && downstream.Accepts(call) {
			out = append(out, downstream)
		}
	}

	return out
}
