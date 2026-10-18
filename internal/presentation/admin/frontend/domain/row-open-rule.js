// The rule the administration webapp's row-list sections apply to their rows: one row open at a time.
//
// The sections show lists the operator edits row by row — the systems tree the systems, the sites,
// the talkgroups, the units; the hand-built list sections the access codes, the API keys, the
// dirwatches, the downstreams, the groups, the tags. Opening a row shrinks the rows that were open,
// so the list stays short and the operator reaches the save bar without scrolling past the rows they
// opened on the way.
//
// Both functions are arithmetic over the section's own state. The open rows are the keys the section
// remembers its rows by; the measurements are the opened row's position in the page before and after
// the siblings collapse. Neither reads the DOM, the store, or the clock: the wiring that hands them
// the rows lives in the row builders, so the rule's boundary cases — the row the operator opened, the
// row that closed, a missing measurement — are proven here, where a failure names the rule alone.

// Every open row the section has to close when the operator opens one: the open rows that are not the
// row they opened. The row the operator opened is never among them, so the list is left with exactly
// one open row.
//
// A row the operator closed is not among the open rows, and closes nothing: shrinking is what opening
// a row does, and a row leaving the list is not an opening.
export function rowsToClose(openKeys, openedKey) {
  if (!openKeys.includes(openedKey)) return [];

  return openKeys.filter((key) => key !== openedKey);
}

// The scroll the section gives back when the rows above the opened row collapse: the distance the
// opened row moved, added to the scroller's offset. Zero when a measurement is missing, which is the
// case outside a browser and the case for a row that is not laid out.
export function scrollCompensation(topBefore, topAfter) {
  if (!Number.isFinite(topBefore) || !Number.isFinite(topAfter)) return 0;

  return topAfter - topBefore;
}
