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
	"errors"
	"fmt"
	"io/fs"
	"math"
	"mime"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	DirwatchTypeDefault       = "default"
	DirwatchTypeDSDPlus       = "dsdplus"
	DirwatchTypeSdrTrunk      = "sdr-trunk"
	DirwatchTypeTrunkRecorder = "trunk-recorder"
)

type Dirwatch struct {
	Id          uint64
	Delay       uint
	DeleteAfter bool
	Directory   string
	Disabled    bool
	Extension   string
	Frequency   uint
	Kind        string
	Mask        string
	Order       uint
	SiteId      uint64
	SystemId    uint64
	TalkgroupId uint64
	controller  *Controller
	dirs        map[string]bool
	mutex       sync.Mutex
	timers      map[string]*time.Timer
	watcher     *fsnotify.Watcher
}

func NewDirwatch() *Dirwatch {
	return &Dirwatch{
		Delay:       defaults.dirwatch.delay,
		DeleteAfter: defaults.dirwatch.deleteAfter,
		Kind:        defaults.dirwatch.kind,
		dirs:        map[string]bool{},
		mutex:       sync.Mutex{},
		timers:      map[string]*time.Timer{},
	}
}

func (dirwatch *Dirwatch) FromMap(m map[string]any) *Dirwatch {
	switch v := m["id"].(type) {
	case float64:
		dirwatch.Id = uint64(v)
	}

	switch v := m["delay"].(type) {
	case float64:
		dirwatch.Delay = uint(v)
	}

	switch v := m["deleteAfter"].(type) {
	case bool:
		dirwatch.DeleteAfter = v
	}

	switch v := m["directory"].(type) {
	case string:
		dirwatch.Directory = v
	}

	switch v := m["disabled"].(type) {
	case bool:
		dirwatch.Disabled = v
	}

	switch v := m["extension"].(type) {
	case string:
		dirwatch.Extension = v
	}

	switch v := m["frequency"].(type) {
	case float64:
		dirwatch.Frequency = uint(v)
	}

	switch v := m["type"].(type) {
	case string:
		dirwatch.Kind = v
	}

	switch v := m["mask"].(type) {
	case string:
		dirwatch.Mask = v
	}

	switch v := m["order"].(type) {
	case float64:
		dirwatch.Order = uint(v)
	}

	switch v := m["siteId"].(type) {
	case float64:
		dirwatch.SiteId = uint64(v)
	}

	switch v := m["systemId"].(type) {
	case float64:
		dirwatch.SystemId = uint64(v)
	}

	switch v := m["talkgroupId"].(type) {
	case float64:
		dirwatch.TalkgroupId = uint64(v)
	}

	return dirwatch
}

func (dirwatch *Dirwatch) Ingest(p string) {
	var err error

	switch dirwatch.Kind {
	case DirwatchTypeDSDPlus:
		err = dirwatch.ingestDSDPlus(p)
	case DirwatchTypeTrunkRecorder:
		err = dirwatch.ingestTrunkRecorder(p)
	case DirwatchTypeSdrTrunk:
		err = dirwatch.ingestSdrTrunk(p)
	default:
		err = dirwatch.ingestDefault(p)
	}

	if err != nil {
		dirwatch.controller.Logs.LogEvent(LogLevelWarn, fmt.Sprintf("dirwatch.ingest: %s, %s", err.Error(), p))
	}
}

func (dirwatch *Dirwatch) ingestDefault(p string) error {
	var (
		err error
		ext string
	)

	if len(dirwatch.Extension) > 0 {
		ext = fmt.Sprintf(".%s", dirwatch.Extension)
	} else {
		ext = ".wav"
	}

	if strings.EqualFold(path.Ext(p), ext) {
		call := NewCall()

		call.AudioFilename = filepath.Base(p)
		call.AudioMime = mime.TypeByExtension(path.Ext(p))
		call.Timestamp = time.Now().UTC()

		if dirwatch.Frequency > 0 {
			call.Frequencies = append(call.Frequencies, CallFrequency{
				Frequency: dirwatch.Frequency,
				Offset:    0,
			})
		}

		if call.Audio, err = os.ReadFile(p); err != nil {
			return err
		}

		dirwatch.parseMask(call)

		if dirwatch.SystemId > 0 {
			call.Meta.SystemId = dirwatch.SystemId
		}

		if dirwatch.TalkgroupId > 0 {
			call.Meta.TalkgroupId = dirwatch.TalkgroupId
		}

		if ok, err := call.IsValid(); ok {
			dirwatch.controller.Ingest <- call

			if dirwatch.DeleteAfter {
				if err = os.Remove(p); err != nil {
					return err
				}
			}

		} else {
			return err
		}
	}

	return err
}

