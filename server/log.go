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
	"fmt"
	"log"
	"math"
	"sync"
	"time"
)

const (
	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelError = "error"
)

type Log struct {
	Id       interface{} `json:"_id"`
	DateTime time.Time   `json:"dateTime"`
	Level    string      `json:"level"`
	Message  string      `json:"message"`
}

type Logs struct {
	database *Database
	mutex    sync.Mutex
	daemon   *Daemon
}

func NewLogs() *Logs {
	return &Logs{
		mutex: sync.Mutex{},
	}
}

func (logs *Logs) LogEvent(level string, message string) error {
	logs.mutex.Lock()
	defer logs.mutex.Unlock()

	if logs.daemon != nil {
		switch level {
		case LogLevelError:
			logs.daemon.Logger.Error(message)
		case LogLevelWarn:
			logs.daemon.Logger.Warning(message)
		case LogLevelInfo:
			logs.daemon.Logger.Info(message)
		}

	} else {
		log.Println(message)
	}

	if logs.database != nil {
		l := Log{
			DateTime: time.Now().UTC(),
			Level:    level,
			Message:  message,
		}

		if _, err := logs.database.Sql.Exec("insert into `rdioScannerLogs` (`dateTime`, `level`, `message`) values (?, ?, ?)", l.DateTime, l.Level, l.Message); err != nil {
			return fmt.Errorf("logs.logevent: %v", err)
		}
	}

	return nil
}

func (logs *Logs) Prune(db *Database, pruneDays uint) error {
	logs.mutex.Lock()
	defer logs.mutex.Unlock()

	date := time.Now().Add(-24 * time.Hour * time.Duration(pruneDays)).Format(db.DateTimeFormat)
	_, err := db.Sql.Exec("delete from `rdioScannerLogs` where `dateTime` < ?", date)

	return err
}

func (logs *Logs) Search(searchOptions *LogsSearchOptions, db *Database) (*LogsSearchResults, error) {
	const (
		ascOrder  = "asc"
		descOrder = "desc"
	)

	var (
		dateTime interface{}
		err      error
		id       sql.NullFloat64
		limit    uint
		offset   uint
		order    string
		query    string
		rows     *sql.Rows
		where    string = "true"
	)

	logs.mutex.Lock()
	defer logs.mutex.Unlock()

	formatError := func(err error) error {
		return fmt.Errorf("logs.search: %v", err)
	}

	logResults := &LogsSearchResults{
		Options: searchOptions,
		Logs:    []Log{},
	}

	switch v := searchOptions.Level.(type) {
	case string:
		where += fmt.Sprintf(" and `level` = '%v'", v)
	}

	switch v := searchOptions.Sort.(type) {
	case int:
		if v < 0 {
			order = descOrder
		} else {
			order = ascOrder
		}
	default:
		order = ascOrder
	}

	switch v := searchOptions.Date.(type) {
	case time.Time:
		var (
			df    string = db.DateTimeFormat
			start time.Time
			stop  time.Time
		)

		if order == ascOrder {
			start = time.Date(v.Year(), v.Month(), v.Day(), v.Hour(), v.Minute(), 0, 0, time.UTC)
			stop = start.Add(time.Hour*24 - time.Millisecond)

		} else {
			start = time.Date(v.Year(), v.Month(), v.Day(), v.Hour(), v.Minute(), 0, 0, time.UTC).Add(time.Hour*-24 - time.Duration(v.Hour())).Add(time.Minute * time.Duration(-v.Minute()))
			stop = start.Add(time.Hour*24 - time.Millisecond - time.Duration(v.Hour())).Add(time.Minute * time.Duration(-v.Minute()))
		}

		where += fmt.Sprintf(" and (`dateTime` between '%v' and '%v')", start.Format(df), stop.Format(df))
	}

	switch v := searchOptions.Limit.(type) {
	case uint:
		limit = uint(math.Min(float64(500), float64(v)))
	default:
		limit = 200
	}

	switch v := searchOptions.Offset.(type) {
	case uint:
		offset = v
	}

	query = fmt.Sprintf("select `dateTime` from `rdioScannerLogs` where %v order by `dateTime` asc", where)
	if err = db.Sql.QueryRow(query).Scan(&dateTime); err != nil && err != sql.ErrNoRows {
		return nil, formatError(fmt.Errorf("%v, %v", err, query))
	}

	if dateTime == nil {
		return nil, nil
	}

	if t, err := db.ParseDateTime(dateTime); err == nil {
		logResults.DateStart = t
	}

	query = fmt.Sprintf("select `dateTime` from `rdioScannerLogs` where %v order by `dateTime` asc", where)
	if err = db.Sql.QueryRow(query).Scan(&dateTime); err != nil && err != sql.ErrNoRows {
		return nil, formatError(fmt.Errorf("%v, %v", err, query))
	}

	if t, err := db.ParseDateTime(dateTime); err == nil {
		logResults.DateStop = t
	}

	query = fmt.Sprintf("select count(*) from `rdioScannerLogs` where %v", where)
	if err = db.Sql.QueryRow(query).Scan(&logResults.Count); err != nil && err != sql.ErrNoRows {
		return nil, formatError(fmt.Errorf("%v, %v", err, query))
	}

	query = fmt.Sprintf("select `_id`, `DateTime`, `level`, `message` from `rdioScannerLogs` where %v order by `dateTime` %v limit %v offset %v", where, order, limit, offset)
	if rows, err = db.Sql.Query(query); err != nil && err != sql.ErrNoRows {
		return nil, formatError(fmt.Errorf("%v, %v", err, query))
	}

	for rows.Next() {
		log := Log{}

		if err = rows.Scan(&id, &dateTime, &log.Level, &log.Message); err != nil {
			break
		}

		if id.Valid && id.Float64 > 0 {
			log.Id = uint(id.Float64)
		}

		if t, err := db.ParseDateTime(dateTime); err == nil {
			log.DateTime = t
		} else {
			continue
		}

		logResults.Logs = append(logResults.Logs, log)
	}

	rows.Close()

	if err != nil {
		return nil, formatError(err)
	}

	return logResults, nil
}

func (logs *Logs) setDaemon(d *Daemon) {
	logs.daemon = d
}

func (logs *Logs) setDatabase(d *Database) {
	logs.database = d
}

type LogsSearchOptions struct {
	Date   interface{} `json:"date,omitempty"`
	Level  interface{} `json:"level,omitempty"`
	Limit  interface{} `json:"limit,omitempty"`
	Offset interface{} `json:"offset,omitempty"`
	Sort   interface{} `json:"sort,omitempty"`
}

func (searchOptions *LogsSearchOptions) FromMap(m map[string]interface{}) error {
	switch v := m["date"].(type) {
	case string:
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			searchOptions.Date = t
		}
	}

	switch v := m["level"].(type) {
	case string:
		searchOptions.Level = v
	}

	switch v := m["limit"].(type) {
	case float64:
		searchOptions.Limit = uint(v)
	}

	switch v := m["offset"].(type) {
	case float64:
		searchOptions.Offset = uint(v)
	}

	switch v := m["sort"].(type) {
	case float64:
		searchOptions.Sort = int(v)
	}

	return nil
}

type LogsSearchResults struct {
	Count     uint               `json:"count"`
	DateStart time.Time          `json:"dateStart"`
	DateStop  time.Time          `json:"dateStop"`
	Options   *LogsSearchOptions `json:"options"`
	Logs      []Log              `json:"logs"`
}
