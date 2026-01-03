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
//
// WebSocket API Access Policy:
// This WebSocket API is reserved exclusively for Saubeo Solutions and its native applications.
// Unauthorized access is strictly prohibited.
// See API_ACCESS_POLICY.md for full terms.

package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Access struct {
	Id         any    `json:"_id"`
	Code       string `json:"code"`
	Expiration any    `json:"expiration"`
	Ident      string `json:"ident"`
	Limit      any    `json:"limit"`
	Order      any    `json:"order"`
	Systems    any    `json:"systems"`
}

func NewAccess() *Access {
	return &Access{Systems: "*"}
}

func (access *Access) FromMap(m map[string]any) *Access {
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
	case []any:
		if b, err := json.Marshal(v); err == nil {
			access.Systems = string(b)
		}
	case string:
		access.Systems = v
	}

	return access
}

func (access *Access) HasAccess(call *Call) bool {
	if access.Systems != nil {
		switch v := access.Systems.(type) {
		case []any:
			for _, f := range v {
				switch v := f.(type) {
				case map[string]any:
					switch id := v["id"].(type) {
					case float64:
						if id == float64(call.System) {
							switch tg := v["talkgroups"].(type) {
							case string:
								if tg == "*" {
									return true
								}
							case []any:
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

type Accesses struct {
	List  []*Access
	mutex sync.Mutex
}

func NewAccesses() *Accesses {
	return &Accesses{
		List:  []*Access{},
		mutex: sync.Mutex{},
	}
}

func (accesses *Accesses) Add(access *Access) (*Accesses, bool) {
	accesses.mutex.Lock()
	defer accesses.mutex.Unlock()

	added := true

	for _, a := range accesses.List {
		if a.Code == access.Code {
			a.Expiration = access.Expiration
			a.Ident = access.Ident
			a.Limit = access.Limit
			a.Systems = access.Systems
			added = false
		}
	}

	if added {
		accesses.List = append(accesses.List, access)
	}

	return accesses, added
}

func (accesses *Accesses) FromMap(f []any) *Accesses {
	accesses.mutex.Lock()
	defer accesses.mutex.Unlock()

	accesses.List = []*Access{}

	for _, r := range f {
		switch m := r.(type) {
		case map[string]any:
			access := &Access{}
			access.FromMap(m)
			accesses.List = append(accesses.List, access)
		}
	}

	return accesses
}

func (accesses *Accesses) GetAccess(code string) (access *Access, ok bool) {
	accesses.mutex.Lock()
	defer accesses.mutex.Unlock()

	for _, access := range accesses.List {
		if access.Code == code {
			return access, true
		}
	}

	return nil, false
}

func (accesses *Accesses) IsRestricted() bool {
	accesses.mutex.Lock()
	defer accesses.mutex.Unlock()

	return len(accesses.List) > 0
}

func (accesses *Accesses) Read(db *Database) error {
	var (
		err        error
		expiration any
		id         sql.NullFloat64
		limit      sql.NullFloat64
		order      sql.NullFloat64
		rows       *sql.Rows
		systems    string
		t          time.Time
	)

	accesses.mutex.Lock()
	defer accesses.mutex.Unlock()

	accesses.List = []*Access{}

	formatError := func(err error) error {
		return fmt.Errorf("accesses.read: %v", err)
	}

	if rows, err = db.Sql.Query("select `_id`, `code`, `expiration`, `ident`, `limit`, `order`, `systems` from `rdioScannerAccesses`"); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		access := &Access{}

		if err = rows.Scan(&id, &access.Code, &expiration, &access.Ident, &limit, &order, &systems); err != nil {
			break
		}

		if id.Valid && id.Float64 > 0 {
			access.Id = uint(id.Float64)
		}

		if len(access.Code) == 0 {
			continue
		}

		if t, err = db.ParseDateTime(expiration); err == nil {
			access.Expiration = t
		}

		if len(access.Ident) == 0 {
			access.Ident = defaults.access.ident
		}

		if limit.Valid && limit.Float64 > 0 {
			access.Limit = uint(limit.Float64)
		}

		if order.Valid && order.Float64 > 0 {
			access.Order = uint(order.Float64)
		}

		if err = json.Unmarshal([]byte(systems), &access.Systems); err != nil {
			access.Systems = []any{}
		}

		accesses.List = append(accesses.List, access)
	}

	rows.Close()

	if err != nil {
		return formatError(err)
	}

	return nil
}

func (accesses *Accesses) Remove(access *Access) (*Accesses, bool) {
	accesses.mutex.Lock()
	defer accesses.mutex.Unlock()

	removed := false

	for i, a := range accesses.List {
		if a.Ident == access.Ident {
			accesses.List = append(accesses.List[:i], accesses.List[i+1:]...)
			removed = true
		}
	}

	return accesses, removed
}

func (accesses *Accesses) Write(db *Database) error {
	var (
		count   uint
		err     error
		rows    *sql.Rows
		rowIds  = []uint{}
		systems any
	)

	accesses.mutex.Lock()
	defer accesses.mutex.Unlock()

	formatError := func(err error) error {
		return fmt.Errorf("accesses.write: %v", err)
	}

	if rows, err = db.Sql.Query("select `_id` from `rdioScannerAccesses`"); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		var id uint
		if err = rows.Scan(&id); err != nil {
			break
		}
		remove := true
		for _, access := range accesses.List {
			if access.Id == nil || access.Id == id {
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
			q := fmt.Sprintf("delete from `rdioScannerAccesses` where `_id` in %v", s)
			if _, err = db.Sql.Exec(q); err != nil {
				return formatError(err)
			}
		}
	}

	for _, access := range accesses.List {
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

	return nil
}
