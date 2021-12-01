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
	"strconv"
)

type LivefeedMap map[uint]map[uint]bool

func (livefeedMap *LivefeedMap) FromMap(f interface{}) {
	for s := range *livefeedMap {
		delete(*livefeedMap, s)
	}

	switch v := f.(type) {
	case map[string]interface{}:
		for s, n := range v {
			if sysId, err := strconv.Atoi(s); err == nil {
				sysId := uint(sysId)
				switch v := n.(type) {
				case map[string]interface{}:
					for t, b := range v {
						switch v := b.(type) {
						case bool:
							if tgId, err := strconv.Atoi(t); err == nil {
								tgId := uint(tgId)
								if (*livefeedMap)[sysId] == nil {
									(*livefeedMap)[sysId] = map[uint]bool{}
								}
								(*livefeedMap)[sysId][tgId] = v
							}
						}
					}
				}
			}
		}
	}
}

func (livefeedMap *LivefeedMap) IsAllOff() bool {
	isAllOff := true

sys:
	for _, sys := range *livefeedMap {
		for _, tg := range sys {
			if tg {
				isAllOff = false
				break sys
			}
		}
	}

	return isAllOff
}

func (livefeedMap *LivefeedMap) IsEnabled(call *Call) bool {
	return (*livefeedMap)[call.System][call.Talkgroup]
}
