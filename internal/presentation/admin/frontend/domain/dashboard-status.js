// The dashboard's own derivations: the counts it reads out of the configuration the page already holds, the
// one word each card answers with and the sentence that says why, the 24 hours the chart is drawn over, and
// the words every figure is shown in.
//
// Pure functions over the snapshot the server answers and the configuration the store already carries. No
// DOM, no clock, no fetch: the element hands in the snapshot, the configuration, and — for the one function
// that needs it — the instant it is rendering at. Keeping the rules here is what lets the sentence a
// sighted operator reads and the sentence a screen reader reads be the same sentence, and what lets them be
// read without a browser.
//
// The inventory is derived here and not read from the server: the page already holds the whole
// configuration, and a second source for a count the page can work out itself is a count that can disagree
// with the page.

export const STATUS_REFRESH_DELAY = 5000;

// Three intervals: one interval of tolerance for a poll that is on its way, and one more for a read that
// has to reach a server that may be busy. Past that, the numbers on the page are the numbers of a moment
// the page stopped reading, and the page says so.
export const STATUS_STALE_AFTER_MS = 3 * STATUS_REFRESH_DELAY;

// The cards, in the order the operator reads the deployment: what the server is, whether anything is
// arriving, who is hearing it, what arrived lately, what arrived most, and what the server had to
// complain about.
export const DASHBOARD_CARDS = [
  { key: 'server', label: 'Server' },
  { key: 'ingestion', label: 'Ingestion' },
  { key: 'listeners', label: 'Listeners' },
  { key: 'activity', label: 'Call activity' },
  { key: 'busiest', label: 'Busiest talkgroups' },
  { key: 'logHealth', label: 'Log health' },
];

const rows = (value) => (Array.isArray(value) ? value : []);

const countOf = (value) => rows(value).length;

const enabledOf = (value) => rows(value).filter((row) => !row?.disabled).length;

const disabledOf = (value) => rows(value).filter((row) => row?.disabled).length;

// The counts the dashboard shows beside the live figures: the configuration the operator has written, read
// back as numbers. The live block says what is running; these say what was asked to run, and the distance
// between them is what a watch that is not watching looks like.
export function deriveInventory(config) {
  const systems = rows(config?.systems).map((system) => ({
    id: system?.id ?? null,
    label: system?.label ?? '',
    talkgroups: countOf(system?.talkgroups),
    sites: countOf(system?.sites),
    units: countOf(system?.units),
  }));

  const total = (key) => systems.reduce((sum, system) => sum + system[key], 0);

  return {
    systems: systems.length,
    groups: countOf(config?.groups),
    accessCodes: countOf(config?.access),
    apikeys: countOf(config?.apikeys),

    // The totals across every system, which is the answer to "how many talkgroups does this deployment
    // have", while the per-system rows are the answer to "which system has none".
    talkgroups: total('talkgroups'),
    sites: total('sites'),
    units: total('units'),
    perSystem: systems,

    dirwatch: {
      configured: countOf(config?.dirwatch),
      enabled: enabledOf(config?.dirwatch),
      disabled: disabledOf(config?.dirwatch),
    },
    downstreams: {
      configured: countOf(config?.downstreams),
      enabled: enabledOf(config?.downstreams),
      disabled: disabledOf(config?.downstreams),
    },
  };
}

const working = (key, label) => ({ key, label, state: 'ok', reason: '' });

const needsAttention = (key, label, reason) => ({ key, label, state: 'attention', reason });

// A snapshot that did not answer a block is not a snapshot that answered zero. Saying "not known" is the
// only thing the page can say honestly, and it is a different sentence from "nothing".
const notKnown = (key, label, reason) => ({ key, label, state: 'unknown', reason });

