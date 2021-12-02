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
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"time"
)

type Controller struct {
	Admin        *Admin
	Api          *Api
	Config       *Config
	Database     *Database
	Accesses     *Accesses
	Apikeys      *Apikeys
	Dirwatches   *Dirwatches
	Downstreams  *Downstreams
	Groups       *Groups
	Options      *Options
	Scheduler    *Scheduler
	Systems      *Systems
	Tags         *Tags
	Clients      map[*Client]bool
	Register     chan *Client
	Unregister   chan *Client
	Ingest       chan *Call
	initialized  bool
	ffmpeg       bool
	ffmpegWarned bool
}

func (controller *Controller) CheckDuplicate(call *Call) bool {
	var count uint

	d := time.Duration(controller.Options.DuplicateDetectionTimeFrame) * time.Millisecond
	from := call.DateTime.Add(-d).Format(controller.Database.DateTimeFormat)
	to := call.DateTime.Add(d).Format(controller.Database.DateTimeFormat)

	query := fmt.Sprintf("select count(*) from `rdioScannerCalls` where (`dateTime` between '%v' and '%v') and `system` = %v and `talkgroup` = %v", from, to, call.System, call.Talkgroup)
	if err := controller.Database.Sql.QueryRow(query).Scan(&count); err != nil {
		return false
	}

	return count > 0
}

func (controller *Controller) ConvertAudio(call *Call) error {
	var (
		args = []string{"-i", "-"}
		err  error
	)

	if !controller.ffmpeg {
		if !controller.ffmpegWarned {
			controller.ffmpegWarned = true

			LogEvent(controller.Database, LogLevelWarn, "ffmpeg is not accessible, no audio conversion will be performed.")
		}

		return nil
	}

	if system, ok := controller.Systems.GetSystem(call.System); ok {
		if talkgroup, ok := system.Talkgroups.GetTalkgroup(call.Talkgroup); ok {
			if tag, ok := controller.Tags.GetTag(talkgroup.TagId); ok {
				args = append(args,
					"-metadata", fmt.Sprintf("album=%v", talkgroup.Label),
					"-metadata", fmt.Sprintf("artist=%v", system.Label),
					"-metadata", fmt.Sprintf("date=%v", call.DateTime),
					"-metadata", fmt.Sprintf("genre=%v", tag),
					"-metadata", fmt.Sprintf("title=%v", talkgroup.Name),
				)
			}
		}
	}

	args = append(args, "-c:a", "aac", "-b:a", "32k", "-movflags", "frag_keyframe+empty_moov", "-f", "ipod", "-")

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stdin = bytes.NewReader(call.Audio)

	stdout := bytes.NewBuffer([]byte(nil))
	cmd.Stdout = stdout

	if err = cmd.Run(); err == nil {
		call.Audio = stdout.Bytes()
		call.AudioType = "audio/mp4"

		switch v := call.AudioName.(type) {
		case string:
			call.AudioName = fmt.Sprintf("%v.m4a", strings.TrimSuffix(v, path.Ext((v))))
		}
	}

	return err
}

func (controller *Controller) EmitCall(call *Call) {
	for client := range controller.Clients {
		if (!controller.Accesses.IsRestricted() || client.Access.HasAccess(call)) && client.LivefeedMap.IsEnabled(call) {
			client.Send <- &Message{Command: MessageCommandCall, Payload: call}
		}
	}

	controller.Downstreams.Send(controller, call)
}

