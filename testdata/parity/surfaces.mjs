// The surfaces the parity probe measures, the states each one must be present in, and the tolerance a measured
// value must fall inside to count as a match.
//
// This file is the machine-readable form of the selector table in
// docs/specs/FEAT-007-scanner-styling-parity/contract-parity-probe.md. A change here and a change there are the
// same change: do one or the other, never both separately.
//
// A locator is a CSS selector plus an optional text test. The text test is needed because the legacy control
// buttons carry no attribute that says what they are — only the words printed on them.

export const viewports = [
  { name: 'wide', width: 1280, height: 1024 },
  { name: 'medium', width: 980, height: 1024 },
  { name: 'narrow', width: 420, height: 800 },
];

// The states, in the order they are driven. Each is reached the same way on both sides: real mouse presses on the
// controls the page itself offers.
export const states = [
  'idle',
  'live-call',
  'controls',
  'select-open',
  'search-open-24h',
  'search-open-12h',
  'search-pending',
  'unlock',
  'snackbar-help',
  'snackbar-promo',
];

// The properties read off getComputedStyle. A property neither side sets is dropped from the comparison rather
// than reported as a difference between two identical "auto" values.
export const computedProperties = [
  'display',
  'position',
  'box-sizing',
  'flex-direction',
  'flex-basis',
  'flex-grow',
  'align-items',
  'justify-content',
  'padding-top',
  'padding-right',
  'padding-bottom',
  'padding-left',
  'margin-top',
  'margin-right',
  'margin-bottom',
  'margin-left',
  'color',
  'background-color',
  'background-image',
  'border-top-width',
  'border-right-width',
  'border-bottom-width',
  'border-left-width',
  'border-top-color',
  'border-right-color',
  'border-bottom-color',
  'border-left-color',
  'border-top-style',
  'border-right-style',
  'border-bottom-style',
  'border-left-style',
  'border-top-left-radius',
  'border-top-right-radius',
  'border-bottom-left-radius',
  'border-bottom-right-radius',
  'box-shadow',
  'opacity',
  'font-family',
  'font-size',
  'font-weight',
  'line-height',
  'letter-spacing',
  'text-transform',
  'white-space',
  'transition-property',
  'transition-duration',
  'transition-timing-function',
  'animation-name',
  'animation-duration',
  'animation-timing-function',
  'animation-iteration-count',
  'transform',
];

// Tolerance, as the contract states it.
export const tolerance = {
  px: 1, // boxes, padding, margins, border widths, radii
  blur: 2, // box-shadow blur radii
  lineHeight: 0.5,
  letterSpacing: 0.05,
  duration: 50, // animation and transition durations
};

