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

package main

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// Recorder is one admin-managed companion daemon (e.g. uniden_recorder.py).
// It exists primarily so the admin UI can edit the "soft" settings of a
// recorder without SSH-ing into the box that runs it. Hardware-local
// settings (serial port, audio device) stay in the recorder's .ini because
// the server cannot know what /dev nodes exist on the recording host.
type Recorder struct {
	Id           any    `json:"_id"`
	ApiKey       string `json:"apiKey"`
	Disabled     bool   `json:"disabled"`
	Label        string `json:"label"`
	Order        any    `json:"order"`
	SystemId     any    `json:"systemId"`
	OutputDir    any    `json:"outputDir"`
	MinSilenceMs any    `json:"minSilenceMs"`
	PreRollMs    any    `json:"preRollMs"`
}

func NewRecorder() *Recorder {
	return &Recorder{}
}

func (recorder *Recorder) FromMap(m map[string]any) *Recorder {
	switch v := m["_id"].(type) {
	case float64:
		recorder.Id = uint(v)
	}

	switch v := m["apiKey"].(type) {
	case string:
		recorder.ApiKey = v
	}

	switch v := m["disabled"].(type) {
	case bool:
		recorder.Disabled = v
	}

	switch v := m["label"].(type) {
	case string:
		recorder.Label = v
	}

	switch v := m["order"].(type) {
	case float64:
		recorder.Order = uint(v)
	}

	switch v := m["systemId"].(type) {
	case float64:
		recorder.SystemId = uint(v)
	}

	switch v := m["outputDir"].(type) {
	case string:
		if len(v) > 0 {
			recorder.OutputDir = v
		}
	}

	switch v := m["minSilenceMs"].(type) {
	case float64:
		recorder.MinSilenceMs = uint(v)
	}

	switch v := m["preRollMs"].(type) {
	case float64:
		recorder.PreRollMs = uint(v)
	}

	return recorder
}

// configPayload returns the subset of fields the recorder daemon needs at
// runtime. It deliberately omits the apiKey (the caller already knows it)
// and any UI-only metadata.
func (recorder *Recorder) configPayload() map[string]any {
	return map[string]any{
		"disabled":     recorder.Disabled,
		"label":        recorder.Label,
		"systemId":     recorder.SystemId,
		"outputDir":    recorder.OutputDir,
		"minSilenceMs": recorder.MinSilenceMs,
		"preRollMs":    recorder.PreRollMs,
	}
}

type Recorders struct {
	List  []*Recorder
	mutex sync.Mutex
}

func NewRecorders() *Recorders {
	return &Recorders{
		List:  []*Recorder{},
		mutex: sync.Mutex{},
	}
}

func (recorders *Recorders) FromMap(f []any) *Recorders {
	recorders.mutex.Lock()
	defer recorders.mutex.Unlock()

	recorders.List = []*Recorder{}

	for _, f := range f {
		switch v := f.(type) {
		case map[string]any:
			recorder := NewRecorder().FromMap(v)
			recorders.List = append(recorders.List, recorder)
		}
	}

	return recorders
}

// GetByApiKey returns the matching recorder, comparing the supplied key in
// constant time so an attacker can't time-leak which prefixes are valid.
func (recorders *Recorders) GetByApiKey(key string) *Recorder {
	if key == "" {
		return nil
	}
	recorders.mutex.Lock()
	defer recorders.mutex.Unlock()
	for _, r := range recorders.List {
		if subtle.ConstantTimeCompare([]byte(r.ApiKey), []byte(key)) == 1 {
			return r
		}
	}
	return nil
}

