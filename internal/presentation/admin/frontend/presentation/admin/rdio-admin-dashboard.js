// rdio-admin-dashboard: the section a signed-in operator lands on. It is a read of the server, not a form:
// nothing on it saves anything, and it has no section slice in the store. The one read the page makes is
// GET /api/admin/status, on its own interval, again the moment the configuration changes, and not at all
// while the page is the one the operator is not looking at.
//
// The layout is six cards — the server, what is ingesting, who is listening, the last 24 hours of calls,
// the busiest talkgroups, and the log — each stating its own state in one word and, when it is not working,
// one sentence saying why. Every figure is text: the bars are the shape a sighted operator reads at a
// glance, and the list they come from says the same thing to an operator who is not looking at them.
//
// The rules that decide the state words, the sentences, and the words the figures are shown in are in
// domain/dashboard-status.js, so that the sentence a sighted operator reads and the sentence a screen
// reader reads are the same sentence. This owns the rendering and the keeping-current.

import { getAdminContext } from '../../application/admin-context.js';
import {
  DASHBOARD_CARDS,
  STATUS_REFRESH_DELAY,
  buildHourSeries,
  deriveCardStates,
  deriveInventory,
  formatBytes,
  formatUptime,
  isStale,
  summarizeHourSeries,
} from '../../domain/dashboard-status.js';
import { createRefreshSchedule } from '../../domain/log-refresh.js';
import { formatMonthDay, formatTime } from '../../../../common/frontend/presentation/display-format.js';
import { announce } from '../../../../common/frontend/presentation/ui/primitives.js';

// The word each card answers with. The state is also carried as data-state for the stylesheet, and the word
// is what the operator reads: colour is never the only signal.
const STATE_WORDS = { ok: 'Working', attention: 'Needs attention', unknown: 'Not known' };

class RdioAdminDashboard extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    this.autoRefresh = true;
    this.closed = false;
    this.reading = null;

    // Whether the failure the page is in has already been said out loud. The page falls into the failure
    // state once and says it once; a server that goes on refusing is the same failure it has already told
    // the operator about.
    this.failureAnnounced = false;

    // The same schedule the log view uses: the next poll is scheduled before this one runs, so a read that
    // fails does not quietly end a refresh the operator asked for, and a read already in flight is not
    // stacked on top of.
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
            <button type="button" class="rdio-button-mini rdio-dashboard-auto-refresh" aria-pressed="true">Auto-refresh</button>
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

    // One control for the operator's own decision, and it says which way that decision is: a control whose
    // pressed state is not exposed is a control a screen reader operator cannot read back.
    const auto = this.querySelector('.rdio-dashboard-auto-refresh');

    auto.addEventListener('click', () => {
      this.autoRefresh = !this.autoRefresh;

      auto.setAttribute('aria-pressed', String(this.autoRefresh));

      this.followThePage();
    });

    this.visibilityChanged = () => this.followThePage();
    document.addEventListener('visibilitychange', this.visibilityChanged);

    this.unsubscribe = getAdminContext().store.subscribe(['status', 'config', 'auth'], () => this.render());

    this.render();

    this.followThePage();

    // The page reads as soon as it is opened: an operator who has just signed in, or who has come back to
    // this section after an hour elsewhere, is looking at the answer to a question they are not asking.
    this.read();
  }

  disconnectedCallback() {
    this.closed = true;

    this.schedule?.stop();

    document.removeEventListener('visibilitychange', this.visibilityChanged);

    this.unsubscribe?.();

    this.reading = null;
  }

  // The page asks the server, and only once at a time: the use case holds the read it has in flight, so the
  // schedule, a configuration push, and the operator's own button cannot each start their own.
  read() {
    const reading = getAdminContext().useCases.loadStatus();

    this.reading = reading;

    // The read is in flight for as long as the use case's read is, whichever way that read ended. The page
    // hands back the use case's own read, so whoever asked for the answer is waiting on the same thing the
    // page is.
    reading.finally(() => {
      if (this.reading === reading) this.reading = null;
    });

    return reading;
  }

  // The operator asked for an answer, so the page answers out loud as well as on the screen. A refresh the
  // page does by itself says nothing on its own schedule: a sentence every five seconds is a page that never
  // stops talking. Asking is asking to be told again, so asking sets aside the page's own "already said", and
  // a refusal the page falls into on the way is said once whichever way it got here.
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

  // The failure sentence has one owner, and it is said once for every time the page falls into the failure
  // state. A server going away is a change of state the operator would find critical, and a change a screen
  // reader is never told about is a change that happens in silence.
  announceTheFailure() {
    if (this.failureAnnounced) return;

    this.failureAnnounced = true;

    announce('The server did not answer the dashboard read.');
  }

  // A page in a tab behind another one, or on a laptop with the lid shut, is a page costing the server
  // several statements every interval for nobody. The browser says when the page stopped being the page the
  // operator is looking at, and the page stops; coming back starts exactly one chain again.
  followThePage() {
    if (this.closed) return;

    if (this.autoRefresh && document.visibilityState !== 'hidden') this.schedule.start();
    else this.schedule.stop();

    // Coming back is the moment the figures on the page are looked at again, and the page's clock has been
    // moving the whole time it was away. The page re-reads its own claim about them here rather than leaving
    // an operator looking at an hour-old answer under a line that says nothing about it, and rather than
    // waiting for the next poll to notice: the first poll lands an interval after the operator is already
    // reading.
    this.render();
  }

  render() {
    const { status, config, auth } = getAdminContext().store.getState();

    const snapshot = status?.snapshot;

    this.renderReadLine(status);

    if (status?.failed) {
      this.announceTheFailure();

      this.strip.textContent = '';

      // The numbers the page is holding stay in the page's state and are not put on the screen. A figure
      // with no moment attached to it is a figure the operator will believe, and the moment is the one thing
      // a refused read took away.
      this.body.innerHTML = `
        <p class="rdio-dashboard-failure">The server did not answer this read. The dashboard is not showing the numbers it is holding, because they are no longer current.</p>
      `;

      return;
    }

    // A read that came back ends the failure the page has already said out loud, so the next time the page
    // loses its server it says so again.
    this.failureAnnounced = false;

    if (!snapshot) {
      this.strip.textContent = '';

      this.body.innerHTML = `<p class="rdio-dashboard-reading">The dashboard is reading the server.</p>`;

      return;
    }

    const inventory = deriveInventory(config);
    const states = deriveCardStates(snapshot, inventory, { passwordNeedChange: !!auth?.passwordNeedChange });
    const series = buildHourSeries(snapshot);

    this.strip.textContent = stripSentence(snapshot, series);

    this.body.innerHTML = `<div class="rdio-dashboard-cards">${states.map((state) => cardMarkup(state, snapshot, inventory, series)).join('')}</div>`;
  }

  // The line that makes every figure on the page a figure with a moment attached to it.
  renderReadLine(status) {
    const at = status?.fetchedAt ? formatTime(new Date(status.fetchedAt)) : '';

    if (!at) {
      this.readLine.textContent = '';

      return;
    }

    // The page's own clock against the instant the answer was read. Past three intervals the page can no
    // longer assume it is keeping the numbers current, and saying so is the only honest thing left.
    //
    // A read the server refused is that state already: the figures on the page are the ones from the last
    // read that answered, and the read that would have replaced them is the one that did not. The store
    // sees the second refusal as the same failure as the first and tells nobody about it, so a page that
    // only rewrites this line when something tells it to would go on claiming its figures are current.
    // The page knows it has not read the server, and says so from the first refusal onward.
    const stale = status.failed || isStale(status.fetchedAt, new Date().toISOString());

    this.readLine.textContent = stale
      ? `Read at ${at}, and this page is no longer keeping it current.`
      : `Read at ${at}.`;
  }
}

