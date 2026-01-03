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

package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"time"
)

type Controller struct {
	Admin       *Admin
	Api         *Api
	Calls       *Calls
	Config      *Config
	Database    *Database
	Accesses    *Accesses
	Apikeys     *Apikeys
	Dirwatches  *Dirwatches
	Downstreams *Downstreams
	FFMpeg      *FFMpeg
	Groups      *Groups
	Logs        *Logs
	Options     *Options
	Scheduler   *Scheduler
	Systems     *Systems
	Tags        *Tags
	Clients     *Clients
	Register    chan *Client
	Unregister  chan *Client
	Ingest      chan *Call
	running     bool
}

func NewController(config *Config) *Controller {
	controller := &Controller{
		Config:      config,
		Accesses:    NewAccesses(),
		Apikeys:     NewApikeys(),
		Calls:       NewCalls(),
		Dirwatches:  NewDirwatches(),
		Downstreams: NewDownstreams(),
		FFMpeg:      NewFFMpeg(),
		Groups:      NewGroups(),
		Logs:        NewLogs(),
		Options:     NewOptions(),
		Systems:     NewSystems(),
		Tags:        NewTags(),
		Clients:     NewClients(),
		Register:    make(chan *Client, 8192),
		Unregister:  make(chan *Client, 8192),
		Ingest:      make(chan *Call, 8192),
	}

	controller.Admin = NewAdmin(controller)
	controller.Api = NewApi(controller)
	controller.Database = NewDatabase(config)
	controller.Scheduler = NewScheduler(controller)

	controller.Logs.setDaemon(config.daemon)
	controller.Logs.setDatabase(controller.Database)

	return controller
}

func (controller *Controller) EmitCall(call *Call) {
	go controller.Downstreams.Send(controller, call)
	go controller.Clients.EmitCall(call, controller.Accesses.IsRestricted())
}

func (controller *Controller) EmitConfig() {
	go controller.Clients.EmitConfig(controller.Groups, controller.Options, controller.Systems, controller.Tags, controller.Accesses.IsRestricted())
	go controller.Admin.BroadcastConfig()
}

