package store

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"sync"
	"time"
)

//go:embed migration.sql
var migration string
var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("already exists")

type Settings struct {
	SoundEnabled bool    `json:"soundEnabled"`
	Volume       float64 `json:"volume"`
	VoiceMuted   bool    `json:"voiceMuted"`
	AvatarEmoji  string  `json:"avatarEmoji"`
}
type User struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	AvatarURL string   `json:"avatarUrl"`
	Guest     bool     `json:"guest"`
	Settings  Settings `json:"settings"`
}
type Account struct {
	User                          User
	Email, GitHubID, PasswordHash string
}
type RoomRecord struct {
	ID, Name, HostID string
	Snapshot         []byte
}
type session struct {
	UserID  string
	Expires time.Time
}
type avatar struct {
	MIME string
	Data []byte
}
type Store struct {
	db       *pgxpool.Pool
	mu       sync.RWMutex
	users    map[string]Account
	sessions map[string]session
	rooms    map[string]RoomRecord
	messages map[string][]json.RawMessage
	avatars  map[string]avatar
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	s := &Store{users: map[string]Account{}, sessions: map[string]session{}, rooms: map[string]RoomRecord{}, messages: map[string][]json.RawMessage{}, avatars: map[string]avatar{}}
	if databaseURL == "" {
		return s, nil
	}
	db, e := pgxpool.New(ctx, databaseURL)
	if e != nil {
		return nil, e
	}
	s.db = db
	if e = db.Ping(ctx); e == nil {
		_, e = db.Exec(ctx, migration)
	}
	if e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) Close() {
	if s.db != nil {
		s.db.Close()
	}
}
func (s *Store) MemoryMode() bool { return s.db == nil }
func (s *Store) CreateAccount(ctx context.Context, a Account) error {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.users[a.User.ID]; ok {
			return ErrConflict
		}
		for _, v := range s.users {
			if a.Email != "" && v.Email == a.Email || a.GitHubID != "" && v.GitHubID == a.GitHubID {
				return ErrConflict
			}
		}
		s.users[a.User.ID] = a
		return nil
	}
	b, e := json.Marshal(a.User)
	if e != nil {
		return e
	}
	_, e = s.db.Exec(ctx, "INSERT INTO river_users(id,email,github_id,password_hash,profile) VALUES($1,NULLIF($2,''),NULLIF($3,''),$4,$5)", a.User.ID, a.Email, a.GitHubID, a.PasswordHash, b)
	if e != nil {
		var pgerr *pgconn.PgError
		if errors.As(e, &pgerr) && pgerr.Code == "23505" {
			return ErrConflict
		}
		return e
	}
	return nil
}
func (s *Store) find(ctx context.Context, key, value string) (Account, error) {
	if s.db == nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		for _, a := range s.users {
			if key == "id" && a.User.ID == value || key == "email" && a.Email == value || key == "github_id" && a.GitHubID == value {
				return a, nil
			}
		}
		return Account{}, ErrNotFound
	}
	var a Account
	var b []byte
	e := s.db.QueryRow(ctx, "SELECT COALESCE(email,''),COALESCE(github_id,''),password_hash,profile FROM river_users WHERE "+key+"=$1", value).Scan(&a.Email, &a.GitHubID, &a.PasswordHash, &b)
	if errors.Is(e, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	if e != nil {
		return a, e
	}
	e = json.Unmarshal(b, &a.User)
	return a, e
}
func (s *Store) AccountByID(ctx context.Context, id string) (Account, error) {
	return s.find(ctx, "id", id)
}
func (s *Store) AccountByEmail(ctx context.Context, email string) (Account, error) {
	return s.find(ctx, "email", email)
}
func (s *Store) AccountByGitHub(ctx context.Context, id string) (Account, error) {
	return s.find(ctx, "github_id", id)
}
func (s *Store) UpdateUser(ctx context.Context, u User) error {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		a, ok := s.users[u.ID]
		if !ok {
			return ErrNotFound
		}
		a.User = u
		s.users[u.ID] = a
		return nil
	}
	b, e := json.Marshal(u)
	if e != nil {
		return e
	}
	tag, e := s.db.Exec(ctx, "UPDATE river_users SET profile=$2 WHERE id=$1", u.ID, b)
	if e == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return e
}
func (s *Store) CreateSession(ctx context.Context, hash, id string, expires time.Time) error {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		for k, v := range s.sessions {
			if time.Now().After(v.Expires) {
				delete(s.sessions, k)
			}
		}
		s.sessions[hash] = session{id, expires}
		return nil
	}
	_, e := s.db.Exec(ctx, "WITH expired AS (DELETE FROM river_sessions WHERE expires_at <= now()) INSERT INTO river_sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3)", hash, id, expires)
	return e
}
func (s *Store) SessionUser(ctx context.Context, hash string) (User, error) {
	var id string
	if s.db == nil {
		s.mu.RLock()
		v, ok := s.sessions[hash]
		s.mu.RUnlock()
		if !ok || time.Now().After(v.Expires) {
			return User{}, ErrNotFound
		}
		id = v.UserID
	} else {
		e := s.db.QueryRow(ctx, "SELECT user_id FROM river_sessions WHERE token_hash=$1 AND expires_at>now()", hash).Scan(&id)
		if errors.Is(e, pgx.ErrNoRows) {
			return User{}, ErrNotFound
		}
		if e != nil {
			return User{}, e
		}
	}
	a, e := s.AccountByID(ctx, id)
	return a.User, e
}
func (s *Store) DeleteSession(ctx context.Context, hash string) error {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.sessions, hash)
		return nil
	}
	_, e := s.db.Exec(ctx, "DELETE FROM river_sessions WHERE token_hash=$1", hash)
	return e
}
func (s *Store) SaveRoom(ctx context.Context, id, name, hostID string, snapshot []byte) error {
	if !json.Valid(snapshot) {
		return errors.New("invalid room JSON")
	}
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.rooms[id] = RoomRecord{id, name, hostID, append([]byte(nil), snapshot...)}
		return nil
	}
	_, e := s.db.Exec(ctx, "INSERT INTO river_rooms(id,name,host_id,snapshot) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET name=$2,host_id=$3,snapshot=$4,updated_at=now()", id, name, hostID, snapshot)
	return e
}
func (s *Store) LoadRooms(ctx context.Context) ([]RoomRecord, error) {
	out := []RoomRecord{}
	if s.db == nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		for _, r := range s.rooms {
			r.Snapshot = append([]byte(nil), r.Snapshot...)
			out = append(out, r)
		}
		return out, nil
	}
	rows, e := s.db.Query(ctx, "SELECT id,name,host_id,snapshot FROM river_rooms ORDER BY updated_at")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var r RoomRecord
		if e = rows.Scan(&r.ID, &r.Name, &r.HostID, &r.Snapshot); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) SaveMessage(ctx context.Context, roomID string, m json.RawMessage) error {
	if !json.Valid(m) {
		return errors.New("invalid message JSON")
	}
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		v := append(s.messages[roomID], append(json.RawMessage(nil), m...))
		if len(v) > 100 {
			v = v[len(v)-100:]
		}
		s.messages[roomID] = v
		return nil
	}
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "INSERT INTO river_messages(room_id,message) VALUES($1,$2)", roomID, []byte(m)); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "DELETE FROM river_messages WHERE room_id=$1 AND id NOT IN (SELECT id FROM river_messages WHERE room_id=$1 ORDER BY id DESC LIMIT 100)", roomID); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) LoadMessages(ctx context.Context, roomID string) ([]json.RawMessage, error) {
	out := []json.RawMessage{}
	if s.db == nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		for _, m := range s.messages[roomID] {
			out = append(out, append(json.RawMessage(nil), m...))
		}
		return out, nil
	}
	rows, e := s.db.Query(ctx, "SELECT message FROM (SELECT id,message FROM river_messages WHERE room_id=$1 ORDER BY id DESC LIMIT 100) m ORDER BY id", roomID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (s *Store) SaveAvatar(ctx context.Context, id, mime string, data []byte) error {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.avatars[id] = avatar{mime, append([]byte(nil), data...)}
		return nil
	}
	_, e := s.db.Exec(ctx, "INSERT INTO river_avatars(id,mime_type,data) VALUES($1,$2,$3)", id, mime, data)
	return e
}
func (s *Store) Avatar(ctx context.Context, id string) (string, []byte, error) {
	if s.db == nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		a, ok := s.avatars[id]
		if !ok {
			return "", nil, ErrNotFound
		}
		return a.MIME, append([]byte(nil), a.Data...), nil
	}
	var mime string
	var data []byte
	e := s.db.QueryRow(ctx, "SELECT mime_type,data FROM river_avatars WHERE id=$1", id).Scan(&mime, &data)
	return mime, data, e
}
