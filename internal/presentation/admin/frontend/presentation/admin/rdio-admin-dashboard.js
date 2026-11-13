// rdio-admin-dashboard: read of the server, not a form: no store slice,
// GET /api/admin/status on its own interval; this file only renders.

import { getAdminContext } from '../../application/admin-context.js';
import {
  DASHBOARD_CARDS,
  STATUS_REFRESH_DELAY,
  buildHourSeries,
  deriveCardStates,
  deriveInventory,
  deriveSystemNames,
  formatBytes,
  formatUptime,
  isStale,
  summarizeHourSeries,
} from '../../domain/dashboard-status.js';
import { createRefreshSchedule } from '../../domain/log-refresh.js';
import { formatMonthDay, formatTime } from '../../../../common/frontend/presentation/display-format.js';
import { announce, escapeHtml } from '../../../../common/frontend/presentation/ui/primitives.js';

// card state words, also carried as data-state for the stylesheet
// the word is what the operator reads: colour is never the only signal
const STATE_WORDS = { ok: 'Working', attention: 'Needs attention', unknown: 'Not known' };

// the words on the auto-refresh control, said in the same order the pressed attribute carries them
// the control's state is part of its name: an operator who cannot see the fill still knows which way it is set
const REFRESH_WORDS = { on: 'Auto-refresh on', off: 'Auto-refresh off' };

class RdioAdminDashboard extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    this.autoRefresh = true;
    this.closed = false;
    this.reading = null;

    // whether this failure has already been announced out loud
    // a server that keeps refusing is the same failure: said once
    this.failureAnnounced = false;

    // same schedule the log view uses: next poll scheduled before this one runs
    // a failed read does not end the refresh; an in-flight read is not stacked
    this.schedule = createRefreshSchedule({
      poll: () => this.read(),
      delay: STATUS_REFRESH_DELAY,
      isBusy: () => this.reading !== null,
    });

    this.innerHTML = `
      <div class="rdio-admin-card dashboard">
        <div class="row top">
          <p>What this server is doing right now.</p>
          <div class="actions">
            <button type="button" class="rdio-button-mini rdio-dashboard-refresh">Read again</button>
            <button type="button" class="rdio-button-mini rdio-dashboard-auto-refresh"></button>
          </div>
        </div>
        <p class="rdio-dashboard-strip"></p>
        <p class="rdio-dashboard-read"></p>
        <div class="rdio-dashboard-body"></div>
      </div>
    `;

    this.body = this.querySelector('.rdio-dashboard-body');
    this.strip = this.querySelector('.rdio-dashboard-strip');
    this.readLine = this.querySelector('.rdio-dashboard-read');

    this.querySelector('.rdio-dashboard-refresh').addEventListener('click', () => this.readAgain());

    // auto-refresh is one control, and its pressed state is exposed for screen readers
    // its words and its attribute are one state: the only place either is written, so they cannot drift
    const auto = this.querySelector('.rdio-dashboard-auto-refresh');

    const showRefreshState = () => {
      auto.setAttribute('aria-pressed', String(this.autoRefresh));
      auto.textContent = this.autoRefresh ? REFRESH_WORDS.on : REFRESH_WORDS.off;
    };

    showRefreshState();

    auto.addEventListener('click', () => {
      this.autoRefresh = !this.autoRefresh;

      showRefreshState();

      this.followThePage();
    });

    this.visibilityChanged = () => this.followThePage();
    document.addEventListener('visibilitychange', this.visibilityChanged);

    this.unsubscribe = getAdminContext().store.subscribe(['status', 'config', 'auth'], () => this.render());

    this.render();

    this.followThePage();

    // read on open: the operator has not asked and should not wait an interval
    this.read();
  }

  disconnectedCallback() {
    this.closed = true;

    this.schedule?.stop();

    document.removeEventListener('visibilitychange', this.visibilityChanged);

    this.unsubscribe?.();

    this.reading = null;
  }

  // one read at a time: the use case holds the read in flight
  // the schedule, a config push, and the operator's button share it
  read() {
    const reading = getAdminContext().useCases.loadStatus();

    this.reading = reading;

    // in flight as long as the use case's read, however it ends
    // the page hands that same read back to whoever asked
    reading.finally(() => {
      if (this.reading === reading) this.reading = null;
    });

    return reading;
  }

  // a requested refresh is answered out loud; a refusal is said
  // once.
  async readAgain() {
    this.failureAnnounced = false;

    const status = await this.read();

    if (!status) return;

    if (status.failed) {
      this.announceTheFailure();

      return;
    }

    announce(`Read again at ${formatTime(new Date(status.fetchedAt))}.`);
  }

  // one owner for the failure sentence, said once per fall into the failure state
  // a screen reader is never told about a silent change of state
  announceTheFailure() {
    if (this.failureAnnounced) return;

    this.failureAnnounced = true;

    announce('The server did not answer the dashboard read.');
  }

  // polls while visible: a hidden page costs the server statements for nobody
  // coming back starts exactly one poll chain
  followThePage() {
    if (this.closed) return;

    if (this.autoRefresh && document.visibilityState !== 'hidden') this.schedule.start();
    else this.schedule.stop();

    // re-render on coming back: the clock kept moving while away,
    // and the first scheduled poll lands an interval late
    this.render();
  }

  render() {
    const { status, config, auth } = getAdminContext().store.getState();

    const snapshot = status?.snapshot;

    this.renderReadLine(status);

    if (status?.failed) {
      this.announceTheFailure();

      this.strip.textContent = '';

      // on a refused read the held numbers stay in state, off screen:
      // a figure with no moment attached is a figure the operator will believe
      this.body.innerHTML = `
        <p class="rdio-dashboard-failure">The server did not answer this read. The dashboard is not showing the numbers it is holding, because they are no longer current.</p>
      `;

      return;
    }

    // a successful read clears failureAnnounced, so the next failure is announced again
    this.failureAnnounced = false;

    if (!snapshot) {
      this.strip.textContent = '';

      this.body.innerHTML = `<p class="rdio-dashboard-reading">The dashboard is reading the server.</p>`;

      return;
    }

    const inventory = deriveInventory(config);
    const states = deriveCardStates(snapshot, inventory, { passwordNeedChange: !!auth?.passwordNeedChange });
    const series = buildHourSeries(snapshot);

    // the busiest rows are named here, where the configuration is read: the card prints the name each
    // row is handed and looks nothing up
    const busiestRows = nameBusiestRows(snapshot, deriveSystemNames(config));

    this.strip.textContent = stripSentence(snapshot, series);

    this.body.innerHTML = `<div class="rdio-dashboard-cards">${states.map((state) => cardMarkup(state, snapshot, inventory, series, busiestRows)).join('')}</div>`;
  }

  // The line that makes every figure on the page a figure with a moment attached to it.
  renderReadLine(status) {
    const at = status?.fetchedAt ? formatTime(new Date(status.fetchedAt)) : '';

    if (!at) {
      this.readLine.textContent = '';

      return;
    }

    // stale past three intervals; a refused read is stale from the first refusal
    // on.
    const stale = status.failed || isStale(status.fetchedAt, new Date().toISOString());

    this.readLine.textContent = stale
      ? `Read at ${at}, and this page is no longer keeping it current.`
      : `Read at ${at}.`;
  }
}

