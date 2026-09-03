-- +goose Up
CREATE TABLE endpoints (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    application_id UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    url            TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    secret         TEXT NOT NULL,
    event_types    TEXT[] NOT NULL DEFAULT '{}',
    disabled_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT endpoints_url_scheme CHECK (url ~ '^https://')
);

CREATE INDEX endpoints_application_id_idx ON endpoints (application_id);

-- +goose Down
DROP TABLE endpoints;