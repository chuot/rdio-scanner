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
	"bytes"
	"encoding/json"
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

	if d := regexp.MustCompile(`([0-9]+)$`).FindStringSubmatch(dir); len(d) == 2 && len(d[1]) == 8 {
		if t := regexp.MustCompile(`^([0-9]+)`).FindStringSubmatch(base); len(t) == 2 && len(t[1]) == 6 {
			if dy, err := strconv.Atoi(d[1][0:4]); err == nil {
				if dm, err := strconv.Atoi(d[1][4:6]); err == nil {
					if dd, err := strconv.Atoi(d[1][6:8]); err == nil {
						if th, err := strconv.Atoi(t[1][0:2]); err == nil {
							if tm, err := strconv.Atoi(t[1][2:4]); err == nil {
								if ts, err := strconv.Atoi(t[1][4:6]); err == nil {
									call.DateTime = time.Date(dy, time.Month(dm), dd, th, tm, ts, 0, time.Now().Location()).UTC()
								}
							}
						}
					}
				}
			}
		}
	}

	if s := regexp.MustCompile(`^[0-9]+_[0-9]+_[^_]+_([0-9]+)-([0-9]+)_`).FindStringSubmatch(base); len(s) == 3 {
		if sys, err := strconv.Atoi(s[1]); err == nil && sys > 0 {
			call.System = uint(sys)
		}
	}

	tu := regexp.MustCompile(`([0-9]+)_([0-9]+)$|([0-9]+)_([0-9]+)\[([^\]]*)\]$`).FindStringSubmatch(base)

	if len(tu[3]) > 0 {
		tu[1] = tu[3]
	}

	if len(tu[4]) > 0 {
		tu[2] = tu[4]
	}

	if tg, err := strconv.Atoi(tu[1]); err == nil && tg > 0 {
		call.Talkgroup = uint(tg)
		if src, err := strconv.Atoi(tu[2]); err == nil && src > 0 {
			call.Source = uint(src)
			if len(tu[5]) > 0 && strings.TrimSpace(tu[5]) != strings.TrimSpace(tu[2]) {
				if b := regexp.MustCompile(`^([\.\-\ ,_]+)$`).MatchString(tu[5]); !b {
					units := NewUnits()
					units.Add(uint(src), tu[5])
					call.units = units
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
		if i, err = strconv.Atoi(s[1][0:1]); err != nil {
			return err
		}
		if i > 0 {
			call.Source = uint(i)

			if len(s) >= 3 && len(s[2]) > 0 {
				if call.units == nil {
					call.units = NewUnits()
				}
				switch units := call.units.(type) {
				case *Units:
					units.Add(uint(i), s[2])
				}
			}
		}
	}

	s = regexp.MustCompile(`Date:([^;]+);`).FindStringSubmatch(m.Comment())
	if len(s) == 2 {
		if t, err = time.ParseInLocation("2006-01-02 15:04:05.999", s[1], time.Now().Location()); err != nil {
			return err
		}
		call.DateTime = t.UTC()
	}

	s = regexp.MustCompile(`Frequency:([0-9]+);`).FindStringSubmatch(m.Comment())
	if len(s) == 2 && len(s[1]) > 0 {
		if i, err = strconv.Atoi(s[1]); err != nil {
			return err
		}
		if i > 0 {
			call.Frequency = uint(i)
		}
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

	s = regexp.MustCompile(`([0-9]+)`).FindStringSubmatch(m.Title())
	if len(s) > 1 && len(s[1]) > 0 {
		if i, err = strconv.Atoi(s[1]); err != nil {
			return err
		}
		if i > 0 {
			call.Talkgroup = uint(i)
		}
	}

	s = regexp.MustCompile(`(\[.+\])`).FindStringSubmatch(m.Title())
	if len(s) > 1 && len(s[1]) > 0 {
		var f any
		if err = json.Unmarshal([]byte(s[1]), &f); err == nil {
			switch v := f.(type) {
			case []any:
				patches := []uint{}
				for _, patch := range v {
					switch v := patch.(type) {
					case float64:
						if v > 0 {
							patches = append(patches, uint(v))
						}
					}
				}
				if len(patches) > 0 {
					call.Patches = patches
				}
			}
		}
	}

	s = regexp.MustCompile(`"([^"]+)"`).FindStringSubmatch(m.Title())
	if len(s) > 1 {
		call.talkgroupLabel = s[1]
		call.talkgroupName = s[1]
	}

	return nil
}

func ParseMultipartContent(call *Call, p *multipart.Part, b []byte) {
	switch p.FormName() {
	case "audio":
		call.Audio = b
		call.AudioName = p.FileName()

	case "audioName":
		call.AudioName = string(b)
		call.AudioType = mime.TypeByExtension(path.Ext(string(b)))

	case "dateTime":
		if regexp.MustCompile(`^[0-9]+$`).Match(b) {
			if i, err := strconv.Atoi(string(b)); err == nil {
				call.DateTime = time.Unix(int64(i), 0).UTC()
			}
		} else {
			call.DateTime, _ = time.Parse(time.RFC3339, string(b))
			call.DateTime = call.DateTime.UTC()
		}

	case "frequencies":
		var f any
		if err := json.Unmarshal(b, &f); err == nil {
			switch v := f.(type) {
			case []any:
				var frequencies = []map[string]any{}
				for _, f := range v {
					freq := map[string]any{}
					switch v := f.(type) {
					case map[string]any:
						switch v := v["errorCount"].(type) {
						case float64:
							if v >= 0 {
								freq["errorCount"] = uint(v)
							}
						}
						switch v := v["freq"].(type) {
						case float64:
							if v > 0 {
								freq["freq"] = uint(v)
							}
						}
						switch v := v["len"].(type) {
						case float64:
							if v >= 0 {
								freq["len"] = uint(v)
							}
						}
						switch v := v["pos"].(type) {
						case float64:
							if v >= 0 {
								freq["pos"] = uint(v)
							}
						}
						switch v := v["spikeCount"].(type) {
						case float64:
							if v >= 0 {
								freq["spikeCount"] = uint(v)
							}
						}
					}
					frequencies = append(frequencies, freq)
				}
				call.Frequencies = frequencies
			}
		}

	case "frequency":
		if i, err := strconv.Atoi(string(b)); err == nil && i > 0 {
			call.Frequency = uint(i)
		}

	case "patches", "patched_talkgroups":
		var (
			f       any
			patches = []uint{}
		)
		if err := json.Unmarshal(b, &f); err == nil {
			switch v := f.(type) {
			case []any:
				for _, patch := range v {
					switch v := patch.(type) {
					case float64:
						if v > 0 {
							patches = append(patches, uint(v))
						}
					}
				}
			}
			call.Patches = patches
		}

	case "source":
		if i, err := strconv.Atoi(string(b)); err == nil {
			call.Source = int(i)
		}

	case "sources":
		var (
			f     any
			units *Units
		)
		if err := json.Unmarshal(b, &f); err == nil {
			switch v := f.(type) {
			case []any:
				var sources = []map[string]any{}
				for _, f := range v {
					src := map[string]any{}
					switch v := f.(type) {
					case map[string]any:
						switch v := v["pos"].(type) {
						case float64:
							if v >= 0 {
								src["pos"] = uint(v)
							}
						}
						switch s := v["src"].(type) {
						case float64:
							if s > 0 {
								src["src"] = uint(s)
								switch t := v["tag"].(type) {
								case string:
									if units == nil {
										units = NewUnits()
									}
									switch units := call.units.(type) {
									case *Units:
										units.Add(uint(s), t)
									}
								}
							}
						}
					}
					sources = append(sources, src)
				}
				call.Sources = sources
				call.units = units
			}
		}

	case "system", "systemId":
		if i, err := strconv.Atoi(string(b)); err == nil && i > 0 {
			call.System = uint(i)
		}

	case "systemLabel":
		call.systemLabel = string(b)

	case "talkgroup", "talkgroupId":
		if i, err := strconv.Atoi(string(b)); err == nil && i > 0 {
			call.Talkgroup = uint(i)
		}

	case "talkgroupGroup":
		if s := string(b); len(s) > 0 && s != "-" {
			call.talkgroupGroup = s
		}

	case "talkgroupLabel":
		if s := string(b); len(s) > 0 && s != "-" {
			call.talkgroupLabel = s
		}

	case "talkgroupName":
		if s := string(b); len(s) > 0 && s != "-" {
			call.talkgroupName = s
		}

	case "talkgroupTag":
		if s := string(b); len(s) > 0 && s != "-" {
			call.talkgroupTag = s
		}
	}
}

func ParseTrunkRecorderMeta(call *Call, b []byte) error {
	m := map[string]any{}

	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}

	switch v := m["freq"].(type) {
	case float64:
		if v > 0 {
			call.Frequency = uint(v)
		}
	}

	switch v := m["freqList"].(type) {
	case []any:
		freqs := []map[string]any{}
		for _, f := range v {
			freq := map[string]any{}
			switch v := f.(type) {
			case map[string]any:
				switch v := v["error_count"].(type) {
				case float64:
					if v >= 0 {
						freq["errorCount"] = uint(v)
					}
				}

				switch v := v["freq"].(type) {
				case float64:
					if v > 0 {
						freq["freq"] = uint(v)
					}
				}

				switch v := v["len"].(type) {
				case float64:
					if v >= 0 {
						freq["len"] = uint(v)
					}
				}

				switch v := v["pos"].(type) {
				case float64:
					if v >= 0 {
						freq["pos"] = uint(v)
					}
				}

				switch v := v["spike_count"].(type) {
				case float64:
					if v >= 0 {
						freq["spikeCount"] = uint(v)
					}
				}

				freqs = append(freqs, freq)
			}
		}
		call.Frequencies = freqs
	}

	switch v := m["patched_talkgroups"].(type) {
	case []any:
		patches := []uint{}
		for _, f := range v {
			switch v := f.(type) {
			case float64:
				if v > 0 {
					patches = append(patches, uint(v))
				}
			}
		}
		if len(patches) > 0 {
			call.Patches = patches
		}
	}

	switch v := m["srcList"].(type) {
	case []any:
		sources := []map[string]any{}
		for _, f := range v {
			source := map[string]any{}
			switch v := f.(type) {
			case map[string]any:
				switch v := v["pos"].(type) {
				case float64:
					if v >= 0 {
						source["pos"] = uint(v)
					}
				}
				switch s := v["src"].(type) {
				case float64:
					if s > 0 {
						source["src"] = uint(s)
						switch t := v["tag"].(type) {
						case string:
							if len(t) > 0 {
								if call.units == nil {
									call.units = NewUnits()
								}
								switch v := call.units.(type) {
								case *Units:
									v.Add(uint(s), t)
								}
							}
						}
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
		if v > 0 {
			call.Talkgroup = uint(v)
		}
	}

	switch v := m["talkgroup_tag"].(type) {
	case string:
		if len(v) > 0 && v != "-" {
			call.talkgroupLabel = v
		}
	}

	return nil
}
