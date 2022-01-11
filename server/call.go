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
	"time"
)

type Call struct {
	Id             interface{} `json:"id"`
	Audio          []byte      `json:"audio"`
	AudioName      interface{} `json:"audioName"`
	AudioType      interface{} `json:"audioType"`
	DateTime       time.Time   `json:"dateTime"`
	Frequencies    interface{} `json:"frequencies"`
	Frequency      interface{} `json:"frequency"`
	Patches        interface{} `json:"patches"`
	Source         interface{} `json:"source"`
	Sources        interface{} `json:"sources"`
	System         uint        `json:"system"`
	Talkgroup      uint        `json:"talkgroup"`
	systemLabel    interface{}
	talkgroupGroup interface{}
	talkgroupLabel interface{}
	talkgroupTag   interface{}
	units          interface{}
}

func NewCall() *Call {
	return &Call{
		Frequencies: []map[string]interface{}{},
		Patches:     []uint{},
		Sources:     []map[string]interface{}{},
	}
}

func (call *Call) IsValid() bool {
	if len(call.Audio) <= 44 {
		return false
	}

	if call.DateTime.Unix() == 0 {
		return false
	}

	if call.System < 1 {
		return false
	}

	if call.Talkgroup < 1 {
		return false
	}

	return true
}

func (call *Call) MarshalJSON() ([]byte, error) {
	audio := fmt.Sprintf("%v", call.Audio)
	audio = strings.ReplaceAll(audio, " ", ",")

	return json.Marshal(map[string]interface{}{
		"id": call.Id,
		"audio": map[string]interface{}{
			"data": json.RawMessage(audio),
			"type": "Buffer",
		},
		"audioName":   call.AudioName,
		"audioType":   call.AudioType,
		"dateTime":    call.DateTime.Format(time.RFC3339),
		"frequencies": call.Frequencies,
		"frequency":   call.Frequency,
		"patches":     call.Patches,
		"source":      call.Source,
		"sources":     call.Sources,
		"system":      call.System,
		"talkgroup":   call.Talkgroup,
	})
}

func (call *Call) ToJson() (string, error) {
	if b, err := json.Marshal(call); err == nil {
		return string(b), nil
	} else {
		return "", fmt.Errorf("call.tojson: %v", err)
	}
}

func (call *Call) Write(db *Database) (uint, error) {
	var (
		b           []byte
		err         error
		frequencies string
		id          int64
		patches     string
		res         sql.Result
		sources     string
	)

	formatError := func(err error) error {
		return fmt.Errorf("call.write: %s", err.Error())
	}

	switch v := call.Frequencies.(type) {
	case []map[string]interface{}:
		if b, err = json.Marshal(v); err == nil {
			frequencies = string(b)
		} else {
			return 0, formatError(err)
		}
	}

	switch v := call.Patches.(type) {
	case []uint:
		if b, err = json.Marshal(v); err == nil {
			patches = string(b)
		} else {
			return 0, formatError(err)
		}
	}

	switch v := call.Sources.(type) {
	case []map[string]interface{}:
		if b, err = json.Marshal(v); err == nil {
			sources = string(b)
		} else {
			return 0, formatError(err)
		}
	}

	if res, err = db.Sql.Exec("insert into `rdioScannerCalls` (`id`, `audio`, `audioName`, `audioType`, `dateTime`, `frequencies`, `frequency`, `patches`, `source`, `sources`, `system`, `talkgroup`) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", call.Id, call.Audio, call.AudioName, call.AudioType, call.DateTime, frequencies, call.Frequency, patches, call.Source, sources, call.System, call.Talkgroup); err != nil {
		return 0, formatError(err)
	}

	if id, err = res.LastInsertId(); err == nil {
		return uint(id), nil
	} else {
		return 0, formatError(err)
	}
}

type Calls []Call

func GetCall(id uint, db *Database) (*Call, error) {
	var (
		dateTime    interface{}
		frequencies string
		patches     string
		sources     string
		t           time.Time
	)

	call := Call{}

	query := fmt.Sprintf("select `id`, `audio`, `audioName`, `audioType`, `DateTime`, `frequencies`, `frequency`, `patches`, `source`, `sources`, `system`, `talkgroup` from `rdioScannerCalls` where `id` = %v", id)
	err := db.Sql.QueryRow(query).Scan(&call.Id, &call.Audio, &call.AudioName, &call.AudioType, &dateTime, &frequencies, &call.Frequency, &patches, &call.Source, &sources, &call.System, &call.Talkgroup)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("getcall: %v, %v", err, query)
	}

	if t, err = db.ParseDateTime(dateTime); err == nil {
		call.DateTime = t
	} else {
		return nil, fmt.Errorf("getcall.parsedatetime: %v", err)
	}

	if len(frequencies) > 0 {
		if err = json.Unmarshal([]byte(frequencies), &call.Frequencies); err != nil {
			return nil, fmt.Errorf("getcall.unmarshal.frequencies: %v", err)
		}
	}

	if len(patches) > 0 {
		if err = json.Unmarshal([]byte(patches), &call.Patches); err != nil {
			return nil, fmt.Errorf("getcall.unmarshal.patches: %v", err)
		}
	}

	if len(sources) > 0 {
		if err = json.Unmarshal([]byte(sources), &call.Sources); err != nil {
			return nil, fmt.Errorf("getcall.unmarshal.sources: %v", err)
		}
	}

	return &call, nil
}
