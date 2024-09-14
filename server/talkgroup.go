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
	"strconv"
	"strings"
	"sync"
)

type Talkgroup struct {
	Id           uint64
	Alert        string
	Delay        uint
	Frequency    uint
	GroupIds     []uint64
	Kind         string
	Label        string
	Led          string
	Name         string
	Order        uint
	TagId        uint64
	TalkgroupRef uint
}

func NewTalkgroup() *Talkgroup {
	return &Talkgroup{
		GroupIds: []uint64{},
	}
}

func (talkgroup *Talkgroup) FromMap(m map[string]any) *Talkgroup {
	switch v := m["id"].(type) {
	case float64:
		talkgroup.Id = uint64(v)
	}

	switch v := m["alert"].(type) {
	case string:
		talkgroup.Alert = v
	}

	switch v := m["delay"].(type) {
	case float64:
		talkgroup.Delay = uint(v)
	}

	switch v := m["frequency"].(type) {
	case float64:
		talkgroup.Frequency = uint(v)
	}

	switch v := m["groupIds"].(type) {
	case []any:
		talkgroup.GroupIds = []uint64{}
		for _, v := range v {
			switch i := v.(type) {
			case float64:
				talkgroup.GroupIds = append(talkgroup.GroupIds, uint64(i))
			}
		}
	}

	switch v := m["type"].(type) {
	case string:
		talkgroup.Kind = v
	}

	switch v := m["label"].(type) {
	case string:
		talkgroup.Label = v
	}

	switch v := m["led"].(type) {
	case string:
		talkgroup.Led = v
	}

	switch v := m["name"].(type) {
	case string:
		talkgroup.Name = v
	}

	switch v := m["order"].(type) {
	case float64:
		talkgroup.Order = uint(v)
	}

	switch v := m["tagId"].(type) {
	case float64:
		talkgroup.TagId = uint64(v)
	}

	switch v := m["talkgroupRef"].(type) {
	case float64:
		talkgroup.TalkgroupRef = uint(v)
	}

	return talkgroup
}

func (talkgroup *Talkgroup) MarshalJSON() ([]byte, error) {
	m := map[string]any{
		"id":           talkgroup.Id,
		"groupIds":     talkgroup.GroupIds,
		"label":        talkgroup.Label,
		"name":         talkgroup.Name,
		"talkgroupRef": talkgroup.TalkgroupRef,
	}

	if len(talkgroup.Alert) > 0 {
		m["alert"] = talkgroup.Alert
	}

	if talkgroup.Delay > 0 {
		m["delay"] = talkgroup.Delay
	}

	if talkgroup.Frequency > 0 {
		m["frequency"] = talkgroup.Frequency
	}

	if len(talkgroup.Kind) > 0 {
		m["type"] = talkgroup.Kind
	}

	if len(talkgroup.Led) > 0 {
		m["led"] = talkgroup.Led
	}

	if talkgroup.Order > 0 {
		m["talkgroup"] = talkgroup.Order
	}

	if talkgroup.TagId > 0 {
		m["tagId"] = talkgroup.TagId
	}

	return json.Marshal(m)
}

type TalkgroupMap map[string]any

type Talkgroups struct {
	List  []*Talkgroup
	mutex sync.Mutex
}

func NewTalkgroups() *Talkgroups {
	return &Talkgroups{
		List:  []*Talkgroup{},
		mutex: sync.Mutex{},
	}
}

func (talkgroups *Talkgroups) FromMap(f []any) *Talkgroups {
	talkgroups.mutex.Lock()
	defer talkgroups.mutex.Unlock()

	talkgroups.List = []*Talkgroup{}

	for _, r := range f {
		switch m := r.(type) {
		case map[string]any:
			talkgroup := NewTalkgroup().FromMap(m)
			talkgroups.List = append(talkgroups.List, talkgroup)
		}
	}

	return talkgroups
}

func (talkgroups *Talkgroups) GetTalkgroupById(id uint64) (system *Talkgroup, ok bool) {
	talkgroups.mutex.Lock()
	defer talkgroups.mutex.Unlock()

	for _, talkgroup := range talkgroups.List {
		if talkgroup.Id == id {
			return talkgroup, true
		}
	}

	return nil, false
}

func (talkgroups *Talkgroups) GetTalkgroupByLabel(label string) (talkgroup *Talkgroup, ok bool) {
	talkgroups.mutex.Lock()
	defer talkgroups.mutex.Unlock()

	for _, talkgroup := range talkgroups.List {
		if talkgroup.Label == label {
			return talkgroup, true
		}
	}

	return nil, false
}

