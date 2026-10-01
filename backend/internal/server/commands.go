package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"log/slog"
	"strings"
	"time"

	"river/internal/poker"
)

type command struct {
	TurnToken  string          `json:"turnToken"`
	Type       string          `json:"type"`
	Seat       int             `json:"seat"`
	BuyIn      int64           `json:"buyIn"`
	SittingOut bool            `json:"sittingOut"`
	Action     string          `json:"action"`
	Amount     int64           `json:"amount"`
	Settings   RoomSettings    `json:"settings"`
	PlayerID   string          `json:"playerId"`
	Text       string          `json:"text"`
	MessageID  string          `json:"messageId"`
	To         string          `json:"to"`
	Emoji      string          `json:"emoji"`
	Data       json.RawMessage `json:"data"`
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func (s *Server) command(c *client, m command) error {
	r := c.room
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.clients[c.id] != c {
		return errors.New("连接已被替换")
	}
	p := r.player(c.id)
	if p == nil {
		return errors.New("不在房间中")
	}
	// Signaling and directed reactions carry no persistent game state.
	switch m.Type {
	case "signal":
		if !s.cfg.VoiceEnabled || !r.Settings.VoiceEnabled {
			return errors.New("本桌未开启语音")
		}
		target := r.clients[m.To]
		if target == nil || m.To == c.id {
			return errors.New("语音对象不在线")
		}
		targetPlayer := r.player(m.To)
		if targetPlayer == nil {
			return errors.New("语音对象不在本桌")
		}
		if (p.Seat < 0 || targetPlayer.Seat < 0) && !(s.cfg.SpectatorVoiceEnabled && r.Settings.SpectatorVoiceEnabled) {
			return errors.New("本桌未允许旁观者语音")
		}
		senderAllowed, targetAllowed := false, false
		for _, id := range r.voiceIDs() {
			if id == p.ID {
				senderAllowed = true
			}
			if id == m.To {
				targetAllowed = true
			}
		}
		if !senderAllowed || !targetAllowed {
			return errors.New("语音席位已满，最多同时 9 人")
		}
		if len(m.Data) == 0 || len(m.Data) > 32*1024 || !json.Valid(m.Data) {
			return errors.New("无效语音信令")
		}
		if !c.allow("signal", 240) {
			return errors.New("语音信令过于频繁")
		}
		raw, _ := json.Marshal(map[string]any{"type": "signal", "from": c.id, "data": m.Data})
		target.enqueue(raw)
		return nil
	case "reaction":
		if !s.cfg.ReactionsEnabled || !r.Settings.ReactionsEnabled {
			return errors.New("本桌未开启表情互动")
		}
		if r.player(m.To) == nil || !validEmoji(m.Emoji) {
			return errors.New("无效表情或对象")
		}
		if !c.allow("reaction", 15) {
			return errors.New("表情发送过于频繁")
		}
		r.broadcast(map[string]any{"type": "reaction", "from": c.id, "to": m.To, "emoji": m.Emoji})
		return nil
	}
	before, _ := json.Marshal(r.roomData)
	previousBoard := 0
	previousHand := r.HandNumber
	if r.Hand != nil {
		previousBoard = len(r.Hand.Board)
	}
	sound := ""
	if err := s.mutate(r, p, c, m, &sound); err != nil {
		r.restore(before)
		return err
	}
	r.Version++
	if err := s.save(context.Background(), r); err != nil {
		r.restore(before)
		slog.Error("room persistence failed", "room", r.ID, "error", err)
		return errors.New("保存失败，操作未生效，请重试")
	}
	if m.Type == "kick" {
		if target := r.clients[m.PlayerID]; target != nil {
			delete(r.clients, m.PlayerID)
			target.error(errors.New("房主已将你移出房间"))
			if target.ws != nil {
				_ = target.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4003, "房主已将你移出房间"), time.Now().Add(time.Second))
				target.ws.Close()
			}
		}
	}
	r.states()
	if r.Hand != nil && len(r.Hand.Board) > 0 && (r.HandNumber != previousHand || len(r.Hand.Board) > previousBoard) {
		r.broadcast(map[string]string{"type": "sound", "sound": "deal"})
	}
	if sound != "" {
		r.broadcast(map[string]string{"type": "sound", "sound": sound})
	}
	return nil
}
func validEmoji(s string) bool {
	return s != "" && len([]rune(s)) <= 16 && len(s) <= 64 && !strings.ContainsAny(s, "\r\n\x00")
}
func (s *Server) mutate(r *room, p *Player, c *client, m command, sound *string) error {
	host := p.ID == r.HostID
	switch m.Type {
	case "sit":
		if r.active() {
			return errors.New("请等本手结束后入座")
		}
		if p.Seat >= 0 {
			return errors.New("你已经入座")
		}
		if m.Seat < 0 || m.Seat >= r.Settings.MaxPlayers {
			return errors.New("无效座位")
		}
		for _, other := range r.Players {
			if other.Seat == m.Seat {
				return errors.New("座位已有玩家")
			}
		}
		amount := m.BuyIn
		if amount == 0 {
			amount = r.Settings.BuyIn
		}
		if amount < r.Settings.BigBlind*2 || amount > 1_000_000_000 {
			return errors.New("买入金额超出范围")
		}
		p.Seat = m.Seat
		p.Stack = amount
		p.SittingOut = false
	case "sitout":
		if p.Seat < 0 {
			return errors.New("入座后才可暂离")
		}
		// The active hand keeps its participants; this flag applies to the next deal.
		p.SittingOut = m.SittingOut
	case "stand":
		if r.active() {
			return errors.New("请等本手结束后离座")
		}
		p.Seat = -1
		p.SittingOut = true
	case "start":
		if !host {
			return errors.New("只有房主可以开始下一手")
		}
		if r.active() {
			return errors.New("本手尚未结束")
		}
		seats := []poker.Seat{}
		for _, player := range r.Players {
			if player.Seat >= 0 && player.Stack > 0 && player.Connected && !player.SittingOut {
				if peer := r.clients[player.ID]; peer != nil && peer.valid != nil && !peer.valid() {
					continue
				}
				seats = append(seats, poker.Seat{ID: player.ID, Seat: player.Seat, Stack: player.Stack})
			}
		}
		if len(seats) < 2 {
			return errors.New("至少需要两位在线入座玩家")
		}
		dealer := -1
		for offset := 1; offset <= r.Settings.MaxPlayers; offset++ {
			candidate := (r.LastDealer + offset) % r.Settings.MaxPlayers
			for _, seat := range seats {
				if seat.Seat == candidate {
					dealer = candidate
					break
				}
			}
			if dealer >= 0 {
				break
			}
		}
		h, err := poker.NewHand(r.HandNumber+1, dealer, r.Settings.SmallBlind, r.Settings.BigBlind, seats)
		if err != nil {
			return err
		}
		r.Hand = h
		r.TurnToken = newID()
		r.HandNumber++
		r.LastDealer = dealer
		r.Deadline = time.Now().Add(time.Duration(r.Settings.ActionSeconds) * time.Second)
		r.syncStacks()
		*sound = "deal"
		if r.Hand.Finished() {
			r.Deadline = time.Time{}
			*sound = "win"
		}
	case "action":
		if !r.active() {
			return errors.New("当前没有进行中的牌局")
		}
		if m.TurnToken == "" || m.TurnToken != r.TurnToken {
			return errors.New("行动已更新，请根据最新牌局操作")
		}
		if err := r.Hand.Action(p.ID, m.Action, m.Amount); err != nil {
			return err
		}
		r.TurnToken = newID()
		r.Deadline = time.Now().Add(time.Duration(r.Settings.ActionSeconds) * time.Second)
		r.syncStacks()
		*sound = "chips"
		if m.Action == "fold" {
			*sound = "fold"
		}
		if r.Hand.Finished() {
			r.Deadline = time.Time{}
			*sound = "win"
			r.cleanupLeavers()
		}
	case "settings":
		if !host {
			return errors.New("只有房主可以调整房间设置")
		}
		if r.active() {
			return errors.New("只能在两手之间调整设置")
		}
		m.Settings = s.normalizeSettings(m.Settings)
		if err := m.Settings.validate(); err != nil {
			return err
		}
		for _, player := range r.Players {
			if player.Seat >= m.Settings.MaxPlayers {
				return errors.New("请先让超出新人数上限的玩家离座")
			}
		}
		r.Settings = m.Settings
	case "stack":
		if !host {
			return errors.New("只有房主可以调整筹码")
		}
		if r.active() {
			return errors.New("只能在两手之间调整筹码")
		}
		target := r.player(m.PlayerID)
		if target == nil || target.Seat < 0 {
			return errors.New("玩家尚未入座")
		}
		if m.Amount < 0 || m.Amount > 1_000_000_000 {
			return errors.New("筹码须为 0 至 10 亿")
		}
		target.Stack = m.Amount
	case "kick":
		if !host {
			return errors.New("只有房主可以移出玩家")
		}
		if r.active() {
			return errors.New("只能在两手之间移出玩家")
		}
		if m.PlayerID == p.ID {
			return errors.New("请使用离开房间")
		}
		if r.player(m.PlayerID) == nil {
			return errors.New("玩家不在本桌")
		}
		r.remove(m.PlayerID)
	case "chat":
		if !s.cfg.ChatEnabled || !r.Settings.ChatEnabled {
			return errors.New("本桌未开启文字聊天")
		}
		if !c.allow("chat", 12) {
			return errors.New("发言过于频繁")
		}
		text := strings.TrimSpace(m.Text)
		if text == "" || len([]rune(text)) > 300 || strings.ContainsRune(text, 0) {
			return errors.New("消息须为 1 至 300 个字符")
		}
		r.Messages = append(r.Messages, Message{ID: newID(), UserID: p.ID, Name: p.Name, Text: text, At: time.Now().UTC()})
		if len(r.Messages) > 100 {
			r.Messages = r.Messages[len(r.Messages)-100:]
		}
	case "recall_message":
		if !s.cfg.ChatEnabled || !r.Settings.ChatEnabled {
			return errors.New("本桌未开启文字聊天")
		}
		if !c.allow("recall", 12) {
			return errors.New("撤回操作过于频繁")
		}
		var selected *Message
		for i := range r.Messages {
			if r.Messages[i].ID == m.MessageID {
				selected = &r.Messages[i]
				break
			}
		}
		if selected == nil && r.PinnedMessage != nil && r.PinnedMessage.ID == m.MessageID {
			selected = r.PinnedMessage
		}
		if selected == nil {
			return errors.New("消息不存在或已不在最近聊天记录中")
		}
		if selected.UserID != p.ID {
			return errors.New("只能撤回自己的消息")
		}
		if selected.Recalled {
			return errors.New("消息已撤回")
		}
		age := time.Since(selected.At)
		if age < 0 || age > 2*time.Minute {
			return errors.New("只能撤回两分钟内发送的消息")
		}
		selected.Text = ""
		selected.Recalled = true
		// A retained pin may outlive the rolling history. Keep its recall notice.
		if selected == r.PinnedMessage {
			r.Messages = append(r.Messages, *selected)
			if len(r.Messages) > 100 {
				r.Messages = r.Messages[len(r.Messages)-100:]
			}
		}
		if r.PinnedMessage != nil && r.PinnedMessage.ID == m.MessageID {
			r.PinnedMessage = nil
		}
	case "pin_message", "unpin_message":
		if !host {
			return errors.New("只有房主可以管理置顶消息")
		}
		if !s.cfg.ChatEnabled || !r.Settings.ChatEnabled {
			return errors.New("本桌未开启文字聊天")
		}
		if !c.allow("pin", 12) {
			return errors.New("置顶操作过于频繁")
		}
		if m.Type == "unpin_message" {
			if r.PinnedMessage == nil || m.MessageID != r.PinnedMessage.ID {
				return errors.New("置顶消息已更新，请根据最新消息操作")
			}
			r.PinnedMessage = nil
		} else {
			var selected *Message
			for _, message := range r.Messages {
				if message.ID == m.MessageID {
					// Keep a copy independently of the rolling chat history.
					selected = &message
					break
				}
			}
			if selected == nil || selected.Recalled {
				return errors.New("消息不存在或已不在最近聊天记录中")
			}
			r.PinnedMessage = selected
		}
	case "emoji":
		if !s.cfg.ReactionsEnabled || !r.Settings.ReactionsEnabled {
			return errors.New("本桌未开启表情互动")
		}
		if m.Emoji != "" && !validEmoji(m.Emoji) {
			return errors.New("无效表情")
		}
		if !c.allow("emoji", 15) {
			return errors.New("表情更新过于频繁")
		}
		if err := s.auth.SetEmoji(context.Background(), p.ID, m.Emoji); err != nil {
			return errors.New("表情保存失败")
		}
		p.AvatarEmoji = m.Emoji
	case "leave":
		p.Leaving = true
		p.SittingOut = true
		p.Connected = false
		p.DisconnectedAt = time.Now()
		if !r.active() {
			r.remove(p.ID)
		}
		if host {
			r.migrateHost(p.ID)
		}
	default:
		return errors.New("未知操作")
	}
	return nil
}
func (r *room) syncStacks() {
	if r.Hand == nil {
		return
	}
	for _, hp := range r.Hand.Players {
		if p := r.player(hp.ID); p != nil {
			p.Stack = hp.Stack
		}
	}
}
func (r *room) cleanupLeavers() {
	for i := len(r.Players) - 1; i >= 0; i-- {
		if r.Players[i].Leaving {
			r.Players = append(r.Players[:i], r.Players[i+1:]...)
		}
	}
}
func (r *room) remove(id string) {
	for i, p := range r.Players {
		if p.ID == id {
			r.Players = append(r.Players[:i], r.Players[i+1:]...)
			return
		}
	}
}
func (r *room) migrateHost(exclude string) {
	r.HostID = ""
	for _, p := range r.Players {
		if p.ID != exclude && p.Connected && !p.Leaving {
			r.HostID = p.ID
			return
		}
	}
}
func (s *Server) timers(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(time.Now())
		}
	}
}
func (s *Server) tick(now time.Time) {
	s.mu.RLock()
	rooms := make([]*room, 0, len(s.rooms))
	for _, r := range s.rooms {
		rooms = append(rooms, r)
	}
	s.mu.RUnlock()
	for _, r := range rooms {
		r.mu.Lock()
		before, _ := json.Marshal(r.roomData)
		changed := false
		previousBoard := 0
		if r.Hand != nil {
			previousBoard = len(r.Hand.Board)
		}
		sound := ""
		if r.active() && !r.Deadline.IsZero() && !now.Before(r.Deadline) {
			wasCheck := false
			for _, p := range r.Hand.Players {
				if p.Seat == r.Hand.TurnSeat {
					wasCheck = p.Bet == r.Hand.CurrentBet
				}
			}
			if err := r.Hand.AutoAction(); err == nil {
				r.TurnToken = newID()
				r.syncStacks()
				changed = true
				sound = "fold"
				if wasCheck {
					sound = ""
				}
				r.Deadline = now.Add(time.Duration(r.Settings.ActionSeconds) * time.Second)
				if r.Hand.Finished() {
					r.Deadline = time.Time{}
					r.cleanupLeavers()
					sound = "win"
				}
			} else {
				slog.Error("automatic action failed", "room", r.ID, "error", err)
			}
		}
		if host := r.player(r.HostID); host != nil && !host.Connected && !host.DisconnectedAt.IsZero() && now.Sub(host.DisconnectedAt) >= 30*time.Second {
			r.migrateHost(host.ID)
			changed = true
		}
		if !r.active() {
			for i := len(r.Players) - 1; i >= 0; i-- {
				p := r.Players[i]
				grace := 2 * time.Minute
				if p.Seat >= 0 {
					grace = 5 * time.Minute
				}
				if !p.Connected && !p.DisconnectedAt.IsZero() && now.Sub(p.DisconnectedAt) >= grace {
					r.Players = append(r.Players[:i], r.Players[i+1:]...)
					changed = true
				}
			}
		}
		if changed {
			r.Version++
			if err := s.save(context.Background(), r); err != nil {
				r.restore(before)
				slog.Error("timer persistence failed", "room", r.ID, "error", err)
			} else {
				r.states()
				if r.Hand != nil && len(r.Hand.Board) > previousBoard {
					r.broadcast(map[string]string{"type": "sound", "sound": "deal"})
				}
				if sound != "" {
					r.broadcast(map[string]string{"type": "sound", "sound": sound})
				}
			}
		}
		r.mu.Unlock()
	}
}
