package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"river/internal/config"
	"river/internal/store"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T) (*Service, http.Handler) {
	t.Helper()
	db, e := store.Open(context.Background(), "")
	if e != nil {
		t.Fatal(e)
	}
	cfg := config.Config{BaseURL: "http://localhost", GuestEnabled: true, JoinPassword: "room-secret", SessionTTL: time.Hour, GitHubClientID: "test", GitHubClientSecret: "secret"}
	s := New(cfg, db)
	m := http.NewServeMux()
	s.Register(m)
	return s, s.Middleware(m)
}
func call(h http.Handler, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func getCookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("missing %s cookie", name)
	return nil
}
func TestEmojiSequencesAndLimits(t *testing.T) {
	s, h := setup(t)
	w := call(h, "POST", "/api/auth/guest", `{"name":"Emoji","password":"room-secret"}`)
	c := getCookie(t, w, "river_session")
	var u User
	if err := json.Unmarshal(w.Body.Bytes(), &u); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		emoji string
		valid bool
	}{
		{"skin tone kiss", "👩🏽‍❤️‍💋‍👨🏻", true},
		{"family", "👨‍👩‍👧‍👦", true},
		{"flag", "🇨🇳", true},
		{"clear", "", true},
		{"maximum code points and bytes", strings.Repeat("😀", 16), true},
		{"too many code points", strings.Repeat("a", 17), false},
		{"too many bytes", strings.Repeat("😀", 17), false},
		{"carriage return", "😀\r", false},
		{"line feed", "😀\n", false},
		{"nul", "😀\x00", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Exercise both persistence entry points with an existing saved value.
			for _, route := range []string{"service", "patch"} {
				if err := s.SetEmoji(context.Background(), u.ID, "🤔"); err != nil {
					t.Fatal(err)
				}
				if route == "service" {
					err := s.SetEmoji(context.Background(), u.ID, tc.emoji)
					if (err == nil) != tc.valid {
						t.Fatalf("SetEmoji valid=%v: %v", tc.valid, err)
					}
				} else {
					body, _ := json.Marshal(map[string]any{"settings": map[string]string{"avatarEmoji": tc.emoji}})
					res := call(h, "PATCH", "/api/me", string(body), c)
					want := http.StatusBadRequest
					if tc.valid {
						want = http.StatusOK
					}
					if res.Code != want {
						t.Fatalf("PATCH status=%d want=%d: %s", res.Code, want, res.Body.String())
					}
				}
				res := call(h, "GET", "/api/me", "", c)
				var saved User
				if err := json.Unmarshal(res.Body.Bytes(), &saved); err != nil {
					t.Fatal(err)
				}
				want := "🤔"
				if tc.valid {
					want = tc.emoji
				}
				if saved.Settings.AvatarEmoji != want {
					t.Fatalf("%s saved %q want %q", route, saved.Settings.AvatarEmoji, want)
				}
			}
		})
	}
}
func TestAccountSessionAndJoinPassword(t *testing.T) {
	_, h := setup(t)
	bad := call(h, "POST", "/api/auth/register", `{"name":"Alice","email":"alice@example.com","password":"longpassword","joinPassword":"wrong"}`)
	if bad.Code != 403 {
		t.Fatal(bad.Code)
	}
	reg := call(h, "POST", "/api/auth/register", `{"name":"Alice","email":"alice@example.com","password":"longpassword","joinPassword":"room-secret"}`)
	if reg.Code != 201 {
		t.Fatal(reg.Code, reg.Body.String())
	}
	c := getCookie(t, reg, "river_session")
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || len(c.Value) != 64 {
		t.Fatal("unsafe cookie")
	}
	if reg.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing cache policy")
	}
	me := call(h, "GET", "/api/me", "", c)
	if me.Code != 200 {
		t.Fatal(me.Code)
	}
	var u User
	if e := json.Unmarshal(me.Body.Bytes(), &u); e != nil {
		t.Fatal(e)
	}
	if u.Guest || u.Name != "Alice" || !strings.HasPrefix(u.AvatarURL, "https://www.gravatar.com/avatar/") {
		t.Fatal(u)
	}
	patch := call(h, "PATCH", "/api/me", `{"settings":{"volume":0.25,"voiceMuted":false,"avatarEmoji":"🤔"}}`, c)
	if patch.Code != 200 {
		t.Fatal(patch.Code, patch.Body.String())
	}
	if !strings.Contains(patch.Body.String(), `"volume":0.25`) {
		t.Fatal(patch.Body.String())
	}
	invalid := call(h, "PATCH", "/api/me", `{"avatarUrl":"javascript:alert(1)"}`, c)
	if invalid.Code != 400 {
		t.Fatal(invalid.Code)
	}
	login := call(h, "POST", "/api/auth/login", `{"email":"ALICE@example.com","password":"longpassword","joinPassword":"room-secret"}`)
	if login.Code != 200 {
		t.Fatal(login.Code, login.Body.String())
	}
	lc := getCookie(t, login, "river_session")
	out := call(h, "POST", "/api/auth/logout", `{}`, lc)
	if out.Code != 204 {
		t.Fatal(out.Code)
	}
	if got := call(h, "GET", "/api/me", "", lc); got.Code != 401 {
		t.Fatal(got.Code)
	}
}
func TestSessionExpiryAndOrigin(t *testing.T) {
	s, h := setup(t)
	w := call(h, "POST", "/api/auth/guest", `{"name":"Guest","password":"room-secret"}`)
	c := getCookie(t, w, "river_session")
	var u User
	_ = json.Unmarshal(w.Body.Bytes(), &u)
	expired := randomToken()
	if e := s.db.CreateSession(context.Background(), digest(expired), u.ID, time.Now().Add(-time.Minute)); e != nil {
		t.Fatal(e)
	}
	if w := call(h, "GET", "/api/me", "", &http.Cookie{Name: "river_session", Value: expired}); w.Code != 401 {
		t.Fatal(w.Code)
	}
	r := httptest.NewRequest("PATCH", "/api/me", strings.NewReader(`{"name":"Bad"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://evil.example")
	r.AddCookie(c)
	out := httptest.NewRecorder()
	h.ServeHTTP(out, r)
	if out.Code != 403 {
		t.Fatal(out.Code)
	}
	if w := call(h, "POST", "/api/auth/guest", `{"name":"guest","password":"wrong"}`); w.Code != 403 {
		t.Fatal(w.Code)
	}
}
func TestAvatarNormalization(t *testing.T) {
	_, h := setup(t)
	w := call(h, "POST", "/api/auth/guest", `{"name":"Guest","password":"room-secret"}`)
	c := getCookie(t, w, "river_session")
	var raw bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 40, 80))
	if e := png.Encode(&raw, img); e != nil {
		t.Fatal(e)
	}
	var body bytes.Buffer
	mp := multipart.NewWriter(&body)
	part, e := mp.CreateFormFile("file", "avatar.png")
	if e != nil {
		t.Fatal(e)
	}
	_, _ = part.Write(raw.Bytes())
	_ = mp.Close()
	r := httptest.NewRequest("POST", "/api/me/avatar", &body)
	r.Header.Set("Content-Type", mp.FormDataContentType())
	r.AddCookie(c)
	out := httptest.NewRecorder()
	h.ServeHTTP(out, r)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	var u User
	_ = json.Unmarshal(out.Body.Bytes(), &u)
	avatar := call(h, "GET", u.AvatarURL, "")
	if avatar.Code != 200 {
		t.Fatal(avatar.Code)
	}
	ic, format, e := image.DecodeConfig(avatar.Body)
	if e != nil || format != "png" || ic.Width != 256 || ic.Height != 256 {
		t.Fatal(ic, format, e)
	}
}

type roundtrip func(*http.Request) (*http.Response, error)

func (f roundtrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestGitHubPasswordStateAndReplay(t *testing.T) {
	s, h := setup(t)
	if w := call(h, "GET", "/api/auth/github", ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	access := call(h, "POST", "/api/auth/access", `{"password":"room-secret"}`)
	ac := getCookie(t, access, "river_access")
	start := call(h, "GET", "/api/auth/github", "", ac)
	if start.Code != 302 {
		t.Fatal(start.Code, start.Body.String())
	}
	stateCookie := getCookie(t, start, "river_oauth_state")
	if strings.Contains(start.Header().Get("Location"), "room-secret") {
		t.Fatal("password leaked in OAuth URL")
	}
	s.client = &http.Client{Transport: roundtrip(func(r *http.Request) (*http.Response, error) {
		text := `{"access_token":"test-token","token_type":"bearer"}`
		if r.URL.Host == "api.github.com" {
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Error("missing token")
			}
			text = `{"id":42,"login":"octocat","name":"Octo","avatar_url":"https://avatars.githubusercontent.com/u/42"}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(text)), Header: make(http.Header)}, nil
	})}
	cb := call(h, "GET", "/api/auth/github/callback?code=test-code&state="+stateCookie.Value, "", stateCookie)
	if cb.Code != 302 {
		t.Fatal(cb.Code, cb.Body.String())
	}
	sess := getCookie(t, cb, "river_session")
	if me := call(h, "GET", "/api/me", "", sess); me.Code != 200 {
		t.Fatal(me.Code)
	}
	if replay := call(h, "GET", "/api/auth/github/callback?code=test-code&state="+stateCookie.Value, "", stateCookie); replay.Code != 403 {
		t.Fatal(replay.Code)
	}
}
func TestAuthRateLimit(t *testing.T) {
	_, h := setup(t)
	for i := 0; i < 16; i++ {
		w := call(h, "POST", "/api/auth/guest", `{"name":"Guest","password":"wrong"}`)
		if i == 15 && w.Code != 429 {
			t.Fatal(w.Code)
		}
	}
}

func TestICECredentialsRequireSession(t *testing.T) {
	s, h := setup(t)
	s.cfg.ICEServers = []config.ICEServer{{URLs: []string{"turn:turn.example"}, Username: "user", Credential: "turn-secret"}}
	public := call(h, "GET", "/api/config", "")
	if strings.Contains(public.Body.String(), "turn-secret") || !strings.Contains(public.Body.String(), `"iceServers":[]`) {
		t.Fatal("ICE credentials exposed before authentication")
	}
	login := call(h, "POST", "/api/auth/guest", `{"name":"Guest","password":"room-secret"}`)
	c := getCookie(t, login, "river_session")
	private := call(h, "GET", "/api/config", "", c)
	if !strings.Contains(private.Body.String(), "turn-secret") {
		t.Fatal("authenticated voice config missing")
	}
}
