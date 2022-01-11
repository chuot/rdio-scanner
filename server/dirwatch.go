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
	"mime"
	"os"
	"path"
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

func (dirwatch *Dirwatch) FromMap(m map[string]interface{}) {
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
		LogEvent(
			dirwatch.controller.Database,
			LogLevelError,
			fmt.Sprintf("dirwatch.ingest: %v", err.Error()),
		)
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

		if call.IsValid() {
			dirwatch.controller.Ingest <- call

			if dirwatch.DeleteAfter {
				if err = os.Remove(p); err != nil {
					return err
				}
			}
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

	if call.Audio, err = os.ReadFile(p); err != nil {
		fmt.Println("err1")
		return err
	}

	if err = ParseSdrTrunkMeta(call, dirwatch.controller); err != nil {
		fmt.Println("err2")
		return err
	}

	if call.IsValid() {
		dirwatch.controller.Ingest <- call

		if dirwatch.DeleteAfter {
			if err = os.Remove(p); err != nil {
				fmt.Println("err3")
				return err
			}
		}
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

	if call.IsValid() {
		dirwatch.controller.Ingest <- call
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
	var meta = map[string][]string{
		"date":      {"#DATE", `[\d-_]+`},
		"hz":        {"#HZ", `[\d]+`},
		"khz":       {"#KHZ", `[\d\.]+`},
		"mhz":       {"#MHZ", `[\d\.]+`},
		"time":      {"#TIME", `[\d-:]+`},
		"system":    {"#SYS", `\d+`},
		"talkgroup": {"#TG", `\d+`},
		"unit":      {"#UNIT", `\d+`},
		"ztime":     {"#ZTIME", `[\d-:]+`},
	}

	var (
		filename string
		mask     string
		maskre   string
		metapos  = [][]interface{}{}
		metaval  = map[string]interface{}{}
	)

	switch v := dirwatch.Mask.(type) {
	case string:
		mask = v
		maskre = v
	default:
		return
	}

	switch v := call.AudioName.(type) {
	case string:
		filename = v
	default:
		return
	}

	for k, v := range meta {
		if i := strings.Index(mask, v[0]); i != -1 {
			metapos = append(metapos, []interface{}{k, i})
			maskre = strings.Replace(maskre, v[0], fmt.Sprintf("(%v)", v[1]), 1)
		}
	}

	sort.Slice(metapos, func(i int, j int) bool {
		return metapos[i][1].(int) < metapos[j][1].(int)
	})

	base := strings.TrimSuffix(filename, path.Ext(filename))
	for i, s := range regexp.MustCompile(maskre).FindStringSubmatch(base) {
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

	switch v := metaval["system"].(type) {
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			call.System = uint(i)
		}
	}

	switch v := metaval["talkgroup"].(type) {
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			call.Talkgroup = uint(i)
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
	var err error

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
			LogEvent(controller.Database, LogLevelError, fmt.Sprintf("dirwatch.watcher: %v", err.Error()))
		}

		newTimer := func(eventName string) *time.Timer {
			return time.AfterFunc(2*time.Second, func() {
				dirwatch.Ingest(eventName)
			})
		}

		pending := map[string]*time.Timer{}

		for {
			if event, ok := <-dirwatch.watcher.Events; ok {
				switch event.Op {
				case fsnotify.Create:
					switch v := dirwatch.Delay.(type) {
					case uint:
						time.Sleep(time.Duration(v) * time.Millisecond)
					}
					if dirwatch.isDir(event.Name) {
						if err := dirwatch.walkDir(event.Name); err != nil {
							logError(err)
						}
					} else {
						pending[event.Name] = newTimer(event.Name)
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
					if pending[event.Name] != nil {
						pending[event.Name].Stop()
						pending[event.Name] = newTimer(event.Name)
					}
				}

			} else {
				dirwatch.Stop()
				break
			}
		}
	}()

	go func() {
		time.Sleep(2 * time.Second)

		fs.WalkDir(os.DirFS(dirwatch.Directory), ".", func(p string, d fs.DirEntry, err error) error {
			fp := path.Join(dirwatch.Directory, p)

			if dirwatch.isDir(fp) {
				dirwatch.dirs[fp] = true
				dirwatch.watcher.Add(fp)

			} else if dirwatch.DeleteAfter {
				dirwatch.Ingest(fp)
			}
			return err
		})
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

func (dirwatches *Dirwatches) FromMap(f []interface{}) {
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

	dirwatches.Stop()

	*dirwatches = Dirwatches{}

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
			LogEvent(
				controller.Database,
				LogLevelError,
				fmt.Sprintf("dirwatches.start: %s", err.Error()),
			)
		}
	}
}

func (dirwatches *Dirwatches) Stop() {
	for i := range dirwatches.List {
		dirwatches.List[i].Stop()
	}
}

func (dirwatches *Dirwatches) Write(db *Database) error {
	var (
		count  uint
		err    error
		rows   *sql.Rows
		rowIds = []uint{}
	)

	dirwatches.mutex.Lock()

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
		rows.Scan(&rowId)
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

	dirwatches.mutex.Unlock()

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
		fp := path.Join(d, p)
		if dirwatch.isDir(fp) {
			if !dirwatch.dirs[fp] {
				dirwatch.dirs[fp] = true
				dirwatch.watcher.Add(fp)
			}
		}
		return err
	})
}
