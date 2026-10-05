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

export function formatTime(date, format12h) {
  if (!(date instanceof Date)) return '';

  let hours = date.getHours();
  const minutes = date.getMinutes().toString().padStart(2, '0');

  if (format12h) {
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
export function resolveUnit(call, elapsed) {
  if (Array.isArray(call.sources) && call.sources.length) {
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