func (controller *Controller) IngestCall(call *Call) {
	var (
		err        error
		group      *Group
		groupId    uint
		groupLabel string
		id         uint
		ok         bool
		populated  bool
		system     *System
		tag        *Tag
		tagId      uint
		tagLabel   string
		talkgroup  *Talkgroup
	)

	logCall := func(call *Call, level string, message string) {
		controller.Logs.LogEvent(level, fmt.Sprintf("newcall: system=%v talkgroup=%v file=%v %v", call.System, call.Talkgroup, call.AudioName, message))
	}

	logError := func(err error) {
		controller.Logs.LogEvent(LogLevelError, fmt.Sprintf("controller.ingestcall: %v", err.Error()))
	}

	if system, ok = controller.Systems.GetSystem(call.System); ok {
		if system.Blacklists.IsBlacklisted(call.Talkgroup) {
			logCall(call, LogLevelInfo, "blacklisted")
			return
		}
		talkgroup, _ = system.Talkgroups.GetTalkgroup(call.Talkgroup)
	}

	if controller.Options.AutoPopulate && system == nil {
		populated = true

		system = NewSystem()
		system.Id = call.System

		switch v := call.systemLabel.(type) {
		case string:
			system.Label = v
		default:
			system.Label = fmt.Sprintf("System %v", call.System)
		}

		controller.Systems.List = append(controller.Systems.List, system)
	}

	if controller.Options.AutoPopulate || (system != nil && system.AutoPopulate) {
		if system != nil && talkgroup == nil {
			populated = true

			switch v := call.talkgroupGroup.(type) {
			case string:
				groupLabel = v
			default:
				groupLabel = "Unknown"
			}

			switch v := call.talkgroupTag.(type) {
			case string:
				tagLabel = v
			default:
				tagLabel = "Untagged"
			}

			if group, ok = controller.Groups.GetGroup(groupLabel); !ok {
				group = &Group{Label: groupLabel}

				controller.Groups.List = append(controller.Groups.List, group)

				if err = controller.Groups.Write(controller.Database); err != nil {
					logError(err)
					return
				}

				if err = controller.Groups.Read(controller.Database); err != nil {
					logError(err)
					return
				}

				if group, ok = controller.Groups.GetGroup(groupLabel); !ok {
					logError(fmt.Errorf("unable to get group %s", groupLabel))
					return
				}
			}

			switch v := group.Id.(type) {
			case uint:
				groupId = v
			default:
				logError(fmt.Errorf("unable to get group id for group %s", groupLabel))
				return
			}

			if tag, ok = controller.Tags.GetTag(tagLabel); !ok {
				tag = &Tag{Label: tagLabel}

				controller.Tags.List = append(controller.Tags.List, tag)

				if err = controller.Tags.Write(controller.Database); err != nil {
					logError(err)
					return
				}

				if err = controller.Tags.Read(controller.Database); err != nil {
					logError(err)
					return
				}

				if tag, ok = controller.Tags.GetTag(tagLabel); !ok {
					logError(fmt.Errorf("unable to get tag %s", tagLabel))
					return
				}
			}

			switch v := tag.Id.(type) {
			case uint:
				tagId = v
			default:
				logError(fmt.Errorf("unable to get tag id for tag %s", tagLabel))
				return
			}

			talkgroup = &Talkgroup{
				GroupId: groupId,
				Id:      call.Talkgroup,
				Label:   fmt.Sprintf("%d", call.Talkgroup),
				TagId:   tagId,
			}

			system.Talkgroups.List = append(system.Talkgroups.List, talkgroup)
		}

		switch v := call.talkgroupLabel.(type) {
		case string:
			if talkgroup.Label != v {
				populated = true
				talkgroup.Label = v
			}
		}

		switch v := call.talkgroupName.(type) {
		case string:
			if talkgroup.Name != v {
				populated = true
				talkgroup.Name = v
			}
		default:
			if len(talkgroup.Name) == 0 {
				populated = true
				talkgroup.Name = talkgroup.Label
			}
		}

		switch v := call.units.(type) {
		case *Units:
			if v != nil {
				populated = system.Units.Merge(v)
			}
		}
	}

	if populated {
		if err = controller.Systems.Write(controller.Database); err != nil {
			logError(err)
			return
		}

		if err = controller.Systems.Read(controller.Database); err != nil {
			logError(err)
			return
		}

		controller.EmitConfig()
	}

	if system == nil || talkgroup == nil {
		logCall(call, LogLevelWarn, "no matching system/talkgroup")
		return
	}

	if !controller.Options.DisableDuplicateDetection {
		if controller.Calls.CheckDuplicate(call, controller.Options.DuplicateDetectionTimeFrame, controller.Database) {
			logCall(call, LogLevelWarn, "duplicate call rejected")
			return
		}
	}

	if err := controller.FFMpeg.Convert(call, controller.Systems, controller.Tags, controller.Options.AudioConversion); err != nil {
		controller.Logs.LogEvent(LogLevelWarn, err.Error())
	}

	if id, err = controller.Calls.WriteCall(call, controller.Database); err == nil {
		call.Id = id
		call.systemLabel = system.Label
		call.talkgroupLabel = talkgroup.Label
		call.talkgroupName = talkgroup.Name

		if group == nil {
			if group, ok = controller.Groups.GetGroup(talkgroup.GroupId); ok {
				call.talkgroupGroup = group.Label
			}
		}

		if tag == nil {
			if tag, ok = controller.Tags.GetTag(talkgroup.TagId); ok {
				call.talkgroupTag = tag.Label
			}
		}

		logCall(call, LogLevelInfo, "success")

		controller.EmitCall(call)

	} else {
		logError(err)
	}
}

