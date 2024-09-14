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
	"log"
	"math/rand"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

type Options struct {
	AudioConversion             uint   `json:"audioConversion"`
	AutoPopulate                bool   `json:"autoPopulate"`
	Branding                    string `json:"branding"`
	DimmerDelay                 uint   `json:"dimmerDelay"`
	DisableDuplicateDetection   bool   `json:"disableDuplicateDetection"`
	DuplicateDetectionTimeFrame uint   `json:"duplicateDetectionTimeFrame"`
	Email                       string `json:"email"`
	KeypadBeeps                 string `json:"keypadBeeps"`
	MaxClients                  uint   `json:"maxClients"`
	PlaybackGoesLive            bool   `json:"playbackGoesLive"`
	PruneDays                   uint   `json:"pruneDays"`
	ShowListenersCount          bool   `json:"showListenersCount"`
	SortTalkgroups              bool   `json:"sortTalkgroups"`
	Time12hFormat               bool   `json:"time12hFormat"`
	adminPassword               string
	adminPasswordNeedChange     bool
	mutex                       sync.Mutex
	secret                      string
}

const (
	AUDIO_CONVERSION_DISABLED          = 0
	AUDIO_CONVERSION_ENABLED           = 1
	AUDIO_CONVERSION_ENABLED_NORM      = 2
	AUDIO_CONVERSION_ENABLED_LOUD_NORM = 3
)

func NewOptions() *Options {
	return &Options{
		mutex: sync.Mutex{},
	}
}

func (options *Options) FromMap(m map[string]any) *Options {
	options.mutex.Lock()
	defer options.mutex.Unlock()

	switch v := m["audioConversion"].(type) {
	case float64:
		options.AudioConversion = uint(v)
	default:
		options.MaxClients = defaults.options.audioConversion
	}

	switch v := m["autoPopulate"].(type) {
	case bool:
		options.AutoPopulate = v
	default:
		options.AutoPopulate = defaults.options.autoPopulate
	}

	switch v := m["branding"].(type) {
	case string:
		options.Branding = v
	}

	switch v := m["dimmerDelay"].(type) {
	case float64:
		options.DimmerDelay = uint(v)
	default:
		options.DimmerDelay = defaults.options.dimmerDelay
	}

	switch v := m["disableAudioConversion"].(type) {
	case bool:
		if v {
			options.AudioConversion = 2
		} else {
			options.AudioConversion = 0
		}
	}

	switch v := m["disableDuplicateDetection"].(type) {
	case bool:
		options.DisableDuplicateDetection = v
	default:
		options.DisableDuplicateDetection = defaults.options.disableDuplicateDetection
	}

	switch v := m["duplicateDetectionTimeFrame"].(type) {
	case float64:
		options.DuplicateDetectionTimeFrame = uint(v)
	default:
		options.DuplicateDetectionTimeFrame = defaults.options.duplicateDetectionTimeFrame
	}

	switch v := m["email"].(type) {
	case string:
		options.Email = v
	}

	switch v := m["keypadBeeps"].(type) {
	case string:
		options.KeypadBeeps = v
	default:
		options.KeypadBeeps = defaults.options.keypadBeeps
	}

	switch v := m["maxClients"].(type) {
	case float64:
		options.MaxClients = uint(v)
	default:
		options.MaxClients = defaults.options.maxClients
	}

	switch v := m["playbackGoesLive"].(type) {
	case bool:
		options.PlaybackGoesLive = v
	}

	switch v := m["pruneDays"].(type) {
	case float64:
		options.PruneDays = uint(v)
	default:
		options.PruneDays = defaults.options.pruneDays
	}

	switch v := m["showListenersCount"].(type) {
	case bool:
		options.ShowListenersCount = v
	default:
		options.ShowListenersCount = defaults.options.showListenersCount
	}

	switch v := m["sortTalkgroups"].(type) {
	case bool:
		options.SortTalkgroups = v
	default:
		options.SortTalkgroups = defaults.options.sortTalkgroups
	}

	switch v := m["time12hFormat"].(type) {
	case bool:
		options.Time12hFormat = v
	default:
		options.Time12hFormat = defaults.options.time12hFormat
	}

	return options
}

