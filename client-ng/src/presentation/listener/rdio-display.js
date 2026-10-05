// rdio-display: the LCD display rows, history table, unlock overlay, dimmer,
// clock and progress tick (legacy main.component.ts + main.component.html).

import { getAppContext } from '../../application/context.js';
import { version } from '../../infrastructure/version.js';
import { readPin, savePin } from '../../domain/pin.js';
import { recordHistory, advanceReplayOffset, HISTORY_SIZE } from '../../domain/replay-history.js';
import {
  activeFrequency,
  formatAfs,
  formatFrequency,
  formatMonthDay,
  formatTime,
  isAfsSystem,
  progressDate,
  resolveUnit,
  shouldShowDate,
} from './display-format.js';

class RdioDisplay extends HTMLElement {
  connectedCallback() {
    const { store, useCases } = getAppContext();

    this.innerHTML = `
      <div class="row three">
        <div><span class="clock"></span></div>
        <div><span class="link"></span></div>
        <div><span class="queue"></span></div>
      </div>
      <div class="row small"></div>
      <div class="row">
        <div><span class="call-system"></span></div>
        <div><span class="call-type"></span></div>
        <div><span class="call-tag"></span></div>
      </div>
      <div class="row">
        <div><span class="call-talkgroup"></span></div>
        <div></div>
        <div><span class="call-date"></span> <span class="call-progress"></span></div>
      </div>
      <div class="row big full"><span class="call-name"></span></div>
      <div class="row">
        <div><span class="call-frequency"></span></div>
        <div></div>
        <div><span class="call-tgid"></span></div>
      </div>
      <div class="row">
        <div><span class="call-stats"></span></div>
        <div></div>
        <div><span class="call-unit"></span></div>
      </div>
      <div class="row right small">
        <div></div>
        <div></div>
        <div>
          <div><span class="flag temp-avoid"></span></div>
          <div><span class="flag flag-avoid">AVOID</span></div>
          <div><span class="flag flag-patch">PATCH</span></div>
          <div><span class="flag flag-delayed">DELAYED</span></div>
        </div>
      </div>
      <div class="wrapper">
        <div class="auth">
          <form autocomplete="off">
            <rdio-form-field>
              <label for="unlock-code">Unlock code</label>
              <input id="unlock-code" name="password" type="password" autocomplete="off" />
            </rdio-form-field>
          </form>
        </div>
        <table class="history">
          <thead>
            <tr><th><span>Time</span></th><th><span>System</span></th><th><span>Talkgroup</span></th><th><span>Name</span></th></tr>
          </thead>
          <tbody></tbody>
        </table>
      </div>
    `;

    const q = (sel) => this.querySelector(sel);

    this.el = {
      clock: q('.clock'), link: q('.link'), queue: q('.queue'),
      system: q('.call-system'), type: q('.call-type'), tag: q('.call-tag'),
      talkgroup: q('.call-talkgroup'), date: q('.call-date'), progress: q('.call-progress'),
      name: q('.call-name'), frequency: q('.call-frequency'), tgid: q('.call-tgid'),
      stats: q('.call-stats'), unit: q('.call-unit'),
      tempAvoid: q('.temp-avoid'), avoid: q('.flag-avoid'), patch: q('.flag-patch'), delayed: q('.flag-delayed'),
      auth: q('.auth'), authForm: q('.auth form'), authInput: q('#unlock-code'),
      historyBody: q('.history tbody'),
    };

    this.callHistory = new Array(HISTORY_SIZE).fill(null);
    this.callTime = 0;
    this.replayOffset = 0;
    this.dimmer = false;
    this.clock = new Date();
    this.lastSubmittedPin = null;

    // Legacy idle display defaults (main.component.ts field initializers):
    // the pristine display shows placeholder rows until the first call.
    this.el.system.textContent = 'System';
    this.el.tag.textContent = 'Tag';
    this.el.talkgroup.textContent = 'Talkgroup';
    this.el.frequency.textContent = `F: ${formatFrequency(0)}`;
    this.el.tgid.textContent = 'TGID: 0';
    this.el.stats.textContent = 'E: 0 S: 0 ';
    this.el.unit.textContent = 'UID: 0';
    this.el.progress.textContent = '00:00';
    this.el.name.textContent = `Rdio Scanner v${version}`;

    this.el.authForm.addEventListener('submit', (ev) => {
      ev.preventDefault();
      this.authenticate();
    });

    this.unsubscribe = store.subscribe(
      ['auth', 'call', 'config', 'linked', 'listeners', 'livefeedMode', 'map', 'pause', 'queue', 'showListenersCount'],
      () => this.render(),
    );

    this.syncClock();
    this.startProgressTick();
    this.updateDimmer();
    this.render();
  }

