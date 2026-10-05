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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Per-section admin config endpoints (REQ-019, decision-admin-section-api.md):
// GET/PUT /api/admin/config/<section> replaces the whole-config-only save with
// per-section updates. The legacy exact route /api/admin/config (ConfigHandler)
// is untouched; this handler is registered on the subtree path.

var adminConfigSections = map[string]bool{
	"access":      true,
	"apikeys":     true,
	"dirwatch":    true,
	"downstreams": true,
	"groups":      true,
	"options":     true,
	"systems":     true,
	"tags":        true,
}

func (admin *Admin) GetSection(section string) any {
	switch section {
	case "access":
		return admin.Controller.Accesses.List
	case "apikeys":
		return admin.Controller.Apikeys.List
	case "dirwatch":
		return admin.Controller.Dirwatches.List
	case "downstreams":
		return admin.Controller.Downstreams.List
	case "groups":
		return admin.Controller.Groups.List
	case "options":
		return admin.Controller.Options
	case "systems":
		return admin.Controller.Systems.List
	case "tags":
		return admin.Controller.Tags.List
	}

	return nil
}

func (admin *Admin) SendSection(w http.ResponseWriter, section string) {
	if b, err := json.Marshal(admin.GetSection(section)); err == nil {
		w.Write(b)
	} else {
		w.WriteHeader(http.StatusExpectationFailed)
	}
}

func (admin *Admin) SectionHandler(w http.ResponseWriter, r *http.Request) {
	logError := func(err error) {
		admin.Controller.Logs.LogEvent(LogLevelError, fmt.Sprintf("admin.sectionhandler.put: %s", err.Error()))
	}

	if strings.EqualFold(r.Header.Get("upgrade"), "websocket") {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	section := strings.TrimPrefix(r.URL.Path, "/api/admin/config/")

	if !adminConfigSections[section] {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	t := admin.GetAuthorization(r)
	if !admin.ValidateToken(t) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		admin.SendSection(w, section)

	case http.MethodPut:
		admin.mutex.Lock()
		defer admin.mutex.Unlock()

		admin.Controller.Dirwatches.Stop()
		defer func() {
			// Dirwatches.Stop clears the in-memory list; unlike the whole-config
			// handler (which always repopulates it from the request body), a
			// per-section save of any other section must reload it from the
			// database before restarting the watchers.
			if section != "dirwatch" {
				if err := admin.Controller.Dirwatches.Read(admin.Controller.Database); err != nil {
					logError(err)
				}
			}
			admin.Controller.Dirwatches.Start(admin.Controller)
		}()

		if section == "options" {
			var m map[string]any

			if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			admin.Controller.Options.FromMap(m)

			if err := admin.Controller.Options.Write(admin.Controller.Database); err != nil {
				logError(err)
				w.WriteHeader(http.StatusExpectationFailed)
				return
			}

			admin.Controller.EmitConfig()
			admin.SendSection(w, section)
			admin.Controller.Logs.LogEvent(LogLevelWarn, "configuration changed")

			return
		}

		var f []any

		if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		var err error

		switch section {
		case "access":
			admin.Controller.Accesses.FromMap(f)
			if err = admin.Controller.Accesses.Write(admin.Controller.Database); err == nil {
				err = admin.Controller.Accesses.Read(admin.Controller.Database)
			}

		case "apikeys":
			admin.Controller.Apikeys.FromMap(f)
			if err = admin.Controller.Apikeys.Write(admin.Controller.Database); err == nil {
				err = admin.Controller.Apikeys.Read(admin.Controller.Database)
			}

		case "dirwatch":
			admin.Controller.Dirwatches.FromMap(f)
			if err = admin.Controller.Dirwatches.Write(admin.Controller.Database); err == nil {
				err = admin.Controller.Dirwatches.Read(admin.Controller.Database)
			}

		case "downstreams":
			admin.Controller.Downstreams.FromMap(f)
			if err = admin.Controller.Downstreams.Write(admin.Controller.Database); err == nil {
				err = admin.Controller.Downstreams.Read(admin.Controller.Database)
			}

		case "groups":
			admin.Controller.Groups.FromMap(f)
			if err = admin.Controller.Groups.Write(admin.Controller.Database); err == nil {
				err = admin.Controller.Groups.Read(admin.Controller.Database)
			}

		case "systems":
			admin.Controller.Systems.FromMap(f)
			if err = admin.Controller.Systems.Write(admin.Controller.Database); err == nil {
				err = admin.Controller.Systems.Read(admin.Controller.Database)
			}

		case "tags":
			admin.Controller.Tags.FromMap(f)
			if err = admin.Controller.Tags.Write(admin.Controller.Database); err == nil {
				err = admin.Controller.Tags.Read(admin.Controller.Database)
			}
		}

		if err != nil {
			logError(err)
			w.WriteHeader(http.StatusExpectationFailed)
			return
		}

		admin.Controller.EmitConfig()
		admin.SendSection(w, section)
		admin.Controller.Logs.LogEvent(LogLevelWarn, "configuration changed")

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
