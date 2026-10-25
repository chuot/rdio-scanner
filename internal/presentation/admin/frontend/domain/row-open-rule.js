// row-list sections keep one row open at a time; opening one closes the others;
// both functions are arithmetic over section state: no DOM, store, or clock
// see the single-open-row-rule maintainer note.

// close the open rows that are not the opened row: one open row remains; a row the
// operator closed closes nothing.
export function rowsToClose(openKeys, openedKey) {
  if (!openKeys.includes(openedKey)) return [];

  return openKeys.filter((key) => key !== openedKey);
}

// the scroll a section gives back when rows above collapse: how far the opened row
// moved, plus the scroller offset; zero when a measurement is missing.
export function scrollCompensation(topBefore, topAfter) {
  if (!Number.isFinite(topBefore) || !Number.isFinite(topAfter)) return 0;

  return topAfter - topBefore;
}
