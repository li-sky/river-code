package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"river/internal/config"
	"river/internal/identity"
	"river/internal/poker"
	"river/internal/store"
)

type RoomSettings struct {
	Visibility            string `json:"visibility"`
	SmallBlind            int64  `json:"smallBlind"`
	BigBlind              int64  `json:"bigBlind"`
	BuyIn                 int64  `json:"buyIn"`
	MaxPlayers            int    `json:"maxPlayers"`
	ActionSeconds         int    `json:"actionSeconds"`
	VoiceEnabled          bool   `json:"voiceEnabled"`
	ChatEnabled           bool   `json:"chatEnabled"`
	ReactionsEnabled      bool   `json:"reactionsEnabled"`
	VoiceMode             string `json:"voiceMode"`
	SpectatorVoiceEnabled bool   `json:"spectatorVoiceEnabled"`
}

func (s RoomSettings) validate() error {
	if s.Visibility != "public" && s.Visibility != "private" {
		return errors.New("房间可见性须为公开或私人")
	}
	if s.VoiceMode != "" && s.VoiceMode != "free" && s.VoiceMode != "push-to-talk" {
		return errors.New("语音模式须为自由发言或按住说话")
	}
	if s.SmallBlind < 1 || s.SmallBlind > 500_000 || s.BigBlind < s.SmallBlind*2 || s.BigBlind > 1_000_000 {
		return errors.New("盲注须为正整数，大盲至少为小盲两倍")
	}
	if s.BuyIn < s.BigBlind*2 || s.BuyIn > 1_000_000_000 {
		return errors.New("买入须在 2 倍大盲至 10 亿筹码之间")
	}
	if s.MaxPlayers < 2 || s.MaxPlayers > 9 {
		return errors.New("牌桌人数须为 2 至 9 人")
	}
	if s.ActionSeconds < 10 || s.ActionSeconds > 120 {
		return errors.New("行动时间须为 10 至 120 秒")
	}
	return nil
}

type Player struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	AvatarURL      string    `json:"avatarUrl"`
	AvatarEmoji    string    `json:"avatarEmoji"`
	Seat           int       `json:"seat"`
	Stack          int64     `json:"stack"`
	Connected      bool      `json:"connected"`
	SittingOut     bool      `json:"sittingOut"`
	DisconnectedAt time.Time `json:"disconnectedAt,omitempty"`
	Leaving        bool      `json:"leaving,omitempty"`
}
type Message struct {
	ID     string    `json:"id"`
	UserID string    `json:"userId"`
	Name   string    `json:"name"`
	Text   string    `json:"text"`
	At     time.Time `json:"at"`
}
type roomData struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	HostID        string       `json:"hostId"`
	Settings      RoomSettings `json:"settings"`
	Players       []*Player    `json:"players"`
	Hand          *poker.Hand  `json:"hand"`
	Messages      []Message    `json:"messages"`
	PinnedMessage *Message     `json:"pinnedMessage"`
	Version       uint64       `json:"version"`
	Deadline      time.Time    `json:"deadline"`
	LastDealer    int          `json:"lastDealer"`
	HandNumber    int          `json:"handNumber"`
	TurnToken     string       `json:"turnToken"`
}
type room struct {
	mu sync.Mutex
	roomData
	clients map[string]*client
	system  *config.Config
}
type client struct {
	ws    *websocket.Conn
	send  chan []byte
	id    string
	room  *room
	rates map[string]rate
	valid func() bool
}
type rate struct {
	start time.Time
	count int
}
type roomStore interface {
	SaveRoom(context.Context, string, string, string, []byte) error
	LoadRooms(context.Context) ([]store.RoomRecord, error)
}
type Server struct {
	cfg    config.Config
	auth   *identity.Service
	db     roomStore
	mu     sync.RWMutex
	rooms  map[string]*room
	cancel context.CancelFunc
}

