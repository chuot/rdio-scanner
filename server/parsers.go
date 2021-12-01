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
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dhowden/tag"
)

func ParseSdrTrunkMeta(call *Call, controller *Controller) error {
	var (
		s   []string
		err error
		i   int
		m   tag.Metadata
		t   time.Time
	)

	if m, err = tag.ReadFrom(bytes.NewReader(call.Audio)); err != nil {
		return err
	}

	if i, err = strconv.Atoi(m.Artist()); err != nil {
		return err
	}
	call.Source = uint(i)

	s = regexp.MustCompile(`Date:([^;]+);`).FindStringSubmatch(m.Comment())
	if len(s) == 2 {
		if t, err = time.ParseInLocation("2006-01-02 15:04:05.999", s[1], time.Now().Location()); err != nil {
			return err
		}
		call.DateTime = t.UTC()
	}

	s = regexp.MustCompile(`Frequency:([0-9]+);`).FindStringSubmatch(m.Comment())
	if len(s) == 2 {
		if i, err = strconv.Atoi(s[1]); err != nil {
			return err
		}
		call.Frequency = uint(i)
	}

	s = regexp.MustCompile(`System:([^;]+);`).FindStringSubmatch(m.Comment())
	if len(s) == 2 {
		if system, ok := controller.Systems.GetSystem(s[1]); ok {
			call.System = system.Id
		} else {
			call.System = controller.Systems.GetNewSystemId()
			call.systemLabel = s[1]
		}
	}

	s = regexp.MustCompile(`^([0-9]+)`).FindStringSubmatch(m.Title())
	if len(s) > 1 {
		if i, err = strconv.Atoi(s[1]); err != nil {
			return err
		}
		call.Talkgroup = uint(i)
	}

	s = regexp.MustCompile(`"([[:alnum:]]+)"`).FindStringSubmatch(m.Title())
	if len(s) > 1 && !strings.EqualFold(s[1], "all") {
		call.talkgroupLabel = s[1]
	}

	return nil
}

func ParseTrunkRecorderMeta(call *Call, b []byte) error {
	m := map[string]interface{}{}

	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}

	switch v := m["freq"].(type) {
	case float64:
		call.Frequency = uint(v)
	}

	switch v := m["freqList"].(type) {
	case []interface{}:
		freqs := []map[string]interface{}{}
		for _, f := range v {
			freq := map[string]interface{}{}
			switch v := f.(type) {
			case map[string]interface{}:
				switch v := v["error_count"].(type) {
				case float64:
					freq["errorCount"] = uint(v)
				}

				switch v := v["freq"].(type) {
				case float64:
					freq["freq"] = uint(v)
				}

				switch v := v["len"].(type) {
				case float64:
					freq["len"] = uint(v)
				}

				switch v := v["pos"].(type) {
				case float64:
					freq["pos"] = uint(v)
				}

				switch v := v["spike_count"].(type) {
				case float64:
					freq["spikeCount"] = uint(v)
				}

				freqs = append(freqs, freq)
			}
		}
		call.Frequencies = freqs
	}

	switch v := m["srcList"].(type) {
	case []interface{}:
		sources := []map[string]interface{}{}
		for _, f := range v {
			source := map[string]interface{}{}
			switch v := f.(type) {
			case map[string]interface{}:
				switch v := v["pos"].(type) {
				case float64:
					source["pos"] = uint(v)
				}

				switch s := v["src"].(type) {
				case float64:
					source["src"] = uint(s)

					switch t := v["tag"].(type) {
					case string:
						var units Units
						switch v := call.units.(type) {
						case Units:
							units = v
						default:
							units = Units{}
						}
						units.Add(uint(s), t)
						call.units = units
					}
				}

				sources = append(sources, source)
			}
		}

		if len(sources) > 0 {
			call.Source = sources[0]["src"]
		}

		call.Sources = sources
	}

	switch v := m["start_time"].(type) {
	case float64:
		call.DateTime = time.Unix(int64(v), 0).UTC()
	}

	switch v := m["talkgroup"].(type) {
	case float64:
		call.Talkgroup = uint(v)
	}

	switch v := m["talkgroup_tag"].(type) {
	case string:
		call.talkgroupTag = v
		call.talkgroupLabel = v
	}

	return nil
}