func (dirwatch *Dirwatch) ingestDSDPlus(p string) error {
	var (
		err error
		ext string
	)

	if len(dirwatch.Extension) > 0 {
		ext = fmt.Sprintf(".%s", dirwatch.Extension)
	} else {
		ext = ".mp3"
	}

	if !strings.EqualFold(path.Ext(p), ext) {
		return nil
	}

	call := NewCall()

	call.AudioFilename = filepath.Base(p)
	call.AudioMime = mime.TypeByExtension(path.Ext(p))

	if dirwatch.Frequency > 0 {
		call.Frequencies = append(call.Frequencies, CallFrequency{
			Frequency: dirwatch.Frequency,
			Offset:    0,
		})
	}

	if dirwatch.SiteId > 0 {
		call.Meta.SiteId = dirwatch.SiteId
	}

	if dirwatch.SystemId > 0 {
		call.Meta.SystemId = dirwatch.SystemId
	}

	if dirwatch.TalkgroupId > 0 {
		call.Meta.TalkgroupId = dirwatch.TalkgroupId
	}

	if call.Audio, err = os.ReadFile(p); err != nil {
		return err
	}

	if err = ParseDSDPlusMeta(call, p); err != nil {
		return err
	}

	if ok, err := call.IsValid(); ok {
		dirwatch.controller.Ingest <- call

		if dirwatch.DeleteAfter {
			if err = os.Remove(p); err != nil {
				return err
			}
		}

	} else {
		return err
	}

	return nil
}

func (dirwatch *Dirwatch) ingestSdrTrunk(p string) error {
	var err error

	if !strings.EqualFold(path.Ext(p), ".mp3") {
		return nil
	}

	call := NewCall()

	call.AudioFilename = filepath.Base(p)
	call.AudioMime = mime.TypeByExtension(path.Ext(p))

	if dirwatch.Frequency > 0 {
		call.Frequencies = append(call.Frequencies, CallFrequency{
			Frequency: dirwatch.Frequency,
			Offset:    0,
		})
	}

	if call.Audio, err = os.ReadFile(p); err != nil {
		return err
	}

	if err = ParseSdrTrunkMeta(call, dirwatch.controller); err != nil {
		return err
	}

	if ok, err := call.IsValid(); ok {
		dirwatch.controller.Ingest <- call

		if dirwatch.DeleteAfter {
			if err = os.Remove(p); err != nil {
				return err
			}
		}

	} else {
		return err
	}

	return nil
}

func (dirwatch *Dirwatch) ingestTrunkRecorder(p string) error {
	var (
		b   []byte
		err error
		ext string
	)

	if !strings.EqualFold(path.Ext(p), ".json") {
		return nil
	}

	if len(dirwatch.Extension) > 0 {
		ext = fmt.Sprintf(".%s", dirwatch.Extension)
	} else {
		ext = ".wav"
	}

	base := strings.TrimSuffix(p, ".json")

	audioName := base + ext

	call := NewCall()

	call.AudioFilename = filepath.Base(audioName)
	call.AudioMime = mime.TypeByExtension(path.Ext(audioName))

	if dirwatch.Frequency > 0 {
		call.Frequencies = append(call.Frequencies, CallFrequency{
			Frequency: dirwatch.Frequency,
			Offset:    0,
		})
	}

	if call.Audio, err = os.ReadFile(audioName); err != nil {
		return nil
	}

	if b, err = os.ReadFile(p); err != nil {
		return err
	}

	if err = ParseTrunkRecorderMeta(call, b); err != nil {
		return err
	}

	if ok, err := call.IsValid(); ok {
		dirwatch.controller.Ingest <- call

	} else {
		return err
	}

	if dirwatch.DeleteAfter {
		if err = os.Remove(p); err != nil {
			return err
		}
		if err = os.Remove(audioName); err != nil {
			return err
		}
	}

	return nil
}

