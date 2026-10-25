// admin alert audition: a private playback AudioContext; the gain lives in
// domain/oscillator; the sequence resolves on the last oscillator's onended.

import { scheduleOscillators } from '../../../../common/frontend/domain/oscillator.js';

let audioContext = null;

export function playAlert(alerts, name) {
  const sequence = name ? alerts?.[name] : null;

  if (!sequence?.length) return Promise.resolve();

  audioContext ??= new globalThis.AudioContext({ latencyHint: 'playback' });

  return scheduleOscillators(audioContext, sequence);
}
