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

package domain

// Configuration is everything the server is currently running with: the eight
// sections an administrator reads and saves. It is a bundle with no behavior
// of its own: every rule lives on the section it belongs to, so a rule is
// never decided in two places.
type Configuration struct {
	Access      Accesses
	Apikeys     Apikeys
	Dirwatches  Dirwatches
	Downstreams Downstreams
	Groups      Groups
	Options     Options
	Systems     Systems
	Tags        Tags
}

// Restricted reports whether this configuration requires listeners to present an access code.
//
// It is the one question the whole configuration is asked often enough to be worth saying here,
// and it is answered entirely by the access list: a server with no access list is open.
func (configuration Configuration) Restricted() bool {
	return configuration.Access.Restricted()
}