func (dirwatch *Dirwatch) MarshalJSON() ([]byte, error) {
	m := map[string]any{
		"id":          dirwatch.Id,
		"delay":       dirwatch.Delay,
		"deleteAfter": dirwatch.DeleteAfter,
		"directory":   dirwatch.Directory,
		"disabled":    dirwatch.Disabled,
	}

	if len(dirwatch.Extension) > 0 {
		m["extension"] = dirwatch.Extension
	}

	if dirwatch.Frequency > 0 {
		m["frequency"] = dirwatch.Frequency
	}

	if len(dirwatch.Kind) > 0 {
		m["type"] = dirwatch.Kind
	}

	if len(dirwatch.Mask) > 0 {
		m["mask"] = dirwatch.Mask
	}

	if dirwatch.Order > 0 {
		m["order"] = dirwatch.Order
	}

	if dirwatch.SiteId > 0 {
		m["siteId"] = dirwatch.SiteId
	}

	if dirwatch.SystemId > 0 {
		m["systemId"] = dirwatch.SystemId
	}

	if dirwatch.TalkgroupId > 0 {
		m["talkgroupId"] = dirwatch.TalkgroupId
	}

	return json.Marshal(m)
}

func (dirwatch *Dirwatch) parseMask(call *Call) {
	var meta = [][]string{
		{"date", "#DATE", `\d{4}[-_]{0,1}\d{2}[-_]{0,1}\d{2}`},
		{"group", "#GROUP", `[a-zA-Z0-9\.\ -]+`},
		{"hz", "#HZ", `\d+`},
		{"khz", "#KHZ", `[\d\.]+`},
		{"mhz", "#MHZ", `[\d\.]+`},
		{"sitelbl", "#SITELBL", `[a-zA-Z0-9,\.\ -]+`},
		{"site", "#SITE", `\d+`},
		{"syslbl", "#SYSLBL", `[a-zA-Z0-9,\.\ -]+`},
		{"sys", "#SYS", `\d+`},
		{"tag", "#TAG", `[a-zA-Z0-9\.\ -]+`},
		{"tgafs", "#TGAFS", `\d{2}-\d{3}`},
		{"tghz", "#TGHZ", `\d+`},
		{"tgkhz", "#TGKHZ", `[\d\.]+`},
		{"tglbl", "#TGLBL", `[a-zA-Z0-9,\.\ -]+`},
		{"tgmhz", "#TGMHZ", `[\d\.]+`},
		{"tg", "#TG", `\d+`},
		{"time", "#TIME", `\d{2}[-:]{0,1}\d{2}[-:]{0,1}\d{2}`},
		{"unit", "#UNIT", `\d+`},
		{"unitbl", "#UNITLBL", `[a-zA-Z0-9,\.\ -]+`},
		{"ztime", "#ZTIME", `\d{2}[-:]{0,1}\d{2}[-:]{0,1}\d{2}`},
	}

	var (
		mask    string
		metapos = [][]any{}
		metaval = map[string]any{}
	)

	if len(dirwatch.Mask) > 0 {
		mask = dirwatch.Mask
	} else {
		return
	}

	for _, v := range meta {
		if i := strings.Index(mask, v[1]); i != -1 {
			metapos = append(metapos, []any{v[0], i})
			mask = strings.Replace(mask, v[1], fmt.Sprintf("(%v)", v[2]), 1)
		}
	}

	sort.Slice(metapos, func(i int, j int) bool {
		return metapos[i][1].(int) < metapos[j][1].(int)
	})

	base := strings.TrimSuffix(call.AudioFilename, path.Ext(call.AudioFilename))
	for i, s := range regexp.MustCompile(mask).FindStringSubmatch(base) {
		if i > 0 {
			v := fmt.Sprintf("%v", metapos[i-1][0])
			metaval[v] = s
		}
	}

	switch vDate := metaval["date"].(type) {
	case string:
		vDate = regexp.MustCompile(`(\d{4})(\d{2})(\d{2})`).ReplaceAllString(vDate, "$1-$2-$3")
		switch vTime := metaval["time"].(type) {
		case string:
			vTime = regexp.MustCompile(`(\d{2})[^\d]*(\d{2})[^\d]*(\d{2})`).ReplaceAllString(vTime, "$1:$2:$3")
			if dateTime, err := time.ParseInLocation("2006-01-02T15:04:05", fmt.Sprintf("%vT%v", vDate, vTime), time.Now().Location()); err == nil {
				call.Timestamp = dateTime.UTC()
			}
		default:
			switch vZtime := metaval["ztime"].(type) {
			case string:
				vZtime = regexp.MustCompile(`(\d{2})[^\d]*(\d{2})[^\d]*(\d{2})`).ReplaceAllString(vZtime, "$1:$2:$3")
				if dateTime, err := time.Parse("2006-01-02T15:04:05", fmt.Sprintf("%vT%v", vDate, vZtime)); err == nil {
					call.Timestamp = dateTime.UTC()
				}
			default:
				vDate = regexp.MustCompile(`[^\d]`).ReplaceAllString(vDate, "")
				if sec, err := strconv.Atoi(vDate); err == nil {
					call.Timestamp = time.Unix(int64(sec), 0).UTC()
				}
			}
		}
	}

	switch v := metaval["group"].(type) {
	case string:
		if len(v) > 0 && v != "-" {
			call.Meta.TalkgroupGroups = []string{v}
		}
	}

	switch v := metaval["hz"].(type) {
	case string:
		if hz, err := strconv.ParseFloat(v, 32); err == nil {
			call.Frequencies = append(call.Frequencies, CallFrequency{
				Frequency: uint(hz),
				Offset:    0,
			})
		}
	default:
		switch v := metaval["khz"].(type) {
		case string:
			if khz, err := strconv.ParseFloat(v, 32); err == nil {
				call.Frequencies = append(call.Frequencies, CallFrequency{
					Frequency: uint(khz * 1e3),
					Offset:    0,
				})
			}
		default:
			switch v := metaval["mhz"].(type) {
			case string:
				if mhz, err := strconv.ParseFloat(v, 32); err == nil {
					call.Frequencies = append(call.Frequencies, CallFrequency{
						Frequency: uint(mhz * 1e6),
						Offset:    0,
					})
				}
			}
		}
	}

	switch v := metaval["sys"].(type) {
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			call.Meta.SystemRef = uint(i)
		}
	default:
		switch v := metaval["syslbl"].(type) {
		case string:
			if system, ok := dirwatch.controller.Systems.GetSystemByLabel(v); ok {
				call.System = system
			} else {
				call.Meta.SystemRef = dirwatch.controller.Systems.GetNewSystemRef()
				call.Meta.SystemLabel = v
			}
		}
	}

	switch v := metaval["site"].(type) {
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			if call.System != nil {
				if site, ok := call.System.Sites.GetSiteByRef(uint(i)); ok {
					call.SiteRef = site.SiteRef
				}
			} else {
				call.Meta.SiteRef = uint(i)
			}
		}
	default:
		switch v := metaval["sitelbl"].(type) {
		case string:
			if call.System != nil {
				if site, ok := call.System.Sites.GetSiteByLabel(v); ok {
					call.SiteRef = site.SiteRef
				}
			}
		}
	}

	switch v := metaval["tag"].(type) {
	case string:
		if len(v) > 0 && v != "-" {
			call.Meta.TalkgroupTag = v
		}
	}

	switch v := metaval["tg"].(type) {
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			call.Meta.TalkgroupRef = uint(i)
		}
	default:
		switch v := metaval["tgafs"].(type) {
		case string:
			if len(v) == 6 && v[2] == '-' {
				if a, err := strconv.Atoi(v[:2]); err == nil {
					if b, err := strconv.Atoi(v[3:5]); err == nil {
						if c, err := strconv.Atoi(v[5:]); err == nil {
							call.Meta.TalkgroupRef = uint(a<<7 | b<<3 | c)
						}
					}
				}
			}
		default:
			switch v := metaval["tghz"].(type) {
			case string:
				if hz, err := strconv.ParseFloat(v, 32); err == nil {
					call.Frequencies = append(call.Frequencies, CallFrequency{
						Frequency: uint(hz),
						Offset:    0,
					})
					call.Meta.TalkgroupRef = uint(hz / 1e3)
				}
			default:
				switch v := metaval["tgkhz"].(type) {
				case string:
					if khz, err := strconv.ParseFloat(v, 32); err == nil {
						call.Frequencies = append(call.Frequencies, CallFrequency{
							Frequency: uint(khz * 1e3),
							Offset:    0,
						})
						call.Meta.TalkgroupRef = uint(khz)
					}
				default:
					switch v := metaval["tgmhz"].(type) {
					case string:
						if mhz, err := strconv.ParseFloat(v, 32); err == nil {
							call.Frequencies = append(call.Frequencies, CallFrequency{
								Frequency: uint(mhz * 1e6),
								Offset:    0,
							})
							call.Meta.TalkgroupRef = uint(mhz * 1e3)
						}
					}
				}
			}
		}
	}

	switch v := metaval["tglbl"].(type) {
	case string:
		if len(v) > 0 {
			call.Meta.TalkgroupLabel = v
		}
	}

	switch v := metaval["unit"].(type) {
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			call.Units = append(call.Units, CallUnit{
				UnitRef: uint(i),
				Offset:  0,
			})
		}
	}

	switch v := metaval["unitlbl"].(type) {
	case string:
		if len(v) > 0 && len(call.Units) > 0 {
			call.Meta.UnitLabels = []string{v}
			call.Meta.UnitRefs = []uint{call.Units[0].UnitRef}
		}
	}
}

