package identity

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"river/internal/store"
	"strconv"
	"strings"
	"time"
)

func (s *Service) cleanPending() {
	now := time.Now()
	for k, v := range s.access {
		if now.After(v.Until) {
			delete(s.access, k)
		}
	}
	for k, v := range s.states {
		if now.After(v.Until) {
			delete(s.states, k)
		}
	}
}

// grantAccess validates the global password in a POST body so it never enters URLs or OAuth redirects.
func (s *Service) grantAccess(w http.ResponseWriter, r *http.Request) {
	var b struct{ Password string }
	if !decode(w, r, &b) {
		return
	}
	if !s.passwordOK(b.Password) {
		fail(w, 403, "加入密码错误")
		return
	}
	token := randomToken()
	s.mu.Lock()
	s.cleanPending()
	s.access[digest(token)] = pending{time.Now().Add(5 * time.Minute)}
	s.mu.Unlock()
	s.cookie(w, "river_access", token, 5*time.Minute)
	reply(w, 200, map[string]bool{"ok": true})
}
func (s *Service) github(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Public().GithubEnabled {
		fail(w, 503, "GitHub 登录未配置")
		return
	}
	if s.cfg.JoinPassword != "" {
		c, e := r.Cookie("river_access")
		if e != nil {
			fail(w, 403, "请先验证加入密码")
			return
		}
		s.mu.Lock()
		s.cleanPending()
		p, ok := s.access[digest(c.Value)]
		delete(s.access, digest(c.Value))
		s.mu.Unlock()
		if !ok || time.Now().After(p.Until) {
			fail(w, 403, "加入密码验证已过期")
			return
		}
		s.cookie(w, "river_access", "", -time.Hour)
	}
	state := randomToken()
	s.mu.Lock()
	s.cleanPending()
	s.states[digest(state)] = pending{time.Now().Add(5 * time.Minute)}
	s.mu.Unlock()
	s.cookie(w, "river_oauth_state", state, 5*time.Minute)
	v := url.Values{"client_id": {s.cfg.GitHubClientID}, "redirect_uri": {s.cfg.BaseURL + "/api/auth/github/callback"}, "scope": {"read:user user:email"}, "state": {state}}
	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+v.Encode(), http.StatusFound)
}
func (s *Service) githubCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	c, e := r.Cookie("river_oauth_state")
	if e != nil || len(state) != 64 || c.Value != state {
		fail(w, 403, "GitHub 登录验证失败，请重试")
		return
	}
	s.mu.Lock()
	s.cleanPending()
	p, ok := s.states[digest(state)]
	delete(s.states, digest(state))
	s.mu.Unlock()
	s.cookie(w, "river_oauth_state", "", -time.Hour)
	if !ok || time.Now().After(p.Until) {
		fail(w, 403, "GitHub 登录验证已过期")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" || len(code) > 1024 {
		fail(w, 400, "GitHub 登录未完成")
		return
	}
	form := url.Values{"client_id": {s.cfg.GitHubClientID}, "client_secret": {s.cfg.GitHubClientSecret}, "code": {code}, "redirect_uri": {s.cfg.BaseURL + "/api/auth/github/callback"}}
	req, e := http.NewRequestWithContext(r.Context(), http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	if e != nil {
		fail(w, 502, "GitHub 登录失败")
		return
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, e := s.client.Do(req)
	if e != nil {
		fail(w, 502, "暂时无法连接 GitHub")
		return
	}
	defer resp.Body.Close()
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&token) != nil || token.AccessToken == "" {
		fail(w, 502, "GitHub 授权失败")
		return
	}
	req, e = http.NewRequestWithContext(r.Context(), http.MethodGet, "https://api.github.com/user", nil)
	if e != nil {
		fail(w, 502, "GitHub 登录失败")
		return
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "River-Poker")
	resp2, e := s.client.Do(req)
	if e != nil {
		fail(w, 502, "无法读取 GitHub 用户")
		return
	}
	defer resp2.Body.Close()
	var profile struct {
		ID          int64 `json:"id"`
		Login, Name string
		AvatarURL   string `json:"avatar_url"`
	}
	if resp2.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp2.Body, 128<<10)).Decode(&profile) != nil || profile.ID <= 0 {
		fail(w, 502, "无法读取 GitHub 用户")
		return
	}
	gid := strconv.FormatInt(profile.ID, 10)
	a, e := s.db.AccountByGitHub(r.Context(), gid)
	if e != nil {
		if e != store.ErrNotFound {
			fail(w, 500, "无法读取用户")
			return
		}
		name := strings.TrimSpace(profile.Name)
		if !validName(name) {
			name = profile.Login
		}
		if !validName(name) {
			name = "GitHub 玩家"
		}
		u := newUser(name, false)
		if validAvatar(profile.AvatarURL) {
			u.AvatarURL = profile.AvatarURL
		}
		a = store.Account{User: u, GitHubID: gid}
		if e = s.db.CreateAccount(r.Context(), a); e != nil {
			a, e = s.db.AccountByGitHub(r.Context(), gid)
			if e != nil {
				fail(w, 500, "无法创建用户")
				return
			}
		}
	}
	if s.startSession(w, r, a.User) {
		http.Redirect(w, r, "/", http.StatusFound)
	}
}
