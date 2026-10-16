// rdio-select-panel: category buttons (shown when ≥ 2 categories), per-system
// talkgroup fieldsets with timed-avoid blink, ALL ON/OFF (legacy select.component).
// A talkgroup press is a plain flip on/off that clears any timed window on it —
// the timed avoid cycle belongs to the control bar's AVOID button alone.

import { getAppContext } from '../../application/context.js';

class RdioSelectPanel extends HTMLElement {
  connectedCallback() {
    const { store } = getAppContext();

    this.innerHTML = '<div class="select-body"></div>';
    this.body = this.querySelector('.select-body');

    this.addEventListener('click', (ev) => this.clickHandler(ev));

    this.unsubscribe = store.subscribe(['categories', 'config', 'map'], () => this.render());
    this.render();
  }

  disconnectedCallback() {
    this.unsubscribe?.();
  }

  render() {
    const { categories, config, map } = getAppContext().store.getState();

    let html = '';

    if (categories.length >= 2) {
      html += '<fieldset class="fieldset"><div>';

      for (const category of categories) {
        html += `<button type="button" class="rdio-button ${category.status}" data-category="${encodeURIComponent(JSON.stringify(category))}">${category.label}<span class="rdio-visually-hidden">${category.status}</span></button>`;
      }

      html += '<button type="button" class="rdio-button-mini all-off" data-all="off">OFF</button>';
      html += '<button type="button" class="rdio-button-mini all-on" data-all="on">ON</button>';
      html += '</div></fieldset>';
    }

    for (const system of config?.systems ?? []) {
      if (!system.talkgroups?.length) continue;

      html += `<fieldset class="fieldset"><legend>${system.label}</legend><div>`;

      for (const talkgroup of system.talkgroups) {
        const lfm = map?.[system.id]?.[talkgroup.id];
        const cls = lfm?.minutes ? 'blink' : lfm?.active ? 'on' : 'off';
        const timed = lfm?.minutes ? `<span class="rdio-visually-hidden">avoided for ${lfm.minutes} minutes</span>` : '';

        html += `<button type="button" class="rdio-button ${cls}" aria-pressed="${lfm?.active ? 'true' : 'false'}" data-system="${system.id}" data-talkgroup="${talkgroup.id}">${talkgroup.label}${timed}</button>`;
      }

      if (system.talkgroups.length > 1) {
        html += `<button type="button" class="rdio-button-mini all-off" data-system-off="${system.id}">OFF</button>`;
        html += `<button type="button" class="rdio-button-mini all-on" data-system-on="${system.id}">ON</button>`;
      }

      html += '</div></fieldset>';
    }

    if (!(config?.systems ?? []).some((s) => s.talkgroups?.length)) {
      html += '<div class="empty">No talkgroup configured!</div>';
    }

    this.body.innerHTML = html;
  }

  async clickHandler(ev) {
    const { useCases, store } = getAppContext();

    const button = ev.target.closest('button');

    if (!button) return;

    if (button.dataset.category) {
      const category = JSON.parse(decodeURIComponent(button.dataset.category));

      useCases.beep(category.status === 'on' ? 'deactivate' : 'activate');
      useCases.toggleCategory(category);
    } else if (button.dataset.all === 'on') {
      useCases.beep('activate');
      useCases.toggleAll(true);
    } else if (button.dataset.all === 'off') {
      useCases.beep('deactivate');
      useCases.toggleAll(false);
    } else if (button.dataset.systemOff) {
      useCases.beep('deactivate');
      useCases.setSystemStatus(Number(button.dataset.systemOff), false);
    } else if (button.dataset.systemOn) {
      useCases.beep('activate');
      useCases.setSystemStatus(Number(button.dataset.systemOn), true);
    } else if (button.dataset.system) {
      const system = Number(button.dataset.system);
      const talkgroup = Number(button.dataset.talkgroup);

      // The tone answers the state the press leaves, the way the legacy panel read
      // the map before handing the flip over.
      useCases.beep(store.getState().map?.[system]?.[talkgroup]?.active ? 'deactivate' : 'activate');
      useCases.toggleTalkgroup(system, talkgroup);
    }
  }
}

customElements.define('rdio-select-panel', RdioSelectPanel);
