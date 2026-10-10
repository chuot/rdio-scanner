// rdio-admin-logs (T043): admin logs section (legacy logs.component.ts).
// Not a config section: no section bar, no store slice — the query state is
// local to the element. rdio-admin.js creates a fresh element on every section
// open, so connecting IS the reload-on-open behavior. Client pages of 10 over
// server pages of 200 reuse the search-pagination domain helpers.
//
// FEAT-010: the view narrows on the part of the server that wrote an entry, searches the message text,
// shows how many entries each part wrote, and can keep itself current. The decisions behind all four —
// what the filters mean as a request, what the counts become, when a refresh is due — are in
// domain/log-search.js and domain/log-refresh.js; this owns the rendering.

import { getAdminContext } from '../../application/admin-context.js';
import {
  LOG_CLIENT_PAGE,
  LOG_SERVER_PAGE,
  logSearchFilterOptions,
  logSearchOptions,
  logSourceCounts,
} from '../../domain/log-search.js';
import { createRefreshSchedule } from '../../domain/log-refresh.js';
import { clientSlice, needsRefetch } from '../../../../common/frontend/domain/search-pagination.js';
import { formatMonthDay, formatTime } from '../../../../common/frontend/presentation/display-format.js';
import { showSnackbar } from '../../../../common/frontend/presentation/ui/primitives.js';

const LEVEL_ICONS = {
  error: { icon: 'error', className: 'error' },
  info: { icon: 'notifications', className: 'info' },
  warn: { icon: 'warning', className: 'warn' },
};

