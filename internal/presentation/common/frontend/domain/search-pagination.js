// Search pagination: server pages of 200 vs client pages of 10
// (legacy search.component.ts refreshResults/searchCalls).

export function serverOffset(pageIndex, clientPage, serverPage) {
  return Math.floor((pageIndex * clientPage) / serverPage) * serverPage;
}

export function needsRefetch(windowStart, currentServerOffset, serverPage) {
  return windowStart >= currentServerOffset + serverPage || windowStart < currentServerOffset;
}

// Legacy quirk fixed: results.slice(from % limit, to % limit + 1) returns an
// empty window when the client window wraps the server page boundary; the
// intended wrap-around concatenation is implemented here.
export function clientSlice(results, windowStart, windowEnd, serverPage, clientPage) {
  const from = windowStart % serverPage;
  const to = windowEnd % serverPage + 1;

  const calls = from < to
    ? results.slice(from, to)
    : [...results.slice(from), ...results.slice(0, to)];

  while (calls.length < clientPage) calls.push(null);

  return calls;
}