  disconnectedCallback() {
    this.unsubscribe?.();
    clearInterval(this.clockTimer);
    clearInterval(this.progressTimer);
    clearTimeout(this.dimmerTimer);
    clearTimeout(this.replayTimer);
  }

  authenticate() {
    const { useCases } = getAppContext();

    const password = this.el.authInput.value;

    if (password) {
      this.lastSubmittedPin = password;
      useCases.authenticate(password);
      this.el.authInput.disabled = true;
    }
  }

  syncClock() {
    clearInterval(this.clockTimer);

    this.clock = new Date();

    this.clockTimer = setInterval(() => {
      this.clock = new Date();
      this.render();
    }, 1000 * (60 - this.clock.getSeconds()));
  }

  startProgressTick() {
    clearInterval(this.progressTimer);

    this.progressTimer = setInterval(() => {
      const { call } = getAppContext().store.getState();

      if (!call) {
        this.callTime = 0;
        return;
      }

      if (this.audioStart === undefined || this.audioStart !== call.id) {
        this.audioStart = call.id;
        this.callStart = Date.now();
      }

      const { pause } = getAppContext().store.getState();

      if (!pause) this.callTime = Math.floor((Date.now() - this.callStart) / 1000);

      this.render();
    }, 500);
  }

  updateDimmer() {
    const { config } = getAppContext().store.getState();

    if (typeof config?.dimmerDelay === 'number') {
      this.dimmer = true;
      this.classList.remove('idle');

      clearTimeout(this.dimmerTimer);
      this.dimmerTimer = setTimeout(() => {
        this.dimmer = false;
        this.classList.add('idle');
      }, config.dimmerDelay);
    }
  }

  refreshDimmer() {
    this.updateDimmer();
  }

  focusAuth() {
    this.el.authInput.focus();
  }

  // legacy main.component.ts replay(): successive presses walk the history
  // within a 1 s window; the offset resets 1 s after the last press.
  replayLast() {
    const { store, useCases } = getAppContext();
    const { call, pause } = store.getState();
    const callPrevious = useCases.callPrevious;

    if (pause || (!call && !callPrevious)) {
      useCases.beep('denied');
      this.updateDimmer();
      return;
    }

    useCases.beep('activate');

    if (this.replayTimer) {
      clearTimeout(this.replayTimer);
      this.replayTimer = null;
      this.replayOffset = advanceReplayOffset(this.replayOffset, this.callHistory.filter(Boolean).length);
    }

    this.replayTimer = setTimeout(() => {
      this.replayTimer = null;
      this.replayOffset = 0;
    }, 1000);

    if (call && !this.replayOffset) {
      useCases.replay();
    } else if (callPrevious !== this.callHistory[0]) {
      if (this.replayOffset) useCases.play(this.callHistory[this.replayOffset - 1]);
      else useCases.replay();
    } else if (this.replayOffset < this.callHistory.length) {
      useCases.play(this.callHistory[this.replayOffset]);
    }

    this.updateDimmer();
  }

