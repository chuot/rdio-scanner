// Copyright (C) 2019-2026 Chrystian Huot <chrystian@huot.qc.ca>
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
	"log"
	"os"
	"runtime"

	"github.com/kardianos/service"
)

type Daemon struct {
	Config    service.Config
	Errors    chan error
	Interface service.Interface
	Service   service.Service
	Logger    service.Logger
}

func NewDaemon() *Daemon {
	var (
		err  error
		name string
	)

	// https://github.com/kardianos/service/issues/223
	if runtime.GOOS == "freebsd" {
		name = "rdioscanner"
	} else {
		name = "rdio-scanner"
	}

	p, _ := os.FindProcess(os.Getpid())

	d := Daemon{
		Errors:    make(chan error),
		Interface: &DaemonInterface{Process: p},
	}

	d.Config = service.Config{
		Name:        name,
		DisplayName: "Rdio Scanner",
		Description: "The perfect software-defined radio companion",
		Arguments:   []string{"-service", "run"},
	}

	if d.Service, err = service.New(d.Interface, &d.Config); err != nil {
		log.Fatal(err)
	}

	if d.Logger, err = d.Service.Logger(nil); err != nil {
		log.Fatal(err)
	}

	return &d
}

func (d *Daemon) Control(action string) *Daemon {
	if action == "run" {
		go func() {
			if err := d.Service.Run(); err != nil {
				d.Logger.Error(err)
			}
		}()

	} else if err := service.Control(d.Service, action); err == nil {
		os.Exit(0)

	} else {
		os.Exit(-1)
	}

	return d
}

type DaemonInterface struct {
	Process *os.Process
}

func (d *DaemonInterface) Start(s service.Service) error {
	return nil
}

func (d *DaemonInterface) Stop(s service.Service) error {
	if d.Process != nil {
		d.Process.Signal(os.Interrupt)
	}
	return nil
}