func (talkgroups *Talkgroups) GetTalkgroupByRef(ref uint) (talkgroup *Talkgroup, ok bool) {
	talkgroups.mutex.Lock()
	defer talkgroups.mutex.Unlock()

	for _, talkgroup := range talkgroups.List {
		if talkgroup.TalkgroupRef == ref {
			return talkgroup, true
		}
	}

	return nil, false
}

func (talkgroups *Talkgroups) ReadTx(tx *sql.Tx, systemId uint64, dbType string) error {
	var (
		err   error
		query string
		rows  *sql.Rows

		groupIds string
	)

	talkgroups.mutex.Lock()
	defer talkgroups.mutex.Unlock()

	talkgroups.List = []*Talkgroup{}

	formatError := errorFormatter("talkgroups", "read")

	if dbType == DbTypePostgresql {
		query = fmt.Sprintf(`SELECT t."talkgroupId", t."alert", t."delay", t."frequency", t."label", t."led", t."name", t."order", t."tagId", t."talkgroupRef", t."type", STRING_AGG(CAST(COALESCE(tg."groupId", 0) AS text), ',') FROM "talkgroups" AS t LEFT JOIN "talkgroupGroups" AS tg ON tg."talkgroupId" = t."talkgroupId" WHERE t."systemId" = %d GROUP BY t."talkgroupId"`, systemId)

	} else {
		query = fmt.Sprintf(`SELECT t."talkgroupId", t."alert", t."delay", t."frequency", t."label", t."led", t."name", t."order", t."tagId", t."talkgroupRef", t."type", GROUP_CONCAT(COALESCE(tg."groupId", 0)) FROM "talkgroups" AS t LEFT JOIN "talkgroupGroups" AS tg ON tg."talkgroupId" = t."talkgroupId" WHERE t."systemId" = %d GROUP BY t."talkgroupId"`, systemId)
	}

	if rows, err = tx.Query(query); err != nil {
		return formatError(err, query)
	}

	for rows.Next() {
		talkgroup := NewTalkgroup()

		if err = rows.Scan(&talkgroup.Id, &talkgroup.Alert, &talkgroup.Delay, &talkgroup.Frequency, &talkgroup.Label, &talkgroup.Led, &talkgroup.Name, &talkgroup.Order, &talkgroup.TagId, &talkgroup.TalkgroupRef, &talkgroup.Kind, &groupIds); err != nil {
			break
		}

		for _, s := range strings.Split(groupIds, ",") {
			if i, err := strconv.Atoi(s); err == nil && i > 0 {
				talkgroup.GroupIds = append(talkgroup.GroupIds, uint64(i))
			}
		}

		talkgroups.List = append(talkgroups.List, talkgroup)
	}

	rows.Close()

	if err != nil {
		return formatError(err, "")
	}

	sort.Slice(talkgroups.List, func(i int, j int) bool {
		return talkgroups.List[i].Order < talkgroups.List[j].Order
	})

	return nil
}

