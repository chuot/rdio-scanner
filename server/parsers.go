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
	"encoding/json"
	"fmt"
	"mime"
	"mime/multipart"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dhowden/tag"
)

func ParseDSDPlusMeta(call *Call, fp string) error {
	dir := filepath.Dir(fp)
	base := strings.TrimSuffix(filepath.Base(fp), filepath.Ext(fp))
	meta := []string{""}

	lbl := false
	ptr := 0
	for i := 0; i < len(base); i++ {
		if base[i] == '[' {
			lbl = true
		} else if base[i] == ']' {
			lbl = false
		}
		if !lbl && base[i] == '_' {
			ptr++
			meta = append(meta, "")
		} else {
			meta[ptr] += string(base[i])
		}
	}

	if d := regexp.MustCompile(`([0-9]+)$`).FindStringSubmatch(dir); len(d) == 2 && len(d[1]) == 8 {
		if t := regexp.MustCompile(`^([0-9]+)`).FindStringSubmatch(base); len(t) == 2 && len(t[1]) == 6 {
			if dy, err := strconv.Atoi(d[1][0:4]); err == nil {
				if dm, err := strconv.Atoi(d[1][4:6]); err == nil {
					if dd, err := strconv.Atoi(d[1][6:8]); err == nil {
						if th, err := strconv.Atoi(t[1][0:2]); err == nil {
							if tm, err := strconv.Atoi(t[1][2:4]); err == nil {
								if ts, err := strconv.Atoi(t[1][4:6]); err == nil {
									call.Timestamp = time.Date(dy, time.Month(dm), dd, th, tm, ts, 0, time.Now().Location()).UTC()
								}
							}
						}
					}
				}
			}
		}
	}

	if len(meta) > 3 {
		switch meta[2] {
		case "ConP(BS)", "DMR(BS)", "P25(BS)":
			if s := regexp.MustCompile(`^([0-9]+)-.+$`).FindStringSubmatch(meta[3]); len(s) > 1 {
				if i, err := strconv.Atoi(s[1]); err == nil {
					call.Meta.SystemRef = uint(i)
				}
			}

		case "NEXEDGE48(CB)", "NEXEDGE48(CS)", "NEXEDGE48(TB)", "NEXEDGE96(CB)", "NEXEDGE96(CS)", "NEXEDGE96(TB)":
			if s := regexp.MustCompile(`^.([0-9]+)-[0-9]+$`).FindStringSubmatch(meta[3]); len(s) > 1 {
				if i, err := strconv.Atoi(s[1]); err == nil && i > 0 {
					call.Meta.SystemRef = uint(i)
				}

			} else if len(meta) > 4 {
				if s := regexp.MustCompile(`RAN([0-9]+)`).FindStringSubmatch(meta[4]); len(s) > 1 {
					if i, err := strconv.Atoi(s[1]); err == nil && i > 0 {
						call.Meta.SystemRef = uint(i)
					}
				}
			}

		case "P25":
			if s := regexp.MustCompile(`^[^\.]+\.([^-]+)`).FindStringSubmatch(meta[3]); len(s) > 1 {
				if i, err := strconv.ParseInt(s[1], 16, 64); err == nil && i > 0 {
					call.Meta.SystemRef = uint(i)
				}
			}
		}
	}

	if s := regexp.MustCompile(`[^\[\]]+`).FindAllString(meta[len(meta)-2], -1); len(s) > 0 {
		if i, err := strconv.Atoi(s[0]); err == nil && i > 0 {
			call.Meta.TalkgroupRef = uint(i)
		}

		if len(s) > 1 && len(s[1]) > 0 {
			if !regexp.MustCompile(`^([\.\-\ ,_]+)$`).MatchString(s[1]) {
				call.Meta.TalkgroupLabel = s[1]
			}
		}
	}

	if s := regexp.MustCompile(`[^\[\]]+`).FindAllString(meta[len(meta)-1], -1); len(s) > 0 {
		if src, err := strconv.Atoi(s[0]); err == nil && src > 0 {
			call.Units = append(call.Units, CallUnit{
				UnitRef: uint(src),
				Offset:  0,
			})

			if len(s) > 1 && len(s[1]) > 0 {
				l := strings.TrimSpace(s[1])
				if l != fmt.Sprintf("%v", src) {
					if !regexp.MustCompile(`^([\.\-\ ,_]+)$`).MatchString(s[1]) {
						call.Meta.UnitLabels = append(call.Meta.UnitLabels, l)
						call.Meta.UnitRefs = append(call.Meta.UnitRefs, uint(src))
					}
				}
			}
		}
	}

	return nil
}

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

	s = regexp.MustCompile(`^([0-9]+) ?(.*)$`).FindStringSubmatch(m.Artist())
	if len(s) >= 2 {
		if i, err = strconv.Atoi(s[1]); err != nil {
			return err
		}
		if i > 0 {
			call.Units = append(call.Units, CallUnit{
				UnitRef: uint(i),
				Offset:  0,
			})

			if len(s) >= 3 && len(s[2]) > 0 {
				call.Meta.UnitLabels = append(call.Meta.UnitLabels, strings.TrimSpace(s[2]))
				call.Meta.UnitRefs = append(call.Meta.UnitRefs, uint(i))
			}
		}
	}

	s = regexp.MustCompile(`Date:([^;]+);`).FindStringSubmatch(m.Comment())
	if len(s) == 2 {
		if t, err = time.ParseInLocation("2006-01-02 15:04:05.999", s[1], time.Now().Location()); err == nil {
			call.Timestamp = t
		} else {
			return err
		}
	}

	s = regexp.MustCompile(`Frequency:([0-9]+);`).FindStringSubmatch(m.Comment())
	if len(s) == 2 && len(s[1]) > 0 {
		if i, err = strconv.Atoi(s[1]); err != nil {
			return err
		}
		if i > 0 {
			call.Frequencies = []CallFrequency{
				{
					Frequency: uint(i),
					Offset:    0,
				},
			}
		}
	}

	s = regexp.MustCompile(`System:([^;]+);`).FindStringSubmatch(m.Comment())
	if len(s) == 2 {
		if system, ok := controller.Systems.GetSystemByLabel(s[1]); ok {
			call.System = system

		} else {
			call.Meta.SystemRef = controller.Systems.GetNewSystemRef()
			call.Meta.SystemLabel = s[1]
		}
	}

	s = regexp.MustCompile(`([0-9]+)`).FindStringSubmatch(m.Title())
	if len(s) > 1 && len(s[1]) > 0 {
		if i, err = strconv.Atoi(s[1]); err != nil {
			return err
		}
		if i > 0 {
			call.Meta.TalkgroupRef = uint(i)
		}
	}

	s = regexp.MustCompile(`(\[.+\])`).FindStringSubmatch(m.Title())
	if len(s) > 1 && len(s[1]) > 0 {
		var f any
		if err = json.Unmarshal([]byte(s[1]), &f); err == nil {
			switch v := f.(type) {
			case []any:
				for _, patch := range v {
					switch v := patch.(type) {
					case float64:
						if v > 0 {
							call.Patches = append(call.Patches, uint(v))
						}
					}
				}
			}
		}
	}

	s = regexp.MustCompile(`"([^"]+)"`).FindStringSubmatch(m.Title())
	if len(s) > 1 {
		call.Meta.TalkgroupLabel = s[1]
		call.Meta.TalkgroupName = s[1]
	}

	return nil
}