func (dirwatch *Dirwatch) Start(controller *Controller) error {
	var (
		delay = time.Duration(math.Max(float64(dirwatch.Delay), 2000)) * time.Millisecond
		err   error
	)

	if dirwatch.Disabled {
		return nil
	}

	if dirwatch.watcher != nil {
		return errors.New("dirwatch.start: already started")
	}

	dirwatch.controller = controller
	dirwatch.dirs = map[string]bool{}

	if dirwatch.watcher, err = fsnotify.NewWatcher(); err != nil {
		return err
	}

	go func() {
		logError := func(err error) {
			controller.Logs.LogEvent(LogLevelError, fmt.Sprintf("dirwatch.watcher: %v", err.Error()))
		}

		newTimer := func(eventName string) *time.Timer {
			return time.AfterFunc(delay, func() {
				dirwatch.mutex.Lock()
				defer dirwatch.mutex.Unlock()

				delete(dirwatch.timers, eventName)

				if _, err := os.Stat(eventName); err == nil {
					dirwatch.Ingest(eventName)
				}
			})
		}

		defer func() {
			switch v := recover().(type) {
			case error:
				controller.Logs.LogEvent(LogLevelError, v.Error())
			}

			dirwatch.mutex.Lock()
			defer dirwatch.mutex.Unlock()

			for e, t := range dirwatch.timers {
				t.Stop()
				delete(dirwatch.timers, e)
			}

			if dirwatch.watcher != nil {
				dirwatch.Start(controller)
			}
		}()

		for {
			if dirwatch.watcher == nil {
				return
			}

			select {
			case event, ok := <-dirwatch.watcher.Events:
				if !ok {
					return
				}

				switch event.Op {
				case fsnotify.Create:
					if dirwatch.isDir(event.Name) {
						if err := dirwatch.walkDir(event.Name); err != nil {
							logError(err)
						}

					} else {
						dirwatch.mutex.Lock()
						if dirwatch.timers[event.Name] != nil {
							dirwatch.timers[event.Name].Stop()
						}
						dirwatch.timers[event.Name] = newTimer(event.Name)
						dirwatch.mutex.Unlock()
					}

				case fsnotify.Remove:
					if dirwatch.dirs[event.Name] {
						if err := dirwatch.watcher.Remove(event.Name); err == nil {
							delete(dirwatch.dirs, event.Name)
						} else {
							logError(err)
						}
					}

				case fsnotify.Write:
					dirwatch.mutex.Lock()
					if dirwatch.timers[event.Name] != nil {
						dirwatch.timers[event.Name].Stop()
					}
					dirwatch.timers[event.Name] = newTimer(event.Name)
					dirwatch.mutex.Unlock()
				}

			case err, ok := <-dirwatch.watcher.Errors:
				if ok {
					logError(err)
				}

				return
			}
		}
	}()

	go func() {
		defer func() {
			switch v := recover().(type) {
			case error:
				controller.Logs.LogEvent(LogLevelError, v.Error())
			}
		}()

		time.Sleep(delay)

		if err := fs.WalkDir(os.DirFS(dirwatch.Directory), ".", func(p string, _ fs.DirEntry, err error) error {
			fp := filepath.Join(dirwatch.Directory, p)

			if dirwatch.isDir(fp) {
				dirwatch.dirs[fp] = true
				dirwatch.watcher.Add(fp)

			} else if dirwatch.DeleteAfter {
				dirwatch.Ingest(fp)
			}

			return err
		}); err != nil {
			controller.Logs.LogEvent(LogLevelError, fmt.Sprintf("dirwatch.walkdir: %s", err.Error()))
		}
	}()

	return nil
}

