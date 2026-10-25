// rdio-app: listener shell with the status bar, display, controls, the select and search side
// panels, fullscreen, the beforeunload live-feed guard, and the help and promo features.

import { getAppContext } from '../../application/context.js';
import { showHelp } from './rdio-help.js';
import { schedulePromo } from './rdio-promo.js';

class RdioApp extends HTMLElement {
  connectedCallback() {
    const { store } = getAppContext();

    // the content the listener works in is a named region, named by the
    // application's own heading: entering the page by landmark says the
    // application name, and the headings name both panels.
    // see the named-working-region maintainer note.
    this.innerHTML = `
      <aside class="panel search" hidden>
        <h2 class="rdio-visually-hidden">Search</h2>
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
        <h2 class="rdio-visually-hidden">Select</h2>
        <div class="toolbar">
          <div>
            <button class="panel-close" type="button" aria-label="Close select panel">
              <i class="material-icons">arrow_back</i>
            </button>
          </div>
        </div>
        <div class="scroll"><rdio-select-panel></rdio-select-panel></div>
      </aside>
      <main id="content" aria-labelledby="rdio-app-heading">
        <h1 id="rdio-app-heading" class="rdio-visually-hidden">Rdio Scanner</h1>
        <rdio-live-region></rdio-live-region>
        <section class="content">
          <rdio-status></rdio-status>
          <rdio-display></rdio-display>
          <rdio-controls></rdio-controls>
          <div class="rdio-help" hidden>
            <button type="button" aria-label="Help">
              <i class="material-icons">help_center</i>
            </button>
          </div>
          <div class="rdio-fullscreen">
            <button type="button" aria-label="Fullscreen" aria-pressed="false">
              <i class="material-icons" aria-hidden="true">fullscreen</i>
            </button>
          </div>
        </section>
      </main>
    `;

    this.panels = {
      search: this.querySelector('.panel.search'),
      select: this.querySelector('.panel.select'),
    };
    this.closings = new Map();
    // The control each panel was opened from: the keyboard goes back there when the panel leaves.
    this.openers = new Map();

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

    // the keyboard's route into the display's double-click fullscreen:
    // the gesture has no keyboard equivalent.
    // see the fullscreen-keyboard-control maintainer note.
    this.fullscreenButton = this.querySelector('.rdio-fullscreen button');
    this.fullscreenButton.addEventListener('click', () => this.toggleFullscreen());

    this.fullscreenChangeHandler = () => this.renderFullscreen();
    document.addEventListener('fullscreenchange', this.fullscreenChangeHandler);
    this.renderFullscreen();

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
    document.removeEventListener('fullscreenchange', this.fullscreenChangeHandler);
  }

  openPanel(name) {
    const panel = this.panels[name];

    // the control the operator pressed to open the panel: the keyboard
    // goes back there when the panel leaves, so the operator is not
    // dropped at the top of the document.
    this.openers.set(panel, document.activeElement);

    this.cancelClosing(panel);

    panel.hidden = false;

    // The closed position has to be laid out before the open one is applied,
    // otherwise there is nothing for the drawer to slide from.
    panel.getBoundingClientRect();
    panel.classList.add('open');

    // legacy scrollTop(): the panel content is scrolled back to the top
    setTimeout(() => panel.querySelector('.scroll').scrollTo(0, 0));

    if (name === 'search') this.querySelector('rdio-search-panel').open();

    // the keyboard travels with the panel it opened: the landing place is
    // a control the panel marks itself, because a search answer repaints
    // a row a landed keyboard would stand on.
    // see the panel-keyboard-landing maintainer note.
    const landing = panel.querySelector('[data-landing]')
      ?? panel.querySelector('.scroll button, .scroll input, .scroll select, .scroll textarea, .scroll a[href], .scroll [tabindex]')
      ?? panel.querySelector('.panel-close');

    landing?.focus();
  }

  closePanel(panel) {
    this.openers.get(panel)?.focus?.();

    panel.classList.remove('open');

    // a drawer leaves the page when it has finished sliding out: hiding
    // it earlier cuts the motion short, and leaving it shown keeps an
    // off-screen panel in the tab order.
    const finished = (event) => {
      if (event.target !== panel || event.propertyName !== 'transform') {
        return;
      }

      this.cancelClosing(panel);
      panel.hidden = true;
    };

    panel.addEventListener('transitionend', finished);
    this.closings.set(panel, finished);
  }

  cancelClosing(panel) {
    const finished = this.closings.get(panel);

    if (!finished) {
      return;
    }

    panel.removeEventListener('transitionend', finished);
    this.closings.delete(panel);
  }

  renderHelp() {
    // the help button only appears when the server config carries a contact email.
    this.querySelector('.rdio-help').hidden = !getAppContext().store.getState().config?.email;
  }

  toggleFullscreen() {
    const el = document.documentElement;

    if (document.fullscreenElement) {
      // the way out of fullscreen is the document's own: Chrome and
      // Safari carry it there, and only Firefox puts it on the element.
      // see the fullscreen-exit-api maintainer note.
      (document.exitFullscreen ?? document.webkitExitFullscreen ?? document.mozCancelFullScreen ?? document.msExitFullscreen)?.call(document);
    } else {
      (el.requestFullscreen ?? el.mozRequestFullScreen ?? el.msRequestFullscreen ?? el.webkitRequestFullscreen)?.call(el);
    }
  }

  // the control names the move it will make, not the state it sits in:
  // pressed says where the page is.
  // see the fullscreen-keyboard-control maintainer note.
  renderFullscreen() {
    const button = this.fullscreenButton;

    if (!button) return;

    const active = !!document.fullscreenElement;

    button.setAttribute('aria-pressed', String(active));
    button.setAttribute('aria-label', active ? 'Exit fullscreen' : 'Fullscreen');

    const glyph = button.querySelector('i');

    if (glyph) glyph.textContent = active ? 'fullscreen_exit' : 'fullscreen';
  }
}

customElements.define('rdio-app', RdioApp);
