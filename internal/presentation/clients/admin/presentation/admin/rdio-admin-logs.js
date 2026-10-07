// rdio-admin-logs (T043): admin logs section (legacy logs.component.ts).
// Not a config section: no section bar, no store slice — the query state is
// local to the element. rdio-admin.js creates a fresh element on every section
// open, so connecting IS the reload-on-open behavior. Client pages of 10 over
// server pages of 200 reuse the search-pagination domain helpers.

import { getAdminContext } from '../../application/admin-context.js';
import { clientSlice, needsRefetch, serverOffset } from '../../../shared/domain/search-pagination.js';
import { formatMonthDay, formatTime } from '../../../shared/presentation/display-format.js';
import { showSnackbar } from '../../../shared/presentation/ui/primitives.js';

const SERVER_PAGE = 200;
const CLIENT_PAGE = 10;

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
        <div class="rdio-list">
          <table>
            <thead><tr><th>Level</th><th>Date</th><th>Time</th><th>Message</th></tr></thead>
            <tbody></tbody>
          </table>
        </div>
        <div class="rdio-progress" aria-busy="false" hidden></div>
        <rdio-paginator page-size="${CLIENT_PAGE}" length="0"></rdio-paginator>
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
    this.sortSelect = this.querySelector('rdio-select[name="sort"]');
    this.dateInput = this.querySelector('input[name="date"]');
    this.levelSelect = this.querySelector('rdio-select[name="level"]');
    this.reloadButton = this.querySelector('button.reload');
    this.resetButton = this.querySelector('button.reset');

    // rdio-form-field only labels native controls; label the inner selects.
    this.sortSelect.select.setAttribute('aria-label', 'Sort order');
    this.levelSelect.select.setAttribute('aria-label', 'Level');

    this.sortSelect.addEventListener('change', () => this.formChangeHandler());
    this.dateInput.addEventListener('change', () => this.formChangeHandler());
    this.levelSelect.addEventListener('change', () => this.formChangeHandler());

    this.reloadButton.addEventListener('click', () => this.reload());
    this.resetButton.addEventListener('click', () => this.reset());

    this.paginator.addEventListener('page', () => this.refresh());

    this.setPending(false);

    this.reload();
  }

  isDefaultFilters() {
    return this.sortSelect.value === '-1' && this.levelSelect.value === '' && this.dateInput.value === '';
  }

  formChangeHandler() {
    this.paginator.firstPage();

    this.reload();
  }

  reset() {
    this.sortSelect.value = '-1';
    this.levelSelect.value = '';
    this.dateInput.value = '';

    this.formChangeHandler();
  }

  refresh() {
    const windowStart = this.paginator.pageIndex * CLIENT_PAGE;
    const windowEnd = windowStart + CLIENT_PAGE - 1;

    if (!this.pending && needsRefetch(windowStart, this.offset, SERVER_PAGE)) {
      this.reload();

      return;
    }

    this.paginator.setLength(this.query?.count ?? 0);

    const rows = clientSlice(this.query?.logs ?? [], windowStart, windowEnd, SERVER_PAGE, CLIENT_PAGE);

    this.tbody.replaceChildren(...rows.map((log) => this.rowElement(log)));
  }

  async reload() {
    const options = {
      limit: SERVER_PAGE,
      offset: serverOffset(this.paginator.pageIndex, CLIENT_PAGE, SERVER_PAGE),
      sort: Number(this.sortSelect.value),
    };

    this.offset = options.offset;

    if (this.levelSelect.value) options.level = this.levelSelect.value;
    if (this.dateInput.value) options.date = new Date(this.dateInput.value).toISOString();

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
