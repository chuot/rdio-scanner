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

package backend

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"rdio-scanner/internal/application"
)

// versionURI is the resource an assistant reads to learn what deployment it is talking to.
const versionURI = "rdio-scanner://version"

// versionInfo is the shape the version resource carries. It is a DTO: the answer is shaped for the
// question "what am I talking to", and it is not the configuration document.
type versionInfo struct {
	Version string `json:"version"`

	// Docker is the server's own reading of whether it is running in a container, the same answer the
	// administrator's page reads.
	Docker bool `json:"docker"`

	// PasswordNeedChange is the server telling the operator the default credential is still in place.
	// An assistant reads this and acts on it with the operator in the loop.
	PasswordNeedChange bool `json:"passwordNeedChange"`
}

// versionContents reads the three facts the version resource reports.
func versionContents(ctx context.Context, deps Deps) ([]*mcp.ResourceContents, error) {
	configuration, err := deps.Config.Handle(ctx, application.GetConfigQuery{})
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(versionInfo{
		Version:            deps.Version,
		Docker:             deps.Docker,
		PasswordNeedChange: configuration.Options.AdminPasswordNeedChange,
	})
	if err != nil {
		return nil, err
	}

	return []*mcp.ResourceContents{{
		URI:      versionURI,
		MIMEType: "application/json",
		Text:     string(payload),
	}}, nil
}
