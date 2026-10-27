package backend

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
)

// TestAGrantOfAccessRacesTheListenerReportsOfTheSameSession: the hub writes
// a session's access under the hub lock in GrantAccess, the path the
// authentication handlers take, while the listener reports for that session
// read the access with no lock and no atomic. The join report runs on the
// session's writer goroutine inside Hub.register, and the departure report
// runs inside Hub.disconnect on whichever goroutine ends the session, so the
// reader goroutine at the end of a live connection. Nothing but the hub lock
// would separate the write from those reads, and neither read takes it. Run
// under -race this test is red: the detector names the GrantAccess write
// racing the unlocked access read. Run without -race it passes. Until the
// unlocked reads take the hub lock, or the access fields move behind one
// synchronization point, this test stays red under -race.
func TestAGrantOfAccessRacesTheListenerReportsOfTheSameSession(t *testing.T) {
	f := newBareFixtureLogging(t, openSettings, &fakes.Logs{})

	id, conn := f.dial(t)

	// The grant side runs exactly as the production grant path does: the
	// authenticate-access handler re-presents an access code for a live
	// session. It repeats because a single grant crossing a single report is
	// a timing coincidence the race detector cannot count on, not because the
	// repetition is part of the defect.
	stop := make(chan struct{})
	var grants sync.WaitGroup

	grants.Add(1)

	go func() {
		defer grants.Done()

		for {
			select {
			case <-stop:
				return
			default:
				f.hub.GrantAccess(id, domain.Access{Code: "code-1", Ident: "Race Probe"})
			}
		}
	}()

	// The join report side: the first configuration makes the writer
	// goroutine register the session and read its access.
	f.register(t, id)

	// The departure report side: hanging up makes the session's reader
	// goroutine run disconnect, which reads the access the same way.
	conn.Close()

	// Reap the grant loop, then tear the fixture down with the hub's own
	// shutdown so no live session and no detached reader outlives the test.
	close(stop)
	grants.Wait()

	f.hub.Close(context.Background())
}

// TestTheHubClosesAListenerStillStalledAtTheShutdownDeadline: the server's
// shutdown hands the hub one deadline and expects the hub to have released
// every connection by then, closing the ones still open at the deadline the
// way the composition root's note promises. Hub.Close takes that context and
// never reads it: it only waits for each session's writer goroutine to come
// back on its own, and a writer stalled in a socket write comes back only
// when its own per-frame write patience runs out, ten seconds in production.
// So one listener that stopped reading keeps the whole shutdown waiting a
// frame's patience past the deadline that was supposed to bound it. This
// test shrinks that patience to five seconds, stalls one registered listener
// by filling its send queue without reading, and gives the close half a
// second: the close comes back roughly a full patience late, and until it
// enforces its own deadline by closing what it is waiting on, this test
// stays red.
func TestTheHubClosesAListenerStillStalledAtTheShutdownDeadline(t *testing.T) {
	f := newBareFixture(t, openSettings)

	// The stalled writer gives up on a frame after the hub's write patience.
	// Five seconds is far enough past this close's half-second deadline to
	// show the overshoot, and far shorter than waiting out the production
	// ten would make the test slow.
	f.hub.writeWait = 5 * time.Second

	id, _ := f.dial(t)
	f.register(t, id)

	f.hub.mutex.Lock()
	session := f.hub.sessions[id]
	f.hub.mutex.Unlock()

	// Stop reading and fill the queue: the writer takes the first frame and
	// blocks in the socket, and the goodbye the close wants to deliver has
	// nowhere to go until the writer gives up on the frame it is holding.
	big := strings.Repeat("x", 4*1024*1024)

fill:
	for {
		select {
		case session.send <- delivery{frame: Message{Command: CommandVersion, Payload: big}}:

		default:
			break fill
		}
	}

	deadline := time.Now().Add(500 * time.Millisecond)

	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	f.hub.Close(ctx)

	if elapsed := time.Since(deadline); elapsed > time.Second {
		t.Errorf("hub.Close finished %s after its deadline, want the stalled connection "+
			"closed at the deadline: %s past", elapsed.Round(time.Millisecond), elapsed.Round(time.Millisecond))
	}
}
