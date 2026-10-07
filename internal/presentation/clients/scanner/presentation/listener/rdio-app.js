// rdio-app: listener shell (legacy rdio-scanner.component) — status bar,
// display, controls, the select/search side panels, fullscreen, the
// beforeunload live-feed guard, and the help/promo identity features.

import { getAppContext } from '../../application/context.js';
import { showHelp } from './rdio-help.js';
import { schedulePromo } from './rdio-promo.js';

class RdioApp extends HTMLElement {
  connectedCallback() {
    const { store } = getAppContext();

    this.innerHTML = `
      <aside class="panel search" hidden>
        <div class="toolbar">
          <div>
            <button class="panel-close" type="button" aria-label="Close search panel">
              <i class="material-icons">arrow_forward</i>
            </button>
          </div>
        </div>
        <div class="scroll"><rdio-search-panel></rdio-search-panel></div>
      </aside>
      <aside class="panel select" hidden>
        <div class="toolbar">
          <div>
            <button class="panel-close" type="button" aria-label="Close select panel">
              <i class="material-icons">arrow_back</i>
            </button>
          </div>
        </div>
        <div class="scroll"><rdio-select-panel></rdio-select-panel></div>
      </aside>
      <section class="content">
        <rdio-status></rdio-status>
        <rdio-display></rdio-display>
        <rdio-controls></rdio-controls>
        <div class="rdio-help" hidden>
          <button type="button" aria-label="Help">
            <i class="material-icons">help_center</i>
          </button>
        </div>
      </section>
    `;

    this.panels = {
      search: this.querySelector('.panel.search'),
      select: this.querySelector('.panel.select'),
    };

    const display = this.querySelector('rdio-display');
    const controls = this.querySelector('rdio-controls');

    controls.addEventListener('open-search', () => this.openPanel('search'));
    controls.addEventListener('open-select', () => this.openPanel('select'));
    controls.addEventListener('auth-focus', () => display.focusAuth());
    controls.addEventListener('dimmer', () => display.refreshDimmer());
    controls.addEventListener('replay', () => display.replayLast());

    for (const panel of Object.values(this.panels)) {
      panel.querySelector('.panel-close').addEventListener('click', () => this.closePanel(panel));
    }

    display.addEventListener('dblclick', () => this.toggleFullscreen());

    this.helpButton = this.querySelector('.rdio-help button');
    this.helpButton.addEventListener('click', () => showHelp(store.getState().config?.email));

    this.beforeUnloadHandler = (event) => {
      if (store.getState().livefeedMode !== 'offline') {
        event.preventDefault();
        event.returnValue = 'Live Feed is ON, do you really want to leave?';
      }
    };

    window.addEventListener('beforeunload', this.beforeUnloadHandler);

    this.unsubscribe = store.subscribe(['config'], () => this.renderHelp());
    this.renderHelp();

    schedulePromo();
  }

  disconnectedCallback() {
    this.unsubscribe?.();
    window.removeEventListener('beforeunload', this.beforeUnloadHandler);
  }

  openPanel(name) {
    const panel = this.panels[name];

    panel.hidden = false;

    // legacy scrollTop(): the panel content is scrolled back to the top
    setTimeout(() => panel.querySelector('.scroll').scrollTo(0, 0));

    if (name === 'search') this.querySelector('rdio-search-panel').open();
  }

  closePanel(panel) {
    panel.hidden = true;
  }

  renderHelp() {
    // legacy main.component.html: the help button only appears when the
    // server config carries a contact email
    this.querySelector('.rdio-help').hidden = !getAppContext().store.getState().config?.email;
  }

  toggleFullscreen() {
    const el = document.documentElement;

    if (document.fullscreenElement) {
      (el.exitFullscreen ?? el.mozCancelFullScreen ?? el.msExitFullscreen ?? el.webkitExitFullscreen)?.call(el);
    } else {
      (el.requestFullscreen ?? el.mozRequestFullScreen ?? el.msRequestFullscreen ?? el.webkitRequestFullscreen)?.call(el);
    }
  }
}

customElements.define('rdio-app', RdioApp);
