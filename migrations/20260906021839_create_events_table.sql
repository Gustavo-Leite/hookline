-- +goose Up
CREATE TABLE events (
  id              UUID        DEFAULT uuidv7() PRIMARY KEY,
  application_id  UUID        NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
  event_type      TEXT        NOT NULL,
  payload         JSONB       NOT NULL,
  idempotency_key TEXT       ,
  payload_hash    BYTEA      ,
  created_at      TIMESTAMPTZ DEFAULT now() NOT NULL
);

CREATE UNIQUE INDEX events_idempotency_key_idx
  ON events(application_id, idempotency_key) WHERE idempotency_key IS NOT NULL;

CREATE INDEX events_application_id_created_at_idx
  ON events(application_id, created_at DESC);

-- +goose Down
DROP TABLE events;