func (talkgroups *Talkgroups) WriteTx(tx *sql.Tx, systemId uint64, dbType string) error {
	var (
		err   error
		query string
		res   sql.Result
		rows  *sql.Rows

		talkgroupGroupIds = []uint64{}
		talkgroupIds      = []uint64{}
	)

	talkgroups.mutex.Lock()
	defer talkgroups.mutex.Unlock()

	formatError := errorFormatter("talkgroups", "writetx")

	query = fmt.Sprintf(`SELECT "talkgroupId" FROM "talkgroups" WHERE "systemId" = %d`, systemId)
	if rows, err = tx.Query(query); err != nil {
		return formatError(err, query)
	}

	for rows.Next() {
		var talkgroupId uint64
		if err = rows.Scan(&talkgroupId); err != nil {
			break
		}
		remove := true
		for _, talkgroup := range talkgroups.List {
			if talkgroupId == 0 || talkgroup.Id == talkgroupId {
				remove = false
				break
			}
		}
		if remove {
			talkgroupIds = append(talkgroupIds, talkgroupId)
		}
	}

	rows.Close()

	if err != nil {
		return formatError(err, "")
	}

	if len(talkgroupIds) > 0 {
		if b, err := json.Marshal(talkgroupIds); err == nil {
			in := strings.ReplaceAll(strings.ReplaceAll(string(b), "[", "("), "]", ")")

			query = fmt.Sprintf(`DELETE FROM "talkgroups" WHERE "talkgroupId" IN %s`, in)
			if _, err = tx.Exec(query); err != nil {
				return formatError(err, query)
			}

			query = fmt.Sprintf(`DELETE FROM "talkgroupGroups" WHERE "talkgroupId" IN %s`, in)
			if _, err = tx.Exec(query); err != nil {
				return formatError(err, query)
			}
		}
	}

	for _, talkgroup := range talkgroups.List {
		var count uint

		if talkgroup.Id > 0 {
			query = fmt.Sprintf(`SELECT COUNT(*) FROM "talkgroups" WHERE "talkgroupId" = %d`, talkgroup.Id)
			if err = tx.QueryRow(query).Scan(&count); err != nil {
				break
			}
		}

		if count == 0 {
			query = fmt.Sprintf(`INSERT INTO "talkgroups" ("alert", "delay", "frequency", "label", "led", "name", "order", "systemId", "tagId", "talkgroupRef", "type") VALUES ('%s', %d, %d, '%s', '%s', '%s', %d, %d, %d, %d, '%s')`, talkgroup.Alert, talkgroup.Delay, talkgroup.Frequency, escapeQuotes(talkgroup.Label), talkgroup.Led, escapeQuotes(talkgroup.Name), talkgroup.Order, systemId, talkgroup.TagId, talkgroup.TalkgroupRef, talkgroup.Kind)

			if dbType == DbTypePostgresql {
				query = query + ` RETURNING "talkgroupId"`

				if err = tx.QueryRow(query).Scan(&talkgroup.Id); err != nil {
					break
				}

			} else {
				if res, err = tx.Exec(query); err == nil {
					if id, err := res.LastInsertId(); err == nil {
						talkgroup.Id = uint64(id)
					}
				} else {
					break
				}
			}

		} else {
			query = fmt.Sprintf(`UPDATE "talkgroups" SET "alert" = '%s', "delay" = %d, "frequency" = %d, "label" = '%s', "led" = '%s', "name" = '%s', "order" = %d, "tagId" = %d, "talkgroupRef" = %d, "type" = '%s' WHERE "talkgroupId" = %d`, talkgroup.Alert, talkgroup.Delay, talkgroup.Frequency, escapeQuotes(talkgroup.Label), talkgroup.Led, escapeQuotes(talkgroup.Name), talkgroup.Order, talkgroup.TagId, talkgroup.TalkgroupRef, talkgroup.Kind, talkgroup.Id)
			if _, err = tx.Exec(query); err != nil {
				break
			}
		}

		query = fmt.Sprintf(`SELECT "groupId", "talkgroupGroupId" FROM "talkgroupGroups" WHERE "talkgroupId" = %d`, talkgroup.Id)
		if rows, err = tx.Query(query); err != nil {
			break
		}

		for rows.Next() {
			var (
				groupId          uint64
				talkgroupGroupId uint64
			)
			if err = rows.Scan(&groupId, &talkgroupGroupId); err != nil {
				break
			}
			remove := true
			for _, id := range talkgroup.GroupIds {
				if id == 0 || id == talkgroupGroupId {
					remove = false
					break
				}
			}
			if remove {
				talkgroupGroupIds = append(talkgroupGroupIds, talkgroupGroupId)
			}
		}

		rows.Close()

		if err != nil {
			return formatError(err, "")
		}

		if len(talkgroupGroupIds) > 0 {
			if b, err := json.Marshal(talkgroupGroupIds); err == nil {
				in := strings.ReplaceAll(strings.ReplaceAll(string(b), "[", "("), "]", ")")
				query = fmt.Sprintf(`DELETE FROM "talkgroupGroups" WHERE "talkgroupGroupId" IN %s`, in)
				if _, err = tx.Exec(query); err != nil {
					return formatError(err, query)
				}
			}
		}

		for _, groupId := range talkgroup.GroupIds {
			query = fmt.Sprintf(`SELECT COUNT(*) FROM "talkgroupGroups" WHERE "talkgroupId" = %d AND "groupId" = %d`, talkgroup.Id, groupId)
			if err = tx.QueryRow(query).Scan(&count); err != nil {
				break
			}

			if count == 0 {
				query = fmt.Sprintf(`INSERT INTO "talkgroupGroups" ("groupId", "talkgroupId") VALUES (%d, %d)`, groupId, talkgroup.Id)
				if _, err = tx.Exec(query); err != nil {
					break
				}
			}
		}
	}

	if err != nil {
		return formatError(err, query)
	}

	return nil
}

type TalkgroupsMap []TalkgroupMap
