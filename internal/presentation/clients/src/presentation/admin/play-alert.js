// Admin-side alert audition (legacy admin.service.ts playAlert): a private
// playback AudioContext at gain 0.1 (the gain lives in domain/oscillator's
// scheduleOscillators), sequences resolved on the last oscillator's onended.

import { scheduleOscillators } from '../../../shared/domain/oscillator.js';

let audioContext = null;

export function playAlert(alerts, name) {
  const sequence = name ? alerts?.[name] : null;

  if (!sequence?.length) return Promise.resolve();

  audioContext ??= new globalThis.AudioContext({ latencyHint: 'playback' });

  return scheduleOscillators(audioContext, sequence);
}
