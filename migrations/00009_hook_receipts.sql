-- The ids of the events providers posted, so one posted again is taken once.

-- +goose Up
CREATE TABLE IF NOT EXISTS hook_receipts (
    id bigserial PRIMARY KEY,
    org_id bigint NOT NULL,
    provider text NOT NULL,
    event_id text NOT NULL,
    received_at timestamp with time zone NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_hook_receipts_event ON hook_receipts USING btree (org_id, provider, event_id);
CREATE INDEX IF NOT EXISTS idx_hook_receipts_received_at ON hook_receipts USING btree (received_at);

-- +goose Down
DROP TABLE IF EXISTS hook_receipts;