func (dirwatch *Dirwatch) Stop() {
	if dirwatch.watcher != nil {
		w := dirwatch.watcher
		dirwatch.watcher = nil
		w.Close()
	}
}

func (dirwatch *Dirwatch) isDir(d string) bool {
	if fi, err := os.Stat(d); err == nil {
		if fi.IsDir() {
			return true
		}
	}

	return false
}

func (dirwatch *Dirwatch) walkDir(d string) error {
	dfs := os.DirFS(d)

	return fs.WalkDir(dfs, ".", func(p string, _ fs.DirEntry, err error) error {
		fp := filepath.Join(d, p)
		if dirwatch.isDir(fp) {
			if !dirwatch.dirs[fp] {
				dirwatch.dirs[fp] = true
				dirwatch.watcher.Add(fp)
			}
		}
		return err
	})
}

type Dirwatches struct {
	List  []*Dirwatch
	mutex sync.Mutex
}

func NewDirwatches() *Dirwatches {
	return &Dirwatches{
		List:  []*Dirwatch{},
		mutex: sync.Mutex{},
	}
}

func (dirwatches *Dirwatches) FromMap(f []any) *Dirwatches {
	dirwatches.mutex.Lock()
	defer dirwatches.mutex.Unlock()

	dirwatches.Stop()

	dirwatches.List = []*Dirwatch{}

	for _, f := range f {
		switch v := f.(type) {
		case map[string]any:
			dirwatch := NewDirwatch().FromMap(v)
			dirwatches.List = append(dirwatches.List, dirwatch)
		}
	}

	return dirwatches
}

