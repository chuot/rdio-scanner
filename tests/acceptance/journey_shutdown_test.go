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

package acceptance

import (
	"bytes"
	"database/sql"
	"errors"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"rdio-scanner/internal/infrastructure/persistence"
)

// This is the shutdown journey, and the only journey that runs the server as a subprocess:
// the interrupt handling is the process ending itself, which a server running inside the
// test process cannot show. The binary is built the way the deployment builds it, started on
// a folder the test owns, and interrupted with real signals.

// secondSignalWindow is how long the second interrupt is allowed to take: the point of
// forcing the exit is that the operator does not sit through the hang again.
const secondSignalWindow = 2 * time.Second

// TestTheFirstInterruptStopsTheServerCleanly is the half of the interrupt journey the
// server already answers: the operator's first interrupt stops the server the way the stop
// is meant to stop — every address shut down, the departure said, the stop recorded, and the
// process ends by running out of work.
func TestTheFirstInterruptStopsTheServerCleanly(t *testing.T) {
	base := t.TempDir()

	port := aFreeTCPPort(t)

	exe := buildServerBinary(t)

	console, cmd, ended, addr := startServerBinary(t, exe, base, port)

	waitForServerAnswer(t, addr)

	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}

	select {
	case err := <-ended:
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 0 {
				t.Fatalf("the process ended with %v, want a clean exit after the interrupt", err)
			}
		}

	case <-time.After(60 * time.Second):
		t.Fatal("the server did not stop after the interrupt")
	}

	if !strings.Contains(console.String(), "the server stopped") {
		t.Errorf("the terminal holds %q, want the stop said on it", console.String())
	}
}

// TestASecondInterruptEndsAServerThatCannotStop: the stop
// is the polite way out, and a stop that cannot finish — a log write the database will not
// take, a goroutine that never returns — leaves the operator holding a running server with
// no answer. Today the second interrupt is ignored and the third is SIGKILL, with no trace
// of why. The second interrupt ends the process within its window and says so on the
// terminal the operator is reading.
func TestASecondInterruptEndsAServerThatCannotStop(t *testing.T) {
	base := t.TempDir()

	port := aFreeTCPPort(t)

	exe := buildServerBinary(t)

	console, cmd, ended, addr := startServerBinary(t, exe, base, port)

	waitForServerAnswer(t, addr)

	// 	// Make the stop one that cannot finish: lock the database the stop's last entry is
	// 	// written into, so the stopping server sits on the log write it cannot make. This is
	// 	// the shape of every stop that hangs — the operator's third press is SIGKILL, and
	// 	// nothing is left behind saying why.
	db := lockTheServerLog(t, base)
	defer db.Close()

	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("sending the first interrupt: %v", err)
	}

	// The margin is the stop's own walk to the write it cannot make — shutting the
	// addresses, ending the connections, reaching the log. The assertion below measures
	// from the second signal, not from this pause.
	time.Sleep(500 * time.Millisecond)

	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("sending the second interrupt: %v", err)
	}

	asked := time.Now()

	select {
	case err := <-ended:
		if elapsed := time.Since(asked); elapsed > secondSignalWindow {
			t.Fatalf("the process ended %s after the second interrupt, want it ended within %s: "+
				"the second interrupt is ignored today, and the operator's next press is SIGKILL",
				elapsed.Round(time.Millisecond), secondSignalWindow)
		}

		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			t.Errorf("the process ended with %v, want exit status 1 from the forced end", err)
		}

	case <-time.After(secondSignalWindow + 30*time.Second):
		t.Fatal("the process did not end after the second interrupt, want it ended within 2 seconds with a console line")
	}

	if !strings.Contains(console.String(), "ending the process") {
		t.Errorf("the terminal holds %q, want the second interrupt said on it", console.String())
	}
}

// buildServerBinary builds the command the way the deployment builds it, so the journey
// runs the same entry point an operator starts.
func buildServerBinary(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	exe := filepath.Join(dir, "rdio-scanner")

	build := exec.Command("go", "build", "-o", exe, "rdio-scanner/cmd/rdio-scanner")

	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("building the server binary: %v\n%s", err, out)
	}

	return exe
}

// startServerBinary starts the built server on a folder the test owns, reading its terminal
// into a buffer the test can read from more than one goroutine at a time, and hands back
// the channel the process's ending arrives on.
func startServerBinary(t *testing.T, exe, base, port string) (*lockedBuffer, *exec.Cmd, chan error, string) {
	t.Helper()

	addr := "127.0.0.1:" + port

	cmd := exec.Command(exe, "-base_dir", base, "-listen", addr)

	console := &lockedBuffer{}

	cmd.Stdout = console
	cmd.Stderr = console

	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the server binary: %v", err)
	}

	ended := make(chan error, 1)

	go func() {
		ended <- cmd.Wait()
	}()

	t.Cleanup(func() {
		// The journeys end the process themselves; this is the net under a journey that
		// failed early, so no built server outlives the test that started it.
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}

		select {
		case <-ended:

		case <-time.After(30 * time.Second):
		}
	})

	return console, cmd, ended, addr
}

// waitForServerAnswer waits until the built server is taking connections, which is the
// journey's first fact: a server that never answered cannot be interrupted.
func waitForServerAnswer(t *testing.T, addr string) {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)

	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			conn.Close()

			return
		}

		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("the server never answered on %s", addr)
}

// lockTheServerLog takes the exclusive lock on the database the server writes its log into
// and holds it: every log write the stopping server attempts then sits on the lock, which
// is the stop that cannot finish.
func lockTheServerLog(t *testing.T, base string) *sql.DB {
	t.Helper()

	db, err := persistence.Open(persistence.Settings{
		Dialect:  persistence.DialectSqlite,
		FilePath: filepath.Join(base, "rdio-scanner.db"),
	})
	if err != nil {
		t.Fatalf("opening the server's database: %v", err)
	}

	if _, err := db.Exec("BEGIN EXCLUSIVE"); err != nil {
		db.Close()

		t.Fatalf("locking the server's database: %v", err)
	}

	return db
}

// aFreeTCPPort picks a port the operating system just handed out and gives it back, so the
// built server answers on an address no other test is holding.
func aFreeTCPPort(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}

	defer listener.Close()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("reading the free port: %v", err)
	}

	return port
}

// lockedBuffer is a console a test can read from more than one goroutine at a time.
type lockedBuffer struct {
	mutex sync.Mutex
	buf   bytes.Buffer
}

func (buffer *lockedBuffer) Write(b []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()

	return buffer.buf.Write(b)
}

func (buffer *lockedBuffer) String() string {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()

	return buffer.buf.String()
}
