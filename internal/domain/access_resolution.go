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

package domain

// AccessResolution is what the server decided about an access credential presented to a stateless
// request. It is the outcome of the same access rule the listener socket applies, stated without the
// socket's replies: the socket maps these onto its access-code prompt, its expiry notice, and its
// disconnect notice; an HTTP request maps them onto its status codes; neither mapping is the rule.
type AccessResolution uint8

const (
	// AccessResolvedAnonymous is a server with no access list: the caller is the anonymous access,
	// sees the whole history, and was never asked for a code.
	AccessResolvedAnonymous AccessResolution = iota

	// AccessResolvedCode is a listed access whose date has not passed.
	AccessResolvedCode

	// AccessRefusedUnknown is a credential the access list does not carry — including the credential
	// of a caller that presented nothing, which the server does not distinguish from a wrong code.
	AccessRefusedUnknown

	// AccessRefusedExpired is a listed access whose date has passed.
	AccessRefusedExpired

	// AccessRefusedBudget is a request from an address that has spent its attempts. The credential
	// was never looked at, so this says nothing about whether what was presented was real — which is
	// what makes it indistinguishable, to the caller, from a wrong code.
	AccessRefusedBudget
)

// Refused reports whether the resolution refused the caller. The two states that resolved are the
// anonymous access and a listed access; everything else is a refusal.
func (resolution AccessResolution) Refused() bool {
	return resolution != AccessResolvedAnonymous && resolution != AccessResolvedCode
}
