package config

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}
type Config struct {
	Address, Addr, StaticDir, DatabaseURL, BaseURL, JoinPassword string
	GuestEnabled, VoiceEnabled, ChatEnabled, ReactionsEnabled    bool
	DefaultVoiceMode                                             string
	SpectatorVoiceEnabled                                        bool
	GitHubClientID, GitHubClientSecret                           string
	SessionTTL                                                   time.Duration
	ICEServers                                                   []ICEServer
	TrustProxy                                                   bool
	SecureCookies                                                bool
}
type PublicConfig struct {
	GuestEnabled          bool        `json:"guestEnabled"`
	GithubEnabled         bool        `json:"githubEnabled"`
	PasswordRequired      bool        `json:"passwordRequired"`
	VoiceEnabled          bool        `json:"voiceEnabled"`
	DefaultVoiceMode      string      `json:"defaultVoiceMode"`
	SpectatorVoiceEnabled bool        `json:"spectatorVoiceEnabled"`
	ChatEnabled           bool        `json:"chatEnabled"`
	ReactionsEnabled      bool        `json:"reactionsEnabled"`
	ICEServers            []ICEServer `json:"iceServers"`
}

func (c Config) Public() PublicConfig {
	return PublicConfig{GuestEnabled: c.GuestEnabled, GithubEnabled: c.GitHubClientID != "" && c.GitHubClientSecret != "", PasswordRequired: c.JoinPassword != "", VoiceEnabled: c.VoiceEnabled, DefaultVoiceMode: c.DefaultVoiceMode, SpectatorVoiceEnabled: c.SpectatorVoiceEnabled, ChatEnabled: c.ChatEnabled, ReactionsEnabled: c.ReactionsEnabled, ICEServers: c.ICEServers}
}
func Load() (Config, error) {
	c := Config{Address: env("LISTEN_ADDR", ":8080"), DatabaseURL: os.Getenv("DATABASE_URL"), BaseURL: strings.TrimRight(env("BASE_URL", "http://localhost:8080"), "/"), JoinPassword: os.Getenv("JOIN_PASSWORD"), GitHubClientID: os.Getenv("GITHUB_CLIENT_ID"), GitHubClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"), SessionTTL: 30 * 24 * time.Hour, GuestEnabled: true, VoiceEnabled: true, ChatEnabled: true, ReactionsEnabled: true, DefaultVoiceMode: env("DEFAULT_VOICE_MODE", "free"), ICEServers: []ICEServer{}}
	for _, b := range []struct {
		name   string
		target *bool
	}{{"GUEST_ENABLED", &c.GuestEnabled}, {"VOICE_ENABLED", &c.VoiceEnabled}, {"CHAT_ENABLED", &c.ChatEnabled}, {"REACTIONS_ENABLED", &c.ReactionsEnabled}, {"TRUST_PROXY", &c.TrustProxy}, {"SPECTATOR_VOICE_ENABLED", &c.SpectatorVoiceEnabled}} {
		if v := os.Getenv(b.name); v != "" {
			x, e := strconv.ParseBool(v)
			if e != nil {
				return c, e
			}
			*b.target = x
		}
	}
	if c.DefaultVoiceMode != "free" && c.DefaultVoiceMode != "push-to-talk" {
		return c, errors.New("DEFAULT_VOICE_MODE must be free or push-to-talk")
	}
	c.Addr = c.Address
	c.StaticDir = env("STATIC_DIR", "../frontend/dist")
	u, e := url.Parse(c.BaseURL)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return c, errors.New("BASE_URL must be an absolute HTTP(S) origin")
	}
	c.SecureCookies = u.Scheme == "https"
	if (c.GitHubClientID == "") != (c.GitHubClientSecret == "") {
		return c, errors.New("GitHub OAuth requires both client ID and secret")
	}
	if raw := os.Getenv("ICE_SERVERS_JSON"); raw != "" {
		if e := json.Unmarshal([]byte(raw), &c.ICEServers); e != nil {
			return c, e
		}
		for _, s := range c.ICEServers {
			if len(s.URLs) == 0 {
				return c, errors.New("ICE server requires urls")
			}
			for _, v := range s.URLs {
				if !strings.HasPrefix(v, "stun:") && !strings.HasPrefix(v, "stuns:") && !strings.HasPrefix(v, "turn:") && !strings.HasPrefix(v, "turns:") {
					return c, errors.New("ICE URL must use stun or turn")
				}
			}
		}
	}
	return c, nil
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