func ParseMultipartContent(call *Call, p *multipart.Part, b []byte) {
	switch p.FormName() {
	case "audio":
		call.Audio = b
		call.AudioFilename = p.FileName()

	case "audioFilename", "audioName":
		call.AudioFilename = string(b)
		call.AudioMime = mime.TypeByExtension(path.Ext(string(b)))

	case "audioMime", "audioType":
		call.AudioMime = string(b)

	case "dateTime":
		if regexp.MustCompile(`^[0-9]+$`).Match(b) {
			if i, err := strconv.Atoi(string(b)); err == nil {
				call.Timestamp = time.Unix(int64(i), 0)
			}
		} else {
			if t, err := time.Parse(time.RFC3339, string(b)); err == nil {
				call.Timestamp = t
			}
		}

	case "frequencies":
		var f any
		if err := json.Unmarshal(b, &f); err == nil {
			switch v := f.(type) {
			case []any:
				call.Frequencies = []CallFrequency{}
				for _, f := range v {
					freq := CallFrequency{}
					switch v := f.(type) {
					case map[string]any:
						switch v := v["dbm"].(type) {
						case float64:
							if v >= 0 {
								freq.Dbm = int(v)
							}
						}
						switch v := v["errorCount"].(type) {
						case float64:
							if v >= 0 {
								freq.Errors = uint(v)
							}
						}
						switch v := v["freq"].(type) {
						case float64:
							if v > 0 {
								freq.Frequency = uint(v)
							}
						}
						switch v := v["pos"].(type) {
						case float64:
							if v >= 0 {
								freq.Offset = float32(v)
							}
						}
						switch v := v["spikeCount"].(type) {
						case float64:
							if v >= 0 {
								freq.Spikes = uint(v)
							}
						}
					}
					call.Frequencies = append(call.Frequencies, freq)
				}
			}
		}

	case "frequency":
		if i, err := strconv.Atoi(string(b)); err == nil && i > 0 {
			call.Frequencies = []CallFrequency{
				{
					Frequency: uint(i),
					Offset:    0,
				},
			}
		}

	case "patches", "patched_talkgroups":
		var f any
		if err := json.Unmarshal(b, &f); err == nil {
			switch v := f.(type) {
			case []any:
				for _, patch := range v {
					switch v := patch.(type) {
					case float64:
						if v > 0 {
							call.Patches = append(call.Patches, uint(v))
						}
					}
				}
			}
		}

	case "site":
		if i, err := strconv.Atoi(string(b)); err == nil && i > 0 {
			call.SiteRef = uint(i)
		}

	case "sources":
		var f any
		if err := json.Unmarshal(b, &f); err == nil {
			switch v := f.(type) {
			case []any:
				for _, f := range v {
					unit := CallUnit{}
					switch v := f.(type) {
					case map[string]any:
						switch v := v["pos"].(type) {
						case float64:
							if v >= 0 {
								unit.Offset = float32(v)
							}
						}
						switch s := v["src"].(type) {
						case float64:
							if s > 0 {
								unit.UnitRef = uint(s)
							}
						}
						switch s := v["tag"].(type) {
						case string:
							if len(s) > 0 {
								call.Meta.UnitLabels = []string{s}
							}
						}
					}
					call.Units = append(call.Units, unit)
				}
			}
		}

	case "source":
		if i, err := strconv.Atoi(string(b)); err == nil && i > 0 {
			call.Units = append(call.Units, CallUnit{
				Offset:  0,
				UnitRef: uint(i),
			})
		}

	case "system":
		if i, err := strconv.Atoi(string(b)); err == nil && i > 0 {
			call.Meta.SystemRef = uint(i)
		}

	case "systemLabel":
		call.Meta.SystemLabel = string(b)

	case "talkgroup":
		if i, err := strconv.Atoi(string(b)); err == nil && i > 0 {
			call.Meta.TalkgroupRef = uint(i)
		}

	case "talkgroupGroup":
		if s := string(b); len(s) > 0 && s != "-" {
			call.Meta.TalkgroupGroups = []string{s}
		}

	case "talkgroupGroups":
		if s := string(b); len(s) > 0 && s != "-" {
			call.Meta.TalkgroupGroups = strings.Split(s, ",")
		}

	case "talkgroupLabel":
		if s := string(b); len(s) > 0 && s != "-" {
			call.Meta.TalkgroupLabel = s
		}

	case "talkgroupName":
		if s := string(b); len(s) > 0 && s != "-" {
			call.Meta.TalkgroupName = s
		}

	case "talkgroupTag":
		if s := string(b); len(s) > 0 && s != "-" {
			call.Meta.TalkgroupTag = s
		}

	case "timestamp":
		if i, err := strconv.Atoi(string(b)); err == nil {
			call.Timestamp = time.UnixMilli(int64(i))
		}

	case "units":
		var f any
		if err := json.Unmarshal(b, &f); err == nil {
			switch v := f.(type) {
			case []any:
				for _, f := range v {
					unit := CallUnit{}
					switch v := f.(type) {
					case map[string]any:
						switch s := v["id"].(type) {
						case float64:
							if s > 0 {
								unit.UnitRef = uint(s)
							}
						}
						switch s := v["label"].(type) {
						case string:
							if len(s) > 0 {
								call.Meta.UnitLabels = []string{s}
							}
						}
						switch v := v["offset"].(type) {
						case float64:
							if v >= 0 {
								unit.Offset = float32(v)
							}
						}
					}
					call.Units = append(call.Units, unit)
				}
			}
		}

	case "unit":
		if i, err := strconv.Atoi(string(b)); err == nil && i > 0 {
			call.Units = append(call.Units, CallUnit{
				Offset:  0,
				UnitRef: uint(i),
			})
		}

	}
}