// One entry per measured surface. `states` lists where the pair must resolve on both sides; a pair missing from a
// state it is required in is a harness error, not a gap. `viewports` lists where it is measured, so a surface that
// does not respond to width is measured once instead of three times.
export const pairs = [
  {
    id: 1, name: 'shell',
    legacy: { css: 'mat-sidenav-container' }, current: { css: 'rdio-app' },
    states: ['idle', 'live-call'], viewports: ['wide'],
  },
  {
    id: 2, name: 'content column',
    legacy: { css: 'rdio-scanner-main' }, current: { css: 'rdio-app > section.content' },
    states: ['idle', 'live-call'], viewports: ['wide', 'narrow'],
  },
  {
    id: 3, name: 'search drawer',
    legacy: { css: 'mat-sidenav[position="start"]' }, current: { css: 'rdio-app .panel.search' },
    states: ['search-open-24h', 'search-open-12h'], viewports: ['wide', 'narrow'],
  },
  {
    id: 4, name: 'select drawer',
    legacy: { css: 'mat-sidenav[position="end"]' }, current: { css: 'rdio-app .panel.select' },
    states: ['select-open'], viewports: ['wide', 'narrow'],
  },
  {
    id: 5, name: 'search panel toolbar',
    legacy: { css: '.panel.search mat-toolbar' }, current: { css: 'rdio-app .panel.search .toolbar' },
    states: ['search-open-24h'], viewports: ['wide', 'narrow'],
  },
  {
    id: 6, name: 'select panel toolbar',
    legacy: { css: '.panel.select mat-toolbar' }, current: { css: 'rdio-app .panel.select .toolbar' },
    states: ['select-open'], viewports: ['wide', 'narrow'],
  },
  {
    id: 7, name: 'search toolbar inner column',
    legacy: { css: '.panel.search mat-toolbar > div' }, current: { css: 'rdio-app .panel.search .toolbar > div' },
    states: ['search-open-24h'], viewports: ['wide', 'narrow'],
  },
  {
    id: 8, name: 'select toolbar inner column',
    legacy: { css: '.panel.select mat-toolbar > div' }, current: { css: 'rdio-app .panel.select .toolbar > div' },
    states: ['select-open'], viewports: ['wide', 'narrow'],
  },
  {
    id: 9, name: 'search close control',
    legacy: { css: '.panel.search mat-toolbar button.mat-mdc-icon-button' }, current: { css: 'rdio-app .panel.search .panel-close' },
    states: ['search-open-24h'], viewports: ['wide'],
  },
  {
    id: 10, name: 'select close control',
    legacy: { css: '.panel.select mat-toolbar button.mat-mdc-icon-button' }, current: { css: 'rdio-app .panel.select .panel-close' },
    states: ['select-open'], viewports: ['wide'],
  },
  {
    id: 11, name: 'search scroll area',
    legacy: { css: '.panel.search > div' }, current: { css: 'rdio-app .panel.search > div.scroll' },
    states: ['search-open-24h'], viewports: ['wide'],
  },
  {
    id: 12, name: 'select scroll area',
    legacy: { css: '.panel.select > div' }, current: { css: 'rdio-app .panel.select > div.scroll' },
    states: ['select-open'], viewports: ['wide'],
  },
  {
    id: 13, name: 'status bar',
    legacy: { css: '.rdio-status' }, current: { css: 'rdio-status' },
    states: ['idle', 'live-call'], viewports: ['wide', 'narrow'],
  },
  {
    id: 14, name: 'branding line',
    legacy: { css: '.rdio-status .branding' }, current: { css: 'rdio-status .branding' },
    states: ['idle', 'live-call'], viewports: ['wide'],
  },
  {
    id: 15, name: 'status LED',
    legacy: { css: '.rdio-status .led' }, current: { css: 'rdio-status .led' },
    states: ['idle', 'live-call', 'controls'], viewports: ['wide'],
  },
  {
    id: 16, name: 'display',
    legacy: { css: '.rdio-display' }, current: { css: 'rdio-display' },
    states: ['idle', 'live-call'], viewports: ['wide', 'narrow'],
  },
  {
    id: 17, name: 'display row',
    legacy: { css: '.rdio-display .row' }, current: { css: 'rdio-display .row' },
    states: ['idle', 'live-call'], viewports: ['wide'],
  },
  {
    id: 18, name: 'display row, big',
    legacy: { css: '.rdio-display .row.big' }, current: { css: 'rdio-display .row.big' },
    states: ['idle', 'live-call'], viewports: ['wide'],
  },
  {
    id: 19, name: 'display row, small',
    legacy: { css: '.rdio-display .row.small' }, current: { css: 'rdio-display .row.small' },
    states: ['idle', 'live-call'], viewports: ['wide'],
  },
  {
    id: 20, name: 'display column',
    legacy: { css: '.rdio-display .row > div:nth-child(2)' }, current: { css: 'rdio-display .row > div:nth-child(2)' },
    states: ['idle', 'live-call'], viewports: ['wide', 'narrow'],
  },
  {
    id: 21, name: 'display flag',
    legacy: { css: '.rdio-display .flag' }, current: { css: 'rdio-display .flag' },
    states: ['live-call'], viewports: ['wide'],
  },
  {
    id: 22, name: 'history row',
    legacy: { css: '.rdio-display table.history tbody tr' }, current: { css: 'rdio-display .history tbody tr' },
    states: ['live-call'], viewports: ['wide'],
  },
  {
    id: 23, name: 'unlock overlay',
    legacy: { css: '.rdio-display .auth' }, current: { css: 'rdio-display .auth' },
    states: ['unlock'], viewports: ['wide'],
  },
  {
    id: 24, name: 'unlock card',
    legacy: { css: '.rdio-display .auth form' }, current: { css: 'rdio-display .auth form' },
    states: ['unlock'], viewports: ['wide'],
  },
  {
    id: 25, name: 'unlock field',
    legacy: { css: '.rdio-display .auth .mat-mdc-form-field' }, current: { css: 'rdio-display .auth rdio-form-field' },
    states: ['unlock'], viewports: ['wide'],
  },
  {
    id: 26, name: 'unlock error line',
    legacy: { css: '.rdio-display .auth .mat-mdc-form-field-subscript-wrapper' }, current: { css: 'rdio-display .auth .rdio-form-error' },
    states: ['unlock'], viewports: ['wide'],
  },
  {
    id: 27, name: 'control row',
    legacy: { css: '.rdio-control .row' }, current: { css: 'rdio-controls .row' },
    states: ['idle', 'controls'], viewports: ['wide', 'narrow'],
  },
  {
    id: 28, name: 'control button',
    legacy: { css: '.rdio-control .rdio-button' }, current: { css: 'rdio-controls .rdio-button' },
    states: ['idle', 'controls'], viewports: ['wide', 'narrow'],
  },
  {
    id: 29, name: 'control spacer',
    legacy: { css: '.rdio-control .spacer' }, current: { css: 'rdio-controls .spacer' },
    states: ['idle'], viewports: ['wide'],
  },
  {
    id: 30, name: 'help control',
    legacy: { css: '.rdio-help button' }, current: { css: 'rdio-app .rdio-help button' },
    states: ['idle'], viewports: ['wide'],
  },
  {
    id: 31, name: 'select panel',
    legacy: { css: 'rdio-scanner-select' }, current: { css: 'rdio-select-panel' },
    states: ['select-open'], viewports: ['wide', 'narrow'],
  },
  {
    id: 32, name: 'fieldset',
    legacy: { css: 'rdio-scanner-select .fieldset' }, current: { css: 'rdio-select-panel .fieldset' },
    states: ['select-open'], viewports: ['wide', 'narrow'],
  },
  {
    id: 33, name: 'legend',
    legacy: { css: 'rdio-scanner-select .fieldset legend' }, current: { css: 'rdio-select-panel .fieldset legend' },
    states: ['select-open'], viewports: ['wide'],
  },
  {
    id: 34, name: 'select button',
    legacy: { css: 'rdio-scanner-select .rdio-button' }, current: { css: 'rdio-select-panel .rdio-button' },
    states: ['select-open'], viewports: ['wide', 'medium', 'narrow'],
  },
  {
    id: 35, name: 'select mini button',
    legacy: { css: 'rdio-scanner-select .rdio-button-mini' }, current: { css: 'rdio-select-panel .rdio-button-mini' },
    states: ['select-open'], viewports: ['wide', 'medium', 'narrow'],
  },
  {
    id: 36, name: 'results card',
    legacy: { css: 'mat-card.rdio-list' }, current: { css: 'rdio-search-panel .rdio-list' },
    states: ['search-open-24h'], viewports: ['wide', 'narrow'],
  },
  {
    id: 37, name: 'table header row',
    legacy: { css: 'mat-header-row' }, current: { css: 'rdio-search-panel thead tr' },
    states: ['search-open-24h', 'search-open-12h'], viewports: ['wide', 'narrow'],
  },
  {
    id: 38, name: 'table header cell',
    legacy: { css: 'mat-header-cell' }, current: { css: 'rdio-search-panel thead th' },
    states: ['search-open-24h', 'search-open-12h'], viewports: ['wide', 'narrow'],
  },
  {
    id: 39, name: 'table body row',
    legacy: { css: 'mat-row' }, current: { css: 'rdio-search-panel tbody tr' },
    states: ['search-open-24h', 'search-open-12h'], viewports: ['wide', 'narrow'],
  },
  {
    id: 40, name: 'table cell',
    legacy: { css: 'mat-cell' }, current: { css: 'rdio-search-panel tbody td' },
    states: ['search-open-24h', 'search-open-12h'], viewports: ['wide', 'narrow'],
  },
  {
    id: 41, name: 'row control',
    legacy: { css: 'mat-cell button.mat-mdc-icon-button' }, current: { css: 'rdio-search-panel .row-action' },
    states: ['search-open-24h'], viewports: ['wide'],
  },
  {
    id: 42, name: 'row control icon',
    legacy: { css: 'mat-cell mat-icon' }, current: { css: 'rdio-search-panel .row-action .material-icons' },
    states: ['search-open-24h'], viewports: ['wide'],
  },
  {
    id: 43, name: 'progress bar',
    legacy: { css: 'mat-progress-bar' }, current: { css: 'rdio-search-panel .progress-bar' },
    states: ['search-open-24h', 'search-pending'], viewports: ['wide'],
  },
  {
    id: 44, name: 'paginator row',
    legacy: { css: '.rdio-list .paginator' }, current: { css: 'rdio-search-panel .paginator' },
    states: ['search-open-24h'], viewports: ['wide', 'narrow'],
  },
  {
    id: 45, name: 'download toggle',
    legacy: { css: '.mat-mdc-slide-toggle' }, current: { css: 'rdio-search-panel rdio-toggle' },
    states: ['search-open-24h'], viewports: ['wide'],
  },
  {
    id: 46, name: 'download toggle icon',
    legacy: { css: '.mat-mdc-slide-toggle mat-icon' }, current: { css: 'rdio-search-panel rdio-toggle .material-icons' },
    states: ['search-open-24h'], viewports: ['wide'],
  },
  {
    id: 47, name: 'paginator',
    legacy: { css: 'mat-paginator' }, current: { css: 'rdio-search-panel rdio-paginator' },
    states: ['search-open-24h'], viewports: ['wide', 'narrow'],
  },
  {
    id: 48, name: 'paginator range label',
    legacy: { css: '.mat-mdc-paginator-range-label' }, current: { css: 'rdio-search-panel rdio-paginator .rdio-paginator-label' },
    states: ['search-open-24h'], viewports: ['wide'],
  },
  {
    id: 49, name: 'paginator control',
    legacy: { css: 'mat-paginator button.mat-mdc-paginator-navigation-first' }, current: { css: 'rdio-search-panel rdio-paginator button' },
    states: ['search-open-24h'], viewports: ['wide'],
  },
  {
    id: 50, name: 'filters card',
    legacy: { css: 'mat-card.rdio-filters' }, current: { css: 'rdio-search-panel .rdio-filters' },
    states: ['search-open-24h'], viewports: ['wide', 'narrow'],
  },
  {
    id: 51, name: 'filter field',
    legacy: { css: 'mat-card.rdio-filters .mat-mdc-form-field' }, current: { css: 'rdio-search-panel .rdio-filters rdio-form-field' },
    states: ['search-open-24h'], viewports: ['wide', 'medium', 'narrow'],
  },
  {
    id: 52, name: 'filter field box',
    legacy: { css: 'mat-card.rdio-filters .mat-mdc-form-field-flex' }, current: { css: 'rdio-search-panel .rdio-filters .rdio-field-flex' },
    states: ['search-open-24h'], viewports: ['wide'],
  },
  {
    id: 53, name: 'filter field subscript',
    legacy: { css: 'mat-card.rdio-filters .mat-mdc-form-field-subscript-wrapper' }, current: { css: 'rdio-search-panel .rdio-filters .rdio-form-error' },
    states: ['search-open-24h'], viewports: ['wide'],
  },
  {
    id: 54, name: 'reset control',
    legacy: { css: '.reset button.mat-mdc-raised-button' }, current: { css: 'rdio-search-panel .reset button' },
    states: ['search-open-24h'], viewports: ['wide'],
  },
  {
    id: 55, name: 'snackbar',
    legacy: { css: '.mat-mdc-snack-bar-container' }, current: { css: 'rdio-snackbar' },
    states: ['snackbar-help', 'snackbar-promo'], viewports: ['wide', 'narrow'],
  },
  {
    id: 56, name: 'snackbar surface',
    legacy: { css: '.mdc-snackbar__surface' }, current: { css: 'rdio-snackbar .rdio-snackbar-surface' },
    states: ['snackbar-help'], viewports: ['wide'],
  },
  {
    id: 60, name: 'snackbar label',
    legacy: { css: '.mat-mdc-snack-bar-label' }, current: { css: 'rdio-snackbar .rdio-snackbar-label' },
    states: ['snackbar-help'], viewports: ['wide'],
  },
  {
    // The beveled buttons are measured with the pointer over them and with the control disabled, because the
    // baseline has no hover rule and no disabled rule at all and the shared theme has both.
    id: 61, name: 'control button, hovered',
    legacy: { css: '.rdio-control .rdio-button' }, current: { css: 'rdio-controls .rdio-button' },
    states: ['controls'], viewports: ['wide'], force: ['hover'],
  },
  {
    id: 62, name: 'control button, disabled',
    legacy: { css: '.rdio-control .rdio-button' }, current: { css: 'rdio-controls .rdio-button' },
    states: ['controls'], viewports: ['wide'], force: ['disabled'],
  },
  {
    id: 63, name: 'select mini button, hovered',
    legacy: { css: 'rdio-scanner-select .rdio-button-mini' }, current: { css: 'rdio-select-panel .rdio-button-mini' },
    states: ['select-open'], viewports: ['wide'], force: ['hover'],
  },
  {
    // The baseline's own stylesheet erases the focus ring of its buttons. The current client keeps one on purpose,
    // which is the accessibility deviation FEAT-002 REQ-014 records; this pair is the proof that it is still there.
    // The outline is read here and nowhere else: on an element that is not focused it is a colour and a width
    // behind a style of `none`, so measuring it on every surface would compare three values that paint nothing.
    id: 64, name: 'control button, focused',
    legacy: { css: '.rdio-control .rdio-button' }, current: { css: 'rdio-controls .rdio-button' },
    states: ['controls'], viewports: ['wide'],
    force: ['focus-visible'], properties: ['outline-color', 'outline-style', 'outline-width'],
  },
  {
    id: 57, name: 'snackbar countdown',
    legacy: { css: '.countdown' }, current: { css: 'rdio-snackbar .countdown' },
    states: ['snackbar-help', 'snackbar-promo'], viewports: ['wide'],
  },
  {
    id: 58, name: 'document typography',
    legacy: { css: 'body' }, current: { css: 'body' },
    states: ['idle'], viewports: ['wide'],
  },
  {
    // The one column whose width the clock format changes: 53px in 24-hour time
    // and 78px in 12-hour time.
    id: 59, name: 'table time cell',
    legacy: { css: 'mat-row mat-cell:nth-child(3)' }, current: { css: 'rdio-search-panel tbody td:nth-child(3)' },
    states: ['search-open-24h', 'search-open-12h'], viewports: ['wide'],
  },
];

