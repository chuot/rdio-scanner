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
	"strconv"
	"sync"
)

type Livefeed struct {
	Matrix map[uint]map[uint]bool
	mutex  sync.Mutex
}

func NewLivefeed() *Livefeed {
	return &Livefeed{
		Matrix: map[uint]map[uint]bool{},
		mutex:  sync.Mutex{},
	}
}

func (livefeed *Livefeed) FromMap(f interface{}) *Livefeed {
	livefeed.mutex.Lock()
	defer livefeed.mutex.Unlock()

	for s := range livefeed.Matrix {
		delete(livefeed.Matrix, s)
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
								if livefeed.Matrix[sysId] == nil {
									livefeed.Matrix[sysId] = map[uint]bool{}
								}
								livefeed.Matrix[sysId][tgId] = v
							}
						}
					}
				}
			}
		}
	}

	return livefeed
}

func (livefeed *Livefeed) IsAllOff() bool {
	livefeed.mutex.Lock()
	defer livefeed.mutex.Unlock()

	for _, sys := range livefeed.Matrix {
		for _, tg := range sys {
			if tg {
				return false
			}
		}
	}

	return true
}

func (livefeed *Livefeed) IsEnabled(call *Call) bool {
	livefeed.mutex.Lock()
	defer livefeed.mutex.Unlock()

	if call != nil {
		if livefeed.Matrix[call.System][call.Talkgroup] {
			return true
		} else {
			switch v := call.Patches.(type) {
			case []uint:
				for _, p := range v {
					if livefeed.Matrix[call.System][p] {
						return true
					}
				}
			}
		}
	}

	return false
}
