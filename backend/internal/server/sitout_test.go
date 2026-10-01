package server

import (
	"context"
	"testing"
)

func TestSittingOutKeepsSeatAndSkipsNextDeal(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	c := r.clients["guest"]
	p := r.player("guest")
	if err := s.command(c, command{Type: "sitout", SittingOut: true, PlayerID: "host"}); err != nil {
		t.Fatal(err)
	}
	if !p.SittingOut || p.Seat != 1 || p.Stack != 1000 || r.player("host").SittingOut {
		t.Fatal("sitout must preserve the caller's seat/stack and never target another player")
	}
	if err := s.command(r.clients["host"], command{Type: "start"}); err == nil {
		t.Fatal("paused player was included in the next deal")
	}
	// Reload the saved flag through the same snapshot path used on restart.
	snapshots, err := s.db.LoadRooms(context.Background())
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("load persisted sitout: %v", err)
	}
	restored := &room{}
	restored.restore(snapshots[0].Snapshot)
	if paused := restored.player("guest"); paused == nil || !paused.SittingOut || paused.Seat != 1 || paused.Stack != 1000 {
		t.Fatal("sitout was not preserved in the saved room snapshot")
	}
	if err := s.command(c, command{Type: "sitout", SittingOut: false}); err != nil {
		t.Fatal(err)
	}
	if err := s.command(r.clients["host"], command{Type: "start"}); err != nil {
		t.Fatal(err)
	}
	token, seat := r.TurnToken, r.Hand.TurnSeat
	if err := s.command(c, command{Type: "sitout", SittingOut: true}); err != nil {
		t.Fatal(err)
	}
	if r.TurnToken != token || r.Hand.TurnSeat != seat || len(r.Hand.Players) != 2 {
		t.Fatal("sitout changed an active hand")
	}
	var actor string
	for _, player := range r.Hand.Players {
		if player.Seat == seat {
			actor = player.ID
		}
	}
	if err := s.command(r.clients[actor], command{Type: "action", Action: "fold", TurnToken: token}); err != nil {
		t.Fatal(err)
	}
	if err := s.command(r.clients["host"], command{Type: "start"}); err == nil {
		t.Fatal("sitout during a hand did not exclude the next deal")
	}
	if err := s.command(c, command{Type: "stand"}); err != nil {
		t.Fatal(err)
	}
	if err := s.command(c, command{Type: "sitout", SittingOut: false}); err == nil {
		t.Fatal("spectator resumed without taking a seat")
	}
}

func TestSittingOutPersistenceFailureRollsBack(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	db := &brokenStore{roomStore: s.db}
	s.db = db
	db.fail.Store(true)
	version := r.Version
	if err := s.command(r.clients["guest"], command{Type: "sitout", SittingOut: true}); err == nil {
		t.Fatal("failed save was accepted")
	}
	if r.player("guest").SittingOut || r.Version != version {
		t.Fatal("failed sitout changed the room")
	}
	if len(r.clients["host"].send) != 0 || len(r.clients["guest"].send) != 0 {
		t.Fatal("failed sitout was broadcast")
	}
}
