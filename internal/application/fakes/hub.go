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

package fakes

import (
	"sync"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Hub is an in-memory listener hub. It holds the same per-session state the real hub
// holds — the access a session presents and the live-watch selection it makes — and
// applies the same delivery rules, so a use case tested against it is tested against
// whether a listener would actually receive something.
type Hub struct {
	// Restricted makes the hub refuse a call for a session that holds no access, which
	// is what a server with an access list does.
	Restricted bool

	// DeliveredCalls records the calls each session received.
	DeliveredCalls map[string][]domain.Call

	// DeliveredConfigs records the configuration each session was sent.
	DeliveredConfigs map[string][]ports.ConfigSnapshot

	// BroadcastConfigs records the configurations sent to every session.
	BroadcastConfigs []ports.ConfigSnapshot

	// Livefeeds records the live-watch selection each session holds.
	Livefeeds map[string]domain.Livefeed

	// LivefeedAcks records the answer each session got to its selection.
	LivefeedAcks map[string][]bool

	// AccessRequests records the sessions asked for an access code.
	AccessRequests []string

	// AccessExpiries records the sessions told their access had run out.
	AccessExpiries []string

	// AccessLimits records the sessions told their identity had too many sessions open.
	AccessLimits []string

	// Stall, when set, is a one-shot gate on the broadcast: the first BroadcastCall
	// announces itself by closing Entered and waits for Release — the shape of a delivery
	// that cannot finish, a listener whose queue is full. Every broadcast after the first
	// passes through, so a test can tell a stalled delivery from a serialized server.
	Stall *BroadcastStall

	// Versions records the version reports sent to each session.
	Versions map[string][]string

	// ListenerCounts records each listener-count report, oldest first.
	ListenerCounts []int

	// ClosedClients records the sessions that were ended.
	ClosedClients []ClosedClient

	// accesses records the access each session holds.
	accesses map[string]domain.Access

	// attempts records how many access-code attempts each session has made.
	attempts map[string]uint

	// addressAttempts records, per address, how many access-code attempts have been spent
	// there and whether the refused-code warning has already been written for it. The
	// per-session counter is bypassable by reconnecting; this is the counter that makes
	// five tries mean five tries per machine.
	addressAttempts map[string]*fakesAddressAttempt

	// remoteAddrs records where each session connected from.
	remoteAddrs map[string]string
}

// fakesAddressAttempt is one address's access-attempt history in the fake hub.
type fakesAddressAttempt struct {
	count  uint
	warned bool
}

// ClosedClient is one session that was ended.
type ClosedClient struct {
	ClientID string
	Code     int
}

var _ ports.Hub = (*Hub)(nil)

// NewHub returns a hub with no sessions open.
func NewHub() *Hub {
	return &Hub{
		DeliveredCalls:   map[string][]domain.Call{},
		DeliveredConfigs: map[string][]ports.ConfigSnapshot{},
		Livefeeds:        map[string]domain.Livefeed{},
		LivefeedAcks:     map[string][]bool{},
		Versions:         map[string][]string{},
		accesses:         map[string]domain.Access{},
		attempts:         map[string]uint{},
		addressAttempts:  map[string]*fakesAddressAttempt{},
		remoteAddrs:      map[string]string{},
	}
}

// Open registers a session, which is what the real hub does when a listener connects.
func (hub *Hub) Open(clientID, remoteAddr string) {
	hub.remoteAddrs[clientID] = remoteAddr
	hub.Livefeeds[clientID] = domain.NewLivefeed()
}

// BroadcastCall delivers a call to every session whose access grants it and whose
// live-watch selection asks for it.
func (hub *Hub) BroadcastCall(call domain.Call) {
	if hub.Stall != nil {
		hub.Stall.gate()
	}

	for clientID := range hub.remoteAddrs {
		if hub.Restricted {
			access, ok := hub.accesses[clientID]
			if !ok || !access.Grants(call) {
				continue
			}
		}

		if !hub.Livefeeds[clientID].Watches(call) {
			continue
		}

		hub.DeliveredCalls[clientID] = append(hub.DeliveredCalls[clientID], call)
	}
}

// SendConfig records a configuration sent to one session.
func (hub *Hub) SendConfig(clientID string, cfg ports.ConfigSnapshot) {
	hub.DeliveredConfigs[clientID] = append(hub.DeliveredConfigs[clientID], cfg)
}

// BroadcastConfig records a configuration sent to every session.
func (hub *Hub) BroadcastConfig(cfg ports.ConfigSnapshot) {
	hub.BroadcastConfigs = append(hub.BroadcastConfigs, cfg)

	for clientID := range hub.remoteAddrs {
		hub.DeliveredConfigs[clientID] = append(hub.DeliveredConfigs[clientID], cfg)
	}
}

// SetLivefeed replaces a session's live-watch selection.
func (hub *Hub) SetLivefeed(clientID string, livefeed domain.Livefeed) {
	hub.Livefeeds[clientID] = livefeed
}

// LivefeedWatchesNothing reports whether a session has nothing switched on.
func (hub *Hub) LivefeedWatchesNothing(clientID string) bool {
	return hub.Livefeeds[clientID].AllOff()
}

// SendLivefeedMap records the answer given to a session's selection.
func (hub *Hub) SendLivefeedMap(clientID string, watching bool) {
	hub.LivefeedAcks[clientID] = append(hub.LivefeedAcks[clientID], watching)
}

// RequestAccessCode records that a session was asked for its access code.
func (hub *Hub) RequestAccessCode(clientID string) {
	hub.AccessRequests = append(hub.AccessRequests, clientID)
}

// ReportAccessExpired records that a session was told its access had run out.
func (hub *Hub) ReportAccessExpired(clientID string) {
	hub.AccessExpiries = append(hub.AccessExpiries, clientID)
}

// ReportAccessLimit records that a session was told its identity had too many sessions.
func (hub *Hub) ReportAccessLimit(clientID string) {
	hub.AccessLimits = append(hub.AccessLimits, clientID)
}

// SendVersion records a version report.
func (hub *Hub) SendVersion(clientID string, version string) {
	hub.Versions[clientID] = append(hub.Versions[clientID], version)
}

// BroadcastListenersCount records a listener-count report.
func (hub *Hub) BroadcastListenersCount(count int) {
	hub.ListenerCounts = append(hub.ListenerCounts, count)
}

// CloseClient records a session that was ended and drops its state, because a closed
// session stops counting, stops holding an access, and stops watching.
func (hub *Hub) CloseClient(clientID string, code int) {
	hub.ClosedClients = append(hub.ClosedClients, ClosedClient{ClientID: clientID, Code: code})

	delete(hub.remoteAddrs, clientID)
	delete(hub.accesses, clientID)
	delete(hub.attempts, clientID)
	delete(hub.Livefeeds, clientID)
}

// GrantAccess attaches an access to a session.
func (hub *Hub) GrantAccess(clientID string, access domain.Access) {
	hub.accesses[clientID] = access
}

// RecordAccessAttempt counts one access-code attempt for a session and reports which
// attempt number that was.
func (hub *Hub) RecordAccessAttempt(clientID string) uint {
	hub.attempts[clientID]++

	return hub.attempts[clientID]
}

// RecordAddressAccessAttempt counts one access-code attempt against the address the session
// connected from and reports how many that address has spent.
func (hub *Hub) RecordAddressAccessAttempt(clientID string) uint {
	address := hub.remoteAddrs[clientID]

	history, ok := hub.addressAttempts[address]
	if !ok {
		history = &fakesAddressAttempt{}
		hub.addressAttempts[address] = history
	}

	history.count++

	return history.count
}

// ShouldWarnAccessAttempt reports whether the refused-code warning is due for this session's
// address: the first refused attempt of the window warns, the rest of the window stays quiet.
func (hub *Hub) ShouldWarnAccessAttempt(clientID string) bool {
	history, ok := hub.addressAttempts[hub.remoteAddrs[clientID]]
	if !ok || history.warned {
		return false
	}

	history.warned = true

	return true
}

// ResetAccessAttempts clears a session's attempt counter, and with it the address's history:
// an accepted code renews the budget of the machine it was accepted from.
func (hub *Hub) ResetAccessAttempts(clientID string) {
	delete(hub.attempts, clientID)
	delete(hub.addressAttempts, hub.remoteAddrs[clientID])
}

// AttemptsOf reports how many attempts a session has made.
func (hub *Hub) AttemptsOf(clientID string) uint {
	return hub.attempts[clientID]
}

// AccessOf returns the access a session holds.
func (hub *Hub) AccessOf(clientID string) (domain.Access, bool) {
	access, ok := hub.accesses[clientID]

	return access, ok
}

// AccessSessionCount counts the sessions holding one access, counted by the access itself.
func (hub *Hub) AccessSessionCount(accessID uint64) uint {
	count := uint(0)

	for _, access := range hub.accesses {
		if access.ID == accessID {
			count++
		}
	}

	return count
}

// ListenerCount counts the open sessions.
func (hub *Hub) ListenerCount() int {
	return len(hub.remoteAddrs)
}

// RemoteAddrOf reports where a session connected from.
func (hub *Hub) RemoteAddrOf(clientID string) string {
	return hub.remoteAddrs[clientID]
}

// BroadcastStall is the one-shot gate the hub fake puts in front of its first broadcast.
// Only the first broadcast waits on the gate; every later one passes straight through —
// sync.Once would make every caller wait for the first to finish, which is exactly the
// serialization a test using this gate is looking for.
type BroadcastStall struct {
	// Entered is closed when the first broadcast is waiting.
	Entered chan struct{}

	// Release lets it go.
	Release chan struct{}

	mutex   sync.Mutex
	stalled bool
}

// gate holds the first broadcast on the stall and lets every later one through.
func (stall *BroadcastStall) gate() {
	stall.mutex.Lock()

	if stall.stalled {
		stall.mutex.Unlock()

		return
	}

	stall.stalled = true

	stall.mutex.Unlock()

	close(stall.Entered)

	<-stall.Release
}
