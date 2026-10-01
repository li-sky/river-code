import { useCallback, useEffect, useRef, useState } from 'react';

export type VoiceMode = 'free' | 'push-to-talk';
export interface VoiceState {
  joined: boolean;
  muted: boolean;
  deafened: boolean;
  mode: VoiceMode;
  error: string | null;
  peerCount: number;
  transmitting: boolean;
}
type Signal = { kind: 'hello' | 'ready' | 'bye' } | { kind: 'description'; description: RTCSessionDescriptionInit } | { kind: 'candidate'; candidate: RTCIceCandidateInit };
interface Peer {
  pc: RTCPeerConnection;
  audio: HTMLAudioElement;
  makingOffer: boolean;
  ignoreOffer: boolean;
  settingAnswer: boolean;
  candidates: RTCIceCandidateInit[];
  chain: Promise<void>;
  restartCount: number;
}
export interface VoiceOptions {
  localUserId: string;
  sendSignal: (to: string, data: unknown) => void;
  iceServers?: RTCIceServer[];
  muted?: boolean;
  mode?: VoiceMode;
  onState?: (state: VoiceState) => void;
}

/** Per-room WebRTC mesh. A joined participant explicitly consents to microphone access. */
export class VoiceSession {
  private allowed = false;
  private destroyed = false;
  private generation = 0;
  private stream: MediaStream | null = null;
  private joining: Promise<void> | null = null;
  private members = new Set<string>();
  private peers = new Map<string, Peer>();
  private held = false;
  private state: VoiceState;

