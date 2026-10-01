// Compile voice.ts to a temporary ES module, then:
// VOICE_MODULE=file:///path/to/voice.js node --test src/lib/voice.test.mjs
import assert from 'node:assert/strict';
import { test } from 'node:test';

const { VoiceSession } = await import(process.env.VOICE_MODULE);
const elements = [];
globalThis.window = { isSecureContext: true };
globalThis.document = { body: { appendChild: element => elements.push(element) }, createElement: () => ({
  style: {}, setAttribute() {}, play: () => Promise.resolve(), pause() {}, remove() { this.removed = true; },
}) };
globalThis.MediaStream = class { constructor(tracks) { this.tracks = tracks; } };
const connections = [];
class FakeConnection {
  connectionState = 'new';
  signalingState = 'stable';
  remoteDescription = null;
  localDescription = null;
  candidates = [];
  constructor(config) { this.config = config; connections.push(this); }
  addTrack() {}
  setConfiguration(config) { this.config = config; }
  close() { this.connectionState = 'closed'; this.signalingState = 'closed'; }
  restartIce() {}
  async setRemoteDescription(description) { this.remoteDescription = description; this.signalingState = description.type === 'offer' ? 'have-remote-offer' : 'stable'; }
  async setLocalDescription() { const type = this.remoteDescription?.type === 'offer' ? 'answer' : 'offer'; this.localDescription = { type, sdp: 'test', toJSON() { return { type, sdp: 'test' }; } }; this.signalingState = type === 'offer' ? 'have-local-offer' : 'stable'; }
  async addIceCandidate(candidate) { assert.ok(this.remoteDescription, 'ICE must wait for a remote description'); this.candidates.push(candidate); }
}
globalThis.RTCPeerConnection = FakeConnection;

function microphone() {
  const track = { enabled: true, stopped: false, onended: null, stop() { this.stopped = true; } };
  return { track, stream: { getTracks: () => [track], getAudioTracks: () => [track] } };
}
function setMicrophone(getUserMedia) {
  Object.defineProperty(globalThis, 'navigator', { value: { mediaDevices: { getUserMedia } }, configurable: true });
}
const tick = () => new Promise(resolve => setImmediate(resolve));

test('disabled voice never requests microphone or forwards signaling', async () => {
  let requests = 0;
  let sends = 0;
  setMicrophone(async () => { requests++; return microphone().stream; });
  const session = new VoiceSession({ localUserId: 'a', sendSignal: () => sends++ });
  session.syncPeers(['a', 'b']);
  await session.join();
  session.handleSignal('b', { kind: 'hello' });
  assert.equal(requests, 0);
  assert.equal(sends, 0);
  assert.equal(session.getState().joined, false);
  session.destroy();
});

test('joining starts muted; push-to-talk, deafen and leave control actual media', async () => {
  const { track, stream } = microphone();
  setMicrophone(async () => stream);
  const signals = [];
  const session = new VoiceSession({ localUserId: 'a', sendSignal: (to, data) => signals.push({ to, data }) });
  session.setAllowed(true);
  session.syncPeers(['a', 'b']);
  await session.join();
  assert.equal(track.enabled, false);
  assert.equal(session.getState().joined, true);
  assert.equal(signals[0].data.kind, 'hello');
  session.setMode('push-to-talk');
  session.setMuted(false);
  assert.equal(track.enabled, false);
  session.pressToTalk(true);
  assert.equal(track.enabled, true);
  session.pressToTalk(false);
  assert.equal(track.enabled, false);
  session.handleSignal('b', { kind: 'ready' });
  session.setDeafened(true);
  assert.equal(elements.at(-1).muted, true);
  session.leave();
  assert.equal(track.stopped, true);
  assert.equal(elements.at(-1).removed, true);
  assert.equal(connections.at(-1).connectionState, 'closed');
  assert.ok(signals.some(item => item.data.kind === 'bye'));
});

test('revoking voice while permission request is pending stops the acquired track', async () => {
  const { track, stream } = microphone();
  let resolve;
  setMicrophone(() => new Promise(done => { resolve = done; }));
  const session = new VoiceSession({ localUserId: 'a', sendSignal() {} });
  session.setAllowed(true);
  const joining = session.join();
  session.setAllowed(false);
  resolve(stream);
  await joining;
  assert.equal(track.stopped, true);
  assert.equal(session.getState().joined, false);
  session.destroy();
});

test('ICE arriving before SDP is queued; signaling accepts only room members', async () => {
  const { stream } = microphone();
  setMicrophone(async () => stream);
  const signals = [];
  const session = new VoiceSession({ localUserId: 'z', sendSignal: (to, data) => signals.push({ to, data }) });
  session.setAllowed(true);
  session.syncPeers(['z', 'a']);
  await session.join();
  const before = connections.length;
  session.handleSignal('stranger', { kind: 'hello' });
  assert.equal(connections.length, before);
  session.handleSignal('a', { kind: 'candidate', candidate: { candidate: 'ice' } });
  await tick();
  assert.equal(connections.at(-1).candidates.length, 0);
  session.handleSignal('a', { kind: 'description', description: { type: 'offer', sdp: 'offer' } });
  await tick();
  assert.equal(connections.at(-1).candidates.length, 1);
  assert.ok(signals.some(item => item.data.kind === 'description' && item.data.description.type === 'answer'));
  session.destroy();
});

test('permission rejection leaves the session unjoined with an actionable error', async () => {
  setMicrophone(async () => { throw new DOMException('Denied', 'NotAllowedError'); });
  const session = new VoiceSession({ localUserId: 'a', sendSignal() {} });
  session.setAllowed(true);
  await session.join();
  assert.equal(session.getState().joined, false);
  assert.match(session.getState().error, /权限被拒绝/);
  session.destroy();
});

test('simultaneous offers use deterministic polite/impolite perfect negotiation', async () => {
  for (const [localUserId, remoteId, accepts] of [['a', 'z', false], ['z', 'a', true]]) {
    setMicrophone(async () => microphone().stream);
    const session = new VoiceSession({ localUserId, sendSignal() {} });
    session.setAllowed(true);
    session.syncPeers([localUserId, remoteId]);
    await session.join();
    session.handleSignal(remoteId, { kind: 'ready' });
    const pc = connections.at(-1);
    // Force an in-flight local offer to exercise renegotiation glare, independently
    // of the deterministic election used for the initial connection.
    await pc.setLocalDescription();
    assert.equal(pc.signalingState, 'have-local-offer');
    session.handleSignal(remoteId, { kind: 'description', description: { type: 'offer', sdp: 'simultaneous' } });
    await tick();
    assert.equal(Boolean(pc.remoteDescription), accepts);
    assert.equal(pc.signalingState, accepts ? 'stable' : 'have-local-offer');
    session.destroy();
  }
});

test('only the elected initial offerer starts negotiation', async () => {
  for (const [localUserId, remoteId, offers] of [['a', 'z', true], ['z', 'a', false]]) {
    setMicrophone(async () => microphone().stream);
    const session = new VoiceSession({ localUserId, sendSignal() {} });
    session.setAllowed(true);
    session.syncPeers([localUserId, remoteId]);
    await session.join();
    session.handleSignal(remoteId, { kind: 'ready' });
    const pc = connections.at(-1);
    await pc.onnegotiationneeded();
    assert.equal(Boolean(pc.localDescription), offers);
    session.destroy();
  }
});
