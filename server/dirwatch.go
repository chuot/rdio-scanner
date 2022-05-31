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
	"errors"
	"fmt"
	"io/fs"
	"io/ioutil"
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
	DirwatchKindDefault       = "default"
	DirwatchKindSdrTrunk      = "sdr-trunk"
	DirwatchKindTrunkRecorder = "trunk-recorder"
)

type Dirwatch struct {
	Id          interface{} `json:"_id"`
	Delay       interface{} `json:"delay"`
	DeleteAfter bool        `json:"deleteAfter"`
	Directory   string      `json:"directory"`
	Disabled    bool        `json:"disabled"`
	Extension   interface{} `json:"extension"`
	Frequency   interface{} `json:"frequency"`
	Mask        interface{} `json:"mask"`
	Order       interface{} `json:"order"`
	SystemId    interface{} `json:"systemId"`
	TalkgroupId interface{} `json:"talkgroupId"`
	Kind        interface{} `json:"type"`
	UsePolling  bool        `json:"usePolling"`
	controller  *Controller
	dirs        map[string]bool
	watcher     *fsnotify.Watcher
}

func (dirwatch *Dirwatch) FromMap(m map[string]interface{}) *Dirwatch {
	switch v := m["_id"].(type) {
	case float64:
		dirwatch.Id = uint(v)
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

	switch v := m["mask"].(type) {
	case string:
		dirwatch.Mask = v
	}

	switch v := m["order"].(type) {
	case float64:
		dirwatch.Order = uint(v)
	}

	switch v := m["systemId"].(type) {
	case float64:
		dirwatch.SystemId = uint(v)
	}

	switch v := m["talkgroupId"].(type) {
	case float64:
		dirwatch.TalkgroupId = uint(v)
	}

	switch v := m["type"].(type) {
	case string:
		dirwatch.Kind = v
	}

	switch v := m["usePolling"].(type) {
	case bool:
		dirwatch.UsePolling = v
	}

	return dirwatch
}

func (dirwatch *Dirwatch) Ingest(p string) {
	var err error

	switch dirwatch.Kind {
	case DirwatchKindTrunkRecorder:
		err = dirwatch.ingestTrunkRecorder(p)
	case DirwatchKindSdrTrunk:
		err = dirwatch.ingestSdrTrunk(p)
	default:
		err = dirwatch.ingestDefault(p)
	}

	if err != nil {
		dirwatch.controller.Logs.LogEvent(LogLevelError, fmt.Sprintf("dirwatch.ingest: %v", err.Error()))
	}
}

func (dirwatch *Dirwatch) ingestDefault(p string) error {
	var (
		err error
		ext string
	)

	switch v := dirwatch.Extension.(type) {
	case string:
		ext = fmt.Sprintf(".%s", v)
	default:
		ext = ".wav"
	}

	if strings.EqualFold(path.Ext(p), ext) {
		call := NewCall()

		call.AudioName = path.Base(p)
		call.AudioType = mime.TypeByExtension(path.Ext(p))
		call.Frequency = dirwatch.Frequency

		if call.Audio, err = os.ReadFile(p); err != nil {
			return err
		}

		dirwatch.parseMask(call)

		switch v := dirwatch.SystemId.(type) {
		case uint:
			call.System = v
		}

		switch v := dirwatch.TalkgroupId.(type) {
		case uint:
			call.Talkgroup = v
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

func (dirwatch *Dirwatch) ingestSdrTrunk(p string) error {
	var (
		err error
		ext string
	)

	switch v := dirwatch.Extension.(type) {
	case string:
		if len(v) > 0 {
			ext = fmt.Sprintf(".%s", v)
		} else {
			ext = ".mp3"
		}
	default:
		ext = ".mp3"
	}

	if !strings.EqualFold(path.Ext(p), ext) {
		return nil
	}

	call := NewCall()

	call.AudioName = path.Base(p)
	call.AudioType = mime.TypeByExtension(path.Ext(p))
	call.Frequency = dirwatch.Frequency

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

	switch v := dirwatch.Extension.(type) {
	case string:
		if len(v) > 0 {
			ext = fmt.Sprintf(".%s", v)
		} else {
			ext = ".wav"
		}
	default:
		ext = ".wav"
	}

	base := strings.TrimSuffix(p, ".json")

	audioName := base + ext

	call := NewCall()

	call.AudioName = path.Base(audioName)
	call.AudioType = mime.TypeByExtension(path.Ext(audioName))
	call.Frequency = dirwatch.Frequency

	switch v := dirwatch.SystemId.(type) {
	case uint:
		call.System = v
	}

	if call.Audio, err = ioutil.ReadFile(audioName); err != nil {
		return nil
	}

	if b, err = ioutil.ReadFile(p); err != nil {
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

func (dirwatch *Dirwatch) parseMask(call *Call) {
	var meta = [][]string{
		{"date", "#DATE", `[\d-_]+`},
		{"group", "#GROUP", `[a-zA-Z0-9\.\ -]+`},
		{"hz", "#HZ", `\d+`},
		{"khz", "#KHZ", `[\d\.]+`},
		{"mhz", "#MHZ", `[\d\.]+`},
		{"syslbl", "#SYSLBL", `[a-zA-Z0-9,\.\ -]+`},
		{"sys", "#SYS", `\d+`},
		{"tag", "#TAG", `[a-zA-Z0-9\.\ -]+`},
		{"tgafs", "#TGAFS", `\d{2}-\d{3}`},
		{"tghz", "#TGHZ", `\d+`},
		{"tgkhz", "#TGKHZ", `[\d\.]+`},
		{"tglbl", "#TGLBL", `[a-zA-Z0-9,\.\ -]+`},
		{"tgmhz", "#TGMHZ", `[\d\.]+`},
		{"tg", "#TG", `\d+`},
		{"time", "#TIME", `[\d-:]+`},
		{"unit", "#UNIT", `\d+`},
		{"ztime", "#ZTIME", `[\d-:]+`},
	}

	var (
		filename string
		mask     string
		metapos  = [][]interface{}{}
		metaval  = map[string]interface{}{}
	)

	switch v := dirwatch.Mask.(type) {
	case string:
		mask = v
	default:
		return
	}

	switch v := call.AudioName.(type) {
	case string:
		filename = v
	default:
		return
	}

	for _, v := range meta {
		if i := strings.Index(mask, v[1]); i != -1 {
			metapos = append(metapos, []interface{}{v[0], i})
			mask = strings.Replace(mask, v[1], fmt.Sprintf("(%v)", v[2]), 1)
		}
	}

	sort.Slice(metapos, func(i int, j int) bool {
		return metapos[i][1].(int) < metapos[j][1].(int)
	})

	base := strings.TrimSuffix(filename, path.Ext(filename))
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
				call.DateTime = dateTime.UTC()
			}
		default:
			switch vZtime := metaval["ztime"].(type) {
			case string:
				vZtime = regexp.MustCompile(`(\d{2})[^\d]*(\d{2})[^\d]*(\d{2})`).ReplaceAllString(vZtime, "$1:$2:$3")
				if dateTime, err := time.Parse("2006-01-02T15:04:05", fmt.Sprintf("%vT%v", vDate, vZtime)); err == nil {
					call.DateTime = dateTime.UTC()
				}
			default:
				vDate = regexp.MustCompile(`[^\d]`).ReplaceAllString(vDate, "")
				if sec, err := strconv.Atoi(vDate); err == nil {
					call.DateTime = time.Unix(int64(sec), 0).UTC()
				}
			}
		}
	}

	switch v := metaval["group"].(type) {
	case string:
		if len(v) > 0 && v != "-" {
			call.talkgroupGroup = v
		}
	}

	switch v := metaval["hz"].(type) {
	case string:
		if hz, err := strconv.ParseFloat(v, 64); err == nil {
			call.Frequency = uint(hz)
		}
	default:
		switch v := metaval["khz"].(type) {
		case string:
			if khz, err := strconv.ParseFloat(v, 64); err == nil {
				call.Frequency = uint(khz * 1e3)
			}
		default:
			switch v := metaval["mhz"].(type) {
			case string:
				if mhz, err := strconv.ParseFloat(v, 64); err == nil {
					call.Frequency = uint(mhz * 1e6)
				}
			}
		}
	}

	switch v := metaval["sys"].(type) {
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			call.System = uint(i)
		}
	default:
		switch v := metaval["syslbl"].(type) {
		case string:
			if system, ok := dirwatch.controller.Systems.GetSystem(v); ok {
				call.System = system.Id
			} else {
				call.System = dirwatch.controller.Systems.GetNewSystemId()
				call.systemLabel = v
			}
		}
	}

	switch v := metaval["tag"].(type) {
	case string:
		if len(v) > 0 && v != "-" {
			call.talkgroupTag = v
		}
	}

	switch v := metaval["tg"].(type) {
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			call.Talkgroup = uint(i)
		}
	default:
		switch v := metaval["tgafs"].(type) {
		case string:
			if len(v) == 6 && v[2] == '-' {
				if a, err := strconv.Atoi(v[:2]); err == nil {
					if b, err := strconv.Atoi(v[3:5]); err == nil {
						if c, err := strconv.Atoi(v[5:]); err == nil {
							call.Talkgroup = uint(a<<7 | b<<3 | c)
						}
					}
				}
			}
		default:
			switch v := metaval["tghz"].(type) {
			case string:
				if hz, err := strconv.ParseFloat(v, 64); err == nil {
					call.Frequency = uint(hz)
					call.Talkgroup = uint(hz / 1e3)
				}
			default:
				switch v := metaval["tgkhz"].(type) {
				case string:
					if khz, err := strconv.ParseFloat(v, 64); err == nil {
						call.Frequency = uint(khz * 1e3)
						call.Talkgroup = uint(khz)
					}
				default:
					switch v := metaval["tgmhz"].(type) {
					case string:
						if mhz, err := strconv.ParseFloat(v, 64); err == nil {
							call.Frequency = uint(mhz * 1e6)
							call.Talkgroup = uint(mhz * 1e3)
						}
					}
				}
			}
		}
	}

	switch v := metaval["tglbl"].(type) {
	case string:
		if len(v) > 0 {
			call.talkgroupLabel = v
		}
	}

	switch v := metaval["unit"].(type) {
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			switch sources := call.Sources.(type) {
			case []map[string]interface{}:
				call.Sources = append(sources, map[string]interface{}{"pos": 0, "src": uint(i)})
			}
		}
	}
}

