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
	"database/sql"
	"fmt"
	"log"
	"math"
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

func (log *Log) Write(db *Database) error {
	if _, err := db.Sql.Exec("insert into `rdioScannerLogs` (`dateTime`, `level`, `message`) values (?, ?, ?)", log.DateTime, log.Level, log.Message); err != nil {
		return fmt.Errorf("logs.write: %v", err)
	}

	return nil
}

type LogOptions struct {
	Date   interface{} `json:"date,omitempty"`
	Level  interface{} `json:"level,omitempty"`
	Limit  interface{} `json:"limit,omitempty"`
	Offset interface{} `json:"offset,omitempty"`
	Sort   interface{} `json:"sort,omitempty"`
}

func (logOptions *LogOptions) FromMap(m map[string]interface{}) error {
	switch v := m["date"].(type) {
	case string:
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			logOptions.Date = t
		}
	}

	switch v := m["level"].(type) {
	case string:
		logOptions.Level = v
	}

	switch v := m["limit"].(type) {
	case float64:
		logOptions.Limit = uint(v)
	}

	switch v := m["offset"].(type) {
	case float64:
		logOptions.Offset = uint(v)
	}

	switch v := m["sort"].(type) {
	case float64:
		logOptions.Sort = int(v)
	}

	return nil
}

type LogResults struct {
	Count     uint        `json:"count"`
	DateStart time.Time   `json:"dateStart"`
	DateStop  time.Time   `json:"dateStop"`
	Options   *LogOptions `json:"options"`
	Logs      []Log       `json:"logs"`
}

func LogEvent(db *Database, level string, message string) error {
	log.Println(message)

	log := Log{
		DateTime: time.Now().UTC(),
		Level:    level,
		Message:  message,
	}

	return log.Write(db)
}

func NewLogResults(logOptions *LogOptions, db *Database) (*LogResults, error) {
	const (
		ascOrder  = "asc"
		descOrder = "desc"
	)

	var (
		dateTime interface{}
		err      error
		limit    uint
		offset   uint
		order    string
		query    string
		rows     *sql.Rows
		where    string = "true"
	)

	formatError := func(err error) error {
		return fmt.Errorf("newLogResults: %v", err)
	}

	logResults := &LogResults{
		Options: logOptions,
		Logs:    []Log{},
	}

	switch v := logOptions.Level.(type) {
	case string:
		where += fmt.Sprintf(" and `level` == '%v'", v)
	}

	switch v := logOptions.Sort.(type) {
	case int:
		if v < 0 {
			order = descOrder
		} else {
			order = ascOrder
		}
	default:
		order = ascOrder
	}

	switch v := logOptions.Date.(type) {
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

	switch v := logOptions.Limit.(type) {
	case uint:
		limit = uint(math.Min(float64(500), float64(v)))
	default:
		limit = 200
	}

	switch v := logOptions.Offset.(type) {
	case uint:
		offset = v
	}

	query = fmt.Sprintf("select `dateTime` from `rdioScannerLogs` where %v order by `dateTime` asc", where)
	if err = db.Sql.QueryRow(query).Scan(&dateTime); err != nil && err != sql.ErrNoRows {
		return nil, formatError(fmt.Errorf("%v, %v", err, query))
	}

	if dateTime == nil {
		return logResults, nil
	}

	if t, err := db.ParseDateTime(dateTime); err == nil {
		logResults.DateStart = t
	} else {
		return nil, err
	}

	query = fmt.Sprintf("select `dateTime` from `rdioScannerLogs` where %v order by `dateTime` asc", where)
	if err = db.Sql.QueryRow(query).Scan(&dateTime); err != nil && err != sql.ErrNoRows {
		return nil, formatError(fmt.Errorf("%v, %v", err, query))
	}

	if t, err := db.ParseDateTime(dateTime); err == nil {
		logResults.DateStop = t
	} else {
		return nil, err
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

		if err = rows.Scan(&log.Id, &dateTime, &log.Level, &log.Message); err != nil {
			break
		}

		if t, err := db.ParseDateTime(dateTime); err == nil {
			log.DateTime = t
		} else {
			continue
		}

		logResults.Logs = append(logResults.Logs, log)
	}

	if err != nil {
		return logResults, formatError(err)
	}

	if err = rows.Close(); err != nil {
		return logResults, formatError(err)
	}

	return logResults, nil
}
