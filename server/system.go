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
	"sort"
	"strconv"
	"strings"
)

type System struct {
	Id           uint        `json:"id"`
	AutoPopulate bool        `json:"autoPopulate"`
	Blacklists   Blacklists  `json:"blacklists"`
	Label        string      `json:"label"`
	Led          interface{} `json:"led"`
	Order        interface{} `json:"order"`
	RowId        interface{} `json:"_id"`
	Talkgroups   Talkgroups  `json:"talkgroups"`
	Units        Units       `json:"units"`
}

func (system *System) FromMap(m map[string]interface{}) {
	switch v := m["_id"].(type) {
	case float64:
		system.RowId = uint(v)
	}

	switch v := m["id"].(type) {
	case float64:
		system.Id = uint(v)
	}

	switch v := m["autoPopulate"].(type) {
	case bool:
		system.AutoPopulate = v
	}

	switch v := m["blacklists"].(type) {
	case string:
		system.Blacklists = Blacklists(v)
	}

	switch v := m["label"].(type) {
	case string:
		system.Label = v
	}

	switch v := m["led"].(type) {
	case string:
		system.Led = v
	}

	switch v := m["order"].(type) {
	case float64:
		system.Order = uint(v)
	}

	switch v := m["talkgroups"].(type) {
	case []interface{}:
		system.Talkgroups.FromMap(v)
	}

	switch v := m["units"].(type) {
	case []interface{}:
		system.Units.FromMap(v)
	}
}

type SystemMap map[string]interface{}

type Systems []*System

func (systems *Systems) FromMap(f []interface{}) {
	*systems = Systems{}

	for _, r := range f {
		switch m := r.(type) {
		case map[string]interface{}:
			system := &System{}
			system.FromMap(m)
			*systems = append(*systems, system)
		}
	}
}

func (systems *Systems) GetNewSystemId() uint {
NextId:
	for i := uint(1); i < 65535; i++ {
		for _, s := range *systems {
			if s.Id == i {
				continue NextId
			}
		}
		return i
	}
	return 0
}

func (systems *Systems) GetSystem(f interface{}) (system *System, ok bool) {
	switch v := f.(type) {
	case uint:
		for _, system := range *systems {
			if system.Id == v {
				return system, true
			}
		}
	case string:
		for _, system := range *systems {
			if system.Label == v {
				return system, true
			}
		}
	}
	return nil, false
}

func (systems *Systems) GetScopedSystems(client *Client, groups *Groups, tags *Tags, sortTalkgroups bool) *SystemsMap {
	var (
		rawSystems = Systems{}
		systemsMap = SystemsMap{}
	)

	if client.Access == nil {
		rawSystems = append(rawSystems, *systems...)

	} else {
		switch v := client.Access.Systems.(type) {
		case nil:
			rawSystems = append(rawSystems, *systems...)

		case string:
			if v == "*" {
				rawSystems = append(rawSystems, *systems...)
			}

		case []interface{}:
			for _, fSystem := range v {
				switch v := fSystem.(type) {
				case map[string]interface{}:
					var (
						mSystemId   = v["id"]
						mTalkgroups = v["talkgroups"]
						systemId    uint
					)

					switch v := mSystemId.(type) {
					case float64:
						systemId = uint(v)
					default:
						continue
					}

					system, ok := systems.GetSystem(systemId)
					if !ok {
						continue
					}

					switch v := mTalkgroups.(type) {
					case string:
						if mTalkgroups == "*" {
							rawSystems = append(rawSystems, system)
							continue
						}

					case []interface{}:
						rawSystem := system
						rawSystem.Talkgroups = Talkgroups{}
						for _, fTalkgroupId := range v {
							switch v := fTalkgroupId.(type) {
							case float64:
								talkgroupId := uint(v)
								rawTalkgroup, ok := system.Talkgroups.GetTalkgroup(talkgroupId)
								if !ok {
									continue
								}
								rawSystem.Talkgroups = append(rawSystem.Talkgroups, rawTalkgroup)
							default:
								continue
							}
						}
						rawSystems = append(rawSystems, rawSystem)
					}
				}
			}
		}
	}

	for i, rawSystem := range rawSystems {
		talkgroupsMap := TalkgroupsMap{}

		if sortTalkgroups {
			sort.Slice(rawSystem.Talkgroups, func(i int, j int) bool {
				return rawSystem.Talkgroups[i].Label < rawSystem.Talkgroups[j].Label
			})
			for i := range rawSystem.Talkgroups {
				rawSystem.Talkgroups[i].Order = uint(i + 1)
			}
		}

		for j, rawTalkgroup := range rawSystem.Talkgroups {
			group, ok := groups.GetGroup(rawTalkgroup.GroupId)
			if !ok {
				continue
			}
			rawSystems[i].Talkgroups[j].group = group.Label

			tag, ok := tags.GetTag(rawTalkgroup.TagId)
			if !ok {
				continue
			}
			rawSystems[i].Talkgroups[j].tag = tag.Label

			talkgroupMap := TalkgroupMap{
				"id":    rawTalkgroup.Id,
				"group": group.Label,
				"label": rawTalkgroup.Label,
				"name":  rawTalkgroup.Name,
				"order": rawTalkgroup.Order,
				"tag":   tag.Label,
			}

			if rawTalkgroup.Frequency != nil {
				talkgroupMap["frequency"] = rawTalkgroup.Frequency
			}

			if rawTalkgroup.Led != nil {
				talkgroupMap["led"] = rawTalkgroup.Led
			}

			talkgroupsMap = append(talkgroupsMap, talkgroupMap)
		}

		sort.Slice(talkgroupsMap, func(i int, j int) bool {
			if a, err := strconv.Atoi(fmt.Sprintf("%v", talkgroupsMap[i]["order"])); err == nil {
				if b, err := strconv.Atoi(fmt.Sprintf("%v", talkgroupsMap[j]["order"])); err == nil {
					return a < b
				}
			}
			return false
		})

		systemMap := SystemMap{
			"id":         rawSystem.Id,
			"label":      rawSystem.Label,
			"talkgroups": talkgroupsMap,
			"units":      rawSystem.Units,
		}

		if rawSystem.Led != nil {
			systemMap["led"] = rawSystem.Led
		}

		if rawSystem.Order != nil {
			systemMap["order"] = rawSystem.Order
		}

		systemsMap = append(systemsMap, systemMap)
	}

	sort.Slice(systemsMap, func(i int, j int) bool {
		if a, err := strconv.Atoi(fmt.Sprintf("%v", systemsMap[i]["order"])); err == nil {
			if b, err := strconv.Atoi(fmt.Sprintf("%v", systemsMap[j]["order"])); err == nil {
				return a < b
			}
		}
		return false
	})

	return &systemsMap
}

