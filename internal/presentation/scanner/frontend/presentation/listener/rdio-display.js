// rdio-display: the LCD display rows, history table, unlock overlay,
// dimmer, clock and progress tick.

import { getAppContext } from '../../application/context.js';
import { version } from '../../../../common/frontend/infrastructure/version.js';
import { readPin, savePin } from '../../domain/pin.js';
import { createDimmer } from './dimmer.js';
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
} from '../../../../common/frontend/presentation/display-format.js';
import { announce, escapeHtml } from '../../../../common/frontend/presentation/ui/primitives.js';

class RdioDisplay extends HTMLElement {
  connectedCallback() {
    const { store, useCases } = getAppContext();

    this.innerHTML = `
      <div class="row">
        <div><span class="clock"></span></div>
        <div><span class="link"></span></div>
        <div><span class="queue"></span></div>
      </div>
      <div class="row small"><span class="call-system-type"></span></div>
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
      <div class="row big"><span class="call-name"></span></div>
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
        <div><span class="flag temp-avoid"></span></div>
        <div><span class="flag flag-avoid">AVOID</span></div>
        <div><span class="flag flag-patch">PATCH</span></div>
        <div><span class="flag flag-delayed">DELAYED</span></div>
      </div>
      <div class="wrapper">
        <div class="auth">
          <form autocomplete="off">
            <rdio-form-field>
              <div class="rdio-field-box"><div class="rdio-field-flex"><input id="unlock-code" name="password" type="password" autocomplete="off" placeholder="Unlock code" aria-label="Unlock code" /></div></div>
            </rdio-form-field>
          </form>
        </div>
        <table class="history">
          <thead>
            <tr><th title="Time"><span>Time</span></th><th title="System"><span>System</span></th><th title="Talkgroup"><span>Talkgroup</span></th><th title="Name"><span>Name</span></th></tr>
          </thead>
          <tbody></tbody>
        </table>
      </div>
    `;

    const q = (sel) => this.querySelector(sel);

    this.el = {
      clock: q('.clock'), link: q('.link'), queue: q('.queue'),
      systemType: q('.call-system-type'),
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
    // the display starts dimmed: only a call arriving or a control pressed lights it; the
    // configuration arriving is not one of those occasions.
    this.dimmer = createDimmer({
      setDimmed: (dimmed) => this.classList.toggle('idle', dimmed),
    });

    this.classList.add('idle');
    this.lastCall = null;
    this.clock = new Date();
    this.lastSubmittedPin = null;

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

      // a submit reaches the field it submits: the code may have arrived
      // without the listener leaving the field, and the server's answer
      // to it is written on that field.
      this.el.auth.querySelector('rdio-form-field')?.markReached();

      this.authenticate();
    });

    this.unsubscribe = store.subscribe(
      ['auth', 'call', 'config', 'linked', 'listeners', 'livefeedMode', 'map', 'pause', 'queue', 'showListenersCount'],
      () => this.render(),
    );

