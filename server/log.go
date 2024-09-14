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
	Id       any       `json:"id"`
	DateTime time.Time `json:"dateTime"`
	Level    string    `json:"level"`
	Message  string    `json:"message"`
}

func NewLog() *Log {
	return &Log{}
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

		query := fmt.Sprintf(`INSERT INTO "logs" ("level", "message", "timestamp") VALUES ('%s', '%s', %d)`, l.Level, l.Message, l.DateTime.UnixMilli())
		if _, err := logs.database.Sql.Exec(query); err != nil {
			return fmt.Errorf("logs.logevent: %s in %s", err, query)
		}
	}

	return nil
}

func (logs *Logs) Prune(db *Database, pruneDays uint) error {
	logs.mutex.Lock()
	defer logs.mutex.Unlock()

	timestamp := time.Now().Add(-24 * time.Hour * time.Duration(pruneDays)).UnixMilli()
	query := fmt.Sprintf(`DELETE FROM "logs" WHERE "timestamp" < %d`, timestamp)

	if _, err := db.Sql.Exec(query); err != nil {
		return fmt.Errorf("%s in %s", err, query)
	}

	return nil
}

func (logs *Logs) Search(searchOptions *LogsSearchOptions, db *Database) (*LogsSearchResults, error) {
	const (
		ascOrder  = "ASC"
		descOrder = "DESC"
	)

	var (
		err  error
		rows *sql.Rows

		limit  uint
		offset uint
		order  string
		query  string
		where  string = "TRUE"

		level     sql.NullString
		logId     sql.NullInt64
		message   sql.NullString
		timestamp sql.NullInt64
	)

	logs.mutex.Lock()
	defer logs.mutex.Unlock()

	formatError := errorFormatter("logs", "search")

	logResults := &LogsSearchResults{
		Options: searchOptions,
		Logs:    []Log{},
	}

	switch v := searchOptions.Level.(type) {
	case string:
		where += fmt.Sprintf(` AND "level" = '%s'`, v)
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

		where += fmt.Sprintf(` AND ("timestamp" BETWEEN %d AND %d)`, start.UnixMilli(), stop.UnixMilli())
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

	query = fmt.Sprintf(`SELECT "timestamp" FROM "logs" WHERE %s ORDER BY "timestamp" ASC`, where)
	if err = db.Sql.QueryRow(query).Scan(&timestamp); err != nil && err != sql.ErrNoRows {
		return nil, formatError(err, query)
	}

	if timestamp.Valid {
		logResults.DateStart = time.UnixMilli(timestamp.Int64)
	}

	query = fmt.Sprintf(`SELECT "timestamp" FROM "logs" WHERE %s ORDER BY "timestamp" DESC`, where)
	if err = db.Sql.QueryRow(query).Scan(&timestamp); err != nil && err != sql.ErrNoRows {
		return nil, formatError(err, query)
	}

	if timestamp.Valid {
		logResults.DateStop = time.UnixMilli(timestamp.Int64)
	}

	query = fmt.Sprintf(`SELECT COUNT(*) FROM "logs" WHERE %s`, where)
	if err = db.Sql.QueryRow(query).Scan(&logResults.Count); err != nil && err != sql.ErrNoRows {
		return nil, formatError(err, query)
	}

	query = fmt.Sprintf(`SELECT "logId", "level", "message", "timestamp" FROM "logs" WHERE %s ORDER BY "timestamp" %s limit %d offset %d`, where, order, limit, offset)
	if rows, err = db.Sql.Query(query); err != nil && err != sql.ErrNoRows {
		return nil, formatError(err, query)
	}

	for rows.Next() {
		log := NewLog()

		if err = rows.Scan(&logId, &level, &message, &timestamp); err != nil {
			continue
		}

		if logId.Valid {
			log.Id = uint64(logId.Int64)
		} else {
			continue
		}

		if level.Valid && len(level.String) > 0 {
			log.Level = level.String
		} else {
			continue
		}

		if message.Valid && len(message.String) > 0 {
			log.Message = message.String
		} else {
			continue
		}

		if timestamp.Valid && timestamp.Int64 > 0 {
			log.DateTime = time.UnixMilli(timestamp.Int64)
		} else {
			continue
		}

		logResults.Logs = append(logResults.Logs, *log)
	}

	rows.Close()

	return logResults, nil
}

func (logs *Logs) setDaemon(d *Daemon) {
	logs.daemon = d
}

func (logs *Logs) setDatabase(d *Database) {
	logs.database = d
}

type LogsSearchOptions struct {
	Date   any `json:"date,omitempty"`
	Level  any `json:"level,omitempty"`
	Limit  any `json:"limit,omitempty"`
	Offset any `json:"offset,omitempty"`
	Sort   any `json:"sort,omitempty"`
}

func NewLogSearchOptions() *LogsSearchOptions {
	return &LogsSearchOptions{}
}

func (searchOptions *LogsSearchOptions) FromMap(m map[string]any) *LogsSearchOptions {
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

	return searchOptions
}

type LogsSearchResults struct {
	Count     uint64             `json:"count"`
	DateStart time.Time          `json:"dateStart"`
	DateStop  time.Time          `json:"dateStop"`
	Options   *LogsSearchOptions `json:"options"`
	Logs      []Log              `json:"logs"`
}