// The controls the harness presses, named once per side. The legacy buttons are found by the words printed on
// them; the current buttons carry a data-action.
export const controls = {
  livefeed: { legacy: { css: '.rdio-control .rdio-button', text: 'LIVE' }, current: { css: 'rdio-controls button[data-action="livefeed"]' } },
  holdTalkgroup: { legacy: { css: '.rdio-control .rdio-button', text: 'HOLD\\s*TG' }, current: { css: 'rdio-controls button[data-action="holdTalkgroup"]' } },
  pause: { legacy: { css: '.rdio-control .rdio-button', text: 'PAUSE' }, current: { css: 'rdio-controls button[data-action="pause"]' } },
  search: { legacy: { css: '.rdio-control .rdio-button', text: 'SEARCH' }, current: { css: 'rdio-controls button[data-action="search"]' } },
  select: { legacy: { css: '.rdio-control .rdio-button', text: 'SELECT' }, current: { css: 'rdio-controls button[data-action="select"]' } },
  help: { legacy: { css: '.rdio-help button' }, current: { css: 'rdio-app .rdio-help button' } },
  unlockInput: { legacy: { css: '.rdio-display .auth input' }, current: { css: 'rdio-display .auth input' } },
  // The date filter is the one field both applications treat as a search request the same way: a change event on
  // it asks the server for a page of results.
  dateFilter: { legacy: { css: '.rdio-filters input[type="datetime-local"]' }, current: { css: 'rdio-search-panel .rdio-filters input[name="date"]' } },
};

