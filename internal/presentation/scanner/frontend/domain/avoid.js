// AVOID button cycle state machine: active, avoided indefinitely,
// 30M, 60M, 120M, back to active.

import { cloneMap } from './livefeed-map.js';

export function nextAvoidStep({ avoided, minutes }) {
  if (!avoided) return { status: false };

  switch (minutes) {
    case 30:
      return { minutes: 60, status: false };
    case 60:
      return { minutes: 120, status: false };
    case 120:
      return { status: true };
    default:
      return { minutes: 30, status: false };
  }
}

// timed-avoid expiry: re-activates and clears.
export function expireAvoid(map, systemRef, talkgroupRef) {
  const next = cloneMap(map);
  const lfm = next?.[systemRef]?.[talkgroupRef];
  if (lfm) {
    lfm.active = true;
    delete lfm.minutes;
  }
  return next;
}
