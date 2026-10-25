// a system's blacklist: comma-separated talkgroup refs stored as one text field.
// an entry names the talkgroupRef: ids are reassigned, id entries refuse nothing.

// The references the list names, in the order it names them.
export function blacklistsEntries(text) {
  return String(text ?? '')
    .split(',')
    .map((entry) => entry.trim())
    .filter((entry) => entry.length > 0);
}

export function appendBlacklistsEntry(text, reference) {
  const entries = blacklistsEntries(text);

  const entry = String(reference);

  return (entries.includes(entry) ? entries : [...entries, entry]).join(',');
}
