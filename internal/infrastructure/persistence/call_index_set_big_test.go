//go:build live

// The read budgets at the two scales the figures in the budgets were read at: the same fixture shape, the
// same inventoried statements and the same ceilings the ordinary suite checks at ten thousand calls, on a
// history an operator would actually have. The `live` tag is what keeps both the fixture and the run out of
// the ordinary suite, which pays for neither, and it is the tag the repository's other large-database test
// already carries.

package persistence

import (
	"fmt"
	"testing"
)

// TestHistoryReadsStayWithinTheirBudgets holds every inventoried read of a deployment's history inside the
// ceiling its shape carries at sixty thousand calls, and again at one hundred and fifty thousand — the two
// scales the figures of the read budgets were read at. The plan-shape tests read the same statements over a
// forty-call database, where a read is cheap enough that a plan choice could go unnoticed; these are the
// scales at which a read that loses a key stops being merely slower and becomes a scan of the whole history.
//
// Each scale runs as its own subtest, so the file one scale seeded is released before the next one seeds
// its own.
func TestHistoryReadsStayWithinTheirBudgets(t *testing.T) {
	for _, calls := range []int{historyBudgetScale, historyUpperBudgetScale} {
		t.Run(fmt.Sprintf("%d calls", calls), func(t *testing.T) {
			db := historyFixtureDatabase(t, calls)

			span := historySpanOfTheDatabase(t, db)

			values := historyPlanValues(t, db, span)

			inventory := callHistoryStatementInventory(span)

			assertHistoryReadsWithinTheirBudgets(t, db, inventory, values, calls)
		})
	}
}