  constructor(private options: VoiceOptions) {
    this.state = { joined: false, muted: options.muted ?? true, deafened: false, mode: options.mode ?? 'free', error: null, peerCount: 0, transmitting: false };
  }
  getState(): VoiceState { return { ...this.state }; }
  private emit(patch: Partial<VoiceState> = {}): void {
    this.state = { ...this.state, ...patch };
    this.options.onState?.(this.getState());
  }
  private send(id: string, data: Signal): void {
    if (!this.allowed || this.destroyed || !this.state.joined || !this.members.has(id)) return;
    try { this.options.sendSignal(id, data); } catch { this.emit({ error: '语音信令发送失败，请检查连接。' }); }
  }
  setAllowed(allowed: boolean): void {
    if (this.allowed === allowed) return;
    if (!allowed) this.leave();
    this.allowed = allowed;
  }
  setIceServers(iceServers: RTCIceServer[]): void {
    this.options.iceServers = iceServers;
    for (const peer of this.peers.values()) {
      try { peer.pc.setConfiguration({ iceServers }); } catch { /* Applied to the next connection. */ }
    }
  }
  syncPeers(ids: string[]): void {
    const next = new Set(ids.filter(id => id && id !== this.options.localUserId).slice(0, 8));
    for (const id of this.members) if (!next.has(id)) this.dropPeer(id);
    const added = [...next].filter(id => !this.members.has(id));
    this.members = next;
    for (const id of added) this.send(id, { kind: 'hello' });
  }
  async join(): Promise<void> {
    if (!this.allowed || this.destroyed || this.state.joined) return;
    if (this.joining) return this.joining;
    if (!window.isSecureContext || !navigator.mediaDevices?.getUserMedia || typeof RTCPeerConnection === 'undefined') {
      this.emit({ error: '语音需要 HTTPS（本机 localhost 除外）和支持麦克风的浏览器。' });
      return;
    }
    const generation = this.generation;
    this.joining = (async () => {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true }, video: false });
        if (this.destroyed || !this.allowed || generation !== this.generation) {
          stream.getTracks().forEach(track => track.stop());
          return;
        }
        this.stream = stream;
        this.emit({ joined: true, error: null });
        this.applyMicrophone();
        for (const track of stream.getAudioTracks()) track.onended = () => {
          if (this.state.joined) { this.leave(); this.emit({ error: '麦克风已断开，请重新加入语音。' }); }
        };
        for (const id of this.members) this.send(id, { kind: 'hello' });
      } catch (error) {
        if (generation !== this.generation || this.destroyed) return;
        const name = error instanceof DOMException ? error.name : '';
        const message = name === 'NotAllowedError' || name === 'SecurityError' ? '麦克风权限被拒绝，请在浏览器中允许后重试。' : name === 'NotFoundError' || name === 'DevicesNotFoundError' ? '未找到麦克风，请连接设备后重试。' : name === 'NotReadableError' ? '麦克风被其他程序占用，请释放设备后重试。' : '无法加入语音，请检查麦克风和浏览器权限。';
        this.emit({ error: message });
      } finally { this.joining = null; }
    })();
    return this.joining;
  }
  leave(): void {
    for (const id of this.members) this.send(id, { kind: 'bye' });
    this.generation++;
    this.held = false;
    for (const id of [...this.peers.keys()]) this.dropPeer(id);
    this.stream?.getTracks().forEach(track => { track.onended = null; track.stop(); });
    this.stream = null;
    this.emit({ joined: false, transmitting: false, peerCount: 0 });
  }
  setMuted(muted: boolean): void { this.emit({ muted }); this.applyMicrophone(); }
  setDeafened(deafened: boolean): void {
    for (const peer of this.peers.values()) peer.audio.muted = deafened;
    this.emit({ deafened });
  }
  setMode(mode: VoiceMode): void {
    this.held = false;
    this.emit({ mode });
    this.applyMicrophone();
  }
  pressToTalk(pressed: boolean): void { this.held = pressed; this.applyMicrophone(); }
  private applyMicrophone(): void {
    const transmitting = this.allowed && this.state.joined && !this.state.muted && (this.state.mode === 'free' || this.held);
    this.stream?.getAudioTracks().forEach(track => { track.enabled = transmitting; });
    this.emit({ transmitting });
  }
  resumeAudio(): void {
    if (!this.allowed || !this.state.joined) return;
    void Promise.all([...this.peers.values()].map(peer => peer.audio.play())).then(() => {
      if (this.state.error?.includes('语音播放')) this.emit({ error: null });
    }).catch(() => this.emit({ error: '浏览器暂停了语音播放，请点击恢复语音。' }));
  }
  private dropPeer(id: string): void {
    const peer = this.peers.get(id);
    if (!peer) return;
    this.peers.delete(id);
    peer.pc.onicecandidate = null;
    peer.pc.onnegotiationneeded = null;
    peer.pc.ontrack = null;
    peer.pc.onconnectionstatechange = null;
    peer.pc.close();
    peer.audio.pause();
    peer.audio.srcObject = null;
    peer.audio.remove();
    this.countPeers();
  }
  private countPeers(): void {
    this.emit({ peerCount: [...this.peers.values()].filter(peer => peer.pc.connectionState === 'connected').length });
  }
  private createPeer(id: string): Peer | null {
    const existing = this.peers.get(id);
    if (existing) return existing;
    if (!this.allowed || !this.state.joined || !this.stream || !this.members.has(id) || this.destroyed) return null;
    const pc = new RTCPeerConnection({ iceServers: this.options.iceServers ?? [] });
    const audio = document.createElement('audio');
    audio.autoplay = true;
    audio.muted = this.state.deafened;
    audio.setAttribute('playsinline', '');
    audio.setAttribute('aria-hidden', 'true');
    audio.style.display = 'none';
    document.body.appendChild(audio);
    const peer: Peer = { pc, audio, makingOffer: false, ignoreOffer: false, settingAnswer: false, candidates: [], chain: Promise.resolve(), restartCount: 0 };
    this.peers.set(id, peer);
    this.stream.getAudioTracks().forEach(track => pc.addTrack(track, this.stream!));
    pc.onicecandidate = event => { if (event.candidate && this.peers.get(id) === peer) this.send(id, { kind: 'candidate', candidate: event.candidate.toJSON() }); };
    pc.ontrack = event => {
      audio.srcObject = event.streams[0] ?? new MediaStream([event.track]);
      void audio.play().catch(() => this.emit({ error: '浏览器暂停了语音播放，请点击恢复语音。' }));
    };
    pc.onnegotiationneeded = async () => {
      // Elect one initial offerer. Creating audio tracks on both sides otherwise
      // causes avoidable glare; some browsers abandon ICE gathering on rollback.
      // Perfect negotiation below still handles subsequent offer collisions.
      if (!pc.remoteDescription && this.options.localUserId.localeCompare(id) > 0) return;
      try {
        peer.makingOffer = true;
        await pc.setLocalDescription();
        if (pc.localDescription && this.peers.get(id) === peer) this.send(id, { kind: 'description', description: pc.localDescription.toJSON() });
      } catch { if (pc.connectionState !== 'closed' && this.peers.get(id) === peer) this.emit({ error: '语音协商失败，请退出语音后重试。' }); }
      finally { peer.makingOffer = false; }
    };
    pc.onconnectionstatechange = () => {
      this.countPeers();
      if (pc.connectionState === 'connected') peer.restartCount = 0;
      if (pc.connectionState === 'failed') {
        if (peer.restartCount++ < 2) pc.restartIce();
        else this.emit({ error: '部分语音连接失败，跨网络通话可能需要在系统配置中设置 TURN 服务。' });
      }
    };
    return peer;
  }
  handleSignal(from: string, data: unknown): void {
    if (!this.allowed || !this.state.joined || this.destroyed || !this.members.has(from) || typeof data !== 'object' || data === null) return;
    const signal = data as Partial<Signal>;
    if (signal.kind === 'bye') { this.dropPeer(from); return; }
    if (signal.kind === 'hello') {
      // An existing peer belongs to the old remote session if that person rejoined.
      this.dropPeer(from);
      this.send(from, { kind: 'ready' });
      this.createPeer(from);
      return;
    }
    if (signal.kind === 'ready') { this.createPeer(from); return; }
    if (signal.kind !== 'description' && signal.kind !== 'candidate') return;
    const peer = this.createPeer(from);
    if (!peer) return;
    peer.chain = peer.chain.then(async () => {
      if (this.peers.get(from) !== peer || peer.pc.signalingState === 'closed') return;
      const pc = peer.pc;
      if (signal.kind === 'description' && 'description' in signal && signal.description) {
        const description = signal.description;
        if (description.type !== 'offer' && description.type !== 'answer') return;
        const readyForOffer = !peer.makingOffer && (pc.signalingState === 'stable' || peer.settingAnswer);
        const collision = description.type === 'offer' && !readyForOffer;
        const polite = this.options.localUserId.localeCompare(from) > 0;
        peer.ignoreOffer = !polite && collision;
        if (peer.ignoreOffer) return;
        peer.settingAnswer = description.type === 'answer';
        try { await pc.setRemoteDescription(description); }
        finally { peer.settingAnswer = false; }
        const candidates = peer.candidates.splice(0);
        for (const candidate of candidates) await pc.addIceCandidate(candidate);
        if (description.type === 'offer') {
          await pc.setLocalDescription();
          if (pc.localDescription) this.send(from, { kind: 'description', description: pc.localDescription.toJSON() });
        }
      } else if (signal.kind === 'candidate' && 'candidate' in signal && signal.candidate && !peer.ignoreOffer) {
        if (pc.remoteDescription) await pc.addIceCandidate(signal.candidate);
        else if (peer.candidates.length < 128) peer.candidates.push(signal.candidate);
      }
    }).catch(() => {
      if (!peer.ignoreOffer && this.peers.get(from) === peer) this.emit({ error: '语音连接失败，请重新加入语音。' });
    });
  }
  destroy(): void { this.leave(); this.allowed = false; this.destroyed = true; this.options.onState = undefined; }
}

