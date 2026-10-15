// rdio-search-panel: call search table + paginator (server pages of 200,
// client pages of 10), PLAY/DOWNLOAD toggle, cascading filter form
// (legacy search.component).

import { getAppContext } from '../../application/context.js';
import { computeFilterOptions, resolveCallSearchCriteria } from '../../domain/search-filters.js';
import { clientSlice, needsRefetch, serverOffset } from '../../../../common/frontend/domain/search-pagination.js';
import { formatDuration, formatMonthDay, formatTime, toLocalInputValue } from '../../../../common/frontend/presentation/display-format.js';
import { announce } from '../../../../common/frontend/presentation/ui/primitives.js';

const SERVER_PAGE = 200;
const CLIENT_PAGE = 10;

// The controls the operator narrows the call list with. They are one group because they are one
// thing: a search in flight makes the whole set of them unavailable at once.
const FILTER_CONTROLS = ['sort', 'date', 'system', 'talkgroup', 'group', 'tag'];

class RdioSearchPanel extends HTMLElement {
  connectedCallback() {
    const { store } = getAppContext();

    this.innerHTML = `
      <div class="rdio-list">
        <table>
          <thead><tr><th></th><th>Date</th><th>Time</th><th>Dur</th><th>System</th><th>Talkgroup</th><th>Name</th></tr></thead>
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
    this.resetButton = this.form.querySelector('.reset button');

    this.form.addEventListener('change', (ev) => {
      if (ev.target.name === 'sort' || ev.target.name === 'date' || ['system', 'talkgroup', 'group', 'tag'].includes(ev.target.name)) {
        this.formChangeHandler();
      }
    });

    this.form.querySelector('.reset').addEventListener('click', () => this.resetForm());

    this.paginator.addEventListener('page', () => {
      this.refreshResults();

      // The page the list moved to is the operator's own move; the count rides the server's
      // answer. Saying it after the results refresh keeps the page the newest word the page says.
      announce(`Page ${this.paginator.pageIndex + 1}`);
    });

    this.downloadMode.addEventListener('change', () => this.renderResults());

    this.querySelector('tbody').addEventListener('click', (ev) => this.rowClick(ev));

    // The counter rides every call-list answer, so the results render - and the progress
    // bar ends, and the form comes back to the operator - even when the answer equals the list
    // already on screen.
    this.unsubscribe = store.subscribe(['playbackList', 'playbackListSeq', 'call', 'livefeedMode', 'config'], () => {
      const seq = getAppContext().store.getState().playbackListSeq ?? 0;

      // The search in flight is answered by the answer the server sent, and that answer carries the
      // counter. When the counter moves, the search that left is over, and the form is the
      // operator's again. A store change that is not an answer - a call starting, a mode changing -
      // leaves the search in flight.
      if (seq !== this.lastAnsweredSeq) {
        this.lastAnsweredSeq = seq;
        this.resultsPending = false;
      }

      this.refreshFilters();
      this.refreshResults();
      this.renderProgress();
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

    // The form is the operator's while no search is in flight, and is not theirs while one is. A
    // change that started a search must not start a second one.
    if (this.resultsPending) return;

    if (useCases.livefeedMode === 'playback') useCases.stopPlaybackMode();

    this.paginator.firstPage();
    this.refreshFilters();
    this.searchCalls();
  }

  resetForm() {
    if (this.resultsPending) return;

    this.form.reset();
    this.paginator.firstPage();
    this.formChangeHandler();
  }

  searchCalls() {
    const { store, useCases } = getAppContext();

    if (useCases.livefeedMode === 'playback') return;

    const selection = this.selection();

    const options = {
      limit: SERVER_PAGE,
      offset: serverOffset(this.paginator.pageIndex, CLIENT_PAGE, SERVER_PAGE),
      sort: selection.sort,
    };

    if (selection.date) options.date = new Date(Date.parse(selection.date)).toISOString();

    // The operator reads labels, and the history is narrowed by reference: a filter sent as the
    // label the operator read is read as no filter at all, and the answer is the whole history.
    const criteria = resolveCallSearchCriteria(store.getState().config, selection);

    if (criteria.group) options.group = criteria.group;
    if (criteria.system) options.system = criteria.system;
    if (criteria.tag) options.tag = criteria.tag;
    if (criteria.talkgroup) options.talkgroup = criteria.talkgroup;

    this.resultsPending = true;
    this.renderProgress();

    // A form that goes quiet for no stated reason is a form the operator cannot trust. The search
    // says it has left, in the page's own words, and the count says where it landed.
    announce('Searching calls');

    useCases.searchCalls(options);
  }

  refreshResults() {
    const { useCases } = getAppContext();

    const from = this.paginator.pageIndex * CLIENT_PAGE;
    const list = useCases.playbackList;

    // A page move while a search is in flight asks the server for nothing: the request already
    // running will answer with the window the list is drawn from. The move itself stands, and the
    // rows come from the window the server already answered with - the same rule the administration
    // log screen follows, and the same one the v6.6.3 search screen followed behind its own guard.
    if (this.resultsPending) {
      this.renderResults();

      return;
    }

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

    // The date field is bounded to the window the search is answering with: a date the operator
    // types outside it is a date the list has no calls for. The two ends the server names are the
    // ends of the whole match, not of the page shown, and an empty match names the zero time - not
    // a date a person can be shown, so an empty match leaves the field open.
    this.form.elements.date.min = toLocalInputValue(list?.dateStart);
    this.form.elements.date.max = toLocalInputValue(list?.dateStop);

    // The count the search answered with is a number the operator cannot see on a locked screen.
    // The answer counter rides every answer, so the page says the count once per answer — not once
    // per paint — and says nothing before the first answer arrives.
    const seq = state.playbackListSeq ?? 0;

    if (seq !== this.lastAnnouncedSeq) {
      this.lastAnnouncedSeq = seq;

      if (list) announce(`${list.count} calls`);
    }

    if (!list) {
      this.tbody.innerHTML = '';
      return;
    }

    this.paginator.setLength(list.count);

    const from = this.paginator.pageIndex * CLIENT_PAGE;
    const to = from + CLIENT_PAGE - 1;

    const rows = clientSlice(list.results ?? [], from, to, SERVER_PAGE, CLIENT_PAGE);
    const downloadMode = this.downloadMode.checked;

    this.tbody.innerHTML = rows.map((row) => {
      if (!row) return '<tr><td colspan="7"></td></tr>';

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

      return `<tr><td>${control}</td><td><span>${formatMonthDay(date)}</span></td><td><span>${formatTime(date)}</span></td><td><span>${formatDuration(row.audioLength)}</span></td><td><span>${row.systemData?.label ?? row.system}</span></td><td><span>${row.talkgroupData?.label ?? row.talkgroup}</span></td><td><span>${row.talkgroupData?.name ?? ''}</span></td></tr>`;
    }).join('');
  }

  // The one place the search's state is painted: the bar that says a search is in flight, and the
  // form that says whether it is the operator's to use.
  //
  // The lock lands as a result of the operator's own change, and the keyboard is standing on the
  // control that caused it. `disabled` would take that control out of the tab order, the browser
  // would put the focus on the body, and the operator's next Tab would start at the top of the
  // document with the place they were working in gone. `aria-disabled` says the same thing to the
  // operator's assistive technology, keeps the control focusable so the keyboard keeps its place,
  // and costs nothing to release. The guards in the handlers are what stop the second search.
  renderProgress() {
    const pending = !!this.resultsPending;

    this.progressBar.classList.toggle('pending', pending);
    this.progressBar.setAttribute('aria-busy', String(pending));

    for (const control of this.filterControls()) {
      control.setAttribute('aria-disabled', String(pending));
      control.classList.toggle('locked', pending);
    }

    // Reset is part of the form the search is holding: a control that answers nothing has to say so.
    this.resetButton.setAttribute('aria-disabled', String(pending));
  }

  filterControls() {
    return FILTER_CONTROLS.map((name) => this.form.elements[name]);
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
