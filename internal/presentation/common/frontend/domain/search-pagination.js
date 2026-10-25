// Search pagination: server pages of 200, client pages of 10, as the
// legacy search component did.

export function serverOffset(pageIndex, clientPage, serverPage) {
  return Math.floor((pageIndex * clientPage) / serverPage) * serverPage;
}

export function needsRefetch(windowStart, currentServerOffset, serverPage) {
  return windowStart >= currentServerOffset + serverPage || windowStart < currentServerOffset;
}

// The client window that wraps the server page boundary is built by the
// wrap-around concatenation, not the legacy modulo slice.
export function clientSlice(results, windowStart, windowEnd, serverPage, clientPage) {
  const from = windowStart % serverPage;
  const to = windowEnd % serverPage + 1;

  const calls = from < to
    ? results.slice(from, to)
    : [...results.slice(from), ...results.slice(0, to)];

  while (calls.length < clientPage) calls.push(null);

  return calls;
}
