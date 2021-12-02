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

	d := Daemon{
		Errors:    make(chan error, 5),
		Interface: &DaemonInterface{},
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

	return &d
}

func (d *Daemon) Control(action string) (bool, error) {
	if action == "run" {
		go d.Service.Run()

		return true, nil
	}

	return false, service.Control(d.Service, action)
}

type DaemonInterface struct{}

func (d *DaemonInterface) Start(s service.Service) error {
	return nil
}

func (d *DaemonInterface) Stop(s service.Service) error {
	os.Exit(0)
	return nil
}
