package identity

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"river/internal/config"
	"river/internal/store"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

type User = store.User
type Settings = store.Settings
type window struct {
	Count int
	Until time.Time
}
type pending struct{ Until time.Time }
type Service struct {
	cfg    config.Config
	db     *store.Store
	mu     sync.Mutex
	limits map[string]window
	access map[string]pending
	states map[string]pending
	client *http.Client
}

func New(cfg config.Config, db *store.Store) *Service {
	return &Service{cfg: cfg, db: db, limits: map[string]window{}, access: map[string]pending{}, states: map[string]pending{}, client: &http.Client{Timeout: 15 * time.Second}}
}
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		public := s.cfg.Public()
		if _, err := s.User(r); err != nil {
			public.ICEServers = []config.ICEServer{}
		}
		reply(w, 200, public)
	})
	mux.HandleFunc("POST /api/auth/guest", s.limit(s.guest))
	mux.HandleFunc("POST /api/auth/register", s.limit(s.register))
	mux.HandleFunc("POST /api/auth/login", s.limit(s.login))
	mux.HandleFunc("POST /api/auth/access", s.limit(s.grantAccess))
	mux.HandleFunc("GET /api/auth/github", s.limit(s.github))
	mux.HandleFunc("GET /api/auth/github/callback", s.limit(s.githubCallback))
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/me", s.me)
	mux.HandleFunc("PATCH /api/me", s.patch)
	mux.HandleFunc("POST /api/me/avatar", s.upload)
	mux.HandleFunc("GET /api/avatars/{id}", s.avatar)
}
func (s *Service) User(r *http.Request) (*User, error) {
	c, e := r.Cookie("river_session")
	if e != nil || len(c.Value) != 64 {
		return nil, store.ErrNotFound
	}
	u, e := s.db.SessionUser(r.Context(), digest(c.Value))
	if e != nil {
		return nil, e
	}
	return &u, nil
}
func (s *Service) SetEmoji(ctx context.Context, id, emoji string) error {
	if !validEmoji(emoji) {
		return errors.New("emoji too long")
	}
	a, e := s.db.AccountByID(ctx, id)
	if e != nil {
		return e
	}
	a.User.Settings.AvatarEmoji = emoji
	return s.db.UpdateUser(ctx, a.User)
}
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			origin := r.Header.Get("Origin")
			if origin != "" {
				u, e := url.Parse(origin)
				base, _ := url.Parse(s.cfg.BaseURL)
				if e != nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Host != base.Host || u.Scheme != base.Scheme {
					fail(w, 403, "请求来源不允许")
					return
				}
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, 403, "请求来源不允许")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Service) limit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip, _, e := net.SplitHostPort(r.RemoteAddr)
		if e != nil {
			ip = r.RemoteAddr
		}
		if s.cfg.TrustProxy {
			forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
			if n := len(forwarded); n > 0 {
				candidate := strings.TrimSpace(forwarded[n-1])
				if net.ParseIP(candidate) != nil {
					ip = candidate
				}
			}
		}
		now := time.Now()
		s.mu.Lock()
		for k, v := range s.limits {
			if now.After(v.Until) {
				delete(s.limits, k)
			}
		}
		v := s.limits[ip]
		if v.Until.IsZero() {
			v.Until = now.Add(time.Minute)
		}
		v.Count++
		s.limits[ip] = v
		s.mu.Unlock()
		if v.Count > 15 {
			w.Header().Set("Retry-After", "60")
			fail(w, 429, "请求过于频繁，请稍后重试")
			return
		}
		next(w, r)
	}
}
func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, message string) {
	reply(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		fail(w, 415, "需要 JSON 请求")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		fail(w, 400, "请求格式错误")
		return false
	}
	if d.Decode(new(any)) != io.EOF {
		fail(w, 400, "请求格式错误")
		return false
	}
	return true
}
func randomToken() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func digest(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func newUser(name string, guest bool) User {
	return User{ID: randomToken(), Name: name, Guest: guest, Settings: Settings{SoundEnabled: true, Volume: .65, VoiceMuted: true}}
}
func (s *Service) passwordOK(given string) bool {
	a, b := sha256.Sum256([]byte(s.cfg.JoinPassword)), sha256.Sum256([]byte(given))
	return s.cfg.JoinPassword == "" || subtle.ConstantTimeCompare(a[:], b[:]) == 1
}
func validName(v string) bool {
	if utf8.RuneCountInString(v) < 1 || utf8.RuneCountInString(v) > 32 {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func validEmail(v string) bool {
	a, e := mail.ParseAddress(v)
	return e == nil && a.Address == v && len(v) <= 254
}
func (s *Service) cookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: int(ttl.Seconds()), Expires: time.Now().Add(ttl)})
}
func (s *Service) startSession(w http.ResponseWriter, r *http.Request, u User) bool {
	token := randomToken()
	ttl := s.cfg.SessionTTL
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	if e := s.db.CreateSession(r.Context(), digest(token), u.ID, time.Now().Add(ttl)); e != nil {
		fail(w, 500, "无法创建会话")
		return false
	}
	if old, e := r.Cookie("river_session"); e == nil {
		_ = s.db.DeleteSession(r.Context(), digest(old.Value))
	}
	s.cookie(w, "river_session", token, ttl)
	return true
}
func (s *Service) guest(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.GuestEnabled {
		fail(w, 403, "系统配置未开放访客")
		return
	}
	var b struct{ Name, Password string }
	if !decode(w, r, &b) {
		return
	}
	b.Name = strings.TrimSpace(b.Name)
	if !s.passwordOK(b.Password) {
		fail(w, 403, "加入密码错误")
		return
	}
	if !validName(b.Name) {
		fail(w, 400, "昵称需要 1 到 32 个字符")
		return
	}
	u := newUser(b.Name, true)
	if e := s.db.CreateAccount(r.Context(), store.Account{User: u}); e != nil {
		fail(w, 500, "无法创建访客")
		return
	}
	if s.startSession(w, r, u) {
		reply(w, 201, u)
	}
}
func (s *Service) register(w http.ResponseWriter, r *http.Request) {
	var b struct{ Name, Email, Password, JoinPassword string }
	if !decode(w, r, &b) {
		return
	}
	b.Name = strings.TrimSpace(b.Name)
	b.Email = strings.ToLower(strings.TrimSpace(b.Email))
	if !s.passwordOK(b.JoinPassword) {
		fail(w, 403, "加入密码错误")
		return
	}
	if !validName(b.Name) || !validEmail(b.Email) || len(b.Password) < 8 || len(b.Password) > 72 {
		fail(w, 400, "昵称、邮箱或密码无效；密码需要 8 到 72 字节")
		return
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(b.Password), bcrypt.DefaultCost)
	if e != nil {
		fail(w, 500, "无法创建账号")
		return
	}
	u := newUser(b.Name, false)
	u.AvatarURL = gravatar(b.Email)
	if e = s.db.CreateAccount(r.Context(), store.Account{User: u, Email: b.Email, PasswordHash: string(hash)}); e != nil {
		if errors.Is(e, store.ErrConflict) {
			fail(w, 409, "邮箱已注册")
		} else {
			fail(w, 500, "无法创建账号")
		}
		return
	}
	if s.startSession(w, r, u) {
		reply(w, 201, u)
	}
}

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.DefaultCost)

