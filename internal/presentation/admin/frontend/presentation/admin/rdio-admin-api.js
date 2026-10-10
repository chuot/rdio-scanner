// rdio-admin-api (FEAT-018, CA-005): the API entrypoint submenu. One card lists
// every OpenAPI entry point the server answers, in seven groups, each rendered by
// the shared rdio-admin-openapi-box. Informational: no save bar, no store slice,
// no requests.

import { API_GROUPS } from './openapi-box.js';

class RdioAdminApi extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    const root = document.createElement('div');
    root.className = 'rdio-admin-card api';

    const intro = document.createElement('p');
    intro.className = 'caption';
    intro.textContent = 'Every entry point the server answers, grouped. The admin webapp uses these; the same calls work from any HTTP client. Admin endpoints take the raw JWT in the Authorization header, no Bearer prefix; the capture endpoints authenticate with the key form field.';

    root.append(intro);

    for (const group of Object.keys(API_GROUPS)) {
      const box = document.createElement('rdio-admin-openapi-box');

      box.dataset.group = group;

      root.append(box);
    }

    this.replaceChildren(root);
  }
}

customElements.define('rdio-admin-api', RdioAdminApi);
