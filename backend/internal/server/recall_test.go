package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestRecallAuthorBroadcastAndRecovery(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	seedPinMessages(r)
	original := r.Messages[0]
	mustPinCommand(t, s, r, "host", "pin_message", original.ID)
	for _, c := range r.clients {
		<-c.send
	}
	mustPinCommand(t, s, r, "guest", "recall_message", original.ID)
	if r.PinnedMessage != nil || !r.Messages[0].Recalled || r.Messages[0].Text != "" || r.Messages[0].At != original.At || r.Messages[0].UserID != original.UserID {
		t.Fatal("recall failed to remove body/pin or preserve authorship/time")
	}
	for _, c := range r.clients {
		var event struct {
			State struct {
				Messages      []Message
				PinnedMessage *Message
				Version       uint64
			}
		}
		if err := json.Unmarshal(<-c.send, &event); err != nil {
			t.Fatal(err)
		}
		if event.State.Version != r.Version || event.State.PinnedMessage != nil || !event.State.Messages[0].Recalled || event.State.Messages[0].Text != "" {
			t.Fatal("recall not broadcast consistently")
		}
	}
	rejectedPinCommand(t, s, r, r.clients["guest"], "recall_message", original.ID)
	rejectedPinCommand(t, s, r, r.clients["host"], "pin_message", original.ID)
	recovered, err := New(context.Background(), s.cfg, s.auth, s.db)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	rr := recovered.find(r.ID)
	if !rr.Messages[0].Recalled || rr.Messages[0].Text != "" || rr.PinnedMessage != nil {
		t.Fatal("recall not durable")
	}
	// Actual legacy JSON with no optional field remains a normal message.
	if rr.Messages[1].Recalled {
		t.Fatal("legacy message was recalled")
	}
}

func TestRecallPermissionTimeAndFeatureBoundaries(t *testing.T) {
	for _, scenario := range []string{"host-other", "guest-other", "stale", "missing", "cross-room", "expired", "future", "system-off", "room-off"} {
		t.Run(scenario, func(t *testing.T) {
			s := testServer(t)
			r := fixtureRoom(s)
			seedPinMessages(r)
			c, id := r.clients["guest"], "first"
			switch scenario {
			case "host-other":
				c = r.clients["host"]
			case "guest-other":
				id = "second"
			case "stale":
				c = &client{id: "guest", room: r}
			case "missing":
				id = ""
			case "cross-room":
				id = "outside"
				s.rooms["another"] = &room{roomData: roomData{Messages: []Message{{ID: id, UserID: "guest", At: time.Now()}}}}
			case "expired":
				r.Messages[0].At = time.Now().Add(-2*time.Minute - time.Second)
			case "future":
				r.Messages[0].At = time.Now().Add(time.Minute)
			case "system-off":
				s.cfg.ChatEnabled = false
			case "room-off":
				r.Settings.ChatEnabled = false
			}
			rejectedPinCommand(t, s, r, c, "recall_message", id)
		})
	}
	// A hand does not affect chat rights, and recall does not affect the hand.
	s := testServer(t)
	r := fixtureRoom(s)
	seedPinMessages(r)
	mustPinCommand(t, s, r, "host", "start", "")
	hand, _ := json.Marshal(r.Hand)
	r.Messages[0].At = time.Now().Add(-119 * time.Second)
	mustPinCommand(t, s, r, "guest", "recall_message", "first")
	after, _ := json.Marshal(r.Hand)
	if string(hand) != string(after) {
		t.Fatal("recall changed active hand")
	}
}

func TestRecallLimitAndTrimmedPin(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	for i := 0; i < 13; i++ {
		id := newID()
		r.Messages = append(r.Messages, Message{ID: id, UserID: "guest", Text: "body", At: time.Now().UTC()})
		if i < 12 {
			mustPinCommand(t, s, r, "guest", "recall_message", id)
		} else {
			rejectedPinCommand(t, s, r, r.clients["guest"], "recall_message", id)
		}
	}
	r.clients["guest"].rates = map[string]rate{}
	seedPinMessages(r)
	mustPinCommand(t, s, r, "host", "pin_message", "first")
	r.Messages = nil
	for i := 0; i < 100; i++ {
		r.Messages = append(r.Messages, Message{ID: newID(), UserID: "host", Text: "later", At: time.Now().UTC()})
	}
	mustPinCommand(t, s, r, "guest", "recall_message", "first")
	if r.PinnedMessage != nil || len(r.Messages) != 100 || r.Messages[99].ID != "first" || !r.Messages[99].Recalled || r.Messages[99].Text != "" {
		t.Fatal("trimmed pin recall lost notice or retained content")
	}
}

func TestRecallPersistenceFailurePreservesBodyAndPin(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	seedPinMessages(r)
	mustPinCommand(t, s, r, "host", "pin_message", "first")
	db := &brokenStore{roomStore: s.db}
	s.db = db
	db.fail.Store(true)
	rejectedPinCommand(t, s, r, r.clients["guest"], "recall_message", "first")
	// Exercise the independent copy after history trimming too.
	r.Messages = r.Messages[1:]
	rejectedPinCommand(t, s, r, r.clients["guest"], "recall_message", "first")
	db.fail.Store(false)
	mustPinCommand(t, s, r, "guest", "recall_message", "first")
}

func TestRecallPreservesAnotherMessagesPin(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	seedPinMessages(r)
	mustPinCommand(t, s, r, "host", "pin_message", "second")
	mustPinCommand(t, s, r, "guest", "recall_message", "first")
	if r.PinnedMessage == nil || *r.PinnedMessage != r.Messages[1] {
		t.Fatal("recall changed unrelated pin")
	}
}
