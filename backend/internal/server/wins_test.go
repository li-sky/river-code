package server

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"river/internal/poker"
)

func winActor(r *room) *client {
	for _, hp := range r.Hand.Players {
		if hp.Seat == r.Hand.TurnSeat {
			return r.clients[hp.ID]
		}
	}
	return nil
}

func TestWinCounterActionsAndTimeout(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	for hand := 1; hand <= 2; hand++ {
		if err := s.command(r.clients["host"], command{Type: "start"}); err != nil {
			t.Fatal(err)
		}
		if r.LastCountedHand != hand-1 {
			t.Fatal("starting a hand changed win count")
		}
		actor := winActor(r)
		if hand == 1 {
			if err := s.command(actor, command{Type: "action", Action: "fold", TurnToken: r.TurnToken}); err != nil {
				t.Fatal(err)
			}
		} else {
			r.Deadline = time.Now().Add(-time.Second)
			s.tick(time.Now())
		}
		if !r.Hand.Finished() || r.LastCountedHand != hand {
			t.Fatal("finished hand was not counted")
		}
		winner := r.Hand.Winners[0].ID
		if r.player(winner).Wins != 1 {
			t.Fatalf("wrong winner count: %+v", r.WinCounts)
		}
		before, _ := json.Marshal(r.roomData)
		for range 3 {
			r.view(winner)
			r.syncStacks()
			s.tick(time.Now())
		}
		after, _ := json.Marshal(r.roomData)
		if !bytes.Equal(before, after) {
			t.Fatal("repeated state/timer/stack sync recounted settlement")
		}
	}
	if r.player("host").Wins != 1 || r.player("guest").Wins != 1 {
		t.Fatal("counts did not accumulate by player")
	}
}

func TestWinCounterSplitAndSidePots(t *testing.T) {
	for _, tc := range []struct {
		name     string
		board    []string
		bets     []int64
		cards    [][]string
		expected map[string]int
	}{
		{"split", []string{"As", "Ks", "Qs", "Js", "Ts"}, []int64{100, 100}, [][]string{{"2c", "3d"}, {"4c", "5d"}}, map[string]int{"host": 1, "guest": 1}},
		{"different side-pot winners", []string{"2c", "3d", "4h", "9s", "Kd"}, []int64{100, 200, 200}, [][]string{{"As", "Ah"}, {"Qs", "Qh"}, {"Ts", "Th"}}, map[string]int{"host": 1, "guest": 1, "third": 0}},
		{"one player wins both pots", []string{"2c", "3d", "4h", "9s", "Kd"}, []int64{200, 100, 200}, [][]string{{"As", "Ah"}, {"Qs", "Qh"}, {"Ts", "Th"}}, map[string]int{"host": 1, "guest": 0, "third": 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testServer(t)
			r := fixtureRoom(s)
			if len(tc.bets) == 3 {
				r.Players = append(r.Players, &Player{ID: "third", Name: "Third", Seat: 2, Stack: 700, Connected: true})
				r.clients["third"] = &client{id: "third", room: r, send: make(chan []byte, 64), rates: map[string]rate{}}
			}
			r.HandNumber = 1
			r.TurnToken = newID()
			r.Hand = &poker.Hand{Number: 1, Phase: "river", DealerSeat: 0, TurnSeat: 0, Board: tc.board, MinRaise: 10}
			for i, p := range r.Players {
				stack := int64(900)
				allIn := false
				if len(tc.bets) == 3 {
					stack = 0
					allIn = true
					if i == 2 {
						stack = 700
						allIn = false
						r.Hand.TurnSeat = 2
					}
				}
				p.Stack = stack
				r.Hand.Players = append(r.Hand.Players, poker.Player{ID: p.ID, Seat: p.Seat, Stack: stack, TotalBet: tc.bets[i], Cards: tc.cards[i], AllIn: allIn})
			}
			for !r.Hand.Finished() {
				if err := s.command(winActor(r), command{Type: "action", Action: "check", TurnToken: r.TurnToken}); err != nil {
					t.Fatal(err)
				}
			}
			for id, expected := range tc.expected {
				if r.player(id).Wins != expected {
					t.Fatalf("%s wins=%d, want %d; settlement=%+v", id, r.player(id).Wins, expected, r.Hand.Winners)
				}
			}
		})
	}
}

