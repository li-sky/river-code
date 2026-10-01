export type SoundName = 'deal' | 'chips' | 'fold' | 'win' | 'reaction';

let audio: AudioContext | undefined;
let enabled = true;
let volume = 0.65;
const lastPlayed = new Map<SoundName, number>();

function context(): AudioContext | undefined {
  if (typeof window === 'undefined') return;
  try {
    const Audio = window.AudioContext || (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (Audio && (!audio || audio.state === 'closed')) audio = new Audio();
    return audio;
  } catch { return; }
}

export function setSoundSettings(on: boolean, level: number): void {
  enabled = on;
  volume = Number.isFinite(level) ? Math.max(0, Math.min(1, level)) : 0.65;
}

/** Call from a click/touch before browser autoplay restrictions have been lifted. */
export function unlockAudio(): void {
  if (!enabled) return;
  const ctx = context();
  if (ctx?.state === 'suspended') void ctx.resume().catch(() => {});
}

function tone(ctx: AudioContext, output: AudioNode, at: number, frequency: number, duration: number, level: number, endFrequency = frequency): void {
  const oscillator = ctx.createOscillator();
  const envelope = ctx.createGain();
  oscillator.type = 'sine';
  oscillator.frequency.setValueAtTime(frequency, at);
  oscillator.frequency.exponentialRampToValueAtTime(endFrequency, at + duration);
  envelope.gain.setValueAtTime(0.001, at);
  envelope.gain.exponentialRampToValueAtTime(level, at + 0.003);
  envelope.gain.exponentialRampToValueAtTime(0.001, at + duration);
  oscillator.connect(envelope).connect(output);
  oscillator.start(at);
  oscillator.stop(at + duration + 0.01);
  oscillator.onended = () => { oscillator.disconnect(); envelope.disconnect(); };
}

function noise(ctx: AudioContext, output: AudioNode, at: number, duration: number, level: number, frequency: number, bandwidth = 1): void {
  const buffer = ctx.createBuffer(1, Math.ceil(ctx.sampleRate * duration), ctx.sampleRate);
  const samples = buffer.getChannelData(0);
  for (let i = 0; i < samples.length; i++) samples[i] = Math.random() * 2 - 1;
  const source = ctx.createBufferSource();
  const filter = ctx.createBiquadFilter();
  const envelope = ctx.createGain();
  source.buffer = buffer;
  filter.type = 'bandpass';
  filter.frequency.value = frequency;
  filter.Q.value = bandwidth;
  envelope.gain.setValueAtTime(0.001, at);
  envelope.gain.exponentialRampToValueAtTime(level, at + 0.004);
  envelope.gain.exponentialRampToValueAtTime(0.001, at + duration);
  source.connect(filter).connect(envelope).connect(output);
  source.start(at);
  source.onended = () => { source.disconnect(); filter.disconnect(); envelope.disconnect(); };
}

/** Small original synthesized cues; no downloads, tracking, or licensed audio assets. */
export function playSound(name: SoundName): void {
  if (!enabled || volume <= 0) return;
  const ctx = context();
  if (!ctx || ctx.state !== 'running') return;
  const now = ctx.currentTime;
  if (now - (lastPlayed.get(name) ?? -Infinity) < 0.07) return;
  lastPlayed.set(name, now);
  const output = ctx.createGain();
  output.gain.value = volume * 0.6;
  output.connect(ctx.destination);
  let length = 0.3;
  switch (name) {
    case 'deal':
      noise(ctx, output, now, 0.095, 0.24, 3300, 0.6);
      noise(ctx, output, now + 0.055, 0.035, 0.14, 1700, 1.5);
      tone(ctx, output, now + 0.07, 230, 0.045, 0.06, 125);
      break;
    case 'chips':
      for (let i = 0; i < 7; i++) {
        const at = now + i * 0.029 + Math.random() * 0.008;
        noise(ctx, output, at, 0.022, 0.13, 2300 + Math.random() * 1500, 2);
        tone(ctx, output, at, 800 + i * 90, 0.04, 0.065, 550);
      }
      tone(ctx, output, now + 0.19, 185, 0.065, 0.1, 110);
      length = 0.4;
      break;
    case 'fold':
      noise(ctx, output, now, 0.12, 0.1, 1300, 0.8);
      tone(ctx, output, now + 0.06, 150, 0.055, 0.045, 90);
      break;
    case 'win':
      [523.25, 659.25, 783.99, 1046.5].forEach((frequency, i) => tone(ctx, output, now + i * 0.09, frequency, 0.34, 0.11));
      length = 0.75;
      break;
    case 'reaction':
      tone(ctx, output, now, 410, 0.14, 0.095, 820);
      tone(ctx, output, now + 0.09, 1000, 0.08, 0.055, 700);
      break;
  }
  // An audio-clock source cleans up the bus even when a background tab suspends JS timers.
  const cleanup = ctx.createOscillator();
  const silence = ctx.createGain();
  silence.gain.value = 0;
  cleanup.connect(silence).connect(output);
  cleanup.start(now);
  cleanup.stop(now + length);
  cleanup.onended = () => { cleanup.disconnect(); silence.disconnect(); output.disconnect(); };
}
