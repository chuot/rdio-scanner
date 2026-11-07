// dirwatch row rules: required, duplicate and network directory, extension, mask
// tokens, delay, frequency, conditional system and talkgroup rules. No DOM.

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

// token matching is a substring check: #SYSLBL satisfies #SYS.
export function maskHasToken(mask, token) {
  return String(mask ?? '').includes(token);
}

// empty passes; a non-empty extension is alphanumeric only.
export function validExtension(value) {
  if (typeof value !== 'string' || !value.length) return true;

  return /^[0-9a-zA-Z]+$/.test(value);
}

// empty, network (backslash), and duplicated directories are invalid; allRows
// counts the row itself, so a duplicate counts as more than one occurrence.
export function validDirectory(value, allRows) {
  if (typeof value !== 'string' || !value.length) return false;

  if (value.startsWith('\\')) return false;

  return (allRows ?? []).filter((row) => row.directory === value).length <= 1;
}

// the server clamps every stored delay to 1440, below this section's 2000 floor:
// no value passes both rules, so the loader applies the 2000 floor on load and a
// saved row reads valid and unchanged.
export function normalizeDirwatchRow(row) {
  return typeof row.delay === 'number' ? { ...row, delay: Math.max(2000, row.delay) } : row;
}

// field to message map, empty when the row is valid; texts are fixed and shown.
export function validateDirwatchRow(row, allRows = [row]) {
  const errors = {};
  const rows = allRows ?? [row];

  if (!row.directory) errors.directory = 'Directory is required';
  else if (row.directory.startsWith('\\')) errors.directory = 'Incompatible network folder';
  else if (rows.filter((other) => other.directory === row.directory).length > 1) errors.directory = 'Directory is already defined';

  if (!validExtension(row.extension)) errors.extension = 'Invalid extension';

  // only string masks are checked: an absent mask passes; an empty string has no
  // tokens and fails the token rule.
  if (typeof row.mask === 'string' && !DIRWATCH_MASK_TOKENS.some((token) => maskHasToken(row.mask, token))) errors.mask = 'Invalid mask';

  if (typeof row.frequency === 'number' && row.frequency < 1) errors.frequency = 'Invalid frequency';

  if (typeof row.delay === 'number' && row.delay < 2000) errors.delay = 'Delay value cannot be less than 2000 milliseconds.';

  // exempt for the tag-parsing types or an id token in the mask; otherwise a null
  // is required; an absent value passes through.
  const idsExempt = ['dsdplus', 'sdr-trunk', 'trunk-recorder'].includes(row.type);

  if (!idsExempt && (row.systemId === null || row.systemId === '') && !maskHasToken(row.mask, '#SYS')) errors.systemId = 'System is required';

  if (!idsExempt && (row.talkgroupId === null || row.talkgroupId === '') && !maskHasToken(row.mask, '#TG')) errors.talkgroupId = 'Talkgroup is required';

  return errors;
}
