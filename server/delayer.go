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
	"fmt"
	"sync"
	"time"
)

type Delayer struct {
	controller *Controller
	mutex      sync.Mutex
	timers     map[uint64]time.Timer
}

func NewDelayer(controller *Controller) *Delayer {
	return &Delayer{
		controller: controller,
		mutex:      sync.Mutex{},
		timers:     make(map[uint64]time.Timer),
	}
}

func (delayer *Delayer) CanDelay(call *Call) bool {
	return delayer.getTimestamp(call).After(time.Now())
}

func (delayer *Delayer) Delay(call *Call) {
	delay := delayer.getDelay(call)

	logError := func(err error) {
		delayer.controller.Logs.LogEvent(LogLevelError, fmt.Sprintf("delayer.delay: %s", err.Error()))
	}

	if delay > 0 {
		call.Delayed = true

		timestamp := delayer.getTimestamp(call)
		remaining := time.Until(timestamp)

		if err := delayer.push(call, timestamp); err == nil {
			delayer.timers[call.Id] = *time.AfterFunc(remaining, func() {
				if err := delayer.pop(call); err != nil {
					logError(err)
				}

				delete(delayer.timers, call.Id)

				delayer.controller.EmitCall(call)
			})

		} else {
			logError(err)
		}

	} else {
		delayer.controller.EmitCall(call)
	}
}

func (delayer *Delayer) Start() error {
	var (
		err   error
		query string
		rows  *sql.Rows
	)

	delayer.mutex.Lock()

	callIds := map[uint64]int64{}

	formatError := errorFormatter("delayer", "restore")

	query = `SELECT "callId", "timestamp" from "delayed"`
	if rows, err = delayer.controller.Database.Sql.Query(query); err != nil {
		return formatError(err, query)
	}

	for rows.Next() {
		var (
			callId    uint64
			timestamp int64
		)

		if err = rows.Scan(&callId, &timestamp); err != nil {
			break
		}

		callIds[callId] = timestamp
	}

	rows.Close()

	if err != nil {
		return formatError(err, "")
	}

	if len(callIds) > 0 {
		query = `DELETE FROM "delayed"`
		if _, err = delayer.controller.Database.Sql.Exec(query); err != nil {
			return formatError(err, query)
		}
	}

	delayer.mutex.Unlock()

	for callId, timestamp := range callIds {
		if call, err := delayer.controller.Calls.GetCall(callId); err == nil {
			call.Delayed = true

			if time.UnixMilli(timestamp).Before(time.Now()) {
				delayer.controller.EmitCall(call)

			} else {
				delayer.Delay(call)
			}
		}
	}

	return nil
}

func (delayer *Delayer) getDelay(call *Call) uint {
	if call.Talkgroup.Delay > 0 {
		return call.Talkgroup.Delay

	} else if call.System.Delay > 0 {
		return call.System.Delay
	}

	return 0
}

func (delayer *Delayer) getTimestamp(call *Call) time.Time {
	delay := delayer.getDelay(call)

	return call.Timestamp.Add(time.Duration(delay) * time.Minute)
}

func (delayer *Delayer) pop(call *Call) error {
	delayer.mutex.Lock()
	defer delayer.mutex.Unlock()

	formatError := errorFormatter("delayer", "pop")

	query := fmt.Sprintf(`DELETE FROM "delayed" WHERE "callId" = %d`, call.Id)
	if _, err := delayer.controller.Database.Sql.Exec(query); err != nil {
		return formatError(err, query)
	}

	return nil
}

func (delayer *Delayer) push(call *Call, timestamp time.Time) error {
	delayer.mutex.Lock()
	defer delayer.mutex.Unlock()

	formatError := errorFormatter("delayer", "push")

	query := fmt.Sprintf(`INSERT INTO "delayed" ("callId", "timestamp") VALUES (%d, %d)`, call.Id, timestamp.UnixMilli())
	if _, err := delayer.controller.Database.Sql.Exec(query); err != nil {
		return formatError(err, query)
	}

	return nil
}
