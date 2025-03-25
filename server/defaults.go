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

type Defaults struct {
	adminPassword           string
	adminPasswordNeedChange bool
	access                  DefaultAccess
	apikey                  DefaultApikey
	dirwatch                DefaultDirwatch
	downstream              DefaultDownstream
	groups                  []string
	keypadBeeps             string
	options                 DefaultOptions
	systems                 []System
	tags                    []string
}

type DefaultAccess struct {
	ident   string
	systems string
}

type DefaultApikey struct {
	ident   string
	systems string
}

type DefaultDirwatch struct {
	deleteAfter bool
	disabled    bool
	usePolling  bool
}

type DefaultDownstream struct {
	systems string
}

type DefaultOptions struct {
	autoPopulate                bool
	audioConversion             uint
	audioCompression            uint
	dimmerDelay                 uint
	disableDuplicateDetection   bool
	duplicateDetectionTimeFrame uint
	keypadBeeps                 string
	maxClients                  uint
	playbackGoesLive            bool
	pruneDays                   uint
	searchPatchedTalkgroups     bool
	showListenersCount          bool
	sortTalkgroups              bool
	tagsToggle                  bool
	time12hFormat               bool
}

var defaults Defaults = Defaults{
	adminPassword:           "rdio-scanner",
	adminPasswordNeedChange: true,
	access: DefaultAccess{
		ident:   "Unknown",
		systems: "*",
	},
	apikey: DefaultApikey{
		ident:   "Unknown",
		systems: "*",
	},
	dirwatch: DefaultDirwatch{
		deleteAfter: true,
		disabled:    false,
		usePolling:  false,
	},
	downstream: DefaultDownstream{
		systems: "*",
	},
	groups: []string{
		"Air",
		"EMS",
		"Fire",
		"Interop",
		"Law",
		"Unknown",
	},
	keypadBeeps: "uniden",
	options: DefaultOptions{
		audioConversion:             AUDIO_CONVERSION_ENABLED,
		audioCompression:            AUDIO_COMPRESSION_DEFAULT,
		autoPopulate:                true,
		dimmerDelay:                 5000,
		disableDuplicateDetection:   false,
		duplicateDetectionTimeFrame: 500,
		keypadBeeps:                 "uniden",
		maxClients:                  200,
		playbackGoesLive:            false,
		pruneDays:                   7,
		searchPatchedTalkgroups:     false,
		showListenersCount:          false,
		sortTalkgroups:              false,
		tagsToggle:                  false,
		time12hFormat:               false,
	},
	systems: []System{},
	tags: []string{
		"Air Traffic Control",
		"Emergency ",
		"Fire Dispatch",
		"Fire Tac",
		"Fire Talk",
		"Interop",
		"Security",
		"Service",
		"Untagged",
	},
}
