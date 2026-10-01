package server

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
)

func seedPinMessages(r *room) {
	r.Messages = []Message{
		{ID: "first", UserID: "guest", Name: "Guest", Text: "guest announcement", At: time.Now().UTC()},
		{ID: "second", UserID: "host", Name: "Host", Text: "host announcement", At: time.Now().UTC()},
	}
}

func mustPinCommand(t *testing.T, s *Server, r *room, who, kind, id string) {
	t.Helper()
	if err := s.command(r.clients[who], command{Type: kind, MessageID: id, Text: "forged content"}); err != nil {
		t.Fatal(err)
	}
}

func rejectedPinCommand(t *testing.T, s *Server, r *room, c *client, kind, id string) {
	t.Helper()
	before, _ := json.Marshal(r.roomData)
	counts := map[string]int{}
	for id, peer := range r.clients {
		counts[id] = len(peer.send)
	}
	if err := s.command(c, command{Type: kind, MessageID: id}); err == nil {
		t.Fatalf("accepted invalid %s from %s with message %q", kind, c.id, id)
	}
	after, _ := json.Marshal(r.roomData)
	if !bytes.Equal(before, after) {
		t.Fatal("rejected operation changed state")
	}
	for id, peer := range r.clients {
		if counts[id] != len(peer.send) {
			t.Fatal("rejected operation broadcast state")
		}
	}
}

func TestPinPermissionsReplacementAndBroadcast(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	seedPinMessages(r)
	for _, kind := range []string{"pin_message", "unpin_message"} {
		rejectedPinCommand(t, s, r, r.clients["guest"], kind, "first")
	}
	stale := &client{id: "host", room: r}
	rejectedPinCommand(t, s, r, stale, "pin_message", "first")
	s.rooms["other"] = &room{roomData: roomData{ID: "other", Messages: []Message{{ID: "another-room-message", Text: "outside this room"}}}}
	for _, id := range []string{"", "missing", "another-room-message"} {
		rejectedPinCommand(t, s, r, r.clients["host"], "pin_message", id)
	}
	mustPinCommand(t, s, r, "host", "pin_message", "first")
	if *r.PinnedMessage != r.Messages[0] {
		t.Fatal("pin accepted client content instead of original message")
	}
	for _, peer := range r.clients {
		var event struct {
			Type  string
			State struct {
				PinnedMessage *Message `json:"pinnedMessage"`
				Version       uint64
			}
		}
		if err := json.Unmarshal(<-peer.send, &event); err != nil {
			t.Fatal(err)
		}
		if event.Type != "state" || event.State.PinnedMessage == nil || *event.State.PinnedMessage != *r.PinnedMessage || event.State.Version != r.Version {
			t.Fatal("members received inconsistent pinned state")
		}
	}
	mustPinCommand(t, s, r, "host", "pin_message", "second")
	rejectedPinCommand(t, s, r, r.clients["host"], "unpin_message", "first")
	if r.PinnedMessage.ID != "second" {
		t.Fatal("replacement failed")
	}
	mustPinCommand(t, s, r, "host", "unpin_message", "second")
	if r.PinnedMessage != nil || r.view("guest").(map[string]any)["pinnedMessage"] != (*Message)(nil) {
		t.Fatal("unpin failed")
	}
	rejectedPinCommand(t, s, r, r.clients["host"], "unpin_message", "second")
}

func TestPinFeatureGatesAndRateLimit(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	seedPinMessages(r)
	mustPinCommand(t, s, r, "host", "pin_message", "first")
	for _, layer := range []string{"system", "room"} {
		if layer == "system" {
			s.cfg.ChatEnabled = false
		} else {
			r.Settings.ChatEnabled = false
		}
		for _, kind := range []string{"pin_message", "unpin_message"} {
			rejectedPinCommand(t, s, r, r.clients["host"], kind, "first")
		}
		s.cfg.ChatEnabled, r.Settings.ChatEnabled = true, true
	}
	r.clients["host"].rates = map[string]rate{}
	for i := 0; i < 12; i++ {
		mustPinCommand(t, s, r, "host", "pin_message", "first")
	}
	rejectedPinCommand(t, s, r, r.clients["host"], "unpin_message", "first")
}