func (dirwatches *Dirwatches) Read(db *Database) error {
	var (
		err   error
		query string
		rows  *sql.Rows
	)

	dirwatches.mutex.Lock()
	defer dirwatches.mutex.Unlock()

	dirwatches.Stop()

	dirwatches.List = []*Dirwatch{}

	formatError := dirwatches.errorFormatter("read")

	query = `SELECT "dirwatchId", "delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "siteId", "systemId", "talkgroupId", "type" FROM "dirwatches"`
	if rows, err = db.Sql.Query(query); err != nil {
		return formatError(err, query)
	}

	for rows.Next() {
		dirwatch := NewDirwatch()

		if err = rows.Scan(&dirwatch.Id, &dirwatch.Delay, &dirwatch.DeleteAfter, &dirwatch.Directory, &dirwatch.Disabled, &dirwatch.Extension, &dirwatch.Frequency, &dirwatch.Mask, &dirwatch.Order, &dirwatch.SiteId, &dirwatch.SystemId, &dirwatch.TalkgroupId, &dirwatch.Kind); err != nil {
			break
		}

		dirwatches.List = append(dirwatches.List, dirwatch)
	}

	rows.Close()

	if err != nil {
		return formatError(err, "")
	}

	sort.Slice(dirwatches.List, func(i int, j int) bool {
		return dirwatches.List[i].Order < dirwatches.List[j].Order
	})

	return nil
}