  render() {
    const { store } = getAppContext();
    const state = store.getState();
    const { call, config, linked, listeners, pause, queue, auth } = state;

    const time12h = config?.time12hFormat ?? false;

    this.el.clock.textContent = formatTime(this.clock, time12h);
    this.el.link.textContent = !linked ? 'NO LINK' : config?.showListenersCount ? `L: ${listeners}` : '';
    this.el.queue.textContent = `Q: ${queue}`;

    if (call) {
      const isAfs = isAfsSystem(call);
      const progress = progressDate(call.dateTime, this.callTime);

      this.el.date.textContent = shouldShowDate(call.dateTime, progress) ? formatMonthDay(call.dateTime) : '';
      this.el.progress.textContent = formatTime(progress, time12h);

      this.el.system.textContent = call.systemData?.label ?? `${call.system}`;
      this.el.type.textContent = (call.talkgroupData?.type ?? call.systemData?.type ?? '').toUpperCase();
      this.el.tag.textContent = call.talkgroupData?.tag ?? '';

      this.el.talkgroup.textContent = call.talkgroupData?.label ?? `${isAfs ? formatAfs(call.talkgroup) : call.talkgroup}`;
      this.el.name.textContent = call.talkgroupData?.name ?? formatFrequency(call.frequency);

      const freq = activeFrequency(call, this.callTime);
      this.el.frequency.textContent = `F: ${freq.frequency || 0}`;
      this.el.stats.textContent = `E: ${freq.error || 0} S: ${freq.spike || 0} `;
      this.el.tgid.textContent = `TGID: ${isAfs ? formatAfs(call.talkgroup) : call.talkgroup}`;

      const unit = resolveUnit(call, this.callTime);
      this.el.unit.textContent = unit ? `UID: ${unit}` : '';

      this.lastCall = call;

      if (call.audio) this.recordPlayed(call);
    } else if (!this.lastCall) {
      // legacy keeps the last call's rows on screen after it ends; only the
      // pristine idle state shows the version line
      this.el.name.textContent = `Rdio Scanner v${version}`;
    }

    // Legacy evaluates the flags from `call || callPrevious` on every event
    // (main.component.ts), so AVOID/PATCH/temp-avoid keep refreshing while
    // the display idles on the last call's rows.
    const reference = call ?? this.lastCall;
    if (reference) {
      // Exact legacy isAvoided/isAvoidedTimer/isPatched semantics
      // (rdio-scanner.service.ts): a system missing from the map is never
      // avoided, and PATCH only shows for an avoided call.
      const sysMap = state.map?.[reference.system];
      const avoided = !!sysMap && sysMap[reference.talkgroup]?.active !== true;
      const minutes = sysMap?.[reference.talkgroup]?.minutes ?? 0;
      const patched = avoided && (reference.patches ?? []).some((tg) => sysMap[tg]?.active || false);

      this.el.tempAvoid.textContent = minutes ? `⌛️ ${minutes}M` : '';
      this.el.tempAvoid.classList.toggle('flaged', avoided || patched);
      this.el.avoid.classList.toggle('flaged', avoided);
      this.el.patch.classList.toggle('flaged', patched);
      this.el.delayed.classList.toggle('flaged', !!reference.delayed);
    }

    this.renderHistory(call);
    this.renderAuth(auth);
  }

  recordPlayed(call) {
    const previous = getAppContext().useCases.callPrevious;

    if (previous && previous.id !== call.id && !this.callHistory.some((c) => c?.id === previous.id)) {
      this.callHistory = recordHistory(this.callHistory, previous);
    }
  }

  renderHistory(currentCall) {
    // legacy renders a fixed 5-row table; missing entries are blank rows
    const rows = [];

    for (let i = 0; i < HISTORY_SIZE; i++) {
      const previousCall = this.callHistory[i];

      if (!previousCall) {
        rows.push('<tr><td></td><td></td><td></td><td></td></tr>');
        continue;
      }

      const time = formatTime(new Date(previousCall.dateTime), getAppContext().store.getState().config?.time12hFormat);
      const system = previousCall.systemData?.label ?? previousCall.system;
      const talkgroup = previousCall.talkgroupData?.label ?? previousCall.talkgroup;
      const name = previousCall.talkgroupData?.name ?? previousCall.frequency;
      const replaying = currentCall && currentCall.id === previousCall.id ? ' class="replaying"' : '';

      rows.push(`<tr${replaying}><td><span>${time}</span></td><td><span>${system}</span></td><td><span>${talkgroup}</span></td><td><span>${name}</span></td></tr>`);
    }

    this.el.historyBody.innerHTML = rows.join('');
  }

  renderAuth(auth) {
    const visible = auth.status === 'pin-required' || auth.status === 'expired' || auth.status === 'too-many';

    this.el.auth.classList.toggle('visible', visible);
    this.el.auth.hidden = !visible;

    if (visible) {
      this.el.authInput.disabled = false;

      const field = this.el.auth.querySelector('rdio-form-field');

      if (auth.status === 'expired') field.setError('This unlock code has expired');
      else if (auth.status === 'too-many') field.setError('Too many connections');
      else if (auth.denied) field.setError('Invalid unlock code');
      else field.setError('');

      this.el.authInput.focus();
    } else if (auth.status === 'authenticated') {
      this.el.authInput.disabled = false;

      const pin = this.lastSubmittedPin ?? readPin(getAppContext().storage.local);
      if (pin) savePin(getAppContext().storage.local, pin);

      this.lastSubmittedPin = null;
    }
  }
}

customElements.define('rdio-display', RdioDisplay);
