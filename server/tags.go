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

type Tags []Tag

func (tags *Tags) FromMap(f []interface{}) {
	*tags = Tags{}

	for _, r := range f {
		switch m := r.(type) {
		case map[string]interface{}:
			tag := Tag{}
			tag.FromMap(m)
			*tags = append(*tags, tag)
		}
	}
}

func (tags *Tags) GetTag(f interface{}) (tag *Tag, ok bool) {
	switch v := f.(type) {
	case uint:
		for _, tag := range *tags {
			if tag.Id == v {
				return &tag, true
			}
		}
	case string:
		for _, tag := range *tags {
			if tag.Label == v {
				return &tag, true
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
		rows *sql.Rows
	)

	*tags = Tags{}

	formatError := func(err error) error {
		return fmt.Errorf("tags read: %v", err)
	}

	if rows, err = db.Sql.Query("select `_id`, `label` from `rdioScannerTags`"); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		var id uint

		tag := Tag{}

		rows.Scan(&id, &tag.Label)

		tag.Id = id

		*tags = append(*tags, tag)
	}

	if err != nil {
		return formatError(err)
	}

	if err = rows.Close(); err != nil {
		return formatError(err)
	}

	return nil
}

func (tags *Tags) Write(db *Database) error {
	var (
		count uint
		err   error
		rows  *sql.Rows
	)

	formatError := func(err error) error {
		return fmt.Errorf("tags write %v", err)
	}

	for _, tag := range *tags {
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
		var id uint
		rows.Scan(&id)
		remove := true
		for _, tag := range *tags {
			if tag.Id == nil || tag.Id == id {
				remove = false
				break
			}
		}
		if remove {
			if _, err = db.Sql.Exec("delete from `rdioScannerTags` where `_id` = ?", id); err != nil {
				break
			}
		}
	}

	if err != nil {
		return formatError(err)
	}

	if err = rows.Close(); err != nil {
		return formatError(err)
	}

	return nil
}

type TagsMap map[string]map[uint][]uint
