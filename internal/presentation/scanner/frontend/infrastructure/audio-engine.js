// Audio engine adapter: dual contexts, playback for call audio and
// interactive for keypad and alert oscillators. contexts are created
// lazily on the first user gesture and kept resumed by a statechange
// re-arm loop.

import { scheduleOscillators } from '../../../common/frontend/domain/oscillator.js';

export const PLAYBACK_GAP_MS = 1000;
export const PROGRESS_TICK_MS = 500;

const BOOTSTRAP_EVENTS = ['keydown', 'mousedown', 'touchstart'];

export function createAudioEngine({ AudioContextImpl = globalThis.AudioContext, documentImpl = globalThis.document }) {
  let playbackContext = null;
  let interactiveContext = null;
  let playbackPaused = false;
  let sourceStartedAt = null;

  function keepResumed(context, isPaused = () => false) {
    const resume = () => {
      // an intentional pause outranks the re-arm loop: the statechange a
      // suspension itself fires would resume the paused context right back.
      if (isPaused()) return;

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

      keepResumed(playbackContext, () => playbackPaused);
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
        source.onended = () => {
          sourceStartedAt = null;
          onEnded?.();
        };

        // the elapsed reading is anchored here, where the recording enters the
        // graph, the same point the reference recorded its start time.
        sourceStartedAt = playbackContext.currentTime;
        source.start();
      },
      () => onEnded?.(),
    );

    return {
      stop: () => {
        if (!source) return;

        // browsers fire ended on a manual stop too; disarming it first keeps a
        // silenced recording from triggering the skip it was wired to.
        source.onended = null;
        source.stop();
        source.disconnect();
        sourceStartedAt = null;
      },
    };
  }

  function playOscillators(sequence) {
    return scheduleOscillators(interactiveContext, sequence);
  }

  // pause silences the call audio by suspending the playback context
  // mid-call, and the recording resumes where it stopped because a
  // suspended context freezes its whole graph. the interactive context is
  // never suspended: keypad and alert tones keep playing.
  function suspend() {
    if (!playbackContext) return;

    playbackPaused = true;
    playbackContext.suspend();
  }

  function resume() {
    if (!playbackContext) return;

    playbackPaused = false;
    playbackContext.resume();
  }

  // seconds since the recording in flight entered the graph, or null
  // when nothing is playing. a suspended context freezes its currentTime,
  // so this reading freezes with the audio across a pause.
  function playbackElapsed() {
    if (!playbackContext || sourceStartedAt === null) return null;

    return playbackContext.currentTime - sourceStartedAt;
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
    suspend,
    resume,
    playbackElapsed,
    download,
    get playbackContext() {
      return playbackContext;
    },
    get interactiveContext() {
      return interactiveContext;
    },
  };
}
