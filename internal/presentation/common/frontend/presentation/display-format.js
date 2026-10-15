// Display formatting helpers ported from legacy main.component.ts.

export function formatAfs(n) {
  return `${(n >> 7 & 15).toString().padStart(2, '0')}-${(n >> 3 & 15).toString().padStart(2, '0')}${n & 7}`;
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

export function isAfsSystem(call) {
  return call.systemData?.type === 'provoice' || call.talkgroupData?.type === 'provoice';
}

// The clock format is the browser locale's hour cycle: a
// 12-hour locale gets the AM/PM suffix, a 24-hour locale gets 24-hour time. No
// administrator option controls it any more. The locale argument exists for
// tests with fixed locales; the browser default is used when it is absent.
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

// The search table's duration column: mm:ss from the seconds a search row names.
// Zero is the never-measured zero the column carries, and it renders blank.
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
//
// A datetime-local field reads its min and max as a wall-clock minute — `YYYY-MM-DDTHH:MM`, in the
// time of the machine the page runs on, because the value the operator types into the field is read
// as that same wall-clock time. An instant in any other shape is a bound the field ignores, so a
// bound written from a Date's own string form is a bound that looks set and constrains nothing.
//
// A search that answered with no calls names the zero time for the two ends of its match. That is
// not a date a person can be shown, and a field bounded to it offers no date at all, so no bound is
// invented for it.
export function toLocalInputValue(value) {
  if (!value) return '';

  const date = new Date(value);

  if (Number.isNaN(date.getTime()) || date.getFullYear() <= 1) return '';

  const pad = (n) => String(n).padStart(2, '0');

  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}
