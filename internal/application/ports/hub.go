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

package ports

import "rdio-scanner/internal/domain"

// ConfigSnapshot is the configuration in effect, handed to the hub for delivery to
// listeners. The hub decides what each listener may see and how it is framed.
type ConfigSnapshot struct {
	Groups  domain.Groups
	Tags    domain.Tags
	Systems domain.Systems
	Options domain.Options
}

// Hub is the set of live listener sessions the application talks to, implemented by the
// websocket presentation layer. A listener is named by an opaque identifier the hub
// chooses: the application never reaches a connection.
type Hub interface {
	// BroadcastCall delivers a recorded call to every listener whose access grants it
	// and whose live-watch selection asks for it.
	BroadcastCall(call domain.Call)

	// SendConfig delivers the configuration one listener is allowed to see.
	SendConfig(clientID string, cfg ConfigSnapshot)

	// BroadcastConfig delivers the configuration to every listener, each scoped to what
	// that listener is allowed to see.
	BroadcastConfig(cfg ConfigSnapshot)

	// SetLivefeed replaces one listener's live-watch selection.
	SetLivefeed(clientID string, livefeed domain.Livefeed)

	// LivefeedWatchesNothing reports whether a listener has nothing switched on.
	LivefeedWatchesNothing(clientID string) bool

	// SendLivefeedMap answers a listener's live-watch selection with whether anything is
	// now switched on.
	SendLivefeedMap(clientID string, watching bool)

	// RequestAccessCode asks a listener for its access code.
	RequestAccessCode(clientID string)

	// ReportAccessExpired tells a listener that its access has run out.
	ReportAccessExpired(clientID string)

	// ReportAccessLimit tells a listener that its identity already has as many sessions
	// open as it is allowed.
	ReportAccessLimit(clientID string)

	// SendVersion reports the server version to one listener.
	SendVersion(clientID string, version string)

	// BroadcastListenersCount reports how many listeners are watching.
	BroadcastListenersCount(count int)

	// CloseClient ends a listener session.
	CloseClient(clientID string, code int)

	// GrantAccess attaches an access to a listener session, which is what makes that
	// session authenticated.
	GrantAccess(clientID string, access domain.Access)

	// AccessOf returns the access a listener session holds, if it holds one.
	AccessOf(clientID string) (domain.Access, bool)

	// RecordAccessAttempt counts one access-code attempt for a listener session and
	// reports which attempt number that was.
	RecordAccessAttempt(clientID string) uint

	// RecordAddressAccessAttempt counts one access-code attempt against the address the
	// listener connected from and reports how many attempts that address has spent in the
	// current window. The per-session counter is bypassable by reconnecting, so the
	// address counter is what makes "five tries" mean five tries per machine.
	RecordAddressAccessAttempt(clientID string) uint

	// ShouldWarnAccessAttempt reports whether to write the refused-code warning for this
	// listener's address now: once per window per address.
	ShouldWarnAccessAttempt(clientID string) bool

	// ResetAccessAttempts clears a session's attempt counter, and only when an access code
	// is accepted: a session refused for any reason keeps spending tries.
	ResetAccessAttempts(clientID string)

	// AccessSessionCount reports how many sessions are open under one access, counted by
	// the access itself rather than its label: two accesses may share a label.
	AccessSessionCount(accessID uint64) uint

	// ListenerCount reports how many listener sessions are open.
	ListenerCount() int

	// RemoteAddrOf reports where a listener session connected from, the address the
	// server names in warnings about refused access codes.
	RemoteAddrOf(clientID string) string
}
