package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// TEST_DATABASE_URL opts into a real PostgreSQL round-trip without requiring a database for unit tests.
func TestPostgresDurableRoundTrip(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, e := Open(ctx, url)
	if e != nil {
		t.Fatal("PostgreSQL initialization failed")
	}
	id := fmt.Sprintf("test-%d", time.Now().UnixNano())
	roomID := id + "-room"
	defer func() {
		_, _ = s.db.Exec(ctx, "DELETE FROM river_rooms WHERE id=$1", roomID)
		_, _ = s.db.Exec(ctx, "DELETE FROM river_users WHERE id=$1", id)
		_, _ = s.db.Exec(ctx, "DELETE FROM river_avatars WHERE id=$1", id)
		s.Close()
	}()
	a := Account{User: User{ID: id, Name: "Database player"}, Email: id + "@example.com", PasswordHash: "test-hash"}
	if e = s.CreateAccount(ctx, a); e != nil {
		t.Fatal(e)
	}
	if e = s.CreateAccount(ctx, Account{User: User{ID: id + "duplicate"}, Email: a.Email}); e != ErrConflict {
		t.Fatalf("expected unique constraint: %v", e)
	}
	if e = s.CreateSession(ctx, id, id, time.Now().Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	if e = s.SaveRoom(ctx, roomID, "Durable Room", id, []byte(`{"secretCards":["As","Ks"]}`)); e != nil {
		t.Fatal(e)
	}
	if e = s.SaveMessage(ctx, roomID, json.RawMessage(`{"text":"hello"}`)); e != nil {
		t.Fatal(e)
	}
	if e = s.SaveAvatar(ctx, id, "image/png", []byte("test")); e != nil {
		t.Fatal(e)
	}
	other, e := Open(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	got, e := other.SessionUser(ctx, id)
	if e != nil || got.Name != a.User.Name {
		t.Fatal(got, e)
	}
	got.Settings.AvatarEmoji = "🤔"
	if e = other.UpdateUser(ctx, got); e != nil {
		t.Fatal(e)
	}
	current, e := s.AccountByID(ctx, id)
	if e != nil || current.User.Settings.AvatarEmoji != "🤔" {
		t.Fatal(current, e)
	}
	rooms, e := other.LoadRooms(ctx)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, r := range rooms {
		if r.ID == roomID {
			found = true
			if !json.Valid(r.Snapshot) {
				t.Fatal("corrupt snapshot")
			}
		}
	}
	if !found {
		t.Fatal("room missing after reconnect")
	}
	messages, e := other.LoadMessages(ctx, roomID)
	if e != nil || len(messages) != 1 {
		t.Fatal(len(messages), e)
	}
	mime, data, e := other.Avatar(ctx, id)
	if e != nil || mime != "image/png" || string(data) != "test" {
		t.Fatal(mime, e)
	}
	if e = other.DeleteSession(ctx, id); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SessionUser(ctx, id); e != ErrNotFound {
		t.Fatal(e)
	}
}
