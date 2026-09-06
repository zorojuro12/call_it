export type Cue = "open" | "lock" | "resolve";

export type AudioContextFactory = () => AudioContext;

const CUE_FREQUENCIES: Record<Cue, number> = {
  open: 440,
  lock: 330,
  resolve: 660,
};

const TONE_DURATION_SECONDS = 0.15;

export function playCue(cue: Cue, factory: AudioContextFactory = () => new AudioContext()): void {
  try {
    const context = factory();
    const oscillator = context.createOscillator();
    const gain = context.createGain();

    oscillator.frequency.value = CUE_FREQUENCIES[cue];
    oscillator.connect(gain);
    gain.connect(context.destination);

    oscillator.start();
    oscillator.stop(context.currentTime + TONE_DURATION_SECONDS);
  } catch {
    // Browsers block audio before a user gesture, and some environments
    // have no AudioContext at all; neither is an error worth surfacing.
    // Swallow: a blocked or missing AudioContext must never crash a render.
  }
}