func (dirwatch *Dirwatch) Start(controller *Controller) error {
	var (
		delay time.Duration
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

	switch v := dirwatch.Delay.(type) {
	case uint:
		delay = time.Duration(math.Max(float64(v), 2000)) * time.Millisecond
	default:
		delay = time.Duration(2000) * time.Millisecond
	}

	go func() {
		// var timers = map[string]*time.Timer{}
		var timers = sync.Map{}

		logError := func(err error) {
			controller.Logs.LogEvent(LogLevelError, fmt.Sprintf("dirwatch.watcher: %v", err.Error()))
		}

		newTimer := func(eventName string) *time.Timer {
			return time.AfterFunc(delay, func() {
				timers.Delete(eventName)

				if _, err := os.Stat(eventName); err == nil {
					dirwatch.Ingest(eventName)
				}
			})
		}

		defer func() {
			timers.Range(func(k interface{}, t interface{}) bool {
				switch v := t.(type) {
				case *time.Timer:
					v.Stop()
				}

				timers.Delete(k)

				return true
			})

			switch v := recover().(type) {
			case error:
				controller.Logs.LogEvent(LogLevelError, v.Error())
			}
		}()

		for {
			if dirwatch.watcher == nil {
				break
			}

			select {
			case event, ok := <-dirwatch.watcher.Events:
				if ok {
					switch event.Op {
					case fsnotify.Create:
						if dirwatch.isDir(event.Name) {
							if err := dirwatch.walkDir(event.Name); err != nil {
								logError(err)
							}

						} else {
							if f, ok := timers.LoadAndDelete(event.Name); ok {
								switch v := f.(type) {
								case *time.Timer:
									v.Stop()
								}
							}
							timers.Store(event.Name, newTimer(event.Name))
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
						if f, ok := timers.LoadAndDelete(event.Name); ok {
							switch v := f.(type) {
							case *time.Timer:
								v.Stop()
							}
						}
						timers.Store(event.Name, newTimer(event.Name))
					}
				}

			case err, ok := <-dirwatch.watcher.Errors:
				if ok {
					logError(err)

					return
				}
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

		if err := fs.WalkDir(os.DirFS(dirwatch.Directory), ".", func(p string, d fs.DirEntry, err error) error {
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
		dirwatch.watcher.Close()
		dirwatch.watcher = nil
	}
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

func (dirwatches *Dirwatches) FromMap(f []interface{}) *Dirwatches {
	dirwatches.mutex.Lock()
	defer dirwatches.mutex.Unlock()

	dirwatches.Stop()

	dirwatches.List = []*Dirwatch{}

	for _, f := range f {
		switch v := f.(type) {
		case map[string]interface{}:
			dirwatch := &Dirwatch{}
			dirwatch.FromMap(v)
			dirwatches.List = append(dirwatches.List, dirwatch)
		}
	}

	return dirwatches
}

func (dirwatches *Dirwatches) Read(db *Database) error {
	var (
		delay       sql.NullFloat64
		err         error
		extension   sql.NullString
		id          sql.NullFloat64
		frequency   sql.NullFloat64
		kind        sql.NullString
		mask        sql.NullString
		order       sql.NullFloat64
		rows        *sql.Rows
		systemId    sql.NullFloat64
		talkgroupId sql.NullFloat64
	)

	dirwatches.mutex.Lock()
	defer dirwatches.mutex.Unlock()

	dirwatches.Stop()

	dirwatches.List = []*Dirwatch{}

	formatError := func(err error) error {
		return fmt.Errorf("dirwatches.read: %v", err)
	}

	if rows, err = db.Sql.Query("select `_id`, `delay`, `deleteAfter`, `directory`, `disabled`, `extension`, `frequency`, `mask`, `order`, `systemId`, `talkgroupId`, `type`, `usePolling` from `rdioScannerDirWatches`"); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		dirwatch := &Dirwatch{}

		if err = rows.Scan(&id, &delay, &dirwatch.DeleteAfter, &dirwatch.Directory, &dirwatch.Disabled, &extension, &frequency, &mask, &order, &systemId, &talkgroupId, &kind, &dirwatch.UsePolling); err != nil {
			break
		}

		if id.Valid && id.Float64 > 0 {
			dirwatch.Id = uint(id.Float64)
		}

		if delay.Valid && id.Float64 > 0 {
			dirwatch.Delay = uint(delay.Float64)
		}

		if extension.Valid && len(extension.String) > 0 {
			dirwatch.Extension = extension.String
		}

		if frequency.Valid && frequency.Float64 > 0 {
			dirwatch.Frequency = uint(frequency.Float64)
		}

		if mask.Valid && len(mask.String) > 0 {
			dirwatch.Mask = mask.String
		}

		if order.Valid && order.Float64 > 0 {
			dirwatch.Order = uint(order.Float64)
		}

		if systemId.Valid && systemId.Float64 > 0 {
			dirwatch.SystemId = uint(systemId.Float64)
		}

		if talkgroupId.Valid && talkgroupId.Float64 > 0 {
			dirwatch.TalkgroupId = uint(talkgroupId.Float64)
		}

		if kind.Valid && len(kind.String) > 0 {
			dirwatch.Kind = kind.String
		}

		dirwatches.List = append(dirwatches.List, dirwatch)
	}

	rows.Close()

	if err != nil {
		return formatError(err)
	}

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
		count  uint
		err    error
		rows   *sql.Rows
		rowIds = []uint{}
	)

	dirwatches.mutex.Lock()
	defer dirwatches.mutex.Unlock()

	formatError := func(err error) error {
		return fmt.Errorf("dirwatches.write: %v", err)
	}

	for _, dirwatch := range dirwatches.List {
		if err = db.Sql.QueryRow("select count(*) from `rdioScannerDirWatches` where `_id` = ?", dirwatch.Id).Scan(&count); err != nil {
			break
		}

		if count == 0 {
			if _, err = db.Sql.Exec("insert into `rdioScannerDirWatches` (`_id`, `delay`, `deleteAfter`, `directory`, `disabled`, `extension`, `frequency`, `mask`, `order`, `systemId`, `talkgroupId`, `type`, `usePolling`) values (?, ?, ?, ?, ?, ?, ?, ?, ? ,? ,? ,? ,?)", dirwatch.Id, dirwatch.Delay, dirwatch.DeleteAfter, dirwatch.Directory, dirwatch.Disabled, dirwatch.Extension, dirwatch.Frequency, dirwatch.Mask, dirwatch.Order, dirwatch.SystemId, dirwatch.TalkgroupId, dirwatch.Kind, dirwatch.UsePolling); err != nil {
				break
			}

		} else if _, err = db.Sql.Exec("update `rdioScannerDirWatches` set `_id` = ?, `delay` = ?, `deleteAfter` = ?, `directory` = ?, `disabled` = ?, `extension` = ?, `frequency` = ?, `mask` = ?, `order` = ?, `systemId` = ?, `talkgroupId` = ?, `type` = ?, `usePolling` = ? where `_id` = ?", dirwatch.Id, dirwatch.Delay, dirwatch.DeleteAfter, dirwatch.Directory, dirwatch.Disabled, dirwatch.Extension, dirwatch.Frequency, dirwatch.Mask, dirwatch.Order, dirwatch.SystemId, dirwatch.TalkgroupId, dirwatch.Kind, dirwatch.UsePolling, dirwatch.Id); err != nil {
			break
		}
	}

	if err != nil {
		return formatError(err)
	}

	if rows, err = db.Sql.Query("select `_id` from `rdioScannerDirWatches`"); err != nil {
		return formatError(err)
	}

	for rows.Next() {
		var rowId uint
		if err = rows.Scan(&rowId); err != nil {
			break
		}
		remove := true
		for _, dirwatch := range dirwatches.List {
			if dirwatch.Id == nil || dirwatch.Id == rowId {
				remove = false
				break
			}
		}
		if remove {
			rowIds = append(rowIds, rowId)
		}
	}

	rows.Close()

	if err != nil {
		return formatError(err)
	}

	if len(rowIds) > 0 {
		if b, err := json.Marshal(rowIds); err == nil {
			s := string(b)
			s = strings.ReplaceAll(s, "[", "(")
			s = strings.ReplaceAll(s, "]", ")")
			q := fmt.Sprintf("delete from `rdioScannerDirwatches` where `_id` in %v", s)
			if _, err = db.Sql.Exec(q); err != nil {
				return formatError(err)
			}
		}
	}

	return nil
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

	return fs.WalkDir(dfs, ".", func(p string, de fs.DirEntry, err error) error {
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
