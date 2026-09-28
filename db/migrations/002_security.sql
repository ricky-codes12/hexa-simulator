CREATE TABLE IF NOT EXISTS simulator_users (
  id BIGSERIAL PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('administrator','operator','viewer')),
  password_hash TEXT NOT NULL,
  mfa_secret TEXT NOT NULL DEFAULT '',
  mfa_enabled BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS simulator_sessions (
  token_hash TEXT PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES simulator_users(id) ON DELETE CASCADE,
  csrf_token TEXT NOT NULL,
  user_agent TEXT NOT NULL DEFAULT '',
  ip_address TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS simulator_sessions_user_id_idx ON simulator_sessions(user_id);
CREATE TABLE IF NOT EXISTS simulator_recovery_codes (
  user_id BIGINT NOT NULL REFERENCES simulator_users(id) ON DELETE CASCADE,
  code_hash TEXT NOT NULL,
  used_at TIMESTAMPTZ,
  PRIMARY KEY(user_id, code_hash)
);
