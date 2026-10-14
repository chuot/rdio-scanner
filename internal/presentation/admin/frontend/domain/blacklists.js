// The system's blacklist as the page edits it: a comma-separated list of talkgroup references the
// server refuses calls on. The list is stored as one text field on the system row, and the page
// edits that field: the Blacklist action adds the talkgroup it is standing on, and the operator
// takes a reference out of the field's own text.
//
// An entry names the talkgroup's reference, the number a recording names its channel by and the
// number the server compares it against. The identifier the store gave the row is a storage detail
// the server reassigns, and an entry written in it refuses nothing.

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