// the strip carries the same figures as the cards: no one-line self-contradiction
function stripSentence(snapshot, series) {
  const parts = [`up ${formatUptime(snapshot.server?.uptimeSeconds)}`];

  if (snapshot.live) parts.push(`${snapshot.live.listeners} listening`);

  const total = series.reduce((sum, hour) => sum + hour.count, 0);

  if (snapshot.activity) parts.push(`${total} ${total === 1 ? 'call' : 'calls'} in the last 24 hours`);

  return `Running · ${parts.join(' · ')}`;
}

// each busiest row reads the name of the system its reference belongs to, or the reference itself when
// the configuration no longer names that system: a removed system, or calls older than the system that
// recorded them. The card never prints a row with an empty System cell.
function nameBusiestRows(snapshot, systemNames) {
  return (snapshot.activity?.busiest ?? []).map((row) => ({
    ...row,
    systemName: systemNames.get(row.systemRef) ?? row.systemRef,
  }));
}

function cardMarkup(state, snapshot, inventory, series, busiestRows) {
  const heading = `rdio-dashboard-${state.key}-heading`;

  const figures = cardFigures(state.key, snapshot, inventory, series, busiestRows);

  return `
    <section class="rdio-dashboard-card ${state.state}" data-card="${state.key}" aria-labelledby="${heading}">
      <h3 id="${heading}">${state.label}</h3>
      <p class="rdio-dashboard-state" data-state="${state.state}">${STATE_WORDS[state.state]}</p>
      ${state.reason ? `<p class="rdio-dashboard-reason">${state.reason}</p>` : ''}
      ${figures}
    </section>
  `;
}

function figures(rows) {
  return `<dl class="rdio-dashboard-figures">${rows
    .map(([term, value]) => `<div><dt>${term}</dt><dd class="rdio-dashboard-figure">${value}</dd></div>`)
    .join('')}</dl>`;
}

