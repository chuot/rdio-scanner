// Dirwatch row validation: pure port of the legacy admin.service.ts
// dirwatch validators — required / duplicate / network directory, extension,
// mask tokens, delay, frequency, and the conditional system / talkgroup rules.
// No DOM, no store: the section element owns rendering, this owns the rules.

export const DIRWATCH_TYPES = ['default', 'dsdplus', 'sdr-trunk', 'trunk-recorder'];

export const DIRWATCH_MASK_TOKENS = [
  '#DATE',
  '#GROUP',
  '#HZ',
  '#KHZ',
  '#MHZ',
  '#SITE',
  '#SITELBL',
  '#SYS',
  '#SYSLBL',
  '#TAG',
  '#TG',
  '#TGAFS',
  '#TGHZ',
  '#TGKHZ',
  '#TGLBL',
  '#TGMHZ',
  '#TIME',
  '#UNIT',
  '#UNITLBL',
  '#ZTIME',
];

// Legacy /#SYS/ and /#TG/ tests are substring matches: #SYSLBL satisfies #SYS.
export function maskHasToken(mask, token) {
  return String(mask ?? '').includes(token);
}

// Legacy validateExtension: empty values pass, otherwise alphanumeric only.
export function validExtension(value) {
  if (typeof value !== 'string' || !value.length) return true;

  return /^[0-9a-zA-Z]+$/.test(value);
}

// Legacy validateDirectory plus the separate required control: empty, network
// (backslash) and duplicated directories are invalid. allRows is the full row
// list including the row being checked (legacy counts occurrences > 1).
export function validDirectory(value, allRows) {
  if (typeof value !== 'string' || !value.length) return false;

  if (value.startsWith('\\')) return false;

  return (allRows ?? []).filter((row) => row.directory === value).length <= 1;
}

// The server clamps every stored delay to one day: 1440, below the floor this section's rule
// accepts — no delay value passes both sides. The legacy admin form loaded the stored delay as
// Math.max(2000, delay) at form creation, so the row read valid and the form was not dirty (an
// initial control value is not a change). The row loader is that clamp: applied to the server's
// rows as they load, baseline and working copy together, so a saved row means the same thing on
// both sides of its save. The rule itself is untouched — a value the operator types below 2000
// still reports.
export function normalizeDirwatchRow(row) {
  return typeof row.delay === 'number' ? { ...row, delay: Math.max(2000, row.delay) } : row;
}

// Field → error-message map (empty object when the row is valid). Messages are
// the exact legacy mat-error texts.
export function validateDirwatchRow(row, allRows = [row]) {
  const errors = {};
  const rows = allRows ?? [row];

  if (!row.directory) errors.directory = 'Directory is required';
  else if (row.directory.startsWith('\\')) errors.directory = 'Incompatible network folder';
  else if (rows.filter((other) => other.directory === row.directory).length > 1) errors.directory = 'Directory is already defined';

  if (!validExtension(row.extension)) errors.extension = 'Invalid extension';

  // Legacy validateMask only inspects string values: an absent mask passes,
  // an empty string has no token and fails.
  if (typeof row.mask === 'string' && !DIRWATCH_MASK_TOKENS.some((token) => maskHasToken(row.mask, token))) errors.mask = 'Invalid mask';

  if (typeof row.frequency === 'number' && row.frequency < 1) errors.frequency = 'Invalid frequency';

  if (typeof row.delay === 'number' && row.delay < 2000) errors.delay = 'Delay value cannot be less than 2000 milliseconds.';

  // Legacy validateDirwatchSystemId / validateDirwatchTalkgroupId: exempt for
  // the tag-parsing types or when the mask carries the id token; a null (or
  // select-empty) value is then required. An absent (undefined) value keeps
  // the legacy pass-through.
  const idsExempt = ['dsdplus', 'sdr-trunk', 'trunk-recorder'].includes(row.type);

  if (!idsExempt && (row.systemId === null || row.systemId === '') && !maskHasToken(row.mask, '#SYS')) errors.systemId = 'System is required';

  if (!idsExempt && (row.talkgroupId === null || row.talkgroupId === '') && !maskHasToken(row.mask, '#TG')) errors.talkgroupId = 'Talkgroup is required';

  return errors;
}
