// rdio-admin-tools: admin tools, not a config section: no section bar.
// Imports make each affected section dirty, and each is saved
// individually.

import { getAdminContext } from '../../application/admin-context.js';
import { ADMIN_CONFIG_SECTIONS } from '../../../../common/frontend/domain/config.js';
import { migrateLegacyConfig } from '../../domain/config-legacy-keys.js';
import { importTalkgroups, importUnits, parseCsv } from '../../domain/csv-import.js';
import { version } from '../../../../common/frontend/infrastructure/version.js';
import { sectionWorking, setSectionWorking } from './section-shell.js';
import { passwordPanel } from './password-panel.js';
import { showSnackbar } from '../../../../common/frontend/presentation/ui/primitives.js';

class RdioAdminTools extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    this.render();
  }

  currentConfig() {
    return {
      access: sectionWorking('access') ?? [],
      apikeys: sectionWorking('apikeys') ?? [],
      dirwatch: sectionWorking('dirwatch') ?? [],
      downstreams: sectionWorking('downstreams') ?? [],
      groups: sectionWorking('groups') ?? [],
      options: sectionWorking('options') ?? {},
      systems: sectionWorking('systems') ?? [],
      tags: sectionWorking('tags') ?? [],
    };
  }

  render() {
    const root = document.createElement('div');
    root.className = 'rdio-admin-card tools';

    root.append(this.exportPanel());
    root.append(this.importPanel());
    root.append(this.csvPanel('talkgroups', 'Import talkgroups', 'Upload a talkgroups CSV file (id, frequency, label, group, name, tag, description columns). Missing groups and tags are created automatically.'));
    root.append(this.csvPanel('units', 'Import units', 'Upload a units CSV file (id, label columns).'));
    root.append(passwordPanel());

    this.replaceChildren(root);
  }

  panel(title, iconName) {
    const details = document.createElement('details');
    details.className = 'rdio-admin-row';

    const summary = document.createElement('summary');

    const icon = document.createElement('i');
    icon.className = 'material-icons';
    icon.setAttribute('aria-hidden', 'true');
    icon.textContent = iconName;

    const titleSpan = document.createElement('span');
    titleSpan.className = 'row-title';
    titleSpan.textContent = title;

    summary.append(icon, titleSpan);
    details.append(summary);

    return details;
  }

  // filePicker: mini button opening a hidden native input, with a
  // caption for the chosen file name: the native picker button is
  // browser-styled, so it never sits in a row itself.
  filePicker(accept, onChange) {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = accept;
    input.hidden = true;

    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'rdio-button-mini';
    button.innerHTML = `<i class="material-icons" aria-hidden="true">upload_file</i><span>Choose file</span>`;
    button.addEventListener('click', () => input.click());

    const name = document.createElement('span');
    name.className = 'file-name';

    input.addEventListener('change', () => {
      const file = input.files?.item(0);

      name.textContent = file ? file.name : '';

      // Reset so picking the same file again fires the change event.
      input.value = '';

      if (file) onChange(file);
    });

    return { button, name, input };
  }

  // export config and import config stay two panels: a button and a
  // file picker in one panel read as a single flow that does not exist.

  exportPanel() {
    const details = this.panel('Export config', 'file_download');

    const exportButton = document.createElement('button');
    exportButton.type = 'button';
    exportButton.className = 'rdio-button-mini';
    exportButton.textContent = 'Export config';
    exportButton.addEventListener('click', async () => {
      const { api } = getAdminContext();

      try {
        const res = await api.getConfig();

        this.downloadFile(`rdio-scanner-${version}.config.json`, JSON.stringify(res.config));

        showSnackbar('Config exported');
      } catch {
        showSnackbar('Unable to export config');
      }
    });

    const captionEl = document.createElement('p');
    captionEl.className = 'caption';
    captionEl.textContent = 'Export downloads the saved config as JSON.';

    const row = document.createElement('div');
    row.className = 'row top';
    row.append(exportButton);

    details.append(captionEl, row);

    return details;
  }

  importPanel() {
    const details = this.panel('Import config', 'file_upload');

    const picker = this.filePicker('.json,application/json', async (file) => {
      try {
        const config = migrateLegacyConfig(JSON.parse(await file.text()));

        let imported = 0;

        for (const name of ADMIN_CONFIG_SECTIONS) {
          if (config[name] === undefined) continue;

          setSectionWorking(name, config[name]);

          imported += 1;
        }

        showSnackbar(`Config imported into ${imported} sections — save each section to apply`);
      } catch {
        showSnackbar('Invalid config file');
      }
    });

    const captionEl = document.createElement('p');
    captionEl.className = 'caption';
    captionEl.textContent = 'Choose a config JSON file to load it into the section working copies, then save each section to apply.';

    const row = document.createElement('div');
    row.className = 'row top';
    row.append(picker.button, picker.name);

    details.append(captionEl, row, picker.input);

    return details;
  }

  // --- csv imports ---------------------------------------------------------------

  csvPanel(kind, title, caption) {
    const details = this.panel(title, kind === 'talkgroups' ? 'group_work' : 'directions_car');

    const captionEl = document.createElement('p');
    captionEl.className = 'caption';
    captionEl.textContent = caption;

    // Target system: saved systems only (imported rows need a real id).
    const systemSelect = document.createElement('rdio-select');

    const systems = (sectionWorking('systems') ?? []).filter((system) => typeof system.id === 'number' && system.id > 0);

    systemSelect.setAttribute('options', JSON.stringify(systems.map((system) => [String(system.id), system.label])));

    let systemId = systems[0] ? String(systems[0].id) : '';

    if (systemId) systemSelect.setAttribute('value', systemId);

    systemSelect.addEventListener('change', (ev) => {
      if (typeof ev.detail === 'string') systemId = ev.detail;
    });

    let rows = [];

    const table = document.createElement('table');
    table.className = 'rdio-admin-csv-preview';

    const importButton = document.createElement('button');
    importButton.type = 'button';
    importButton.className = 'rdio-button-mini import';
    importButton.textContent = kind === 'talkgroups' ? 'Import talkgroups' : 'Import units';
    importButton.disabled = true;

    const renderTable = () => {
      const head = document.createElement('tr');

      for (const column of kind === 'talkgroups' ? ['id', 'label', 'name', 'tag', 'group'] : ['id', 'label']) {
        const th = document.createElement('th');
        th.textContent = column;
        head.append(th);
      }

      table.replaceChildren(head);

      for (const [rowIndex, row] of rows.entries()) {
        const tr = document.createElement('tr');

        const cells = kind === 'talkgroups' ? [row[0], row[2], row[4], row[5], row[6]] : [row[0], row[1]];

        for (const cell of cells) {
          const td = document.createElement('td');
          td.textContent = cell ?? '';
          tr.append(td);
        }

        const action = document.createElement('td');

        const remove = document.createElement('button');
        remove.type = 'button';
        remove.className = 'rdio-icon-button';
        remove.setAttribute('aria-label', 'Remove row');
        remove.innerHTML = '<i class="material-icons" aria-hidden="true">delete</i>';
        remove.addEventListener('click', () => {
          rows = rows.filter((_, i) => i !== rowIndex);

          renderTable();
          importButton.disabled = rows.length === 0;
        });

        action.append(remove);
        tr.append(action);
        table.append(tr);
      }

      importButton.disabled = rows.length === 0;
    };

    const picker = this.filePicker('.csv,text/csv', async (file) => {
      rows = parseCsv(await file.text());

      renderTable();
    });

    importButton.addEventListener('click', () => {
      const system = systems.find((s) => String(s.id) === systemId);

      if (!system || rows.length === 0) return;

      const imported = kind === 'talkgroups'
        ? importTalkgroups(this.currentConfig(), system, rows)
        : importUnits(this.currentConfig(), system, rows);

      setSectionWorking('systems', imported.systems);

      if (kind === 'talkgroups') {
        setSectionWorking('groups', imported.groups);
        setSectionWorking('tags', imported.tags);
      }

      rows = [];

      renderTable();

      showSnackbar(kind === 'talkgroups' ? 'Talkgroups imported — save the systems, groups and tags sections to apply' : 'Units imported — save the systems section to apply');
    });

    const top = document.createElement('div');
    top.className = 'row top';
    top.append(systemSelect, picker.button, picker.name);

    const bottom = document.createElement('div');
    bottom.className = 'row bottom';
    bottom.append(importButton);

    details.append(captionEl, top, picker.input, table, bottom);

    return details;
  }

  downloadFile(fileName, contents) {
    const blob = new Blob([contents], { type: 'application/json' });

    const url = URL.createObjectURL(blob);

    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = fileName;

    document.body.append(anchor);
    anchor.click();
    anchor.remove();

    URL.revokeObjectURL(url);
  }
}

customElements.define('rdio-admin-tools', RdioAdminTools);
