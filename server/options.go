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
	"sync"

	"golang.org/x/crypto/bcrypt"
)

type Options struct {
	AfsSystems                  string `json:"afsSystems"`
	AudioConversion             uint   `json:"audioConversion"`
	AudioCompression            uint   `json:"audioCompression"`
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
	SearchPatchedTalkgroups     bool   `json:"searchPatchedTalkgroups"`
	ShowListenersCount          bool   `json:"showListenersCount"`
	SortTalkgroups              bool   `json:"sortTalkgroups"`
	TagsToggle                  bool   `json:"tagsToggle"`
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

const (
	AUDIO_COMPRESSION_DEFAULT          = 0
	AUDIO_COMPRESSION_LOW              = 1
	AUDIO_COMPRESSION_MEDIUM           = 2
	AUDIO_COMPRESSION_HIGH             = 3
	AUDIO_COMPRESSION_ULTRA            = 4
	AUDIO_COMPRESSION_EXTREME          = 5
	AUDIO_COMPRESSION_BETA_1           = 6
	AUDIO_COMPRESSION_BETA_2           = 7
)

func NewOptions() *Options {
	return &Options{
		mutex: sync.Mutex{},
	}
}

func (options *Options) FromMap(m map[string]any) *Options {
	options.mutex.Lock()
	defer options.mutex.Unlock()

	switch v := m["afsSystems"].(type) {
	case string:
		options.AfsSystems = v
	}

	switch v := m["audioConversion"].(type) {
	case float64:
		options.AudioConversion = uint(v)
	default:
		options.MaxClients = defaults.options.audioConversion
	}

	switch v := m["audioCompression"].(type) {
	case float64:
		options.AudioCompression = uint(v)
	default:
		options.AudioCompression = defaults.options.audioCompression
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

	switch v := m["searchPatchedTalkgroups"].(type) {
	case bool:
		options.SearchPatchedTalkgroups = v
	default:
		options.SearchPatchedTalkgroups = defaults.options.searchPatchedTalkgroups
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

	switch v := m["tagsToggle"].(type) {
	case bool:
		options.TagsToggle = v
	default:
		options.TagsToggle = defaults.options.tagsToggle
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
		s               string
	)

	options.mutex.Lock()
	defer options.mutex.Unlock()

	defaultPassword, _ = bcrypt.GenerateFromPassword([]byte(defaults.adminPassword), bcrypt.DefaultCost)

	options.adminPassword = string(defaultPassword)
	options.adminPasswordNeedChange = defaults.adminPasswordNeedChange
	options.AudioConversion = defaults.options.audioConversion
	options.AudioCompression = defaults.options.audioCompression
	options.AutoPopulate = defaults.options.autoPopulate
	options.DimmerDelay = defaults.options.dimmerDelay
	options.DisableDuplicateDetection = defaults.options.disableDuplicateDetection
	options.DuplicateDetectionTimeFrame = defaults.options.duplicateDetectionTimeFrame
	options.KeypadBeeps = defaults.options.keypadBeeps
	options.MaxClients = defaults.options.maxClients
	options.PlaybackGoesLive = defaults.options.playbackGoesLive
	options.PruneDays = defaults.options.pruneDays
	options.SearchPatchedTalkgroups = defaults.options.searchPatchedTalkgroups
	options.ShowListenersCount = defaults.options.showListenersCount
	options.SortTalkgroups = defaults.options.sortTalkgroups
	options.TagsToggle = defaults.options.tagsToggle

	err = db.Sql.QueryRow("select `val` from `rdioScannerConfigs` where `key` = 'adminPassword'").Scan(&s)
	if err == nil {
		if err = json.Unmarshal([]byte(s), &s); err == nil {
			options.adminPassword = s
		}
	}

	err = db.Sql.QueryRow("select `val` from `rdioScannerConfigs` where `key` = 'adminPasswordNeedChange'").Scan(&s)
	if err == nil {
		var b bool
		if err = json.Unmarshal([]byte(s), &b); err == nil {
			options.adminPasswordNeedChange = b
		}
	}

	err = db.Sql.QueryRow("select `val` from `rdioScannerConfigs` where `key` = 'options'").Scan(&s)
	if err == nil {
		var m map[string]any

		if err = json.Unmarshal([]byte(s), &m); err == nil {
			switch v := m["afsSystems"].(type) {
			case string:
				options.AfsSystems = v
			}

			switch v := m["audioConversion"].(type) {
			case float64:
				options.AudioConversion = uint(v)
			}

			switch v := m["autoPopulate"].(type) {
			case bool:
				options.AutoPopulate = v
			}

			switch v := m["branding"].(type) {
			case string:
				options.Branding = v
			}

			switch v := m["dimmerDelay"].(type) {
			case float64:
				options.DimmerDelay = uint(v)
			}

			switch v := m["disableDuplicateDetection"].(type) {
			case bool:
				options.DisableDuplicateDetection = v
			}

			switch v := m["duplicateDetectionTimeFrame"].(type) {
			case float64:
				options.DuplicateDetectionTimeFrame = uint(v)
			}

			switch v := m["email"].(type) {
			case string:
				options.Email = v
			}

			switch v := m["keypadBeeps"].(type) {
			case string:
				options.KeypadBeeps = v
			}

			switch v := m["maxClients"].(type) {
			case float64:
				options.MaxClients = uint(v)
			}

			switch v := m["playbackGoesLive"].(type) {
			case bool:
				options.PlaybackGoesLive = v
			}

			switch v := m["pruneDays"].(type) {
			case float64:
				options.PruneDays = uint(v)
			}

			switch v := m["searchPatchedTalkgroups"].(type) {
			case bool:
				options.SearchPatchedTalkgroups = v
			}

			switch v := m["showListenersCount"].(type) {
			case bool:
				options.ShowListenersCount = v
			}

			switch v := m["sortTalkgroups"].(type) {
			case bool:
				options.SortTalkgroups = v
			}

			switch v := m["tagsToggle"].(type) {
			case bool:
				options.TagsToggle = v
			}

			switch v := m["time12hFormat"].(type) {
			case bool:
				options.Time12hFormat = v
			}
		}
	}

	err = db.Sql.QueryRow("select `val` from `rdioScannerConfigs` where `key` = 'secret'").Scan(&s)
	if err == nil {
		if err = json.Unmarshal([]byte(s), &s); err == nil {
			options.secret = s
		}
	}

	return nil
}

func (options *Options) Write(db *Database) error {
	var (
		b   []byte
		err error
		i   int64
		res sql.Result
	)

	options.mutex.Lock()
	defer options.mutex.Unlock()

	formatError := func(err error) error {
		return fmt.Errorf("options.write: %v", err)
	}

	if b, err = json.Marshal(options.adminPassword); err != nil {
		return formatError(err)
	}

	if res, err = db.Sql.Exec("update `rdioScannerConfigs` set `val` = ? where `key` = 'adminPassword'", string(b)); err != nil {
		return formatError(err)
	}

	if i, err = res.RowsAffected(); err == nil && i == 0 {
		db.Sql.Exec("insert into `rdioScannerConfigs` (`key`, `val`) values (?, ?)", "adminPassword", string(b))
	}

	if b, err = json.Marshal(options.adminPasswordNeedChange); err != nil {
		return formatError(err)
	}

	if res, err = db.Sql.Exec("update `rdioScannerConfigs` set `val` = ? where `key` = 'adminPasswordNeedChange'", string(b)); err != nil {
		return formatError(err)
	}

	if i, err = res.RowsAffected(); err == nil && i == 0 {
		db.Sql.Exec("insert into `rdioScannerConfigs` (`key`, `val`) values (?, ?)", "adminPasswordNeedChange", string(b))
	}

	if b, err = json.Marshal(map[string]any{
		"afsSystems":                  options.AfsSystems,
		"audioConversion":             options.AudioConversion,
		"audioCompression":            options.AudioCompression,
		"autoPopulate":                options.AutoPopulate,
		"branding":                    options.Branding,
		"dimmerDelay":                 options.DimmerDelay,
		"disableDuplicateDetection":   options.DisableDuplicateDetection,
		"duplicateDetectionTimeFrame": options.DuplicateDetectionTimeFrame,
		"email":                       options.Email,
		"keypadBeeps":                 options.KeypadBeeps,
		"maxClients":                  options.MaxClients,
		"playbackGoesLive":            options.PlaybackGoesLive,
		"pruneDays":                   options.PruneDays,
		"searchPatchedTalkgroups":     options.SearchPatchedTalkgroups,
		"showListenersCount":          options.ShowListenersCount,
		"sortTalkgroups":              options.SortTalkgroups,
		"tagsToggle":                  options.TagsToggle,
		"time12hFormat":               options.Time12hFormat,
	}); err != nil {
		return formatError(err)
	}

	if res, err = db.Sql.Exec("update `rdioScannerConfigs` set `val` = ? where `key` = 'options'", string(b)); err != nil {
		return formatError(err)
	}

	if i, err = res.RowsAffected(); err == nil && i == 0 {
		db.Sql.Exec("insert into `rdioScannerConfigs` (`key`, `val`) values (?, ?)", "options", string(b))
	}

	return nil
}