func ParseTrunkRecorderMeta(call *Call, b []byte) error {
	m := map[string]any{}

	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}

	switch v := m["freqList"].(type) {
	case []any:
		for _, f := range v {
			freq := CallFrequency{}
			switch v := f.(type) {
			case map[string]any:
				switch v := v["error_count"].(type) {
				case float64:
					if v >= 0 {
						freq.Errors = uint(v)
					}
				}

				switch v := v["freq"].(type) {
				case float64:
					if v > 0 {
						freq.Frequency = uint(v)
					}
				}

				switch v := v["pos"].(type) {
				case float64:
					if v >= 0 {
						freq.Offset = float32(v)
					}
				}

				switch v := v["spike_count"].(type) {
				case float64:
					if v >= 0 {
						freq.Spikes = uint(v)
					}
				}

				call.Frequencies = append(call.Frequencies, freq)
			}
		}
	}

	switch v := m["patched_talkgroups"].(type) {
	case []any:
		for _, f := range v {
			switch v := f.(type) {
			case float64:
				if v > 0 {
					call.Patches = append(call.Patches, uint(v))
				}
			}
		}
	}

	switch v := m["srcList"].(type) {
	case []any:
		for _, f := range v {
			unit := CallUnit{}
			switch v := f.(type) {
			case map[string]any:
				switch v := v["pos"].(type) {
				case float64:
					if v >= 0 {
						unit.Offset = float32(v)
					}
				}
				switch s := v["src"].(type) {
				case float64:
					if s > 0 {
						unit.UnitRef = uint(s)
					}
				}
				call.Units = append(call.Units, unit)
			}
		}
	}

	switch v := m["start_time"].(type) {
	case float64:
		call.Timestamp = time.Unix(int64(v), 0)
		call.Timestamp = time.Now() // DBEUG
	}

	switch v := m["short_name"].(type) {
	case string:
		if len(v) > 0 && v != "-" {
			call.Meta.SystemLabel = v
		}
	}

	switch v := m["talkgroup"].(type) {
	case float64:
		if v > 0 {
			call.Meta.TalkgroupRef = uint(v)
		}
	}

	switch v := m["talkgroup_description"].(type) {
	case string:
		if len(v) > 0 && v != "-" {
			call.Meta.TalkgroupName = v
		}
	}

	switch v := m["talkgroup_group"].(type) {
	case string:
		if len(v) > 0 && v != "-" {
			call.Meta.TalkgroupGroups = append(call.Meta.TalkgroupGroups, v)
		}
	}

	switch v := m["talkgroup_group_tag"].(type) {
	case string:
		if len(v) > 0 && v != "-" {
			call.Meta.TalkgroupTag = v
		}
	}

	switch v := m["talkgroup_tag"].(type) {
	case string:
		if len(v) > 0 && v != "-" {
			call.Meta.TalkgroupLabel = v
		}
	}

	switch v := m["timestamp"].(type) {
	case float64:
		call.Timestamp = time.UnixMilli(int64(v))
	}

	return nil
}
