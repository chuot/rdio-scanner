// rdio-search-panel: call search table + paginator (server pages of 200,
// client pages of 10), PLAY/DOWNLOAD toggle, cascading filter form
// (legacy search.component).

import { getAppContext } from '../../application/context.js';
import { computeFilterOptions } from '../../domain/search-filters.js';
import { clientSlice, needsRefetch, serverOffset } from '../../../shared/domain/search-pagination.js';
import { formatMonthDay, formatTime } from '../../../shared/presentation/display-format.js';

const SERVER_PAGE = 200;
const CLIENT_PAGE = 10;

class RdioSearchPanel extends HTMLElement {
  connectedCallback() {
    const { store } = getAppContext();

    this.innerHTML = `
      <div class="rdio-list">
        <table>
          <thead><tr><th></th><th>Date</th><th>Time</th><th>System</th><th>Talkgroup</th><th>Name</th></tr></thead>
          <tbody></tbody>
        </table>
        <div class="progress-bar"></div>
        <div class="paginator">
          <rdio-toggle aria-label="Download mode" icon="save_alt"></rdio-toggle>
          <rdio-paginator page-size="${CLIENT_PAGE}" length="0" material></rdio-paginator>
        </div>
      </div>
      <div class="rdio-filters">
        <form autocomplete="off">
          <rdio-form-field><div class="rdio-field-box"><div class="rdio-field-flex"><label>Sort order</label><select name="sort"><option value="1">Ascending</option><option value="-1" selected>Descending</option></select></div></div></rdio-form-field>
          <rdio-form-field><div class="rdio-field-box"><div class="rdio-field-flex"><label>Date</label><input name="date" type="datetime-local" /></div></div></rdio-form-field>
          <rdio-form-field><div class="rdio-field-box"><div class="rdio-field-flex"><label>System</label><select name="system"><option value="">All Systems</option></select></div></div></rdio-form-field>
          <rdio-form-field><div class="rdio-field-box"><div class="rdio-field-flex"><label>Talkgroup</label><select name="talkgroup"><option value="">All Talkgroups</option></select></div></div></rdio-form-field>
          <rdio-form-field><div class="rdio-field-box"><div class="rdio-field-flex"><label>Group</label><select name="group"><option value="">All Groups</option></select></div></div></rdio-form-field>
          <rdio-form-field><div class="rdio-field-box"><div class="rdio-field-flex"><label>Tag</label><select name="tag"><option value="">All Tags</option></select></div></div></rdio-form-field>
          <div class="reset"><button type="button" class="rdio-button-mini">Reset</button></div>
        </form>
      </div>
    `;

    this.form = this.querySelector('form');
    this.table = this.querySelector('table');
    this.tbody = this.querySelector('tbody');
    this.paginator = this.querySelector('rdio-paginator');
    this.downloadMode = this.querySelector('rdio-toggle');
    this.progressBar = this.querySelector('.progress-bar');

    this.form.addEventListener('change', (ev) => {
      if (ev.target.name === 'sort' || ev.target.name === 'date' || ['system', 'talkgroup', 'group', 'tag'].includes(ev.target.name)) {
        this.formChangeHandler();
      }
    });

    this.form.querySelector('.reset').addEventListener('click', () => this.resetForm());

    this.paginator.addEventListener('page', () => this.refreshResults());

    this.downloadMode.addEventListener('change', () => this.renderResults());

    this.querySelector('tbody').addEventListener('click', (ev) => this.rowClick(ev));

    this.unsubscribe = store.subscribe(['playbackList', 'call', 'livefeedMode', 'config'], () => {
      this.refreshFilters();
      this.refreshResults();
    });

    this.refreshFilters();
  }

  disconnectedCallback() {
    this.unsubscribe?.();
  }

  open() {
    this.searchCalls();
  }

  selection() {
    const value = (name) => {
      const v = this.form.elements[name]?.value;
      return v === '' || v === null ? undefined : v;
    };

    return {
      sort: Number(this.form.elements.sort.value),
      date: value('date'),
      system: value('system'),
      talkgroup: value('talkgroup'),
      group: value('group'),
      tag: value('tag'),
    };
  }