// The strip is the page's one-sentence answer, and it is the same figures the cards carry: a strip that
// disagreed with the card beside it would be a page contradicting itself in one line.
function stripSentence(snapshot, series) {
  const parts = [`up ${formatUptime(snapshot.server?.uptimeSeconds)}`];

  if (snapshot.live) parts.push(`${snapshot.live.listeners} listening`);

  const total = series.reduce((sum, hour) => sum + hour.count, 0);

  if (snapshot.activity) parts.push(`${total} ${total === 1 ? 'call' : 'calls'} in the last 24 hours`);

  return `Running · ${parts.join(' · ')}`;
}

function cardMarkup(state, snapshot, inventory, series) {
  const heading = `rdio-dashboard-${state.key}-heading`;

  const figures = cardFigures(state.key, snapshot, inventory, series);

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

function cardFigures(key, snapshot, inventory, series) {
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
  if (key === 'busiest') return busiestCard(snapshot);

  return logHealthCard(snapshot);
}

function activityCard(snapshot, series) {
  const activity = snapshot.activity ?? {};

  const total = series.reduce((sum, hour) => sum + hour.count, 0);

  const hours = series
    .map((hour) => {
      // The bar's height as a share of the busiest hour. The floor the stylesheet gives a zero bar is what
      // keeps an empty hour visible as an hour instead of as a gap in the chart.
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

function busiestCard(snapshot) {
  const busiest = snapshot.activity?.busiest ?? [];

  if (busiest.length === 0) {
    return `<p class="rdio-dashboard-empty">No calls in the last 24 hours.</p>`;
  }

  const rows = busiest
    .map((row) => `<tr><td>${row.name || `T-${row.talkgroupRef}`}</td><td>${row.systemRef}</td><td>${row.count}</td></tr>`)
    .join('');

  return `
    <table class="rdio-dashboard-busiest">
      <thead><tr><th scope="col">Talkgroup</th><th scope="col">System</th><th scope="col">Calls</th></tr></thead>
      <tbody>${rows}</tbody>
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
