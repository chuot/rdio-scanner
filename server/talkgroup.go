// Copyright (C) 2019-2022 Chrystian Huot <chrystian.huot@saubeo.solutions>
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
)

type Talkgroup struct {
	Frequency interface{} `json:"frequency"`
	group     string
	GroupId   uint        `json:"groupId"`
	Id        uint        `json:"id"`
	Label     string      `json:"label"`
	Led       interface{} `json:"led"`
	Name      string      `json:"name"`
	Order     uint        `json:"order"`
	TagId     uint        `json:"tagId"`
	tag       string
}

func (talkgroup *Talkgroup) FromMap(m map[string]interface{}) {
	switch v := m["id"].(type) {
	case float64:
		talkgroup.Id = uint(v)
	}

	switch v := m["frequency"].(type) {
	case float64:
		talkgroup.Frequency = uint(v)
	}

	switch v := m["group"].(type) {
	case string:
		talkgroup.group = v
	}

	switch v := m["groupId"].(type) {
	case float64:
		talkgroup.GroupId = uint(v)
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

	switch v := m["tag"].(type) {
	case string:
		talkgroup.tag = v
	}

	switch v := m["tagId"].(type) {
	case float64:
		talkgroup.TagId = uint(v)
	}
}

type TalkgroupMap map[string]interface{}

type Talkgroups []*Talkgroup

func (talkgroups *Talkgroups) FromMap(f []interface{}) {
	*talkgroups = Talkgroups{}

	for _, r := range f {
		switch m := r.(type) {
		case map[string]interface{}:
			talkgroup := &Talkgroup{}
			talkgroup.FromMap(m)
			*talkgroups = append(*talkgroups, talkgroup)
		}
	}
}

func (talkgroups *Talkgroups) GetTalkgroup(f interface{}) (system *Talkgroup, ok bool) {
	switch v := f.(type) {
	case uint:
		for _, talkgroup := range *talkgroups {
			if talkgroup.Id == v {
				return talkgroup, true
			}
		}
	case string:
		for _, talkgroup := range *talkgroups {
			if talkgroup.Label == v {
				return talkgroup, true
			}
		}
	}
	return nil, false
}

func (talkgroups *Talkgroups) Read(db *Database, systemId uint) error {
	var (
		err       error
		frequency sql.NullFloat64
		led       sql.NullString
		rows      *sql.Rows
	)

	*talkgroups = Talkgroups{}

	formatError := func(err error) error {
		return fmt.Errorf("talkgroups.read: %v", err)
	}

	if rows, err = db.Sql.Query("select `frequency`, `groupId`, `id`, `label`, `led`, `name`, `order`, `tagId` from `rdioScannerTalkgroups` where `systemId` = ?", systemId); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		talkgroup := &Talkgroup{}

		if err = rows.Scan(&frequency, &talkgroup.GroupId, &talkgroup.Id, &talkgroup.Label, &led, &talkgroup.Name, &talkgroup.Order, &talkgroup.TagId); err != nil {
			continue
		}

		if frequency.Valid && frequency.Float64 > 0 {
			talkgroup.Frequency = uint(frequency.Float64)
		}

		if led.Valid && len(led.String) > 0 {
			talkgroup.Led = led.String
		}

		*talkgroups = append(*talkgroups, talkgroup)
	}

	rows.Close()

	if err != nil {
		return formatError(err)
	}

	sort.Slice(*talkgroups, func(i int, j int) bool {
		return (*talkgroups)[i].Order < (*talkgroups)[j].Order
	})

	return nil
}

func (talkgroups *Talkgroups) Write(db *Database, systemId uint) error {
	var (
		count uint
		err   error
		ids   = []uint{}
		rows  *sql.Rows
	)

	formatError := func(err error) error {
		return fmt.Errorf("talkgroups.write: %v", err)
	}

	for _, talkgroup := range *talkgroups {
		if err = db.Sql.QueryRow("select count(*) from `rdioScannerTalkgroups` where `id` = ? and `systemId` = ?", talkgroup.Id, systemId).Scan(&count); err != nil {
			break
		}

		if count == 0 {
			if _, err = db.Sql.Exec("insert into `rdioScannerTalkgroups` (`frequency`, `groupId`, `id`, `label`, `led`, `name`, `order`, `systemId`, `tagId`) values (?, ?, ?, ?, ?, ?, ?, ?, ?)", talkgroup.Frequency, talkgroup.GroupId, talkgroup.Id, talkgroup.Label, talkgroup.Led, talkgroup.Name, talkgroup.Order, systemId, talkgroup.TagId); err != nil {
				break
			}

		} else if _, err = db.Sql.Exec("update `rdioScannerTalkgroups` set `frequency` = ?, `groupId` = ?, `label` = ?, `led` = ?, `name` = ?, `order` = ?, `tagId` = ? where `id` = ? and `systemId` = ?", talkgroup.Frequency, talkgroup.GroupId, talkgroup.Label, talkgroup.Led, talkgroup.Name, talkgroup.Order, talkgroup.TagId, talkgroup.Id, systemId); err != nil {
			break
		}
	}

	if err != nil {
		return formatError(err)
	}

	if rows, err = db.Sql.Query("select `id` from `rdioScannerTalkgroups` where `systemId` = ?", systemId); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		var id uint
		rows.Scan(&id)
		remove := true
		for _, talkgroup := range *talkgroups {
			if talkgroup.Id == id {
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
			q := fmt.Sprintf("delete from `rdioScannerTalkgroups` where `id` in %v and `systemId` = %v", s, systemId)
			if _, err = db.Sql.Exec(q); err != nil {
				return formatError(err)
			}
		}
	}

	return nil
}

type TalkgroupsMap []TalkgroupMap
