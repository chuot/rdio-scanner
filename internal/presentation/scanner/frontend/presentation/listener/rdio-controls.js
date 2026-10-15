// rdio-controls: the 9 control buttons (legacy main.component.ts control row),
// each guarded by the unlock overlay, with keypad beeps and dimmer refresh.

import { getAppContext } from '../../application/context.js';

class RdioControls extends HTMLElement {
  connectedCallback() {
    const { store, useCases } = getAppContext();

    this.innerHTML = `
      <div class="row">
        <button class="rdio-button" data-action="livefeed">LIVE<br>FEED</button>
        <div class="spacer"></div>
        <button class="rdio-button" data-action="holdSystem">HOLD<br>SYS</button>
        <div class="spacer"></div>
        <button class="rdio-button" data-action="holdTalkgroup">HOLD<br>TG</button>
      </div>
      <div class="row">
        <button class="rdio-button" data-action="replay">REPLAY<br>LAST</button>
        <div class="spacer"></div>
        <button class="rdio-button" data-action="skip">SKIP<br>NEXT</button>
        <div class="spacer"></div>
        <button class="rdio-button" data-action="avoid">AVOID</button>
      </div>
      <div class="row">
        <button class="rdio-button" data-action="search">SEARCH<br>CALL</button>
        <div class="spacer"></div>
        <button class="rdio-button" data-action="pause">PAUSE</button>
        <div class="spacer"></div>
        <button class="rdio-button" data-action="select">SELECT<br>TG</button>
      </div>
    `;

    this.addEventListener('click', (ev) => {
      const button = ev.target.closest('button[data-action]');

      if (button) this.handle(button.dataset.action, button);
    });

    this.unsubscribe = store.subscribe(['auth', 'holdSys', 'holdTg', 'livefeedMode', 'pause'], () => this.render());
    this.render();
  }

  disconnectedCallback() {
    this.unsubscribe?.();
  }

  handle(action, button) {
    const { store, useCases } = getAppContext();
    const state = store.getState();

    if (state.auth.status === 'pin-required' || state.auth.status === 'expired' || state.auth.status === 'too-many') {
      // legacy main.component.ts: locked controls only focus the unlock field, no beep
      this.dispatchEvent(new CustomEvent('auth-focus'));
      return;
    }

    // The v6.6.3 control row arms the dimmer on the controls that change what the display is
    // showing — livefeed, the two holds, replay, skip, pause, and avoid when there is a call to
    // avoid. The two panel openers leave the dimmer where it is, and so does avoid denied.
    let armed = true;

    switch (action) {
      case 'livefeed':
        useCases.beep(state.livefeedMode === 'offline' ? 'activate' : 'deactivate');
        useCases.livefeed();
        break;
      case 'holdSystem':
        if (state.call || useCases.callPrevious) {
          useCases.beep(state.holdSys ? 'deactivate' : 'activate');
          useCases.holdSystem();
        } else {
          useCases.beep('denied');
        }
        break;
      case 'holdTalkgroup':
        if (state.call || useCases.callPrevious) {
          useCases.beep(state.holdTg ? 'deactivate' : 'activate');
          useCases.holdTalkgroup();
        } else {
          useCases.beep('denied');
        }
        break;
      case 'replay':
        this.dispatchEvent(new CustomEvent('replay'));
        break;
      case 'skip':
        useCases.beep('activate');
        useCases.skip();
        break;
      case 'avoid': {
        const call = state.call || useCases.callPrevious;

        if (!call) {
          useCases.beep('denied');
          armed = false;
          break;
        }

        const avoided = useCases.avoid();

        useCases.beep(avoided ? 'activate' : 'deactivate');
        break;
      }
      case 'search':
        armed = false;

        if (!state.config) break;

        useCases.beep('activate');
        this.dispatchEvent(new CustomEvent('open-search'));
        break;
      case 'pause':
        useCases.beep(state.pause ? 'deactivate' : 'activate');
        useCases.pause();
        break;
      case 'select':
        armed = false;

        if (!state.config) break;

        useCases.beep('activate');
        this.dispatchEvent(new CustomEvent('open-select'));
        break;
    }

    if (armed) this.dispatchEvent(new CustomEvent('dimmer'));
  }

  render() {
    const { auth, holdSys, holdTg, livefeedMode, pause } = getAppContext().store.getState();

    const locked = auth.status === 'pin-required' || auth.status === 'expired' || auth.status === 'too-many';

    for (const button of this.querySelectorAll('button[data-action]')) {
      switch (button.dataset.action) {
        case 'livefeed':
          button.classList.toggle('off', livefeedMode === 'offline');
          button.classList.toggle('on', livefeedMode === 'online');
          button.classList.toggle('partial', livefeedMode === 'playback');
          break;
        case 'holdSystem':
          button.classList.toggle('off', !holdSys);
          button.classList.toggle('on', holdSys);
          break;
        case 'holdTalkgroup':
          button.classList.toggle('off', !holdTg);
          button.classList.toggle('on', holdTg);
          break;
        case 'pause':
          button.classList.toggle('off', !pause);
          button.classList.toggle('on', pause);
          break;
      }

      button.setAttribute('aria-disabled', String(locked));
    }
  }
}

customElements.define('rdio-controls', RdioControls);