class RdioAdminLogs extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    this.query = null;
    this.offset = 0;
    this.pending = false;

    this.innerHTML = `
      <div class="rdio-admin-card logs">
        <div class="rdio-log-sources" role="group" aria-label="Entries by part of the server"></div>
        <div class="rdio-list">
          <table>
            <thead><tr><th>Level</th><th>Date</th><th>Time</th><th>Message</th></tr></thead>
            <tbody></tbody>
          </table>
        </div>
        <div class="rdio-progress" aria-busy="false" hidden></div>
        <rdio-paginator page-size="${LOG_CLIENT_PAGE}" length="0"></rdio-paginator>
        <div class="rdio-filters">
          <form autocomplete="off">
            <rdio-form-field>
              <label>Sort order</label>
              <rdio-select name="sort" options='[["1","Ascending"],["-1","Descending"]]' value="-1"></rdio-select>
            </rdio-form-field>
            <rdio-form-field>
              <label>Date</label>
              <input name="date" type="datetime-local" placeholder="Choose a date" />
            </rdio-form-field>
            <rdio-form-field>
              <label>Level</label>
              <rdio-select name="level" options='[["","All Levels"],["info","Informative"],["warn","Warning"],["error","Error"]]' value=""></rdio-select>
            </rdio-form-field>
            <rdio-form-field>
              <label>Subsystem</label>
              <rdio-select name="source" value=""></rdio-select>
            </rdio-form-field>
            <rdio-form-field>
              <label>Text</label>
              <input name="text" type="search" placeholder="Search the message" />
            </rdio-form-field>
            <rdio-form-field>
              <label>Auto-refresh</label>
              <rdio-toggle name="autoRefresh" aria-label="Auto-refresh"></rdio-toggle>
            </rdio-form-field>
            <div class="actions">
              <button type="button" class="rdio-button-mini reload">Reload</button>
              <button type="button" class="rdio-button-mini reset">Reset</button>
            </div>
          </form>
        </div>
      </div>
    `;

    this.tbody = this.querySelector('tbody');
    this.progress = this.querySelector('.rdio-progress');
    this.paginator = this.querySelector('rdio-paginator');
    this.sourceCounts = this.querySelector('.rdio-log-sources');
    this.sortSelect = this.querySelector('rdio-select[name="sort"]');
    this.dateInput = this.querySelector('input[name="date"]');
    this.levelSelect = this.querySelector('rdio-select[name="level"]');
    this.sourceSelect = this.querySelector('rdio-select[name="source"]');
    this.textInput = this.querySelector('input[name="text"]');
    this.autoRefreshToggle = this.querySelector('rdio-toggle[name="autoRefresh"]');
    this.reloadButton = this.querySelector('button.reload');
    this.resetButton = this.querySelector('button.reset');

    // The subsystem choices are the server's vocabulary, and they are built from it rather than written
    // out here: a select that names the subsystems by hand is a select that silently lacks the one the
    // server gains next release.
    this.sourceSelect.options = logSearchFilterOptions();

    // rdio-form-field only labels native controls; label the inner selects.
    this.sortSelect.select.setAttribute('aria-label', 'Sort order');
    this.levelSelect.select.setAttribute('aria-label', 'Level');
    this.sourceSelect.select.setAttribute('aria-label', 'Subsystem');

    this.sortSelect.addEventListener('change', () => this.formChangeHandler());
    this.dateInput.addEventListener('change', () => this.formChangeHandler());
    this.levelSelect.addEventListener('change', () => this.formChangeHandler());
    this.sourceSelect.addEventListener('change', () => this.formChangeHandler());
    this.textInput.addEventListener('change', () => this.formChangeHandler());

    this.reloadButton.addEventListener('click', () => this.reload());
    this.resetButton.addEventListener('click', () => this.reset());

    this.paginator.addEventListener('page', () => this.refresh());

    // The counts are how the view offers the narrowing: an operator who sees that the directory watchers
    // wrote 137 of the 183 entries in front of them is one click away from a log that is only those 137.
    this.sourceCounts.addEventListener('click', (event) => {
      const source = event.target?.dataset?.source;

      if (!source) return;

      this.sourceSelect.value = source;

      this.formChangeHandler();
    });

    this.autoRefreshToggle.addEventListener('change', () => this.autoRefreshChangeHandler());

    this.refreshSchedule = createRefreshSchedule({
      poll: () => this.reload(),
      isBusy: () => this.pending,
    });

    this.setPending(false);

    this.reload();
  }

  // The element is created fresh every time the section is opened, and a schedule outliving the element it
  // was started for is a request for a log view nobody is looking at.
  disconnectedCallback() {
    this.refreshSchedule?.stop();
  }

  isDefaultFilters() {
    return (
      this.sortSelect.value === '-1' &&
      this.levelSelect.value === '' &&
      this.sourceSelect.value === '' &&
      this.textInput.value === '' &&
      this.dateInput.value === ''
    );
  }

  formChangeHandler() {
    this.paginator.firstPage();

    this.reload();
  }

  autoRefreshChangeHandler() {
    if (this.autoRefreshToggle.checked) {
      this.refreshSchedule.start();

      return;
    }

    this.refreshSchedule.stop();
  }

  reset() {
    this.sortSelect.value = '-1';
    this.levelSelect.value = '';
    this.sourceSelect.value = '';
    this.textInput.value = '';
    this.dateInput.value = '';

    this.formChangeHandler();
  }

  refresh() {
    const windowStart = this.paginator.pageIndex * LOG_CLIENT_PAGE;
    const windowEnd = windowStart + LOG_CLIENT_PAGE - 1;

    if (!this.pending && needsRefetch(windowStart, this.offset, LOG_SERVER_PAGE)) {
      this.reload();

      return;
    }

    this.paginator.setLength(this.query?.count ?? 0);

    const rows = clientSlice(this.query?.logs ?? [], windowStart, windowEnd, LOG_SERVER_PAGE, LOG_CLIENT_PAGE);

    this.tbody.replaceChildren(...rows.map((log) => this.rowElement(log)));

    this.renderSourceCounts();
  }

  // The counts describe everything the criteria match — except the subsystem filter itself, which is the
  // point of them: a strip narrowed to the one subsystem already chosen would offer nothing to narrow to
  // next.
  renderSourceCounts() {
    const counts = logSourceCounts(this.query?.sources);

    if (counts.length === 0) {
      this.sourceCounts.hidden = true;
      this.sourceCounts.replaceChildren();

      return;
    }

    this.sourceCounts.hidden = false;

    const selected = this.sourceSelect.value;

    this.sourceCounts.replaceChildren(
      ...counts.map((entry) => {
        const button = document.createElement('button');

        button.type = 'button';
        button.className = 'rdio-log-source';
        button.dataset.source = entry.source;
        button.setAttribute('aria-pressed', String(entry.source === selected));
        button.textContent = `${entry.label} (${entry.count})`;

        return button;
      }),
    );
  }

  async reload() {
    const options = logSearchOptions({
      pageIndex: this.paginator.pageIndex,
      sort: Number(this.sortSelect.value),
      level: this.levelSelect.value,
      source: this.sourceSelect.value,
      text: this.textInput.value.trim(),
      date: this.dateInput.value ? new Date(this.dateInput.value).toISOString() : '',
    });

    this.offset = options.offset;

    this.pending = true;
    this.setPending(true);

    try {
      const query = await getAdminContext().api.searchLogs(options);

      if (!this.isConnected) return;

      this.query = query;
    } catch {
      this.query = null;

      showSnackbar('Unable to load logs');
    }

    this.dateInput.min = toLocalInputValue(this.query?.dateStart);
    this.dateInput.max = toLocalInputValue(this.query?.dateStop);

    this.pending = false;
    this.setPending(false);

    this.refresh();
  }

  setPending(pending) {
    this.progress.hidden = !pending;
    this.progress.setAttribute('aria-busy', String(pending));

    this.sortSelect.select.disabled = pending;
    this.dateInput.disabled = pending;
    this.levelSelect.select.disabled = pending;
    this.sourceSelect.select.disabled = pending;
    this.textInput.disabled = pending;

    this.reloadButton.disabled = pending;
    this.resetButton.disabled = pending || this.isDefaultFilters();
  }

  rowElement(log) {
    const tr = document.createElement('tr');

    if (!log) {
      for (let i = 0; i < 4; i++) tr.append(document.createElement('td'));

      return tr;
    }

    const levelCell = document.createElement('td');

    const meta = LEVEL_ICONS[log.level];

    if (meta) {
      const icon = document.createElement('i');
      icon.className = `material-icons ${meta.className}`;
      icon.setAttribute('aria-hidden', 'true');
      icon.textContent = meta.icon;
      levelCell.append(icon);
    }

    const date = new Date(log.dateTime);
    const valid = !Number.isNaN(date.getTime());

    const dateCell = document.createElement('td');
    dateCell.textContent = valid ? formatMonthDay(date) : '';

    const timeCell = document.createElement('td');
    timeCell.textContent = valid ? formatTime(date, false) : '';

    const messageCell = document.createElement('td');
    messageCell.textContent = log.message ?? '';

    tr.append(levelCell, dateCell, timeCell, messageCell);

    return tr;
  }
}

customElements.define('rdio-admin-logs', RdioAdminLogs);

function toLocalInputValue(value) {
  if (!value) return '';

  const date = new Date(value);

  if (Number.isNaN(date.getTime())) return '';

  const pad = (n) => String(n).padStart(2, '0');

  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}