function cardFigures(key, snapshot, inventory, series, busiestRows) {
  if (key === 'server') {
    const server = snapshot.server ?? {};

    return figures([
      ['Uptime', formatUptime(server.uptimeSeconds)],
      ['Version', server.version ?? ''],
      ['Runs as', server.docker ? 'Container' : 'Host'],
      ['Serving folder', server.baseDir ?? ''],
      ['Database', formatBytes(server.databaseBytes, server.databaseKnown !== false)],
    ]);
  }

  if (key === 'ingestion') {
    const live = snapshot.live ?? {};

    return `${figures([
      ['Directory watchers', `${live.dirwatchersRunning} of ${inventory.dirwatch.enabled} enabled`],
      ['Watches disabled', inventory.dirwatch.disabled],
      ['Downstreams', `${inventory.downstreams.enabled} of ${inventory.downstreams.configured} enabled`],
      ['Held back', live.heldCalls],
      ['Audio conversion', live.audioConversion ? 'Available' : 'Not available'],
    ])}
    <p class="rdio-dashboard-note">The watchers counted are the ones this server started, not the ones the configuration names.</p>`;
  }

  if (key === 'listeners') {
    const live = snapshot.live ?? {};

    return `${figures([
      ['Listening', live.listeners],
      ['Administrator sessions', live.administratorSessions],
    ])}
    <p class="rdio-dashboard-note">The administrator sessions include this page.</p>`;
  }

  if (key === 'activity') return activityCard(snapshot, series);
  if (key === 'busiest') return busiestCard(busiestRows);

  return logHealthCard(snapshot);
}

function activityCard(snapshot, series) {
  const activity = snapshot.activity ?? {};

  const total = series.reduce((sum, hour) => sum + hour.count, 0);

  const hours = series
    .map((hour) => {
      // bar width as a share of the busiest hour
      // the stylesheet floor keeps a zero bar visible as an hour, not a gap
      const width = Math.round(hour.ratio * 100);

      return `
      <li class="rdio-dashboard-hour-item">
        <span class="rdio-dashboard-bar" aria-hidden="true" style="width: ${width}%"></span>
        <span class="rdio-dashboard-hour">${hour.label}</span>
        <span class="rdio-dashboard-count">${hour.count} ${hour.count === 1 ? 'call' : 'calls'}</span>
      </li>`;
    })
    .join('');

  return `${figures([
    ['Last 24 hours', `${total} ${total === 1 ? 'call' : 'calls'}`],
    ['Last hour', activity.callsLastHour ?? 0],
    ['Today', activity.callsToday ?? 0],
    ['Last call', activity.lastCallAt ? `${formatMonthDay(new Date(activity.lastCallAt))} ${formatTime(new Date(activity.lastCallAt))}` : 'none'],
  ])}
  <p class="rdio-dashboard-summary">${summarizeHourSeries(series)}</p>
  <ul class="rdio-dashboard-hours">${hours}</ul>`;
}

// the rows arrive already named by the configuration the page holds. The system's name is an operator's
// own words from the systems form, and the talkgroup's name is text the ingest path wrote from call
// metadata: both are rendered as the text they are.
function busiestCard(rows) {
  if (rows.length === 0) {
    return `<p class="rdio-dashboard-empty">No calls in the last 24 hours.</p>`;
  }

  const markup = rows
    .map((row) => `<tr><td>${escapeHtml(row.name || `T-${row.talkgroupRef}`)}</td><td>${escapeHtml(row.systemName)}</td><td>${row.count}</td></tr>`)
    .join('');

  return `
    <table class="rdio-dashboard-busiest">
      <thead><tr><th scope="col">Talkgroup</th><th scope="col">System</th><th scope="col">Calls</th></tr></thead>
      <tbody>${markup}</tbody>
    </table>`;
}

function logHealthCard(snapshot) {
  const logs = snapshot.logs ?? {};

  const sources = (logs.sources ?? [])
    .map((source) => `<li>${source.source} — ${source.errors} ${source.errors === 1 ? 'error' : 'errors'}, ${source.warnings} ${source.warnings === 1 ? 'warning' : 'warnings'}</li>`)
    .join('');

  return `${figures([
    ['Errors', logs.errors ?? 0],
    ['Warnings', logs.warnings ?? 0],
  ])}
  ${sources ? `<p class="rdio-dashboard-note">Which parts of the server wrote the log over the last 24 hours:</p>
  <ul class="rdio-dashboard-sources">${sources}</ul>` : '<p class="rdio-dashboard-empty">Nothing was written to the log over the last 24 hours.</p>'}`;
}

customElements.define('rdio-admin-dashboard', RdioAdminDashboard);
