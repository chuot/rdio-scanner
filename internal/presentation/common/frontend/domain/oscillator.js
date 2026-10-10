// Keypad beep / alert oscillator sequence scheduling
// (legacy rdio-scanner.service.ts playOscillatorSequence).
// Deviation: an empty sequence resolves immediately; the legacy promise never
// resolved for an empty array because no oscillator ever fired onended.

export function scheduleOscillators(context, sequence) {
  return new Promise((resolve) => {
    if (!context || !Array.isArray(sequence) || sequence.length === 0) {
      resolve();
      return;
    }

    const gain = context.createGain();

    gain.gain.value = 0.1;

    gain.connect(context.destination);

    sequence.forEach((data, index) => {
      const oscillator = context.createOscillator();

      oscillator.connect(gain);

      oscillator.frequency.value = data.frequency;

      oscillator.type = data.type;

      if (index === sequence.length - 1) {
        oscillator.onended = () => resolve();
      }

      oscillator.start(context.currentTime + data.begin);

      oscillator.stop(context.currentTime + data.end);
    });
  });
}
