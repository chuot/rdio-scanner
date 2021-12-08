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
	"errors"
	"fmt"
	"time"
)

type Scheduler struct {
	Controller *Controller
	Ticker     *time.Ticker
	cancel     chan interface{}
	running    bool
}

func NewScheduler(controller *Controller) *Scheduler {
	return &Scheduler{
		Controller: controller,
		cancel:     make(chan interface{}),
	}
}

func (scheduler *Scheduler) run() {
	logError := func(err error) {
		LogEvent(
			scheduler.Controller.Database,
			LogLevelError,
			fmt.Sprintf("scheduler.run: %s", err.Error()),
		)
	}

	if err := scheduler.Controller.Database.Prune(scheduler.Controller); err != nil {
		logError(err)
	}
}

func (scheduler *Scheduler) Start() error {
	if scheduler.running {
		return errors.New("scheduler already running")
	} else {
		scheduler.running = true
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
	if !scheduler.running {
		return errors.New("scheduler not running")
	}

	scheduler.Ticker.Stop()
	scheduler.Ticker = nil
	scheduler.running = false

	return nil
}