func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	var b struct{ Email, Password, JoinPassword string }
	if !decode(w, r, &b) {
		return
	}
	if !s.passwordOK(b.JoinPassword) {
		fail(w, 403, "加入密码错误")
		return
	}
	if len(b.Password) > 72 {
		fail(w, 401, "邮箱或密码错误")
		return
	}
	a, e := s.db.AccountByEmail(r.Context(), strings.ToLower(strings.TrimSpace(b.Email)))
	hash := []byte(a.PasswordHash)
	if e != nil || len(hash) == 0 {
		hash = dummyHash
	}
	valid := bcrypt.CompareHashAndPassword(hash, []byte(b.Password)) == nil
	if e != nil || !valid || a.PasswordHash == "" {
		fail(w, 401, "邮箱或密码错误")
		return
	}
	if s.startSession(w, r, a.User) {
		reply(w, 200, a.User)
	}
}
func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	if c, e := r.Cookie("river_session"); e == nil {
		if e = s.db.DeleteSession(r.Context(), digest(c.Value)); e != nil {
			fail(w, 500, "退出失败")
			return
		}
	}
	s.cookie(w, "river_session", "", -time.Hour)
	w.WriteHeader(http.StatusNoContent)
}
func (s *Service) me(w http.ResponseWriter, r *http.Request) {
	u, e := s.User(r)
	if e != nil {
		fail(w, 401, "请先登录")
		return
	}
	reply(w, 200, u)
}
func gravatar(email string) string {
	h := md5.Sum([]byte(strings.ToLower(strings.TrimSpace(email))))
	return "https://www.gravatar.com/avatar/" + hex.EncodeToString(h[:]) + "?d=identicon&s=256"
}
func validEmoji(v string) bool {
	return utf8.RuneCountInString(v) <= 8 && len(v) <= 40 && !strings.ContainsAny(v, "\r\n\x00")
}
func validAvatar(v string) bool {
	if v == "" {
		return true
	}
	u, e := url.Parse(v)
	if e != nil || u.User != nil {
		return false
	}
	if u.Scheme == "" && u.Host == "" {
		return strings.HasPrefix(u.Path, "/api/avatars/") && u.RawQuery == "" && u.Fragment == "" && !strings.Contains(u.Path, "..")
	}
	host := strings.ToLower(u.Hostname())
	if u.Scheme != "https" || host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
		return false
	}
	return true
}
func (s *Service) patch(w http.ResponseWriter, r *http.Request) {
	u, e := s.User(r)
	if e != nil {
		fail(w, 401, "请先登录")
		return
	}
	var b struct {
		Name, AvatarURL, GravatarEmail *string
		Settings                       *struct {
			SoundEnabled *bool
			Volume       *float64
			VoiceMuted   *bool
			AvatarEmoji  *string
		}
	}
	if !decode(w, r, &b) {
		return
	}
	if b.Name != nil {
		v := strings.TrimSpace(*b.Name)
		if !validName(v) {
			fail(w, 400, "昵称无效")
			return
		}
		u.Name = v
	}
	if b.AvatarURL != nil {
		if !validAvatar(*b.AvatarURL) {
			fail(w, 400, "头像需要有效的 HTTPS 地址")
			return
		}
		u.AvatarURL = *b.AvatarURL
	}
	if b.GravatarEmail != nil {
		v := strings.ToLower(strings.TrimSpace(*b.GravatarEmail))
		if !validEmail(v) {
			fail(w, 400, "Gravatar 邮箱无效")
			return
		}
		u.AvatarURL = gravatar(v)
	}
	if b.Settings != nil {
		v := b.Settings
		if v.SoundEnabled != nil {
			u.Settings.SoundEnabled = *v.SoundEnabled
		}
		if v.Volume != nil {
			if *v.Volume < 0 || *v.Volume > 1 {
				fail(w, 400, "音量需要在 0 到 1 之间")
				return
			}
			u.Settings.Volume = *v.Volume
		}
		if v.VoiceMuted != nil {
			u.Settings.VoiceMuted = *v.VoiceMuted
		}
		if v.AvatarEmoji != nil {
			if !validEmoji(*v.AvatarEmoji) {
				fail(w, 400, "表情过长")
				return
			}
			u.Settings.AvatarEmoji = *v.AvatarEmoji
		}
	}
	if e = s.db.UpdateUser(r.Context(), *u); e != nil {
		fail(w, 500, "保存用户设置失败")
		return
	}
	reply(w, 200, u)
}
func (s *Service) upload(w http.ResponseWriter, r *http.Request) {
	u, e := s.User(r)
	if e != nil {
		fail(w, 401, "请先登录")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, (2<<20)+(64<<10))
	if e = r.ParseMultipartForm(2 << 20); e != nil {
		fail(w, 400, "头像文件最大 2 MB")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	f, _, e := r.FormFile("file")
	if e != nil {
		fail(w, 400, "请选择头像文件")
		return
	}
	defer f.Close()
	data, e := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if e != nil || len(data) > 2<<20 {
		fail(w, 400, "头像文件最大 2 MB")
		return
	}
	ic, format, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil || ic.Width < 1 || ic.Height < 1 || ic.Width > 4096 || ic.Height > 4096 || ic.Width*ic.Height > 4_000_000 || (format != "png" && format != "jpeg" && format != "gif") {
		fail(w, 400, "需要有效的 PNG、JPEG 或 GIF 图片，最多 400 万像素")
		return
	}
	img, _, e := image.Decode(bytes.NewReader(data))
	if e != nil {
		fail(w, 400, "无法读取头像")
		return
	}
	bounds := img.Bounds()
	side := min(bounds.Dx(), bounds.Dy())
	x0, y0 := bounds.Min.X+(bounds.Dx()-side)/2, bounds.Min.Y+(bounds.Dy()-side)/2
	out := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			out.Set(x, y, img.At(x0+x*side/256, y0+y*side/256))
		}
	}
	var encoded bytes.Buffer
	if e = png.Encode(&encoded, out); e != nil {
		fail(w, 500, "无法保存头像")
		return
	}
	id := randomToken()
	if e = s.db.SaveAvatar(r.Context(), id, "image/png", encoded.Bytes()); e != nil {
		fail(w, 500, "无法保存头像")
		return
	}
	u.AvatarURL = "/api/avatars/" + id
	if e = s.db.UpdateUser(r.Context(), *u); e != nil {
		fail(w, 500, "无法保存头像")
		return
	}
	reply(w, 200, u)
}
func (s *Service) avatar(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if len(id) != 64 {
		http.NotFound(w, r)
		return
	}
	mime, data, e := s.db.Avatar(r.Context(), id)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}
