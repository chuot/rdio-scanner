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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Group struct {
	Id    uint64
	Alert string
	Label string
	Led   string
	Order uint
}

func NewGroup() *Group {
	return &Group{}
}

func (group *Group) FromMap(m map[string]any) *Group {
	switch v := m["id"].(type) {
	case float64:
		group.Id = uint64(v)
	}

	switch v := m["alert"].(type) {
	case string:
		group.Alert = v
	}

	switch v := m["label"].(type) {
	case string:
		group.Label = v
	}

	switch v := m["led"].(type) {
	case string:
		group.Led = v
	}

	switch v := m["order"].(type) {
	case float64:
		group.Order = uint(v)
	}

	return group
}

func (group *Group) MarshalJSON() ([]byte, error) {
	m := map[string]any{
		"id":    group.Id,
		"label": group.Label,
	}

	if len(group.Alert) > 0 {
		m["alert"] = group.Alert
	}

	if len(group.Led) > 0 {
		m["led"] = group.Led
	}

	if group.Order > 0 {
		m["order"] = group.Order
	}

	return json.Marshal(m)
}

type Groups struct {
	List  []*Group
	mutex sync.Mutex
}

func NewGroups() *Groups {
	return &Groups{
		List:  []*Group{},
		mutex: sync.Mutex{},
	}
}

func (groups *Groups) FromMap(f []any) *Groups {
	groups.mutex.Lock()
	defer groups.mutex.Unlock()

	groups.List = []*Group{}

	for _, r := range f {
		switch m := r.(type) {
		case map[string]any:
			group := NewGroup().FromMap(m)
			groups.List = append(groups.List, group)
		}
	}

	return groups
}

func (groups *Groups) GetGroupById(id uint64) (group *Group, ok bool) {
	groups.mutex.Lock()
	defer groups.mutex.Unlock()

	for _, group := range groups.List {
		if group.Id == id {
			return group, true
		}
	}

	return nil, false
}

func (groups *Groups) GetGroupByLabel(label string) (group *Group, ok bool) {
	groups.mutex.Lock()
	defer groups.mutex.Unlock()

	for _, group := range groups.List {
		if group.Label == label {
			return group, true
		}
	}

	return nil, false
}

func (groups *Groups) GetGroupsData(systemsMap *SystemsMap) []Group {
	var list = []Group{}

	for _, systemMap := range *systemsMap {
		switch talkgroupsMap := systemMap["talkgroups"].(type) {
		case TalkgroupsMap:
			for _, talkgroupMap := range talkgroupsMap {
				switch label := talkgroupMap["group"].(type) {
				case string:
					if group, ok := groups.GetGroupByLabel(label); ok {
						add := true
						for _, l := range list {
							if l == *group {
								add = false
								break
							}
						}
						if add {
							list = append(list, *group)
						}
					}
				}
			}
		}
	}

	return list
}

func (groups *Groups) GetGroupIds(labels []string) []uint64 {
	var groupIds = []uint64{}

	for _, label := range labels {
		if group, ok := groups.GetGroupByLabel(label); ok {
			groupIds = append(groupIds, group.Id)
		}
	}

	return groupIds
}

func (groups *Groups) GetGroupsMap(systemsMap *SystemsMap) GroupsMap {
	var groupsMap = GroupsMap{}

	for _, system := range *systemsMap {
		var (
			fSystemId     = system["id"]
			fTalkgroups   = system["talkgroups"]
			systemId      uint
			talkgroupsMap TalkgroupsMap
		)

		switch v := fSystemId.(type) {
		case uint:
			systemId = v
		}

		switch v := fTalkgroups.(type) {
		case TalkgroupsMap:
			talkgroupsMap = v
		}

		for _, talkgroup := range talkgroupsMap {
			var (
				fTalkgroupGroups = talkgroup["groups"]
				fTalkgroupId     = talkgroup["id"]
				talkgroupGroups  []string
				talkgroupId      uint
			)

			switch v := fTalkgroupGroups.(type) {
			case []string:
				talkgroupGroups = v
			}

			switch v := fTalkgroupId.(type) {
			case uint:
				talkgroupId = v
			}

			for _, talkgroupGroup := range talkgroupGroups {
				group, ok := groups.GetGroupByLabel(talkgroupGroup)
				if !ok {
					continue
				}

				if groupsMap[group.Label] == nil {
					groupsMap[group.Label] = map[uint][]uint{}
				}

				if groupsMap[group.Label][systemId] == nil {
					groupsMap[group.Label][systemId] = []uint{}
				}

				found := false
				for _, id := range groupsMap[group.Label][systemId] {
					if id == talkgroupId {
						found = true
						break
					}
				}
				if !found {
					groupsMap[group.Label][systemId] = append(groupsMap[group.Label][systemId], talkgroupId)
				}
			}
		}
	}

	return groupsMap
}

