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

package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Unit struct {
	Id    uint   `json:"id"`
	Label string `json:"label"`
	Order uint   `json:"order"`
}

func (unit *Unit) FromMap(m map[string]any) *Unit {
	switch v := m["id"].(type) {
	case float64:
		unit.Id = uint(v)
	}

	switch v := m["label"].(type) {
	case string:
		unit.Label = v
	}

	switch v := m["order"].(type) {
	case float64:
		unit.Order = uint(v)
	}

	return unit
}

type Units struct {
	List  []*Unit
	mutex sync.Mutex
}

func NewUnits() *Units {
	return &Units{
		List:  []*Unit{},
		mutex: sync.Mutex{},
	}
}

func (units *Units) Add(id uint, label string) (*Units, bool) {
	added := true

	for _, u := range units.List {
		if u.Id == id {
			added = false
			break
		}
	}

	if added {
		units.List = append(units.List, &Unit{Id: id, Label: label})
	}

	return units, added
}

func (units *Units) FromMap(f []any) *Units {
	units.mutex.Lock()
	defer units.mutex.Unlock()

	units.List = []*Unit{}

	for _, r := range f {
		switch m := r.(type) {
		case map[string]any:
			unit := &Unit{}
			unit.FromMap(m)
			units.List = append(units.List, unit)
		}
	}

	return units
}

func (u *Units) Merge(units *Units) bool {
	merged := false

	if units != nil {
		u.mutex.Lock()
		defer u.mutex.Unlock()

		for _, v := range units.List {
			if _, added := u.Add(v.Id, v.Label); added {
				merged = added
			}
		}
	}

	return merged
}

func (units *Units) Read(db *Database, systemId uint) error {
	var (
		err  error
		rows *sql.Rows
	)

	units.mutex.Lock()
	defer units.mutex.Unlock()

	units.List = []*Unit{}

	formatError := func(err error) error {
		return fmt.Errorf("units.read: %v", err)
	}

	if rows, err = db.Sql.Query("select `id`, `label`, `order` from `rdioScannerUnits` where `systemId` = ?", systemId); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		unit := &Unit{}

		if err = rows.Scan(&unit.Id, &unit.Label, &unit.Order); err != nil {
			break
		}

		units.List = append(units.List, unit)
	}

	rows.Close()

	if err != nil {
		return formatError(err)
	}

	sort.Slice(units.List, func(i int, j int) bool {
		return units.List[i].Order < units.List[j].Order
	})

	return nil
}

func (units *Units) Write(db *Database, systemId uint) error {
	var (
		count uint
		err   error
		ids   = []uint{}
		rows  *sql.Rows
	)

	units.mutex.Lock()
	defer units.mutex.Unlock()

	formatError := func(err error) error {
		return fmt.Errorf("units.write: %v", err)
	}

	if rows, err = db.Sql.Query("select `id` from `rdioScannerUnits` where `systemId` = ?", systemId); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		var id uint
		if err = rows.Scan(&id); err != nil {
			break
		}
		remove := true
		for _, unit := range units.List {
			if unit.Id == id {
				remove = false
				break
			}
		}
		if remove {
			ids = append(ids, id)
		}
	}

	rows.Close()

	if err != nil {
		return formatError(err)
	}

	if len(ids) > 0 {
		if b, err := json.Marshal(ids); err == nil {
			s := string(b)
			s = strings.ReplaceAll(s, "[", "(")
			s = strings.ReplaceAll(s, "]", ")")
			q := fmt.Sprintf("delete from `rdioScannerUnits` where `id` in %v and `systemId` = %v", s, systemId)
			if _, err = db.Sql.Exec(q); err != nil {
				return formatError(err)
			}
		}
	}

	for _, unit := range units.List {
		if err = db.Sql.QueryRow("select count(*) from `rdioScannerUnits` where `id` = ? and `systemId` = ?", unit.Id, systemId).Scan(&count); err != nil {
			break
		}

		if count == 0 {
			if _, err = db.Sql.Exec("insert into `rdioScannerUnits` (`id`, `label`, `order`, `systemId`) values (?, ?, ?, ?)", unit.Id, unit.Label, unit.Order, systemId); err != nil {
				break
			}

		} else if _, err = db.Sql.Exec("update `rdioScannerUnits` set `label` = ?, `order` = ? where `id` = ? and `systemId` = ?", unit.Label, unit.Order, unit.Id, systemId); err != nil {
			break
		}
	}

	if err != nil {
		return formatError(err)
	}

	return nil
}