func (options *Options) Read(db *Database) error {
	var (
		defaultPassword []byte
		err             error
		f               any
		query           string
		rows            *sql.Rows

		key   sql.NullString
		value sql.NullString
	)

	options.mutex.Lock()
	defer options.mutex.Unlock()

	defaultPassword, _ = bcrypt.GenerateFromPassword([]byte(defaults.adminPassword), bcrypt.DefaultCost)

	options.adminPassword = string(defaultPassword)
	options.adminPasswordNeedChange = defaults.adminPasswordNeedChange
	options.AudioConversion = defaults.options.audioConversion
	options.AutoPopulate = defaults.options.autoPopulate
	options.DimmerDelay = defaults.options.dimmerDelay
	options.DisableDuplicateDetection = defaults.options.disableDuplicateDetection
	options.DuplicateDetectionTimeFrame = defaults.options.duplicateDetectionTimeFrame
	options.KeypadBeeps = defaults.options.keypadBeeps
	options.MaxClients = defaults.options.maxClients
	options.PlaybackGoesLive = defaults.options.playbackGoesLive
	options.PruneDays = defaults.options.pruneDays
	options.ShowListenersCount = defaults.options.showListenersCount
	options.SortTalkgroups = defaults.options.sortTalkgroups

	formatError := errorFormatter("options", "read")

	newSecret := func(n uint) string {
		var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!#$%&")

		s := make([]rune, n)
		for i := range s {
			s[i] = letters[rand.Intn(len(letters))]
		}
		return string(s)
	}

	query = `SELECT "key", "value" FROM "options"`
	if rows, err = db.Sql.Query(query); err != nil {
		return formatError(err, query)
	}

	for rows.Next() {
		if err = rows.Scan(&key, &value); err != nil {
			continue
		}

		if !key.Valid || !value.Valid {
			continue
		}

		switch key.String {
		case "adminPassword":
			if err = json.Unmarshal([]byte(value.String), &f); err == nil {
				switch v := f.(type) {
				case string:
					options.adminPassword = v
				}
			}
		case "adminPasswordNeedChange":
			if err = json.Unmarshal([]byte(value.String), &f); err == nil {
				switch v := f.(type) {
				case bool:
					options.adminPasswordNeedChange = v
				}
			}
		case "audioConversion":
			if err = json.Unmarshal([]byte(value.String), &f); err == nil {
				switch v := f.(type) {
				case float64:
					options.AudioConversion = uint(v)
				}
			}
		case "autoPopulate":
			if err = json.Unmarshal([]byte(value.String), &f); err == nil {
				switch v := f.(type) {
				case bool:
					options.AutoPopulate = v
				}
			}
		case "branding":
			if err = json.Unmarshal([]byte(value.String), &f); err == nil {
				switch v := f.(type) {
				case string:
					options.Branding = v
				}
			}
		case "dimmerDelay":
			if err = json.Unmarshal([]byte(value.String), &f); err == nil {
				switch v := f.(type) {
				case float64:
					options.DimmerDelay = uint(v)
				}
			}
		case "disableDuplicateDetection":
			if err = json.Unmarshal([]byte(value.String), &f); err == nil {
				switch v := f.(type) {
				case bool:
					options.DisableDuplicateDetection = v
				}
			}
		case "duplicateDetectionTimeframe":
			if err = json.Unmarshal([]byte(value.String), &f); err == nil {
				switch v := f.(type) {
				case float64:
					options.DuplicateDetectionTimeFrame = uint(v)
				}
			}
		case "email":
			if err = json.Unmarshal([]byte(value.String), &f); err == nil {
				switch v := f.(type) {
				case string:
					options.Email = v
				}
			}
		case "keypadBeeps":
			if err = json.Unmarshal([]byte(value.String), &f); err == nil {
				switch v := f.(type) {
				case string:
					options.KeypadBeeps = v
				}
			}
		case "maxClients":
			if err = json.Unmarshal([]byte(value.String), &f); err == nil {
				switch v := f.(type) {
				case float64:
					options.MaxClients = uint(v)
				}
			}
		case "playbackGoesLive":
			if err = json.Unmarshal([]byte(value.String), &f); err == nil {
				switch v := f.(type) {
				case bool:
					options.PlaybackGoesLive = v
				}
			}
		case "pruneDays":
			if err = json.Unmarshal([]byte(value.String), &f); err == nil {
				switch v := f.(type) {
				case float64:
					options.PruneDays = uint(v)
				}
			}
		case "secret":
			if err = json.Unmarshal([]byte(value.String), &f); err == nil {
				const n = 256
				switch v := f.(type) {
				case string:
					if len(v) == n {
						options.secret = v
					} else {
						options.secret = newSecret(n)
					}
				default:
					options.secret = newSecret(n)
				}
			}
		case "showListenersCount":
			if err = json.Unmarshal([]byte(value.String), &f); err == nil {
				switch v := f.(type) {
				case bool:
					options.ShowListenersCount = v
				}
			}
		case "sortTalkgroups":
			if err = json.Unmarshal([]byte(value.String), &f); err == nil {
				switch v := f.(type) {
				case bool:
					options.SortTalkgroups = v
				}
			}
		case "time12hFormat":
			if err = json.Unmarshal([]byte(value.String), &f); err == nil {
				switch v := f.(type) {
				case bool:
					options.Time12hFormat = v
				}
			}
		}
	}

	return nil
}

func (options *Options) Write(db *Database) error {
	var (
		err error
		res sql.Result
		tx  *sql.Tx
	)
	options.mutex.Lock()
	defer options.mutex.Unlock()

	formatError := errorFormatter("options", "write")

	set := func(key string, val any) {
		if val, err = json.Marshal(val); err == nil {
			switch v := val.(type) {
			case string:
				val = escapeQuotes(v)
			}

			query := fmt.Sprintf(`UPDATE "options" SET "value" = '%s' WHERE "key" = '%s'`, val, key)
			if res, err = tx.Exec(query); err == nil {
				if i, err := res.RowsAffected(); err == nil && i == 0 {
					query = fmt.Sprintf(`INSERT INTO "options" ("key", "value") VALUES ('%s', '%s')`, key, val)
					if _, err = tx.Exec(query); err != nil {
						log.Println(formatError(err, query))
					}
				}
			}
		}
	}

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	set("adminPassword", options.adminPassword)
	set("adminPasswordNeedChange", options.adminPasswordNeedChange)
	set("autoPopulate", options.AutoPopulate)
	set("branding", options.Branding)
	set("dimmerDelay", options.DimmerDelay)
	set("disableDuplicateDetection", options.DisableDuplicateDetection)
	set("duplicateDetectionTimeFrame", options.DuplicateDetectionTimeFrame)
	set("email", options.Email)
	set("keypadBeeps", options.KeypadBeeps)
	set("maxClients", options.MaxClients)
	set("playbackGoesLive", options.PlaybackGoesLive)
	set("pruneDays", options.PruneDays)
	set("secret", options.secret)
	set("showListenersCount", options.ShowListenersCount)
	set("sortTalkgroups", options.SortTalkgroups)
	set("time12hFormat", options.Time12hFormat)

	if err = tx.Commit(); err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	return nil
}
