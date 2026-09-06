-- +goose Up
CREATE TABLE api_keys (
  id             UUID        DEFAULT uuidv7() PRIMARY KEY,
  application_id UUID        NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
  name           TEXT        NOT NULL,
  prefix         TEXT        NOT NULL,
  token_hash     BYTEA       NOT NULL UNIQUE,
  last_used_at   TIMESTAMPTZ,
  expires_at     TIMESTAMPTZ,
  revoked_at     TIMESTAMPTZ,
  created_at     TIMESTAMPTZ DEFAULT now() NOT NULL
);

CREATE INDEX api_keys_application_id_idx
  ON api_keys(application_id);

-- +goose Down
DROP TABLE api_keys;