  refreshFilters() {
    const { config } = getAppContext().store.getState();

    if (!config) return;

    const selection = this.selection();
    const options = computeFilterOptions(config, selection);

    const fill = (name, allLabel, labels) => {
      const select = this.form.elements[name];
      const current = select.value;

      select.innerHTML = `<option value="">${allLabel}</option>`;

      for (const label of labels) {
        const option = document.createElement('option');
        option.value = label;
        option.textContent = label;
        select.append(option);
      }

      select.value = labels.includes(current) ? current : '';
    };

    fill('system', 'All Systems', options.systems);
    fill('talkgroup', 'All Talkgroups', options.talkgroups);
    fill('group', 'All Groups', options.groups);
    fill('tag', 'All Tags', options.tags);
  }

  formChangeHandler() {
    const { useCases } = getAppContext();

    if (useCases.livefeedMode === 'playback') useCases.stopPlaybackMode();

    this.paginator.firstPage();
    this.refreshFilters();
    this.searchCalls();
  }

  resetForm() {
    this.form.reset();
    this.paginator.firstPage();
    this.formChangeHandler();
  }

  searchCalls() {
    const { useCases } = getAppContext();

    if (useCases.livefeedMode === 'playback') return;

    const selection = this.selection();

    const options = {
      limit: SERVER_PAGE,
      offset: serverOffset(this.paginator.pageIndex, CLIENT_PAGE, SERVER_PAGE),
      sort: selection.sort,
    };

    if (selection.date) options.date = new Date(Date.parse(selection.date)).toISOString();
    if (selection.group) options.group = selection.group;
    if (selection.system) options.system = selection.system;
    if (selection.tag) options.tag = selection.tag;
    if (selection.talkgroup) options.talkgroup = selection.talkgroup;

    this.resultsPending = true;
    this.renderProgress();

    useCases.searchCalls(options);
  }

  refreshResults() {
    const { useCases } = getAppContext();

    const from = this.paginator.pageIndex * CLIENT_PAGE;
    const list = useCases.playbackList;

    if (list && needsRefetch(from, list.options?.offset ?? 0, SERVER_PAGE)) {
      this.searchCalls();
    } else {
      this.renderResults();
    }
  }

  renderResults() {
    const { store, useCases } = getAppContext();
    const state = store.getState();
    const list = state.playbackList;

    this.resultsPending = false;
    this.renderProgress();

    if (!list) {
      this.tbody.innerHTML = '';
      return;
    }

    this.paginator.setLength(list.count);

    const from = this.paginator.pageIndex * CLIENT_PAGE;
    const to = from + CLIENT_PAGE - 1;

    const rows = clientSlice(list.results ?? [], from, to, SERVER_PAGE, CLIENT_PAGE);
    const time12h = state.config?.time12hFormat ?? false;
    const downloadMode = this.downloadMode.checked;

    // The legacy marks each time cell with the clock format; marking the table
    // says the same thing to every cell at once.
    this.table.classList.toggle('time12h', time12h);

    this.tbody.innerHTML = rows.map((row) => {
      if (!row) return '<tr><td colspan="6"></td></tr>';

      let control = '';

      if (downloadMode) {
        control = `<button type="button" class="row-action" data-download="${row.id}" aria-label="Download"><i class="material-icons">save_alt</i></button>`;
      } else if (row.id === useCases.playbackPending) {
        control = '<button type="button" class="row-action" disabled aria-label="Loading"><i class="material-icons spin">cached</i></button>';
      } else if (state.call && row.id === state.call.id) {
        control = `<button type="button" class="row-action" data-stop="${row.id}" aria-label="Stop"><i class="material-icons">stop</i></button>`;
      } else {
        control = `<button type="button" class="row-action" data-play="${row.id}" aria-label="Play"><i class="material-icons">play_arrow</i></button>`;
      }

      const date = new Date(row.dateTime);

      return `<tr><td>${control}</td><td><span>${formatMonthDay(date)}</span></td><td><span>${formatTime(date, time12h)}</span></td><td><span>${row.systemData?.label ?? row.system}</span></td><td><span>${row.talkgroupData?.label ?? row.talkgroup}</span></td><td><span>${row.talkgroupData?.name ?? ''}</span></td></tr>`;
    }).join('');
  }

  renderProgress() {
    this.progressBar.classList.toggle('pending', !!this.resultsPending);
  }

  rowClick(ev) {
    const { useCases } = getAppContext();

    const button = ev.target.closest('button[data-play],button[data-download],button[data-stop]');

    if (!button) return;

    if (button.dataset.play) useCases.loadAndPlay(Number(button.dataset.play));
    else if (button.dataset.download) useCases.download(Number(button.dataset.download));
    else if (button.dataset.stop) useCases.stop();
  }
}

customElements.define('rdio-search-panel', RdioSearchPanel);