func (controller *Controller) LogClientsCount() {
	controller.Logs.LogEvent(LogLevelInfo, fmt.Sprintf("listeners count is %v", controller.Clients.Count()))
}

func (controller *Controller) ProcessMessage(client *Client, message *Message) error {
	if message.Command == MessageCommandVersion {
		controller.ProcessMessageCommandVersion(client)

	} else if controller.Accesses.IsRestricted() && client.Access.Systems == nil && message.Command != MessageCommandPin {
		client.Send <- &Message{Command: MessageCommandPin}

	} else if message.Command == MessageCommandCall {
		if err := controller.ProcessMessageCommandCall(client, message); err != nil {
			return err
		}

	} else if message.Command == MessageCommandConfig {
		client.SendConfig(controller.Groups, controller.Options, controller.Systems, controller.Tags)

	} else if message.Command == MessageCommandListCall {
		if err := controller.ProcessMessageCommandListCall(client, message); err != nil {
			return err
		}

	} else if message.Command == MessageCommandLivefeedMap {
		controller.ProcessMessageCommandLivefeedMap(client, message)

	} else if message.Command == MessageCommandPin {
		if err := controller.ProcessMessageCommandPin(client, message); err != nil {
			return err
		}
	}

	return nil
}

func (controller *Controller) ProcessMessageCommandCall(client *Client, message *Message) error {
	var (
		call *Call
		err  error
		i    int
		id   uint
	)

	switch v := message.Payload.(type) {
	case float64:
		id = uint(v)
	case string:
		if i, err = strconv.Atoi(v); err == nil {
			id = uint(i)
		} else {
			return err
		}
	}

	if call, err = controller.Calls.GetCall(id, controller.Database); err != nil {
		return err
	}

	if !controller.Accesses.IsRestricted() || client.Access.HasAccess(call) {
		client.Send <- &Message{Command: MessageCommandCall, Payload: call, Flag: message.Flag}
	}

	return nil
}

func (controller *Controller) ProcessMessageCommandListCall(client *Client, message *Message) error {
	switch v := message.Payload.(type) {
	case map[string]any:
		searchOptions := CallsSearchOptions{searchPatchedTalkgroups: controller.Options.SearchPatchedTalkgroups}
		searchOptions.fromMap(v)
		if searchResults, err := controller.Calls.Search(&searchOptions, client); err == nil {
			client.Send <- &Message{Command: MessageCommandListCall, Payload: searchResults}
		} else {
			return fmt.Errorf("controller.processmessage.commandlistcall: %v", err)
		}
	}
	return nil
}

func (controller *Controller) ProcessMessageCommandLivefeedMap(client *Client, message *Message) {
	client.Livefeed.FromMap(message.Payload)
	client.Send <- &Message{Command: MessageCommandLivefeedMap, Payload: !client.Livefeed.IsAllOff()}
}

func (controller *Controller) ProcessMessageCommandPin(client *Client, message *Message) error {
	const maxAuthCount = 5

	switch v := message.Payload.(type) {
	case string:
		b, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return fmt.Errorf("controller.processmessage.commandpin: %v", err)
		}

		client.AuthCount++
		if client.AuthCount > maxAuthCount {
			client.Send <- &Message{Command: MessageCommandPin}
			return nil
		}

		if controller.Accesses.IsRestricted() {
			code := string(b)
			if access, ok := controller.Accesses.GetAccess(code); ok {
				client.Access = access
			} else {
				controller.Logs.LogEvent(LogLevelWarn, fmt.Sprintf("invalid access code %s for ip %s", code, client.GetRemoteAddr()))
				client.Send <- &Message{Command: MessageCommandPin}
				return nil
			}

			if client.AuthCount == maxAuthCount {
				controller.Logs.LogEvent(LogLevelWarn, fmt.Sprintf("locked access for ident %s locked", client.Access.Ident))
				client.Send <- &Message{Command: MessageCommandPin}
				return nil
			}

			if client.Access.HasExpired() {
				controller.Logs.LogEvent(LogLevelWarn, fmt.Sprintf("expired access for ident %s", client.Access.Ident))
				client.Send <- &Message{Command: MessageCommandExpired}
				return nil
			}

			switch v := client.Access.Limit.(type) {
			case uint:
				if controller.Clients.AccessCount(client) > int(v) {
					controller.Logs.LogEvent(LogLevelWarn, fmt.Sprintf("too many concurrent connections for ident %s, limit is %d", client.Access.Ident, client.Access.Limit))
					client.Send <- &Message{Command: MessageCommandMax}
					return nil
				}
			}
		}

		client.AuthCount = 0

		client.SendConfig(controller.Groups, controller.Options, controller.Systems, controller.Tags)
	}

	return nil
}

