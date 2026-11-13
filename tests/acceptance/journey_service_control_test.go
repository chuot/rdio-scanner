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

package acceptance

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// the service-control journey runs the built program as its own client of the
// service manager: a refused control is what an operator's shell sees, a
// status and a line on the terminal, invisible to an in-process run. Nothing
// is installed, started, or stopped, and the manager is never asked to act.

// controlWindow is how long the client may take: asking and being refused is
// one round trip, and a client that outlives the window never ended.
const controlWindow = 90 * time.Second

// TestARefusedServiceControlEndsWithTheFailureStatusAndSaysWhy covers the
// control the service manager has never heard of: the built program asks,
// the manager refuses, and what the operator gets is the exit status of a
// failure and a line naming the action and the refusal's reason.
func TestARefusedServiceControlEndsWithTheFailureStatusAndSaysWhy(t *testing.T) {
	base := t.TempDir()

	exe := buildServerBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), controlWindow)
	defer cancel()

	cmd := exec.CommandContext(ctx, exe, "-base_dir", base, "-service", "bogus")

	said, err := cmd.CombinedOutput()
	if err != nil && ctx.Err() != nil {
		t.Fatalf("the refused control never ended within %s: %v\n%s", controlWindow, ctx.Err(), said)
	}

	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatalf("running the built program with a control the manager does not know: %v\n%s", err, said)
	}

	code := 0
	if exit != nil {
		code = exit.ExitCode()
	}

	if code != 1 {
		t.Errorf("the refused control ended with status %d, want the status a failure ends with:\n%s", code, said)
	}

	if !strings.Contains(string(said), "bogus") {
		t.Errorf("the refused control named no action on the terminal, want the action that was asked for:\n%s", said)
	}

	// the reason is the manager's own words for the action it does not
	// know, named here rather than the whole sentence it composes.
	if !strings.Contains(string(said), "Unknown action") {
		t.Errorf("the refused control said no reason on the terminal, want the reason the manager gave:\n%s", said)
	}
}
