// Audio engine adapter (legacy rdio-scanner.service.ts bootstrapAudio/play/
// download/playOscillatorSequence).
// Dual contexts: `playback` for call audio, `interactive` for keypad/alert
// oscillators. Contexts are created lazily on the first user gesture and kept
// resumed via an onstatechange re-arm loop.

import { scheduleOscillators } from '../../../common/frontend/domain/oscillator.js';

export const PLAYBACK_GAP_MS = 1000;
export const PROGRESS_TICK_MS = 500;

const BOOTSTRAP_EVENTS = ['keydown', 'mousedown', 'touchstart'];

export function createAudioEngine({ AudioContextImpl = globalThis.AudioContext, documentImpl = globalThis.document }) {
  let playbackContext = null;
  let interactiveContext = null;

  function keepResumed(context) {
    const resume = () => {
      if (context.state === 'suspended') {
        context.resume().then(() => resume());
      }
    };

    context.resume().then(() => {
      context.onstatechange = () => resume();
    });
  }

  function bootstrap() {
    const start = async () => {
      if (!playbackContext) {
        playbackContext = new AudioContextImpl({ latencyHint: 'playback' });
      }

      if (!interactiveContext) {
        interactiveContext = new AudioContextImpl({ latencyHint: 'interactive' });
      }

      keepResumed(playbackContext);
      keepResumed(interactiveContext);

      if (playbackContext && interactiveContext) {
        for (const event of BOOTSTRAP_EVENTS) documentImpl.body.removeEventListener(event, start);
      }
    };

    for (const event of BOOTSTRAP_EVENTS) documentImpl.body.addEventListener(event, start);
  }

  function playCall(call, { onEnded } = {}) {
    if (!playbackContext || !call?.audio) return undefined;

    const { data } = call.audio;

    const arrayBuffer = new ArrayBuffer(data.length);
    const view = new Uint8Array(arrayBuffer);
    for (let i = 0; i < data.length; i++) view[i] = data[i];

    let source = null;

    playbackContext.decodeAudioData(
      arrayBuffer,
      (buffer) => {
        source = playbackContext.createBufferSource();
        source.buffer = buffer;
        source.connect(playbackContext.destination);
        source.onended = () => onEnded?.();
        source.start();
      },
      () => onEnded?.(),
    );

    return { stop: () => source?.stop() };
  }

  function playOscillators(sequence) {
    return scheduleOscillators(interactiveContext, sequence);
  }

  function download(call) {
    if (!call?.audio) return;

    const file = call.audio.data.reduce((str, val) => str + String.fromCharCode(val), '');
    const fileName = call.audioName || 'unknown.dat';
    const fileType = call.audioType || 'audio/*';
    const fileUri = `data:${fileType};base64,${btoa(file)}`;

    const el = documentImpl.createElement('a');

    el.style.display = 'none';
    el.setAttribute('href', fileUri);
    el.setAttribute('download', fileName);

    documentImpl.body.appendChild(el);
    el.click();
    documentImpl.body.removeChild(el);
  }

  return {
    bootstrap,
    playCall,
    playOscillators,
    download,
    get playbackContext() {
      return playbackContext;
    },
    get interactiveContext() {
      return interactiveContext;
    },
  };
}