func (controller *Controller) ProcessMessageCommandVersion(client *Client) {
	p := map[string]string{"version": Version}

	if len(controller.Options.Branding) > 0 {
		p["branding"] = controller.Options.Branding
	}

	if len(controller.Options.Email) > 0 {
		p["email"] = controller.Options.Email
	}

	client.Send <- &Message{Command: MessageCommandVersion, Payload: p}
}

func (controller *Controller) Start() error {
	var err error

	if controller.running {
		return errors.New("controller already running")
	} else {
		controller.running = true
	}

	controller.Logs.LogEvent(LogLevelWarn, "server started")

	if len(controller.Config.BaseDir) > 0 {
		log.Printf("base folder is %s\n", controller.Config.BaseDir)
	}

	if err = controller.Accesses.Read(controller.Database); err != nil {
		return err
	}
	if err = controller.Apikeys.Read(controller.Database); err != nil {
		return err
	}
	if err = controller.Dirwatches.Read(controller.Database); err != nil {
		return err
	}
	if err = controller.Downstreams.Read(controller.Database); err != nil {
		return err
	}
	if err = controller.Groups.Read(controller.Database); err != nil {
		return err
	}
	if err = controller.Options.Read(controller.Database); err != nil {
		return err
	}
	if err = controller.Systems.Read(controller.Database); err != nil {
		return err
	}
	if err = controller.Tags.Read(controller.Database); err != nil {
		return err
	}

	if err = controller.Admin.Start(); err != nil {
		return err
	}
	if err = controller.Scheduler.Start(); err != nil {
		return err
	}

	go func() {
		c := make(chan os.Signal, 8)
		signal.Notify(c, os.Interrupt)
		<-c
		controller.Terminate()
	}()

	go func() {
		for {
			call := <-controller.Ingest
			controller.IngestCall(call)
		}
	}()

	go func() {
		const (
			minTimeout = 3
			maxTimeout = 15
		)

		var (
			timeout time.Duration = minTimeout
			timer   *time.Timer
		)

		doClientsCount := func() {
			if timer != nil {
				timer.Stop()

				timeout++
				if timeout > maxTimeout {
					timeout = maxTimeout
				}
			}

			timer = time.AfterFunc(timeout*time.Second, func() {
				timer = nil
				timeout = minTimeout

				controller.LogClientsCount()

				if controller.Options.ShowListenersCount {
					controller.Clients.EmitListenersCount()
				}
			})
		}

		for {
			select {
			case client := <-controller.Register:
				controller.Clients.Add(client)
				doClientsCount()

			case client := <-controller.Unregister:
				controller.Clients.Remove(client)
				doClientsCount()
			}
		}
	}()

	controller.Dirwatches.Start(controller)

	return nil
}

func (controller *Controller) Terminate() {
	controller.Dirwatches.Stop()

	if err := controller.Database.Sql.Close(); err != nil {
		log.Println(err)
	}

	log.Println("terminated")

	os.Exit(0)
}
