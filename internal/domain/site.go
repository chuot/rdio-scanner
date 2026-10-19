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

import "sort"

// Site is a physical location of a trunked system, identified to clients by SiteRef.
type Site struct {
	ID       uint64
	Label    string
	Order    uint
	SiteRef  uint
	SystemID uint64
}

// ApplyMap reads a site out of an administrator configuration payload, keeping the
// current value for keys that are absent or wrongly typed.
func (site Site) ApplyMap(m map[string]any) Site {
	if v, ok := m["id"].(float64); ok {
		site.ID = uint64(v)
	}

	if v, ok := m["label"].(string); ok {
		site.Label = v
	}

	if v, ok := m["order"].(float64); ok {
		site.Order = uint(v)
	}

	if v, ok := m["siteRef"].(float64); ok {
		site.SiteRef = uint(v)
	}

	if v, ok := m["systemId"].(float64); ok {
		site.SystemID = uint64(v)
	}

	return site
}

// Sites is a collection of sites belonging to one system.
type Sites []Site

// ByID finds a site by its database identifier.
func (sites Sites) ByID(id uint64) (Site, bool) {
	for _, site := range sites {
		if site.ID == id {
			return site, true
		}
	}

	return Site{}, false
}

// ByRef finds a site by the reference clients identify it by.
func (sites Sites) ByRef(ref uint) (Site, bool) {
	for _, site := range sites {
		if site.SiteRef == ref {
			return site, true
		}
	}

	return Site{}, false
}

// ByLabel finds a site by the name the ingest source gave it. The default directory-watch
// mask names a site by label, and only for a call whose system was resolved, because a site
// belongs to a system.
func (sites Sites) ByLabel(label string) (Site, bool) {
	for _, site := range sites {
		if site.Label == label {
			return site, true
		}
	}

	return Site{}, false
}

// Ordered returns the sites sorted by their display order.
func (sites Sites) Ordered() Sites {
	out := append(Sites{}, sites...)

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Order < out[j].Order
	})

	return out
}
