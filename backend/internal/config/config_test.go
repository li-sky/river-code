package config

import "testing"

func TestLoadValidation(t *testing.T) {
	for _, key := range []string{"BASE_URL", "GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET", "ICE_SERVERS_JSON", "GUEST_ENABLED", "TRUST_PROXY", "DEFAULT_VOICE_MODE", "SPECTATOR_VOICE_ENABLED"} {
		t.Setenv(key, "")
	}
	t.Setenv("BASE_URL", "https://poker.example")
	t.Setenv("TRUST_PROXY", "true")
	t.Setenv("ICE_SERVERS_JSON", `[{"urls":["turn:turn.example:3478"],"username":"player","credential":"secret"}]`)
	c, e := Load()
	if e != nil || !c.SecureCookies || !c.TrustProxy || len(c.ICEServers) != 1 {
		t.Fatal(c, e)
	}
	t.Setenv("BASE_URL", "https://poker.example/subpath")
	if _, e = Load(); e == nil {
		t.Fatal("base path accepted")
	}
	t.Setenv("BASE_URL", "https://poker.example")
	t.Setenv("GITHUB_CLIENT_ID", "abc")
	if _, e = Load(); e == nil {
		t.Fatal("partial OAuth credentials accepted")
	}
}

func TestVoiceConfiguration(t *testing.T) {
	for _, key := range []string{"BASE_URL", "GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET", "ICE_SERVERS_JSON", "GUEST_ENABLED", "TRUST_PROXY", "DEFAULT_VOICE_MODE", "SPECTATOR_VOICE_ENABLED"} {
		t.Setenv(key, "")
	}
	c, e := Load()
	if e != nil || c.DefaultVoiceMode != "free" || c.SpectatorVoiceEnabled {
		t.Fatal("unexpected voice defaults")
	}
	t.Setenv("DEFAULT_VOICE_MODE", "push-to-talk")
	t.Setenv("SPECTATOR_VOICE_ENABLED", "true")
	c, e = Load()
	if e != nil {
		t.Fatal(e)
	}
	public := c.Public()
	if public.DefaultVoiceMode != "push-to-talk" || !public.SpectatorVoiceEnabled {
		t.Fatal("public voice config missing")
	}
	t.Setenv("DEFAULT_VOICE_MODE", "broadcast")
	if _, e = Load(); e == nil {
		t.Fatal("invalid voice mode accepted")
	}
	t.Setenv("DEFAULT_VOICE_MODE", "free")
	t.Setenv("SPECTATOR_VOICE_ENABLED", "sometimes")
	if _, e = Load(); e == nil {
		t.Fatal("invalid spectator voice bool accepted")
	}
}
