package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func visibilitySession(t *testing.T, handler http.Handler, name string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/auth/guest", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	return w.Result().Cookies()[0]
}

func visibilityRequest(handler http.Handler, cookie *http.Cookie, method, path string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestRoomVisibilityHTTPAndLinkJoin(t *testing.T) {
	s := testServer(t)
	h := s.Handler()
	host := visibilitySession(t, h, "Host")
	guest := visibilitySession(t, h, "Friend")
	ids := map[string]string{}
	for _, visibility := range []string{"public", "private", "", "omitted", "invalid"} {
		settings := map[string]any{"smallBlind": 5, "bigBlind": 10, "buyIn": 1000, "maxPlayers": 9, "actionSeconds": 30}
		if visibility != "omitted" {
			settings["visibility"] = visibility
		}
		body, _ := json.Marshal(map[string]any{"name": "Table " + visibility, "settings": settings})
		w := visibilityRequest(h, host, "POST", "/api/rooms", body)
		if visibility == "invalid" || visibility == "" || visibility == "omitted" {
			if w.Code != http.StatusBadRequest {
				t.Fatalf("invalid visibility accepted: %d", w.Code)
			}
			continue
		}
		if w.Code != http.StatusCreated {
			t.Fatalf("create %q: %d %s", visibility, w.Code, w.Body.String())
		}
		var result struct{ ID string }
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		ids[visibility] = result.ID
	}
	for _, cookie := range []*http.Cookie{host, guest} {
		w := visibilityRequest(h, cookie, "GET", "/api/rooms", nil)
		var rooms []struct {
			ID       string
			Settings RoomSettings
		}
		if err := json.Unmarshal(w.Body.Bytes(), &rooms); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || len(rooms) != 1 || strings.Contains(w.Body.String(), ids["private"]) || strings.Contains(w.Body.String(), "Table private") {
			t.Fatalf("private room leaked in lobby: %d %s", w.Code, w.Body.String())
		}
		for _, r := range rooms {
			if r.ID != ids["public"] || r.Settings.Visibility != "public" {
				t.Fatalf("unexpected lobby room: %+v", r)
			}
		}
	}
	path := "/api/rooms/" + ids["private"]
	for _, path := range []string{"/api/rooms", path, path + "/ws"} {
		if w := visibilityRequest(h, nil, "GET", path, nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous access accepted: %s %d", path, w.Code)
		}
	}
	w := visibilityRequest(h, guest, "GET", path, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"visibility":"private"`) {
		t.Fatalf("private link read failed: %d %s", w.Code, w.Body.String())
	}
	ts := httptest.NewServer(h)
	defer ts.Close()
	header := http.Header{"Cookie": []string{guest.String()}, "Origin": []string{ts.URL}}
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+path+"/ws", header)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	var message struct {
		Type  string
		State struct {
			Settings RoomSettings
			Players  []Player
		}
	}
	if err := ws.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	if message.Type != "state" || message.State.Settings.Visibility != "private" || len(message.State.Players) != 1 || message.State.Players[0].Seat != -1 {
		t.Fatalf("private link join failed: %+v", message)
	}
}

func TestRoomVisibilitySettingsPermissionsAndBroadcast(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	settings := r.Settings
	settings.Visibility = "private"
	if err := s.command(r.clients["guest"], command{Type: "settings", Settings: settings}); err == nil {
		t.Fatal("non-host changed visibility")
	}
	if err := s.command(r.clients["host"], command{Type: "settings", Settings: settings}); err != nil {
		t.Fatal(err)
	}
	if r.Settings.Visibility != "private" {
		t.Fatal("private visibility not applied")
	}
	for _, c := range r.clients {
		if raw := <-c.send; !bytes.Contains(raw, []byte(`"visibility":"private"`)) {
			t.Fatalf("visibility not broadcast: %s", raw)
		}
	}
	h := s.Handler()
	cookie := visibilitySession(t, h, "Lobby viewer")
	if w := visibilityRequest(h, cookie, "GET", "/api/rooms", nil); w.Body.String() != "[]\n" {
		t.Fatalf("private table listed: %s", w.Body.String())
	}
	settings.Visibility = ""
	if err := s.command(r.clients["host"], command{Type: "settings", Settings: settings}); err == nil || r.Settings.Visibility != "private" {
		t.Fatal("missing visibility accepted in settings")
	}
	settings.Visibility = "invalid"
	if err := s.command(r.clients["host"], command{Type: "settings", Settings: settings}); err == nil || r.Settings.Visibility != "private" {
		t.Fatal("invalid visibility changed room")
	}
	settings.Visibility = "public"
	if err := s.command(r.clients["host"], command{Type: "settings", Settings: settings}); err != nil {
		t.Fatal(err)
	}
	if w := visibilityRequest(h, cookie, "GET", "/api/rooms", nil); !strings.Contains(w.Body.String(), r.ID) {
		t.Fatal("published table missing from lobby")
	}
	if err := s.command(r.clients["host"], command{Type: "start"}); err != nil {
		t.Fatal(err)
	}
	settings.Visibility = "private"
	if err := s.command(r.clients["host"], command{Type: "settings", Settings: settings}); err == nil || r.Settings.Visibility != "public" {
		t.Fatal("visibility changed during hand")
	}
}

func TestRoomVisibilityPersistenceFailure(t *testing.T) {
	s := testServer(t)
	r := fixtureRoom(s)
	r.Settings.Visibility = "private"
	if err := s.save(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	db := &brokenStore{roomStore: s.db}
	db.fail.Store(true)
	s.db = db
	settings := r.Settings
	settings.Visibility = "public"
	version := r.Version
	if err := s.command(r.clients["host"], command{Type: "settings", Settings: settings}); err == nil {
		t.Fatal("failed save accepted")
	}
	if r.Settings.Visibility != "private" || r.Version != version {
		t.Fatal("failed save exposed private room")
	}
	for _, c := range r.clients {
		if len(c.send) != 0 {
			t.Fatal("failed visibility change broadcast")
		}
	}
	recovered, err := New(context.Background(), s.cfg, s.auth, s.db)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if recovered.find(r.ID).Settings.Visibility != "private" {
		t.Fatal("failed save corrupted persisted visibility")
	}
}

func TestRoomVisibilityRecovery(t *testing.T) {
	for _, visibility := range []string{"private", "public", "", "omitted", "invalid"} {
		t.Run(visibility, func(t *testing.T) {
			s := testServer(t)
			r := fixtureRoom(s)
			r.Settings.Visibility = visibility
			raw, _ := json.Marshal(r.roomData)
			if visibility == "omitted" {
				raw = bytes.Replace(raw, []byte(`"visibility":"omitted",`), nil, 1)
			}
			if err := s.db.SaveRoom(context.Background(), r.ID, r.Name, r.HostID, raw); err != nil {
				t.Fatal(err)
			}
			recovered, err := New(context.Background(), s.cfg, s.auth, s.db)
			if visibility == "invalid" || visibility == "" || visibility == "omitted" {
				if err == nil {
					recovered.Close()
					t.Fatal("missing or invalid visibility recovered")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer recovered.Close()
			want := visibility
			if recovered.find(r.ID).Settings.Visibility != want {
				t.Fatalf("recovered visibility: want %s", want)
			}
		})
	}
}
