// the log view's rules: the subsystems offered, the filters' request, the counts;
// the element renders, the api requests, this owns what the filters mean
// see the log-subsystem-vocabulary maintainer note.

import { serverOffset } from '../../../common/frontend/domain/search-pagination.js';

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
  ['mcp', 'Assistant surface'],
  ['calls', 'Calls'],
  ['legacy', 'From the previous server'],
];

// an unknown subsystem keeps its own name: a readable name beats a blank.
export function logSourceLabel(source) {
  const found = LOG_SOURCES.find(([value]) => value === source);

  return found ? found[1] : source;
}

// the select's choices: every subsystem the server has, plus the all choice.
export function logSearchFilterOptions() {
  return [['', 'All Subsystems'], ...LOG_SOURCES];
}

// unset filters are not sent: the server reads absent as no filter
// server pages of 200, client pages of 10: the covering page loads lazily
// see the log-search-paging maintainer note.
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

// counts keep the server's order, noisiest first; an unnamed subsystem shows
// own name
// see the log-subsystem-vocabulary maintainer note.
export function logSourceCounts(sources) {
  return (sources ?? []).map((entry) => ({
    source: entry.source,
    label: logSourceLabel(entry.source),
    count: entry.count,
  }));
}
