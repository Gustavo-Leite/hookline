-- +goose Up
CREATE TABLE applications (
  id         UUID        DEFAULT uuidv7() PRIMARY KEY,
  name       TEXT        NOT NULL,
  created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
  updated_at TIMESTAMPTZ DEFAULT now() NOT NULL
);

-- +goose Down
DROP TABLE applications;