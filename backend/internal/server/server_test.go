package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"river/internal/config"
	"river/internal/identity"
	"river/internal/poker"
	"river/internal/store"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	db, err := store.Open(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{BaseURL: "http://localhost", GuestEnabled: true, VoiceEnabled: true, ChatEnabled: true, ReactionsEnabled: true, SessionTTL: time.Hour, StaticDir: t.TempDir()}
	s, err := New(context.Background(), cfg, identity.New(cfg, db), db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); db.Close() })
	return s
}
func fixtureRoom(s *Server) *room {
	r := &room{roomData: roomData{ID: "testroom", Name: "Test", HostID: "host", Settings: RoomSettings{Visibility: "public", SmallBlind: 5, BigBlind: 10, BuyIn: 1000, MaxPlayers: 9, ActionSeconds: 30, VoiceEnabled: true, ChatEnabled: true, ReactionsEnabled: true}, Players: []*Player{{ID: "host", Name: "Host", Seat: 0, Stack: 1000, Connected: true}, {ID: "guest", Name: "Guest", Seat: 1, Stack: 1000, Connected: true}}, Messages: []Message{}, LastDealer: -1}, clients: map[string]*client{}, system: &s.cfg}
	for _, p := range r.Players {
		r.clients[p.ID] = &client{id: p.ID, room: r, send: make(chan []byte, 64), rates: map[string]rate{}}
	}
	s.rooms[r.ID] = r
	return r
}
func TestHostPermissionsAndHandPrivacy(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	if err := s.command(r.clients["guest"], command{Type: "start"}); err == nil {
		t.Fatal("guest started hand")
	}
	if err := s.command(r.clients["host"], command{Type: "start"}); err != nil {
		t.Fatal(err)
	}
	if err := s.command(r.clients["host"], command{Type: "stack", PlayerID: "guest", Amount: 500}); err == nil {
		t.Fatal("stack changed during hand")
	}
	if err := s.command(r.clients["host"], command{Type: "settings", Settings: r.Settings}); err == nil {
		t.Fatal("settings changed during hand")
	}
	for _, viewer := range []string{"host", "guest", "spectator"} {
		raw, _ := json.Marshal(r.view(viewer))
		var state struct {
			Hand poker.HandView `json:"hand"`
		}
		if err := json.Unmarshal(raw, &state); err != nil {
			t.Fatal(err)
		}
		for _, p := range state.Hand.Players {
			if p.ID == viewer {
				if len(p.Cards) != 2 {
					t.Fatal("own cards missing")
				}
			} else if len(p.Cards) != 0 {
				t.Fatalf("private cards leaked to %s", viewer)
			}
		}
		if bytes.Contains(raw, []byte(`"deck"`)) {
			t.Fatal("private deck leaked")
		}
	}
}
func TestTimeoutAndRestartRecovery(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	if err := s.command(r.clients["host"], command{Type: "start"}); err != nil {
		t.Fatal(err)
	}
	r.Deadline = time.Now().Add(-time.Second)
	if err := s.save(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	recovered, err := New(context.Background(), s.cfg, s.auth, s.db)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	rr := recovered.find(r.ID)
	if rr.Deadline.After(time.Now()) {
		t.Fatal("restart extended expired deadline")
	}
	for _, p := range rr.Players {
		if p.Connected {
			t.Fatal("recovered phantom connection")
		}
	}
	recovered.tick(time.Now())
	if !rr.Hand.Finished() {
		t.Fatal("timeout did not fold heads-up small blind")
	}
	total := int64(0)
	for _, p := range rr.Players {
		total += p.Stack
	}
	if total != 2000 {
		t.Fatalf("chips lost: %d", total)
	}
	records, _ := s.db.LoadRooms(context.Background())
	var saved roomData
	_ = json.Unmarshal(records[0].Snapshot, &saved)
	if !saved.Hand.Finished() {
		t.Fatal("timeout not durable")
	}
}
func TestSignalIsolationAndDisabledFeatures(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	guest := r.clients["guest"]
	if err := s.command(guest, command{Type: "signal", To: "outside", Data: json.RawMessage(`{"type":"offer"}`)}); err == nil {
		t.Fatal("signal accepted outside room")
	}
	if err := s.command(guest, command{Type: "signal", To: "host", Data: json.RawMessage(`{"type":"offer"}`)}); err != nil {
		t.Fatal(err)
	}
	raw := <-r.clients["host"].send
	if !bytes.Contains(raw, []byte(`"from":"guest"`)) {
		t.Fatal("untrusted sender")
	}
	r.Settings.VoiceEnabled = false
	if err := s.command(guest, command{Type: "signal", To: "host", Data: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("disabled voice accepted")
	}
	s.cfg.ChatEnabled = false
	if err := s.command(guest, command{Type: "chat", Text: "hello"}); err == nil {
		t.Fatal("disabled chat accepted")
	}
}
func TestRollbackRestoresOmittedFields(t *testing.T) {
	r := &room{roomData: roomData{Players: []*Player{{ID: "x", Seat: 0}}}}
	old, _ := json.Marshal(r.roomData)
	r.Players[0].Leaving = true
	r.Players[0].DisconnectedAt = time.Now()
	r.restore(old)
	if r.Players[0].Leaving || !r.Players[0].DisconnectedAt.IsZero() {
		t.Fatal("rollback retained mutated omitted fields")
	}
}
func TestHTTPAuthAndWebSocketOrigin(t *testing.T) {
	s := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/rooms")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal("anonymous room access")
	}
	login, err := http.Post(ts.URL+"/api/auth/guest", "application/json", strings.NewReader(`{"name":"Alice","password":""}`))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, login.Body)
	login.Body.Close()
	if login.StatusCode != 200 && login.StatusCode != 201 {
		t.Fatalf("login %d", login.StatusCode)
	}
	cookies := login.Cookies()
	if len(cookies) == 0 {
		t.Fatal("missing session cookie")
	}
	r := fixtureRoom(s)
	header := http.Header{"Cookie": []string{cookies[0].Name + "=" + cookies[0].Value}, "Origin": []string{"https://evil.example"}}
	ws, rejected, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/api/rooms/"+r.ID+"/ws", header)
	if ws != nil {
		ws.Close()
	}
	if err == nil || rejected == nil || rejected.StatusCode != 403 {
		t.Fatal("cross origin websocket accepted")
	}
	header.Set("Origin", ts.URL)
	ws, _, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/api/rooms/"+r.ID+"/ws", header)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var message map[string]any
	if err = ws.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	if message["type"] != "state" {
		t.Fatal("state not sent")
	}
	state := message["state"].(map[string]any)
	if len(state["players"].([]any)) != 3 {
		t.Fatal("authenticated visitor did not join as spectator")
	}
	// Logout must revoke an already-open recipient before another private state is sent.
	logoutReq, _ := http.NewRequest("POST", ts.URL+"/api/auth/logout", nil)
	logoutReq.Header.Set("Cookie", cookies[0].Name+"="+cookies[0].Value)
	logout, err := http.DefaultClient.Do(logoutReq)
	if err != nil {
		t.Fatal(err)
	}
	logout.Body.Close()
	if err := s.command(r.clients["host"], command{Type: "start"}); err != nil {
		t.Fatal(err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(time.Second))
	if err := ws.ReadJSON(&message); err == nil {
		t.Fatal("logged-out websocket received future hand state")
	}
}

type brokenStore struct {
	roomStore
	fail atomic.Bool
}

func (b *brokenStore) SaveRoom(ctx context.Context, id, name, host string, data []byte) error {
	if b.fail.Load() {
		return errors.New("database unavailable")
	}
	return b.roomStore.SaveRoom(ctx, id, name, host, data)
}
func TestPersistenceFailureDoesNotBroadcastOrAdvanceHand(t *testing.T) {
	s := testServer(t)
	db := &brokenStore{roomStore: s.db}
	s.db = db
	r := fixtureRoom(s)
	if err := s.command(r.clients["host"], command{Type: "start"}); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(r.roomData)
	counts := map[string]int{}
	for id, c := range r.clients {
		counts[id] = len(c.send)
	}
	db.fail.Store(true)
	turnID := ""
	for _, p := range r.Hand.Players {
		if p.Seat == r.Hand.TurnSeat {
			turnID = p.ID
		}
	}
	if err := s.command(r.clients[turnID], command{Type: "action", Action: "fold", TurnToken: r.TurnToken}); err == nil {
		t.Fatal("write failure accepted")
	}
	after, _ := json.Marshal(r.roomData)
	if !bytes.Equal(before, after) {
		t.Fatal("hand advanced despite failed persistence")
	}
	for id, c := range r.clients {
		if len(c.send) != counts[id] {
			t.Fatal("uncommitted state broadcast")
		}
	}
	db.fail.Store(false)
	if err := s.command(r.clients[turnID], command{Type: "action", Action: "fold", TurnToken: r.TurnToken}); err != nil {
		t.Fatal(err)
	}
	if !r.Hand.Finished() {
		t.Fatal("hand failed after repository recovered")
	}
}

func TestStaleTurnTokenRejectsRepeatedCheckOnNextStreet(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	if err := s.command(r.clients["host"], command{Type: "start"}); err != nil {
		t.Fatal(err)
	}
	if err := s.command(r.clients["host"], command{Type: "action", Action: "call", TurnToken: r.TurnToken}); err != nil {
		t.Fatal(err)
	}
	stale := r.TurnToken
	if err := s.command(r.clients["guest"], command{Type: "action", Action: "check", TurnToken: stale}); err != nil {
		t.Fatal(err)
	}
	if r.Hand.Phase != "flop" || r.Hand.TurnSeat != 1 {
		t.Fatal("expected big blind to act first on flop")
	}
	before, _ := json.Marshal(r.roomData)
	if err := s.command(r.clients["guest"], command{Type: "action", Action: "check", TurnToken: stale}); err == nil {
		t.Fatal("duplicate preflop action accepted on flop")
	}
	after, _ := json.Marshal(r.roomData)
	if !bytes.Equal(before, after) {
		t.Fatal("stale action changed state")
	}
	if err := (RoomSettings{Visibility: "public", SmallBlind: 9223372036854775807, BigBlind: 10, BuyIn: 1000, MaxPlayers: 9, ActionSeconds: 30}).validate(); err == nil {
		t.Fatal("overflowing small blind accepted")
	}
}

func TestAbandonedPlayersReclaimedOnlyAfterSettlement(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	if err := s.command(r.clients["host"], command{Type: "start"}); err != nil {
		t.Fatal(err)
	}
	r.Deadline = time.Now().Add(time.Minute)
	r.player("guest").Connected = false
	r.player("guest").DisconnectedAt = time.Now().Add(-10 * time.Minute)
	s.tick(time.Now())
	if r.player("guest") == nil {
		t.Fatal("active hand player removed before settlement")
	}
	if err := s.command(r.clients["host"], command{Type: "action", Action: "fold", TurnToken: r.TurnToken}); err != nil {
		t.Fatal(err)
	}
	r.Players = append(r.Players, &Player{ID: "spectator", Seat: -1, DisconnectedAt: time.Now().Add(-3 * time.Minute)})
	s.tick(time.Now())
	if r.player("guest") != nil || r.player("spectator") != nil {
		t.Fatal("abandoned memberships were not reclaimed")
	}
}
func TestHostKickIsDurableAndBetweenHands(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	if err := s.command(r.clients["guest"], command{Type: "kick", PlayerID: "host"}); err == nil {
		t.Fatal("guest kicked host")
	}
	if err := s.command(r.clients["host"], command{Type: "start"}); err != nil {
		t.Fatal(err)
	}
	if err := s.command(r.clients["host"], command{Type: "kick", PlayerID: "guest"}); err == nil {
		t.Fatal("active player kicked")
	}
	if err := s.command(r.clients["host"], command{Type: "action", Action: "fold", TurnToken: r.TurnToken}); err != nil {
		t.Fatal(err)
	}
	db := &brokenStore{roomStore: s.db}
	s.db = db
	db.fail.Store(true)
	if err := s.command(r.clients["host"], command{Type: "kick", PlayerID: "guest"}); err == nil {
		t.Fatal("failed durable kick accepted")
	}
	if r.player("guest") == nil || r.clients["guest"] == nil {
		t.Fatal("uncommitted kick removed participant")
	}
	db.fail.Store(false)
	if err := s.command(r.clients["host"], command{Type: "kick", PlayerID: "guest"}); err != nil {
		t.Fatal(err)
	}
	if r.player("guest") != nil || r.clients["guest"] != nil {
		t.Fatal("kick did not remove participant")
	}
	records, _ := db.LoadRooms(context.Background())
	var d roomData
	_ = json.Unmarshal(records[0].Snapshot, &d)
	if len(d.Players) != 1 {
		t.Fatal("kick not persisted")
	}
}

func TestSpectatorVoicePermissionsAndNinePeerRoster(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	for _, id := range []string{"s07", "s02", "s04", "s00", "s01", "s06", "s05", "s03"} {
		r.Players = append(r.Players, &Player{ID: id, Seat: -1, Connected: true})
		r.clients[id] = &client{id: id, room: r, send: make(chan []byte, 64), rates: map[string]rate{}}
	}
	signal := command{Type: "signal", To: "host", Data: json.RawMessage(`{"type":"offer"}`)}
	r.Settings.SpectatorVoiceEnabled = true
	if err := s.command(r.clients["s00"], signal); err == nil {
		t.Fatal("global spectator denial bypassed")
	}
	s.cfg.SpectatorVoiceEnabled = true
	r.Settings.SpectatorVoiceEnabled = false
	if err := s.command(r.clients["s00"], signal); err == nil {
		t.Fatal("room spectator denial bypassed")
	}
	signal.To = "s00"
	if err := s.command(r.clients["host"], signal); err == nil {
		t.Fatal("spectator target denial bypassed")
	}
	r.Settings.SpectatorVoiceEnabled = true
	signal.To = "host"
	ids := r.voiceIDs()
	expected := []string{"host", "guest", "s00", "s01", "s02", "s03", "s04", "s05", "s06"}
	raw, _ := json.Marshal(ids)
	want, _ := json.Marshal(expected)
	if !bytes.Equal(raw, want) {
		t.Fatalf("roster %s want %s", raw, want)
	}
	if err := s.command(r.clients["s00"], signal); err != nil {
		t.Fatal(err)
	}
	if err := s.command(r.clients["s07"], signal); err == nil {
		t.Fatal("tenth participant sent signal")
	}
	signal.To = "s07"
	if err := s.command(r.clients["host"], signal); err == nil {
		t.Fatal("signal target bypassed peer cap")
	}
	view := r.view("host").(map[string]any)
	advertised, _ := json.Marshal(view["voiceParticipantIds"])
	if !bytes.Equal(advertised, want) {
		t.Fatal("advertised voice roster differs from enforcement")
	}
}
func TestVoiceModeNormalizationAndRecovery(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	s.cfg.DefaultVoiceMode = "push-to-talk"
	if err := s.command(r.clients["host"], command{Type: "settings", Settings: r.Settings}); err != nil {
		t.Fatal(err)
	}
	if r.Settings.VoiceMode != "push-to-talk" {
		t.Fatal("system voice default not applied")
	}
	invalid := r.Settings
	invalid.VoiceMode = "unknown"
	if err := s.command(r.clients["host"], command{Type: "settings", Settings: invalid}); err == nil {
		t.Fatal("invalid voice mode accepted")
	}
	r.Settings.VoiceMode = ""
	if err := s.save(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	recovered, err := New(context.Background(), s.cfg, s.auth, s.db)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if recovered.find(r.ID).Settings.VoiceMode != "push-to-talk" {
		t.Fatal("legacy snapshot voice mode not normalized")
	}
	s.cfg.DefaultVoiceMode = ""
	if s.normalizeSettings(RoomSettings{}).VoiceMode != "free" {
		t.Fatal("missing system default not normalized to free")
	}
}

func TestNinePlayerVoiceNegotiationBurstAndSignalLimit(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	peers := []string{"guest"}
	for seat, id := range []string{"peer2", "peer3", "peer4", "peer5", "peer6", "peer7", "peer8"} {
		r.Players = append(r.Players, &Player{ID: id, Seat: seat + 2, Connected: true})
		r.clients[id] = &client{id: id, room: r, send: make(chan []byte, 64), rates: map[string]rate{}}
		peers = append(peers, id)
	}
	sender := r.clients["host"]
	// Eight peer negotiations can send SDP plus many interface/STUN/TURN candidates.
	for i := 0; i < 100; i++ {
		msg := command{Type: "signal", To: peers[i%len(peers)], Data: json.RawMessage(`{"type":"candidate","candidate":{"candidate":"candidate:1 1 udp 2122260223 192.0.2.1 5000 typ host"}}`)}
		if err := s.command(sender, msg); err != nil {
			t.Fatalf("legitimate negotiation message %d rejected: %v", i, err)
		}
	}
	delivered := 0
	for _, id := range peers {
		delivered += len(r.clients[id].send)
	}
	if delivered != 100 {
		t.Fatalf("delivered %d of 100 legitimate signals", delivered)
	}
	for i := 100; i < 240; i++ {
		if err := s.command(sender, command{Type: "signal", To: peers[i%len(peers)], Data: json.RawMessage(`{"type":"ready"}`)}); err != nil {
			t.Fatalf("bounded signal burst %d rejected: %v", i, err)
		}
	}
	if err := s.command(sender, command{Type: "signal", To: "guest", Data: json.RawMessage(`{"type":"ready"}`)}); err == nil {
		t.Fatal("excess signal burst accepted")
	}
	delivered = 0
	for _, id := range peers {
		delivered += len(r.clients[id].send)
	}
	if delivered != 240 {
		t.Fatalf("rate-limited signal forwarded: %d", delivered)
	}
}