// The card states: the dashboard's only rules, all of them here, so that the word on a card, the sentence
// under it, and the test that proves both are decided in one place.
//
// A card that is never `attention` on a state that is normal — nobody listening, no talkgroups busy — is a
// deliberate choice: a card that cries at a quiet deployment is a card the operator learns to ignore, and
// then it is not there for the deployment that really is broken.
export function deriveCardStates(status, inventory, { passwordNeedChange = false } = {}) {
  const server = status?.server;
  const live = status?.live;
  const activity = status?.activity;
  const logs = status?.logs;

  const watches = inventory?.dirwatch ?? { configured: 0, enabled: 0, disabled: 0 };
  const ingestsSomething = watches.configured > 0 || (inventory?.apikeys ?? 0) > 0;

  return DASHBOARD_CARDS.map(({ key, label }) => {
    if (key === 'server') {
      if (!server) return notKnown(key, label, 'The status read did not answer what this server is.');
      if (passwordNeedChange) return needsAttention(key, label, 'This server is still using the default administrator password.');

      return working(key, label);
    }

    if (key === 'ingestion') {
      if (!live) return notKnown(key, label, 'The status read did not answer what this server is running.');

      if (watches.enabled > 0 && live.dirwatchersRunning < watches.enabled) {
        return needsAttention(
          key,
          label,
          `${watches.enabled} directory watches are enabled and ${live.dirwatchersRunning} ${live.dirwatchersRunning === 1 ? 'is' : 'are'} watching.`,
        );
      }

      if (!ingestsSomething && live.dirwatchersRunning === 0) {
        return needsAttention(key, label, 'Nothing is configured to ingest, and nothing is watching.');
      }

      if (!live.audioConversion) {
        return needsAttention(key, label, 'This server has no working audio converter, so recordings are stored exactly as they arrive.');
      }

      return working(key, label);
    }

    if (key === 'listeners') {
      if (!live) return notKnown(key, label, 'The status read did not answer who is listening.');

      // A count of zero listeners is a quiet deployment, not a broken one.
      return working(key, label);
    }

    if (key === 'activity') {
      if (!activity) return notKnown(key, label, 'The status read did not answer what this server recorded.');

      const inWindow = rows(activity.hours).reduce((sum, hour) => sum + (Number(hour?.count) || 0), 0);

      // The case every other figure on the page misses: the deployment has calls in it, ingestion is
      // configured, and nothing has arrived for a day. Every number in the window is a zero and the server
      // is healthy, so only this comparison names it.
      if (inWindow === 0 && activity.lastCallAt && ingestsSomething) {
        return needsAttention(key, label, 'Calls were recorded before, but nothing has arrived in the last 24 hours.');
      }

      return working(key, label);
    }

    if (key === 'busiest') {
      if (!activity) return notKnown(key, label, 'The status read did not answer what this server recorded.');

      return working(key, label);
    }

    if (!logs) return notKnown(key, label, 'The status read did not answer what this server wrote to the log.');

    if (logs.errors > 0) {
      return needsAttention(key, label, `${logs.errors} ${logs.errors === 1 ? 'error' : 'errors'} and ${logs.warnings} ${logs.warnings === 1 ? 'warning' : 'warnings'} in the log over the last 24 hours.`);
    }

    // Warnings alone are a server describing something awkward, which is what the log is for. A card that
    // asked for attention at every warning would be answered every day, and then it would stop meaning
    // anything.
    return working(key, label);
  });
}

// The chart's 24 hours, oldest first, each with the bar's height against the busiest hour and the label the
// list carries. The snapshot already hands over the whole window filled with zeros, so the chart has the
// same shape for every deployment and every quiet hour is visible as an hour.
export function buildHourSeries(status) {
  const hours = rows(status?.activity?.hours);

  const counts = hours.map((hour) => Number(hour?.count) || 0);
  const peak = counts.reduce((highest, count) => Math.max(highest, count), 0);

  return hours.map((hour, index) => ({
    hour: hour?.start ?? '',
    count: counts[index],
    // The bar's share of the busiest hour. An empty window has no busiest hour, and dividing by nothing is
    // not a quiet chart: it is a chart that stopped rendering.
    ratio: peak > 0 ? counts[index] / peak : 0,
    label: hourLabel(hour?.start),
  }));
}

// The hour's own words. The instant is the one the server named; the hour is the hour the operator's clock
// says it is, because a chart of the last 24 hours is read against the operator's own day.
function hourLabel(instant) {
  const date = new Date(instant);

  if (Number.isNaN(date.getTime())) return '';

  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
}

// The sentence the chart carries for anyone who is not reading the bars, and the sentence a sighted
// operator gets instead of adding up 24 numbers in their head.
export function summarizeHourSeries(series) {
  const total = series.reduce((sum, hour) => sum + hour.count, 0);

  if (total === 0) return 'No calls in the last 24 hours.';

  const busiest = series.reduce((best, hour) => (hour.count > best.count ? hour : best), series[0]);

  return `${total} ${total === 1 ? 'call' : 'calls'} in the last 24 hours. The busiest hour was ${busiest.label} with ${busiest.count}.`;
}

// The uptime in the words an operator reads rather than a number of seconds they have to convert. Each
// form names the two largest units, because "2h 14m 8s" is a number and "2h 14m" is a fact.
export function formatUptime(seconds) {
  if (typeof seconds !== 'number' || !Number.isFinite(seconds) || seconds < 0) return '';

  const total = Math.floor(seconds);

  if (total < 60) return `${total}s`;

  if (total < 3600) return `${Math.floor(total / 60)}m ${String(total % 60).padStart(2, '0')}s`;

  if (total < 86400) return `${Math.floor(total / 3600)}h ${Math.floor((total % 3600) / 60)}m`;

  return `${Math.floor(total / 86400)}d ${Math.floor((total % 86400) / 3600)}h`;
}

// A database size in the unit that fits it. The not-known answer is words and not a zero: a zero reads as
// an empty database to an operator who was never given a size at all, which is the case for every
// deployment writing into a database server this one does not own.
export function formatBytes(bytes, known) {
  const size = Number(bytes);

  if (!known || !Number.isFinite(size) || size < 0) return 'not known';

  if (size === 0) return '0 B';

  const units = [
    ['GB', 1024 * 1024 * 1024],
    ['MB', 1024 * 1024],
    ['KB', 1024],
  ];

  for (const [unit, factor] of units) {
    if (size >= factor) {
      const value = size / factor;

      return `${Number.isInteger(value) ? value : value.toFixed(1)} ${unit}`;
    }
  }

  return `${size} B`;
}

// Whether the answer the page is holding is still the answer the server would give. The page's own clock is
// handed in, because the comparison is between the page's moment and the moment the answer was read, and a
// module that read the clock itself could not be tested against either.
export function isStale(fetchedAt, now) {
  if (!fetchedAt || !now) return false;

  const read = Date.parse(fetchedAt);
  const at = Date.parse(now);

  if (Number.isNaN(read) || Number.isNaN(at)) return false;

  return at - read >= STATUS_STALE_AFTER_MS;
}
