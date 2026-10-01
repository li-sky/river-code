export type HapticName = 'check' | 'call' | 'raise' | 'fold' | 'chip';

// The web API controls timing only. Shorter trailing pulses suggest a soft landing.
const patterns: Record<HapticName, number[]> = {
  check: [18, 65, 10],
  call: [14, 24, 9],
  raise: [16, 18, 11, 22, 7, 26, 4],
  fold: [24, 8, 5],
  chip: [6, 13, 4],
};

function vibrate(pattern: number[]): boolean {
  try {
    if (
      typeof window === 'undefined' ||
      typeof navigator === 'undefined' ||
      typeof navigator.vibrate !== 'function' ||
      document.visibilityState !== 'visible' ||
      !window.matchMedia('(any-pointer: coarse)').matches ||
      window.matchMedia('(prefers-reduced-motion: reduce)').matches ||
      navigator.userActivation?.hasBeenActive === false
    ) return false;
    return navigator.vibrate([...pattern]);
  } catch {
    // Unavailable hardware or browser policy must never block a poker action.
    return false;
  }
}

export function playHaptic(name: HapticName): boolean {
  return vibrate(patterns[name]);
}

export function stopHaptics(): void {
  try {
    if (typeof navigator !== 'undefined' && typeof navigator.vibrate === 'function') {
      navigator.vibrate(0);
    }
  } catch { /* Best-effort cancellation also works after the page becomes hidden. */ }
}

/** Quantize the whole legal range, rather than buzzing once per chip or DOM event. */
export class ChipSliderHaptics {
  private band: number | null = null;
  private lastPulse = -Infinity;
  private ticks = 0;

  private bandFor(value: number, min: number, max: number): number | null {
    if (![value, min, max].every(Number.isFinite) || max <= min) return null;
    return Math.round(Math.max(0, Math.min(1, (value - min) / (max - min))) * 32);
  }

  begin(value: number, min: number, max: number): void {
    this.reset();
    this.band = this.bandFor(value, min, max);
  }

  update(value: number, min: number, max: number): boolean {
    const next = this.bandFor(value, min, max);
    if (next === null || next === this.band) return false;
    const distance = this.band === null ? 1 : Math.abs(next - this.band);
    this.band = next;
    const now = performance.now();
    if (now - this.lastPulse < 50) return false;
    this.lastPulse = now;
    const pattern = next === 0 || next === 32
      ? [12, 14, 6]
      : distance >= 3
        ? [6, 9, 4]
        : [this.ticks++ % 2 === 0 ? 6 : 9];
    return vibrate(pattern);
  }

  reset(): void {
    this.band = null;
    this.lastPulse = -Infinity;
    this.ticks = 0;
  }

  cancel(): void {
    this.reset();
    stopHaptics();
  }
}
