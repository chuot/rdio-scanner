// dashboard derivations: pure functions over the snapshot and stored config; no
// DOM, clock, or fetch; the inventory is derived here, not read from the server.

export const STATUS_REFRESH_DELAY = 5000;

// three intervals: poll tolerance plus a margin for a busy server; past that the
// page calls its numbers stale.
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

// counts from the stored config: what was asked to run, beside the live block's
// counts of what is running.
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

    // totals across all systems; per-system rows show which system has none.
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

// what each configured system is called: the pair is keyed by the `systemRef` every call and every
// busiest row carries, which is a different number from the store row's own `id` the inventory keys its
// per-system rows by. A system carrying no reference cannot name a row, and a system carrying no name
// does not name one either: a row keeps its reference rather than reading as a blank.
export function deriveSystemNames(config) {
  const named = rows(config?.systems).filter((system) => system?.systemRef !== null && system?.systemRef !== undefined && system?.label);

  return new Map(named.map((system) => [system.systemRef, system.label]));
}

const working = (key, label) => ({ key, label, state: 'ok', reason: '' });

const needsAttention = (key, label, reason) => ({ key, label, state: 'attention', reason });

// a block that did not answer is not one answering zero: unknown is not nothing
const notKnown = (key, label, reason) => ({ key, label, state: 'unknown', reason });

// the dashboard's only card-state rules: the word, the sentence, and the test
// in one place; a card never seeks attention on a normal state.
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

      // calls recorded, ingestion configured, nothing for a day: every in-window figure
      // is zero; only this comparison names it.
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

    // warnings alone never seek attention: the log carries them; a daily alert would
    // stop meaning anything.
    return working(key, label);
  });
}

// the chart's 24 hours, oldest first, with each bar's ratio against the peak hour.
// the server fills the window with zeros, so every deployment gets the same shape.
export function buildHourSeries(status) {
  const hours = rows(status?.activity?.hours);

  const counts = hours.map((hour) => Number(hour?.count) || 0);
  const peak = counts.reduce((highest, count) => Math.max(highest, count), 0);

  return hours.map((hour, index) => ({
    hour: hour?.start ?? '',
    count: counts[index],
    // the bar's share of the busiest hour: an empty window has no peak, and dividing
    // by zero stops the render.
    ratio: peak > 0 ? counts[index] / peak : 0,
    label: hourLabel(hour?.start),
  }));
}

// the instant is the server's; the label shows the operator's own hour.
function hourLabel(instant) {
  const date = new Date(instant);

  if (Number.isNaN(date.getTime())) return '';

  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
}

// the chart's text alternative: the sentence a screen reader reads, operators too.
export function summarizeHourSeries(series) {
  const total = series.reduce((sum, hour) => sum + hour.count, 0);

  if (total === 0) return 'No calls in the last 24 hours.';

  const busiest = series.reduce((best, hour) => (hour.count > best.count ? hour : best), series[0]);

  return `${total} ${total === 1 ? 'call' : 'calls'} in the last 24 hours. The busiest hour was ${busiest.label} with ${busiest.count}.`;
}

// uptime names its two largest units; seconds of precision is not a fact.
export function formatUptime(seconds) {
  if (typeof seconds !== 'number' || !Number.isFinite(seconds) || seconds < 0) return '';

  const total = Math.floor(seconds);

  if (total < 60) return `${total}s`;

  if (total < 3600) return `${Math.floor(total / 60)}m ${String(total % 60).padStart(2, '0')}s`;

  if (total < 86400) return `${Math.floor(total / 3600)}h ${Math.floor((total % 3600) / 60)}m`;

  return `${Math.floor(total / 86400)}d ${Math.floor((total % 86400) / 3600)}h`;
}

// a database size in the unit that fits; an unknown size answers in words, never
// a zero that reads as an empty database.
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

// whether the held answer still matches the page's moment; the clock is handed in,
// not read, which is also what makes this testable.
export function isStale(fetchedAt, now) {
  if (!fetchedAt || !now) return false;

  const read = Date.parse(fetchedAt);
  const at = Date.parse(now);

  if (Number.isNaN(read) || Number.isNaN(at)) return false;

  return at - read >= STATUS_STALE_AFTER_MS;
}
