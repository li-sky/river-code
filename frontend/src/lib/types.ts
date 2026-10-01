export interface UserSettings {
  soundEnabled: boolean;
  volume: number;
  voiceMuted: boolean;
  avatarEmoji: string;
}
export interface User {
  id: string;
  name: string;
  avatarUrl: string;
  guest: boolean;
  settings: UserSettings;
}
export interface Config {
  guestEnabled: boolean;
  githubEnabled: boolean;
  passwordRequired: boolean;
  voiceEnabled: boolean;
  chatEnabled: boolean;
  reactionsEnabled: boolean;
  iceServers: RTCIceServer[];
  defaultVoiceMode: "free" | "push-to-talk";
  spectatorVoiceEnabled: boolean;
}
export interface RoomSettings {
  visibility: "public" | "private";
  smallBlind: number;
  bigBlind: number;
  buyIn: number;
  maxPlayers: number;
  actionSeconds: number;
  voiceEnabled: boolean;
  voiceMode: "free" | "push-to-talk";
  spectatorVoiceEnabled: boolean;
  chatEnabled: boolean;
  reactionsEnabled: boolean;
}
export interface RoomSummary {
  id: string;
  name: string;
  hostId: string;
  players: number;
  settings: RoomSettings;
  status: string;
}
export interface Player {
  id: string;
  name: string;
  avatarUrl: string;
  avatarEmoji: string;
  seat: number;
  stack: number;
  wins?: number;
  connected: boolean;
  sittingOut: boolean;
}
export interface HandPlayer {
  id: string;
  seat: number;
  bet: number;
  totalBet: number;
  folded: boolean;
  allIn: boolean;
  cards: string[];
  acted: boolean;
  canRaise: boolean;
  currentHand?: string;
}
export interface HandView {
  number: number;
  turnToken: string;
  phase: "preflop" | "flop" | "turn" | "river" | "showdown" | "complete";
  board: string[];
  pot: number;
  dealerSeat: number;
  turnSeat: number;
  currentBet: number;
  minRaise: number;
  deadline: string;
  players: HandPlayer[];
  winners?: { id: string; amount: number; description: string }[];
}
export interface Message {
  id: string;
  userId: string;
  name: string;
  text: string;
  at: string;
  recalled?: boolean;
}
export interface RoomState {
  id: string;
  name: string;
  hostId: string;
  settings: RoomSettings;
  players: Player[];
  hand: HandView | null;
  messages: Message[];
  pinnedMessage: Message | null;
  version: number;
  voiceParticipantIds: string[];
}
export type WSMessage =
  | { type: "state"; state: RoomState }
  | { type: "error"; message: string }
  | { type: "reaction"; from: string; to: string; emoji: string }
  | { type: "signal"; from: string; data: unknown }
  | { type: "sound"; sound: "deal" | "chips" | "fold" | "win" };