func New(ctx context.Context, cfg config.Config, auth *identity.Service, db roomStore) (*Server, error) {
	s := &Server{cfg: cfg, auth: auth, db: db, rooms: map[string]*room{}}
	records, err := db.LoadRooms(ctx)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		var d roomData
		if err = json.Unmarshal(record.Snapshot, &d); err != nil {
			return nil, fmt.Errorf("recover room %s: %w", record.ID, err)
		}
		d.Settings = s.normalizeSettings(d.Settings)
		if err := d.Settings.validate(); err != nil {
			return nil, fmt.Errorf("recover room %s settings: %w", record.ID, err)
		}
		if d.ID == "" || d.ID != record.ID {
			return nil, fmt.Errorf("invalid saved room %s", record.ID)
		}
		for _, p := range d.Players {
			p.Connected = false
			p.DisconnectedAt = time.Now()
		}
		if d.Hand != nil && !d.Hand.Finished() {
			if d.TurnToken == "" {
				d.TurnToken = newID()
			}
			if d.Deadline.IsZero() {
				d.Deadline = time.Now()
			}
		}
		if d.Players == nil {
			d.Players = []*Player{}
		}
		if d.Messages == nil {
			d.Messages = []Message{}
		}
		s.rooms[d.ID] = &room{roomData: d, clients: map[string]*client{}, system: &s.cfg}
	}
	runctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	go s.timers(runctx)
	return s, nil
}
func (s *Server) Close() {
	s.cancel()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.rooms {
		r.mu.Lock()
		for _, c := range r.clients {
			if c.ws != nil {
				c.ws.Close()
			}
		}
		r.mu.Unlock()
	}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.auth.Register(mux)

	health := func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if _, err := s.db.LoadRooms(ctx); err != nil {
			writeJSON(w, 503, map[string]string{"status": "unavailable"})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
	}
	mux.HandleFunc("GET /api/health", health)
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /api/rooms", s.listRooms)
	mux.HandleFunc("POST /api/rooms", s.createRoom)
	mux.HandleFunc("GET /api/rooms/{id}", s.getRoom)
	mux.HandleFunc("GET /api/rooms/{id}/ws", s.connect)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, 404, "接口不存在") })
	mux.HandleFunc("/", s.static)
	return s.auth.Middleware(mux)
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
func (s *Server) user(w http.ResponseWriter, r *http.Request) *identity.User {
	u, err := s.auth.User(r)
	if err != nil {
		writeError(w, 401, "请先登录或使用访客身份")
		return nil
	}
	return u
}
func (s *Server) find(id string) *room { s.mu.RLock(); defer s.mu.RUnlock(); return s.rooms[id] }
func (s *Server) listRooms(w http.ResponseWriter, r *http.Request) {
	if s.user(w, r) == nil {
		return
	}
	s.mu.RLock()
	rooms := make([]*room, 0, len(s.rooms))
	for _, rr := range s.rooms {
		rooms = append(rooms, rr)
	}
	s.mu.RUnlock()
	result := []any{}
	for _, rr := range rooms {
		rr.mu.Lock()
		if rr.Settings.Visibility != "public" {
			rr.mu.Unlock()
			continue
		}
		seated := 0
		for _, p := range rr.Players {
			if p.Seat >= 0 {
				seated++
			}
		}
		status := "waiting"
		if rr.Hand != nil && !rr.Hand.Finished() {
			status = "playing"
		}
		result = append(result, map[string]any{"id": rr.ID, "name": rr.Name, "hostId": rr.HostID, "players": seated, "settings": rr.Settings, "status": status})
		rr.mu.Unlock()
	}
	writeJSON(w, 200, result)
}
func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) {
	u := s.user(w, r)
	if u == nil {
		return
	}
	var body struct {
		Name     string       `json:"name"`
		Settings RoomSettings `json:"settings"`
	}
	if err := decode(w, r, &body); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if len([]rune(body.Name)) < 1 || len([]rune(body.Name)) > 48 {
		writeError(w, 400, "房间名称须为 1 至 48 个字符")
		return
	}
	body.Settings = s.normalizeSettings(body.Settings)
	if err := body.Settings.validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	owned := 0
	for _, rr := range s.rooms {
		rr.mu.Lock()
		if rr.HostID == u.ID {
			owned++
		}
		rr.mu.Unlock()
	}
	if owned >= 10 || len(s.rooms) >= 1000 {
		writeError(w, 429, "房间数量已达上限")
		return
	}
	id := newID()
	rr := &room{roomData: roomData{ID: id, Name: body.Name, HostID: u.ID, Settings: body.Settings, Players: []*Player{}, Messages: []Message{}, LastDealer: -1, Version: 1}, clients: map[string]*client{}, system: &s.cfg}
	if err := s.save(r.Context(), rr); err != nil {
		writeError(w, 503, "房间保存失败")
		return
	}
	s.rooms[id] = rr
	writeJSON(w, 201, map[string]string{"id": id})
}
func (s *Server) getRoom(w http.ResponseWriter, r *http.Request) {
	u := s.user(w, r)
	if u == nil {
		return
	}
	rr := s.find(r.PathValue("id"))
	if rr == nil {
		writeError(w, 404, "房间不存在")
		return
	}
	rr.mu.Lock()
	defer rr.mu.Unlock()
	writeJSON(w, 200, rr.view(u.ID))
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return errors.New("请求格式无效")
	}
	return nil
}
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.cfg.StaticDir, filepath.Clean("/"+r.URL.Path))
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	if strings.Contains(filepath.Base(r.URL.Path), ".") {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.cfg.StaticDir, "index.html"))
}
func (s *Server) save(ctx context.Context, r *room) error {
	data, err := json.Marshal(r.roomData)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.db.SaveRoom(ctx, r.ID, r.Name, r.HostID, data)
}
func (r *room) view(id string) any {
	var hand any
	if r.Hand != nil {
		raw, _ := json.Marshal(r.Hand.View(id))
		var h map[string]any
		_ = json.Unmarshal(raw, &h)
		h["deadline"] = r.Deadline
		h["turnToken"] = r.TurnToken
		hand = h
	}
	return map[string]any{"id": r.ID, "name": r.Name, "hostId": r.HostID, "settings": r.Settings, "players": r.Players, "hand": hand, "messages": r.Messages, "pinnedMessage": r.PinnedMessage, "version": r.Version, "voiceParticipantIds": r.voiceIDs()}
}
func (r *room) player(id string) *Player {
	for _, p := range r.Players {
		if p.ID == id {
			return p
		}
	}
	return nil
}
func (r *room) active() bool { return r.Hand != nil && !r.Hand.Finished() }
func (r *room) broadcast(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	for _, c := range r.clients {
		c.enqueue(data)
	}
}
func (r *room) states() {
	for id, c := range r.clients {
		data, _ := json.Marshal(map[string]any{"type": "state", "state": r.view(id)})
		c.enqueue(data)
	}
}
func (c *client) enqueue(data []byte) {
	if c.valid != nil && !c.valid() {
		c.ws.Close()
		return
	}
	select {
	case c.send <- data:
	default:
		c.ws.Close()
	}
}
func (c *client) error(err error) {
	data, _ := json.Marshal(map[string]any{"type": "error", "message": err.Error()})
	c.enqueue(data)
}
func (c *client) allow(key string, limit int) bool {
	now := time.Now()
	v := c.rates[key]
	if now.Sub(v.start) > 10*time.Second {
		v = rate{start: now}
	}
	v.count++
	c.rates[key] = v
	return v.count <= limit
}

