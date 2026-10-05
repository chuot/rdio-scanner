// rdio-status: branding line + server LED (legacy .rdio-status block).

import { getAppContext } from '../../application/context.js';
import { LED_COLORS, resolveLedColor } from '../../domain/alert-priority.js';

class RdioStatus extends HTMLElement {
  connectedCallback() {
    const { store } = getAppContext();

    this.innerHTML = `
      <div class="branding"></div>
      <div class="led off" role="img" aria-label="Server status"></div>
    `;

    this.branding = this.querySelector('.branding');
    this.led = this.querySelector('.led');

    this.unsubscribe = store.subscribe(['config', 'call', 'pause', 'listeners'], () => this.render());
    this.render();
  }

  disconnectedCallback() {
    this.unsubscribe?.();
  }

  render() {
    const { config, call, pause } = getAppContext().store.getState();

    this.branding.textContent = config?.branding ?? '';

    const style = call && pause ? 'on paused' : call ? 'on' : 'off';

    const color = call ? resolveLedColor(call, LED_COLORS) : 'green';

    this.led.className = `led ${style} ${color}`;
    this.led.setAttribute('aria-label', `Server status: ${style}`);
  }
}

customElements.define('rdio-status', RdioStatus);