func (controller *Controller) EmitConfig() {
	restricted := controller.Accesses.IsRestricted()

	for client := range controller.Clients {
		if restricted {
			client.Send <- &Message{Command: MessageCommandPin}
		} else {
			controller.SendClientConfig(client)
		}
	}

	controller.Admin.BroadcastConfig()
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
		LogEvent(
			controller.Database,
			level,
			fmt.Sprintf("newcall: system=%v talkgroup=%v file=%v %v", call.System, call.Talkgroup, call.AudioName, message),
		)
	}

	logError := func(err error) {
		LogEvent(controller.Database, LogLevelError, fmt.Sprintf("controller.ingestcall: %v", err.Error()))
	}

	if system, ok = controller.Systems.GetSystem(call.System); ok {
		if talkgroup, ok = system.Talkgroups.GetTalkgroup(call.Talkgroup); ok {
			if system.Blacklists.IsBlacklisted(talkgroup.Id) {
				logCall(call, LogLevelInfo, "blacklisted")
				return
			}
		}
	}

	if system == nil && controller.Options.AutoPopulate {
		populated = true

		system = &System{Id: call.System}

		switch v := call.systemLabel.(type) {
		case string:
			system.Label = v
		default:
			system.Label = fmt.Sprintf("System %v", call.System)
		}

		*controller.Systems = append(*controller.Systems, system)
	}

	if system != nil && talkgroup == nil && (controller.Options.AutoPopulate || system.AutoPopulate) {
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

			*controller.Groups = append(*controller.Groups, *group)

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

			*controller.Tags = append(*controller.Tags, *tag)

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
			TagId:   tagId,
		}

		switch v := call.talkgroupLabel.(type) {
		case string:
			talkgroup.Label = v
			talkgroup.Name = v
		default:
			talkgroup.Label = fmt.Sprintf("%d", call.Talkgroup)
			talkgroup.Name = fmt.Sprintf("Talkgroup %d", call.Talkgroup)
		}

		system.Talkgroups = append(system.Talkgroups, talkgroup)
	}

	if controller.Options.AutoPopulate || system.AutoPopulate {
		switch v := call.units.(type) {
		case Units:
			populated = true
			system.Units.Merge(&v)
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
		logCall(call, LogLevelInfo, "no matching system/talkgroup")
		return
	}

	if !controller.Options.DisableDuplicateDetection {
		if controller.CheckDuplicate(call) {
			logCall(call, LogLevelWarn, "duplicate call rejected")
			return
		}
	}

	if !controller.Options.DisableAudioConversion {
		if err = controller.ConvertAudio(call); err != nil {
			logError(fmt.Errorf("convertaudio: %s", err.Error()))
			return
		}
	}

	if id, err = call.Write(controller.Database); err == nil {
		logCall(call, LogLevelInfo, "success")
		call.Id = id
		controller.EmitCall(call)

	} else {
		logError(err)
	}
}

func (controller *Controller) Init(config *Config, database *Database) error {
	if controller.initialized {
		return errors.New("controller already initialized")
	}

	var err error

	controller.initialized = true
	controller.Admin = &Admin{}
	controller.Api = &Api{}
	controller.Config = config
	controller.Database = database
	controller.Accesses = &Accesses{}
	controller.Apikeys = &Apikeys{}
	controller.Dirwatches = &Dirwatches{}
	controller.Downstreams = &Downstreams{}
	controller.Groups = &Groups{}
	controller.Options = &Options{}
	controller.Scheduler = &Scheduler{}
	controller.Systems = &Systems{}
	controller.Tags = &Tags{}
	controller.Clients = make(map[*Client]bool)
	controller.Register = make(chan *Client, 64)
	controller.Unregister = make(chan *Client, 64)
	controller.Ingest = make(chan *Call, 64)

	if err = controller.Accesses.Read(database); err != nil {
		return err
	}
	if err = controller.Apikeys.Read(database); err != nil {
		return err
	}
	if err = controller.Dirwatches.Read(database); err != nil {
		return err
	}
	if err = controller.Downstreams.Read(database); err != nil {
		return err
	}
	if err = controller.Groups.Read(database); err != nil {
		return err
	}
	if err = controller.Options.Read(database); err != nil {
		return err
	}
	if err = controller.Systems.Read(database); err != nil {
		return err
	}
	if err = controller.Tags.Read(database); err != nil {
		return err
	}

	if err = controller.Admin.Init(controller); err != nil {
		return err
	}
	if err = controller.Api.Init(controller); err != nil {
		return err
	}
	if err = controller.Scheduler.Init(controller); err != nil {
		return err
	}

	cmd := exec.Command("ffmpeg", "-version")
	if err := cmd.Run(); err == nil {
		controller.ffmpeg = true

	} else {
		controller.ffmpeg = false
	}

	if err = controller.Scheduler.Start(); err != nil {
		log.Printf("scheduler: %s", err.Error())
	}

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		controller.Terminate()
	}()

	go func() {
		for {
			select {
			case call := <-controller.Ingest:
				controller.IngestCall(call)

			case client := <-controller.Register:
				controller.Clients[client] = true
				controller.LogClientsCount()

			case client := <-controller.Unregister:
				if _, ok := controller.Clients[client]; ok {
					delete(controller.Clients, client)
					close(client.Send)
					controller.LogClientsCount()
				}
			}
		}
	}()

	controller.Dirwatches.Start(controller)

	return nil
}

