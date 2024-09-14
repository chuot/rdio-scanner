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
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

type Downstream struct {
	Id         uint64
	Apikey     string
	Disabled   bool
	Order      uint
	Systems    any
	Url        string
	controller *Controller
}

func NewDownstream(controller *Controller) *Downstream {
	return &Downstream{
		controller: controller,
	}
}

func (downstream *Downstream) FromMap(m map[string]any) *Downstream {
	switch v := m["id"].(type) {
	case float64:
		downstream.Id = uint64(v)
	}

	switch v := m["apikey"].(type) {
	case string:
		downstream.Apikey = v
	}

	switch v := m["disabled"].(type) {
	case bool:
		downstream.Disabled = v
	}

	switch v := m["order"].(type) {
	case float64:
		downstream.Order = uint(v)
	}

	downstream.Systems = m["systems"]

	switch v := m["url"].(type) {
	case string:
		downstream.Url = v
	}

	return downstream
}

func (downstream *Downstream) HasAccess(call *Call) bool {
	if downstream.Disabled {
		return false
	}

	switch v := downstream.Systems.(type) {
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

func (downstream *Downstream) MarshalJSON() ([]byte, error) {
	m := map[string]any{
		"id":       downstream.Id,
		"apikey":   downstream.Apikey,
		"disabled": downstream.Disabled,
		"systems":  downstream.Systems,
		"url":      downstream.Url,
	}

	if downstream.Order > 0 {
		m["order"] = downstream.Order
	}

	return json.Marshal(m)
}

func (downstream *Downstream) Send(call *Call) error {
	var buf = bytes.Buffer{}

	formatError := func(err error) error {
		return fmt.Errorf("downstream.send: %s", err.Error())
	}

	if downstream.controller == nil {
		return formatError(errors.New("no controller available"))
	}

	if downstream.Disabled {
		return nil
	}

	mw := multipart.NewWriter(&buf)

	if w, err := mw.CreateFormFile("audio", call.AudioFilename); err == nil {
		if _, err = w.Write(call.Audio); err != nil {
			return formatError(err)
		}
	} else {
		return formatError(err)
	}

	if w, err := mw.CreateFormField("audioFilename"); err == nil {
		if _, err = w.Write([]byte(call.AudioFilename)); err != nil {
			return formatError(err)
		}
	} else {
		return formatError(err)
	}

	if w, err := mw.CreateFormField("audioMime"); err == nil {
		if _, err = w.Write([]byte(call.AudioMime)); err != nil {
			return formatError(err)
		}
	} else {
		return formatError(err)
	}

	// pre v7 comptability
	if w, err := mw.CreateFormField("dateTime"); err == nil {
		if _, err = w.Write([]byte(call.Timestamp.Format(time.RFC3339))); err != nil {
			return formatError(err)
		}
	} else {
		return formatError(err)
	}

	if w, err := mw.CreateFormField("frequencies"); err == nil {
		if b, err := json.Marshal(call.Frequencies); err == nil {
			if _, err = w.Write(b); err != nil {
				return formatError(err)
			}
		} else {
			return formatError(err)
		}
	} else {
		return formatError(err)
	}

	if w, err := mw.CreateFormField("key"); err == nil {
		if _, err = w.Write([]byte(downstream.Apikey)); err != nil {
			return formatError(err)
		}
	} else {
		return formatError(err)
	}

	if w, err := mw.CreateFormField("patches"); err == nil {
		if b, err := json.Marshal(call.Patches); err == nil {
			if _, err = w.Write(b); err != nil {
				return formatError(err)
			}
		} else {
			return formatError(err)
		}
	} else {
		return formatError(err)
	}

	if w, err := mw.CreateFormField("system"); err == nil {
		if _, err = w.Write([]byte(fmt.Sprintf("%v", call.System.SystemRef))); err != nil {
			return formatError(err)
		}
	} else {
		return formatError(err)
	}

	if w, err := mw.CreateFormField("systemLabel"); err == nil {
		if _, err = w.Write([]byte(call.System.Label)); err != nil {
			return formatError(err)
		}
	} else {
		return formatError(err)
	}

	if w, err := mw.CreateFormField("talkgroup"); err == nil {
		if _, err = w.Write([]byte(fmt.Sprintf("%v", call.Talkgroup.TalkgroupRef))); err != nil {
			return formatError(err)
		}
	} else {
		return formatError(err)
	}

	if w, err := mw.CreateFormField("talkgroupGroups"); err == nil {
		var labels = []string{}
		for _, id := range call.Talkgroup.GroupIds {
			if group, ok := downstream.controller.Groups.GetGroupById(id); ok {
				labels = append(labels, group.Label)
			}
		}
		if _, err = w.Write([]byte(strings.Join(labels, ","))); err != nil {
			return formatError(err)
		}
	} else {
		return formatError(err)
	}

	if w, err := mw.CreateFormField("talkgroupLabel"); err == nil {
		if _, err = w.Write([]byte(call.Talkgroup.Label)); err != nil {
			return formatError(err)
		}
	} else {
		return formatError(err)
	}

	if w, err := mw.CreateFormField("talkgroupName"); err == nil {
		if _, err = w.Write([]byte(call.Talkgroup.Name)); err != nil {
			return formatError(err)
		}
	} else {
		return formatError(err)
	}

	if w, err := mw.CreateFormField("talkgroupTag"); err == nil {
		if tag, ok := downstream.controller.Tags.GetTagById(call.Talkgroup.TagId); ok {
			if _, err = w.Write([]byte(tag.Label)); err != nil {
				return formatError(err)
			}
		}
	} else {
		return formatError(err)
	}

	if w, err := mw.CreateFormField("timestamp"); err == nil {
		if _, err = w.Write([]byte(fmt.Sprintf("%d", call.Timestamp.UnixMilli()))); err != nil {
			return formatError(err)
		}
	} else {
		return formatError(err)
	}

	if w, err := mw.CreateFormField("units"); err == nil {
		if b, err := json.Marshal(call.Units); err == nil {
			if _, err = w.Write(b); err != nil {
				return formatError(err)
			}
		} else {
			return formatError(err)
		}
	} else {
		return formatError(err)
	}

	if err := mw.Close(); err != nil {
		return formatError(err)
	}

	if u, err := url.Parse(downstream.Url); err == nil {
		u.Path = path.Join(u.Path, "/api/call-upload")

		c := http.Client{Timeout: 30 * time.Second}

		if res, err := c.Post(u.String(), mw.FormDataContentType(), &buf); err == nil {
			if res.StatusCode != http.StatusOK {
				return formatError(fmt.Errorf("bad status: %s", res.Status))
			}

		} else {
			return formatError(err)
		}

	} else {
		return formatError(err)
	}

	return nil
}

type Downstreams struct {
	List       []*Downstream
	controller *Controller
	mutex      sync.Mutex
}

func NewDownstreams(controller *Controller) *Downstreams {
	return &Downstreams{
		List:       []*Downstream{},
		controller: controller,
		mutex:      sync.Mutex{},
	}
}

func (downstreams *Downstreams) FromMap(f []any) *Downstreams {
	downstreams.mutex.Lock()
	defer downstreams.mutex.Unlock()

	downstreams.List = []*Downstream{}

	for _, r := range f {
		switch m := r.(type) {
		case map[string]any:
			downstream := NewDownstream(downstreams.controller).FromMap(m)
			downstreams.List = append(downstreams.List, downstream)
		}
	}

	return downstreams
}

func (downstreams *Downstreams) Read(db *Database) error {
	var (
		err   error
		query string
		rows  *sql.Rows
	)

	downstreams.mutex.Lock()
	defer downstreams.mutex.Unlock()

	downstreams.List = []*Downstream{}

	formatError := downstreams.errorFormatter("read")

	query = `SELECT "downstreamId", "apikey", "disabled", "order", "systems", "url" FROM "downstreams"`
	if rows, err = db.Sql.Query(query); err != nil {
		return formatError(err, query)
	}

	for rows.Next() {
		var (
			downstream = NewDownstream(downstreams.controller)
			systems    string
		)

		if err = rows.Scan(&downstream.Id, &downstream.Apikey, &downstream.Disabled, &downstream.Order, &systems, &downstream.Url); err != nil {
			break
		}

		if len(systems) > 0 {
			json.Unmarshal([]byte(systems), &downstream.Systems)
		}

		downstreams.List = append(downstreams.List, downstream)
	}

	rows.Close()

	if err != nil {
		return formatError(err, "")
	}

	sort.Slice(downstreams.List, func(i int, j int) bool {
		return downstreams.List[i].Order < downstreams.List[j].Order
	})

	return nil
}

func (downstreams *Downstreams) Send(controller *Controller, call *Call) {
	for _, downstream := range downstreams.List {
		logEvent := func(logLevel string, message string) {
			controller.Logs.LogEvent(logLevel, fmt.Sprintf("downstream: system=%d talkgroup=%d file=%s to %s %s", call.System.SystemRef, call.Talkgroup.TalkgroupRef, call.AudioFilename, downstream.Url, message))
		}

		if downstream.HasAccess(call) {
			if err := downstream.Send(call); err == nil {
				logEvent(LogLevelInfo, "success")
			} else {
				logEvent(LogLevelError, err.Error())
			}
		}
	}
}

func (downstreams *Downstreams) Write(db *Database) error {
	var (
		downstreamIds = []uint64{}
		err           error
		query         string
		rows          *sql.Rows
		tx            *sql.Tx
	)

	downstreams.mutex.Lock()
	defer downstreams.mutex.Unlock()

	formatError := downstreams.errorFormatter("write")

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "downstreamId" FROM "downstreams"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		var downstreamId uint64
		if err = rows.Scan(&downstreamId); err != nil {
			break
		}
		remove := true
		for _, downstream := range downstreams.List {
			if downstream.Id == 0 || downstream.Id == downstreamId {
				remove = false
				break
			}
		}
		if remove {
			downstreamIds = append(downstreamIds, downstreamId)
		}
	}

	rows.Close()

	if err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	if len(downstreamIds) > 0 {
		if b, err := json.Marshal(downstreamIds); err == nil {
			in := strings.ReplaceAll(strings.ReplaceAll(string(b), "[", "("), "]", ")")
			query = fmt.Sprintf(`DELETE FROM "downstreams" WHERE "downstreamId" IN %s`, in)
			if _, err = tx.Exec(query); err != nil {
				tx.Rollback()
				return formatError(err, query)
			}
		}
	}

	for _, downstream := range downstreams.List {
		var (
			count   uint
			systems string
		)

		if downstream.Systems != nil {
			if b, err := json.Marshal(downstream.Systems); err == nil {
				systems = string(b)
			}
		}

		if downstream.Id > 0 {
			query = fmt.Sprintf(`SELECT COUNT(*) FROM "downstreams" WHERE "downstreamId" = %d`, downstream.Id)
			if err = tx.QueryRow(query).Scan(&count); err != nil {
				break
			}
		}

		if count == 0 {
			query = fmt.Sprintf(`INSERT INTO "downstreams" ("apikey", "disabled", "order", "systems", "url") VALUES ('%s', %t, %d, '%s', '%s')`, escapeQuotes(downstream.Apikey), downstream.Disabled, downstream.Order, systems, escapeQuotes(downstream.Url))
			if _, err = tx.Exec(query); err != nil {
				break
			}

		} else {
			query = fmt.Sprintf(`UPDATE "downstreams" SET "apikey" = '%s', "disabled" = %t, "order" = %d, "systems" = '%s', "url" = '%s' WHERE "downstreamId" = %d`, escapeQuotes(downstream.Apikey), downstream.Disabled, downstream.Order, systems, escapeQuotes(downstream.Url), downstream.Id)
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

func (downstreams *Downstreams) errorFormatter(label string) func(err error, query string) error {
	return func(err error, query string) error {
		s := fmt.Sprintf("downstreams.%s: %s", label, err.Error())

		if len(query) > 0 {
			s = fmt.Sprintf("%s in %s", s, query)
		}

		return errors.New(s)
	}
}
