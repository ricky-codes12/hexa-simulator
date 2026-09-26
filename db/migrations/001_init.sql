CREATE TABLE IF NOT EXISTS todos (
  id BIGSERIAL PRIMARY KEY,
  title TEXT NOT NULL CHECK (length(trim(title)) > 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
