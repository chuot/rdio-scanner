// Copyright (C) 2019-2024 Chrystian Huot <chrystian@huot.qc.ca>
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

package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Site struct {
	Id       uint64
	Label    string
	Order    uint
	SiteRef  uint
	SystemId uint64
}

func NewSite() *Site {
	return &Site{}
}

func (site *Site) FromMap(m map[string]any) *Site {
	switch v := m["id"].(type) {
	case float64:
		site.Id = uint64(v)
	}

	switch v := m["label"].(type) {
	case string:
		site.Label = v
	}

	switch v := m["order"].(type) {
	case float64:
		site.Order = uint(v)
	}

	switch v := m["siteRef"].(type) {
	case float64:
		site.SiteRef = uint(v)
	}

	switch v := m["systemId"].(type) {
	case float64:
		site.SystemId = uint64(v)
	}

	return site
}

func (site *Site) MarshalJSON() ([]byte, error) {
	m := map[string]any{
		"id":       site.Id,
		"label":    site.Label,
		"siteRef":  site.SiteRef,
		"systemId": site.SystemId,
	}

	if site.Order > 0 {
		m["order"] = site.Order
	}

	return json.Marshal(m)
}

type Sites struct {
	List  []*Site
	mutex sync.Mutex
}

func NewSites() *Sites {
	return &Sites{
		List:  []*Site{},
		mutex: sync.Mutex{},
	}
}

func (sites *Sites) FromMap(f []any) *Sites {
	sites.mutex.Lock()
	defer sites.mutex.Unlock()

	sites.List = []*Site{}

	for _, r := range f {
		switch m := r.(type) {
		case map[string]any:
			site := NewSite().FromMap(m)
			sites.List = append(sites.List, site)
		}
	}

	return sites
}

func (sites *Sites) GetSiteById(id uint64) (site *Site, ok bool) {
	sites.mutex.Lock()
	defer sites.mutex.Unlock()

	for _, site := range sites.List {
		if site.Id == id {
			return site, true
		}
	}

	return nil, false
}

func (sites *Sites) GetSiteByLabel(label string) (site *Site, ok bool) {
	sites.mutex.Lock()
	defer sites.mutex.Unlock()

	for _, site := range sites.List {
		if site.Label == label {
			return site, true
		}
	}

	return nil, false
}

func (sites *Sites) GetSiteByRef(ref uint) (site *Site, ok bool) {
	sites.mutex.Lock()
	defer sites.mutex.Unlock()

	for _, site := range sites.List {
		if site.SiteRef == ref {
			return site, true
		}
	}

	return nil, false
}

func (sites *Sites) ReadTx(tx *sql.Tx, systemId uint64) error {
	var (
		err   error
		query string
		rows  *sql.Rows
	)

	sites.mutex.Lock()
	defer sites.mutex.Unlock()

	sites.List = []*Site{}

	formatError := errorFormatter("sites", "read")

	query = fmt.Sprintf(`SELECT "siteId", "label", "order", "siteRef" FROM "sites" WHERE "systemId" = %d`, systemId)
	if rows, err = tx.Query(query); err != nil {
		return formatError(err, query)
	}

	for rows.Next() {
		site := NewSite()

		if err = rows.Scan(&site.Id, &site.Label, &site.Order, &site.SiteRef); err != nil {
			break
		}

		sites.List = append(sites.List, site)
	}

	rows.Close()

	if err != nil {
		return formatError(err, "")
	}

	sort.Slice(sites.List, func(i int, j int) bool {
		return sites.List[i].Order < sites.List[j].Order
	})

	return nil
}

func (sites *Sites) WriteTx(tx *sql.Tx, systemId uint64) error {
	var (
		err     error
		query   string
		rows    *sql.Rows
		siteIds = []uint64{}
	)

	sites.mutex.Lock()
	defer sites.mutex.Unlock()

	formatError := errorFormatter("sites", "writetx")

	query = fmt.Sprintf(`SELECT "siteId" FROM "sites" WHERE "systemId" = %d`, systemId)
	if rows, err = tx.Query(query); err != nil {
		return formatError(err, query)
	}

	for rows.Next() {
		var siteId uint64
		if err = rows.Scan(&siteId); err != nil {
			break
		}
		remove := true
		for _, site := range sites.List {
			if site.Id == 0 || site.Id == siteId {
				remove = false
				break
			}
		}
		if remove {
			siteIds = append(siteIds, siteId)
		}
	}

	rows.Close()

	if err != nil {
		return formatError(err, "")
	}

	if len(siteIds) > 0 {
		if b, err := json.Marshal(siteIds); err == nil {
			in := strings.ReplaceAll(strings.ReplaceAll(string(b), "[", "("), "]", ")")
			query = fmt.Sprintf(`DELETE FROM "sites" WHERE "siteId" IN %s`, in)
			if _, err = tx.Exec(query); err != nil {
				return formatError(err, query)
			}
		}
	}

	for _, site := range sites.List {
		var count uint

		if site.Id > 0 {
			query = fmt.Sprintf(`SELECT COUNT(*) FROM "sites" WHERE "siteId" = %d`, site.Id)
			if err = tx.QueryRow(query).Scan(&count); err != nil {
				break
			}
		}

		if count == 0 {
			query = fmt.Sprintf(`INSERT INTO "sites" ("label", "order", "siteRef", "systemId") VALUES ('%s', %d, %d, %d)`, escapeQuotes(site.Label), site.Order, site.SiteRef, systemId)
			if _, err = tx.Exec(query); err != nil {
				break
			}

		} else {
			query = fmt.Sprintf(`UPDATE "sites" SET "label" = '%s', "order" = %d, "siteRef" = %d where "siteId" = %d`, escapeQuotes(site.Label), site.Order, site.SiteRef, site.Id)
			if _, err = tx.Exec(query); err != nil {
				break
			}
		}
	}

	if err != nil {
		return formatError(err, query)
	}

	return nil
}
