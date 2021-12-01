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
	"encoding/json"
	"fmt"
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
	Patches   string      `json:"patches"`
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

	switch v := m["patches"].(type) {
	case string:
		talkgroup.Patches = v
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

func (talkgroups *Talkgroups) FromJson(str string) error {
	var f interface{}

	*talkgroups = Talkgroups{}

	formatError := func(err error) error {
		return fmt.Errorf("talkgroups.fromjson")
	}

	if err := json.Unmarshal([]byte(str), &f); err != nil {
		return formatError(err)
	}

	switch v := f.(type) {
	case []interface{}:
		for _, r := range v {
			switch m := r.(type) {
			case map[string]interface{}:
				talkgroup := &Talkgroup{}
				talkgroup.FromMap(m)
				*talkgroups = append(*talkgroups, talkgroup)
			}
		}
	}

	return nil
}

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

func (talkgroups *Talkgroups) ToJson() (string, error) {
	if b, err := json.Marshal(*talkgroups); err == nil {
		return string(b), nil

	} else {
		return "", fmt.Errorf("talkgroups.tojson: %v", err)
	}
}

type TalkgroupsMap []TalkgroupMap