func (groups *Groups) Read(db *Database) error {
	var (
		err   error
		query string
		rows  *sql.Rows
	)

	groups.mutex.Lock()
	defer groups.mutex.Unlock()

	groups.List = []*Group{}

	formatError := groups.errorFormatter("read")

	query = `SELECT "groupId", "alert", "label", "led", "order" FROM "groups"`
	if rows, err = db.Sql.Query(query); err != nil {
		return formatError(err, query)
	}

	for rows.Next() {
		group := NewGroup()

		if err = rows.Scan(&group.Id, &group.Alert, &group.Label, &group.Led, &group.Order); err != nil {
			break
		}

		groups.List = append(groups.List, group)
	}

	rows.Close()

	if err != nil {
		return formatError(err, query)
	}

	sort.Slice(groups.List, func(i int, j int) bool {
		return groups.List[i].Order < groups.List[j].Order
	})

	return nil
}

func (groups *Groups) Write(db *Database) error {
	var (
		err      error
		groupIds = []uint64{}
		query    string
		rows     *sql.Rows
		tx       *sql.Tx
	)

	groups.mutex.Lock()
	defer groups.mutex.Unlock()

	formatError := groups.errorFormatter("write")

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "groupId" FROM "groups"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		var groupId uint64
		if err = rows.Scan(&groupId); err != nil {
			break
		}
		remove := true
		for _, group := range groups.List {
			if group.Id == 0 || group.Id == groupId {
				remove = false
				break
			}
		}
		if remove {
			groupIds = append(groupIds, groupId)
		}
	}

	rows.Close()

	if err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	if len(groupIds) > 0 {
		if b, err := json.Marshal(groupIds); err == nil {
			in := strings.ReplaceAll(strings.ReplaceAll(string(b), "[", "("), "]", ")")
			query = fmt.Sprintf(`DELETE FROM "groups" WHERE "groupId" IN %s`, in)
			if _, err = tx.Exec(query); err != nil {
				tx.Rollback()
				return formatError(err, query)
			}
		}
	}

	for _, group := range groups.List {
		var count uint

		if group.Id > 0 {
			query = fmt.Sprintf(`SELECT COUNT(*) FROM "groups" WHERE "groupId" = %d`, group.Id)
			if err = tx.QueryRow(query).Scan(&count); err != nil {
				break
			}
		}

		if count == 0 {
			query = fmt.Sprintf(`INSERT INTO "groups" ("alert", "label", "led", "order") VALUES ('%s', '%s', '%s', %d)`, group.Alert, escapeQuotes(group.Label), group.Led, group.Order)
			if _, err = tx.Exec(query); err != nil {
				break
			}

		} else {
			query = fmt.Sprintf(`UPDATE "groups" SET "alert" = '%s', "label" = '%s', "led" = '%s', "order" = %d where "groupId" = %d`, group.Alert, escapeQuotes(group.Label), group.Led, group.Order, group.Id)
			if _, err = tx.Exec(query); err != nil {
				break
			}
		}
	}

	if err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	if err = tx.Commit(); err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	return nil
}

func (groups *Groups) errorFormatter(label string) func(err error, query string) error {
	return func(err error, query string) error {
		s := fmt.Sprintf("groups.%s: %s", label, err.Error())

		if len(query) > 0 {
			s = fmt.Sprintf("%s in %s", s, query)
		}

		return errors.New(s)
	}
}

type GroupsMap map[string]map[uint][]uint