func (systems *Systems) Read(db *Database) error {
	var (
		err  error
		rows *sql.Rows
	)

	*systems = Systems{}

	formatError := func(err error) error {
		return fmt.Errorf("systems.read: %v", err)
	}

	if rows, err = db.Sql.Query("select `_id`, `autoPopulate`, `blacklists`, `id`, `label`, `led`, `order`, `talkgroups`, `units` from `rdioScannerSystems`"); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		var (
			blacklists interface{}
			talkgroups interface{}
			units      interface{}
		)

		system := &System{}
		if err = rows.Scan(&system.RowId, &system.AutoPopulate, &blacklists, &system.Id, &system.Label, &system.Led, &system.Order, &talkgroups, &units); err != nil {
			break
		}

		switch v := blacklists.(type) {
		case string:
			v = strings.ReplaceAll(v, "[", "")
			v = strings.ReplaceAll(v, "]", "")
			system.Blacklists = Blacklists(v)
		}

		switch v := system.RowId.(type) {
		case int64:
			system.RowId = uint(v)
		}

		switch v := talkgroups.(type) {
		case []uint8:
			system.Talkgroups = Talkgroups{}
			if err = system.Talkgroups.FromJson(string(v)); err != nil {
				break
			}
		case string:
			system.Talkgroups = Talkgroups{}
			if err = system.Talkgroups.FromJson(v); err != nil {
				break
			}
		default:
			system.Talkgroups = Talkgroups{}
		}

		switch v := units.(type) {
		case []uint8:
			system.Units = Units{}
			if err = system.Units.FromJson(string(v)); err != nil {
				break
			}
		case string:
			system.Units = Units{}
			if err = system.Units.FromJson(v); err != nil {
				break
			}
		default:
			system.Units = Units{}
		}

		*systems = append(*systems, system)
	}

	rows.Close()

	if err != nil {
		return formatError(err)
	}

	return nil
}

func (systems *Systems) Write(db *Database) error {
	var (
		blacklists string
		count      uint
		err        error
		rows       *sql.Rows
		talkgroups string
		units      string
	)

	formatError := func(err error) error {
		return fmt.Errorf("systems.write: %v", err)
	}

	for _, system := range *systems {
		if len(system.Blacklists) > 0 {
			blacklists = strings.Join([]string{"[", system.Blacklists.String(), "]"}, "")
		} else {
			blacklists = "[]"
		}

		if talkgroups, err = system.Talkgroups.ToJson(); err != nil {
			return formatError(err)
		}

		if units, err = system.Units.ToJson(); err != nil {
			return formatError(err)
		}

		if err = db.Sql.QueryRow("select count(*) from `rdioScannerSystems` where `_id` = ?", system.RowId).Scan(&count); err != nil {
			break
		}

		if count == 0 {
			if _, err = db.Sql.Exec("insert into `rdioScannerSystems` (`_id`, `autoPopulate`, `blacklists`, `id`, `label`, `led`, `order`, `talkgroups`, `units`) values (?, ?, ?, ?, ?, ?, ?, ?, ?)", system.RowId, system.AutoPopulate, blacklists, system.Id, system.Label, system.Led, system.Order, talkgroups, units); err != nil {
				break
			}

		} else if _, err = db.Sql.Exec("update `rdioScannerSystems` set `_id` = ?, `autoPopulate` = ?, `blacklists` = ?, `id` = ?, `label` = ?, `led` = ?, `order` = ?, `talkgroups` = ?, `units` = ? where `_id` = ?", system.RowId, system.AutoPopulate, blacklists, system.Id, system.Label, system.Led, system.Order, talkgroups, units, system.RowId); err != nil {
			break
		}
	}

	if err != nil {
		return formatError(err)
	}

	if rows, err = db.Sql.Query("select `_id` from `rdioScannerSystems`"); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		var rowId uint
		rows.Scan(&rowId)
		remove := true
		for _, system := range *systems {
			if system.RowId == nil || system.RowId == rowId {
				remove = false
				break
			}
		}
		if remove {
			if _, err = db.Sql.Exec("delete from `rdioScannerSystems` where `_id` = ?", rowId); err != nil {
				break
			}
		}
	}

	rows.Close()

	if err != nil {
		return formatError(err)
	}

	return nil
}

type SystemsMap []SystemMap
