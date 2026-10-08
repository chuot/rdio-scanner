// The administrator's log view's rules: which subsystems the view offers, the request its filters produce,
// and what the per-subsystem counts become in the view. No DOM and no fetch — the logs element owns
// rendering, the api owns the request, this owns what the filters mean.
//
// The subsystem names are the server's vocabulary (internal/domain/log.go), copied here deliberately: the
// server does not publish the list, and a select built from whatever subsystems happen to be in today's
// answer would hide the one the operator is looking for on a day it wrote nothing. The copy is pinned by a
// Go test (cmd/rdio-scanner/log_vocabulary_test.go), so a server that gains a subsystem cannot leave the
// administrator's view silently unable to ask for it.

import { serverOffset } from '../../shared/domain/search-pagination.js';

export const LOG_SERVER_PAGE = 200;
export const LOG_CLIENT_PAGE = 10;

// In the order the server lists them, which is the order the view shows them.
export const LOG_SOURCES = [
  ['server', 'Server'],
  ['config', 'Configuration'],
  ['dirwatch', 'Directory watchers'],
  ['downstream', 'Downstreams'],
  ['delayer', 'Delayed calls'],
  ['ingest', 'Call ingest'],
  ['login', 'Logins'],
  ['access', 'Access codes'],
  ['livefeed', 'Live feed'],
  ['ffmpeg', 'Audio conversion'],
  ['upload', 'Uploads'],
  ['prune', 'Retention'],
  ['logs', 'Log search'],
  ['database', 'Database'],
  ['admin', 'Administration'],
  ['calls', 'Calls'],
  ['legacy', 'From the previous server'],
];

// logSourceLabel names a subsystem the way the view names it. A name the server has and this copy does not
// is labelled with its own name: the operator is better served by a name they can read than by a blank, and
// the pin above is what keeps that case rare rather than merely handled.
export function logSourceLabel(source) {
  const found = LOG_SOURCES.find(([value]) => value === source);

  return found ? found[1] : source;
}

// logSearchFilterOptions are the select's choices: every subsystem the server has, and the choice that
// asks for all of them.
export function logSearchFilterOptions() {
  return [['', 'All Subsystems'], ...LOG_SOURCES];
}

// logSearchOptions is the request the filters produce. A filter the operator did not set is not sent: the
// server reads an absent criterion as no filter, and sending an empty one would be the view claiming a
// filter it does not have.
//
// The paging is the log view's existing one — server pages of 200, client pages of 10 — and the server
// page a client page falls inside is the one asked for, so paging backwards re-asks the page it needs
// instead of drifting.
export function logSearchOptions({ pageIndex, sort, level, source, text, date }) {
  const options = {
    limit: LOG_SERVER_PAGE,
    offset: serverOffset(pageIndex, LOG_CLIENT_PAGE, LOG_SERVER_PAGE),
    sort,
  };

  if (level) options.level = level;
  if (source) options.source = source;
  if (text) options.text = text;
  if (date) options.date = date;

  return options;
}

// logSourceCounts turns the counts the search answered into what the view shows. They arrive in the order
// the server reported them — most first — and stay in it: the point of the order is that the noisiest part
// of the server is the first thing an operator is offered.
//
// A subsystem the server reported and this copy does not name is still shown, under its own name, because
// a count the view hides is a part of the server the operator cannot narrow to.
export function logSourceCounts(sources) {
  return (sources ?? []).map((entry) => ({
    source: entry.source,
    label: logSourceLabel(entry.source),
    count: entry.count,
  }));
}