var upgrader = websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: func(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return origin == "https://"+r.Host || origin == "http://"+r.Host
}}

func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	u := s.user(w, r)
	if u == nil {
		return
	}
	rr := s.find(r.PathValue("id"))
	if rr == nil {
		writeError(w, 404, "房间不存在")
		return
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{ws: ws, send: make(chan []byte, 64), id: u.ID, room: rr, rates: map[string]rate{}, valid: func() bool {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		current, e := s.auth.User(r.Clone(ctx))
		return e == nil && current.ID == u.ID
	}}
	go c.writer()
	rr.mu.Lock()
	old, _ := json.Marshal(rr.roomData)
	p := rr.player(u.ID)
	if p == nil {
		if len(rr.Players) >= 100 {
			rr.mu.Unlock()
			c.error(errors.New("房间已满"))
			ws.Close()
			return
		}
		p = &Player{ID: u.ID, Seat: -1}
		rr.Players = append(rr.Players, p)
	}
	p.Name = u.Name
	p.AvatarURL = u.AvatarURL
	p.AvatarEmoji = u.Settings.AvatarEmoji
	p.Connected = true
	p.Leaving = false
	p.DisconnectedAt = time.Time{}
	if rr.HostID == "" {
		rr.HostID = u.ID
	}
	rr.Version++
	if err = s.save(r.Context(), rr); err != nil {
		rr.restore(old)
		rr.mu.Unlock()
		c.error(errors.New("状态保存失败"))
		ws.Close()
		return
	}
	previous := rr.clients[u.ID]
	rr.clients[u.ID] = c
	if previous != nil {
		previous.ws.Close()
	}
	rr.states()
	rr.mu.Unlock()
	ws.SetReadLimit(64 * 1024)
	_ = ws.SetReadDeadline(time.Now().Add(70 * time.Second))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(70 * time.Second)) })
	defer func() {
		ws.Close()
		rr.mu.Lock()
		defer rr.mu.Unlock()
		if rr.clients[u.ID] != c {
			return
		}
		delete(rr.clients, u.ID)
		if p := rr.player(u.ID); p != nil {
			before, _ := json.Marshal(rr.roomData)
			p.Connected = false
			p.DisconnectedAt = time.Now()
			rr.Version++
			if err := s.save(context.Background(), rr); err != nil {
				rr.restore(before)
				slog.Error("disconnect persistence failed", "room", rr.ID, "error", err)
				return
			}
			rr.states()
		}
	}()
	for {
		var msg command
		if err = ws.ReadJSON(&msg); err != nil {
			return
		}
		if _, authErr := s.auth.User(r); authErr != nil {
			c.error(errors.New("登录已过期，请重新登录"))
			return
		}
		if !c.allow("all", 300) {
			c.error(errors.New("操作过于频繁"))
			continue
		}
		if err = s.command(c, msg); err != nil {
			c.error(err)
		}
		if msg.Type == "leave" && err == nil {
			return
		}
	}
}
func (c *client) writer() {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case data := <-c.send:
			_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.ws.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (r *room) restore(data []byte) { var d roomData; _ = json.Unmarshal(data, &d); r.roomData = d }

func (s *Server) normalizeSettings(settings RoomSettings) RoomSettings {
	if settings.VoiceMode == "" {
		settings.VoiceMode = s.cfg.DefaultVoiceMode
		if settings.VoiceMode == "" {
			settings.VoiceMode = "free"
		}
	}
	return settings
}
func (r *room) voiceIDs() []string {
	ids := []string{}
	if !r.Settings.VoiceEnabled || (r.system != nil && !r.system.VoiceEnabled) {
		return ids
	}
	seated := []*Player{}
	spectators := []*Player{}
	spectatorAllowed := r.system != nil && r.system.SpectatorVoiceEnabled && r.Settings.SpectatorVoiceEnabled
	for _, p := range r.Players {
		if !p.Connected || p.Leaving {
			continue
		}
		if p.Seat >= 0 {
			seated = append(seated, p)
		} else if spectatorAllowed {
			spectators = append(spectators, p)
		}
	}
	sort.Slice(seated, func(i, j int) bool {
		if seated[i].Seat == seated[j].Seat {
			return seated[i].ID < seated[j].ID
		}
		return seated[i].Seat < seated[j].Seat
	})
	sort.Slice(spectators, func(i, j int) bool { return spectators[i].ID < spectators[j].ID })
	for _, group := range [][]*Player{seated, spectators} {
		for _, p := range group {
			if len(ids) >= 9 {
				return ids
			}
			ids = append(ids, p.ID)
		}
	}
	return ids
}
