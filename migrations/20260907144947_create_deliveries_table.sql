-- +goose Up
CREATE TABLE deliveries (
  id               UUID        DEFAULT uuidv7() PRIMARY KEY,
  event_id         UUID        NOT NULL REFERENCES events (id) ON DELETE CASCADE,
  endpoint_id      UUID        NOT NULL REFERENCES endpoints (id) ON DELETE CASCADE,
  application_id   UUID        NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
  status           TEXT        NOT NULL DEFAULT 'pending',
  attempt_count    INTEGER     NOT NULL DEFAULT 0,
  next_attempt_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at     TIMESTAMPTZ,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT deliveries_status_check CHECK (status IN ('pending', 'succeeded', 'dead')),
  CONSTRAINT deliveries_event_endpoint_key UNIQUE (event_id, endpoint_id)
);

CREATE INDEX deliveries_claimable_idx
  ON deliveries (next_attempt_at)
  WHERE status = 'pending';

CREATE INDEX deliveries_application_id_created_at_idx
  ON deliveries (application_id, created_at DESC);

CREATE TABLE delivery_attempts (
  id            UUID        DEFAULT uuidv7() PRIMARY KEY,
  delivery_id   UUID        NOT NULL REFERENCES deliveries (id) ON DELETE CASCADE,
  attempt_number INTEGER    NOT NULL,
  status_code   INTEGER,
  error         TEXT,
  duration_ms   INTEGER     NOT NULL,
  attempted_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX delivery_attempts_delivery_id_idx
  ON delivery_attempts (delivery_id, attempted_at DESC);

-- +goose Down
DROP TABLE delivery_attempts;
DROP TABLE deliveries;
