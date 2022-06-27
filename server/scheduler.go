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
	"errors"
	"fmt"
	"sync"
	"time"
)

type Scheduler struct {
	Controller *Controller
	Ticker     *time.Ticker
	cancel     chan any
	mutex      sync.Mutex
	started    bool
}

func NewScheduler(controller *Controller) *Scheduler {
	return &Scheduler{
		Controller: controller,
		cancel:     make(chan any),
	}
}

func (scheduler *Scheduler) pruneDatabase() error {
	if scheduler.Controller.Options.PruneDays == 0 {
		return nil
	}

	scheduler.Controller.Logs.LogEvent(LogLevelInfo, "database pruning")

	if err := scheduler.Controller.Calls.Prune(scheduler.Controller.Database, scheduler.Controller.Options.PruneDays); err != nil {
		return err
	}

	if err := scheduler.Controller.Logs.Prune(scheduler.Controller.Database, scheduler.Controller.Options.PruneDays); err != nil {
		return err
	}

	return nil
}

func (scheduler *Scheduler) run() {
	scheduler.mutex.Lock()
	defer scheduler.mutex.Unlock()

	logError := func(err error) {
		scheduler.Controller.Logs.LogEvent(LogLevelError, fmt.Sprintf("scheduler.run: %s", err.Error()))
	}

	if err := scheduler.pruneDatabase(); err != nil {
		logError(err)
	}
}

func (scheduler *Scheduler) Start() error {
	if scheduler.started {
		return errors.New("scheduler already started")
	} else {
		scheduler.started = true
	}

	scheduler.Ticker = time.NewTicker(time.Hour)

	go func() {
		for {
			select {
			case <-scheduler.cancel:
				scheduler.Stop()
				return
			case <-scheduler.Ticker.C:
				scheduler.run()
			}
		}
	}()

	return nil
}

func (scheduler *Scheduler) Stop() error {
	if !scheduler.started {
		return errors.New("scheduler not started")
	}

	scheduler.Ticker.Stop()
	scheduler.Ticker = nil
	scheduler.started = false

	return nil
}
