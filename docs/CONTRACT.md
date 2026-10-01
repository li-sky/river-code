# RIVER implementation contract

Self-hosted Go + PostgreSQL + React/TypeScript. Chinese UI; system configuration = 配置; personal/room settings = 设置. No-limit cash tables, integer entertainment chips, max 9 seats. Server owns all cards and actions. Changes to blinds/stacks occur between hands. Guest means room participant; unauthenticated identities are visitors.

## HTTP (same origin, httpOnly session cookie)
- GET /api/config => {guestEnabled, githubEnabled, passwordRequired, voiceEnabled, chatEnabled, reactionsEnabled, defaultVoiceMode:'free'|'push-to-talk', spectatorVoiceEnabled, iceServers: RTCIceServer[]}; ICE credentials omitted until authenticated.
- POST /api/auth/guest {name,password}; POST /api/auth/register {name,email,password,joinPassword}; POST /api/auth/login {email,password,joinPassword}; GET /api/auth/github; POST /api/auth/logout
- GET /api/me => User or 401. User {id,name,avatarUrl,guest,settings:{soundEnabled,volume,voiceMuted,avatarEmoji}}
- PATCH /api/me {name?,avatarUrl?,gravatarEmail?,settings?}; POST /api/me/avatar multipart file
- GET /api/rooms => public RoomSummary[] only (private rooms are omitted even for their host); POST /api/rooms {name,settings:RoomSettings} => {id}
- GET /api/rooms/:id => personalized RoomState; GET /api/rooms/:id/ws websocket

RoomSettings {visibility:'public'|'private',smallBlind,bigBlind,buyIn,maxPlayers,actionSeconds,voiceEnabled,voiceMode:'free'|'push-to-talk',spectatorVoiceEnabled,chatEnabled,reactionsEnabled}; omitted voiceMode uses system default. Visibility is required on creation, settings commands, and saved snapshots; missing, empty, or invalid values are rejected. No legacy snapshot migration or visibility fallback is provided. The new-room UI initially selects public.
Private rooms use the existing /?room=<128-bit random room ID> invitation link: any authenticated identity with the link can read personalized state and join over WS. Private means hidden from discovery, with no separate room password or per-user invitation list. Public/private changes are host-only between hands, persist in the room snapshot, and broadcast after successful save. Links and existing members remain valid after switching.
RoomSummary {id,name,hostId,players,settings,status}
RoomState {id,name,hostId,settings,players:Player[],hand:HandView|null,messages:Message[],pinnedMessage:Message|null,version,voiceParticipantIds:string[]}; authoritative voice roster is capped at 9, connected seated players first, then permitted connected spectators. Both system and room spectator flags are required.
Player {id,name,avatarUrl,avatarEmoji,seat:number,stack:number,connected:boolean,sittingOut:boolean}
HandView {number,phase:'preflop'|'flop'|'turn'|'river'|'showdown'|'complete',board:string[],pot:number,dealerSeat:number,turnSeat:number,currentBet:number,minRaise:number,deadline:string,turnToken:string,players:HandPlayer[],winners?:{id,amount,description}[]}
HandPlayer {id,seat,bet,totalBet,folded,allIn,cards:string[],acted:boolean,canRaise:boolean}
Cards rank+suit eg As, Th, 2c; opponent hidden cards = [].
Message {id,userId,name,text,at}

## WS
Server: {type:'state',state:RoomState}; {type:'error',message}; {type:'reaction',from,to,emoji}; {type:'signal',from,data}; {type:'sound',sound:'deal'|'chips'|'fold'|'win'}
Client: {type:'sit',seat,buyIn?}; {type:'stand'}; {type:'start'} (host next hand); {type:'action',action:'fold'|'check'|'call'|'raise'|'allin',amount?,turnToken} (raise amount is TOTAL street bet; token is from current hand view and rejects stale/double submissions); {type:'settings',settings:RoomSettings}; {type:'stack',playerId,amount} host between hands; {type:'chat',text}; {type:'reaction',to,emoji}; {type:'emoji',emoji}; {type:'signal',to,data}; {type:'leave'}.

Emoji strings in room commands and user settings allow up to 16 Unicode code points and 64 UTF-8 bytes, rejecting CR/LF/NUL. Avatar emoji may be empty to clear; directed reactions must be nonempty. The picker sends native Unicode, including skin-tone and ZWJ sequences, from its complete bundled dataset. This length validation is not an emoji whitelist.
Host chat moderation: {type:'pin_message',messageId} pins or replaces the room's single pinned message using a server-owned message from this room's latest 100 messages; client-supplied content is ignored. {type:'unpin_message',messageId} clears only a matching current pin; stale/missing IDs are rejected. Both commands require the current host and system/room chat enabled, may run during a hand, and share a per-connection limit of 12 operations per 10 seconds. Save before broadcast; failures restore pin/version and produce no state broadcast. pinnedMessage stores a separate Message copy in the room snapshot, retained after history trimming, author departure, and host migration. Snapshots predating this field restore with null. Chat disabled hides its panel and preserves the pin for re-enabling. No SQL schema change or separate message-table integration.

Private cards must never appear in public list or other players' state. Spectators use seat -1. Engine persists full private JSON snapshot, public view separately. Timers/recovery occur server-side. HTTPS needed for microphone outside localhost; TURN configurable for NAT.

Host may issue {type:'kick',playerId} between hands. Expelled sockets close with code4003 and do not automatically reconnect. Between hands, disconnected spectators are pruned after2minutes and seated players after5minutes; host migrates after30seconds offline. Active hands retain departed participants through settlement.

## Backend package interfaces
Poker package owned by engine agent; engine exports types/functions and publishes own final exact API promptly. Server agent adapts to engine. Identity/store owned by identity agent; exports repository and HTTP/session interfaces and reports exact API promptly. module river; Go >=1.23. Main/dependency files owned by server agent.

## Frontend helpers
lib/api.ts frontend-owned api fetch helper; lib/types.ts frontend-owned above types. lib/sound.ts media-owned exports playSound(name), setSoundSettings(enabled,volume). lib/voice.ts media-owned exports VoiceSession class or useVoice hook; coordinate API with frontend owner. CSS and components frontend-owned.
