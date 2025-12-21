// Copyright (C) 2019-2026 Chrystian Huot <chrystian@huot.qc.ca>
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

type System struct {
	Id           uint64
	Alert        string
	AutoPopulate bool
	Blacklists   Blacklists
	Delay        uint
	Kind         string
	Label        string
	Led          string
	Order        uint
	Sites        *Sites
	SystemRef    uint
	Talkgroups   *Talkgroups
	Units        *Units
}

func NewSystem() *System {
	return &System{
		Sites:      NewSites(),
		Talkgroups: NewTalkgroups(),
		Units:      NewUnits(),
	}
}

func (system *System) FromMap(m map[string]any) *System {
	switch v := m["id"].(type) {
	case float64:
		system.Id = uint64(v)
	}

	switch v := m["alert"].(type) {
	case string:
		system.Alert = v
	}

	switch v := m["autoPopulate"].(type) {
	case bool:
		system.AutoPopulate = v
	}

	switch v := m["blacklists"].(type) {
	case string:
		system.Blacklists = Blacklists(v)
	}

	switch v := m["delay"].(type) {
	case float64:
		system.Delay = uint(v)
	}

	switch v := m["type"].(type) {
	case string:
		system.Kind = v
	}

	switch v := m["label"].(type) {
	case string:
		system.Label = v
	}

	switch v := m["led"].(type) {
	case string:
		if len(v) > 0 {
			system.Led = v
		}
	}

	switch v := m["order"].(type) {
	case float64:
		system.Order = uint(v)
	}

	switch v := m["sites"].(type) {
	case []any:
		system.Sites.FromMap(v)
	}

	switch v := m["systemRef"].(type) {
	case float64:
		system.SystemRef = uint(v)
	}

	switch v := m["talkgroups"].(type) {
	case []any:
		system.Talkgroups.FromMap(v)
	}

	switch v := m["units"].(type) {
	case []any:
		system.Units.FromMap(v)
	}

	return system
}

func (system *System) MarshalJSON() ([]byte, error) {
	m := map[string]any{
		"id":           system.Id,
		"autoPopulate": system.AutoPopulate,
		"label":        system.Label,
		"sites":        system.Sites.List,
		"systemRef":    system.SystemRef,
		"talkgroups":   system.Talkgroups.List,
		"units":        system.Units.List,
	}

	if len(system.Alert) > 0 {
		m["alert"] = system.Alert
	}

	if len(system.Blacklists) > 0 {
		m["blacklists"] = system.Blacklists
	}

	if system.Delay > 0 {
		m["delay"] = system.Delay
	}

	if len(system.Kind) > 0 {
		m["type"] = system.Kind
	}

	if len(system.Led) > 0 {
		m["led"] = system.Led
	}

	if system.Order > 0 {
		m["order"] = system.Order
	}

	return json.Marshal(m)
}

type SystemMap map[string]any

type Systems struct {
	List  []*System
	mutex sync.Mutex
}

func NewSystems() *Systems {
	return &Systems{
		List:  []*System{},
		mutex: sync.Mutex{},
	}
}

func (systems *Systems) FromMap(f []any) *Systems {
	systems.mutex.Lock()
	defer systems.mutex.Unlock()

	systems.List = []*System{}

	for _, r := range f {
		switch m := r.(type) {
		case map[string]any:
			system := NewSystem()
			system.FromMap(m)
			systems.List = append(systems.List, system)
		}
	}

	return systems
}

func (systems *Systems) GetNewSystemRef() uint {
	systems.mutex.Lock()
	defer systems.mutex.Unlock()

NextRef:
	for i := uint(1); i < 2e16; i++ {
		for _, s := range systems.List {
			if s.SystemRef == i {
				continue NextRef
			}
		}
		return i
	}
	return 0
}

func (systems *Systems) GetSystemById(id uint64) (system *System, ok bool) {
	systems.mutex.Lock()
	defer systems.mutex.Unlock()

	for _, system := range systems.List {
		if system.Id == id {
			return system, true
		}
	}

	return nil, false
}

func (systems *Systems) GetSystemByLabel(label string) (system *System, ok bool) {
	systems.mutex.Lock()
	defer systems.mutex.Unlock()

	for _, system := range systems.List {
		if system.Label == label {
			return system, true
		}
	}

	return nil, false
}