    this.syncClock();
    this.startProgressTick();
    this.render();
  }

  disconnectedCallback() {
    this.unsubscribe?.();
    clearInterval(this.clockTimer);
    clearInterval(this.progressTimer);
    this.dimmer.stop();
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

      // the elapsed reading rides the audio clock: it restarts on every
      // new recording, freezes with the audio while playback is
      // suspended, and answers null when nothing plays.
      this.callTime = Math.floor(getAppContext().audio.playbackElapsed() ?? 0);

      // the dimmer re-arms on every progress update, so the display stays lit
      // while a call is playing.
      this.updateDimmer();
      this.render();
    }, 500);
  }

  updateDimmer() {
    this.dimmer.arm();
  }

  refreshDimmer() {
    this.updateDimmer();
  }

  focusAuth() {
    this.el.authInput.focus();
  }

  // successive replay presses walk the history within a 1 s window; the
  // offset resets 1 s after the last press.
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

  // the display writes the listener's state it shows into the shell's
  // polite region, in the page's own words; the first render stays silent.
  // see the display-state-announcements maintainer note.
  announceState(call, pause, queue) {
    const playing = call ? (call.talkgroupData?.label ?? `${call.talkgroup}`) : null;

    if (this.lastAnnounced === undefined) {
      this.lastAnnounced = { id: call?.id ?? null, pause: !!pause, queue, name: playing };

      return;
    }

    const last = this.lastAnnounced;

    // the queue sentence goes first: on a call start the queue
    // empties in one paint, and the played talkgroup comes last.
    if (queue !== last.queue) announce(`Queue: ${queue}`);

    if (playing && (call.id !== last.id || !!pause !== last.pause)) {
      announce(`${pause ? 'Paused' : 'Playing'} ${playing}`);
    } else if (!call && last.id !== null) {
      announce(`Stopped ${last.name}`);
    }

    this.lastAnnounced = { id: call?.id ?? null, pause: !!pause, queue, name: playing ?? last.name };
  }

  // the display is a fixed grid and a long label is truncated by
  // the cell it will not fit; the full text is the row's accessible
  // name, so the whole of it reaches a screen reader and 400% zoom.
  write(el, text) {
    el.textContent = text;

    if (text) el.setAttribute('aria-label', text);
    else el.removeAttribute('aria-label');
  }

  render() {
    const { store } = getAppContext();
    const state = store.getState();
    const { call, config, linked, listeners, pause, queue, auth } = state;

    // a call arriving is what lights the display: neither the
    // configuration arriving nor the clock ticking arms the dimmer,
    // so the display sits dimmed from the moment the page loads.
    if (call && call !== this.lastCall) this.updateDimmer();

    this.announceState(call, pause, queue);

    this.write(this.el.clock, formatTime(this.clock));
    this.write(this.el.link, !linked ? 'NO LINK' : config?.showListenersCount ? `L: ${listeners}` : '');
    this.write(this.el.queue, `Q: ${queue}`);

    if (call) {
      const isAfs = isAfsSystem(call);
      const progress = progressDate(call.dateTime, this.callTime);

      this.write(this.el.date, shouldShowDate(call.dateTime, progress) ? formatMonthDay(call.dateTime) : '');
      this.write(this.el.progress, formatTime(progress));

      this.write(this.el.system, call.systemData?.label ?? `${call.system}`);
      this.write(this.el.type, (call.talkgroupData?.type ?? call.systemData?.type ?? '').toUpperCase());
      this.write(this.el.tag, call.talkgroupData?.tag ?? '');

      // the type is said once: the small line above the system row carries
      // the system's type only when the system row's own type cell hides
      // it. The wire always names a talkgroup type, so an untyped
      // talkgroup arrives as the empty string and hides the system type
      // with nothing: that hides it just as surely as a different type
      // does, while a talkgroup entry that omits the type entirely lets
      // the cell's own fallback show it.
      const systemType = call.systemData?.type ?? '';
      const cellType = call.talkgroupData?.type ?? systemType;
      const typeOverride = systemType
        && cellType.toUpperCase() !== systemType.toUpperCase();

      this.write(this.el.systemType, typeOverride ? systemType.toUpperCase() : '');

      this.write(this.el.talkgroup, call.talkgroupData?.label ?? `${isAfs ? formatAfs(call.talkgroup) : call.talkgroup}`);
      this.write(this.el.name, call.talkgroupData?.name ?? formatFrequency(call.frequency));

      const freq = activeFrequency(call, this.callTime);
      this.write(this.el.frequency, `F: ${freq.frequency || 0}`);
      this.write(this.el.stats, `E: ${freq.error || 0} S: ${freq.spike || 0} `);
      this.write(this.el.tgid, `TGID: ${isAfs ? formatAfs(call.talkgroup) : call.talkgroup}`);

      const unit = resolveUnit(call, this.callTime);
      this.write(this.el.unit, unit ? `UID: ${unit}` : '');

      this.lastCall = call;

      if (call.audio) this.recordPlayed(call);
    } else if (!this.lastCall) {
      // the last call's rows stay on screen after it ends; only the
      // pristine idle state shows the version line.
      this.write(this.el.name, `Rdio Scanner v${version}`);
    }

    // the flags evaluate from the current call or the last call on
    // every event, so AVOID, PATCH, and timed-avoid keep refreshing
    // while the display idles on the last call's rows.
    const reference = call ?? this.lastCall;
    if (reference) {
      // a system missing from the map is never avoided, and PATCH only
      // shows for an avoided call.
      const sysMap = state.map?.[reference.system];
      const avoided = !!sysMap && sysMap[reference.talkgroup]?.active !== true;
      const minutes = sysMap?.[reference.talkgroup]?.minutes ?? 0;
      const patched = avoided && (reference.patches ?? []).some((tg) => sysMap[tg]?.active || false);

      this.el.tempAvoid.textContent = minutes ? `⧖ ${minutes}M` : '';
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
    // the history is a fixed 5-row table; missing entries are blank rows.
    const rows = [];

    for (let i = 0; i < HISTORY_SIZE; i++) {
      const previousCall = this.callHistory[i];

      if (!previousCall) {
        rows.push('<tr><td></td><td></td><td></td><td></td></tr>');
        continue;
      }

      const time = formatTime(new Date(previousCall.dateTime));
      const system = previousCall.systemData?.label ?? previousCall.system;
      const talkgroup = previousCall.talkgroupData?.label ?? previousCall.talkgroup;
      const name = previousCall.talkgroupData?.name ?? previousCall.frequency;
      const replaying = currentCall && currentCall.id === previousCall.id ? ' class="replaying"' : '';

      // the history rows truncate like the display rows; the full text rides
      // on the cell that clips it. the labels are free text the server
      // carries, so it rides escaped and shows as the text it is.
      const escSystem = escapeHtml(system);
      const escTalkgroup = escapeHtml(talkgroup);
      const escName = escapeHtml(name);

      rows.push(`<tr${replaying}><td title="${time}"><span>${time}</span></td><td title="${escSystem}"><span>${escSystem}</span></td><td title="${escTalkgroup}"><span>${escTalkgroup}</span></td><td title="${escName}"><span>${escName}</span></td></tr>`);
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
