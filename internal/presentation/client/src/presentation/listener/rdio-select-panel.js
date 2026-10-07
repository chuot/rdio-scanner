// rdio-select-panel: category buttons (shown when ≥ 2 categories), per-system
// talkgroup fieldsets with timed-avoid blink, ALL ON/OFF (legacy select.component).

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
        html += `<button type="button" class="rdio-button ${category.status}" data-category="${encodeURIComponent(JSON.stringify(category))}">${category.label}</button>`;
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

        html += `<button type="button" class="rdio-button ${cls}" data-system="${system.id}" data-talkgroup="${talkgroup.id}">${talkgroup.label}</button>`;
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
    const { useCases } = getAppContext();

    const button = ev.target.closest('button');

    if (!button) return;

    if (button.dataset.category) {
      useCases.toggleCategory(JSON.parse(decodeURIComponent(button.dataset.category)));
    } else if (button.dataset.all === 'on') {
      useCases.toggleAll(true);
    } else if (button.dataset.all === 'off') {
      useCases.toggleAll(false);
    } else if (button.dataset.systemOff) {
      useCases.setSystemStatus(Number(button.dataset.systemOff), false);
    } else if (button.dataset.systemOn) {
      useCases.setSystemStatus(Number(button.dataset.systemOn), true);
    } else if (button.dataset.system) {
      useCases.beep('activate');
      useCases.avoid({ system: Number(button.dataset.system), talkgroup: Number(button.dataset.talkgroup) });
    }
  }
}

customElements.define('rdio-select-panel', RdioSelectPanel);