func (systems *Systems) GetSystemByRef(ref uint) (system *System, ok bool) {
	systems.mutex.Lock()
	defer systems.mutex.Unlock()

	for _, system := range systems.List {
		if system.SystemRef == ref {
			return system, true
		}
	}

	return nil, false
}

func (systems *Systems) GetScopedSystems(client *Client, groups *Groups, tags *Tags, sortTalkgroups bool) SystemsMap {
	var (
		rawSystems = []System{}
		systemsMap = SystemsMap{}
	)

	if client.Access == nil {
		for _, system := range systems.List {
			rawSystems = append(rawSystems, *system)
		}

	} else {
		switch v := client.Access.Systems.(type) {
		case nil:
			for _, system := range systems.List {
				rawSystems = append(rawSystems, *system)
			}

		case string:
			if v == "*" {
				for _, system := range systems.List {
					rawSystems = append(rawSystems, *system)
				}
			}

		case []any:
			for _, fSystem := range v {
				switch v := fSystem.(type) {
				case map[string]any:
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

					system, ok := systems.GetSystemByRef(systemId)
					if !ok {
						continue
					}

					switch v := mTalkgroups.(type) {
					case string:
						if mTalkgroups == "*" {
							rawSystems = append(rawSystems, *system)
							continue
						}

					case []any:
						rawSystem := *system
						rawSystem.Talkgroups = NewTalkgroups()
						for _, fTalkgroupId := range v {
							switch v := fTalkgroupId.(type) {
							case float64:
								rawTalkgroup, ok := system.Talkgroups.GetTalkgroupByRef(uint(v))
								if !ok {
									continue
								}
								rawSystem.Talkgroups.List = append(rawSystem.Talkgroups.List, rawTalkgroup)
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

	for _, rawSystem := range rawSystems {
		talkgroupsMap := TalkgroupsMap{}

		if sortTalkgroups {
			sort.Slice(rawSystem.Talkgroups.List, func(i int, j int) bool {
				return rawSystem.Talkgroups.List[i].Label < rawSystem.Talkgroups.List[j].Label
			})
			for i := range rawSystem.Talkgroups.List {
				rawSystem.Talkgroups.List[i].Order = uint(i + 1)
			}
		}

		for _, rawTalkgroup := range rawSystem.Talkgroups.List {
			var (
				groupLabel  string
				groupLabels = []string{}
			)

			for _, id := range rawTalkgroup.GroupIds {
				if group, ok := groups.GetGroupById(id); ok {
					groupLabels = append(groupLabels, group.Label)
				}
			}

			if len(groupLabels) > 0 {
				groupLabel = groupLabels[0]
			}

			tag, ok := tags.GetTagById(rawTalkgroup.TagId)
			if !ok {
				continue
			}

			talkgroupMap := TalkgroupMap{
				"id":        rawTalkgroup.TalkgroupRef,
				"alert":     rawTalkgroup.Alert,
				"frequency": rawTalkgroup.Frequency,
				"group":     groupLabel,
				"groups":    groupLabels,
				"label":     rawTalkgroup.Label,
				"led":       rawTalkgroup.Led,
				"name":      rawTalkgroup.Name,
				"order":     rawTalkgroup.Order,
				"tag":       tag.Label,
				"type":      rawTalkgroup.Kind,
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
			"id":         rawSystem.SystemRef,
			"alert":      rawSystem.Alert,
			"label":      rawSystem.Label,
			"led":        rawSystem.Led,
			"order":      rawSystem.Order,
			"talkgroups": talkgroupsMap,
			"units":      rawSystem.Units.List,
			"type":       rawSystem.Kind,
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

	return systemsMap
}

func (systems *Systems) Read(db *Database) error {
	var (
		err   error
		query string
		rows  *sql.Rows
		tx    *sql.Tx
	)

	systems.mutex.Lock()
	defer systems.mutex.Unlock()

	systems.List = []*System{}

	formatError := errorFormatter("systems", "read")

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "systemId", "alert", "autoPopulate", "blacklists", "delay", "label", "led", "order", "systemRef", "type" FROM "systems"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		system := NewSystem()

		if err = rows.Scan(&system.Id, &system.Alert, &system.AutoPopulate, &system.Blacklists, &system.Delay, &system.Label, &system.Led, &system.Order, &system.SystemRef, &system.Kind); err != nil {
			break
		}

		systems.List = append(systems.List, system)
	}

	rows.Close()

	if err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	for _, system := range systems.List {
		if err = system.Sites.ReadTx(tx, system.Id); err != nil {
			break
		}

		if err = system.Talkgroups.ReadTx(tx, system.Id, db.Config.DbType); err != nil {
			break
		}

		if err = system.Units.ReadTx(tx, system.Id); err != nil {
			break
		}
	}

	if err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	if err = tx.Commit(); err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	sort.Slice(systems.List, func(i int, j int) bool {
		return systems.List[i].Order < systems.List[j].Order
	})

	return nil
}

func (systems *Systems) Write(db *Database) error {
	var (
		err       error
		query     string
		res       sql.Result
		rows      *sql.Rows
		systemIds = []uint64{}
		tx        *sql.Tx
	)

	systems.mutex.Lock()
	defer systems.mutex.Unlock()

	formatError := errorFormatter("systems", "write")

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "systemId" FROM "systems"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		var systemId uint64
		if err = rows.Scan(&systemId); err != nil {
			break
		}
		remove := true
		for _, system := range systems.List {
			if system.Id == 0 || system.Id == systemId {
				remove = false
				break
			}
		}
		if remove {
			systemIds = append(systemIds, systemId)
		}
	}

	rows.Close()

	if err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	if len(systemIds) > 0 {
		if b, err := json.Marshal(systemIds); err == nil {
			in := strings.ReplaceAll(strings.ReplaceAll(string(b), "[", "("), "]", ")")

			query = fmt.Sprintf(`DELETE FROM "systems" WHERE "systemId" IN %s`, in)
			if res, err = tx.Exec(query); err != nil {
				tx.Rollback()
				return formatError(err, query)
			}

			if count, err := res.RowsAffected(); err == nil && count > 0 {
				query = fmt.Sprintf(`DELETE FROM "sites" WHERE "systemId" IN %s`, in)
				if _, err = tx.Exec(query); err != nil {
					tx.Rollback()
					return formatError(err, query)
				}

				query = fmt.Sprintf(`DELETE FROM "talkgroups" WHERE "systemId" IN %s`, in)
				if _, err = tx.Exec(query); err != nil {
					tx.Rollback()
					return formatError(err, query)
				}

				query = fmt.Sprintf(`DELETE FROM "units" WHERE "systemId" IN %s`, in)
				if _, err = tx.Exec(query); err != nil {
					tx.Rollback()
					return formatError(err, query)
				}
			}
		}
	}

	for _, system := range systems.List {
		var count uint

		if system.Id > 0 {
			query = fmt.Sprintf(`SELECT COUNT(*) FROM "systems" WHERE "systemId" = %d`, system.Id)
			if err = tx.QueryRow(query).Scan(&count); err != nil {
				break
			}
		}

		if count == 0 {
			query = fmt.Sprintf(`INSERT INTO "systems" ("alert", "autoPopulate", "blacklists", "delay", "label", "led", "order", "systemRef", "type") VALUES ('%s', %t, '%s', %d, '%s', '%s', %d, %d, '%s')`, system.Alert, system.AutoPopulate, system.Blacklists, system.Delay, escapeQuotes(system.Label), system.Led, system.Order, system.SystemRef, system.Kind)

			if db.Config.DbType == DbTypePostgresql {
				query = query + ` RETURNING "systemId"`

				if err = tx.QueryRow(query).Scan(&system.Id); err != nil {
					break
				}

			} else {
				if res, err = tx.Exec(query); err == nil {
					if id, err := res.LastInsertId(); err == nil {
						system.Id = uint64(id)
					}
				} else {
					break
				}
			}

		} else {
			query = fmt.Sprintf(`UPDATE "systems" SET "alert" = '%s', "autoPopulate" = %t, "blacklists" = '%s', "delay" = %d, "label" = '%s', "led" = '%s', "order" = %d, "systemRef" = %d, "type" = '%s' WHERE "systemId" = %d`, system.Alert, system.AutoPopulate, system.Blacklists, system.Delay, escapeQuotes(system.Label), system.Led, system.Order, system.SystemRef, system.Kind, system.Id)
			if _, err = tx.Exec(query); err != nil {
				break
			}
		}

		query = ""

		if err = system.Sites.WriteTx(tx, system.Id); err != nil {
			break
		}

		if err = system.Talkgroups.WriteTx(tx, system.Id, db.Config.DbType); err != nil {
			break
		}

		if err = system.Units.WriteTx(tx, system.Id); err != nil {
			break
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

type SystemsMap []SystemMap
