// Copyright (C) 2019-2026 Chrystian Huot <chrystian@huot.qc.ca>
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
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Apikey struct {
	Id       uint64
	Disabled bool
	Ident    string
	Key      string
	Order    uint
	Systems  any
}

func NewApikey() *Apikey {
	return &Apikey{Systems: "*"}
}

func (apikey *Apikey) FromMap(m map[string]any) *Apikey {
	switch v := m["id"].(type) {
	case float64:
		apikey.Id = uint64(v)
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

	apikey.Systems = m["systems"]

	return apikey
}

func (apikey *Apikey) HasAccess(call *Call) bool {
	switch v := apikey.Systems.(type) {
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

func (apikey *Apikey) MarshalJSON() ([]byte, error) {
	m := map[string]any{
		"id":       apikey.Id,
		"disabled": apikey.Disabled,
		"ident":    apikey.Ident,
		"key":      apikey.Key,
		"systems":  apikey.Systems,
	}

	if apikey.Order > 0 {
		m["order"] = apikey.Order
	}

	return json.Marshal(m)
}

type Apikeys struct {
	List  []*Apikey
	mutex sync.Mutex
}

func NewApikeys() *Apikeys {
	return &Apikeys{
		List:  []*Apikey{},
		mutex: sync.Mutex{},
	}
}

func (apikeys *Apikeys) FromMap(f []any) *Apikeys {
	apikeys.mutex.Lock()
	defer apikeys.mutex.Unlock()

	apikeys.List = []*Apikey{}

	for _, r := range f {
		switch m := r.(type) {
		case map[string]any:
			apikey := NewApikey().FromMap(m)
			apikeys.List = append(apikeys.List, apikey)
		}
	}

	return apikeys
}

func (apikeys *Apikeys) GetApikey(key string) (apikey *Apikey, ok bool) {
	apikeys.mutex.Lock()
	defer apikeys.mutex.Unlock()

	for _, apikey := range apikeys.List {
		if apikey.Key == key && !apikey.Disabled {
			return apikey, true
		}
	}
	return nil, false
}

func (apikeys *Apikeys) Read(db *Database) error {
	var (
		err   error
		query string
		rows  *sql.Rows
	)

	apikeys.mutex.Lock()
	defer apikeys.mutex.Unlock()

	apikeys.List = []*Apikey{}

	formatError := apikeys.errorFormatter("read")

	query = `SELECT "apikeyId", "disabled", "ident", "key", "order", "systems" FROM "apikeys"`
	if rows, err = db.Sql.Query(query); err != nil {
		return formatError(err, query)
	}

	for rows.Next() {
		var (
			apikey  = NewApikey()
			systems string
		)

		if err = rows.Scan(&apikey.Id, &apikey.Disabled, &apikey.Ident, &apikey.Key, &apikey.Order, &systems); err != nil {
			break
		}

		if len(systems) > 0 {
			json.Unmarshal([]byte(systems), &apikey.Systems)
		}

		apikeys.List = append(apikeys.List, apikey)
	}

	rows.Close()

	if err != nil {
		return formatError(err, "")
	}

	sort.Slice(apikeys.List, func(i int, j int) bool {
		return apikeys.List[i].Order < apikeys.List[j].Order
	})

	return nil
}

func (apikeys *Apikeys) Write(db *Database) error {
	var (
		apikeyIds = []uint64{}
		err       error
		query     string
		rows      *sql.Rows
		tx        *sql.Tx
	)

	apikeys.mutex.Lock()
	defer apikeys.mutex.Unlock()

	formatError := apikeys.errorFormatter("write")

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "apikeyId" FROM "apikeys"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		var apikeyId uint64
		if err = rows.Scan(&apikeyId); err != nil {
			break
		}
		remove := true
		for _, apikey := range apikeys.List {
			if apikey.Id == 0 || apikey.Id == apikeyId {
				remove = false
				break
			}
		}
		if remove {
			apikeyIds = append(apikeyIds, apikeyId)
		}
	}

	rows.Close()

	if err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	if len(apikeyIds) > 0 {
		if b, err := json.Marshal(apikeyIds); err == nil {
			in := strings.ReplaceAll(strings.ReplaceAll(string(b), "[", "("), "]", ")")
			query = fmt.Sprintf(`DELETE FROM "apikeys" WHERE "apikeyId" IN %s`, in)
			if _, err = tx.Exec(query); err != nil {
				tx.Rollback()
				return formatError(err, query)
			}
		}
	}

	for _, apikey := range apikeys.List {
		var (
			count   uint
			systems string
		)

		if apikey.Systems != nil {
			if b, err := json.Marshal(apikey.Systems); err == nil {
				systems = string(b)
			}
		}

		if apikey.Id > 0 {
			query = fmt.Sprintf(`SELECT COUNT(*) FROM "apikeys" WHERE "apikeyId" = %d`, apikey.Id)
			if err = tx.QueryRow(query).Scan(&count); err != nil {
				break
			}
		}

		if count == 0 {
			query = fmt.Sprintf(`INSERT INTO "apikeys" ("disabled", "ident", "key", "order", "systems") VALUES (%t, '%s', '%s', %d, '%s')`, apikey.Disabled, apikey.Ident, apikey.Key, apikey.Order, systems)
			if _, err = tx.Exec(query); err != nil {
				break
			}

		} else {
			query = fmt.Sprintf(`UPDATE "apikeys" SET "disabled" = %t, "ident" = '%s', "key" = '%s', "order" = %d, "systems" = '%s' WHERE "apikeyId" = %d`, apikey.Disabled, apikey.Ident, apikey.Key, apikey.Order, systems, apikey.Id)
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
		return formatError(err, query)
	}

	return nil
}

func (apikeys *Apikeys) errorFormatter(label string) func(err error, query string) error {
	return func(err error, query string) error {
		s := fmt.Sprintf("apikeys.%s: %s", label, err.Error())

		if len(query) > 0 {
			s = fmt.Sprintf("%s in %s", s, query)
		}

		return errors.New(s)
	}
}
