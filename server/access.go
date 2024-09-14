// Copyright (C) 2019-2024 Chrystian Huot <chrystian@huot.qc.ca>
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
	"time"
)

type Access struct {
	Id         uint64
	Code       string
	Expiration uint64
	Ident      string
	Limit      uint
	Order      uint
	Systems    any
}

func NewAccess() *Access {
	return &Access{}
}

func (access *Access) FromMap(m map[string]any) *Access {
	switch v := m["id"].(type) {
	case float64:
		access.Id = uint64(v)
	}

	switch v := m["code"].(type) {
	case string:
		access.Code = v
	}

	switch v := m["expiration"].(type) {
	case string:
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			access.Expiration = uint64(t.Unix())
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

	access.Systems = m["systems"]

	return access
}

func (access *Access) HasAccess(call *Call) bool {
	switch v := access.Systems.(type) {
	case []any:
		for _, f := range v {
			switch v := f.(type) {
			case map[string]any:
				switch id := v["id"].(type) {
				case float64:
					if id == float64(call.System.SystemRef) {
						switch tg := v["talkgroups"].(type) {
						case string:
							if tg == "*" {
								return true
							}
						case []any:
							for _, f := range tg {
								switch tg := f.(type) {
								case float64:
									if tg == float64(call.Talkgroup.TalkgroupRef) {
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

func (access *Access) HasExpired() bool {
	if access.Expiration > 0 {
		return time.Unix(int64(access.Expiration), 0).Before(time.Now())
	}
	return false
}

func (access *Access) MarshalJSON() ([]byte, error) {
	m := map[string]any{
		"id":      access.Id,
		"code":    access.Code,
		"ident":   access.Ident,
		"systems": access.Systems,
	}

	if access.Expiration > 0 {
		m["expiration"] = time.Unix(int64(access.Expiration), 0)
	}

	if access.Limit > 0 {
		m["limit"] = access.Limit
	}

	if access.Order > 0 {
		m["order"] = access.Order
	}

	return json.Marshal(m)
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
			access := NewAccess().FromMap(m)
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
		err   error
		query string
		rows  *sql.Rows
	)

	accesses.mutex.Lock()
	defer accesses.mutex.Unlock()

	accesses.List = []*Access{}

	formatError := errorFormatter("accesses", "read")

	query = `SELECT "accessId", "code", "expiration", "ident", "limit", "order", "systems" FROM "accesses"`
	if rows, err = db.Sql.Query(query); err != nil {
		return formatError(err, query)
	}

	for rows.Next() {
		var (
			access  = NewAccess()
			systems string
		)

		if err = rows.Scan(&access.Id, &access.Code, &access.Expiration, &access.Ident, &access.Limit, &access.Order, &systems); err != nil {
			break
		}

		if len(systems) > 0 {
			json.Unmarshal([]byte(systems), &access.Systems)
		}

		accesses.List = append(accesses.List, access)
	}

	rows.Close()

	if err != nil {
		return formatError(err, "")
	}

	sort.Slice(accesses.List, func(i int, j int) bool {
		return accesses.List[i].Order < accesses.List[j].Order
	})

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
		accessIds = []uint64{}
		err       error
		query     string
		rows      *sql.Rows
		tx        *sql.Tx
	)

	accesses.mutex.Lock()
	defer accesses.mutex.Unlock()

	formatError := errorFormatter("accesses", "write")

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "accessId" FROM "accesses"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		var accessId uint64
		if err = rows.Scan(&accessId); err != nil {
			break
		}
		remove := true
		for _, access := range accesses.List {
			if access.Id == 0 || access.Id == accessId {
				remove = false
				break
			}
		}
		if remove {
			accessIds = append(accessIds, accessId)
		}
	}

	rows.Close()

	if err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	if len(accessIds) > 0 {
		if b, err := json.Marshal(accessIds); err == nil {
			in := strings.ReplaceAll(strings.ReplaceAll(string(b), "[", "("), "]", ")")
			query = fmt.Sprintf(`DELETE FROM "accesses" WHERE "accessId" IN %s`, in)
			if _, err = tx.Exec(query); err != nil {
				tx.Rollback()
				return formatError(err, query)
			}
		}
	}

	for _, access := range accesses.List {
		var (
			count   uint
			systems string
		)

		if b, err := json.Marshal(access.Systems); err == nil {
			systems = string(b)
		}

		if access.Id > 0 {
			query = fmt.Sprintf(`SELECT COUNT(*) FROM "accesses" WHERE "accessId" = %d`, access.Id)
			if err = tx.QueryRow(query).Scan(&count); err != nil {
				break
			}
		}

		if count == 0 {
			query = fmt.Sprintf(`INSERT INTO "accesses" ("code", "expiration", "ident", "limit", "order", "systems") VALUES ('%s', %d, '%s', %d, %d, '%s')`, escapeQuotes(access.Code), access.Expiration, escapeQuotes(access.Ident), access.Limit, access.Order, systems)
			if _, err = tx.Exec(query); err != nil {
				break
			}

		} else {
			query = fmt.Sprintf(`UPDATE "accesses" SET "code" = '%s', "expiration" = %d, "ident" = '%s', "limit" = %d, "order" = %d, "systems" = '%s' WHERE "accessId" = %d`, escapeQuotes(access.Code), access.Expiration, escapeQuotes(access.Ident), access.Limit, access.Order, systems, access.Id)
			if _, err = tx.Exec(query); err != nil {
				break
			}
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
