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
	"encoding/json"
	"fmt"
	"time"
)

type Access struct {
	Id         interface{} `json:"_id"`
	Code       string      `json:"code"`
	Expiration interface{} `json:"expiration"`
	Ident      string      `json:"ident"`
	Limit      interface{} `json:"limit"`
	Order      interface{} `json:"order"`
	Systems    interface{} `json:"systems"`
}

func (access *Access) FromMap(m map[string]interface{}) {
	switch v := m["_id"].(type) {
	case float64:
		access.Id = uint(v)
	}

	switch v := m["code"].(type) {
	case string:
		access.Code = v
	}

	switch v := m["expiration"].(type) {
	case string:
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			access.Expiration = t.UTC()
		}
	}

	switch v := m["ident"].(type) {
	case string:
		access.Ident = v
	}

	switch v := m["limit"].(type) {
	case float64:
		access.Limit = uint(v)
	}

	switch v := m["order"].(type) {
	case float64:
		access.Order = uint(v)
	}

	switch v := m["systems"].(type) {
	case []interface{}:
		if b, err := json.Marshal(v); err == nil {
			access.Systems = string(b)
		}
	case string:
		access.Systems = v
	}
}

func (access *Access) HasAccess(call *Call) bool {
	if access.Systems != nil {
		switch v := access.Systems.(type) {
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
	}

	return false
}

func (access *Access) HasExpired() bool {
	switch v := access.Expiration.(type) {
	case time.Time:
		return v.Before(time.Now())
	}
	return false
}

type Accesses []Access

func (accesses *Accesses) FromMap(f []interface{}) {
	*accesses = Accesses{}

	for _, r := range f {
		switch m := r.(type) {
		case map[string]interface{}:
			access := Access{}
			access.FromMap(m)
			*accesses = append(*accesses, access)
		}
	}
}

func (accesses *Accesses) GetAccess(code string) (access *Access, ok bool) {
	for _, access := range *accesses {
		if access.Code == code {
			return &access, true
		}
	}
	return nil, false
}

func (accesses *Accesses) IsRestricted() bool {
	return len(*accesses) > 0
}

func (accesses *Accesses) Read(db *Database) error {
	var (
		err        error
		expiration interface{}
		id         uint
		rows       *sql.Rows
		systems    string
		t          time.Time
	)

	*accesses = Accesses{}

	formatError := func(err error) error {
		return fmt.Errorf("accesses.read: %v", err)
	}

	if rows, err = db.Sql.Query("select `_id`, `code`, `expiration`, `ident`, `limit`, `order`, `systems` from `rdioScannerAccesses`"); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		access := Access{}
		if err = rows.Scan(&id, &access.Code, &expiration, &access.Ident, &access.Limit, &access.Order, &systems); err != nil {
			break
		}

		access.Id = id

		if len(access.Code) == 0 {
			continue
		}

		if expiration != nil {
			if t, err = db.ParseDateTime(expiration); err == nil {
				access.Expiration = t
			} else {
				continue
			}
		}

		if len(access.Ident) == 0 {
			access.Ident = defaults.access.ident
		}

		if err = json.Unmarshal([]byte(systems), &access.Systems); err != nil {
			access.Systems = []interface{}{}
		}

		*accesses = append(*accesses, access)
	}

	rows.Close()

	if err != nil {
		return formatError(err)
	}

	return nil
}

func (accesses *Accesses) Write(db *Database) error {
	var (
		count   uint
		err     error
		rows    *sql.Rows
		systems interface{}
	)

	formatError := func(err error) error {
		return fmt.Errorf("accesses.write: %v", err)
	}

	for _, access := range *accesses {
		switch access.Systems {
		case "*":
			systems = `"*"`
		default:
			systems = access.Systems
		}

		if err = db.Sql.QueryRow("select count(*) from `rdioScannerAccesses` where `_id` = ?", access.Id).Scan(&count); err != nil {
			break
		}

		if count == 0 {
			if _, err = db.Sql.Exec("insert into `rdioScannerAccesses` (`_id`, `code`, `expiration`, `ident`, `limit`, `order`, `systems`) values (?, ?, ?, ?, ?, ?, ?)", access.Id, access.Code, access.Expiration, access.Ident, access.Limit, access.Order, systems); err != nil {
				break
			}

		} else if _, err = db.Sql.Exec("update `rdioScannerAccesses` set `_id` = ?, `code` = ?, `expiration` = ?, `ident` = ?, `limit` = ?, `order` = ?, `systems` = ? where `_id` = ?", access.Id, access.Code, access.Expiration, access.Ident, access.Limit, access.Order, systems, access.Id); err != nil {
			break
		}
	}

	if err != nil {
		return formatError(err)
	}

	if rows, err = db.Sql.Query("select `_id` from `rdioScannerAccesses`"); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		var id uint
		rows.Scan(&id)
		remove := true
		for _, access := range *accesses {
			if access.Id == nil || access.Id == id {
				remove = false
				break
			}
		}
		if remove {
			_, err = db.Sql.Exec("delete from `rdioScannerAccesses` where `_id` = ?", id)
		}
	}

	rows.Close()

	if err != nil {
		return formatError(err)
	}

	return nil
}
