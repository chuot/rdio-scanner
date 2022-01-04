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

	"github.com/google/uuid"
)

type Apikey struct {
	Id       interface{} `json:"_id"`
	Disabled bool        `json:"disabled"`
	Ident    string      `json:"ident"`
	Key      string      `json:"key"`
	Order    interface{} `json:"order"`
	Systems  interface{} `json:"systems"`
}

func (apikey *Apikey) FromMap(m map[string]interface{}) {
	switch v := m["_id"].(type) {
	case float64:
		apikey.Id = uint(v)
	}

	switch v := m["disabled"].(type) {
	case bool:
		apikey.Disabled = v
	}

	switch v := m["ident"].(type) {
	case string:
		apikey.Ident = v
	}

	switch v := m["key"].(type) {
	case string:
		apikey.Key = v
	}

	switch v := m["order"].(type) {
	case float64:
		apikey.Order = uint(v)
	}

	switch v := m["systems"].(type) {
	case []interface{}:
		if b, err := json.Marshal(v); err == nil {
			apikey.Systems = string(b)
		}
	case string:
		apikey.Systems = v
	}
}

func (apikey *Apikey) HasAccess(call *Call) bool {
	switch v := apikey.Systems.(type) {
	case []interface{}:
		for _, f := range v {
			switch v := f.(type) {
			case map[string]interface{}:
				switch id := v["id"].(type) {
				case float64:
					if id == float64(call.System) {
						switch tg := v["talkgroups"].(type) {
						case string:
							if tg == "*" {
								return true
							}
						case []interface{}:
							for _, f := range tg {
								switch tg := f.(type) {
								case float64:
									if tg == float64(call.Talkgroup) {
										return true
									}
								}
							}
						}
					}
				}
			}
		}

	case string:
		if v == "*" {
			return true
		}
	}

	return false
}

type Apikeys []Apikey

func (apikeys *Apikeys) FromMap(f []interface{}) {
	*apikeys = Apikeys{}

	for _, r := range f {
		switch m := r.(type) {
		case map[string]interface{}:
			apikey := Apikey{}
			apikey.FromMap(m)
			*apikeys = append(*apikeys, apikey)
		}
	}
}

func (apikeys *Apikeys) GetApikey(key string) (apikey *Apikey, ok bool) {
	for _, apikey := range *apikeys {
		if apikey.Key == key {
			return &apikey, true
		}
	}
	return nil, false
}

func (apikeys *Apikeys) Read(db *Database) error {
	var (
		err     error
		id      sql.NullFloat64
		order   sql.NullFloat64
		rows    *sql.Rows
		systems string
	)

	*apikeys = Apikeys{}

	formatError := func(err error) error {
		return fmt.Errorf("apikeys.read: %v", err)
	}

	if rows, err = db.Sql.Query("select `_id`, `disabled`, `ident`, `key`, `order`, `systems` from `rdioScannerApiKeys`"); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		apikey := Apikey{}
		if err = rows.Scan(&id, &apikey.Disabled, &apikey.Ident, &apikey.Key, &order, &systems); err != nil {
			break
		}

		if id.Valid && id.Float64 > 0 {
			apikey.Id = uint(id.Float64)
		}

		if len(apikey.Ident) == 0 {
			apikey.Ident = defaults.apikey.ident
		}

		if len(apikey.Key) == 0 {
			apikey.Key = uuid.New().String()
		}

		if order.Valid && order.Float64 > 0 {
			apikey.Order = uint(order.Float64)
		}

		if err = json.Unmarshal([]byte(systems), &apikey.Systems); err != nil {
			apikey.Systems = []interface{}{}
		}

		*apikeys = append(*apikeys, apikey)
	}

	rows.Close()

	if err != nil {
		return formatError(err)
	}

	return nil
}

func (apikeys *Apikeys) Write(db *Database) error {
	var (
		count   uint
		err     error
		rows    *sql.Rows
		rowIds  = []uint{}
		systems interface{}
	)

	formatError := func(err error) error {
		return fmt.Errorf("apikeys.write %v", err)
	}

	for _, apikey := range *apikeys {
		switch apikey.Systems {
		case "*":
			systems = `"*"`
		default:
			systems = apikey.Systems
		}

		if err = db.Sql.QueryRow("select count(*) from `rdioScannerApiKeys` where `_id` = ?", apikey.Id).Scan(&count); err != nil {
			break
		}

		if count == 0 {
			if _, err = db.Sql.Exec("insert into `rdioScannerApiKeys` (`_id`, `disabled`, `ident`, `key`, `order`, `systems`) values (?, ?, ?, ?, ?, ?)", apikey.Id, apikey.Disabled, apikey.Ident, apikey.Key, apikey.Order, systems); err != nil {
				break
			}

		} else if _, err = db.Sql.Exec("update `rdioScannerApiKeys` set `_id` = ?, `disabled` = ?, `ident` = ?, `key` = ?, `order` = ?, `systems` = ? where `_id` = ?", apikey.Id, apikey.Disabled, apikey.Ident, apikey.Key, apikey.Order, systems, apikey.Id); err != nil {
			break
		}
	}

	if err != nil {
		return formatError(err)
	}

	if rows, err = db.Sql.Query("select `_id` from `rdioScannerApiKeys`"); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		var id uint
		rows.Scan(&id)
		remove := true
		for _, apikey := range *apikeys {
			if apikey.Id == nil || apikey.Id == id {
				remove = false
				break
			}
		}
		if remove {
			rowIds = append(rowIds, id)
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
			q := fmt.Sprintf("delete from `rdioScannerApikeys` where `_id` in %v", s)
			if _, err = db.Sql.Exec(q); err != nil {
				return formatError(err)
			}
		}
	}

	return nil
}