func (controller *Controller) LogClientsCount() {
	LogEvent(
		controller.Database,
		LogLevelInfo,
		fmt.Sprintf("listeners count is %v", len(controller.Clients)),
	)
}

func (controller *Controller) ProcessMessage(client *Client, message *Message) error {
	if message.Command == MessageCommandVersion {
		client.Send <- &Message{Command: MessageCommandVersion, Payload: Version}

	} else if controller.Accesses.IsRestricted() && client.Access.Systems == nil && message.Command != MessageCommandPin {
		client.Send <- &Message{Command: MessageCommandPin}

	} else if message.Command == MessageCommandCall {
		if err := controller.ProcessMessageCommandCall(client, message); err != nil {
			return err
		}

	} else if message.Command == MessageCommandConfig {
		controller.SendClientConfig(client)

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

	if call, err = GetCall(id, controller.Database); err != nil {
		return err
	}

	if !controller.Accesses.IsRestricted() || client.Access.HasAccess(call) {
		client.Send <- &Message{Command: MessageCommandCall, Payload: call, Flag: message.Flag}
	}

	return nil
}

func (controller *Controller) ProcessMessageCommandListCall(client *Client, message *Message) error {
	switch v := message.Payload.(type) {
	case map[string]interface{}:
		searchOptions := SearchOptions{}
		searchOptions.fromMap(v)
		if searchResults, err := NewSearchResults(&searchOptions, client); err == nil {
			client.Send <- &Message{Command: MessageCommandListCall, Payload: searchResults}
		} else {
			return fmt.Errorf("controller.processmessage.commandlistcall: %v", err)
		}
	}
	return nil
}

func (controller *Controller) ProcessMessageCommandLivefeedMap(client *Client, message *Message) {
	client.LivefeedMap.FromMap(message.Payload)
	client.Send <- &Message{Command: MessageCommandLivefeedMap, Payload: !client.LivefeedMap.IsAllOff()}
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
				LogEvent(
					controller.Database,
					LogLevelWarn,
					fmt.Sprintf("invalid access code=\"%s\" address=\"%s\"", code, client.Conn.RemoteAddr().String()),
				)
				client.Send <- &Message{Command: MessageCommandPin}
				return nil
			}

			if client.AuthCount == maxAuthCount {
				LogEvent(
					controller.Database,
					LogLevelWarn,
					fmt.Sprintf("access ident=\"%s\" locked", client.Access.Ident),
				)
				client.Send <- &Message{Command: MessageCommandPin}
				return nil
			}

			if client.Access.HasExpired() {
				LogEvent(
					controller.Database,
					LogLevelWarn,
					fmt.Sprintf("access ident=\"%s\" expired", client.Access.Ident),
				)
				client.Send <- &Message{Command: MessageCommandExpired}
				return nil
			}

			switch v := client.Access.Limit.(type) {
			case uint:
				count := uint(0)
				for _, acc := range *controller.Accesses {
					if acc == *client.Access {
						count++
					}
				}
				if count >= v {
					LogEvent(
						controller.Database,
						LogLevelWarn,
						fmt.Sprintf("access ident=\"%s\" too many concurrent connections, limit is %d", client.Access.Ident, client.Access.Limit),
					)
					client.Send <- &Message{Command: MessageCommandMax}
					return nil
				}
			}
		}

		client.AuthCount = 0

		controller.SendClientConfig(client)
	}

	return nil
}

func (controller *Controller) SendClientConfig(client *Client) {
	client.SystemsMap = *controller.Systems.GetScopedSystems(client, controller.Groups, controller.Tags, controller.Options.SortTalkgroups)
	client.GroupsMap = *controller.Groups.GetGroupsMap(&client.SystemsMap)
	client.TagsMap = *controller.Tags.GetTagsMap(&client.SystemsMap)

	client.Send <- &Message{
		Command: MessageCommandConfig,
		Payload: map[string]interface{}{
			"dimmerDelay": controller.Options.DimmerDelay,
			"groups":      client.GroupsMap,
			"keypadBeeps": GetKeypadBeeps(controller.Options),
			"systems":     client.SystemsMap,
			"tags":        client.TagsMap,
			"tagsToggle":  controller.Options.TagsToggle,
		},
	}
}

func (controller *Controller) Terminate() {
	if err := controller.Database.Sql.Close(); err != nil {
		log.Println(err)
	}

	controller.Dirwatches.Stop()

	log.Println("terminated")

	os.Exit(0)
}