export interface UseVoiceOptions extends Omit<VoiceOptions, 'onState'> {
  roomId: string;
  enabled: boolean;
  players: { id: string }[];
}

export function useVoice(options: UseVoiceOptions) {
  const session = useRef<VoiceSession | null>(null);
  const send = useRef(options.sendSignal);
  send.current = options.sendSignal;
  const [state, setState] = useState<VoiceState>({ joined: false, muted: options.muted ?? true, deafened: false, mode: options.mode ?? 'free', error: null, peerCount: 0, transmitting: false });
  const membersKey = options.players.map(player => player.id).sort().join(',');
  const iceKey = JSON.stringify(options.iceServers ?? []);
  useEffect(() => {
    const next = new VoiceSession({ ...options, sendSignal: (to, data) => send.current(to, data), onState: setState });
    session.current = next;
    next.setAllowed(options.enabled);
    next.syncPeers(options.players.map(player => player.id));
    setState(next.getState());
    const release = () => next.pressToTalk(false);
    const visibility = () => { if (document.hidden) release(); };
    window.addEventListener('blur', release);
    document.addEventListener('visibilitychange', visibility);
    return () => {
      window.removeEventListener('blur', release);
      document.removeEventListener('visibilitychange', visibility);
      next.destroy();
      if (session.current === next) session.current = null;
    };
    // A room or identity change tears down media; changing settings updates it in place below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [options.roomId, options.localUserId]);
  useEffect(() => { session.current?.setAllowed(options.enabled); }, [options.enabled]);
  useEffect(() => { session.current?.syncPeers(options.players.map(player => player.id)); }, [membersKey]);
  useEffect(() => { session.current?.setIceServers(options.iceServers ?? []); }, [iceKey]);
  useEffect(() => { if (options.muted !== undefined) session.current?.setMuted(options.muted); }, [options.muted]);
  useEffect(() => { if (options.mode !== undefined) session.current?.setMode(options.mode); }, [options.mode]);
  return {
    ...state,
    join: useCallback(() => session.current?.join() ?? Promise.resolve(), []),
    leave: useCallback(() => session.current?.leave(), []),
    toggleMute: useCallback(() => { const s = session.current; if (s) s.setMuted(!s.getState().muted); }, []),
    toggleDeafen: useCallback(() => { const s = session.current; if (s) s.setDeafened(!s.getState().deafened); }, []),
    setMode: useCallback((mode: VoiceMode) => session.current?.setMode(mode), []),
    pressToTalk: useCallback((pressed: boolean) => session.current?.pressToTalk(pressed), []),
    handleSignal: useCallback((from: string, data: unknown) => session.current?.handleSignal(from, data), []),
    resumeAudio: useCallback(() => session.current?.resumeAudio(), []),
  };
}