func TestPinDuringHandAndHostMigration(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	seedPinMessages(r)
	if err := s.command(r.clients["host"], command{Type: "start"}); err != nil {
		t.Fatal(err)
	}
	mustPinCommand(t, s, r, "host", "pin_message", "first")
	if err := s.command(r.clients["host"], command{Type: "leave"}); err != nil {
		t.Fatal(err)
	}
	if r.HostID != "guest" || r.PinnedMessage.ID != "first" {
		t.Fatal("host migration lost pin")
	}
	rejectedPinCommand(t, s, r, r.clients["host"], "pin_message", "second")
	mustPinCommand(t, s, r, "guest", "unpin_message", "first")
	mustPinCommand(t, s, r, "guest", "pin_message", "second")
}

func TestPinRetainedAfterHistoryTrimAndRecovery(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	seedPinMessages(r)
	original := r.Messages[0]
	mustPinCommand(t, s, r, "host", "pin_message", "first")
	for i := 0; i < 105; i++ {
		r.clients["host"].rates = map[string]rate{}
		if err := s.command(r.clients["host"], command{Type: "chat", Text: "new message"}); err != nil {
			t.Fatal(err)
		}
		for _, peer := range r.clients {
			for len(peer.send) > 0 {
				<-peer.send
			}
		}
	}
	if len(r.Messages) != 100 || *r.PinnedMessage != original {
		t.Fatal("history trimming lost pin")
	}
	if err := s.command(r.clients["guest"], command{Type: "leave"}); err != nil {
		t.Fatal(err)
	}
	recovered, err := New(context.Background(), s.cfg, s.auth, s.db)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	rr := recovered.find(r.ID)
	if rr.PinnedMessage == nil || *rr.PinnedMessage != original || rr.player("guest") != nil {
		t.Fatal("recovery lost retained pin after author left")
	}
	mustPinCommand(t, s, r, "host", "unpin_message", "first")
	recoveredUnpinned, err := New(context.Background(), s.cfg, s.auth, s.db)
	if err != nil {
		t.Fatal(err)
	}
	defer recoveredUnpinned.Close()
	if recoveredUnpinned.find(r.ID).PinnedMessage != nil {
		t.Fatal("unpin not durable")
	}
	// Simulate a pre-feature snapshot by actually removing the JSON field.
	raw, _ := json.Marshal(r.roomData)
	var old map[string]json.RawMessage
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	delete(old, "pinnedMessage")
	raw, _ = json.Marshal(old)
	if err := s.db.SaveRoom(context.Background(), r.ID, r.Name, r.HostID, raw); err != nil {
		t.Fatal(err)
	}
	legacy, err := New(context.Background(), s.cfg, s.auth, s.db)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	if legacy.find(r.ID).PinnedMessage != nil {
		t.Fatal("old snapshot manufactured pin")
	}
}

func TestPinPersistenceFailureRollsBackWithoutBroadcast(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	seedPinMessages(r)
	db := &brokenStore{roomStore: s.db}
	s.db = db
	db.fail.Store(true)
	rejectedPinCommand(t, s, r, r.clients["host"], "pin_message", "first")
	db.fail.Store(false)
	mustPinCommand(t, s, r, "host", "pin_message", "first")
	db.fail.Store(true)
	rejectedPinCommand(t, s, r, r.clients["host"], "pin_message", "second")
	rejectedPinCommand(t, s, r, r.clients["host"], "unpin_message", "first")
	db.fail.Store(false)
	mustPinCommand(t, s, r, "host", "unpin_message", "first")
}
