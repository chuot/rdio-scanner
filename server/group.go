// Copyright (C) 2019-2021 Chrystian Huot <chrystian.huot@saubeo.solutions>
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
	"fmt"
)

type Group struct {
	Id    interface{} `json:"_id"`
	Label string      `json:"label"`
}

func (group *Group) FromMap(m map[string]interface{}) {
	switch v := m["_id"].(type) {
	case float64:
		group.Id = uint(v)
	}

	switch v := m["label"].(type) {
	case string:
		group.Label = v
	}
}

type Groups []Group

func (groups *Groups) FromMap(f []interface{}) {
	*groups = Groups{}

	for _, r := range f {
		switch m := r.(type) {
		case map[string]interface{}:
			group := Group{}
			group.FromMap(m)
			*groups = append(*groups, group)
		}
	}
}

func (groups *Groups) GetGroup(f interface{}) (group *Group, ok bool) {
	switch v := f.(type) {
	case uint:
		for _, group := range *groups {
			if group.Id == v {
				return &group, true
			}
		}
	case string:
		for _, group := range *groups {
			if group.Label == v {
				return &group, true
			}
		}
	}
	return nil, false
}

func (groups *Groups) GetGroupsMap(systemsMap *SystemsMap) *GroupsMap {
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
				fTalkgroupGroup = talkgroup["group"]
				fTalkgroupId    = talkgroup["id"]
				talkgroupGroup  string
				talkgroupId     uint
			)

			switch v := fTalkgroupGroup.(type) {
			case string:
				talkgroupGroup = v
			}

			switch v := fTalkgroupId.(type) {
			case uint:
				talkgroupId = v
			}

			group, ok := groups.GetGroup(talkgroupGroup)
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

	return &groupsMap
}

func (groups *Groups) Read(db *Database) error {
	var (
		err  error
		rows *sql.Rows
	)

	*groups = Groups{}

	formatError := func(err error) error {
		return fmt.Errorf("groups.read: %v", err)
	}

	if rows, err = db.Sql.Query("select `_id`, `label` from `rdioScannerGroups`"); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		var id uint

		group := Group{}

		rows.Scan(&id, &group.Label)

		group.Id = id

		*groups = append(*groups, group)
	}

	if err != nil {
		return formatError(err)
	}

	if err = rows.Close(); err != nil {
		return formatError(err)
	}

	return nil
}

func (groups *Groups) Write(db *Database) error {
	var (
		count uint
		err   error
		rows  *sql.Rows
	)

	formatError := func(err error) error {
		return fmt.Errorf("groups.write %v", err)
	}

	for _, group := range *groups {
		if err = db.Sql.QueryRow("select count(*) from `rdioScannerGroups` where `_id` = ?", group.Id).Scan(&count); err != nil {
			break
		}

		if count == 0 {
			if _, err = db.Sql.Exec("insert into `rdioScannerGroups` (`_id`, `label`) values (?, ?)", group.Id, group.Label); err != nil {
				break
			}

		} else if _, err = db.Sql.Exec("update `rdioScannerGroups` set `_id` = ?, `label` = ? where `_id` = ?", group.Id, group.Label, group.Id); err != nil {
			break
		}
	}

	if err != nil {
		return formatError(err)
	}

	if rows, err = db.Sql.Query("select `_id` from `rdioScannerGroups`"); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		var id uint
		rows.Scan(&id)
		remove := true
		for _, group := range *groups {
			if group.Id == nil || group.Id == id {
				remove = false
				break
			}
		}
		if remove {
			if _, err = db.Sql.Exec("delete from `rdioScannerGroups` where `_id` = ?", id); err != nil {
				break
			}
		}
	}

	if err != nil {
		rows.Close()
		return formatError(err)
	}

	if err = rows.Close(); err != nil {
		return formatError(err)
	}

	return nil
}

type GroupsMap map[string]map[uint][]uint
