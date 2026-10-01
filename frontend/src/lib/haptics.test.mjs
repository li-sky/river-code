import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

const source = readFileSync(new URL('./haptics.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext },
});
const { playHaptic, stopHaptics, ChipSliderHaptics } = await import(
  'data:text/javascript;base64,' + Buffer.from(outputText).toString('base64')
);

function environment(t, { coarse = true, reduced = false, visible = true,
    available = true, active = true, result = true, throws = false } = {}) {
  const calls = [];
  let now = 0;
  const globals = {
    window: { matchMedia: query => ({ matches: query.includes('coarse') ? coarse : reduced }) },
    document: { visibilityState: visible ? 'visible' : 'hidden' },
    navigator: {
      userActivation: { hasBeenActive: active },
      ...(available ? { vibrate: pattern => {
        if (throws) throw new Error('API denied');
        calls.push(pattern); return result;
      }} : {}),
    },
    performance: { now: () => now },
  };
  for (const [name, value] of Object.entries(globals)) {
    const original = Object.getOwnPropertyDescriptor(globalThis, name);
    Object.defineProperty(globalThis, name, { value, configurable: true });
    t.after(() => {
      if (original) Object.defineProperty(globalThis, name, original);
      else delete globalThis[name];
    });
  }
  return { calls, advance: amount => { now += amount; } };
}

test('distinct bounded action rhythms: check double tap and denser raise', t => {
  const { calls } = environment(t);
  for (const action of ['check', 'call', 'raise', 'fold']) assert(playHaptic(action));
  const [check, call, raise, fold] = calls;
  assert.equal(check.length, 3);
  assert(check[2] < check[0], 'the second tap is shorter');
  assert(raise.length > call.length, 'raising has more collisions');
  assert(fold.reduce((a, b) => a + b, 0) < check.reduce((a, b) => a + b, 0));
  for (const pattern of calls) {
    assert(pattern.length <= 9);
    assert(pattern.reduce((a, b) => a + b, 0) <= 150);
    assert(pattern.every(duration => duration > 0));
  }
});

for (const [label, options] of Object.entries({
  'no API': { available: false }, 'desktop': { coarse: false },
  'reduced motion': { reduced: true }, 'hidden page': { visible: false },
  'no activation': { active: false }, 'rejected API': { result: false },
  'throwing API': { throws: true },
})) test(`graceful fallback for ${label}`, t => {
  const { calls } = environment(t, options);
  assert.equal(playHaptic('raise'), false);
  assert.doesNotThrow(() => new ChipSliderHaptics().update(50, 0, 100));
  assert.doesNotThrow(stopHaptics);
  if (['no API', 'desktop', 'reduced motion', 'no activation'].includes(label)) {
    assert(calls.every(pattern => pattern === 0));
  }
});

test('slider skips unchanged bands and caps rapid updates', t => {
  const { calls, advance } = environment(t);
  const slider = new ChipSliderHaptics();
  slider.begin(0, 0, 3200);
  assert.equal(slider.update(5, 0, 3200), false);
  assert.equal(slider.update(100, 0, 3200), true);
  for (let i = 2; i <= 16; i++) {
    advance(2);
    assert.equal(slider.update(i * 100, 0, 3200), false);
  }
  assert.equal(calls.length, 1);
  advance(50);
  assert.equal(slider.update(1700, 0, 3200), true);
  assert.equal(slider.update(1701, 0, 3200), false);
  advance(50);
  assert.equal(slider.update(3200, 0, 3200), true);
  assert(calls.at(-1).length > calls[0].length, 'endpoint has a softer double stop');
});

test('slider feedback scales with legal range and follows reversal', t => {
  const { calls, advance } = environment(t);
  const slider = new ChipSliderHaptics();
  slider.begin(40, 40, 1000000040);
  assert.equal(slider.update(50, 40, 1000000040), false);
  assert.equal(slider.update(500000040, 40, 1000000040), true);
  advance(60);
  assert.equal(slider.update(250000040, 40, 1000000040), true);
  assert.equal(calls.length, 2);
});

test('collapsed or invalid ranges cannot buzz', t => {
  const { calls } = environment(t);
  const slider = new ChipSliderHaptics();
  for (const [value, min, max] of [[40, 40, 40], [30, 40, 20], [NaN, 0, 40], [5, 0, Infinity]]) {
    slider.begin(value, min, max);
    assert.equal(slider.update(value, min, max), false);
  }
  assert.deepEqual(calls, []);
});

test('cancellation stops hardware and reset permits the next gesture', t => {
  const { calls } = environment(t);
  const slider = new ChipSliderHaptics();
  slider.begin(0, 0, 100);
  slider.update(20, 0, 100);
  slider.cancel();
  assert.equal(calls.at(-1), 0);
  slider.begin(20, 0, 100);
  assert.equal(slider.update(30, 0, 100), true);
  slider.reset();
  assert.equal(slider.update(50, 0, 100), true);
});