func (recorders *Recorders) Read(db *Database) error {
	var (
		err          error
		id           sql.NullFloat64
		label        sql.NullString
		minSilenceMs sql.NullFloat64
		order        sql.NullFloat64
		outputDir    sql.NullString
		preRollMs    sql.NullFloat64
		rows         *sql.Rows
		systemId     sql.NullFloat64
	)

	recorders.mutex.Lock()
	defer recorders.mutex.Unlock()

	recorders.List = []*Recorder{}

	formatError := func(err error) error {
		return fmt.Errorf("recorders.read: %v", err)
	}

	if rows, err = db.Sql.Query("select `_id`, `apiKey`, `disabled`, `label`, `order`, `systemId`, `outputDir`, `minSilenceMs`, `preRollMs` from `rdioScannerRecorders`"); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		recorder := NewRecorder()

		if err = rows.Scan(&id, &recorder.ApiKey, &recorder.Disabled, &label, &order, &systemId, &outputDir, &minSilenceMs, &preRollMs); err != nil {
			break
		}

		if id.Valid && id.Float64 > 0 {
			recorder.Id = uint(id.Float64)
		}

		if label.Valid {
			recorder.Label = label.String
		}

		if order.Valid && order.Float64 > 0 {
			recorder.Order = uint(order.Float64)
		}

		if systemId.Valid && systemId.Float64 > 0 {
			recorder.SystemId = uint(systemId.Float64)
		}

		if outputDir.Valid && len(outputDir.String) > 0 {
			recorder.OutputDir = outputDir.String
		}

		if minSilenceMs.Valid && minSilenceMs.Float64 > 0 {
			recorder.MinSilenceMs = uint(minSilenceMs.Float64)
		}

		if preRollMs.Valid && preRollMs.Float64 > 0 {
			recorder.PreRollMs = uint(preRollMs.Float64)
		}

		recorders.List = append(recorders.List, recorder)
	}

	rows.Close()

	if err != nil {
		return formatError(err)
	}

	return nil
}

func (recorders *Recorders) Write(db *Database) error {
	var (
		count  uint
		err    error
		rows   *sql.Rows
		rowIds = []uint{}
	)

	recorders.mutex.Lock()
	defer recorders.mutex.Unlock()

	formatError := func(err error) error {
		return fmt.Errorf("recorders.write: %v", err)
	}

	if rows, err = db.Sql.Query("select `_id` from `rdioScannerRecorders`"); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		var rowId uint
		if err = rows.Scan(&rowId); err != nil {
			break
		}
		remove := true
		for _, recorder := range recorders.List {
			if recorder.Id == nil || recorder.Id == rowId {
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
			q := fmt.Sprintf("delete from `rdioScannerRecorders` where `_id` in %v", s)
			if _, err = db.Sql.Exec(q); err != nil {
				return formatError(err)
			}
		}
	}

	for _, recorder := range recorders.List {
		if err = db.Sql.QueryRow("select count(*) from `rdioScannerRecorders` where `_id` = ?", recorder.Id).Scan(&count); err != nil {
			break
		}

		if count == 0 {
			if _, err = db.Sql.Exec(
				"insert into `rdioScannerRecorders` (`_id`, `apiKey`, `disabled`, `label`, `order`, `systemId`, `outputDir`, `minSilenceMs`, `preRollMs`) values (?, ?, ?, ?, ?, ?, ?, ?, ?)",
				recorder.Id, recorder.ApiKey, recorder.Disabled, recorder.Label, recorder.Order, recorder.SystemId, recorder.OutputDir, recorder.MinSilenceMs, recorder.PreRollMs,
			); err != nil {
				break
			}
		} else if _, err = db.Sql.Exec(
			"update `rdioScannerRecorders` set `_id` = ?, `apiKey` = ?, `disabled` = ?, `label` = ?, `order` = ?, `systemId` = ?, `outputDir` = ?, `minSilenceMs` = ?, `preRollMs` = ? where `_id` = ?",
			recorder.Id, recorder.ApiKey, recorder.Disabled, recorder.Label, recorder.Order, recorder.SystemId, recorder.OutputDir, recorder.MinSilenceMs, recorder.PreRollMs, recorder.Id,
		); err != nil {
			break
		}
	}

	if err != nil {
		return formatError(err)
	}

	return nil
}

// RecorderConfigHandler is the endpoint companion recorders poll to learn
// their current configuration. The recorder authenticates by sending its
// apiKey as `Authorization: Bearer <key>` (or in the legacy `apiKey` query
// param). On success the response is a JSON object with the "soft"
// settings; the recorder applies what it can apply at runtime.
func (admin *Admin) RecorderConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	key := ""
	if h := r.Header.Get("Authorization"); h != "" {
		// Tolerate "Bearer xxx" and bare "xxx".
		if strings.HasPrefix(strings.ToLower(h), "bearer ") {
			key = strings.TrimSpace(h[7:])
		} else {
			key = strings.TrimSpace(h)
		}
	}
	if key == "" {
		key = r.URL.Query().Get("apiKey")
	}

	rec := admin.Controller.Recorders.GetByApiKey(key)
	if rec == nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	payload := rec.configPayload()
	b, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}
