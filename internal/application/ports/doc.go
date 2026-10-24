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

// Package ports declares the abstractions the application layer depends on: the
// repository interfaces for every aggregate, the adapter interfaces for the audio
// converter, downstream forwarder, token issuer, and clock, and the listener hub
// through which use cases emit calls, scoped configurations, and listener counts.
//
// The implementations live in internal/infrastructure and internal/presentation,
// injected by the composition root.
package ports