// ---------------------------------------------------------------------------
// The administration application, measured against itself
// ---------------------------------------------------------------------------

// REQ-010 says this work must not change what the administration renders. The probe proves it by measuring the
// administration built before the work against the administration built after it, through the same machinery that
// compares the two scanner pages. Here `legacy` is the build from before the work and `current` the build from
// after it, and every row is expected to match exactly: a difference there is not a parity gap, it is the
// regression the requirement forbids.

export const adminStates = ['login', 'todos', 'options', 'logs', 'snackbar'];

export const adminPairs = [
  {
    id: 1, name: 'login card',
    legacy: { css: 'rdio-admin-login' }, current: { css: 'rdio-admin-login' },
    states: ['login'], viewports: ['wide'],
  },
  {
    id: 2, name: 'login field',
    legacy: { css: 'rdio-admin-login rdio-form-field' }, current: { css: 'rdio-admin-login rdio-form-field' },
    states: ['login'], viewports: ['wide'],
  },
  {
    id: 3, name: 'login button',
    legacy: { css: 'rdio-admin-login button' }, current: { css: 'rdio-admin-login button' },
    states: ['login'], viewports: ['wide'],
  },
  {
    id: 4, name: 'header',
    legacy: { css: '.rdio-admin-header' }, current: { css: '.rdio-admin-header' },
    states: ['todos'], viewports: ['wide'],
  },
  {
    id: 5, name: 'section nav',
    legacy: { css: '.rdio-admin-nav' }, current: { css: '.rdio-admin-nav' },
    states: ['todos'], viewports: ['wide'],
  },
  {
    id: 6, name: 'section nav item',
    legacy: { css: '.rdio-admin-nav button' }, current: { css: '.rdio-admin-nav button' },
    states: ['todos'], viewports: ['wide'],
  },
  {
    id: 7, name: 'section content',
    legacy: { css: '.rdio-admin-content' }, current: { css: '.rdio-admin-content' },
    states: ['todos'], viewports: ['wide'],
  },
  {
    id: 8, name: 'section bar',
    legacy: { css: 'rdio-admin-options rdio-admin-section-bar' },
    current: { css: 'rdio-admin-options rdio-admin-section-bar' },
    states: ['options'], viewports: ['wide'],
  },
  {
    id: 9, name: 'option row',
    legacy: { css: 'rdio-admin-options .row' }, current: { css: 'rdio-admin-options .row' },
    states: ['options'], viewports: ['wide'],
  },
  {
    // The toggle is one of the two primitives this feature gave an optional attribute. The administration sets
    // neither and must render the box it always rendered.
    id: 10, name: 'option toggle',
    legacy: { css: 'rdio-admin-options rdio-toggle' }, current: { css: 'rdio-admin-options rdio-toggle' },
    states: ['options'], viewports: ['wide'],
  },
  {
    id: 11, name: 'option field',
    legacy: { css: 'rdio-admin-options rdio-form-field' }, current: { css: 'rdio-admin-options rdio-form-field' },
    states: ['options'], viewports: ['wide'],
  },
  {
    id: 12, name: 'logs card',
    legacy: { css: 'rdio-admin-logs .rdio-admin-card' }, current: { css: 'rdio-admin-logs .rdio-admin-card' },
    states: ['logs'], viewports: ['wide'],
  },
  {
    id: 13, name: 'logs table',
    legacy: { css: 'rdio-admin-logs table' }, current: { css: 'rdio-admin-logs table' },
    states: ['logs'], viewports: ['wide'],
  },
  {
    id: 14, name: 'logs header cell',
    legacy: { css: 'rdio-admin-logs th' }, current: { css: 'rdio-admin-logs th' },
    states: ['logs'], viewports: ['wide'],
  },
  {
    // The paginator is the other primitive this feature gave an optional attribute.
    id: 15, name: 'logs paginator',
    legacy: { css: 'rdio-admin-logs rdio-paginator' }, current: { css: 'rdio-admin-logs rdio-paginator' },
    states: ['logs'], viewports: ['wide'],
  },
  {
    id: 16, name: 'logs filter field',
    legacy: { css: 'rdio-admin-logs rdio-form-field' }, current: { css: 'rdio-admin-logs rdio-form-field' },
    states: ['logs'], viewports: ['wide'],
  },
  {
    // The snack bar is the primitive this feature rebuilt. The administration creates the host itself, asks for
    // nothing of the baseline's shape, and must get the box it always had.
    id: 17, name: 'snackbar',
    legacy: { css: 'rdio-snackbar' }, current: { css: 'rdio-snackbar' },
    states: ['snackbar'], viewports: ['wide'],
  },
];