func (dirwatches *Dirwatches) Start(controller *Controller) {
	for i := range dirwatches.List {
		if err := dirwatches.List[i].Start(controller); err != nil {
			controller.Logs.LogEvent(LogLevelError, fmt.Sprintf("dirwatches.start: %s", err.Error()))
		}
	}
}

func (dirwatches *Dirwatches) Stop() {
	for i := range dirwatches.List {
		dirwatches.List[i].Stop()
	}
	dirwatches.List = []*Dirwatch{}
}

func (dirwatches *Dirwatches) Write(db *Database) error {
	var (
		dirwatchIds = []uint64{}
		err         error
		query       string
		rows        *sql.Rows
		tx          *sql.Tx
	)

	dirwatches.mutex.Lock()
	defer dirwatches.mutex.Unlock()

	formatError := dirwatches.errorFormatter("write")

	if tx, err = db.Sql.Begin(); err != nil {
		return formatError(err, "")
	}

	query = `SELECT "dirwatchId" FROM "dirwatches"`
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	for rows.Next() {
		var dirwatchId uint64
		if err = rows.Scan(&dirwatchId); err != nil {
			break
		}
		remove := true
		for _, dirwatch := range dirwatches.List {
			if dirwatch.Id == 0 || dirwatch.Id == dirwatchId {
				remove = false
				break
			}
		}
		if remove {
			dirwatchIds = append(dirwatchIds, dirwatchId)
		}
	}

	rows.Close()

	if err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	if len(dirwatchIds) > 0 {
		if b, err := json.Marshal(dirwatchIds); err == nil {
			in := strings.ReplaceAll(strings.ReplaceAll(string(b), "[", "("), "]", ")")
			query = fmt.Sprintf(`DELETE FROM "dirwatches" WHERE "dirwatchId" IN %s`, in)
			if _, err = tx.Exec(query); err != nil {
				tx.Rollback()
				return formatError(err, query)
			}
		}
	}

	for _, dirwatch := range dirwatches.List {
		var count uint

		query = fmt.Sprintf(`SELECT COUNT(*) FROM "dirwatches" WHERE "dirwatchId" = %d`, dirwatch.Id)
		if err = tx.QueryRow(query).Scan(&count); err != nil {
			break
		}

		if count == 0 {
			query = fmt.Sprintf(`INSERT INTO "dirwatches" ("delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "siteId", "systemId", "talkgroupId", "type") VALUES (%d, %t, '%s', %t, '%s', %d, '%s', %d, %d, %d, %d, '%s')`, dirwatch.Delay, dirwatch.DeleteAfter, dirwatch.Directory, dirwatch.Disabled, dirwatch.Extension, dirwatch.Frequency, dirwatch.Mask, dirwatch.Order, dirwatch.SiteId, dirwatch.SystemId, dirwatch.TalkgroupId, dirwatch.Kind)
			if _, err = tx.Exec(query); err != nil {
				break
			}

		} else {
			query = fmt.Sprintf(`UPDATE "dirwatches" SET "delay" = %d, "deleteAfter" = %t, "directory" = '%s', "disabled" = %t, "extension" = '%s', "frequency" = %d, "mask" = '%s', "order" = %d, "siteId" = %d, "systemId" = %d, "talkgroupId" = %d, "type" = '%s' WHERE "dirwatchId" = %d`, dirwatch.Delay, dirwatch.DeleteAfter, dirwatch.Directory, dirwatch.Disabled, dirwatch.Extension, dirwatch.Frequency, dirwatch.Mask, dirwatch.Order, dirwatch.SiteId, dirwatch.SystemId, dirwatch.TalkgroupId, dirwatch.Kind, dirwatch.Id)
			if _, err = tx.Exec(query); err != nil {
				break
			}
		}
	}

	if err != nil {
		tx.Rollback()
		return formatError(err, query)
	}

	if err = tx.Commit(); err != nil {
		tx.Rollback()
		return formatError(err, "")
	}

	return nil
}

func (dirwatches *Dirwatches) errorFormatter(label string) func(err error, query string) error {
	return func(err error, query string) error {
		s := fmt.Sprintf("dirwatches.%s: %s", label, err.Error())

		if len(query) > 0 {
			s = fmt.Sprintf("%s in %s", s, query)
		}

		return errors.New(s)
	}
}
