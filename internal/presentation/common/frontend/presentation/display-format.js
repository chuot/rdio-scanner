// Display formatting helpers for the listener's call views.

// The types whose talkgroup identifiers are eleven-bit group ids read as agency, fleet
// and subfleet: EDACS, and the Provoice trunking carried over the same scheme. Every
// other type's identifier is a decimal number looked up in a fleet map.
const AFS_TYPES = ['edacs', 'provoice'];

// The largest identifier those eleven bits carry: agency 00-15, fleet 00-15, subfleet 0-7.
const MAX_AFS_REFERENCE = 2047;

export function isAfsSystem(call) {
  return AFS_TYPES.includes(call.systemData?.type) || AFS_TYPES.includes(call.talkgroupData?.type);
}

// The agency-fleet-subfleet form of an identifier. An identifier the eleven bits cannot
// carry is not a talkgroup in this scheme, and folding it into one would name a talkgroup
// the system does not have, so it keeps the decimal the system names it in.
export function formatAfs(n) {
  if (!Number.isInteger(n) || n < 0 || n > MAX_AFS_REFERENCE) return `${n}`;

  return `${(n >> 7 & 15).toString().padStart(2, '0')}-${(n >> 3 & 15).toString().padStart(2, '0')}${n & 7}`;
}

// The text a surface names a talkgroup by: the label an operator wrote, else the form the
// type numbers it in, else the decimal identifier. An empty label, or the decimal text of
// the very reference it stands for, is the placeholder auto-populate gives a talkgroup
// nobody has described yet — the identifier in disguise rather than a name. The same rule
// the domain's own placeholder check states.
export function talkgroupText(talkgroupData, reference, systemData) {
  const label = talkgroupData?.label;

  if (label && `${label}` !== `${reference}`) return label;

  if (isAfsSystem({ talkgroupData, systemData })) return formatAfs(reference);

  return `${reference}`;
}

export function formatFrequency(frequency) {
  return typeof frequency === 'number'
    ? frequency
        .toString()
        .padStart(9, '0')
        .replace(/(\d)(?=(\d{3})+$)/g, '$1 ')
        .concat(' Hz')
    : '';
}

// The clock format is the browser locale's hour cycle: a 12-hour locale
// gets the AM/PM suffix, a 24-hour locale gets 24-hour time. The locale
// argument exists for tests with fixed locales.
export function formatTime(date, locale) {
  if (!(date instanceof Date)) return '';

  const hourCycle = new Intl.DateTimeFormat(locale, { hour: 'numeric' }).resolvedOptions().hourCycle;

  let hours = date.getHours();
  const minutes = date.getMinutes().toString().padStart(2, '0');

  if (hourCycle === 'h12' || hourCycle === 'h11') {
    const suffix = hours >= 12 ? ' PM' : ' AM';
    hours = hours % 12 || 12;

    return `${hours.toString().padStart(2, '0')}:${minutes}${suffix}`;
  }

  return `${hours.toString().padStart(2, '0')}:${minutes}`;
}

export function formatMonthDay(date) {
  if (!(date instanceof Date)) return '';

  return `${(date.getMonth() + 1).toString().padStart(2, '0')}/${date.getDate().toString().padStart(2, '0')}`;
}

// The search table's duration column: mm:ss, and the never-measured zero
// renders blank.
export function formatDuration(seconds) {
  if (typeof seconds !== 'number' || !Number.isFinite(seconds) || seconds <= 0) return '';

  const minutes = Math.floor(seconds / 60);
  const rest = Math.floor(seconds % 60);

  return `${minutes.toString().padStart(2, '0')}:${rest.toString().padStart(2, '0')}`;
}

// Progress = call start + elapsed seconds (legacy callProgress).
export function progressDate(callDateTime, elapsedSeconds) {
  const progress = new Date(callDateTime);
  progress.setSeconds(progress.getSeconds() + elapsedSeconds);
  return progress;
}

// Date column shows only when the call is more than a day old.
export function shouldShowDate(callDateTime, progress) {
  return Date.now() - progress.getTime() >= 86400000;
}

// Frequency/error/spike row: last source entry whose pos ≤ elapsed.
export function activeFrequency(call, elapsed) {
  if (Array.isArray(call.frequencies) && call.frequencies.length) {
    const entry = call.frequencies.reduce((p, v) => ((v.pos || 0) <= elapsed ? v : p), {});

    return {
      error: typeof entry.errorCount === 'number' ? `${entry.errorCount}` : '',
      frequency: formatFrequency(typeof entry.freq === 'number' ? entry.freq : call.frequency),
      spike: typeof entry.spikeCount === 'number' ? `${entry.spikeCount}` : '',
    };
  }

  return {
    error: '',
    frequency: typeof call.frequency === 'number' ? formatFrequency(call.frequency) : '',
    spike: '',
  };
}

// Unit label resolution (legacy sources.src → system units ranges).
export function resolveUnit(call, elapsed) {  if (Array.isArray(call.sources) && call.sources.length) {
    const source = call.sources.reduce((p, v) => ((v.pos || 0) <= elapsed ? v : p), {});

    if (typeof source.src === 'number') {
      if (Array.isArray(call.systemData?.units)) {
        return call.systemData.units.find((u) => {
          if (typeof u.unitFrom === 'number' && typeof u.unitTo === 'number') {
            if (u.unitFrom <= source.src && u.unitTo >= source.src) return true;
          }

          return u.id === source.src;
        })?.label ?? `${source.src}`;
      }

      return `${source.src}`;
    }

    return undefined;
  }

  return call.systemData?.units?.find((u) => u.id === call.source)?.label ?? `${call.source ?? ''}`;
}

// The bound a date field is given, written the way the field reads it.
export function toLocalInputValue(value) {
  if (!value) return '';

  const date = new Date(value);

  if (Number.isNaN(date.getTime()) || date.getFullYear() <= 1) return '';

  const pad = (n) => String(n).padStart(2, '0');

  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}
