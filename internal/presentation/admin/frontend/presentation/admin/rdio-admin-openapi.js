// rdio-admin-openapi: endpoint groups derived from the docs/openapi.json contract, shown by
// rdio-admin-openapi-box; a fetch failure renders one honest caption. Informational only:
// no save bar, no store slice, no requests beyond the fetch
// see the openapi-contract-card maintainer note.

import './openapi-box.js';

import { buildOpenApiGroups } from '../../domain/openapi-view.js';

class RdioAdminOpenApi extends HTMLElement {
  async connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    const root = document.createElement('div');
    root.className = 'rdio-admin-card openapi';

    const intro = document.createElement('p');
    intro.className = 'caption';
    intro.textContent = 'Every entry point the server answers, read from the OpenAPI contract. The administration interface uses these; the same calls work from any HTTP client. Admin endpoints take the raw JWT in the Authorization header, no Bearer prefix; the capture endpoints authenticate with the key form field.';

    root.append(intro);

    this.replaceChildren(root);

    let contract;

    try {
      const res = await fetch('openapi.json');

      if (!res.ok) throw new Error(`openapi.json answered ${res.status}`);

      contract = await res.json();
    } catch {
      const note = document.createElement('p');
      note.className = 'caption';
      note.textContent = 'The API contract is not available.';

      root.append(note);

      return;
    }

    for (const group of buildOpenApiGroups(contract)) {
      const box = document.createElement('rdio-admin-openapi-box');

      box.group = group;

      // In the browser the append connects the box; the bound guard makes the
      // explicit call here safe for both worlds.
      box.connectedCallback();

      root.append(box);
    }
  }
}

customElements.define('rdio-admin-openapi', RdioAdminOpenApi);
