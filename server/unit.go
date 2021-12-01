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

type Unit struct {
	Id    uint   `json:"id"`
	Label string `json:"label"`
}

func (unit *Unit) FromMap(m map[string]interface{}) {
	switch v := m["id"].(type) {
	case float64:
		unit.Id = uint(v)
	}

	switch v := m["label"].(type) {
	case string:
		unit.Label = v
	}
}

type Units []Unit

func (units *Units) Add(id uint, label string) *Units {
	found := false

	for _, u := range *units {
		if u.Id == id {
			found = true
			break
		}
	}

	if !found {
		*units = append(*units, Unit{Id: id, Label: label})
	}

	return units
}

func (units *Units) FromJson(str string) error {
	var v interface{}

	*units = Units{}

	formatError := func(err error) error {
		return fmt.Errorf("units.fromjson")
	}

	if err := json.Unmarshal([]byte(str), &v); err != nil {
		return formatError(err)
	}

	switch v.(type) {
	case []interface{}:
		for _, r := range v.([]interface{}) {
			switch m := r.(type) {
			case map[string]interface{}:
				unit := Unit{}
				unit.FromMap(m)
				*units = append(*units, unit)
			}
		}
	}

	return nil
}

func (u *Units) Merge(units *Units) {
	for _, v := range *units {
		u.Add(v.Id, v.Label)
	}
}

func (units *Units) ToJson() (string, error) {
	if b, err := json.Marshal(*units); err == nil {
		return string(b), nil

	} else {
		return "", fmt.Errorf("units.tojson: %v", err)
	}
}

func (units *Units) FromMap(f []interface{}) {
	*units = Units{}

	for _, r := range f {
		switch m := r.(type) {
		case map[string]interface{}:
			unit := Unit{}
			unit.FromMap(m)
			*units = append(*units, unit)
		}
	}
}
