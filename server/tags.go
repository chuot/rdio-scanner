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
	"strings"
	"sync"
)

type Tag struct {
	Id    interface{} `json:"_id"`
	Label string      `json:"label"`
}

func (tag *Tag) FromMap(m map[string]interface{}) {
	switch v := m["_id"].(type) {
	case float64:
		tag.Id = uint(v)
	}

	switch v := m["label"].(type) {
	case string:
		tag.Label = v
	}
}

type Tags struct {
	List  []*Tag
	mutex sync.RWMutex
}

func NewTags() *Tags {
	return &Tags{
		List:  []*Tag{},
		mutex: sync.RWMutex{},
	}
}

func (tags *Tags) FromMap(f []interface{}) {
	tags.mutex.Lock()
	defer tags.mutex.Unlock()

	tags.List = []*Tag{}

	for _, r := range f {
		switch m := r.(type) {
		case map[string]interface{}:
			tag := &Tag{}
			tag.FromMap(m)
			tags.List = append(tags.List, tag)
		}
	}
}

func (tags *Tags) GetTag(f interface{}) (tag *Tag, ok bool) {
	tags.mutex.RLock()
	defer tags.mutex.RUnlock()

	switch v := f.(type) {
	case uint:
		for _, tag := range tags.List {
			if tag.Id == v {
				return tag, true
			}
		}
	case string:
		for _, tag := range tags.List {
			if tag.Label == v {
				return tag, true
			}
		}
	}

	return nil, false
}

func (tags *Tags) GetTagsMap(systemsMap *SystemsMap) *TagsMap {
	tagsMap := TagsMap{}

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
				fTalkgroupTag = talkgroup["tag"]
				fTalkgroupId  = talkgroup["id"]
				talkgroupTag  string
				talkgroupId   uint
			)

			switch v := fTalkgroupTag.(type) {
			case string:
				talkgroupTag = v
			}

			switch v := fTalkgroupId.(type) {
			case uint:
				talkgroupId = v
			}

			tag, ok := tags.GetTag(talkgroupTag)
			if !ok {
				continue
			}

			if tagsMap[tag.Label] == nil {
				tagsMap[tag.Label] = map[uint][]uint{}
			}

			if tagsMap[tag.Label][systemId] == nil {
				tagsMap[tag.Label][systemId] = []uint{}
			}

			found := false
			for _, id := range tagsMap[tag.Label][systemId] {
				if id == talkgroupId {
					found = true
					break
				}
			}
			if !found {
				tagsMap[tag.Label][systemId] = append(tagsMap[tag.Label][systemId], talkgroupId)
			}
		}
	}

	return &tagsMap
}

func (tags *Tags) Read(db *Database) error {
	var (
		err  error
		id   sql.NullFloat64
		rows *sql.Rows
	)

	tags.mutex.RLock()
	defer tags.mutex.RUnlock()

	tags.List = []*Tag{}

	formatError := func(err error) error {
		return fmt.Errorf("tags read: %v", err)
	}

	if rows, err = db.Sql.Query("select `_id`, `label` from `rdioScannerTags`"); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		tag := &Tag{}

		if err = rows.Scan(&id, &tag.Label); err != nil {
			break
		}

		if id.Valid && id.Float64 > 0 {
			tag.Id = uint(id.Float64)
		}

		tags.List = append(tags.List, tag)
	}

	rows.Close()

	if err != nil {
		return formatError(err)
	}

	return nil
}

func (tags *Tags) Write(db *Database) error {
	var (
		count  uint
		err    error
		rows   *sql.Rows
		rowIds = []uint{}
	)

	tags.mutex.Lock()
	defer tags.mutex.Unlock()

	formatError := func(err error) error {
		return fmt.Errorf("tags write %v", err)
	}

	for _, tag := range tags.List {
		if err = db.Sql.QueryRow("select count(*) from `rdioScannerTags` where `_id` = ?", tag.Id).Scan(&count); err != nil {
			break
		}

		if count == 0 {
			if _, err = db.Sql.Exec("insert into `rdioScannerTags` (`_id`, `label`) values (?, ?)", tag.Id, tag.Label); err != nil {
				break
			}
		} else if _, err = db.Sql.Exec("update `rdioScannerTags` set `_id` = ?, `label` = ? where `_id` = ?", tag.Id, tag.Label, tag.Id); err != nil {
			break
		}
	}

	if err != nil {
		return formatError(err)
	}

	if rows, err = db.Sql.Query("select `_id` from `rdioScannerTags`"); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		var rowId uint
		rows.Scan(&rowId)
		remove := true
		for _, tag := range tags.List {
			if tag.Id == nil || tag.Id == rowId {
				remove = false
				break
			}
		}
		if remove {
			rowIds = append(rowIds, rowId)
		}
	}

	rows.Close()

	if err != nil {
		return formatError(err)
	}

	if len(rowIds) > 0 {
		if b, err := json.Marshal(rowIds); err == nil {
			s := string(b)
			s = strings.ReplaceAll(s, "[", "(")
			s = strings.ReplaceAll(s, "]", ")")
			q := fmt.Sprintf("delete from `rdioScannerTags` where `_id` in %v", s)
			if _, err = db.Sql.Exec(q); err != nil {
				return formatError(err)
			}
		}
	}

	return nil
}

type TagsMap map[string]map[uint][]uint
