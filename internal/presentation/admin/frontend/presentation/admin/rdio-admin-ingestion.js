// rdio-admin-ingestion (FEAT-016, T020): the Ingestion section. Informational
// only: it explains the three ways calls reach the server and names the OpenAPI
// entry point of each. Nothing here is edited, so there is no store slice and
// no save bar; the editing lives in the API Keys, Dirwatch and Downstreams
// sections. The help box at the bottom names the capture endpoint from
// docs/api.md.

import { openApiBox } from './section-shell.js';

const METHODS = [
  [
    'API key capture',
    'upload',
    'A recording reaches the server through POST /api/call-upload, keyed by an apikey that scopes the systems and talkgroups it may write to. The upload is a multipart form: the recording file, the system, the talkgroup, the frequency, the start time and the call type. Keys are defined in the API Keys section.',
  ],
  [
    'Dirwatch',
    'folder',
    'The server watches the configured directories for new audio files and ingests each one as a call. Editing lives in the Dirwatch section.',
  ],
  [
    'Downstreams',
    'import_export',
    'Another Rdio Scanner instance pushes its calls to this server through POST /api/call-upload, using an apikey you share with it. Editing lives in the Downstreams section.',
  ],
];

class RdioAdminIngestion extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    this.render();
  }

  render() {
    const root = document.createElement('div');
    root.className = 'rdio-admin-card ingestion';

    const intro = document.createElement('p');
    intro.textContent = 'Calls reach the server in three ways. This page explains each one and where it is configured; nothing on this page is saved.';

    root.append(intro);

    for (const [title, icon, body] of METHODS) {
      const details = document.createElement('details');
      details.className = 'rdio-admin-row';

      const summary = document.createElement('summary');

      const iconEl = document.createElement('i');
      iconEl.className = 'material-icons';
      iconEl.setAttribute('aria-hidden', 'true');
      iconEl.textContent = icon;

      const titleSpan = document.createElement('span');
      titleSpan.className = 'row-title';
      titleSpan.textContent = title;

      summary.append(iconEl, titleSpan);
      details.append(summary);

      const text = document.createElement('p');
      text.className = 'caption';
      text.textContent = body;

      details.append(text);
      root.append(details);
    }

    root.append(openApiBox('ingestion'));

    this.replaceChildren(root);
  }
}

customElements.define('rdio-admin-ingestion', RdioAdminIngestion);
