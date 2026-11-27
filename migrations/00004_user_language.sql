-- The language someone chose for themselves, which outranks their browser's; empty is
-- none chosen.

-- +goose Up
ALTER TABLE users ADD COLUMN IF NOT EXISTS language text DEFAULT ''::text NOT NULL;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS language;
