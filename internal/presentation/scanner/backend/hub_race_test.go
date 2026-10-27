package backend

import (
	"context"
	"sync"
	"testing"

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
