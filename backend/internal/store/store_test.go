package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestMemoryPersistenceIsolation(t *testing.T) {
	ctx := context.Background()
	s, e := Open(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	u := User{ID: "user", Name: "A"}
	if e = s.CreateAccount(ctx, Account{User: u, Email: "a@example.com"}); e != nil {
		t.Fatal(e)
	}
	if e = s.CreateAccount(ctx, Account{User: User{ID: "second"}, Email: "a@example.com"}); e != ErrConflict {
		t.Fatal(e)
	}
	if e = s.CreateSession(ctx, "token", "user", time.Now().Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	if v, e := s.SessionUser(ctx, "token"); e != nil || v.ID != "user" {
		t.Fatal(v, e)
	}
	snapshot := []byte(`{"privateCards":["As","Ks"]}`)
	if e = s.SaveRoom(ctx, "room", "Name", "user", snapshot); e != nil {
		t.Fatal(e)
	}
	snapshot[0] = '!'
	rooms, e := s.LoadRooms(ctx)
	if e != nil || len(rooms) != 1 || !json.Valid(rooms[0].Snapshot) {
		t.Fatal(rooms, e)
	}
	rooms[0].Snapshot[0] = '!'
	rooms, e = s.LoadRooms(ctx)
	if e != nil || !json.Valid(rooms[0].Snapshot) {
		t.Fatal("mutable stored snapshot")
	}
	for i := 0; i < 110; i++ {
		b, _ := json.Marshal(map[string]int{"n": i})
		if e = s.SaveMessage(ctx, "room", b); e != nil {
			t.Fatal(e)
		}
	}
	messages, e := s.LoadMessages(ctx, "room")
	if e != nil || len(messages) != 100 {
		t.Fatal(len(messages), e)
	}
	var first map[string]int
	_ = json.Unmarshal(messages[0], &first)
	if first["n"] != 10 {
		t.Fatal(first)
	}
}
