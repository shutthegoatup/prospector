-- The secrets providers post to /hooks/{provider} with, as a key set: each with its own
-- id, a replaced one living on for a while beside the new.

-- +goose Up
CREATE TABLE IF NOT EXISTS hook_keys (
    id bigserial PRIMARY KEY,
    org_id bigint NOT NULL,
    provider text NOT NULL,
    kid text NOT NULL,
    hash text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    expires_at timestamp with time zone
);
CREATE INDEX IF NOT EXISTS idx_hook_keys_for ON hook_keys USING btree (org_id, provider);
CREATE INDEX IF NOT EXISTS idx_hook_keys_expires_at ON hook_keys USING btree (expires_at);

-- +goose Down
DROP TABLE IF EXISTS hook_keys;