func TestWinCounterSaveFailureRecoveryAndRetry(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	if err := s.command(r.clients["host"], command{Type: "start"}); err != nil {
		t.Fatal(err)
	}
	actor := winActor(r)
	action := command{Type: "action", Action: "fold", TurnToken: r.TurnToken}
	before, _ := json.Marshal(r.roomData)
	queue := len(r.clients["host"].send)
	db := &brokenStore{roomStore: s.db}
	s.db = db
	db.fail.Store(true)
	if err := s.command(actor, action); err == nil {
		t.Fatal("failed settlement save accepted")
	}
	after, _ := json.Marshal(r.roomData)
	if !bytes.Equal(before, after) || len(r.clients["host"].send) != queue {
		t.Fatal("failed save changed/broadcast wins or hand")
	}
	db.fail.Store(false)
	if err := s.command(actor, action); err != nil {
		t.Fatal(err)
	}
	winner := r.Hand.Winners[0].ID
	if r.player(winner).Wins != 1 || r.LastCountedHand != 1 {
		t.Fatal("retry did not count exactly once")
	}
	if err := s.command(actor, action); err == nil {
		t.Fatal("duplicate action accepted")
	}
	restored, err := New(context.Background(), s.cfg, s.auth, s.db)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	rr := restored.find(r.ID)
	rr.syncStacks()
	if rr.player(winner).Wins != 1 || rr.WinCounts[winner] != 1 || rr.LastCountedHand != 1 {
		t.Fatal("recovery lost/recounted wins")
	}
	raw, _ := json.Marshal(rr.view(winner))
	if bytes.Contains(raw, []byte("winCounts")) || bytes.Contains(raw, []byte("lastCountedHand")) {
		t.Fatal("private win ledger leaked")
	}
}

func TestWinCounterLegacySnapshot(t *testing.T) {
	for _, finished := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "complete"}[finished], func(t *testing.T) {
			s := testServer(t)
			r := fixtureRoom(s)
			if err := s.command(r.clients["host"], command{Type: "start"}); err != nil {
				t.Fatal(err)
			}
			if finished {
				if err := s.command(winActor(r), command{Type: "action", Action: "fold", TurnToken: r.TurnToken}); err != nil {
					t.Fatal(err)
				}
			}
			for _, p := range r.Players {
				p.Wins = 0
			}
			raw, _ := json.Marshal(r.roomData)
			var legacy map[string]any
			json.Unmarshal(raw, &legacy)
			delete(legacy, "winCounts")
			delete(legacy, "lastCountedHand")
			raw, _ = json.Marshal(legacy)
			if err := s.db.SaveRoom(context.Background(), r.ID, r.Name, r.HostID, raw); err != nil {
				t.Fatal(err)
			}
			restored, err := New(context.Background(), s.cfg, s.auth, s.db)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			rr := restored.find(r.ID)
			if finished {
				rr.syncStacks()
				if rr.player(rr.Hand.Winners[0].ID).Wins != 0 || rr.LastCountedHand != 1 {
					t.Fatal("legacy completed hand was backfilled")
				}
			} else {
				rr.Deadline = time.Now().Add(-time.Second)
				restored.tick(time.Now())
				if !rr.Hand.Finished() || rr.player(rr.Hand.Winners[0].ID).Wins != 1 {
					t.Fatal("legacy active hand was not counted at completion")
				}
			}
		})
	}
}

func TestWinCounterImmediateBlindAllIn(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	for _, p := range r.Players {
		p.Stack = 5
	}
	if err := s.command(r.clients["host"], command{Type: "start"}); err != nil {
		t.Fatal(err)
	}
	if !r.Hand.Finished() || r.LastCountedHand != 1 {
		t.Fatal("blind all-in settlement not counted")
	}
	for _, winner := range r.Hand.Winners {
		if r.player(winner.ID).Wins != 1 {
			t.Fatal("immediate winner missing count")
		}
	}
}

func TestWinCounterTimeoutSaveFailure(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	if err := s.command(r.clients["host"], command{Type: "start"}); err != nil {
		t.Fatal(err)
	}
	r.Deadline = time.Now().Add(-time.Second)
	before, _ := json.Marshal(r.roomData)
	db := &brokenStore{roomStore: s.db}
	s.db = db
	db.fail.Store(true)
	s.tick(time.Now())
	after, _ := json.Marshal(r.roomData)
	if !bytes.Equal(before, after) {
		t.Fatal("failed timeout save changed wins/hand")
	}
	db.fail.Store(false)
	s.tick(time.Now())
	if !r.Hand.Finished() || r.LastCountedHand != 1 || r.player(r.Hand.Winners[0].ID).Wins != 1 {
		t.Fatal("timeout retry lost/doubled wins")
	}
}
