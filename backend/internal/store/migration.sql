CREATE TABLE IF NOT EXISTS river_users (id text PRIMARY KEY, email text UNIQUE, github_id text UNIQUE, password_hash text NOT NULL DEFAULT '', profile jsonb NOT NULL);
CREATE TABLE IF NOT EXISTS river_sessions (token_hash text PRIMARY KEY, user_id text NOT NULL REFERENCES river_users(id) ON DELETE CASCADE, expires_at timestamptz NOT NULL);
CREATE INDEX IF NOT EXISTS river_sessions_expiry ON river_sessions(expires_at);
CREATE TABLE IF NOT EXISTS river_rooms (id text PRIMARY KEY, name text NOT NULL, host_id text NOT NULL, snapshot jsonb NOT NULL, updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS river_messages (id bigserial PRIMARY KEY, room_id text NOT NULL REFERENCES river_rooms(id) ON DELETE CASCADE, message jsonb NOT NULL);
CREATE INDEX IF NOT EXISTS river_messages_room ON river_messages(room_id,id DESC);
CREATE TABLE IF NOT EXISTS river_avatars (id text PRIMARY KEY, mime_type text NOT NULL, data bytea NOT NULL);
